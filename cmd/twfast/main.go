//go:build linux

// twfast is Termwright's fast traffic engine: a helper Termwright copies to a KVM host next to tw-traffic.py. It
// takes the test ports' sockets (QEMU's UDP sockets, or a network port of the host by AF_PACKET), sends the streams
// from the frames tw-traffic.py made for them (patching each frame's counting signature and the checksums over it)
// and counts what arrives, in batches. Every other frame - ARP, DHCP, LACP, 802.1X, TCP sessions, pcap replays -
// goes to and from tw-traffic.py over a local UDP socket, so the protocols stay in one place.
//
// It reads traffic.json (as tw-traffic.py does), writes its port to fast.port and its counters to fast-stats.json
// (the same keys as traffic-stats.json, so Termwright adds them up), and ends when traffic.json is gone.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

var magic = []byte("TWTG")

type cfgTester struct {
	ID     string   `json:"id"`
	MAC    string   `json:"mac"`
	Listen int      `json:"listen"`
	Send   int      `json:"send"`
	NIC    string   `json:"nic"`
	Lag    string   `json:"lag"`
	Groups []string `json:"groups"`
}

type cfgStream struct {
	ID    string `json:"id"`
	From  string `json:"from"`
	To    string `json:"to"`
	Kind  string `json:"kind"`
	DstIP string `json:"dstIp"`
	RTT   bool   `json:"rtt"`
	Mods  []any  `json:"mods"`
}

type config struct {
	Run     uint32      `json:"run"`
	Fast    bool        `json:"fast"`
	Testers []cfgTester `json:"testers"`
	Streams []cfgStream `json:"streams"`
}

// port is a test port's way to the wire.
type port struct {
	id     string
	mac    []byte
	lag    string
	groups map[string]bool
	udp    *net.UDPConn
	pc     *ipv4.PacketConn
	to     *net.UDPAddr
	fd     int // AF_PACKET
	closed atomic.Bool
}

func (p *port) send(frames [][]byte) int {
	if p.closed.Load() {
		return 0
	}
	if p.pc != nil {
		ms := make([]ipv4.Message, len(frames))
		for i, f := range frames {
			ms[i] = ipv4.Message{Buffers: [][]byte{f}, Addr: p.to}
		}
		n := 0
		for n < len(ms) {
			k, err := p.pc.WriteBatch(ms[n:], 0)
			if err != nil || k == 0 {
				return n
			}
			n += k
		}
		return n
	}
	n := 0
	for _, f := range frames {
		if _, err := unix.Write(p.fd, f); err == nil {
			n++
		}
	}
	return n
}

func (p *port) close() {
	p.closed.Store(true)
	if p.udp != nil {
		_ = p.udp.Close()
	}
	if p.fd > 0 {
		_ = unix.Close(p.fd)
	}
}

func htons(v uint16) uint16 { return v<<8 | v>>8 }

func openPort(t cfgTester) (*port, error) {
	mac, _ := net.ParseMAC(t.MAC)
	p := &port{id: t.ID, mac: mac, lag: t.Lag, groups: map[string]bool{}}
	for _, g := range t.Groups {
		if ip := net.ParseIP(g); ip != nil {
			p.groups[ip.String()] = true
		}
	}
	if t.NIC != "" {
		fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(unix.ETH_P_ALL)))
		if err != nil {
			return nil, fmt.Errorf("a raw socket on %s (it needs root): %w", t.NIC, err)
		}
		ifi, err := net.InterfaceByName(t.NIC)
		if err != nil {
			unix.Close(fd)
			return nil, err
		}
		if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: htons(unix.ETH_P_ALL), Ifindex: ifi.Index}); err != nil {
			unix.Close(fd)
			return nil, err
		}
		_ = unix.SetsockoptPacketMreq(fd, unix.SOL_PACKET, unix.PACKET_ADD_MEMBERSHIP, &unix.PacketMreq{Ifindex: int32(ifi.Index), Type: unix.PACKET_MR_PROMISC})
		_ = unix.SetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_AUXDATA, 1)
		_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 16<<20)
		p.fd = fd
		return p, nil
	}
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: t.Listen})
	if err != nil {
		return nil, err
	}
	_ = c.SetReadBuffer(16 << 20)
	_ = c.SetWriteBuffer(8 << 20)
	p.udp, p.pc, p.to = c, ipv4.NewPacketConn(c), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: t.Send}
	return p, nil
}

// recv reads frames until the port closes, handing each to fn (a VLAN tag a network card took off put back).
func (p *port) recv(fn func(p *port, f []byte)) {
	if p.pc != nil {
		ms := make([]ipv4.Message, 64)
		for i := range ms {
			ms[i].Buffers = [][]byte{make([]byte, 10000)}
		}
		for !p.closed.Load() {
			n, err := p.pc.ReadBatch(ms, 0)
			if err != nil {
				return
			}
			for i := 0; i < n; i++ {
				fn(p, ms[i].Buffers[0][:ms[i].N])
			}
		}
		return
	}
	buf, oob := make([]byte, 10000), make([]byte, 256)
	for !p.closed.Load() {
		n, oobn, _, from, err := unix.Recvmsg(p.fd, buf[4:], oob, 0)
		if err != nil {
			if err == unix.EINTR || err == unix.EAGAIN {
				continue
			}
			return
		}
		if ll, ok := from.(*unix.SockaddrLinklayer); ok && ll.Pkttype == unix.PACKET_OUTGOING {
			continue
		}
		f := buf[4 : 4+n]
		if cms, err := unix.ParseSocketControlMessage(oob[:oobn]); err == nil {
			for _, cm := range cms {
				if cm.Header.Level == unix.SOL_PACKET && cm.Header.Type == unix.PACKET_AUXDATA && len(cm.Data) >= 20 {
					st := binary.NativeEndian.Uint32(cm.Data[0:])
					tci, tpid := binary.NativeEndian.Uint16(cm.Data[16:]), binary.NativeEndian.Uint16(cm.Data[18:])
					if st&unix.TP_STATUS_VLAN_VALID != 0 && n >= 12 {
						if st&unix.TP_STATUS_VLAN_TPID_VALID == 0 || tpid == 0 {
							tpid = 0x8100
						}
						copy(buf[0:12], buf[4:16]) // the MACs 4 bytes earlier, the tag after them
						binary.BigEndian.PutUint16(buf[12:], tpid)
						binary.BigEndian.PutUint16(buf[14:], tci)
						f = buf[:n+4]
					}
				}
			}
		}
		fn(p, f)
	}
}

// ---- the counters, as tw-traffic.py keeps them ----

type seenState struct {
	hi      int64
	recent  map[uint32]bool
	last    float64
	hasLast bool
}

type flowRX struct{ rx, sum, max, n float64 }

type sstat struct {
	tx, rx, other, ooo, dup int
	lat                     [4]float64 // sum, max, n, min
	hist                    map[string]int
	jit                     [2]float64
	frx                     map[int]*flowRX
	ftx                     map[int]int
	qos                     map[string]int
	busy                    bool
	err                     string
	fsent, fgot             string
}

type pstat struct {
	tx, rx, txb, rxb, err int
	srx                   map[string]int
	lat                   [3]float64 // sum, n, max
}

type engine struct {
	dir     string
	mu      sync.Mutex
	cfg     config
	ports   map[string]*port
	streams map[string]*sstat
	pstats  map[string]*pstat
	seen    map[string]*seenState
	senders map[int]*sender
	relay   *net.UDPConn
	py      atomic.Pointer[net.UDPAddr]
	gen     uint64 // bumped on every new run: senders of an older one stop
	txAll   atomic.Int64
	fps     float64
	errs    map[string]string
}

func (e *engine) st(id string) *sstat {
	s := e.streams[id]
	if s == nil {
		s = &sstat{hist: map[string]int{}, frx: map[int]*flowRX{}, ftx: map[int]int{}, qos: map[string]int{}}
		e.streams[id] = s
	}
	return s
}

func (e *engine) ps(id string) *pstat {
	p := e.pstats[id]
	if p == nil {
		p = &pstat{srx: map[string]int{}}
		e.pstats[id] = p
	}
	return p
}

func (e *engine) resetCounts() {
	e.streams, e.pstats, e.seen = map[string]*sstat{}, map[string]*pstat{}, map[string]*seenState{}
}

// ---- receiving ----

func now() float64 { return float64(time.Now().UnixNano()) / 1e9 }

func (e *engine) got(p *port, f []byte) {
	if len(f) < 14 {
		return
	}
	i := bytes.LastIndex(f, magic)
	e.mu.Lock()
	pc := e.ps(p.id)
	pc.rx++
	pc.rxb += len(f) + 4
	if i < 0 || len(f) < i+24 {
		e.mu.Unlock()
		e.toPy(p.id, f)
		return
	}
	g := f[i+4:]
	idx, run, seq := int(binary.BigEndian.Uint16(g)), binary.BigEndian.Uint32(g[2:]), binary.BigEndian.Uint32(g[6:])
	ts, flow := math.Float64frombits(binary.BigEndian.Uint64(g[10:])), int(binary.BigEndian.Uint16(g[18:]))
	if run != e.cfg.Run || idx >= len(e.cfg.Streams) {
		e.mu.Unlock()
		e.toPy(p.id, f)
		return
	}
	x := e.cfg.Streams[idx]
	s := e.st(x.ID)
	_, local := e.ports[x.From]
	echo := x.From == p.id && x.To == "" && x.Kind == "icmp"
	toLag := func() bool {
		for _, t := range e.cfg.Testers {
			if t.ID == x.To {
				return p.lag != "" && t.Lag == p.lag
			}
		}
		return false
	}
	switch {
	case x.RTT && x.From == p.id && x.To != "": // its own frame, back: the round trip
		k := x.ID + "|rtt"
		sn := e.seen[k]
		if sn == nil {
			sn = &seenState{hi: -1, recent: map[uint32]bool{}}
			e.seen[k] = sn
		}
		e.latency(s, x.ID, flow, ts, sn, nil)
	case x.To != "" && x.To != p.id && !toLag():
		s.other++
	case x.From != p.id || echo:
		dst := net.ParseIP(x.DstIP)
		if dst != nil && dst.IsMulticast() && len(x.Mods) == 0 && !p.groups[dst.String()] {
			s.other++
			break
		}
		if s.fgot == "" {
			s.fgot = sample(f)
		}
		rtt := x.RTT && (x.To == p.id || p.lag != "" && toLag())
		vlan, prio, dscp := marks(f)
		e.count(s, x.ID, p, seq, ts, flow, local && !rtt, fmt.Sprintf("%d/%d/%d", vlan, prio, dscp))
		if rtt {
			e.mu.Unlock()
			reflect(p, f)
			return
		}
	}
	e.mu.Unlock()
}

func sample(f []byte) string {
	n := min(len(f), 160)
	return strconv.Itoa(len(f)) + ":" + fmt.Sprintf("%x", f[:n])
}

func marks(f []byte) (vlan, prio, dscp int) {
	o := 12
	if binary.BigEndian.Uint16(f[o:]) == 0x8100 && len(f) >= 18 {
		tci := binary.BigEndian.Uint16(f[14:])
		vlan, prio = int(tci&0xFFF), int(tci>>13)
		o += 4
	}
	et, b := binary.BigEndian.Uint16(f[o:]), f[o+2:]
	dscp = -1
	switch {
	case et == 0x0800 && len(b) > 1:
		dscp = int(b[1] >> 2)
	case et == 0x86DD && len(b) > 1:
		dscp = int(((b[0]&15)<<4 | b[1]>>4) >> 2)
	}
	return
}

func (e *engine) count(s *sstat, sid string, p *port, seq uint32, ts float64, flow int, timed bool, mark string) {
	k := sid + "|" + p.id
	sn := e.seen[k]
	if sn == nil {
		sn = &seenState{hi: -1, recent: map[uint32]bool{}}
		e.seen[k] = sn
	}
	pc := e.ps(p.id)
	if sn.recent[seq] {
		s.dup++
		pc.err++
		return
	}
	s.rx++
	pc.srx[sid]++
	sn.recent[seq] = true
	if len(sn.recent) > 8192 {
		for q := range sn.recent {
			if int64(q) < sn.hi-4096 {
				delete(sn.recent, q)
			}
		}
	}
	if int64(seq) < sn.hi {
		s.ooo++
		pc.err++
	} else {
		sn.hi = int64(seq)
	}
	s.qos[mark]++
	if flow != 0xFFFF {
		fr := s.frx[flow]
		if fr == nil {
			fr = &flowRX{}
			s.frx[flow] = fr
		}
		fr.rx++
	}
	if ts > 0 && timed {
		dl := e.latency(s, sid, flow, ts, sn, nil)
		pc.lat[0] += dl
		pc.lat[1]++
		pc.lat[2] = max(pc.lat[2], dl)
	}
}

func (e *engine) latency(s *sstat, sid string, flow int, ts float64, sn *seenState, _ any) float64 {
	dl := (now() - ts) * 1000
	if s.lat[2] == 0 {
		s.lat[3] = 1e9
	}
	s.lat[0] += dl
	s.lat[1] = max(s.lat[1], dl)
	s.lat[2]++
	s.lat[3] = min(s.lat[3], dl)
	b := int(4 * math.Log2(max(dl*1000, 1)))
	s.hist[strconv.Itoa(min(120, max(0, b)))]++
	if flow != 0xFFFF {
		if fr := s.frx[flow]; fr != nil {
			fr.sum += dl
			fr.max = max(fr.max, dl)
			fr.n++
		}
	}
	if sn.hasLast {
		s.jit[0] += math.Abs(dl - sn.last)
		s.jit[1]++
	}
	sn.last, sn.hasLast = dl, true
	return dl
}

// reflect sends a round-trip stream's frame back: MACs, addresses and ports swapped (the checksums stay right).
func reflect(p *port, f []byte) {
	b := make([]byte, 0, len(f))
	b = append(append(append(b, f[6:12]...), p.mac...), f[12:]...)
	o := 12
	for binary.BigEndian.Uint16(b[o:]) == 0x8100 || binary.BigEndian.Uint16(b[o:]) == 0x88A8 {
		o += 4
	}
	et, i := binary.BigEndian.Uint16(b[o:]), o+2
	l4, proto := 0, byte(0)
	switch {
	case et == 0x0800 && len(b) >= i+20:
		ihl := int(b[i]&15) * 4
		var t [4]byte
		copy(t[:], b[i+12:i+16])
		copy(b[i+12:i+16], b[i+16:i+20])
		copy(b[i+16:i+20], t[:])
		l4, proto = i+ihl, b[i+9]
	case et == 0x86DD && len(b) >= i+40:
		var t [16]byte
		copy(t[:], b[i+8:i+24])
		copy(b[i+8:i+24], b[i+24:i+40])
		copy(b[i+24:i+40], t[:])
		l4, proto = i+40, b[i+6]
	}
	if (proto == 6 || proto == 17) && len(b) >= l4+4 {
		var t [2]byte
		copy(t[:], b[l4:l4+2])
		copy(b[l4:l4+2], b[l4+2:l4+4])
		copy(b[l4+2:l4+4], t[:])
	}
	p.send([][]byte{b})
}

// ---- tw-traffic.py: frames to and from it ----

func (e *engine) toPy(id string, f []byte) {
	a := e.py.Load()
	if a == nil {
		return
	}
	m := make([]byte, 0, len(f)+2+len(id))
	m = append(append(append(m, 'R', byte(len(id))), id...), f...)
	_, _ = e.relay.WriteToUDP(m, a)
}

func (e *engine) fromPy() {
	buf := make([]byte, 70000)
	for {
		n, a, err := e.relay.ReadFromUDP(buf)
		if err != nil {
			return
		}
		e.py.Store(a)
		m := buf[:n]
		switch {
		case n >= 2 && m[0] == 'F' && n >= 2+int(m[1]):
			id := string(m[2 : 2+int(m[1])])
			e.mu.Lock()
			p := e.ports[id]
			if p != nil {
				pc := e.ps(id)
				pc.tx++
				pc.txb += n - 2 - len(id) + 4
			}
			e.mu.Unlock()
			if p != nil {
				p.send([][]byte{append([]byte(nil), m[2+len(id):]...)})
			}
		case n >= 16 && m[0] == 'S':
			idx := int(binary.BigEndian.Uint16(m[1:]))
			count, pps, burst := int(binary.BigEndian.Uint32(m[3:])), int(binary.BigEndian.Uint32(m[7:])), int(binary.BigEndian.Uint32(m[11:]))
			e.start(idx, count, pps, burst, string(m[16:]))
		}
	}
}

// ---- sending ----

type tpl struct {
	f     []byte
	sig   int
	spots [][2]int // checksum offset, where its sum starts (innermost last)
}

type sender struct {
	idx            int
	count, pps, bu int
	tpls           []tpl
	p              *port
	gen            uint64
	stop           atomic.Bool
}

func readTpls(path string) ([]tpl, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []tpl
	for len(b) >= 5 {
		n, sig, k := int(binary.BigEndian.Uint16(b)), int(binary.BigEndian.Uint16(b[2:])), int(b[4])
		b = b[5:]
		if len(b) < 4*k+n {
			return nil, fmt.Errorf("%s is cut short", filepath.Base(path))
		}
		t := tpl{sig: sig}
		if sig == 0xFFFF {
			t.sig = -1
		}
		for j := 0; j < k; j++ {
			t.spots = append(t.spots, [2]int{int(binary.BigEndian.Uint16(b[4*j:])), int(binary.BigEndian.Uint16(b[4*j+2:]))})
		}
		t.f = append([]byte(nil), b[4*k:4*k+n]...)
		b = b[4*k+n:]
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s has no frames", filepath.Base(path))
	}
	return out, nil
}

func (e *engine) start(idx, count, pps, burst int, path string) {
	tpls, err := readTpls(path)
	_ = os.Remove(path)
	e.mu.Lock()
	defer e.mu.Unlock()
	if idx >= len(e.cfg.Streams) {
		return
	}
	x := e.cfg.Streams[idx]
	s := e.st(x.ID)
	if err != nil {
		s.err = "the fast engine: " + err.Error()
		return
	}
	p := e.ports[x.From]
	if p == nil {
		return
	}
	if old := e.senders[idx]; old != nil {
		old.stop.Store(true)
	}
	sd := &sender{idx: idx, count: count, pps: max(1, pps), bu: max(1, burst), tpls: tpls, p: p, gen: e.gen}
	e.senders[idx] = sd
	s.busy = true
	go e.send(sd, x.ID)
}

// sum16 adds b as 16-bit words, starting on a word's high byte (odd: on its low byte), without the final folding.
func sum16(b []byte, odd bool) uint32 {
	var s uint32
	i := 0
	if odd && len(b) > 0 {
		s += uint32(b[0])
		i = 1
	}
	for ; i+1 < len(b); i += 2 {
		s += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if i < len(b) {
		s += uint32(b[i]) << 8
	}
	return s
}

func fold(s uint32) uint16 {
	for s>>16 != 0 {
		s = s&0xFFFF + s>>16
	}
	return uint16(s)
}

// fix updates the checksums over the changed bytes (RFC 1624), innermost first: an outer one covers the inner's
// checksum field too.
func fix(old, f []byte, t *tpl, changed [][2]int) {
	for j := len(t.spots) - 1; j >= 0; j-- {
		co, cov := t.spots[j][0], t.spots[j][1]
		if co+2 > len(f) {
			continue
		}
		hc := binary.BigEndian.Uint16(f[co:])
		var oldS, newS uint32
		for _, r := range changed {
			if r[0] < cov || r[0] == co {
				continue
			}
			odd := (r[0]-cov)%2 == 1
			oldS += sum16(old[r[0]:r[1]], odd)
			newS += sum16(f[r[0]:r[1]], odd)
		}
		if oldS == newS {
			continue
		}
		x := uint32(^hc) + uint32(^fold(oldS)) + uint32(fold(newS))
		nc := ^fold(x)
		if nc == 0 { // (the same in ones' complement; for UDP 0 would mean no checksum)
			nc = 0xFFFF
		}
		binary.BigEndian.PutUint16(f[co:], nc)
		changed = append(changed, [2]int{co, co + 2})
	}
}

func (e *engine) send(sd *sender, sid string) {
	start := time.Now()
	sent := 0
	const batch = 64
	bufs := make([][]byte, batch)
	for sent < sd.count && !sd.stop.Load() {
		el := time.Since(start).Seconds()
		due := min(sd.count, int(el*float64(sd.pps))+1)
		if sd.bu > 1 {
			due = min(sd.count, (due+sd.bu-1)/sd.bu*sd.bu)
		}
		if due <= sent {
			wait := time.Duration((float64(sent)/float64(sd.pps) - el) * 1e9)
			time.Sleep(min(max(wait, 20*time.Microsecond), 2*time.Millisecond))
			continue
		}
		for sent < due && !sd.stop.Load() {
			k := min(batch, due-sent)
			ts := now()
			flows := map[int]int{}
			for j := 0; j < k; j++ {
				t := &sd.tpls[(sent+j)%len(sd.tpls)]
				f := append(bufs[j][:0], t.f...)
				if t.sig >= 0 && t.sig+24 <= len(f) {
					g := f[t.sig+4:]
					binary.BigEndian.PutUint32(g[6:], uint32(sent+j))
					binary.BigEndian.PutUint64(g[10:], math.Float64bits(ts))
					fix(t.f, f, t, [][2]int{{t.sig + 10, t.sig + 22}})
					if fl := int(binary.BigEndian.Uint16(g[18:])); fl != 0xFFFF {
						flows[fl]++
					}
				}
				bufs[j] = f
			}
			n := sd.p.send(bufs[:k])
			e.mu.Lock()
			if sd.gen == e.gen {
				s := e.st(sid)
				s.tx += k
				if s.fsent == "" && k > 0 {
					s.fsent = sample(bufs[0])
				}
				for fl, c := range flows {
					s.ftx[fl] += c
				}
				pc := e.ps(sd.p.id)
				pc.tx += n
				pc.err += k - n
				for j := 0; j < n; j++ {
					pc.txb += len(bufs[j]) + 4
				}
			}
			e.mu.Unlock()
			e.txAll.Add(int64(n))
			sent += k
		}
	}
	e.mu.Lock()
	if sd.gen == e.gen {
		e.st(sid).busy = false
	}
	if e.senders[sd.idx] == sd {
		delete(e.senders, sd.idx)
	}
	e.mu.Unlock()
}

// ---- traffic.json and fast-stats.json ----

func (e *engine) load() bool {
	b, err := os.ReadFile(filepath.Join(e.dir, "traffic.json"))
	if err != nil {
		return false
	}
	var c config
	if json.Unmarshal(b, &c) != nil {
		return true
	}
	if len(c.Testers) == 0 || !c.Fast {
		return false
	}
	e.mu.Lock()
	newRun := c.Run != e.cfg.Run
	want := map[string]cfgTester{}
	for _, t := range c.Testers {
		want[t.ID] = t
	}
	for id, p := range e.ports {
		w, ok := want[id]
		old := e.find(id)
		if !ok || old == nil || w.Listen != old.Listen || w.Send != old.Send || w.NIC != old.NIC {
			p.close()
			delete(e.ports, id)
		}
	}
	var opened []*port
	for _, t := range c.Testers {
		if p, ok := e.ports[t.ID]; ok {
			mac, _ := net.ParseMAC(t.MAC)
			p.mac, p.lag, p.groups = mac, t.Lag, map[string]bool{}
			for _, g := range t.Groups {
				if ip := net.ParseIP(g); ip != nil {
					p.groups[ip.String()] = true
				}
			}
			continue
		}
		p, err := openPort(t)
		if err != nil {
			e.errs["port "+t.ID] = err.Error()
			continue
		}
		delete(e.errs, "port "+t.ID)
		e.ports[t.ID] = p
		opened = append(opened, p)
	}
	e.cfg = c
	if newRun {
		e.gen++
		for _, sd := range e.senders {
			sd.stop.Store(true)
		}
		e.senders = map[int]*sender{}
		if c.Run != 0 {
			e.resetCounts()
		}
	}
	e.mu.Unlock()
	for _, p := range opened {
		go p.recv(e.got)
	}
	return true
}

func (e *engine) find(id string) *cfgTester {
	for i := range e.cfg.Testers {
		if e.cfg.Testers[i].ID == id {
			return &e.cfg.Testers[i]
		}
	}
	return nil
}

func (e *engine) write() {
	e.mu.Lock()
	o := map[string]any{"run": e.cfg.Run, "at": now(), "engine": "fast", "fps": math.Round(e.fps)}
	tx, rx, other, ooo, dup, busy, errs := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}, map[string]bool{}, map[string]string{}
	lat, jit, hist, frx, ftx, qos := map[string][]float64{}, map[string][]float64{}, map[string]map[string]int{}, map[string]map[string][]float64{}, map[string]map[string]int{}, map[string]map[string]int{}
	fsent, fgot := map[string]string{}, map[string]string{}
	for id, s := range e.streams {
		tx[id], rx[id], busy[id] = s.tx, s.rx, s.busy
		if s.other > 0 {
			other[id] = s.other
		}
		if s.ooo > 0 {
			ooo[id] = s.ooo
		}
		if s.dup > 0 {
			dup[id] = s.dup
		}
		if s.err != "" {
			errs[id] = s.err
		}
		if s.lat[2] > 0 {
			lat[id] = s.lat[:]
		}
		if s.jit[1] > 0 {
			jit[id] = s.jit[:]
		}
		if len(s.hist) > 0 {
			hist[id] = s.hist
		}
		if len(s.frx) > 0 {
			m := map[string][]float64{}
			for k, f := range s.frx {
				m[strconv.Itoa(k)] = []float64{f.rx, f.sum, f.max, f.n}
			}
			frx[id] = m
		}
		if len(s.ftx) > 0 {
			m := map[string]int{}
			for k, n := range s.ftx {
				m[strconv.Itoa(k)] = n
			}
			ftx[id] = m
		}
		if len(s.qos) > 0 {
			qos[id] = s.qos
		}
		if s.fsent != "" {
			fsent[id] = s.fsent
		}
		if s.fgot != "" {
			fgot[id] = s.fgot
		}
	}
	for k, v := range e.errs {
		errs[k] = v
	}
	ports := map[string]any{}
	for id, p := range e.pstats {
		ports[id] = map[string]any{"tx": p.tx, "rx": p.rx, "txb": p.txb, "rxb": p.rxb, "srx": p.srx, "lat": p.lat[:], "err": p.err}
	}
	o["tx"], o["rx"], o["other"], o["ooo"], o["dup"], o["busy"], o["err"] = tx, rx, other, ooo, dup, busy, errs
	o["lat"], o["jit"], o["hist"], o["frx"], o["ftx"], o["qos"], o["ports"], o["fsent"], o["fgot"] = lat, jit, hist, frx, ftx, qos, ports, fsent, fgot
	b, _ := json.Marshal(o)
	e.mu.Unlock()
	p := filepath.Join(e.dir, "fast-stats.json")
	if os.WriteFile(p+".tmp", b, 0o644) == nil {
		_ = os.Rename(p+".tmp", p)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: tw-fast <lab folder>")
		os.Exit(2)
	}
	if len(os.Args) > 2 && os.Args[2] == "-version" {
		fmt.Println("tw-fast 1")
		return
	}
	e := &engine{dir: os.Args[1], ports: map[string]*port{}, senders: map[int]*sender{}, errs: map[string]string{}}
	e.resetCounts()
	r, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = r.SetReadBuffer(16 << 20)
	e.relay = r
	if !e.load() {
		return
	}
	go e.fromPy()
	pf := filepath.Join(e.dir, "fast.port")
	_ = os.WriteFile(pf+".tmp", []byte(strconv.Itoa(r.LocalAddr().(*net.UDPAddr).Port)), 0o644)
	_ = os.Rename(pf+".tmp", pf)
	defer os.Remove(pf)
	var m0 time.Time
	lastTx, lastAt := int64(0), time.Now()
	for range time.Tick(250 * time.Millisecond) {
		fi, err := os.Stat(filepath.Join(e.dir, "traffic.json"))
		if err != nil {
			break
		}
		if fi.ModTime() != m0 {
			m0 = fi.ModTime()
			if !e.load() {
				break
			}
		}
		if el := time.Since(lastAt).Seconds(); el >= 1 {
			t := e.txAll.Load()
			e.fps = max(e.fps, float64(t-lastTx)/el)
			lastTx, lastAt = t, time.Now()
		}
		e.write()
	}
	e.mu.Lock()
	for _, p := range e.ports {
		p.close()
	}
	e.mu.Unlock()
}
