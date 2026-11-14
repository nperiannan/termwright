package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// One packet of a capture in full: its bytes and the fields of the protocols
// Termwright knows (Ethernet, VLAN, ARP, IPv4/IPv6, ICMP, UDP, TCP, LLDP, STP,
// LACP, EDP), each with the bytes it takes so the screen can mark them.

// EmuField is a decoded field or protocol layer: Off and N are its bytes in the frame.
type EmuField struct {
	Name  string     `json:"name"`
	Value string     `json:"value,omitempty"`
	Off   int        `json:"off"`
	N     int        `json:"n"`
	Kids  []EmuField `json:"kids,omitempty"`
}

// EmuPacketDetail is one packet: the list row, when it was captured, its bytes (hex) and its fields.
type EmuPacketDetail struct {
	EmuPacket
	At     string     `json:"at"` // wall clock, 15:04:05.000000
	Hex    string     `json:"hex"`
	Fields []EmuField `json:"fields"`
	Total  int        `json:"total"` // packets in the capture
}

type emuRec struct {
	ts   float64
	orig int
	data []byte
}

// emuPcapRecords splits a pcap file into its records (byte order and nanoseconds handled).
func emuPcapRecords(b []byte) ([]emuRec, bool) {
	if len(b) < 24 {
		return nil, false
	}
	var bo binary.ByteOrder = binary.LittleEndian
	switch binary.LittleEndian.Uint32(b) {
	case 0xa1b2c3d4, 0xa1b23c4d:
	case 0xd4c3b2a1, 0x4d3cb2a1:
		bo = binary.BigEndian
	default:
		return nil, false
	}
	div := 1e6
	if binary.LittleEndian.Uint32(b) == 0xa1b23c4d || binary.BigEndian.Uint32(b) == 0xa1b23c4d {
		div = 1e9
	}
	var out []emuRec
	for off := 24; off+16 <= len(b); {
		ts := float64(bo.Uint32(b[off:])) + float64(bo.Uint32(b[off+4:]))/div
		incl, orig := int(bo.Uint32(b[off+8:])), int(bo.Uint32(b[off+12:]))
		off += 16
		if incl < 0 || off+incl > len(b) {
			break
		}
		out = append(out, emuRec{ts, orig, b[off : off+incl]})
		off += incl
	}
	return out, true
}

// The last capture file read, kept a little: clicking through packets reads it once.
var emuCapCache struct {
	sync.Mutex
	file string
	at   time.Time
	data []byte
	done bool // the capture had stopped: the file does not change
}

func (a *App) emuCaptureBytes(key string) ([]byte, error) {
	c, ok := emuCaptureByKey(key)
	if !ok {
		return nil, errors.New("no such capture")
	}
	emuCapCache.Lock()
	if emuCapCache.file == c.File && (emuCapCache.done || time.Since(emuCapCache.at) < 3*time.Second) {
		b := emuCapCache.data
		emuCapCache.Unlock()
		return b, nil
	}
	emuCapCache.Unlock()
	b, err := a.emuCaptureRead(c, 32<<20)
	if err != nil {
		return nil, err
	}
	emuCapCache.Lock()
	emuCapCache.file, emuCapCache.at, emuCapCache.data, emuCapCache.done = c.File, time.Now(), b, !c.Running
	emuCapCache.Unlock()
	return b, nil
}

// EmuCapturePacket is packet no (1 = the first) of a capture in full.
func (a *App) EmuCapturePacket(key string, no int) (EmuPacketDetail, error) {
	b, err := a.emuCaptureBytes(key)
	if err != nil {
		return EmuPacketDetail{}, err
	}
	rs, ok := emuPcapRecords(b)
	if !ok {
		return EmuPacketDetail{}, errors.New("not a pcap file")
	}
	if no < 1 || no > len(rs) {
		return EmuPacketDetail{}, fmt.Errorf("no packet %d (the capture has %d)", no, len(rs))
	}
	r := rs[no-1]
	p := emuDecodeFrame(r.data)
	p.No, p.Time, p.Len = no, r.ts-rs[0].ts, r.orig
	sec := int64(r.ts)
	at := time.Unix(sec, int64((r.ts-float64(sec))*1e9)).Format("2006-01-02 15:04:05.000000")
	return EmuPacketDetail{EmuPacket: p, At: at, Hex: hex.EncodeToString(r.data), Fields: emuFields(r.data), Total: len(rs)}, nil
}

// EmuCaptureSavePackets writes the chosen packets of a capture to a pcap file on this computer.
func (a *App) EmuCaptureSavePackets(key string, nos []int, local string) error {
	defer emuAs("user")()
	b, err := a.emuCaptureBytes(key)
	if err != nil {
		return err
	}
	rs, ok := emuPcapRecords(b)
	if !ok {
		return errors.New("not a pcap file")
	}
	var pick []emuRec
	for i, r := range rs {
		if slices.Contains(nos, i+1) {
			pick = append(pick, r)
		}
	}
	if len(pick) == 0 {
		return errors.New("no packet chosen")
	}
	out := emuPcapWrite(pick)
	return os.WriteFile(local, out, 0o644)
}

// emuPcapWrite makes a pcap file (microseconds, Ethernet) of records.
func emuPcapWrite(rs []emuRec) []byte {
	out := make([]byte, 24)
	binary.LittleEndian.PutUint32(out, 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(out[4:], 2)
	binary.LittleEndian.PutUint16(out[6:], 4)
	binary.LittleEndian.PutUint32(out[16:], 65535)
	binary.LittleEndian.PutUint32(out[20:], 1)
	for _, r := range rs {
		h := make([]byte, 16)
		sec := uint32(r.ts)
		us := uint32((r.ts-float64(sec))*1e6 + 0.5)
		if us >= 1e6 {
			sec, us = sec+1, us-1e6
		}
		binary.LittleEndian.PutUint32(h, sec)
		binary.LittleEndian.PutUint32(h[4:], us)
		binary.LittleEndian.PutUint32(h[8:], uint32(len(r.data)))
		binary.LittleEndian.PutUint32(h[12:], uint32(r.orig))
		out = append(append(out, h...), r.data...)
	}
	return out
}

// ---- the fields

func fld(name, value string, off, n int, kids ...EmuField) EmuField {
	return EmuField{Name: name, Value: value, Off: off, N: n, Kids: kids}
}

func be16(b []byte, i int) int { return int(binary.BigEndian.Uint16(b[i:])) }

// emuFields decodes a frame into layers of fields (as far as it knows; the rest is Data).
func emuFields(f []byte) []EmuField {
	if len(f) < 14 {
		return []EmuField{fld("Frame", fmt.Sprintf("%d bytes, too short for Ethernet", len(f)), 0, len(f))}
	}
	eth := fld("Ethernet II", emuMAC(f[6:12])+" → "+emuMAC(f[0:6]), 0, 14,
		fld("Destination", emuMAC(f[0:6]), 0, 6), fld("Source", emuMAC(f[6:12]), 6, 6))
	out := []EmuField{eth}
	off := 12
	et := be16(f, off)
	for (et == 0x8100 || et == 0x88a8) && off+6 <= len(f) {
		tci := be16(f, off+2)
		out = append(out, fld("802.1Q VLAN", fmt.Sprintf("VLAN %d, priority %d", tci&0xfff, tci>>13), off, 4,
			fld("TPID", fmt.Sprintf("0x%04x", et), off, 2), fld("Priority", fmt.Sprint(tci>>13), off+2, 2), fld("DEI", fmt.Sprint(tci>>12&1), off+2, 2), fld("VLAN ID", fmt.Sprint(tci&0xfff), off+2, 2)))
		off += 4
		et = be16(f, off)
	}
	tf := fld("Type", fmt.Sprintf("0x%04x", et), off, 2)
	if et < 0x0600 {
		tf = fld("Length", fmt.Sprint(et), off, 2)
	}
	out[0].Kids = append(out[0].Kids, tf)
	off += 2
	pl := f[off:]
	var more []EmuField
	switch {
	case et < 0x0600:
		more = emuLLCFields(pl, off, emuMAC(f[0:6]))
	case et == 0x0806:
		more = emuARPFields(pl, off)
	case et == 0x88cc:
		more = []EmuField{emuLLDPFields(pl, off)}
	case et == 0x8809:
		more = []EmuField{emuLACPFields(pl, off)}
	case et == 0x0800:
		more = emuIPv4Fields(pl, off)
	case et == 0x86dd:
		more = emuIPv6Fields(pl, off)
	}
	out = append(out, more...)
	used := 0
	for _, x := range out {
		used = max(used, x.Off+x.N)
	}
	if used < len(f) {
		out = append(out, fld("Data", fmt.Sprintf("%d bytes", len(f)-used), used, len(f)-used))
	}
	return out
}

func emuARPFields(b []byte, o int) []EmuField {
	if len(b) < 28 {
		return nil
	}
	op := map[int]string{1: "request", 2: "reply"}[be16(b, 6)]
	return []EmuField{fld("ARP", op, o, 28,
		fld("Hardware type", fmt.Sprint(be16(b, 0)), o, 2), fld("Protocol type", fmt.Sprintf("0x%04x", be16(b, 2)), o+2, 2),
		fld("Opcode", fmt.Sprintf("%d (%s)", be16(b, 6), op), o+6, 2),
		fld("Sender MAC", emuMAC(b[8:14]), o+8, 6), fld("Sender IP", net.IP(b[14:18]).String(), o+14, 4),
		fld("Target MAC", emuMAC(b[18:24]), o+18, 6), fld("Target IP", net.IP(b[24:28]).String(), o+24, 4))}
}

func emuIPv4Fields(b []byte, o int) []EmuField {
	if len(b) < 20 {
		return nil
	}
	ihl := int(b[0]&15) * 4
	if ihl < 20 || ihl > len(b) {
		return nil
	}
	proto := b[9]
	fl := b[6] >> 5
	ip := fld("IPv4", net.IP(b[12:16]).String()+" → "+net.IP(b[16:20]).String(), o, ihl,
		fld("Version", fmt.Sprint(b[0]>>4), o, 1), fld("Header length", fmt.Sprintf("%d bytes", ihl), o, 1),
		fld("DSCP", fmt.Sprint(b[1]>>2), o+1, 1), fld("Total length", fmt.Sprint(be16(b, 2)), o+2, 2),
		fld("Identification", fmt.Sprintf("0x%04x", be16(b, 4)), o+4, 2),
		fld("Flags", strings.TrimSpace(map[bool]string{true: "DF ", false: ""}[fl&2 != 0]+map[bool]string{true: "MF", false: ""}[fl&1 != 0]), o+6, 2),
		fld("TTL", fmt.Sprint(b[8]), o+8, 1), fld("Protocol", fmt.Sprint(proto), o+9, 1),
		fld("Checksum", fmt.Sprintf("0x%04x", be16(b, 10)), o+10, 2),
		fld("Source", net.IP(b[12:16]).String(), o+12, 4), fld("Destination", net.IP(b[16:20]).String(), o+16, 4))
	return append([]EmuField{ip}, emuL4Fields(proto, b[ihl:min(len(b), max(ihl, be16(b, 2)))], o+ihl)...)
}

func emuIPv6Fields(b []byte, o int) []EmuField {
	if len(b) < 40 {
		return nil
	}
	ip := fld("IPv6", net.IP(b[8:24]).String()+" → "+net.IP(b[24:40]).String(), o, 40,
		fld("Payload length", fmt.Sprint(be16(b, 4)), o+4, 2), fld("Next header", fmt.Sprint(b[6]), o+6, 1),
		fld("Hop limit", fmt.Sprint(b[7]), o+7, 1), fld("Source", net.IP(b[8:24]).String(), o+8, 16), fld("Destination", net.IP(b[24:40]).String(), o+24, 16))
	return append([]EmuField{ip}, emuL4Fields(b[6], b[40:], o+40)...)
}

func emuL4Fields(proto byte, b []byte, o int) []EmuField {
	switch {
	case proto == 17 && len(b) >= 8:
		return []EmuField{fld("UDP", fmt.Sprintf("%d → %d", be16(b, 0), be16(b, 2)), o, 8,
			fld("Source port", fmt.Sprint(be16(b, 0)), o, 2), fld("Destination port", fmt.Sprint(be16(b, 2)), o+2, 2),
			fld("Length", fmt.Sprint(be16(b, 4)), o+4, 2), fld("Checksum", fmt.Sprintf("0x%04x", be16(b, 6)), o+6, 2))}
	case proto == 6 && len(b) >= 20:
		hl := int(b[12]>>4) * 4
		var f []string
		for i, n := range []string{"FIN", "SYN", "RST", "PSH", "ACK", "URG"} {
			if b[13]&(1<<i) != 0 {
				f = append(f, n)
			}
		}
		return []EmuField{fld("TCP", fmt.Sprintf("%d → %d [%s]", be16(b, 0), be16(b, 2), strings.Join(f, ",")), o, min(max(hl, 20), len(b)),
			fld("Source port", fmt.Sprint(be16(b, 0)), o, 2), fld("Destination port", fmt.Sprint(be16(b, 2)), o+2, 2),
			fld("Sequence", fmt.Sprint(binary.BigEndian.Uint32(b[4:])), o+4, 4), fld("Acknowledgment", fmt.Sprint(binary.BigEndian.Uint32(b[8:])), o+8, 4),
			fld("Header length", fmt.Sprintf("%d bytes", hl), o+12, 1), fld("Flags", strings.Join(f, ", "), o+13, 1),
			fld("Window", fmt.Sprint(be16(b, 14)), o+14, 2), fld("Checksum", fmt.Sprintf("0x%04x", be16(b, 16)), o+16, 2))}
	case (proto == 1 || proto == 58) && len(b) >= 4:
		name := map[bool]string{true: "ICMPv6", false: "ICMP"}[proto == 58]
		x := fld(name, fmt.Sprintf("type %d code %d", b[0], b[1]), o, min(8, len(b)),
			fld("Type", fmt.Sprint(b[0]), o, 1), fld("Code", fmt.Sprint(b[1]), o+1, 1), fld("Checksum", fmt.Sprintf("0x%04x", be16(b, 2)), o+2, 2))
		echo := proto == 1 && (b[0] == 0 || b[0] == 8) || proto == 58 && (b[0] == 128 || b[0] == 129)
		if echo && len(b) >= 8 {
			x.Value = map[bool]string{true: "echo request", false: "echo reply"}[b[0] == 8 || b[0] == 128]
			x.Kids = append(x.Kids, fld("Identifier", fmt.Sprint(be16(b, 4)), o+4, 2), fld("Sequence", fmt.Sprint(be16(b, 6)), o+6, 2))
		}
		return []EmuField{x}
	}
	return nil
}

var emuLLDPNames = map[int]string{1: "Chassis ID", 2: "Port ID", 3: "Time to live", 4: "Port description", 5: "System name", 6: "System description", 7: "Capabilities", 8: "Management address", 127: "Organization specific"}

func emuLLDPFields(b []byte, o int) EmuField {
	x := fld("LLDP", "", o, 0)
	i := 0
	for i+2 <= len(b) {
		h := be16(b, i)
		t, n := h>>9, h&0x1ff
		if i+2+n > len(b) {
			break
		}
		v := b[i+2 : i+2+n]
		name := orStr(emuLLDPNames[t], fmt.Sprintf("TLV %d", t))
		val := ""
		switch {
		case t == 0:
			name = "End"
		case (t == 1 || t == 2) && n > 1:
			if v[0] == 4 && n == 7 || t == 2 && v[0] == 3 && n == 7 { // a MAC address
				val = emuMAC(v[1:7])
			} else {
				val = emuPrintable(v[1:])
			}
		case t == 3 && n >= 2:
			val = fmt.Sprintf("%d s", be16(v, 0))
		case t == 7 && n >= 4:
			val = emuCaps(be16(v, 0)) + " (on: " + emuCaps(be16(v, 2)) + ")"
		case t == 8 && n >= 2 && int(v[0]) >= 5 && v[1] == 1 && n >= 6:
			val = net.IP(v[2:6]).String()
		case t == 127 && n >= 4:
			val = fmt.Sprintf("OUI %02x:%02x:%02x, subtype %d", v[0], v[1], v[2], v[3])
		case t == 4 || t == 5 || t == 6:
			val = emuPrintable(v)
		default:
			val = fmt.Sprintf("%d bytes", n)
		}
		x.Kids = append(x.Kids, fld(name, val, o+i, 2+n))
		if t == 5 {
			x.Value = val
		}
		i += 2 + n
		if t == 0 {
			break
		}
	}
	x.N = i
	return x
}

func emuCaps(c int) string {
	var out []string
	for i, n := range []string{"other", "repeater", "bridge", "WLAN AP", "router", "telephone", "DOCSIS", "station"} {
		if c&(1<<i) != 0 {
			out = append(out, n)
		}
	}
	return orStr(strings.Join(out, ", "), "none")
}

func emuPrintable(b []byte) string {
	s := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return '.'
		}
		return r
	}, string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func emuBridgeID(b []byte) string {
	return fmt.Sprintf("%d / %s", be16(b, 0), emuMAC(b[2:8]))
}

func emuLLCFields(b []byte, o int, dst string) []EmuField {
	if len(b) < 3 {
		return nil
	}
	llc := fld("LLC", fmt.Sprintf("DSAP 0x%02x SSAP 0x%02x", b[0], b[1]), o, 3)
	if b[0] == 0xaa && len(b) >= 8 { // SNAP
		llc.N = 8
		llc.Kids = append(llc.Kids, fld("OUI", fmt.Sprintf("%02x:%02x:%02x", b[3], b[4], b[5]), o+3, 3), fld("PID", fmt.Sprintf("0x%04x", be16(b, 6)), o+6, 2))
		if dst == "00:e0:2b:00:00:00" && b[4] == 0xe0 && b[5] == 0x2b && len(b) >= 24 {
			e := b[8:]
			return []EmuField{llc, fld("EDP", "discovery protocol advertisement", o+8, len(e),
				fld("Version", fmt.Sprint(e[0]), o+8, 1), fld("Length", fmt.Sprint(be16(e, 2)), o+10, 2),
				fld("Sequence", fmt.Sprint(be16(e, 6)), o+14, 2), fld("Device MAC", emuMAC(e[10:16]), o+18, 6))}
		}
		return []EmuField{llc}
	}
	if b[0] == 0x42 && b[1] == 0x42 && len(b) >= 7 && strings.HasPrefix(dst, "01:80:c2:00:00:00") {
		s, so := b[3:], o+3
		kinds := map[byte]string{0: "configuration", 0x80: "topology change", 2: "RST/MST"}
		bp := fld("STP", "BPDU "+orStr(kinds[s[3]], fmt.Sprint(s[3])), so, len(s),
			fld("Protocol", fmt.Sprint(be16(s, 0)), so, 2), fld("Version", map[byte]string{0: "STP", 2: "RSTP", 3: "MSTP"}[s[2]], so+2, 1),
			fld("BPDU type", fmt.Sprintf("0x%02x", s[3]), so+3, 1))
		if len(s) >= 35 {
			bp.N = min(len(s), map[bool]int{true: 36, false: 35}[s[2] >= 2])
			bp.Kids = append(bp.Kids, fld("Flags", fmt.Sprintf("0x%02x", s[4]), so+4, 1),
				fld("Root", emuBridgeID(s[5:13]), so+5, 8), fld("Root path cost", fmt.Sprint(binary.BigEndian.Uint32(s[13:])), so+13, 4),
				fld("Bridge", emuBridgeID(s[17:25]), so+17, 8), fld("Port", fmt.Sprintf("0x%04x", be16(s, 25)), so+25, 2),
				fld("Message age", fmt.Sprintf("%d s", be16(s, 27)/256), so+27, 2), fld("Max age", fmt.Sprintf("%d s", be16(s, 29)/256), so+29, 2),
				fld("Hello time", fmt.Sprintf("%d s", be16(s, 31)/256), so+31, 2), fld("Forward delay", fmt.Sprintf("%d s", be16(s, 33)/256), so+33, 2))
		}
		return []EmuField{fld("LLC", "spanning tree (0x42)", o, 3), bp}
	}
	return []EmuField{llc}
}

func emuLACPState(s byte) string {
	var out []string
	for i, n := range []string{"active", "short timeout", "aggregatable", "in sync", "collecting", "distributing", "defaulted", "expired"} {
		if s&(1<<i) != 0 {
			out = append(out, n)
		}
	}
	return fmt.Sprintf("0x%02x %s", s, strings.Join(out, ", "))
}

func emuLACPFields(b []byte, o int) EmuField {
	x := fld("LACP", "", o, len(b))
	if len(b) < 2 {
		return x
	}
	if b[0] == 2 {
		x.Name, x.Value = "Marker", "LACP marker"
		return x
	}
	x.Kids = append(x.Kids, fld("Subtype", fmt.Sprint(b[0]), o, 1), fld("Version", fmt.Sprint(b[1]), o+1, 1))
	side := func(name string, i int) {
		if i+20 > len(b) {
			return
		}
		v := b[i+2:]
		x.Kids = append(x.Kids, fld(name, fmt.Sprintf("system %s, key %d, port %d", emuMAC(v[2:8]), be16(v, 8), be16(v, 12)), o+i, 20,
			fld("System priority", fmt.Sprint(be16(v, 0)), o+i+2, 2), fld("System", emuMAC(v[2:8]), o+i+4, 6), fld("Key", fmt.Sprint(be16(v, 8)), o+i+10, 2),
			fld("Port priority", fmt.Sprint(be16(v, 10)), o+i+12, 2), fld("Port", fmt.Sprint(be16(v, 12)), o+i+14, 2), fld("State", emuLACPState(v[14]), o+i+16, 1)))
	}
	side("Actor", 2)
	side("Partner", 22)
	if len(x.Kids) > 2 {
		x.Value = x.Kids[2].Value
	}
	x.N = min(len(b), 110)
	return x
}
