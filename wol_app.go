package main

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/discover"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/wol"
)

// WakeResult tells where a Wake-on-LAN packet went and whether the device answered.
type WakeResult struct {
	Sent   []string `json:"sent"`   // broadcast addresses
	Via    string   `json:"via"`    // "" = from this computer, else the server it was sent from
	Target string   `json:"target"` // host:port that was watched
	Awake  bool     `json:"awake"`
	Waited int      `json:"waited"` // seconds until it answered (or gave up)
}

// WakeSession sends a Wake-on-LAN packet to the session's MAC address - from
// this computer, or from its jump host / jump station so it reaches the
// device's network - and with wait, watches the session's port for up to
// three minutes until the device answers.
func (a *App) WakeSession(tabID string, s session.Session, wait bool) (WakeResult, error) {
	if err := a.ready(); err != nil {
		return WakeResult{}, err
	}
	s = a.withFolderDefaults(s)
	if strings.TrimSpace(s.MAC) == "" {
		return WakeResult{}, errors.New("no MAC address saved for " + s.Name + " - add it in Session Options → Connection")
	}
	pkt, err := wol.Packet(strings.TrimSpace(s.MAC))
	if err != nil {
		return WakeResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	res, client, release, err := a.sendWake(ctx, tabID, s, pkt)
	if err != nil {
		return WakeResult{}, err
	}
	defer release()
	host, port := wakeTarget(s)
	if host == "" {
		return res, nil
	}
	res.Target = net.JoinHostPort(host, strconv.Itoa(port))
	if !wait {
		return res, nil
	}
	start := time.Now()
	for time.Since(start) < 3*time.Minute {
		var c net.Conn
		if client != nil {
			c, err = client.Dial("tcp", res.Target)
		} else {
			c, err = net.DialTimeout("tcp", res.Target, 2*time.Second)
		}
		if err == nil {
			_ = c.Close()
			res.Awake = true
			break
		}
		select {
		case <-ctx.Done():
			res.Waited = int(time.Since(start).Seconds())
			return res, nil
		case <-time.After(3 * time.Second):
		}
	}
	res.Waited = int(time.Since(start).Seconds())
	return res, nil
}

// wolBroadcast is where the packet goes - only the device's own network:
// the session's broadcast address, else the broadcast address of its
// network, else of the network its IP address is on (this computer's
// interface on it, or its /24). "" only when none of them is known.
func wolBroadcast(s session.Session) string {
	if s.WOLBroadcast != "" {
		return s.WOLBroadcast
	}
	if n := strings.TrimSpace(s.WOLNetwork); n != "" {
		if p, err := discover.NetworkOf(n); err == nil {
			return lastAddr(p)
		}
	}
	host, _ := wakeTarget(s)
	if s.Protocol == session.ProtoWOL {
		host = strings.TrimSpace(s.Hostname)
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.Is4() {
		if p, ok := discover.LocalPrefix(ip); ok && p.Addr().Is4() {
			return lastAddr(p)
		}
		return lastAddr(netip.PrefixFrom(ip, 24))
	}
	return ""
}

// lastAddr is the broadcast (last) address of an IPv4 network.
func lastAddr(p netip.Prefix) string {
	b := p.Masked().Addr().As4()
	host := 32 - p.Bits()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	if host > 0 && host < 32 {
		v |= (1 << host) - 1
	}
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}).String()
}

// wakeAddr is an IPv4 address of the device's network: the broadcast address
// or network saved with the session, else the device's own address.
func wakeAddr(s session.Session) (netip.Addr, bool) {
	if b := strings.TrimSpace(s.WOLBroadcast); b != "" {
		ip, err := netip.ParseAddr(b)
		return ip, err == nil && ip.Is4()
	}
	if n := strings.TrimSpace(s.WOLNetwork); n != "" {
		if p, err := discover.NetworkOf(n); err == nil && p.Addr().Is4() {
			return p.Addr(), true
		}
	}
	host, _ := wakeTarget(s)
	if s.Protocol == session.ProtoWOL {
		host = strings.TrimSpace(s.Hostname)
	}
	ip, err := netip.ParseAddr(host)
	return ip, err == nil && ip.Is4()
}

// wakeFromHere says whether the packet is sent from this computer. "local" and
// "jump" are the session's choice; otherwise (automatic) it is this computer
// when it has an interface on the device's network - a broadcast from there
// reaches the device, as a phone on the same Wi-Fi does - and the jump host
// / jump station when it has not (a broadcast does not cross routers).
func wakeFromHere(s session.Session) bool {
	switch s.WOLFrom {
	case "local":
		return true
	case "jump":
		return false
	}
	if s.JumpSession == "" {
		if _, hop := session.HopOf(s); !hop {
			return true
		}
	}
	if ip, ok := wakeAddr(s); ok {
		_, on := discover.LocalPrefix(ip)
		return on
	}
	return false
}

// sendWake sends the packet from this computer or the route's SSH server and
// returns that server's connection (nil = this computer) for the checks.
func (a *App) sendWake(ctx context.Context, tabID string, s session.Session, pkt []byte) (WakeResult, *ssh.Client, func(), error) {
	var res WakeResult
	var client *ssh.Client
	release := func() {}
	bc := wolBroadcast(s)
	if !wakeFromHere(s) {
		c, rel, via, err := a.terms.RouteClient(ctx, tabID, s, tabID)
		if err != nil {
			return res, nil, release, err
		}
		client, release, res.Via = c, rel, via
	}
	var err error
	if client != nil {
		cmd, err := wol.RemoteCommand(pkt, s.MAC, bc, s.WOLPort)
		if err != nil {
			release()
			return res, nil, func() {}, err
		}
		ss, err := client.NewSession()
		if err != nil {
			release()
			return res, nil, func() {}, err
		}
		out, err := ss.CombinedOutput(cmd)
		_ = ss.Close()
		if err != nil {
			release()
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return res, nil, func() {}, errors.New("sending from " + res.Via + " failed (it needs python3 or wakeonlan): " + msg)
		}
		if bc == "" {
			bc = "255.255.255.255"
		}
		res.Sent = []string{bc}
	} else if res.Sent, err = wol.Send(pkt, bc, s.WOLPort); err != nil {
		return res, nil, release, err
	}
	return res, client, release, nil
}

// wakeTarget is the address and port that show the device is up.
func wakeTarget(s session.Session) (string, int) {
	if s.Protocol == session.ProtoWOL {
		return "", 0 // watched by ping (WakeDevice)
	}
	if h, ok := session.HopOf(s); ok && s.Protocol == session.ProtoSSH2 {
		t := h.Target
		return t[strings.LastIndex(t, "@")+1:], 22
	}
	port := s.Port
	if port == 0 {
		port = map[string]int{"SSH2": 22, "Telnet": 23, "RDP": 3389, "VNC": 5900}[s.Protocol]
	}
	if s.Hostname == "" || port == 0 {
		return "", 0
	}
	return s.Hostname, port
}

// WakeNetworkFor is the network a device's IP address is on, to fill in the
// Wake-on-LAN session's Network box: the network of this computer's interface
// on it, else the /24 around the address. "" when it is not an IPv4 address.
func (a *App) WakeNetworkFor(ip string) string { return networkOfIP(ip) }

func networkOfIP(ip string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil || !addr.Is4() {
		return ""
	}
	if p, ok := discover.LocalPrefix(addr); ok {
		return p.String()
	}
	return netip.PrefixFrom(addr, 24).Masked().String()
}
