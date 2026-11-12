package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"github.com/termwright/termwright/internal/emu"
)

// Packet capture on the emulated lab: a switch port's traffic (both ways - a
// cable is captured at one of its ends) written to a pcap file on its host by
// QEMU (filter-dump, added to the running switch, no restart), read back here
// for a live packet list, saved to this computer, or opened in Wireshark live.

// EmuCapture is a capture, running or done.
type EmuCapture struct {
	Key     string `json:"key"` // switch id|port
	VM      string `json:"vm"`
	Switch  string `json:"switch"`
	Port    string `json:"port"`
	Peer    string `json:"peer"` // the other end of its cable ("switch port")
	Host    string `json:"host"`
	File    string `json:"file"` // on the host
	Started string `json:"started"`
	Running bool   `json:"running"`
	Size    int64  `json:"size"`
}

// EmuPacket is one packet of a capture, decoded enough for a list.
type EmuPacket struct {
	No    int     `json:"no"`
	Time  float64 `json:"time"` // seconds since the first packet
	Src   string  `json:"src"`
	Dst   string  `json:"dst"`
	Proto string  `json:"proto"`
	Len   int     `json:"len"`
	Info  string  `json:"info"`
}

// emuCapID is QEMU's object id for a capture on a port.
func emuCapID(port int) string { return fmt.Sprintf("twcap%d", port) }

// emuHome is a host's home folder (QEMU wants an absolute path).
func (a *App) emuHome(host string) (string, error) {
	out, err := a.emuRun(host, "printf '%s' \"$HOME\"", 20*time.Second)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(out), "/") {
		return "", fmt.Errorf("its home folder: %v %s", err, out)
	}
	return strings.TrimSpace(out), nil
}

// EmuCaptureStart starts capturing a switch port (its cable) while it runs.
func (a *App) EmuCaptureStart(id, port string) ([]EmuCapture, error) {
	st := a.emuPollFresh()
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	var v *emuVMRef
	if l != nil {
		if vm := l.VMByID(id); vm != nil {
			im := emuImage(vm.Image)
			if im != nil {
				v = &emuVMRef{vm: *vm, n: emuPort(im, port), name: im.PortNameOf(emuPort(im, port))}
			}
		}
	}
	if v == nil || v.n == 0 {
		emuLab.mu.Unlock()
		return nil, errors.New("no such switch or port")
	}
	host := emuHostOf(v.vm)
	lh := a.emuFor(host)
	cabled := false
	for _, c := range l.Cables {
		cabled = cabled || c.A == id && c.APort == v.n || c.B == id && c.BPort == v.n
	}
	up := false
	for _, u := range v.vm.Uplinks {
		up = up || u == v.n
	}
	key := id + "|" + v.name
	for _, c := range emuLab.f.Captures {
		if c.Key == key && c.Running {
			emuLab.mu.Unlock()
			return nil, errors.New("that port is being captured already")
		}
	}
	topo := orStr(emuLab.f.Topology, "untitled")
	dir := emuLabDir()
	mon := lh.MonitorPort(v.vm)
	emuLab.mu.Unlock()
	if _, ok := st.Up[id]; !ok {
		return nil, errors.New("start the switch first: a running switch is captured")
	}
	if !cabled && !up {
		return nil, fmt.Errorf("%s has no cable: there is nothing on it to capture", v.name)
	}
	home, err := a.emuHome(host)
	if err != nil {
		return nil, err
	}
	safe := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == '/' || r == ':' || r == ' ' {
				return '_'
			}
			return r
		}, s)
	}
	rel := fmt.Sprintf("%s/captures/%s/%s-%s-%s.pcap", dir, safe(topo), safe(v.vm.Name), safe(v.name), time.Now().Format("20060102-150405"))
	if _, err := a.emuRun(host, "mkdir -p "+path.Dir("\"$HOME\"/"+rel), 20*time.Second); err != nil {
		return nil, err
	}
	h, err := a.emuMonitor(host, mon)
	if err != nil {
		return nil, err
	}
	defer h.close()
	_, _ = h.do("object_del "+emuCapID(v.n), 10*time.Second) // one left from before Termwright started again
	out, err := h.do(fmt.Sprintf("object_add filter-dump,id=%s,netdev=n%d,file=%s/%s", emuCapID(v.n), v.n, home, rel), 15*time.Second)
	if err != nil || strings.Contains(strings.ToLower(out), "error") || strings.Contains(out, "not found") {
		return nil, fmt.Errorf("QEMU did not start the capture: %v %s", err, out)
	}
	c := EmuCapture{Key: key, VM: id, Switch: v.vm.Name, Port: v.name, Host: host, File: rel, Started: time.Now().Format(time.RFC3339), Running: true}
	emuLab.mu.Lock()
	c.Peer = emuPeerText(id, v.n)
	emuLab.f.Captures = append(emuLab.f.Captures, c)
	_ = a.emuSave()
	emuEv(id, "info", fmt.Sprintf("capturing %s%s into %s on %s", v.name, map[bool]string{true: " (the cable to " + c.Peer + ")", false: ""}[c.Peer != ""], rel, host[strings.LastIndex(host, "/")+1:]))
	emuLab.mu.Unlock()
	return a.EmuCaptures(), nil
}

type emuVMRef struct {
	vm   emu.VM
	n    int
	name string
}

// emuPeerText is the other end of a port's cable (emuLab.mu held).
func emuPeerText(id string, n int) string {
	l := emuLab.f.Lab
	for _, c := range l.Cables {
		oid, on := "", 0
		if c.A == id && c.APort == n {
			oid, on = c.B, c.BPort
		} else if c.B == id && c.BPort == n {
			oid, on = c.A, c.APort
		}
		if o := l.VMByID(oid); o != nil {
			if im := emuImage(o.Image); im != nil {
				return o.Name + " " + im.PortNameOf(on)
			}
		}
	}
	return ""
}

// EmuCaptureStop stops a capture (its file stays on the host).
func (a *App) EmuCaptureStop(key string) ([]EmuCapture, error) {
	emuLab.mu.Lock()
	var c *EmuCapture
	for i := range emuLab.f.Captures {
		if emuLab.f.Captures[i].Key == key && emuLab.f.Captures[i].Running {
			c = &emuLab.f.Captures[i]
		}
	}
	if c == nil {
		emuLab.mu.Unlock()
		return a.EmuCaptures(), nil
	}
	cc := *c
	l := emuLab.f.Lab
	var mon, n int
	if l != nil {
		if vm := l.VMByID(cc.VM); vm != nil {
			mon = a.emuFor(cc.Host).MonitorPort(*vm)
			n = emuPort(emuImage(vm.Image), cc.Port)
		}
	}
	emuLab.mu.Unlock()
	if mon > 0 && n > 0 {
		if h, err := a.emuMonitor(cc.Host, mon); err == nil { // a stopped switch has no capture left anyway
			_, _ = h.do("object_del "+emuCapID(n), 10*time.Second)
			h.close()
		}
	}
	emuLab.mu.Lock()
	for i := range emuLab.f.Captures {
		if emuLab.f.Captures[i].Key == key && emuLab.f.Captures[i].Running {
			emuLab.f.Captures[i].Running = false
		}
	}
	_ = a.emuSave()
	emuEv(cc.VM, "info", "capture of "+cc.Port+" stopped ("+cc.File+")")
	emuLab.mu.Unlock()
	return a.EmuCaptures(), nil
}

// EmuCaptures lists the captures, newest first, with their sizes now.
func (a *App) EmuCaptures() []EmuCapture {
	emuLab.mu.Lock()
	a.emuLoad()
	cs := append([]EmuCapture(nil), emuLab.f.Captures...)
	emuLab.mu.Unlock()
	byHost := map[string][]int{}
	for i, c := range cs {
		byHost[c.Host] = append(byHost[c.Host], i)
	}
	for h, idx := range byHost {
		var q []string
		for _, i := range idx {
			q = append(q, "\"$HOME\"/"+cs[i].File)
		}
		out, err := a.emuRun(h, "for f in "+strings.Join(q, " ")+"; do s=$(stat -c %s \"$f\" 2>/dev/null || echo -1); echo \"$s\"; done", 20*time.Second)
		if err != nil {
			continue
		}
		for k, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if k < len(idx) {
				fmt.Sscan(line, &cs[idx[k]].Size)
			}
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Started > cs[j].Started })
	if cs == nil {
		cs = []EmuCapture{}
	}
	return cs
}

// emuCapture finds a capture by key (the newest).
func emuCaptureByKey(key string) (EmuCapture, bool) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	var best EmuCapture
	ok := false
	for _, c := range emuLab.f.Captures {
		if (c.Key == key || c.File == key) && (!ok || c.Started > best.Started) {
			best, ok = c, true
		}
	}
	return best, ok
}

// emuCaptureRead reads a capture's pcap file from its host (up to max bytes: the start, and the newest packets when it is bigger).
func (a *App) emuCaptureRead(c EmuCapture, max int64) ([]byte, error) {
	cl, err := a.emuClient(context.Background(), c.Host)
	if err != nil {
		return nil, err
	}
	sc, err := sftp.NewClient(cl)
	if err != nil {
		return nil, err
	}
	defer sc.Close()
	f, err := sc.Open(c.File)
	if err != nil {
		return nil, fmt.Errorf("the capture file on the host: %w", err)
	}
	defer f.Close()
	if max <= 0 {
		return io.ReadAll(f)
	}
	return io.ReadAll(io.LimitReader(f, max))
}

// EmuCapturePackets decodes the packets of a capture (the last ones, at most last).
func (a *App) EmuCapturePackets(key string, last int) ([]EmuPacket, error) {
	c, ok := emuCaptureByKey(key)
	if !ok {
		return nil, errors.New("no such capture")
	}
	b, err := a.emuCaptureRead(c, 32<<20)
	if err != nil {
		return nil, err
	}
	ps := emuDecodePcap(b)
	if last > 0 && len(ps) > last {
		ps = ps[len(ps)-last:]
	}
	if ps == nil {
		ps = []EmuPacket{}
	}
	return ps, nil
}

// EmuCaptureSave copies a capture's pcap file to this computer.
func (a *App) EmuCaptureSave(key, local string) error {
	c, ok := emuCaptureByKey(key)
	if !ok {
		return errors.New("no such capture")
	}
	b, err := a.emuCaptureRead(c, 0)
	if err != nil {
		return err
	}
	return os.WriteFile(local, b, 0o600)
}

// emuWireshark finds Wireshark on this computer.
func emuWireshark() string {
	for _, p := range []string{"/Applications/Wireshark.app/Contents/MacOS/Wireshark", `C:\Program Files\Wireshark\Wireshark.exe`, "/usr/bin/wireshark", "/usr/local/bin/wireshark", "/opt/homebrew/bin/wireshark"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("wireshark"); err == nil {
		return p
	}
	return ""
}

// EmuCaptureWireshark opens a capture in Wireshark on this computer: live (the file followed as it grows) while it runs.
func (a *App) EmuCaptureWireshark(key string) error {
	c, ok := emuCaptureByKey(key)
	if !ok {
		return errors.New("no such capture")
	}
	ws := emuWireshark()
	if ws == "" {
		hint := "install it from wireshark.org"
		if runtime.GOOS == "darwin" {
			hint = "install it from wireshark.org (or brew install --cask wireshark)"
		}
		return errors.New("Wireshark is not on this computer: " + hint + ", or use Save .pcap…")
	}
	cl, err := a.emuClient(context.Background(), c.Host)
	if err != nil {
		return err
	}
	s, err := cl.NewSession()
	if err != nil {
		return err
	}
	out, err := s.StdoutPipe()
	if err != nil {
		s.Close()
		return err
	}
	follow := "-f"
	if !c.Running {
		follow = ""
	}
	if err := s.Start(fmt.Sprintf("tail -c +1 %s \"$HOME\"/%s", follow, c.File)); err != nil {
		s.Close()
		return err
	}
	cmd := exec.Command(ws, "-k", "-i", "-")
	cmd.Stdin = out
	if err := cmd.Start(); err != nil {
		s.Close()
		return fmt.Errorf("starting Wireshark: %w", err)
	}
	go func() { _ = cmd.Wait(); s.Close() }() // closing Wireshark ends the stream
	return nil
}

// emuDecodePcap decodes a pcap file enough for a packet list (Ethernet, VLAN, ARP, IPv4/6, TCP, UDP, ICMP, LLDP, STP, LACP).
func emuDecodePcap(b []byte) []EmuPacket {
	if len(b) < 24 {
		return nil
	}
	var bo binary.ByteOrder = binary.LittleEndian
	switch binary.LittleEndian.Uint32(b) {
	case 0xa1b2c3d4, 0xa1b23c4d:
	case 0xd4c3b2a1, 0x4d3cb2a1:
		bo = binary.BigEndian
	default:
		return nil
	}
	nano := binary.LittleEndian.Uint32(b) == 0xa1b23c4d || binary.BigEndian.Uint32(b) == 0xa1b23c4d
	var out []EmuPacket
	var t0 float64
	for off := 24; off+16 <= len(b); {
		ts := float64(bo.Uint32(b[off:])) + float64(bo.Uint32(b[off+4:]))/map[bool]float64{true: 1e9, false: 1e6}[nano]
		incl, orig := int(bo.Uint32(b[off+8:])), int(bo.Uint32(b[off+12:]))
		off += 16
		if incl < 0 || off+incl > len(b) {
			break
		}
		p := emuDecodeFrame(b[off : off+incl])
		off += incl
		if len(out) == 0 {
			t0 = ts
		}
		p.No, p.Time, p.Len = len(out)+1, ts-t0, orig
		out = append(out, p)
	}
	return out
}

func emuMAC(b []byte) string { return net.HardwareAddr(b).String() }

func emuDecodeFrame(f []byte) EmuPacket {
	p := EmuPacket{Proto: "Ethernet"}
	if len(f) < 14 {
		p.Info = "short frame"
		return p
	}
	p.Dst, p.Src = emuMAC(f[0:6]), emuMAC(f[6:12])
	et := binary.BigEndian.Uint16(f[12:])
	pl := f[14:]
	vlan := ""
	for (et == 0x8100 || et == 0x88a8) && len(pl) >= 4 {
		vlan += fmt.Sprintf("VLAN %d ", binary.BigEndian.Uint16(pl)&0x0fff)
		et, pl = binary.BigEndian.Uint16(pl[2:]), pl[4:]
	}
	defer func() { p.Info = strings.TrimSpace(vlan + p.Info) }()
	switch {
	case et < 0x0600: // 802.3 length: LLC (STP and friends)
		if strings.HasPrefix(p.Dst, "01:80:c2:00:00:00") {
			p.Proto, p.Info = "STP", "spanning tree BPDU"
		} else {
			p.Proto, p.Info = "LLC", fmt.Sprintf("802.3 length %d", et)
		}
	case et == 0x0806 && len(pl) >= 28:
		op, sip, tip := binary.BigEndian.Uint16(pl[6:]), net.IP(pl[14:18]), net.IP(pl[24:28])
		p.Proto = "ARP"
		if op == 1 {
			p.Info = fmt.Sprintf("Who has %s? Tell %s", tip, sip)
		} else {
			p.Info = fmt.Sprintf("%s is at %s", sip, emuMAC(pl[8:14]))
		}
	case et == 0x88cc:
		p.Proto, p.Info = "LLDP", emuLLDP(pl)
	case et == 0x8809:
		p.Proto, p.Info = "LACP", "link aggregation"
		if len(pl) > 0 && pl[0] == 2 {
			p.Proto = "Marker"
		}
	case et == 0x0800 && len(pl) >= 20:
		ihl := int(pl[0]&0x0f) * 4
		p.Src, p.Dst = net.IP(pl[12:16]).String(), net.IP(pl[16:20]).String()
		emuL4(&p, pl[9], pl[min(ihl, len(pl)):])
	case et == 0x86dd && len(pl) >= 40:
		p.Src, p.Dst = net.IP(pl[8:24]).String(), net.IP(pl[24:40]).String()
		emuL4(&p, pl[6], pl[40:])
		if p.Proto == "ICMP" {
			p.Proto = "ICMPv6"
		}
	default:
		p.Info = fmt.Sprintf("ethertype 0x%04x", et)
	}
	return p
}

func emuL4(p *EmuPacket, proto byte, b []byte) {
	switch proto {
	case 6:
		p.Proto = "TCP"
		if len(b) >= 14 {
			sp, dp, fl := binary.BigEndian.Uint16(b), binary.BigEndian.Uint16(b[2:]), b[13]
			var f []string
			for i, n := range []string{"FIN", "SYN", "RST", "PSH", "ACK"} {
				if fl&(1<<i) != 0 {
					f = append(f, n)
				}
			}
			p.Info = fmt.Sprintf("%d → %d [%s]", sp, dp, strings.Join(f, ","))
			if sp == 22 || dp == 22 {
				p.Proto = "SSH"
			} else if sp == 179 || dp == 179 {
				p.Proto = "BGP"
			}
		}
	case 17:
		p.Proto = "UDP"
		if len(b) >= 8 {
			sp, dp := binary.BigEndian.Uint16(b), binary.BigEndian.Uint16(b[2:])
			p.Info = fmt.Sprintf("%d → %d", sp, dp)
			switch {
			case sp == 67 || sp == 68 || dp == 67 || dp == 68:
				p.Proto = "DHCP"
			case sp == 53 || dp == 53:
				p.Proto = "DNS"
			case sp == 123 || dp == 123:
				p.Proto = "NTP"
			case sp == 161 || dp == 161 || dp == 162:
				p.Proto = "SNMP"
			case dp == 4789:
				p.Proto = "VXLAN"
			}
		}
	case 1, 58:
		p.Proto = "ICMP"
		if len(b) >= 2 {
			switch {
			case proto == 1 && b[0] == 8, proto == 58 && b[0] == 128:
				p.Info = "echo request"
			case proto == 1 && b[0] == 0, proto == 58 && b[0] == 129:
				p.Info = "echo reply"
			case proto == 58 && (b[0] == 135 || b[0] == 136):
				p.Info = "neighbor discovery"
			default:
				p.Info = fmt.Sprintf("type %d code %d", b[0], b[1])
			}
		}
	case 89:
		p.Proto, p.Info = "OSPF", "routing"
	case 2:
		p.Proto, p.Info = "IGMP", "group membership"
	case 112:
		p.Proto, p.Info = "VRRP", "virtual router"
	default:
		p.Proto, p.Info = "IP", fmt.Sprintf("protocol %d", proto)
	}
}

// emuLLDP names the sender of an LLDP frame: its system name and port.
func emuLLDP(b []byte) string {
	var sys, port string
	for len(b) >= 2 {
		h := binary.BigEndian.Uint16(b)
		t, n := h>>9, int(h&0x1ff)
		b = b[2:]
		if t == 0 || n > len(b) {
			break
		}
		v := b[:n]
		switch t {
		case 2:
			if len(v) > 1 {
				port = string(v[1:])
			}
		case 4:
			if port == "" {
				port = string(v)
			}
		case 5:
			sys = string(v)
		}
		b = b[n:]
	}
	if sys == "" && port == "" {
		return "neighbour announcement"
	}
	return strings.TrimSpace("from " + sys + " port " + port)
}
