package main

// The traffic report to share and keep: HTML (printed to PDF by a Chromium browser) and an Excel workbook, with the
// run's graphs, the streams on the lab's topology, what each stream sent and what arrived.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/xlsx"
)

// emuRep is what a traffic report shows.
type emuRep struct {
	Lab     string
	At      time.Time
	Secs    float64 // how long the run went (from its graphs)
	V       EmuTrafficView
	Tester  map[string]emu.Tester
	Res     map[string]EmuStreamResult
	Port    map[string]EmuPortResult
	VMs     []emu.VM // the devices drawn
	Cables  []emu.Cable
	VMName  map[string]string
	Color   map[string]string // stream → its colour in the graphs and the topology
	fan     map[string]int    // stream → which of the streams between its two test ports it is (0, 1…), to draw them apart
	Num     map[string]int    // stream → its number (1, 2…)
	Passed  int
	Failed  int
	Sent    int
	Got     int
	Expect  int     // frames that should have arrived, of the streams whose frames should arrive
	Lost    int     // of those, how many did not
	LatAvg  float64 // ms, over the streams that measured it (-1: none)
	LatMax  float64
	Version string
}

func (a *App) emuReportData() (emuRep, error) {
	v, err := a.EmuTraffic()
	if err != nil {
		return emuRep{}, err
	}
	r := emuRep{At: time.Now(), V: v, Tester: map[string]emu.Tester{}, Res: map[string]EmuStreamResult{}, Port: map[string]EmuPortResult{},
		VMName: map[string]string{}, Color: map[string]string{}, Num: map[string]int{}, LatAvg: -1, Version: Version}
	emuLab.mu.Lock()
	r.Lab = orStr(emuLab.f.Topology, "Untitled")
	if l := emuLab.f.Lab; l != nil {
		r.VMs = append(r.VMs, l.VMs...)
		r.Cables = append(r.Cables, l.Cables...)
	}
	emuLab.mu.Unlock()
	for _, vm := range r.VMs {
		r.VMName[vm.ID] = vm.Name
	}
	for _, t := range v.Testers {
		r.Tester[t.ID] = t
	}
	for _, p := range v.Ports {
		r.Port[p.ID] = p
	}
	for _, x := range v.Results {
		r.Res[x.ID] = x
	}
	var latSum float64
	var latN int
	for i, s := range v.Streams {
		r.Color[s.ID], r.Num[s.ID] = emuReportPalette[i%len(emuReportPalette)], i+1
		x := r.Res[s.ID]
		r.Passed += map[bool]int{true: 1}[x.Result == "pass"]
		r.Failed += map[bool]int{true: 1}[x.Result == "fail"]
		r.Sent += x.TX
		r.Got += x.RX
		if s.Expect == "" {
			r.Expect += x.Expected
			r.Lost += max(0, x.Expected-x.RX)
		}
		if x.LatAvg >= 0 && x.RX > 0 {
			latSum += x.LatAvg * float64(x.RX)
			latN += x.RX
			r.LatMax = math.Max(r.LatMax, x.LatMax)
		}
		for _, p := range v.History[s.ID] {
			r.Secs = math.Max(r.Secs, p[0])
		}
	}
	if latN > 0 {
		r.LatAvg = latSum / float64(latN)
	}
	return r, nil
}

// testerAt is where a test port is: "sw1 port 3".
func (r emuRep) testerAt(id string) string {
	t, ok := r.Tester[id]
	if !ok {
		return ""
	}
	at := orStr(r.VMName[t.VM], t.VM)
	if t.PortName != "" {
		return at + " " + t.PortName
	}
	return fmt.Sprintf("%s port %d", at, t.Port)
}

func (r emuRep) testerName(id string) string { return orStr(r.Tester[id].Name, id) }

// to is where a stream goes: a test port, an address, or every other test port.
func (r emuRep) to(s emu.Stream) string {
	switch {
	case s.To != "":
		return r.testerName(s.To)
	case s.DstIP != "":
		return s.DstIP
	}
	return "every other test port"
}

// emuTesterProto is what a test port speaks besides its streams, and how it went: clients, VRRP, BFD.
func emuTesterProto(t emu.Tester) string {
	var p []string
	if c := t.Clients; c != nil && c.N > 0 {
		x := fmt.Sprintf("%d clients", c.N)
		if cs := t.ClientState; cs != nil {
			if c.Dot1x != nil {
				x += fmt.Sprintf(", %d in by 802.1X (%s)", cs.Auth, map[bool]string{true: "PEAP-MSCHAPv2", false: "EAP-MD5"}[c.Dot1x.Method == "peap"])
			}
			if c.DHCP {
				x += fmt.Sprintf(", %d with DHCP", cs.Bound)
			}
			if c.DHCP6 {
				x += fmt.Sprintf(", %d with DHCPv6", cs.Bound6)
			}
		}
		p = append(p, x)
	}
	if v := t.VRRP; v != nil {
		x := fmt.Sprintf("VRRPv%d %s", v.Ver, v.VIP)
		if st := t.VRRPState; st != nil {
			x += ": " + map[bool]string{true: "master", false: "backup"}[st.Us]
			if st.Takeover > 0 {
				x += fmt.Sprintf(", the switch took over in %d ms", st.Takeover)
			}
		}
		p = append(p, x)
	}
	if b := t.BFD; b != nil {
		x := "BFD with " + b.Peer
		if st := t.BFDState; st != nil {
			x += ": " + st.State
			if st.Measured > 0 {
				x += fmt.Sprintf(", the switch noticed silence in %d ms", st.Measured)
			}
		}
		p = append(p, x)
	}
	return strings.Join(p, "; ")
}

// emuDur writes seconds as 45 s, 2 min 05 s, 1 h 02 min.
func emuDur(s float64) string {
	n := int(math.Round(s))
	switch {
	case n < 60:
		return fmt.Sprintf("%d s", n)
	case n < 3600:
		return fmt.Sprintf("%d min %02d s", n/60, n%60)
	}
	return fmt.Sprintf("%d h %02d min", n/3600, n%3600/60)
}

func emuThousands(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return "-" + emuThousands(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// emuFrameText is what a stream's frames are, in a few words: UDP · 128 B · VLAN 10 · prio 5 · DSCP 46.
func emuFrameText(s emu.Stream) string {
	kind := map[string]string{"udp": "UDP", "tcp": "TCP SYN", "icmp": "ICMP echo", "arp": "ARP", "raw": "Raw L2", "pcap": "pcap replay", "custom": "Custom"}[s.Kind]
	size := fmt.Sprintf("%d B", s.Size)
	switch s.SizeMode {
	case "imix":
		size = "IMIX"
	case "inc", "random":
		size = fmt.Sprintf("%d-%d B %s", s.Size, s.SizeMax, map[string]string{"inc": "rising", "random": "random"}[s.SizeMode])
	}
	if s.Kind == "pcap" || s.Kind == "custom" && s.Size == 0 {
		size = ""
	}
	p := []string{orStr(kind, s.Kind), size}
	if s.VLAN > 0 {
		p = append(p, fmt.Sprintf("VLAN %d", s.VLAN))
	}
	if s.Prio > 0 {
		p = append(p, fmt.Sprintf("prio %d", s.Prio))
	}
	if s.DSCP > 0 {
		p = append(p, fmt.Sprintf("DSCP %d", s.DSCP))
	}
	if s.Bad != "" {
		if b := emu.BadFrame(s.Bad); b != nil {
			p = append(p, b.Name)
		}
	}
	if s.Fuzz != nil {
		p = append(p, fmt.Sprintf("fuzzed %d-%s", s.Fuzz.From, map[bool]string{true: "end", false: fmt.Sprint(s.Fuzz.To)}[s.Fuzz.To == 0]))
	}
	if e := s.Encap; e != nil {
		if e.Outer != nil {
			p = append(p, fmt.Sprintf("outer tag %d", e.Outer.VLAN))
		}
		if len(e.MPLS) > 0 {
			var l []string
			for _, m := range e.MPLS {
				l = append(l, fmt.Sprint(m.Label))
			}
			p = append(p, "MPLS "+strings.Join(l, "/"))
		}
		if e.GRE != nil {
			p = append(p, "GRE to "+e.GRE.Dst)
		}
		if e.VXLAN != nil {
			p = append(p, fmt.Sprintf("VXLAN %d to %s", e.VXLAN.VNI, e.VXLAN.Dst))
		}
		if n := len(e.V6Ext); n > 0 {
			p = append(p, fmt.Sprintf("%d IPv6 extension header%s", n, map[bool]string{true: "s"}[n > 1]))
		}
	}
	if n := len(s.UDFs) + len(s.Mods); n > 0 {
		p = append(p, fmt.Sprintf("%d changing field%s", n, map[bool]string{true: "s"}[n > 1]))
	}
	var out []string
	for _, x := range p {
		if x != "" {
			out = append(out, x)
		}
	}
	return strings.Join(out, " · ")
}

// emuRateText is a stream's rate as it was given, and for how long.
func emuRateText(s emu.Stream) (rate, dur string) {
	rate = emuThousands(s.PPS) + " fps"
	switch s.RateUnit {
	case "mbps":
		rate = fmt.Sprintf("%g Mbit/s (%s fps)", s.Rate, emuThousands(s.PPS))
	case "pct":
		rate = fmt.Sprintf("%g %% of line (%s fps)", s.Rate, emuThousands(s.PPS))
	}
	switch {
	case s.Count > 0:
		dur = emuThousands(s.Count) + " frames"
	case s.Seconds > 0:
		dur = emuDur(float64(s.Seconds))
	default:
		dur = "until stopped"
	}
	return
}

// emuPassText is what makes a stream pass.
func emuPassText(s emu.Stream) string {
	switch s.Expect {
	case "drop":
		return "none of its frames arrives (they should be dropped)"
	case "any":
		return "always: its frames are only counted"
	}
	p := []string{fmt.Sprintf("loss at most %g %%", s.MaxLoss)}
	if s.MaxLatency > 0 {
		p = append(p, fmt.Sprintf("latency at most %g ms", s.MaxLatency))
	}
	if s.MaxJitter > 0 {
		p = append(p, fmt.Sprintf("jitter at most %g ms", s.MaxJitter))
	}
	return strings.Join(p, ", ")
}

// emuFld is one field of a decoded frame.
type emuFld struct{ Layer, Name, Val string }

var emuEtherTypes = map[uint16]string{0x0800: "IPv4", 0x86DD: "IPv6", 0x0806: "ARP", 0x8100: "802.1Q", 0x88A8: "802.1ad", 0x88B5: "local experimental", 0x88CC: "LLDP", 0x888E: "EAPOL", 0x8809: "slow protocols", 0x8847: "MPLS", 0x9100: "VLAN (0x9100)"}

// emuIPSum is the IPv4 header checksum: 0 when the header's own is right.
func emuIPSum(h []byte) uint16 {
	var s uint32
	for i := 0; i+1 < len(h); i += 2 {
		s += uint32(h[i])<<8 | uint32(h[i+1])
	}
	for s>>16 != 0 {
		s = s&0xFFFF + s>>16
	}
	return ^uint16(s)
}

// emuFrameFields names the fields of an Ethernet frame: its MACs, VLAN tags, MPLS labels, IPv4/IPv6 with extension
// headers, GRE and VXLAN tunnels (the frame inside decoded as Inner …), UDP/TCP/ICMP or ARP, and Termwright's
// counting signature.
func emuFrameFields(f []byte) []emuFld {
	var out []emuFld
	add := func(l, n, v string) { out = append(out, emuFld{l, n, v}) }
	emuDecodeEth(f, "", add)
	if i := bytes.Index(f, []byte("TWTG")); i >= 0 && len(f) >= i+24 {
		g := f[i+4:]
		add("Termwright", "Signature", fmt.Sprintf("at byte %d: stream %d, frame %d", i, binary.BigEndian.Uint16(g)+1, binary.BigEndian.Uint32(g[6:])))
	}
	return out
}

func emuDecodeEth(f []byte, pfx string, add func(l, n, v string)) {
	if len(f) < 14 {
		add(pfx+"Ethernet", "Frame", fmt.Sprintf("%d bytes (shorter than an Ethernet header)", len(f)))
		return
	}
	add(pfx+"Ethernet", "Destination MAC", emuMAC(f[0:6]))
	add(pfx+"Ethernet", "Source MAC", emuMAC(f[6:12]))
	o, tag := 12, 0
	et := binary.BigEndian.Uint16(f[o:])
	for (et == 0x8100 || et == 0x88A8 || et == 0x9100) && len(f) >= o+6 {
		tag++
		tci := binary.BigEndian.Uint16(f[o+2:])
		l := pfx + "VLAN tag"
		if tag > 1 {
			l = fmt.Sprintf("%sVLAN tag %d", pfx, tag)
		}
		if et != 0x8100 {
			add(l, "TPID", fmt.Sprintf("0x%04X", et))
		}
		add(l, "VLAN ID", fmt.Sprint(tci&0xFFF))
		add(l, "Priority (802.1p)", fmt.Sprint(tci>>13))
		o += 4
		et = binary.BigEndian.Uint16(f[o:])
	}
	if tag == 0 {
		add(pfx+"VLAN tag", "VLAN ID", "untagged")
	}
	add(pfx+"Ethernet", "EtherType", fmt.Sprintf("0x%04X %s", et, emuEtherTypes[et]))
	b := f[o+2:]
	if et == 0x8847 { // MPLS: the label stack, then IPv4 or IPv6 by its first nibble
		for n := 1; len(b) >= 4; n++ {
			e := binary.BigEndian.Uint32(b)
			add(fmt.Sprintf("%sMPLS %d", pfx, n), "Label, TC, TTL", fmt.Sprintf("%d, %d, %d%s", e>>12, e>>9&7, e&255, map[bool]string{true: " (bottom)"}[e&0x100 != 0]))
			b = b[4:]
			if e&0x100 != 0 {
				break
			}
		}
		et = map[bool]uint16{true: 0x0800, false: 0x86DD}[len(b) > 0 && b[0]>>4 == 4]
	}
	emuDecodeIP(b, et, pfx, add)
}

func emuDecodeIP(b []byte, et uint16, pfx string, add func(l, n, v string)) {
	l4, proto := []byte(nil), -1
	switch {
	case et == 0x0800 && len(b) >= 20:
		ihl := int(b[0]&15) * 4
		L := pfx + "IPv4"
		add(L, "Version", fmt.Sprint(b[0]>>4))
		add(L, "Header length", fmt.Sprintf("%d bytes", ihl))
		add(L, "DSCP", fmt.Sprint(b[1]>>2))
		add(L, "Total length", fmt.Sprint(binary.BigEndian.Uint16(b[2:])))
		ff := binary.BigEndian.Uint16(b[6:])
		frag := "none"
		if ff&0x3FFF != 0 {
			frag = fmt.Sprintf("offset %d%s", (ff&0x1FFF)*8, map[bool]string{true: ", more fragments"}[ff&0x2000 != 0])
		}
		add(L, "Fragment", frag+map[bool]string{true: ", don't fragment"}[ff&0x4000 != 0])
		add(L, "TTL", fmt.Sprint(b[8]))
		proto = int(b[9])
		add(L, "Protocol", fmt.Sprintf("%d %s", b[9], map[int]string{1: "ICMP", 6: "TCP", 17: "UDP", 47: "GRE", 112: "VRRP"}[proto]))
		ok := "wrong"
		if ihl >= 20 && len(b) >= ihl && emuIPSum(b[:ihl]) == 0 {
			ok = "right"
		}
		add(L, "Header checksum", fmt.Sprintf("0x%04X (%s)", binary.BigEndian.Uint16(b[10:]), ok))
		add(L, "Source", net.IP(b[12:16]).String())
		add(L, "Destination", net.IP(b[16:20]).String())
		if ihl > 20 {
			add(L, "Options", fmt.Sprintf("%d bytes", ihl-20))
		}
		if ihl >= 20 && len(b) > ihl && ff&0x1FFF == 0 {
			l4 = b[ihl:]
		}
	case et == 0x86DD && len(b) >= 40:
		L := pfx + "IPv6"
		add(L, "Traffic class (DSCP)", fmt.Sprint(((b[0]&15)<<4|b[1]>>4)>>2))
		add(L, "Payload length", fmt.Sprint(binary.BigEndian.Uint16(b[4:])))
		proto = int(b[6])
		add(L, "Hop limit", fmt.Sprint(b[7]))
		add(L, "Source", net.IP(b[8:24]).String())
		add(L, "Destination", net.IP(b[24:40]).String())
		l4 = b[40:]
		for n := 0; n < 8 && len(l4) >= 8 && (proto == 0 || proto == 60 || proto == 43 || proto == 44); n++ { // extension headers
			name := map[int]string{0: "Hop-by-hop options", 60: "Destination options", 43: "Routing", 44: "Fragment"}[proto]
			size := 8
			v := fmt.Sprintf("%d bytes", 8+int(l4[1])*8)
			switch proto {
			case 44:
				fo := binary.BigEndian.Uint16(l4[2:])
				v = fmt.Sprintf("offset %d%s, ID 0x%08X", (fo>>3)*8, map[bool]string{true: ", more fragments"}[fo&1 != 0], binary.BigEndian.Uint32(l4[4:]))
			case 43:
				size = 8 + int(l4[1])*8
				v = fmt.Sprintf("type %d, %d segments left", l4[2], l4[3])
				if l4[2] == 4 && len(l4) >= size {
					var sg []string
					for i := 8; i+16 <= size; i += 16 {
						sg = append(sg, net.IP(l4[i:i+16]).String())
					}
					v += ": " + strings.Join(sg, ", ")
				}
			default:
				size = 8 + int(l4[1])*8
			}
			add(L, name, v)
			proto = int(l4[0])
			if len(l4) < size {
				l4 = nil
				break
			}
			l4 = l4[size:]
		}
		add(L, "Next header", fmt.Sprintf("%d %s", proto, map[int]string{58: "ICMPv6", 6: "TCP", 17: "UDP", 47: "GRE", 112: "VRRP"}[proto]))
	case et == 0x0806 && len(b) >= 28:
		add(pfx+"ARP", "Operation", map[uint16]string{1: "request", 2: "reply"}[binary.BigEndian.Uint16(b[6:])])
		add(pfx+"ARP", "Sender", emuMAC(b[8:14])+" "+net.IP(b[14:18]).String())
		add(pfx+"ARP", "Target", emuMAC(b[18:24])+" "+net.IP(b[24:28]).String())
	}
	switch {
	case proto == 17 && len(l4) >= 8:
		dp := binary.BigEndian.Uint16(l4[2:])
		add(pfx+"UDP", "Ports", fmt.Sprintf("%d → %d", binary.BigEndian.Uint16(l4), dp))
		add(pfx+"UDP", "Length", fmt.Sprint(binary.BigEndian.Uint16(l4[4:])))
		add(pfx+"UDP", "Checksum", fmt.Sprintf("0x%04X", binary.BigEndian.Uint16(l4[6:])))
		if dp == 4789 && len(l4) >= 16 && pfx == "" { // VXLAN: the frame inside
			add("VXLAN", "VNI", fmt.Sprint(binary.BigEndian.Uint32(l4[12:])>>8))
			emuDecodeEth(l4[16:], "Inner ", add)
		}
	case proto == 6 && len(l4) >= 20:
		add(pfx+"TCP", "Ports", fmt.Sprintf("%d → %d", binary.BigEndian.Uint16(l4), binary.BigEndian.Uint16(l4[2:])))
		var fl []string
		for i, n := range []string{"FIN", "SYN", "RST", "PSH", "ACK", "URG"} {
			if l4[13]&(1<<i) != 0 {
				fl = append(fl, n)
			}
		}
		add(pfx+"TCP", "Flags", orStr(strings.Join(fl, " "), "none"))
		add(pfx+"TCP", "Checksum", fmt.Sprintf("0x%04X", binary.BigEndian.Uint16(l4[16:])))
	case (proto == 1 || proto == 58) && len(l4) >= 4:
		add(pfx+"ICMP", "Type, code", fmt.Sprintf("%d, %d", l4[0], l4[1]))
	case proto == 47 && len(l4) >= 4 && pfx == "": // GRE: the packet inside
		fl, pt := binary.BigEndian.Uint16(l4), binary.BigEndian.Uint16(l4[2:])
		n := 4
		if fl&0x8000 != 0 {
			n += 4
		}
		v := fmt.Sprintf("protocol 0x%04X", pt)
		if fl&0x2000 != 0 && len(l4) >= n+4 {
			v += fmt.Sprintf(", key %d", binary.BigEndian.Uint32(l4[n:]))
			n += 4
		}
		add("GRE", "Header", v)
		if len(l4) > n {
			emuDecodeIP(l4[n:], pt, "Inner ", add)
		}
	}
}

// emuHexDump is a frame as a hex dump: offset, 16 bytes, their text.
func emuHexDump(f []byte) string {
	var b strings.Builder
	for i := 0; i < len(f); i += 16 {
		row := f[i:min(i+16, len(f))]
		fmt.Fprintf(&b, "%04x  ", i)
		for j := range 16 {
			if j < len(row) {
				fmt.Fprintf(&b, "%02x ", row[j])
			} else {
				b.WriteString("   ")
			}
			if j == 7 {
				b.WriteByte(' ')
			}
		}
		b.WriteByte(' ')
		for _, c := range row {
			if c < 32 || c > 126 {
				c = '.'
			}
			b.WriteByte(c)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// emuFrameRows lines up the fields of the frame sent and the one received: [layer, field, sent, received, changed].
func emuFrameRows(sent, got []byte) [][5]string {
	a, b := emuFrameFields(sent), []emuFld(nil)
	if got != nil {
		b = emuFrameFields(got)
	}
	gv := map[string]string{}
	for _, f := range b {
		gv[f.Layer+"/"+f.Name] = f.Val
	}
	seen := map[string]bool{}
	var out [][5]string
	for _, f := range a {
		k := f.Layer + "/" + f.Name
		seen[k] = true
		g, ok := gv[k]
		if got == nil {
			g = ""
		} else if !ok {
			g = "-"
		}
		out = append(out, [5]string{f.Layer, f.Name, f.Val, g, map[bool]string{true: "changed"}[got != nil && g != f.Val && f.Layer != "Termwright"]})
	}
	for _, f := range b {
		if k := f.Layer + "/" + f.Name; !seen[k] {
			out = append(out, [5]string{f.Layer, f.Name, "-", f.Val, "changed"})
		}
	}
	return out
}

// emuHexBytes reads a frame the generator kept: "508:0004…" (its length, then its first bytes), or hex alone.
func emuHexBytes(s string) (b []byte, n int) {
	if i := strings.IndexByte(s, ':'); i > 0 {
		fmt.Sscan(s[:i], &n)
		s = s[i+1:]
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) == 0 {
		return nil, 0
	}
	return b, max(n, len(b))
}

// emuTopoSVG draws the lab's devices and cables, the test ports on their switch ports, and each stream as a numbered
// arrow from its test port to the one it goes to, in the stream's colour.
func emuTopoSVG(r emuRep) string {
	used := map[string]bool{}
	for _, t := range r.V.Testers {
		used[t.VM] = true
	}
	var vms []emu.VM
	for _, vm := range r.VMs {
		if len(r.VMs) <= 16 || used[vm.ID] {
			vms = append(vms, vm)
		}
	}
	if len(vms) < 16 { // the switches between them too
		in := map[string]bool{}
		for _, vm := range vms {
			in[vm.ID] = true
		}
		for _, c := range r.Cables {
			for _, e := range [][2]string{{c.A, c.B}, {c.B, c.A}} {
				if in[e[0]] && !in[e[1]] && used[e[0]] {
					for _, vm := range r.VMs {
						if vm.ID == e[1] && len(vms) < 16 {
							vms, in[vm.ID] = append(vms, vm), true
						}
					}
				}
			}
		}
	}
	if len(vms) == 0 {
		return ""
	}
	type pt struct{ x, y float64 }
	pos := map[string]pt{}
	for i, vm := range vms {
		p := pt{float64(vm.X), float64(vm.Y)}
		if vm.X == 0 && vm.Y == 0 {
			p = pt{float64(i%4) * 260, float64(i/4) * 200}
		}
		pos[vm.ID] = p
	}
	if r.fan == nil {
		r.fan = map[string]int{}
	}
	tpos := map[string]pt{}
	per := map[string]int{}
	for _, t := range r.V.Testers {
		sw, ok := pos[t.VM]
		if !ok {
			continue
		}
		if t.X != 0 || t.Y != 0 {
			tpos[t.ID] = pt{t.X, t.Y}
		} else {
			n := per[t.VM]
			per[t.VM]++
			tpos[t.ID] = pt{sw.x - 90 + float64(n%3)*90, sw.y + 120 + float64(n/3)*44}
		}
	}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, m := range []map[string]pt{pos, tpos} {
		for _, p := range m {
			x0, y0, x1, y1 = math.Min(x0, p.x), math.Min(y0, p.y), math.Max(x1, p.x), math.Max(y1, p.y)
		}
	}
	// the streams between the same two test ports fan out, one below the other
	nth, deepest := map[string]int{}, 0
	for _, s := range r.V.Streams {
		k := min(s.From, s.To) + "|" + max(s.From, s.To)
		r.fan[s.ID] = nth[k]
		deepest = max(deepest, nth[k])
		nth[k]++
	}
	const W, padX, padY, maxH, pill, step = 1000.0, 90.0, 40.0, 420.0, 15.0, 24.0
	sc := 1.0
	if x1 > x0 {
		sc = math.Min(1.4, (W-2*padX)/(x1-x0))
	}
	if (y1-y0)*sc > maxH-2*padY {
		sc = (maxH - 2*padY) / (y1 - y0)
	}
	ox := (W - (x1-x0)*sc) / 2
	below := 36 + step*float64(deepest) + 22 // room for the streams under the test ports
	H := math.Max(160, (y1-y0)*sc+padY+pill+below)
	at := func(p pt) pt { return pt{ox + (p.x-x0)*sc, padY + (p.y-y0)*sc} }
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="topo" viewBox="0 0 %.0f %.0f" role="img" aria-label="Topology"><defs>`, W, H)
	for _, c := range r.Color {
		fmt.Fprintf(&b, `<marker id="a%s" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0L10 5L0 10z" fill="%s"/></marker>`, c[1:], c)
	}
	b.WriteString(`</defs>`)
	var labels strings.Builder // the cables' port numbers, over the boxes
	for _, c := range r.Cables {
		pa, oka := pos[c.A]
		pb, okb := pos[c.B]
		if !oka || !okb {
			continue
		}
		a, z := at(pa), at(pb)
		dash := map[bool]string{true: ` stroke-dasharray="5 4"`}[c.Down]
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#9aa3ad" stroke-width="2"%s/>`, a.x, a.y, z.x, z.y, dash)
		for _, e := range []struct {
			p, q pt
			n    int
		}{{a, z, c.APort}, {z, a, c.BPort}} {
			d := math.Hypot(e.q.x-e.p.x, e.q.y-e.p.y)
			if d < 150 {
				continue
			}
			ux, uy := (e.q.x-e.p.x)/d, (e.q.y-e.p.y)/d
			out := math.Min(60/math.Max(math.Abs(ux), 1e-9), 20/math.Max(math.Abs(uy), 1e-9)) + 14 // just outside the box
			fmt.Fprintf(&labels, `<text x="%.1f" y="%.1f" class="pn">%d</text>`, e.p.x+ux*out-uy*9, e.p.y+uy*out+ux*9*-1+3, e.n)
		}
	}
	for id, tp := range tpos {
		sw, p := at(pos[r.Tester[id].VM]), at(tp)
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#c0c6cc" stroke-width="1.5"/>`, p.x, p.y, sw.x, sw.y)
	}
	for _, vm := range vms {
		p := at(pos[vm.ID])
		fmt.Fprintf(&b, `<g><rect x="%.1f" y="%.1f" width="120" height="40" rx="7" class="sw"/><text x="%.1f" y="%.1f" class="swn">%s</text></g>`, p.x-60, p.y-20, p.x, p.y+4, html.EscapeString(vm.Name))
	}
	b.WriteString(labels.String())
	// the streams: a curve under the test ports from one to the other (a short arrow out for a broadcast), its number at its lowest point
	for _, s := range r.V.Streams {
		a, ok := tpos[s.From]
		if !ok {
			continue
		}
		pa, c := at(a), r.Color[s.ID]
		pa.y += pill
		dash := map[bool]string{true: ` stroke-dasharray="6 4"`}[s.Expect == "drop"]
		depth := 36 + step*float64(r.fan[s.ID])
		z, ok := tpos[s.To]
		if !ok {
			fmt.Fprintf(&b, `<path d="M%.1f %.1f l40 %.1f" fill="none" stroke="%s" stroke-width="2.2"%s marker-end="url(#a%s)"/>`, pa.x, pa.y, depth, c, dash, c[1:])
			fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="9" fill="%s"/><text x="%.1f" y="%.1f" class="sn">%d</text>`, pa.x+52, pa.y+depth+6, c, pa.x+52, pa.y+depth+9.5, r.Num[s.ID])
			continue
		}
		pz := at(z)
		pz.y += pill
		// a quadratic curve's lowest point is half way to its control point
		cy := math.Max(pa.y, pz.y) + 2*depth
		fmt.Fprintf(&b, `<path d="M%.1f %.1f Q%.1f %.1f %.1f %.1f" fill="none" stroke="%s" stroke-width="2.2"%s marker-end="url(#a%s)"/>`, pa.x+8, pa.y, (pa.x+pz.x)/2, cy, pz.x-8, pz.y+2, c, dash, c[1:])
		lx, ly := (pa.x+pz.x)/2, (pa.y+2*cy+pz.y)/4
		fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="9" fill="%s" stroke="#fff" stroke-width="1.5"/><text x="%.1f" y="%.1f" class="sn">%d</text>`, lx, ly, c, lx, ly+3.5, r.Num[s.ID])
	}
	for id, tp := range tpos {
		p := at(tp)
		t := r.Tester[id]
		sub := fmt.Sprintf("port %d", t.Port)
		if ip := strings.SplitN(orStr(t.IP, t.Lease), " ", 2)[0]; ip != "" {
			sub += " · " + ip
		}
		fmt.Fprintf(&b, `<g><rect x="%.1f" y="%.1f" width="128" height="30" rx="15" class="tp"/><text x="%.1f" y="%.1f" class="tpn">%s</text><text x="%.1f" y="%.1f" class="tpa">%s</text></g>`,
			p.x-64, p.y-15, p.x, p.y-2, html.EscapeString(t.Name), p.x, p.y+9, html.EscapeString(sub))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

const emuReportCSS = `:root{--ink:#1d232a;--mut:#5f6b76;--line:#e3e7eb;--soft:#f5f7f9;--acc:#2a78d6;--ok:#1a7f37;--bad:#cf222e;--chg:#fff4d6}
*{box-sizing:border-box}body{font:13px/1.45 -apple-system,"Segoe UI",system-ui,sans-serif;margin:0;color:var(--ink);background:#fff;-webkit-print-color-adjust:exact;print-color-adjust:exact}
main{max-width:1180px;margin:0 auto;padding:28px 32px 40px}
header{display:flex;align-items:flex-start;gap:16px;border-bottom:3px solid var(--acc);padding-bottom:14px}header .t{flex:1}
h1{font-size:24px;margin:0 0 4px;font-weight:650}h2{font-size:16px;margin:28px 0 10px;padding-bottom:5px;border-bottom:1px solid var(--line)}
h3{font-size:14px;margin:0;display:flex;align-items:center;gap:8px}
.meta{color:var(--mut)}.brand{color:var(--mut);font-size:12px;text-align:right}.brand b{color:var(--acc);font-size:14px}
.verdict{font-weight:700;font-size:13px;padding:5px 12px;border-radius:14px;color:#fff;display:inline-block;margin-bottom:6px}.v-pass{background:var(--ok)}.v-fail{background:var(--bad)}.v-none{background:#8a949e}
.tiles{display:grid;grid-template-columns:repeat(6,1fr);gap:10px;margin-top:16px}.tile{border:1px solid var(--line);border-radius:10px;padding:10px 12px;background:var(--soft)}
.tile .n{font-size:20px;font-weight:650}.tile .l{color:var(--mut);font-size:11.5px;text-transform:uppercase;letter-spacing:.04em}
table{border-collapse:collapse;width:100%}td,th{border-bottom:1px solid var(--line);padding:4px 8px;text-align:left;vertical-align:top}th{color:var(--mut);font-weight:600;font-size:11.5px;background:var(--soft)}
td.r,th.r{text-align:right}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:11.5px}
.pass{color:var(--ok);font-weight:700}.fail{color:var(--bad);font-weight:700}.dot{display:inline-block;width:18px;height:18px;border-radius:9px;color:#fff;font-size:11px;font-weight:700;text-align:center;line-height:18px}
.g{display:grid;grid-template-columns:repeat(auto-fill,minmax(420px,1fr));gap:12px}figure{margin:0;border:1px solid var(--line);border-radius:10px;padding:8px 10px;break-inside:avoid}figure svg{width:100%;height:auto}
figcaption{font-size:12px;margin-bottom:2px;color:var(--mut)}figcaption b{color:var(--ink)}figcaption i{display:inline-block;width:12px;height:3px;margin:0 4px 3px 10px;border-radius:2px}
.topo{width:100%;height:auto;border:1px solid var(--line);border-radius:10px;background:var(--soft)}.topo .sw{fill:#fff;stroke:#3b4752;stroke-width:1.5}.topo .swn{font-size:13px;font-weight:600;text-anchor:middle;fill:var(--ink)}
.topo .tp{fill:#eaf2fc;stroke:var(--acc);stroke-width:1.2}.topo .tpn{font-size:11px;font-weight:600;text-anchor:middle;fill:var(--ink)}.topo .tpa{font-size:9.5px;text-anchor:middle;fill:var(--mut)}
.topo .pn{font-size:10px;fill:var(--mut);text-anchor:middle}.topo .sn{font-size:11px;font-weight:700;fill:#fff;text-anchor:middle}
.card{border:1px solid var(--line);border-radius:12px;padding:14px 16px;margin:14px 0;break-inside:avoid}.card .why{color:var(--bad);margin:4px 0 0}
.two{display:grid;grid-template-columns:1fr 1fr;gap:18px;margin-top:10px}.two h4,.sub{font-size:12px;color:var(--mut);text-transform:uppercase;letter-spacing:.04em;margin:12px 0 6px}
dl{display:grid;grid-template-columns:150px 1fr;gap:3px 10px;margin:0}dt{color:var(--mut)}dd{margin:0}
tr.chg td{background:var(--chg)}td.c{color:#9a6700;font-size:11px;font-weight:600}
pre{background:var(--soft);border:1px solid var(--line);border-radius:8px;padding:8px 10px;margin:0;font-size:10.5px;overflow:auto;white-space:pre}
.legend{color:var(--mut);font-size:12px;margin:6px 0 0}.legend span{margin-right:14px}
footer{margin-top:30px;color:var(--mut);font-size:11.5px;border-top:1px solid var(--line);padding-top:8px}
@page{size:A4 landscape;margin:10mm}@media print{main{padding:0;max-width:none}h2{break-after:avoid}.tiles{grid-template-columns:repeat(6,1fr)}}`

// emuReportHTML is the report as a page of its own (no outside files: it opens anywhere, also offline).
func emuReportHTML(r emuRep) string {
	var b strings.Builder
	e := html.EscapeString
	v := r.V
	verdict, vc := "NO RESULT YET", "v-none"
	switch {
	case v.Running:
		verdict = "STILL RUNNING"
	case r.Failed > 0:
		verdict, vc = fmt.Sprintf("%d FAILED", r.Failed), "v-fail"
	case r.Passed > 0:
		verdict, vc = "ALL PASSED", "v-pass"
	}
	fmt.Fprintf(&b, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Traffic report - %s</title><style>%s</style></head><body><main>`, e(r.Lab), emuReportCSS)
	fmt.Fprintf(&b, `<header><div class="t"><h1>Traffic report: %s</h1><div class="meta">%s · ran %s · %d stream%s on %d test port%s</div></div><div class="brand"><span class="verdict %s">%s</span><br><b>Termwright</b> %s<br>emulated lab traffic generator</div></header>`,
		e(r.Lab), r.At.Format("Monday, January 2, 2006 15:04"), emuDur(r.Secs), len(v.Streams), map[bool]string{true: "s"}[len(v.Streams) != 1], len(v.Testers), map[bool]string{true: "s"}[len(v.Testers) != 1], vc, verdict, e(r.Version))
	loss := "-"
	if r.Expect > 0 {
		loss = fmt.Sprintf("%.3g %%", float64(r.Lost)/float64(r.Expect)*100)
	}
	lat := "-"
	if r.LatAvg >= 0 {
		lat = fmt.Sprintf("%.3f / %.3f", r.LatAvg, r.LatMax)
	}
	b.WriteString(`<div class="tiles">`)
	for _, t := range [][2]string{{emuThousands(r.Sent), "frames sent"}, {emuThousands(r.Got), "frames received"}, {loss, "loss (streams that should arrive)"}, {lat, "latency avg / max, ms"}, {fmt.Sprint(r.Passed), "streams passed"}, {fmt.Sprint(r.Failed), "streams failed"}} {
		fmt.Fprintf(&b, `<div class="tile"><div class="n">%s</div><div class="l">%s</div></div>`, e(t[0]), e(t[1]))
	}
	b.WriteString(`</div>`)
	if v.Running {
		b.WriteString(`<p class="why">The streams were still running when this report was made: the numbers are not final.</p>`)
	}
	if svg := emuTopoSVG(r); svg != "" {
		b.WriteString(`<h2>Topology and streams</h2>` + svg + `<p class="legend"><span>▭ switch (cable ends show the port numbers)</span><span>◯ test port</span><span>→ stream, by its number; dashed: its frames should be dropped</span></p>`)
	}
	if g := emuReportRunGraphs(r); g != "" {
		b.WriteString(`<h2>Over the run</h2>` + g)
	}
	b.WriteString(`<h2>Streams</h2><table><tr><th>#</th><th>Stream</th><th>From → to</th><th>Frames</th><th>Rate</th><th class="r">Sent</th><th class="r">Received</th><th class="r">Loss %</th><th class="r">Latency avg / max ms</th><th class="r">Jitter ms</th><th>Result</th></tr>`)
	for _, s := range v.Streams {
		x := r.Res[s.ID]
		rate, _ := emuRateText(s)
		la, ji := "-", "-"
		if x.LatAvg >= 0 && x.RX > 0 {
			la, ji = fmt.Sprintf("%.3f / %.3f", x.LatAvg, x.LatMax), fmt.Sprintf("%.3f", x.Jitter)
		}
		res := strings.ToUpper(orStr(x.Result, "-"))
		ls, note := fmt.Sprintf("%.2f", x.Loss), ""
		switch s.Expect {
		case "drop":
			ls, note = "-", `<div class="meta">should be dropped</div>`
		case "any":
			ls, note = "-", `<div class="meta">only counted</div>`
		}
		fmt.Fprintf(&b, `<tr><td><span class="dot" style="background:%s">%d</span></td><td><b>%s</b>%s</td><td>%s → %s</td><td>%s</td><td>%s</td><td class="r">%s</td><td class="r">%s</td><td class="r">%s</td><td class="r">%s</td><td class="r">%s</td><td class="%s">%s</td></tr>`,
			r.Color[s.ID], r.Num[s.ID], e(s.Name), note, e(r.testerName(s.From)), e(r.to(s)), e(emuFrameText(s)), e(rate), emuThousands(x.TX), emuThousands(x.RX), ls, la, ji, strings.ToLower(x.Result), res)
	}
	b.WriteString(`</table><h2>Each stream: frames sent and what arrived</h2>`)
	for _, s := range v.Streams {
		b.WriteString(emuReportStream(r, s))
	}
	b.WriteString(`<h2>Test ports</h2><table><tr><th>Test port</th><th>On</th><th>MAC</th><th>Address</th><th>Gateway</th><th>VLAN</th><th class="r">Frames sent</th><th class="r">Frames received</th><th class="r">Its streams' loss %</th><th class="r">Latency avg ms</th><th class="r">Errors</th><th>Protocols</th></tr>`)
	ts := append([]emu.Tester(nil), v.Testers...)
	sort.Slice(ts, func(i, j int) bool { return ts[i].Name < ts[j].Name })
	for _, t := range ts {
		p := r.Port[t.ID]
		la := "-"
		if p.LatAvg >= 0 && p.Got > 0 {
			la = fmt.Sprintf("%.3f", p.LatAvg)
		}
		vl := "untagged"
		if t.VLAN > 0 {
			vl = fmt.Sprint(t.VLAN)
		}
		ip := orStr(t.IP, t.Lease, map[bool]string{true: "DHCP"}[t.DHCP], "-")
		fmt.Fprintf(&b, `<tr><td><b>%s</b></td><td>%s</td><td class="mono">%s</td><td>%s</td><td>%s</td><td>%s</td><td class="r">%s</td><td class="r">%s</td><td class="r">%.2f</td><td class="r">%s</td><td class="r">%d</td><td>%s</td></tr>`,
			e(t.Name), e(r.testerAt(t.ID)), e(t.MAC), e(ip), e(orStr(t.GW, "-")), vl, emuThousands(p.TX), emuThousands(p.RX), p.Loss, la, p.Errors, e(emuTesterProto(t)))
	}
	fmt.Fprintf(&b, `</table><footer>Made by Termwright %s on %s. Frame sizes count the 4-byte FCS. Latency is one way when both test ports run on one KVM host, else the round trip. A frame that arrives twice counts once.</footer></main></body></html>`, e(r.Version), r.At.Format("2006-01-02 15:04:05 MST"))
	return b.String()
}

// emuReportRunGraphs: throughput sent and received over the run, and each stream's loss and latency.
func emuReportRunGraphs(r emuRep) string {
	tx, rx := map[float64]float64{}, map[float64]float64{}
	var loss, lat []emuSeries
	for _, s := range r.V.Streams {
		h := r.V.History[s.ID]
		if len(h) == 0 {
			continue
		}
		name := fmt.Sprintf("stream %d", r.Num[s.ID]) // the numbers of the streams table and the topology
		ls, la := emuSeries{Name: name, Color: r.Color[s.ID]}, emuSeries{Name: name, Color: r.Color[s.ID]}
		for _, p := range h {
			tx[p[0]] += p[1]
			rx[p[0]] += p[2]
			ls.Pts = append(ls.Pts, [2]float64{p[0], p[3]})
			if p[4] >= 0 {
				la.Pts = append(la.Pts, [2]float64{p[0], p[4]})
			}
		}
		loss = append(loss, ls)
		if len(la.Pts) > 0 {
			lat = append(lat, la)
		}
	}
	if len(loss) == 0 {
		return ""
	}
	pts := func(m map[float64]float64) [][2]float64 {
		var out [][2]float64
		for t, n := range m {
			out = append(out, [2]float64{t, n})
		}
		sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
		return out
	}
	var b strings.Builder
	b.WriteString(`<div class="g">`)
	b.WriteString(emuSVGLines("Throughput, all streams", "frames per second", []emuSeries{{"Sent", "#2a78d6", pts(tx)}, {"Received", "#1baf7a", pts(rx)}}))
	b.WriteString(emuSVGLines("Loss so far", "%", loss))
	if len(lat) > 0 {
		b.WriteString(emuSVGLines("Latency", "ms, average each second", lat))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// emuReportStream is one stream's card: what it sent (its settings and first frame) and what arrived.
func emuReportStream(r emuRep, s emu.Stream) string {
	var b strings.Builder
	e := html.EscapeString
	x := r.Res[s.ID]
	res := orStr(strings.ToUpper(x.Result), "NO RESULT")
	fmt.Fprintf(&b, `<div class="card"><h3><span class="dot" style="background:%s">%d</span>%s <span class="%s">%s</span></h3>`, r.Color[s.ID], r.Num[s.ID], e(s.Name), strings.ToLower(x.Result), res)
	if x.Why != "" {
		fmt.Fprintf(&b, `<p class="why">%s</p>`, e(x.Why))
	}
	rate, dur := emuRateText(s)
	b.WriteString(`<div class="two"><div><h4>Frames sent</h4><dl>`)
	dd := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, `<dt>%s</dt><dd>%s</dd>`, e(k), e(v))
		}
	}
	dd("From", r.testerName(s.From)+" ("+r.testerAt(s.From)+")")
	if s.To != "" {
		dd("To", r.testerName(s.To)+" ("+r.testerAt(s.To)+")")
	} else {
		dd("To", r.to(s))
	}
	dd("Frames", emuFrameText(s))
	dd("Rate", rate)
	dd("For", dur)
	if s.Burst > 1 {
		dd("Bursts", fmt.Sprintf("%d frames back to back", s.Burst))
	}
	if s.RTT {
		dd("Latency", "round trip (the far test port sends each frame back)")
	}
	for _, m := range s.Mods {
		dd("Changing field", fmt.Sprintf("%s: %d values, %s", m.Field, m.Count, m.Mode))
	}
	for _, u := range s.UDFs {
		dd("User-defined field", fmt.Sprintf("bytes %d-%d: %s, %d values", u.Offset, u.Offset+u.Width-1, u.Mode, u.Count))
	}
	if s.Bad != "" {
		if f := emu.BadFrame(s.Bad); f != nil {
			dd("Fault", f.Name+": "+f.What)
		}
	}
	if s.Fuzz != nil {
		dd("Fuzzing", fmt.Sprintf("random bytes from %d to %s, seed %d", s.Fuzz.From, map[bool]string{true: "the signature", false: fmt.Sprint(s.Fuzz.To)}[s.Fuzz.To == 0], s.Fuzz.Seed))
	}
	if s.Kind == "pcap" {
		dd("Capture", fmt.Sprintf("%s: %d frames", filepath.Base(s.Pcap), s.PcapFrames))
	}
	dd("Passes when", emuPassText(s))
	b.WriteString(`</dl></div><div><h4>Output received</h4><dl>`)
	dd("Frames sent", emuThousands(x.TX))
	if s.Expect != "drop" {
		dd("Should arrive", emuThousands(x.Expected))
	}
	dd("Arrived", emuThousands(x.RX))
	if x.Other > 0 {
		dd("Arrived elsewhere", emuThousands(x.Other)+" (flooded to other test ports)")
	}
	if s.Expect != "drop" {
		dd("Loss", fmt.Sprintf("%.3f %% (%s frames)", x.Loss, emuThousands(max(0, x.Expected-x.RX))))
	}
	if x.OOO > 0 || x.Dup > 0 {
		dd("Out of order, twice", fmt.Sprintf("%s, %s", emuThousands(x.OOO), emuThousands(x.Dup)))
	}
	if x.LatAvg >= 0 && x.RX > 0 {
		dd("Latency, ms", fmt.Sprintf("min %.3f · avg %.3f · max %.3f", x.LatMin, x.LatAvg, x.LatMax))
		if x.P50 > 0 {
			dd("Percentiles, ms", fmt.Sprintf("p50 %.3f · p95 %.3f · p99 %.3f", x.P50, x.P95, x.P99))
		}
		dd("Jitter", fmt.Sprintf("%.3f ms", x.Jitter))
	}
	for _, m := range x.Marks {
		d := ""
		if m.DSCP >= 0 {
			d = fmt.Sprintf(", DSCP %d", m.DSCP)
		}
		dd("Arrived marked", fmt.Sprintf("%s, priority %d%s: %s frames", map[bool]string{true: "untagged", false: fmt.Sprintf("VLAN %d", m.VLAN)}[m.VLAN == 0], m.Prio, d, emuThousands(m.N)))
	}
	b.WriteString(`</dl></div></div>`)
	sent, sn := emuHexBytes(x.Sent)
	got, gn := emuHexBytes(x.Got)
	if sent != nil {
		b.WriteString(`<div class="sub">First frame sent and received`)
		if got == nil {
			b.WriteString(` (none arrived)`)
		}
		b.WriteString(`</div><table><tr><th>Layer</th><th>Field</th><th>Sent</th><th>Received</th><th></th></tr>`)
		for _, f := range emuFrameRows(sent, got) {
			cls := map[bool]string{true: ` class="chg"`}[f[4] != ""]
			fmt.Fprintf(&b, `<tr%s><td>%s</td><td>%s</td><td class="mono">%s</td><td class="mono">%s</td><td class="c">%s</td></tr>`, cls, e(f[0]), e(f[1]), e(f[2]), e(f[3]), map[bool]string{true: "changed on the way"}[f[4] != ""])
		}
		b.WriteString(`</table><div class="two">`)
		shown := func(b []byte, n int) string {
			if n > len(b) {
				return fmt.Sprintf(" (the first %d)", len(b))
			}
			return ""
		}
		fmt.Fprintf(&b, `<div><div class="sub">Sent: %d bytes and the FCS%s</div><pre>%s</pre></div>`, sn, shown(sent, sn), e(emuHexDump(sent)))
		if got != nil {
			fmt.Fprintf(&b, `<div><div class="sub">Received: %d bytes%s</div><pre>%s</pre></div>`, gn, shown(got, gn), e(emuHexDump(got)))
		}
		b.WriteString(`</div>`)
	}
	if len(x.Hist) > 0 {
		b.WriteString(`<div class="g" style="margin-top:12px">` + emuSVGHist(fmt.Sprintf("Latency: p50 %.3f · p95 %.3f · p99 %.3f ms", x.P50, x.P95, x.P99), x.Hist) + `</div>`)
	}
	if len(x.Flows) > 0 {
		fmt.Fprintf(&b, `<div class="sub">%d flows, %d with loss</div><table><tr><th>Flow</th><th class="r">Sent</th><th class="r">Received</th><th class="r">Loss %%</th><th class="r">Latency avg ms</th><th class="r">Latency max ms</th></tr>`, x.FlowsN, x.FlowsBad)
		for _, f := range x.Flows {
			la, lm := "-", "-"
			if f.LatAvg >= 0 {
				la, lm = fmt.Sprintf("%.3f", f.LatAvg), fmt.Sprintf("%.3f", f.LatMax)
			}
			fmt.Fprintf(&b, `<tr><td>%s</td><td class="r">%s</td><td class="r">%s</td><td class="r">%.2f</td><td class="r">%s</td><td class="r">%s</td></tr>`, e(f.Name), emuThousands(f.TX), emuThousands(f.RX), f.Loss, la, lm)
		}
		b.WriteString(`</table>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// emuPDFBrowser is a Chromium browser that prints a page to PDF (Chrome, Edge, Chromium or Brave).
func (a *App) emuPDFBrowser() string {
	if p := os.Getenv("TW_PDF_BROWSER"); p != "" {
		return p
	}
	for _, w := range a.WebBrowsers() {
		if w.kind == "chromium" {
			return w.path
		}
	}
	return ""
}

// emuHTMLToPDF prints a page to PDF with a browser of this computer, headless, in a profile of its own.
func (a *App) emuHTMLToPDF(page string) ([]byte, error) {
	br := a.emuPDFBrowser()
	if br == "" {
		return nil, errors.New("a PDF needs Google Chrome, Microsoft Edge, Chromium or Brave on this computer: save the report as HTML and print it to PDF from a browser")
	}
	dir, err := os.MkdirTemp("", "tw-report-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "report.html"), filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(in, []byte(page), 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	u := "file://" + filepath.ToSlash(in)
	if !strings.HasPrefix(filepath.ToSlash(in), "/") {
		u = "file:///" + filepath.ToSlash(in)
	}
	args := []string{"--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--disable-extensions",
		"--user-data-dir=" + filepath.Join(dir, "profile"), "--no-pdf-header-footer", "--print-to-pdf=" + out}
	if os.Geteuid() == 0 { // Chromium will not run as root with its sandbox (a container)
		args = append(args, "--no-sandbox")
	}
	cmd := exec.CommandContext(ctx, br, append(args, u)...)
	hideWindow(cmd)
	msg, err := cmd.CombinedOutput()
	b, rerr := os.ReadFile(out)
	if rerr != nil || len(b) == 0 {
		if err == nil {
			err = rerr
		}
		return nil, fmt.Errorf("printing the report to PDF with %s failed: %v %s", filepath.Base(br), err, strings.TrimSpace(string(msg)))
	}
	return b, nil
}

// emuReportXLSX is the report as an Excel workbook: a summary with the streams, the graphs (Excel's own charts over
// the Timeline sheet), frames sent, output received, the first frames decoded, flows, test ports and the topology.
func emuReportXLSX(r emuRep) ([]byte, error) {
	v := r.V
	H := func(s ...string) []any {
		out := make([]any, len(s))
		for i, x := range s {
			out[i] = xlsx.Cell{V: x, S: xlsx.Header}
		}
		return out
	}
	res := func(x EmuStreamResult) any {
		return xlsx.Cell{V: orStr(strings.ToUpper(x.Result), "-"), S: map[string]int{"pass": xlsx.Pass, "fail": xlsx.Fail}[x.Result]}
	}
	lat := func(x float64, ok bool) any {
		if !ok {
			return "-"
		}
		return math.Round(x*1000) / 1000
	}
	// Summary
	sum := [][]any{{xlsx.Cell{V: "Traffic report: " + r.Lab, S: xlsx.Title}}, {"Made", r.At.Format("2006-01-02 15:04")}, {"Ran", emuDur(r.Secs)},
		{"Streams", len(v.Streams)}, {"Test ports", len(v.Testers)}, {"Frames sent", r.Sent}, {"Frames received", r.Got}, {"Passed", xlsx.Cell{V: r.Passed, S: xlsx.Pass}}, {"Failed", xlsx.Cell{V: r.Failed, S: map[bool]int{true: xlsx.Fail}[r.Failed > 0]}}}
	if r.LatAvg >= 0 {
		sum = append(sum, []any{"Latency avg, ms", math.Round(r.LatAvg*1000) / 1000}, []any{"Latency max, ms", math.Round(r.LatMax*1000) / 1000})
	}
	if v.Running {
		sum = append(sum, []any{xlsx.Cell{V: "Still running: the numbers are not final", S: xlsx.Fail}})
	}
	sum = append(sum, []any{"Termwright", r.Version}, []any{}, H("#", "Stream", "From", "To", "Frames", "Rate", "Sent", "Received", "Loss %", "Latency avg ms", "Latency max ms", "Jitter ms", "Result", "Why"))
	for _, s := range v.Streams {
		x := r.Res[s.ID]
		rate, _ := emuRateText(s)
		ok := x.LatAvg >= 0 && x.RX > 0
		sum = append(sum, []any{r.Num[s.ID], xlsx.Cell{V: s.Name, S: xlsx.Bold}, r.testerName(s.From), r.to(s), emuFrameText(s), rate, x.TX, x.RX, math.Round(x.Loss*1000) / 1000, lat(x.LatAvg, ok), lat(x.LatMax, ok), lat(x.Jitter, ok), res(x), x.Why})
	}
	// Timeline: one row per moment, the throughput of all streams and each stream's loss and latency
	type key = int64
	at := map[key]float64{}
	txs, rxs := map[key]float64{}, map[key]float64{}
	loss, lt := map[string]map[key]float64{}, map[string]map[key]float64{}
	var latStreams []emu.Stream
	for _, s := range v.Streams {
		loss[s.ID], lt[s.ID] = map[key]float64{}, map[key]float64{}
		has := false
		for _, p := range v.History[s.ID] {
			k := key(math.Round(p[0] * 10))
			at[k] = math.Round(p[0]*10) / 10
			txs[k] += p[1]
			rxs[k] += p[2]
			loss[s.ID][k] = math.Round(p[3]*1000) / 1000
			if p[4] >= 0 {
				lt[s.ID][k], has = math.Round(p[4]*1000)/1000, true
			}
		}
		if has {
			latStreams = append(latStreams, s)
		}
	}
	keys := make([]key, 0, len(at))
	for k := range at {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	head := []string{"Seconds", "Sent, frames/s", "Received, frames/s"}
	for _, s := range v.Streams {
		head = append(head, fmt.Sprintf("Loss %% %d %s", r.Num[s.ID], s.Name))
	}
	for _, s := range latStreams {
		head = append(head, fmt.Sprintf("Latency ms %d %s", r.Num[s.ID], s.Name))
	}
	tl := [][]any{H(head...)}
	for _, k := range keys {
		row := []any{at[k], math.Round(txs[k]), math.Round(rxs[k])}
		for _, s := range v.Streams {
			if x, ok := loss[s.ID][k]; ok {
				row = append(row, x)
			} else {
				row = append(row, nil)
			}
		}
		for _, s := range latStreams {
			if x, ok := lt[s.ID][k]; ok {
				row = append(row, x)
			} else {
				row = append(row, nil)
			}
		}
		tl = append(tl, row)
	}
	var charts []xlsx.Chart
	if len(keys) > 0 {
		charts = append(charts, xlsx.Chart{Title: "Throughput, all streams", YTitle: "frames per second", XTitle: "seconds", Sheet: "Timeline", Series: []int{1, 2}, Colors: []string{"2A78D6", "1BAF7A"}, Rows: len(keys), Col: 0, Row: 1, W: 9, H: 18})
		var ls, lc []int
		var cs, cc []string
		for i, s := range v.Streams {
			ls, cs = append(ls, 3+i), append(cs, r.Color[s.ID])
		}
		for i, s := range latStreams {
			lc, cc = append(lc, 3+len(v.Streams)+i), append(cc, r.Color[s.ID])
		}
		charts = append(charts, xlsx.Chart{Title: "Loss so far", YTitle: "%", XTitle: "seconds", Sheet: "Timeline", Series: ls, Colors: cs, Rows: len(keys), Col: 10, Row: 1, W: 9, H: 18})
		if len(lc) > 0 {
			charts = append(charts, xlsx.Chart{Title: "Latency", YTitle: "ms", XTitle: "seconds", Sheet: "Timeline", Series: lc, Colors: cc, Rows: len(keys), Col: 0, Row: 20, W: 9, H: 18})
		}
	}
	// Frames sent: each stream's settings
	sent := [][]any{H("#", "Stream", "From", "From switch port", "To", "To switch port", "Type", "Size", "VLAN", "Priority", "DSCP", "Rate", "For", "Burst", "Fault", "Fuzzing", "Changing fields", "Custom frame (hex)", "Passes when", "Frames sent")}
	for _, s := range v.Streams {
		var ch []string
		for _, m := range s.Mods {
			ch = append(ch, fmt.Sprintf("%s %s ×%d", m.Field, m.Mode, m.Count))
		}
		for _, u := range s.UDFs {
			ch = append(ch, fmt.Sprintf("bytes %d-%d %s ×%d", u.Offset, u.Offset+u.Width-1, u.Mode, u.Count))
		}
		fault, fz := "", ""
		if f := emu.BadFrame(s.Bad); f != nil {
			fault = f.Name
		}
		if s.Fuzz != nil {
			fz = fmt.Sprintf("bytes %d-%d, seed %d", s.Fuzz.From, s.Fuzz.To, s.Fuzz.Seed)
		}
		rate, dur := emuRateText(s)
		size := any(s.Size)
		if s.SizeMode != "" {
			size = map[string]string{"imix": "IMIX"}[s.SizeMode]
			if s.SizeMode != "imix" {
				size = fmt.Sprintf("%d-%d %s", s.Size, s.SizeMax, s.SizeMode)
			}
		}
		sent = append(sent, []any{r.Num[s.ID], xlsx.Cell{V: s.Name, S: xlsx.Bold}, r.testerName(s.From), r.testerAt(s.From), r.to(s), r.testerAt(s.To), s.Kind, size, s.VLAN, s.Prio, s.DSCP, rate, dur, max(1, s.Burst), fault, fz, strings.Join(ch, "; "), xlsx.Cell{V: s.Hex, S: xlsx.Mono}, emuPassText(s), r.Res[s.ID].TX})
	}
	// Output received
	recv := [][]any{H("#", "Stream", "Sent", "Should arrive", "Received", "Elsewhere (flooded)", "Lost", "Loss %", "Out of order", "Twice", "Latency min ms", "Latency avg ms", "p50 ms", "p95 ms", "p99 ms", "Latency max ms", "Jitter ms", "Arrived marked (VLAN / priority / DSCP: frames)", "Flows", "Flows with loss", "Result", "Why")}
	for _, s := range v.Streams {
		x := r.Res[s.ID]
		ok := x.LatAvg >= 0 && x.RX > 0
		var mk []string
		for _, m := range x.Marks {
			mk = append(mk, fmt.Sprintf("%s / %d / %d: %d", map[bool]string{true: "untagged", false: fmt.Sprint(m.VLAN)}[m.VLAN == 0], m.Prio, m.DSCP, m.N))
		}
		recv = append(recv, []any{r.Num[s.ID], xlsx.Cell{V: s.Name, S: xlsx.Bold}, x.TX, x.Expected, x.RX, x.Other, max(0, x.Expected-x.RX), math.Round(x.Loss*1000) / 1000, x.OOO, x.Dup,
			lat(x.LatMin, ok), lat(x.LatAvg, ok), lat(x.P50, ok && x.P50 > 0), lat(x.P95, ok && x.P50 > 0), lat(x.P99, ok && x.P50 > 0), lat(x.LatMax, ok), lat(x.Jitter, ok), strings.Join(mk, "; "), x.FlowsN, x.FlowsBad, res(x), x.Why})
	}
	// First frames, decoded
	fr := [][]any{H("#", "Stream", "Layer", "Field", "Sent", "Received", "")}
	for _, s := range v.Streams {
		x := r.Res[s.ID]
		sb, _ := emuHexBytes(x.Sent)
		gb, _ := emuHexBytes(x.Got)
		if sb == nil {
			continue
		}
		for _, f := range emuFrameRows(sb, gb) {
			st := map[bool]int{true: xlsx.Change}[f[4] != ""]
			fr = append(fr, []any{r.Num[s.ID], s.Name, xlsx.Cell{V: f[0], S: st}, xlsx.Cell{V: f[1], S: st}, xlsx.Cell{V: f[2], S: max(st, xlsx.Mono*map[bool]int{true: 1}[st == 0])}, xlsx.Cell{V: f[3], S: max(st, xlsx.Mono*map[bool]int{true: 1}[st == 0])}, map[bool]string{true: "changed on the way"}[f[4] != ""]})
		}
		fr = append(fr, []any{r.Num[s.ID], s.Name, "Hex", "sent", xlsx.Cell{V: strings.TrimRight(emuHexDump(sb), "\n"), S: xlsx.Wrap}, xlsx.Cell{V: strings.TrimRight(emuHexDump(gb), "\n"), S: xlsx.Wrap}}, []any{})
	}
	// Flows
	fl := [][]any{H("#", "Stream", "Flow", "Sent", "Received", "Loss %", "Latency avg ms", "Latency max ms")}
	for _, s := range v.Streams {
		for _, f := range r.Res[s.ID].Flows {
			fl = append(fl, []any{r.Num[s.ID], s.Name, f.Name, f.TX, f.RX, math.Round(f.Loss*1000) / 1000, lat(f.LatAvg, f.LatAvg >= 0), lat(f.LatMax, f.LatAvg >= 0)})
		}
	}
	// Test ports
	tp := [][]any{H("Test port", "Switch port", "MAC", "Address", "Gateway", "VLAN", "Line rate Mbit/s", "Frames sent", "Frames received", "Bytes sent", "Bytes received", "Its streams' frames expected", "Arrived", "Loss %", "Latency avg ms", "Latency max ms", "Errors", "Protocols")}
	ts := append([]emu.Tester(nil), v.Testers...)
	sort.Slice(ts, func(i, j int) bool { return ts[i].Name < ts[j].Name })
	for _, t := range ts {
		p := r.Port[t.ID]
		ok := p.LatAvg >= 0 && p.Got > 0
		tp = append(tp, []any{xlsx.Cell{V: t.Name, S: xlsx.Bold}, r.testerAt(t.ID), xlsx.Cell{V: t.MAC, S: xlsx.Mono}, orStr(t.IP, t.Lease, map[bool]string{true: "DHCP"}[t.DHCP]), t.GW, t.VLAN, t.LineMbps(), p.TX, p.RX, p.TxBytes, p.RxBytes, p.Expected, p.Got, math.Round(p.Loss*1000) / 1000, lat(p.LatAvg, ok), lat(p.LatMax, ok), p.Errors, emuTesterProto(t)})
	}
	// Topology: the cables between the switches, and where the test ports are
	topo := [][]any{H("From", "Port", "To", "Port", "State")}
	for _, c := range r.Cables {
		a, b := r.VMName[c.A], r.VMName[c.B]
		if a == "" || b == "" {
			continue
		}
		topo = append(topo, []any{a, c.APort, b, c.BPort, map[bool]string{true: "pulled", false: "up"}[c.Down]})
	}
	topo = append(topo, []any{}, H("Test port", "On", "", "", ""))
	for _, t := range ts {
		topo = append(topo, []any{t.Name, r.testerAt(t.ID)})
	}
	sheets := []xlsx.Sheet{
		{Name: "Summary", Rows: sum, Widths: []float64{18, 26, 16, 16, 34, 22, 12, 12, 9, 14, 14, 10, 9, 40}},
		{Name: "Graphs", Rows: [][]any{{xlsx.Cell{V: "Over the run (data on the Timeline sheet)", S: xlsx.Bold}}}, Charts: charts},
		{Name: "Frames sent", Rows: sent, Freeze: true, Widths: []float64{5, 24, 14, 16, 14, 16, 8, 10, 6, 8, 6, 22, 12, 6, 20, 18, 26, 40, 34, 12}},
		{Name: "Output received", Rows: recv, Freeze: true, Widths: []float64{5, 24, 11, 12, 11, 12, 9, 9, 10, 8, 12, 12, 9, 9, 9, 12, 10, 36, 7, 12, 8, 40}},
		{Name: "First frames", Rows: fr, Freeze: true, Widths: []float64{5, 22, 12, 20, 76, 76, 18}},
		{Name: "Flows", Rows: fl, Freeze: true, Widths: []float64{5, 24, 40, 11, 11, 9, 14, 14}},
		{Name: "Test ports", Rows: tp, Freeze: true, Widths: []float64{14, 18, 19, 18, 14, 7, 10, 12, 13, 12, 13, 14, 10, 9, 12, 12, 8}},
		{Name: "Timeline", Rows: tl, Freeze: true, Widths: []float64{10, 14, 16}},
		{Name: "Topology", Rows: topo, Widths: []float64{16, 18, 16, 8, 8}},
	}
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, sheets); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
