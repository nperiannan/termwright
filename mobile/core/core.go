// Package twcore is the part of Termwright that the Android app uses, built
// into an Android library with gomobile (see mobile/README.md). It reuses the
// desktop code: the session import and the Wake-on-LAN packet and commands.
//
// gomobile passes only simple types, so lists and records go in and out as JSON.
package twcore

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/wol"
)

// appSession is what the app keeps of a Termwright session.
type appSession struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Folder      string `json:"folder"`
	Protocol    string `json:"protocol"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	MAC         string `json:"mac"`
	WOLNetwork  string `json:"wolNetwork"`
	WOLPort     int    `json:"wolPort"`
	Broadcast   string `json:"broadcast"`
	Knock       string `json:"knock"`
	Jump        string `json:"jump"` // path of the jump host session
	DeviceType  string `json:"deviceType"`
	Environment string `json:"environment"`
	KeyFile     string `json:"keyFile"` // the desktop's key file name; the app asks for the key itself
	// the desktop's wake schedule ("", "daily" or "weekdays", at "HH:MM"): the app offers it as the phone's
	WakeSchedule string `json:"wakeSchedule"`
	WakeAt       string `json:"wakeAt"`
}

// ImportSessions reads a file exported or saved by Termwright or another
// program (Termwright .zip, SecureCRT, PuTTY, MobaXterm, the Termwright CSV
// template...) - name is the file name, which tells the format - and returns
// its sessions as a JSON list. tmpDir is a folder the app may write to (its
// cache folder): Go's default, /data/local/tmp, is closed to apps on Android.
func ImportSessions(tmpDir, name string, data []byte) (string, error) {
	// a zip that lost its name on the way (shared from a chat or a cloud drive)
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) && !strings.HasSuffix(strings.ToLower(name), ".zip") {
		name += ".zip"
	}
	if name == "" || name == ".zip" {
		name = "sessions" + name
	}
	dir, err := os.MkdirTemp(tmpDir, "twimport")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(file, data, 0o600); err != nil {
		return "", err
	}
	st, err := session.NewStore(filepath.Join(dir, "Sessions"))
	if err != nil {
		return "", err
	}
	if _, err := st.Import(file, session.ImportOptions{}); err != nil {
		return "", err
	}
	tree, err := st.Tree()
	if err != nil {
		return "", err
	}
	out := []appSession{}
	var walk func(n *session.Node)
	walk = func(n *session.Node) {
		for _, c := range n.Children {
			if c.IsFolder {
				walk(c)
				continue
			}
			s, err := st.Get(c.Path)
			if err != nil {
				continue
			}
			out = append(out, fromSession(s))
		}
	}
	walk(tree)
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path) })
	b, err := json.Marshal(out)
	return string(b), err
}

func fromSession(s session.Session) appSession {
	folder := ""
	if i := strings.LastIndex(s.Path, "/"); i >= 0 {
		folder = s.Path[:i]
	}
	return appSession{Path: s.Path, Name: s.Name, Folder: folder, Protocol: s.Protocol, Host: s.Hostname, Port: s.Port,
		User: s.Username, MAC: s.MAC, WOLNetwork: s.WOLNetwork, WOLPort: s.WOLPort, Broadcast: s.WOLBroadcast,
		Knock: s.WakeKnock, Jump: s.JumpSession, DeviceType: s.DeviceType, Environment: s.Environment, KeyFile: filepath.Base(s.IdentityFile),
		WakeSchedule: s.WakeSchedule, WakeAt: s.WakeAt}
}

// wakeRequest is what Wake needs: the device, and the jump host to send from
// when the phone is not on the device's network.
type wakeRequest struct {
	Device    appSession `json:"device"`
	LocalNets []string   `json:"localNets"` // the phone's own networks, e.g. "192.168.1.23/24"
	Jump      *jumpHost  `json:"jump"`
	Force     string     `json:"force"` // "" automatic, "local" or "jump"
}

// jumpHost is an SSH login to the jump host (a NAS, a server on the device's network).
type jumpHost struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password"`
	Key        string `json:"key"` // private key (PEM / OpenSSH)
	Passphrase string `json:"passphrase"`
	HostKey    string `json:"hostKey"` // the key accepted before ("" = first time)
}

// wakeResult tells what was done.
type wakeResult struct {
	Sent    []string `json:"sent"`    // broadcast addresses
	Via     string   `json:"via"`     // "" = from the phone, else the jump host
	Knocked string   `json:"knocked"` // "host: ports" connected to for a sleep proxy
	HostKey string   `json:"hostKey"` // the jump host's key (to be saved after the first connection)
}

// HostKeyChanged starts the error message when the jump host's key
// is not the one accepted before.
const HostKeyChanged = "host key changed: "

// Wake sends the magic packet to every MAC address of the device - from the
// phone when it is on the device's network, otherwise from the jump host over
// SSH - and then connects to the device's "knock" ports for a sleep proxy.
func Wake(reqJSON string) (string, error) {
	var req wakeRequest
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		return "", err
	}
	d := req.Device
	macs, err := wol.MACs(d.MAC)
	if err != nil {
		return "", errors.New("the device has no valid MAC address")
	}
	bc := broadcastOf(d, req.LocalNets)
	if err := wol.CheckBroadcast(bc); err != nil {
		return "", err
	}
	local := req.Force == "local" || req.Jump == nil || (req.Force != "jump" && onNetwork(d, req.LocalNets))
	res := wakeResult{}
	ports := wol.KnockPorts(d.Knock)
	host := strings.TrimSpace(d.Host)
	if local {
		for _, mac := range macs {
			pkt, err := wol.Packet(mac)
			if err != nil {
				return "", err
			}
			res.Sent, err = sendUDP(pkt, bc, d.WOLPort)
			if err != nil {
				return "", err
			}
		}
		if len(ports) > 0 && net.ParseIP(host) != nil {
			knockHere(host, ports)
			res.Knocked = knocked(host, ports)
		}
	} else {
		client, key, err := dialJump(req.Jump)
		res.HostKey = key
		if err != nil {
			return "", err
		}
		defer client.Close()
		res.Via = req.Jump.Host
		var cmds []string
		for _, mac := range macs {
			pkt, _ := wol.Packet(mac)
			c, err := wol.RemoteCommand(pkt, mac, bc, d.WOLPort)
			if err != nil {
				return "", err
			}
			cmds = append(cmds, "{ "+c+"; }")
		}
		if out, err := run(client, strings.Join(cmds, " && ")); err != nil {
			msg := strings.TrimSpace(out)
			if msg == "" {
				msg = err.Error()
			}
			return "", errors.New("sending from " + req.Jump.Host + " failed (it needs python3, perl or wakeonlan): " + msg)
		}
		if bc == "" {
			bc = wol.Limited
		}
		res.Sent = []string{bc}
		if len(ports) > 0 && net.ParseIP(host) != nil {
			if c, err := wol.KnockCommand(host, ports); err == nil {
				_, _ = run(client, c)
				res.Knocked = knocked(host, ports)
			}
		}
	}
	b, err := json.Marshal(res)
	return string(b), err
}

// Reachable reports whether host answers on a TCP port within timeoutMs (the
// green dot). Android apps cannot ping without root, so a port is checked.
func Reachable(host string, port, timeoutMs int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), time.Duration(timeoutMs)*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// CheckJump logs in to a jump host and returns its host key (to be accepted by
// the user), or the error.
func CheckJump(jumpJSON string) (string, error) {
	var j jumpHost
	if err := json.Unmarshal([]byte(jumpJSON), &j); err != nil {
		return "", err
	}
	c, key, err := dialJump(&j)
	if err != nil {
		return key, err
	}
	_ = c.Close()
	return key, nil
}

func dialJump(j *jumpHost) (*ssh.Client, string, error) {
	if j == nil || j.Host == "" {
		return nil, "", errors.New("no jump host")
	}
	var auth []ssh.AuthMethod
	if strings.TrimSpace(j.Key) != "" {
		var signer ssh.Signer
		var err error
		if j.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(j.Key), []byte(j.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(j.Key))
		}
		if err != nil {
			return nil, "", errors.New("the private key could not be read: " + err.Error())
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if j.Password != "" {
		pw := j.Password
		auth = append(auth, ssh.Password(pw), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			a := make([]string, len(qs))
			for i := range a {
				a[i] = pw
			}
			return a, nil
		}))
	}
	seen := ""
	cfg := &ssh.ClientConfig{
		User: j.User,
		Auth: auth,
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			seen = fingerprint(k)
			if j.HostKey != "" && j.HostKey != seen {
				return errors.New(HostKeyChanged + seen)
			}
			return nil
		},
		Timeout: 15 * time.Second,
	}
	port := j.Port
	if port == 0 {
		port = 22
	}
	c, err := ssh.Dial("tcp", net.JoinHostPort(j.Host, strconv.Itoa(port)), cfg)
	if err != nil && strings.Contains(err.Error(), HostKeyChanged) {
		err = errors.New(HostKeyChanged + seen)
	}
	return c, seen, err
}

func fingerprint(k ssh.PublicKey) string {
	h := sha256.Sum256(k.Marshal())
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(h[:]), "=")
}

func run(c *ssh.Client, cmd string) (string, error) {
	s, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	out, err := s.CombinedOutput(cmd)
	return string(out), err
}

// sendUDP sends the packet three times to the network's broadcast address and
// to 255.255.255.255. (wol.Send lists the network interfaces, which Android 11
// and later do not allow apps to do.)
func sendUDP(pkt []byte, bc string, port int) ([]string, error) {
	if port <= 0 {
		port = wol.DefaultPort
	}
	targets := []string{wol.Limited}
	if bc != "" && bc != wol.Limited {
		targets = []string{bc, wol.Limited}
	}
	var sent []string
	var last error
	for _, t := range targets {
		c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP(t), Port: port})
		if err != nil {
			last = err
			continue
		}
		ok := false
		for i := 0; i < 3; i++ {
			if _, err := c.Write(pkt); err == nil {
				ok = true
			} else {
				last = err
			}
		}
		_ = c.Close()
		if ok {
			sent = append(sent, t)
		}
	}
	if len(sent) == 0 {
		return nil, last
	}
	return sent, nil
}

func knockHere(host string, ports []int) {
	done := make(chan struct{}, len(ports))
	for _, p := range ports {
		go func(p int) {
			if c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(p)), 2*time.Second); err == nil {
				_ = c.Close()
			}
			done <- struct{}{}
		}(p)
	}
	for range ports {
		<-done
	}
}

func knocked(host string, ports []int) string {
	ps := make([]string, len(ports))
	for i, p := range ports {
		ps[i] = strconv.Itoa(p)
	}
	return host + ": " + strings.Join(ps, ", ")
}

// broadcastOf is the device network's broadcast address: from its Network
// field, else from its IP address (with the phone's prefix when the phone is
// on that network, else /24).
func broadcastOf(d appSession, localNets []string) string {
	if d.Broadcast != "" {
		return d.Broadcast
	}
	if n := strings.TrimSpace(d.WOLNetwork); n != "" {
		if p, err := prefixOf(n); err == nil {
			return lastAddr(p)
		}
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(d.Host))
	if err != nil || !ip.Is4() {
		return ""
	}
	for _, l := range localNets {
		if p, err := netip.ParsePrefix(l); err == nil && p.Masked().Contains(ip) {
			return lastAddr(p.Masked())
		}
	}
	return lastAddr(netip.PrefixFrom(ip, 24).Masked())
}

// onNetwork: the phone has an address in the device's network.
func onNetwork(d appSession, localNets []string) bool {
	var dev []netip.Addr
	if ip, err := netip.ParseAddr(strings.TrimSpace(d.Host)); err == nil {
		dev = append(dev, ip)
	}
	var devNet netip.Prefix
	if p, err := prefixOf(d.WOLNetwork); err == nil {
		devNet = p
	}
	for _, l := range localNets {
		p, err := netip.ParsePrefix(l)
		if err != nil {
			continue
		}
		if devNet.IsValid() && devNet.Contains(p.Addr()) {
			return true
		}
		for _, ip := range dev {
			if p.Masked().Contains(ip) {
				return true
			}
		}
	}
	return false
}

// prefixOf reads "192.168.1.0/24" or "192.168.1.0" (its /24).
func prefixOf(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return netip.Prefix{}, errors.New("no network")
	}
	if !strings.Contains(s, "/") {
		s += "/24"
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return p.Masked(), nil
}

func lastAddr(p netip.Prefix) string {
	a := p.Masked().Addr().As4()
	bits := p.Bits()
	for i := 0; i < 4; i++ {
		for b := 0; b < 8; b++ {
			if i*8+b >= bits {
				a[i] |= 1 << (7 - b)
			}
		}
	}
	return netip.AddrFrom4(a).String()
}
