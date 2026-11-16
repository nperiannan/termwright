package main

import (
	"cmp"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// The traffic generator: test ports on free switch ports and streams of
// frames between them, sent and counted by traffic.py on each host.

// EmuStreamResult is a stream's counters (all hosts added up) and its verdict.
type EmuStreamResult struct {
	ID       string            `json:"id"`                   // the stream
	TX       int               `json:"tx"`                   // frames sent
	RX       int               `json:"rx"`                   // frames received at the test ports it is for (a duplicate counts once)
	Expected int               `json:"expected"`             // frames that should arrive (to every other test port for a broadcast)
	Other    int               `json:"other"`                // arrived at a test port it was not for (flooded)
	Loss     float64           `json:"loss"`                 // %
	LatAvg   float64           `json:"latAvg"`               // ms (-1 = not measured: the ends on different hosts)
	LatMax   float64           `json:"latMax"`               // ms
	LatMin   float64           `json:"latMin,omitempty"`     // ms
	Jitter   float64           `json:"jitter,omitempty"`     // ms, the mean change of latency between frames
	OOO      int               `json:"ooo,omitempty"`        // frames that arrived after a later one
	Dup      int               `json:"dup,omitempty"`        // frames that arrived twice (counted once)
	TxRate   float64           `json:"txRate"`               // frames per second, now
	RxRate   float64           `json:"rxRate"`               // frames per second, now
	Running  bool              `json:"running"`              // it is still sending
	Result   string            `json:"result"`               // "", pass, fail
	Why      string            `json:"why,omitempty"`        // why it failed
	P50      float64           `json:"p50,omitempty"`        // latency percentiles, ms (from a histogram of quarter octaves: within 10 %)
	P95      float64           `json:"p95,omitempty"`        // ms: 95 % of the frames were faster
	P99      float64           `json:"p99,omitempty"`        // ms: 99 % of the frames were faster
	Hist     []EmuLatBin       `json:"hist,omitempty"`       // the latency histogram, an octave a bar
	Flows    []EmuFlowResult   `json:"flows,omitempty"`      // a stream with changing fields: its flows, the most lossy first (at most 32)
	FlowsN   int               `json:"flowsN,omitempty"`     // how many flows it has sent
	FlowsBad int               `json:"flowsLossy,omitempty"` // how many of them lost frames
	Marks    []EmuMark         `json:"marks,omitempty"`      // the VLAN, priority and DSCP its frames arrived with
	TxMbps   float64           `json:"txMbps,omitempty"`     // Mbit/s on the wire, now
	RxMbps   float64           `json:"rxMbps,omitempty"`     // Mbit/s on the wire received, now
	Sent     string            `json:"sentFrame,omitempty"`  // its first frame sent: its length, then up to 160 bytes in hex ("508:0004…"; no FCS)
	Got      string            `json:"gotFrame,omitempty"`   // the first of its frames that arrived where it goes, the same way
	Sessions *EmuSessionResult `json:"sessions,omitempty"`   // a session stream: its TCP sessions (sent: tried, received: connected, latency: setup time)
}

// EmuSessionResult is a session stream's TCP sessions.
type EmuSessionResult struct {
	Tried    int     `json:"tried"`              // opened (SYN sent)
	OK       int     `json:"ok"`                 // connected (the SYN-ACK came back)
	Refused  int     `json:"refused"`            // answered with a reset
	Timeout  int     `json:"timeout"`            // no answer, or not finished in time
	Reset    int     `json:"reset,omitempty"`    // reset after they connected
	Done     int     `json:"done"`               // finished: data both ways, then closed
	Open     int     `json:"open,omitempty"`     // open now
	Peak     int     `json:"peak,omitempty"`     // open at once, at most
	SetupAvg float64 `json:"setupAvg,omitempty"` // setup time (SYN to SYN-ACK), ms
	SetupP95 float64 `json:"setupP95,omitempty"` // ms, 95th percentile
	SetupMax float64 `json:"setupMax,omitempty"` // ms
	CPS      float64 `json:"cps,omitempty"`      // connected a second, over the run
	Bytes    int64   `json:"bytes,omitempty"`    // data bytes, both ways
	Secs     float64 `json:"secs,omitempty"`     // how long it has run, s
}

// emuSessCount is a session stream's sessions as traffic.py writes them.
type emuSessCount = EmuSessionResult

// EmuPortResult is a test port's statistics over the run: every frame it sent and got (the protocols' too), its
// streams' frames that were for it, their loss and latency, and its rates now.
type EmuPortResult struct {
	ID       string         `json:"id"`                // the test port
	TX       int            `json:"tx"`                // frames sent
	RX       int            `json:"rx"`                // frames received
	TxBytes  int            `json:"txBytes"`           // bytes sent (with the FCS)
	RxBytes  int            `json:"rxBytes"`           // bytes received
	TxRate   float64        `json:"txRate"`            // frames per second, now
	RxRate   float64        `json:"rxRate"`            // frames per second, now
	TxMbps   float64        `json:"txMbps"`            // Mbit/s on the wire, now (with the preamble and gap)
	RxMbps   float64        `json:"rxMbps"`            // Mbit/s on the wire, now
	Expected int            `json:"expected"`          // its streams' frames that should have arrived here
	Got      int            `json:"got"`               // of those, how many did
	Loss     float64        `json:"loss"`              // %
	LatAvg   float64        `json:"latAvg"`            // ms (-1 = not measured)
	LatMax   float64        `json:"latMax"`            // ms
	Errors   int            `json:"errors"`            // frames it could not send, out of order or twice
	Streams  map[string]int `json:"streams,omitempty"` // per stream: its frames that arrived here
}

// emuPortCount is a test port's counters as traffic.py writes them.
type emuPortCount struct {
	TX  int            `json:"tx"`
	RX  int            `json:"rx"`
	TXB int            `json:"txb"`
	RXB int            `json:"rxb"`
	SRX map[string]int `json:"srx"`
	Lat [3]float64     `json:"lat"` // sum, count, max (ms)
	Err int            `json:"err"`
}

// EmuTrafficView is the Traffic tab.
type EmuTrafficView struct {
	Testers []emu.Tester      `json:"testers"`           // the test ports
	Streams []emu.Stream      `json:"streams"`           // the streams
	Results []EmuStreamResult `json:"results"`           // per stream: its counters and verdict (the last run's, or the running one's)
	Running bool              `json:"running"`           // a run is going on
	Started string            `json:"started,omitempty"` // when the run started (RFC 3339)
	Note    string            `json:"note,omitempty"`    // something to do (restart a switch for its test port)
	// History: per stream, the run so far ([seconds since the start, tx/s, rx/s, loss % so far, average latency ms (-1: not measured)]), for the graphs.
	History     map[string][][5]float64 `json:"history,omitempty"`
	Ports       []EmuPortResult         `json:"ports,omitempty"`       // per test port: its statistics over the run
	PortHistory map[string][][3]float64 `json:"portHistory,omitempty"` // per test port: its rates over the last minute ([seconds since the start, tx/s, rx/s])
}

var emuTraf struct {
	run     uint32
	started time.Time
	on      map[string]bool
	last    []EmuStreamResult
	final   bool                  // the last run's counters are final (not read from the hosts again)
	prev    map[string][3]float64 // per stream: when, tx and rx at the last read (for the rates)
	hist    map[string][][5]float64
	ports   []EmuPortResult             // the last run's per test port statistics
	pprev   map[string][3]float64       // per test port: when, frames sent and received at the last read
	phist   map[string][][3]float64     // per test port: its rates over the last minute
	leases  map[string][2]string        // test port → the address it got by DHCP ("ip/len via gw"), by SLAAC
	proto   map[string][2]string        // test port → its LAG as LACP has it, its LLDP neighbour
	clients map[string]*emu.ClientState // test port → how its clients are doing
	stp     map[string]*emu.STPState    // test port → what it hears by spanning tree
	vrrp    map[string]*emu.VRRPState   // test port → what it hears by VRRP
	bfd     map[string]*emu.BFDState    // test port → its BFD session
	engNote map[string]string           // host → why the fast engine does not run there ("" = it does)
	engFPS  map[string]float64          // host → the highest rate the fast engine sent there (frames/s)
	engOn   map[string]bool             // host → its fast engine reported in the last read
	engArch map[string]string           // host → its CPU (uname -m)
	trex    map[string]*emuTrexRun      // TRex server → its connection and the run's streams there
	// per switch port of a test port (vm:port): where its QEMU really sends, read from its monitor once per
	// switch start (0 = where a port without a cable sends; -1 = nowhere the generator can listen)
	sock map[string]int
}

func emuTesterName(l *emu.Lab) string {
	for n := 1; ; n++ {
		name := fmt.Sprintf("Test port %d", n)
		if !slices.ContainsFunc(l.Testers, func(t emu.Tester) bool { return t.Name == name }) {
			return name
		}
	}
}

// emuTrafficPush writes traffic.json on every host of the lab that has (or had) test ports (emuLab.mu not held).
func (a *App) emuTrafficPush() error {
	a.emuTesterSockets()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return errors.New("there is no emulated lab")
	}
	type job struct{ host, script string }
	var jobs []job
	var pcaps [][3]string // host, lab folder, capture
	var fast [][2]string  // host, lab folder: the fast engine goes there
	for _, h := range a.emuTrafHosts(l) {
		lh := a.emuFor(h)
		lh.Testers = slices.Clone(lh.Testers)
		for i, t := range lh.Testers {
			if l := emuTraf.leases[t.ID]; t.DHCP && l[0] != "" {
				lh.Testers[i].IP = strings.Split(l[0], " ")[0]
			}
			if l := emuTraf.leases[t.ID]; t.SLAAC && l[1] != "" {
				lh.Testers[i].IP6 = l[1]
			}
			if p := emuTraf.sock[emuPeerKey(t.VM, t.Port)]; p > 0 {
				lh.Testers[i].Listen = p
			}
		}
		cfg, n := lh.TrafficConfig(func(v emu.VM) bool { return emuHostOf(v) == h }, h, emuTraf.run, emuTraf.on)
		if n == 0 && !emuLab.trafHosts[h] {
			continue
		}
		emuLab.trafHosts[h] = n > 0
		nic := slices.ContainsFunc(l.Testers, func(t emu.Tester) bool { return t.NIC != "" && t.Host == h })
		jobs = append(jobs, job{h, lh.TrafficScript(cfg, nic)})
		if l.Engine == "fast" && n > 0 {
			fast = append(fast, [2]string{h, lh.VMDir()})
		}
		for _, s := range l.Streams {
			if s.Kind == "pcap" && s.PcapID != "" && n > 0 {
				pcaps = append(pcaps, [3]string{h, lh.VMDir(), s.PcapID}) // where tw-traffic.py runs
			}
		}
	}
	emuLab.mu.Unlock()
	var errs []string
	for _, f := range fast { // a host it cannot run on sends with the built-in engine (Engines… says why)
		note := ""
		if err := a.emuFastUpload(f[0], f[1]); err != nil {
			note = err.Error()
		}
		emuLab.mu.Lock()
		emuEngInit()
		emuTraf.engNote[f[0]] = note
		emuLab.mu.Unlock()
	}
	for _, p := range pcaps { // the captures the streams replay, on each host with test ports
		if err := a.emuPcapUpload(p[0], p[1], p[2]); err != nil {
			errs = append(errs, p[0][strings.LastIndex(p[0], "/")+1:]+": "+err.Error())
		}
	}
	for _, j := range jobs {
		out, err := a.emuRun(j.host, j.script, 30*time.Second)
		if err != nil || !strings.Contains(out, "traffic-ok") {
			errs = append(errs, j.host[strings.LastIndex(j.host, "/")+1:]+": "+strings.TrimSpace(out+" "+errText(err)))
		}
	}
	if len(errs) > 0 {
		return errors.New("test ports: " + strings.Join(errs, "; "))
	}
	return nil
}

// emuTesterLink turns a test port's switch port link on or off (a running switch; started before
// test ports existed, the port has no socket: it needs a restart).
func (a *App) emuTesterLink(vmID string, port int, up bool) string {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	v := l.VMByID(vmID)
	if v == nil {
		emuLab.mu.Unlock()
		return ""
	}
	host, mon, name := emuHostOf(*v), a.emuFor(emuHostOf(*v)).MonitorPort(*v), v.Name
	emuLab.mu.Unlock()
	h, err := a.emuMonitor(host, mon)
	if err != nil {
		return "" // not running: it gets the link at its start
	}
	defer h.close()
	if up {
		if out, _ := h.do("info network", 10*time.Second); !regexp.MustCompile(fmt.Sprintf(`\bn%d: index=\d+,type=socket`, port)).MatchString(out) {
			return "restart " + name + " once for its test port: it was started before test ports existed"
		}
	}
	// the netdev's name: a port that started with a cable has no nicN (set_link on it reaches the card too)
	_, _ = h.do(fmt.Sprintf("set_link n%d %s", port, map[bool]string{true: "on", false: "off"}[up]), 10*time.Second)
	return ""
}

var emuSockRe = regexp.MustCompile(`\bn(\d+): index=\d+,type=socket,[^\n]*?udp=([0-9.]+):(\d+)`)

// emuTesterSockets reads, once per switch start, where the switch ports of test ports really send. A port
// without a cable when its switch started sends where its test port listens; one that started with a cable
// since removed still sends to that cable's old other end - its test port listens there instead (it got no
// frame from the switch before), unless that end still runs or is on another host: then the switch needs a
// restart, said in the Events and the test port's note (emuLab.mu not held).
func (a *App) emuTesterSockets() {
	type q struct {
		vm, host, name string
		mon            int
		ports          []int
	}
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return
	}
	if emuTraf.sock == nil {
		emuTraf.sock = map[string]int{}
	}
	byVM := map[string]*q{}
	var qs []*q
	for _, t := range l.Testers {
		v := l.VMByID(t.VM)
		if _, run := emuLab.runCables[t.VM]; v == nil || !run {
			continue // not running: it starts with the usual socket
		}
		if _, ok := emuTraf.sock[emuPeerKey(t.VM, t.Port)]; ok {
			continue
		}
		x := byVM[t.VM]
		if x == nil {
			h := emuHostOf(*v)
			x = &q{vm: t.VM, host: h, name: v.Name, mon: a.emuFor(h).MonitorPort(*v)}
			byVM[t.VM] = x
			qs = append(qs, x)
		}
		x.ports = append(x.ports, t.Port)
	}
	emuLab.mu.Unlock()
	for _, x := range qs {
		out := ""
		if h, err := a.emuMonitor(x.host, x.mon); err == nil {
			out, _ = h.do("info network", 10*time.Second)
			h.close()
		}
		got := map[int][2]string{}
		for _, m := range emuSockRe.FindAllStringSubmatch(out, -1) {
			n, _ := strconv.Atoi(m[1])
			got[n] = [2]string{m[2], m[3]}
		}
		emuLab.mu.Lock()
		l := emuLab.f.Lab
		v := l.VMByID(x.vm)
		for _, n := range x.ports {
			key := emuPeerKey(x.vm, n)
			g, ok := got[n]
			if v == nil || !ok {
				emuTraf.sock[key] = 0 // not read (stopped meanwhile, or no socket: emuTesterLink says so)
				continue
			}
			li, _ := l.TesterPorts(*v, n)
			p, _ := strconv.Atoi(g[1])
			if g[0] == "127.0.0.1" && p == li {
				emuTraf.sock[key] = 0
				continue
			}
			why := "a cable to another host"
			if g[0] == "127.0.0.1" {
				why = ""
				for _, o := range l.VMs {
					for k := 1; k <= 64 && why == ""; k++ {
						if _, run := emuLab.runCables[o.ID]; run && o.ID != x.vm && l.UDPPort(o, k) == p {
							why = "its cable to " + o.Name + ", which still runs"
						}
					}
				}
			}
			if why == "" {
				emuTraf.sock[key] = p
				continue
			}
			emuTraf.sock[key] = -1
			emuEv(x.vm, "warn", fmt.Sprintf("the test port on %s %s gets no frames until %s restarts: the port started with %s", x.name, emuImage(v.Image).PortNameOf(n), x.name, why))
		}
		emuLab.mu.Unlock()
	}
}

// EmuTraffic is the Traffic tab: test ports, streams and their counters now.
func (a *App) EmuTraffic() (EmuTrafficView, error) {
	v, err := a.emuTrafficAll()
	if err != nil {
		return v, err
	}
	// a test's trial streams are its own (EmuTestRuns shows the test)
	test := map[string]bool{}
	v.Streams = slices.DeleteFunc(v.Streams, func(s emu.Stream) bool { test[s.ID] = s.Test != ""; return s.Test != "" })
	v.Results = slices.DeleteFunc(v.Results, func(r EmuStreamResult) bool { return test[r.ID] })
	if len(test) > 0 && v.History != nil {
		maps.DeleteFunc(v.History, func(id string, _ [][5]float64) bool { return test[id] })
	}
	return v, nil
}

// emuTrafficAll is EmuTraffic with the test runs' streams.
func (a *App) emuTrafficAll() (EmuTrafficView, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{Testers: []emu.Tester{}, Streams: []emu.Stream{}, Results: []EmuStreamResult{}}, nil
	}
	v := EmuTrafficView{Testers: append([]emu.Tester{}, l.Testers...), Streams: append([]emu.Stream{}, l.Streams...)}
	for i, t := range v.Testers {
		if vm := l.VMByID(t.VM); vm != nil {
			if im := emuImage(vm.Image); im != nil {
				v.Testers[i].PortName = im.PortNameOf(t.Port)
			}
		}
	}
	var hosts []string
	for _, t := range l.Testers {
		if vm := l.VMByID(t.VM); vm != nil && !slices.Contains(hosts, emuHostOf(*vm)) {
			hosts = append(hosts, emuHostOf(*vm))
		}
		if t.NIC != "" && !slices.Contains(hosts, t.Host) {
			hosts = append(hosts, t.Host)
		}
	}
	dir, fast := l.VMDir(), l.Engine == "fast"
	emuEngInit()
	emuTraf.engOn = map[string]bool{}
	trex := slices.ContainsFunc(l.Testers, func(t emu.Tester) bool { return t.Trex != "" })
	run := emuTraf.run
	if run != 0 && emuTraf.final {
		v.Results = emuTrafCached(v.Streams)
		v.Started, v.History = emuTraf.started.Format(time.RFC3339), maps.Clone(emuTraf.hist)
		v.Ports, v.PortHistory = emuTraf.ports, maps.Clone(emuTraf.phist)
		emuLab.mu.Unlock()
		return v, nil
	}
	emuLab.mu.Unlock()
	type stats struct {
		Run   uint32               `json:"run"`
		TX    map[string]int       `json:"tx"`
		RX    map[string]int       `json:"rx"`
		Other map[string]int       `json:"other"`
		Lat   map[string][]float64 `json:"lat"`
		Err   map[string]string    `json:"err"`
		Busy  map[string]bool      `json:"busy"`
		OOO   map[string]int       `json:"ooo"`
		Lease map[string]struct {
			IP, GW, Server string
		} `json:"lease"`
		Lease6 map[string]struct{ IP, GW string } `json:"lease6"`
		Dup    map[string]int                     `json:"dup"`
		LACP   map[string]*struct {
			Up      bool `json:"up"`
			Partner *struct {
				Sys  string `json:"sys"`
				Key  int    `json:"key"`
				Port int    `json:"port"`
			} `json:"partner"`
		} `json:"lacp"`
		LLDP map[string]struct {
			Name, Port, PortDesc, Chassis string
		} `json:"lldp"`
		Clients map[string]*emu.ClientState     `json:"clients"`
		STP     map[string]*emu.STPState        `json:"stp"`
		VRRP    map[string]*emu.VRRPState       `json:"vrrp"`
		BFD     map[string]*emu.BFDState        `json:"bfd"`
		Jit     map[string][]float64            `json:"jit"`
		Hist    map[string]map[string]int       `json:"hist"`
		FRX     map[string]map[string][]float64 `json:"frx"`
		FTX     map[string]map[string]int       `json:"ftx"`
		FLab    map[string]map[string]string    `json:"flab"`
		QoS     map[string]map[string]int       `json:"qos"`
		FSent   map[string]string               `json:"fsent"` // per stream: its first frame sent (hex)
		FGot    map[string]string               `json:"fgot"`  // per stream: the first of its frames that arrived where it goes
		Ports   map[string]emuPortCount         `json:"ports"`
		Sess    map[string]*emuSessCount        `json:"sess"` // per session stream: its sessions
		FPS     float64                         `json:"fps"`  // the fast engine: the highest rate it sent
	}
	sum := stats{TX: map[string]int{}, RX: map[string]int{}, Other: map[string]int{}, Lat: map[string][]float64{}, Err: map[string]string{}, Busy: map[string]bool{}, OOO: map[string]int{}, Dup: map[string]int{}, Jit: map[string][]float64{},
		Hist: map[string]map[string]int{}, FRX: map[string]map[string][]float64{}, FTX: map[string]map[string]int{}, FLab: map[string]map[string]string{}, QoS: map[string]map[string]int{}, FSent: map[string]string{}, FGot: map[string]string{}, Ports: map[string]emuPortCount{}, Sess: map[string]*emuSessCount{}}
	dyn := slices.ContainsFunc(v.Testers, func(t emu.Tester) bool {
		return t.DHCP || t.SLAAC || t.Lag != "" || t.LLDP || t.Clients != nil || t.STP != nil
	}) // (their protocols report while nothing runs)
	if run != 0 || emuTraf.last != nil || dyn {
		type part struct {
			host, js string
			fast     bool
		}
		var parts []part
		for _, h := range hosts { // traffic.py's counters, and the fast engine's
			cmd := "cat " + emu.Q(dir) + "/traffic-stats.json 2>/dev/null"
			if fast {
				cmd += "; echo; echo '#twfast'; cat " + emu.Q(dir) + "/fast-stats.json 2>/dev/null"
			}
			out, err := a.emuRun(h, cmd, 15*time.Second)
			if err != nil {
				continue
			}
			py, fa, _ := strings.Cut(out, "#twfast")
			parts = append(parts, part{h, py, false})
			if fast {
				parts = append(parts, part{h, fa, true})
			}
		}
		if trex && run != 0 { // the TRex ports' streams
			if js := a.emuTrexStats(run); js != "" {
				parts = append(parts, part{"", js, false})
			}
		}
		for _, p := range parts {
			var st stats
			if json.Unmarshal([]byte(strings.TrimSpace(p.js)), &st) != nil || st.Run != run && run != 0 {
				continue
			}
			if p.fast {
				emuLab.mu.Lock()
				emuTraf.engOn[p.host] = true
				emuTraf.engFPS[p.host] = max(emuTraf.engFPS[p.host], st.FPS)
				emuLab.mu.Unlock()
			}
			for k, x := range st.Sess {
				if x != nil {
					sum.Sess[k] = x
				}
			}
			for k, n := range st.TX {
				sum.TX[k] += n
			}
			for k, n := range st.RX {
				sum.RX[k] += n
			}
			for k, n := range st.Other {
				sum.Other[k] += n
			}
			for k, x := range st.Lat {
				if len(x) >= 3 {
					s := sum.Lat[k]
					if s == nil {
						s = []float64{0, 0, 0, math.Inf(1)}
					}
					s[0], s[1], s[2] = s[0]+x[0], math.Max(s[1], x[1]), s[2]+x[2]
					if len(x) > 3 {
						s[3] = math.Min(s[3], x[3])
					}
					sum.Lat[k] = s
				}
			}
			emuLab.mu.Lock()
			if emuTraf.leases == nil {
				emuTraf.leases = map[string][2]string{}
			}
			for k, x := range st.Lease {
				l := emuTraf.leases[k]
				l[0] = x.IP + map[bool]string{true: " via " + x.GW, false: ""}[x.GW != ""]
				emuTraf.leases[k] = l
			}
			for k, x := range st.Lease6 {
				l := emuTraf.leases[k]
				l[1] = x.IP
				emuTraf.leases[k] = l
			}
			if emuTraf.proto == nil {
				emuTraf.proto = map[string][2]string{}
			}
			if emuTraf.clients == nil {
				emuTraf.clients, emuTraf.stp = map[string]*emu.ClientState{}, map[string]*emu.STPState{}
			}
			maps.Copy(emuTraf.clients, st.Clients)
			maps.Copy(emuTraf.stp, st.STP)
			if emuTraf.vrrp == nil {
				emuTraf.vrrp, emuTraf.bfd = map[string]*emu.VRRPState{}, map[string]*emu.BFDState{}
			}
			maps.Copy(emuTraf.vrrp, st.VRRP)
			maps.Copy(emuTraf.bfd, st.BFD)
			for k, x := range st.LACP {
				p := emuTraf.proto[k]
				switch {
				case x == nil:
				case x.Up:
					p[0] = fmt.Sprintf("up (switch port %d, key %d)", x.Partner.Port, x.Partner.Key)
				case x.Partner != nil:
					p[0] = fmt.Sprintf("negotiating with switch port %d", x.Partner.Port)
				default:
					p[0] = "no LACP from the switch (is its port in a LAG with LACP?)"
				}
				emuTraf.proto[k] = p
			}
			for k, x := range st.LLDP {
				p := emuTraf.proto[k]
				p[1] = strings.TrimSpace(orStr(x.Name, x.Chassis) + " " + orStr(x.PortDesc, x.Port))
				emuTraf.proto[k] = p
			}
			emuLab.mu.Unlock()
			for k, n := range st.OOO {
				sum.OOO[k] += n
			}
			for k, n := range st.Dup {
				sum.Dup[k] += n
			}
			for k, c := range st.Ports {
				sum.Ports[k] = emuAddPort(sum.Ports[k], c)
			}
			for k, x := range st.Jit {
				if len(x) == 2 {
					j := sum.Jit[k]
					if j == nil {
						j = []float64{0, 0}
					}
					j[0], j[1] = j[0]+x[0], j[1]+x[1]
					sum.Jit[k] = j
				}
			}
			for k, e := range st.Err {
				sum.Err[k] = e
			}
			emuAddCounts(sum.Hist, st.Hist)
			emuAddCounts(sum.FTX, st.FTX)
			emuAddCounts(sum.QoS, st.QoS)
			for _, m := range [][2]map[string]string{{sum.FSent, st.FSent}, {sum.FGot, st.FGot}} {
				for k, h := range m[1] {
					if m[0][k] == "" {
						m[0][k] = h
					}
				}
			}
			for sid, m := range st.FLab {
				if sum.FLab[sid] == nil {
					sum.FLab[sid] = map[string]string{}
				}
				maps.Copy(sum.FLab[sid], m)
			}
			for sid, m := range st.FRX {
				if sum.FRX[sid] == nil {
					sum.FRX[sid] = map[string][]float64{}
				}
				for k, x := range m {
					if len(x) != 4 {
						continue
					}
					if y := sum.FRX[sid][k]; y != nil {
						y[0], y[1], y[2], y[3] = y[0]+x[0], y[1]+x[1], math.Max(y[2], x[2]), y[3]+x[3]
					} else {
						sum.FRX[sid][k] = slices.Clone(x)
					}
				}
			}
			for k, b := range st.Busy {
				sum.Busy[k] = sum.Busy[k] || b
			}
		}
	}
	for _, s := range v.Streams {
		r := EmuStreamResult{ID: s.ID, TX: sum.TX[s.ID], RX: sum.RX[s.ID], Other: sum.Other[s.ID], Running: sum.Busy[s.ID] && run != 0, LatAvg: -1, OOO: sum.OOO[s.ID], Dup: sum.Dup[s.ID]}
		recv := 1
		switch {
		case s.To == "" && emu.IsMulticast(s.DstIP): // the test ports that joined the group
			recv = 0
			for _, t := range v.Testers {
				if t.ID != s.From && slices.ContainsFunc(t.Groups, func(g string) bool { return g == s.DstIP }) {
					recv++
				}
			}
		case s.To == "" && s.Kind != "icmp": // a broadcast reaches every other test port; a ping to an address comes back
			recv = max(0, len(v.Testers)-1)
		}
		r.Expected = r.TX * recv
		if r.Expected > 0 {
			r.Loss = math.Max(0, float64(r.Expected-r.RX)*100/float64(r.Expected))
		}
		if x := sum.Lat[s.ID]; len(x) >= 3 && x[2] > 0 {
			r.LatAvg, r.LatMax = x[0]/x[2], x[1]
			if len(x) > 3 && !math.IsInf(x[3], 1) {
				r.LatMin = x[3]
			}
		}
		if j := sum.Jit[s.ID]; len(j) == 2 && j[1] > 0 {
			r.Jitter = j[0] / j[1]
		}
		if h := sum.Hist[s.ID]; len(h) > 0 {
			r.P50, r.P95, r.P99 = emuLatPct(h, 0.5), emuLatPct(h, 0.95), emuLatPct(h, 0.99)
			if r.LatMin > 0 { // within the bucket: never outside what was seen
				r.P50, r.P95, r.P99 = max(r.P50, r.LatMin), max(r.P95, r.LatMin), max(r.P99, r.LatMin)
			}
			r.P50, r.P95, r.P99 = min(r.P50, r.LatMax), min(r.P95, r.LatMax), min(r.P99, r.LatMax)
			r.Hist = emuLatBins(h)
		}
		if ftx := sum.FTX[s.ID]; len(ftx) > 0 {
			r.Flows, r.FlowsBad = emuFlowResults(ftx, sum.FRX[s.ID], sum.FLab[s.ID], recv)
			r.FlowsN = len(r.Flows)
			r.Flows = r.Flows[:min(len(r.Flows), emuFlowsShown)]
		}
		r.Marks = emuMarks(sum.QoS[s.ID])
		r.Sent, r.Got = sum.FSent[s.ID], sum.FGot[s.ID]
		if s.Kind == "session" {
			emuSessResult(&r, s, sum.Sess[s.ID], sum.Err[s.ID])
			v.Results = append(v.Results, r)
			continue
		}
		switch e := sum.Err[s.ID]; {
		case e != "":
			r.Result, r.Why = "fail", e
		case r.Running || r.TX == 0:
		case s.Expect == "any":
			r.Result = "pass"
		case s.Expect == "drop" && r.RX > 0:
			r.Result, r.Why = "fail", fmt.Sprintf("%d of %d frames got through (they should be dropped)", r.RX, r.TX)
		case s.Expect == "drop":
			r.Result, r.Why = "pass", "dropped, as expected"
		case r.Loss > s.MaxLoss:
			r.Result, r.Why = "fail", fmt.Sprintf("loss %.1f %% > %.1f %%", r.Loss, s.MaxLoss)
		case s.MaxLatency > 0 && r.LatAvg > s.MaxLatency:
			r.Result, r.Why = "fail", fmt.Sprintf("latency %.2f ms > %.2f ms", r.LatAvg, s.MaxLatency)
		case s.MaxJitter > 0 && r.Jitter > s.MaxJitter:
			r.Result, r.Why = "fail", fmt.Sprintf("jitter %.2f ms > %.2f ms", r.Jitter, s.MaxJitter)
		default:
			r.Result = "pass"
		}
		v.Results = append(v.Results, r)
	}
	if v.Results == nil {
		v.Results = []EmuStreamResult{}
	}
	if run != 0 {
		v.Ports = emuPortResults(v.Testers, v.Streams, v.Results, sum.Ports)
	}
	emuLab.mu.Lock()
	for i := range v.Testers {
		if l, ok := emuTraf.leases[v.Testers[i].ID]; ok {
			v.Testers[i].Lease, v.Testers[i].Lease6 = l[0], l[1]
		}
		if p, ok := emuTraf.proto[v.Testers[i].ID]; ok {
			v.Testers[i].LacpUp, v.Testers[i].Neighbor = p[0], p[1]
		}
		if v.Testers[i].Clients != nil {
			v.Testers[i].ClientState = emuTraf.clients[v.Testers[i].ID]
		}
		if v.Testers[i].STP != nil {
			v.Testers[i].STPState = emuTraf.stp[v.Testers[i].ID]
		}
		if v.Testers[i].VRRP != nil {
			v.Testers[i].VRRPState = emuTraf.vrrp[v.Testers[i].ID]
		}
		if v.Testers[i].BFD != nil {
			v.Testers[i].BFDState = emuTraf.bfd[v.Testers[i].ID]
		}
	}
	emuLab.mu.Unlock()
	emuLab.mu.Lock()
	if run != 0 {
		wire := map[string]float64{}
		for _, s := range v.Streams {
			if s.Kind != "session" { // (sessions a second, not frames)
				wire[s.ID] = s.WireBits()
			}
		}
		emuTrafRates(v.Results, time.Since(emuTraf.started).Seconds(), wire)
		emuPortRates(v.Ports, time.Since(emuTraf.started).Seconds())
		v.History, v.PortHistory = maps.Clone(emuTraf.hist), maps.Clone(emuTraf.phist)
		v.Running = slices.ContainsFunc(v.Results, func(r EmuStreamResult) bool { return r.Running }) || time.Since(emuTraf.started) < 3*time.Second
		v.Started = emuTraf.started.Format(time.RFC3339)
		if !v.Running && time.Since(emuTraf.started) > 5*time.Second {
			emuTraf.last, emuTraf.ports, emuTraf.final = v.Results, v.Ports, true // frames still on their way have arrived
		}
	}
	emuLab.mu.Unlock()
	return v, nil
}

// emuSessCheck checks (and fills in) a session stream: it goes to a test port, which answers its sessions; the
// other kinds have no sessions (emuLab.mu held).
func emuSessCheck(l *emu.Lab, s *emu.Stream) error {
	if s.Kind != "session" {
		s.Session = nil
		return nil
	}
	if s.To == "" && s.DstIP == "" {
		return errors.New("a session stream goes to a test port (it answers the sessions) or to an address")
	}
	if f, to := l.TesterByID(s.From), l.TesterByID(s.To); f != nil && f.Trex != "" || to != nil && to.Trex != "" {
		return errors.New("sessions are opened by the KVM hosts' test ports and host network ports, not TRex ports (stateless)")
	}
	x := s.Session
	if x == nil {
		x = &emu.Session{}
	}
	x.DPort = min(65535, max(1, orInt(x.DPort, 80)))
	x.Count = min(10000000, max(1, orInt(x.Count, 1000)))
	x.Concurrent = min(100000, max(1, orInt(x.Concurrent, 100)))
	if x.Rate <= 0 {
		x.Rate = 100
	}
	x.Rate = math.Min(100000, x.Rate)
	x.Req, x.Resp = min(100<<20, max(0, x.Req)), min(100<<20, max(0, x.Resp))
	x.Hold = math.Min(3600, math.Max(0, x.Hold))
	if !slices.Contains([]string{"", "refused", "dropped"}, x.Expect) {
		return errors.New("its sessions should connect, be refused or get no answer")
	}
	s.Session, s.DPort, s.Expect = x, x.DPort, ""
	return nil
}

// emuSessResult is a session stream's result from its sessions: sent are the sessions tried, received the ones
// that connected, the latency their setup time (emuLab.mu not needed).
func emuSessResult(r *EmuStreamResult, s emu.Stream, c *EmuSessionResult, err string) {
	if c == nil {
		c = &EmuSessionResult{}
	}
	if c.Secs > 0 {
		c.CPS = math.Round(float64(c.OK)/c.Secs*10) / 10
	}
	r.Sessions = c
	r.TX, r.RX, r.Expected, r.LatAvg, r.LatMax = c.Tried, c.OK, c.Tried, -1, 0
	failed := c.Tried - c.OK + c.Reset
	if c.Tried > 0 {
		r.Loss = math.Max(0, float64(failed)*100/float64(c.Tried))
	}
	if c.OK > 0 {
		r.LatAvg, r.LatMax = c.SetupAvg, c.SetupMax
	}
	exp := ""
	if s.Session != nil {
		exp = s.Session.Expect
	}
	switch {
	case err != "":
		r.Result, r.Why = "fail", err
	case r.Running || c.Tried == 0:
	case exp == "refused" && c.OK > 0:
		r.Result, r.Why = "fail", fmt.Sprintf("%d of %d sessions connected (they should be refused)", c.OK, c.Tried)
	case exp == "refused" && c.Refused < c.Tried:
		r.Result, r.Why = "fail", fmt.Sprintf("%d of %d sessions got no answer (they should be refused with a reset)", c.Tried-c.Refused, c.Tried)
	case exp == "refused":
		r.Result, r.Why = "pass", "refused, as expected"
	case exp == "dropped" && c.OK > 0:
		r.Result, r.Why = "fail", fmt.Sprintf("%d of %d sessions connected (they should get no answer)", c.OK, c.Tried)
	case exp == "dropped" && c.Refused > 0:
		r.Result, r.Why = "fail", fmt.Sprintf("%d of %d sessions were refused with a reset (they should get no answer)", c.Refused, c.Tried)
	case exp == "dropped":
		r.Result, r.Why = "pass", "no answer, as expected"
	case r.Loss > s.MaxLoss:
		r.Result, r.Why = "fail", fmt.Sprintf("%d of %d sessions failed (%.1f %% > %.1f %%): %d refused, %d timed out, %d reset", failed, c.Tried, r.Loss, s.MaxLoss, c.Refused, c.Timeout, c.Reset)
	case s.MaxLatency > 0 && c.SetupAvg > s.MaxLatency:
		r.Result, r.Why = "fail", fmt.Sprintf("setup %.2f ms > %.2f ms", c.SetupAvg, s.MaxLatency)
	default:
		r.Result = "pass"
	}
}

// emuTrafRates works out each stream's rates since the last read and keeps them for the graphs (emuLab.mu held).
func emuTrafRates(rs []EmuStreamResult, at float64, wire map[string]float64) {
	if emuTraf.prev == nil {
		emuTraf.prev, emuTraf.hist = map[string][3]float64{}, map[string][][5]float64{}
	}
	for i := range rs {
		r := &rs[i]
		p, ok := emuTraf.prev[r.ID]
		if ok && at-p[0] >= 0.5 {
			dt := at - p[0]
			r.TxRate, r.RxRate = math.Max(0, (float64(r.TX)-p[1])/dt), math.Max(0, (float64(r.RX)-p[2])/dt)
			if b := wire[r.ID]; b > 0 {
				r.TxMbps, r.RxMbps = math.Round(r.TxRate*b/1e4)/100, math.Round(r.RxRate*b/1e4)/100
			}
			h := append(emuTraf.hist[r.ID], [5]float64{math.Round(at*10) / 10, math.Round(r.TxRate), math.Round(r.RxRate), math.Round(r.Loss*100) / 100, math.Round(r.LatAvg*1000) / 1000})
			if len(h) > 300 {
				h = h[len(h)-300:]
			}
			emuTraf.hist[r.ID] = h
		} else if ok {
			continue
		}
		emuTraf.prev[r.ID] = [3]float64{at, float64(r.TX), float64(r.RX)}
	}
}

// emuAddPort adds a host's counters of a test port to the others' (a test port is on one host; this keeps it simple).
func emuAddPort(a, b emuPortCount) emuPortCount {
	a.TX, a.RX, a.TXB, a.RXB, a.Err = a.TX+b.TX, a.RX+b.RX, a.TXB+b.TXB, a.RXB+b.RXB, a.Err+b.Err
	if a.SRX == nil {
		a.SRX = map[string]int{}
	}
	for k, n := range b.SRX {
		a.SRX[k] += n
	}
	a.Lat = [3]float64{a.Lat[0] + b.Lat[0], a.Lat[1] + b.Lat[1], math.Max(a.Lat[2], b.Lat[2])}
	return a
}

// emuPortFor tells whether test port t is where stream s's frames should arrive: the one it is to (a LAG's port
// stands for the LAG), every other port for a broadcast, the ports that joined its group, or the sender for a ping
// to an address.
func emuPortFor(s emu.Stream, t emu.Tester) bool {
	switch {
	case s.To != "":
		return t.ID == s.To
	case emu.IsMulticast(s.DstIP):
		return t.ID != s.From && slices.Contains(t.Groups, s.DstIP)
	case s.Kind == "icmp":
		return t.ID == s.From
	}
	return t.ID != s.From
}

// emuPortResults: each test port's statistics from the hosts' counters (frames of a stream to a LAG count at the
// LAG's port the stream is to, whichever member they came in on).
func emuPortResults(ts []emu.Tester, ss []emu.Stream, rs []EmuStreamResult, c map[string]emuPortCount) []EmuPortResult {
	lag := map[string]string{}
	for _, t := range ts {
		if t.Lag != "" {
			lag[t.ID] = t.Lag
		}
	}
	tx := map[string]int{}
	for _, r := range rs {
		tx[r.ID] = r.TX
	}
	out := make([]EmuPortResult, 0, len(ts))
	for _, t := range ts {
		x := c[t.ID]
		p := EmuPortResult{ID: t.ID, TX: x.TX, RX: x.RX, TxBytes: x.TXB, RxBytes: x.RXB, Errors: x.Err, LatAvg: -1, Streams: x.SRX}
		if x.Lat[1] > 0 {
			p.LatAvg, p.LatMax = math.Round(x.Lat[0]/x.Lat[1]*1000)/1000, math.Round(x.Lat[2]*1000)/1000
		}
		for _, s := range ss {
			if !emuPortFor(s, t) || tx[s.ID] == 0 {
				continue
			}
			p.Expected += tx[s.ID]
			got := x.SRX[s.ID]
			if l := lag[t.ID]; l != "" {
				for _, o := range ts {
					if o.ID != t.ID && lag[o.ID] == l {
						got += c[o.ID].SRX[s.ID]
					}
				}
			}
			p.Got += min(got, tx[s.ID])
		}
		if p.Expected > 0 {
			p.Loss = math.Round(math.Max(0, float64(p.Expected-p.Got)*100/float64(p.Expected))*100) / 100
		}
		out = append(out, p)
	}
	return out
}

// emuPortRates works out each test port's rates since the last read and keeps the last minute of them (emuLab.mu held).
func emuPortRates(ps []EmuPortResult, at float64) {
	if emuTraf.pprev == nil {
		emuTraf.pprev, emuTraf.phist = map[string][3]float64{}, map[string][][3]float64{}
	}
	for i := range ps {
		p := &ps[i]
		q, ok := emuTraf.pprev[p.ID]
		if ok && at-q[0] < 0.5 {
			continue
		}
		if ok {
			dt := at - q[0]
			p.TxRate, p.RxRate = math.Max(0, (float64(p.TX)-q[1])/dt), math.Max(0, (float64(p.RX)-q[2])/dt)
			if p.TX > 0 {
				p.TxMbps = math.Round(p.TxRate*(float64(p.TxBytes)/float64(p.TX)+20)*8/1e4) / 100
			}
			if p.RX > 0 {
				p.RxMbps = math.Round(p.RxRate*(float64(p.RxBytes)/float64(p.RX)+20)*8/1e4) / 100
			}
			h := append(emuTraf.phist[p.ID], [3]float64{math.Round(at*10) / 10, math.Round(p.TxRate), math.Round(p.RxRate)})
			if len(h) > 60 {
				h = h[len(h)-60:]
			}
			emuTraf.phist[p.ID] = h
		}
		emuTraf.pprev[p.ID] = [3]float64{at, float64(p.TX), float64(p.RX)}
	}
}

// emuTrafCached: the last run's results for the streams there are now (emuLab.mu held).
func emuTrafCached(ss []emu.Stream) []EmuStreamResult {
	out := []EmuStreamResult{}
	for _, s := range ss {
		r := EmuStreamResult{ID: s.ID, LatAvg: -1}
		for _, x := range emuTraf.last {
			if x.ID == s.ID {
				r = x
			}
		}
		out = append(out, r)
	}
	return out
}

// EmuTesterAdd puts a test port on a free port of a switch.
func (a *App) EmuTesterAdd(vmID, port, ip, gw string, vlan int) (EmuTrafficView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("there is no emulated lab")
	}
	v := l.VMByID(vmID)
	if v == nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("no such switch")
	}
	n := emuPort(emuImage(v.Image), port)
	if n == 0 {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, fmt.Errorf("%s has no port %s", v.Name, port)
	}
	free := a.emuFreePortsLocked(vmID)
	if !slices.Contains(free, emuImage(v.Image).PortNameOf(n)) {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, fmt.Errorf("%s %s is in use (a cable, the host's network or a test port)", v.Name, port)
	}
	dhcp := strings.EqualFold(ip, "dhcp")
	if dhcp {
		ip = ""
	}
	if ip != "" && !strings.Contains(ip, "/") {
		ip += "/24"
	}
	t := emu.Tester{ID: "t" + randHex(3), Name: emuTesterName(l), VM: vmID, Port: n, MAC: "02:74:77:" + randHex(1) + ":" + randHex(1) + ":" + randHex(1), IP: ip, GW: gw, VLAN: vlan, DHCP: dhcp, LLDP: true}
	t.MAC = strings.ToLower(t.MAC)
	l.Testers = append(l.Testers, t)
	_ = a.emuSave()
	emuEv(vmID, "info", fmt.Sprintf("%s on port %s%s", t.Name, emuImage(v.Image).PortNameOf(n), map[bool]string{true: " (" + ip + ")", false: ""}[ip != ""]))
	emuLab.mu.Unlock()
	note := a.emuTesterLink(vmID, n, true)
	err := a.emuTrafficPush()
	emuLab.mu.Lock()
	if note == "" && emuTraf.sock[emuPeerKey(vmID, n)] < 0 {
		note = "restart " + v.Name + " once for its test port: the port started with a cable whose other end still runs or is on another host"
	}
	emuLab.mu.Unlock()
	tv, _ := a.EmuTraffic()
	tv.Note = note
	return tv, err
}

// EmuTesterSet changes a test port's name, address, VLAN or place.
func (a *App) EmuTesterSet(t emu.Tester) (EmuTrafficView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var cur *emu.Tester
	if l != nil {
		cur = l.TesterByID(t.ID)
	}
	if cur == nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("no such test port")
	}
	if strings.EqualFold(t.IP, "dhcp") {
		t.IP, t.DHCP = "", true
	}
	if strings.EqualFold(t.IP6, "slaac") {
		t.IP6, t.SLAAC = "", true
	}
	if t.IP != "" && !strings.Contains(t.IP, "/") {
		t.IP += "/24"
	}
	moved := cur.X != t.X || cur.Y != t.Y
	if t.IP6 != "" && !strings.Contains(t.IP6, "/") {
		t.IP6 += "/64"
	}
	for _, g := range t.Groups {
		if !emu.IsMulticast(g) {
			emuLab.mu.Unlock()
			return EmuTrafficView{}, fmt.Errorf("%s is not a multicast group", g)
		}
	}
	cur.Name, cur.IP, cur.GW, cur.VLAN, cur.X, cur.Y = orStr(strings.TrimSpace(t.Name), cur.Name), t.IP, t.GW, t.VLAN, t.X, t.Y
	cur.IP6, cur.Groups, cur.DHCP, cur.SLAAC, cur.LLDP = t.IP6, t.Groups, t.DHCP && t.IP == "", t.SLAAC && t.IP6 == "", t.LLDP
	cur.Speed = min(400000, max(0, t.Speed))
	cur.Cabled = strings.TrimSpace(t.Cabled)
	cur.Lag, cur.LacpFast = strings.TrimSpace(t.Lag), t.LacpFast
	if err := emuCheckClients(t.Clients, t.STP); err != nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, err
	}
	if err := emuCheckRedundancy(t.VRRP, t.BFD); err != nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, err
	}
	if t.Clients != nil && t.Clients.N == 0 {
		t.Clients = nil
	}
	if t.STP != nil && t.STP.Mode == "" {
		t.STP = nil
	}
	cur.Clients, cur.STP, cur.VRRP, cur.BFD = t.Clients, t.STP, t.VRRP, t.BFD
	for i, x := range l.Streams { // rates in % of its line rate follow it
		if x.From == cur.ID && x.RateUnit == "pct" {
			l.Streams[i].PPS = min(emuMaxPPS(l, x), max(1, int(math.Round(x.PPSFor(cur.LineMbps())))))
		}
	}
	if l := emuTraf.leases[cur.ID]; emuTraf.leases != nil {
		if !cur.DHCP {
			l[0] = ""
		}
		if !cur.SLAAC {
			l[1] = ""
		}
		emuTraf.leases[cur.ID] = l
	}
	_ = a.emuSave()
	emuLab.mu.Unlock()
	if !moved {
		if err := a.emuTrafficPush(); err != nil {
			return EmuTrafficView{}, err
		}
	}
	return a.EmuTraffic()
}

// emuCheckClients refuses clients or spanning tree settings that cannot work.
func emuCheckClients(c *emu.Clients, s *emu.STP) error {
	if c != nil {
		switch {
		case c.N < 0 || c.N > emu.ClientsMax:
			return fmt.Errorf("a test port stands for 1 to %d clients", emu.ClientsMax)
		case c.MAC != "" && !emuMACRe.MatchString(c.MAC):
			return fmt.Errorf("%s is not a MAC address", c.MAC)
		case c.Rate < 0 || c.Rate > 1000:
			return errors.New("clients start at 1 to 1000 a second")
		case c.Dot1x != nil && c.Dot1x.Method != "" && c.Dot1x.Method != "md5" && c.Dot1x.Method != "peap":
			return errors.New("the 802.1X method is md5 or peap")
		}
		for _, g := range c.Groups {
			if !emu.IsMulticast(g) {
				return fmt.Errorf("%s is not a multicast group", g)
			}
		}
	}
	if s != nil {
		if s.Mode != "" && s.Mode != "stp" && s.Mode != "rstp" {
			return errors.New("spanning tree is stp or rstp")
		}
		if s.Prio != nil && (*s.Prio < 0 || *s.Prio > 61440 || *s.Prio%4096 != 0) {
			return errors.New("a bridge priority is 0 to 61440 in steps of 4096")
		}
	}
	return nil
}

var emuMACRe = regexp.MustCompile(`^(?i)[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)

// emuCheckRedundancy refuses VRRP or BFD settings that cannot work.
func emuCheckRedundancy(v *emu.VRRP, b *emu.BFD) error {
	if v != nil {
		ip := net.ParseIP(v.VIP)
		switch {
		case v.Ver != 2 && v.Ver != 3:
			return errors.New("VRRP is version 2 or 3")
		case v.VRID < 1 || v.VRID > 255:
			return errors.New("a VRRP router ID is 1 to 255")
		case v.Prio < 1 || v.Prio > 254:
			return errors.New("a VRRP priority is 1 to 254")
		case ip == nil:
			return fmt.Errorf("%q is not a virtual IP address", v.VIP)
		case ip.To4() == nil && v.Ver == 2:
			return errors.New("an IPv6 virtual address needs VRRPv3")
		case v.Interval != 0 && (v.Interval < 10 || v.Interval > 40950):
			return errors.New("the VRRP interval is 10 to 40,950 ms")
		}
	}
	if b != nil {
		ip := net.ParseIP(b.Peer)
		switch {
		case ip == nil || ip.To4() == nil:
			return fmt.Errorf("%q is not the switch's IPv4 address", b.Peer)
		case b.TX != 0 && (b.TX < 10 || b.TX > 10000), b.RX != 0 && (b.RX < 10 || b.RX > 10000):
			return errors.New("BFD intervals are 10 to 10,000 ms")
		case b.Mult < 0 || b.Mult > 255:
			return errors.New("the BFD multiplier is 1 to 255")
		}
	}
	return nil
}

// EmuTesterRemove takes a test port off (its streams go too).
func (a *App) EmuTesterRemove(id string) (EmuTrafficView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var t *emu.Tester
	if l != nil {
		t = l.TesterByID(id)
	}
	if t == nil {
		emuLab.mu.Unlock()
		return a.EmuTraffic()
	}
	tt := *t
	l.Testers = slices.DeleteFunc(l.Testers, func(x emu.Tester) bool { return x.ID == id })
	l.Streams = slices.DeleteFunc(l.Streams, func(s emu.Stream) bool { return s.From == id || s.To == id })
	_ = a.emuSave()
	emuEv(tt.VM, "info", tt.Name+" removed")
	emuLab.mu.Unlock()
	a.emuTesterLink(tt.VM, tt.Port, false)
	err := a.emuTrafficPush()
	tv, _ := a.EmuTraffic()
	return tv, err
}

// EmuStreamSave adds a stream (no ID) or changes one.
func (a *App) EmuStreamSave(s emu.Stream) (EmuTrafficView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("there is no emulated lab")
	}
	if l.TesterByID(s.From) == nil || s.To != "" && l.TesterByID(s.To) == nil || s.To == s.From {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("choose the test port it goes from, and the one it goes to")
	}
	s.Kind = orStr(s.Kind, "udp")
	if !slices.Contains([]string{"udp", "icmp", "tcp", "arp", "raw", "pcap", "custom", "session"}, s.Kind) {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("the kind is udp, icmp, tcp, arp, raw, pcap, custom or session")
	}
	if err := emuSessCheck(l, &s); err != nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, err
	}
	if s.Kind == "pcap" {
		if err := a.emuPcapKeep(&s); err != nil {
			emuLab.mu.Unlock()
			return EmuTrafficView{}, err
		}
	} else {
		s.Pcap, s.PcapID, s.PcapFrames, s.PcapSecs, s.PcapTiming, s.Loops, s.RewriteMAC = "", "", 0, 0, false, 0, false
	}
	if s.Kind != "arp" && s.Kind != "raw" && s.Kind != "pcap" && s.Kind != "custom" && s.To == "" && s.DstIP == "" {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("an IP stream needs a destination test port or address")
	}
	if f := l.TesterByID(s.From); s.Kind != "arp" && s.Kind != "raw" && s.Kind != "pcap" && s.Kind != "custom" && s.SrcIP == "" && (f.IP == "" && !f.DHCP && !strings.Contains(s.DstIP, ":") || strings.Contains(s.DstIP, ":") && f.IP6 == "" && !f.SLAAC) {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("give the test port it goes from an IP address (or the stream a source address)")
	}
	s.Size = min(9000, max(60, orInt(s.Size, 128)))
	switch s.RateUnit {
	case "":
		s.Rate = 0
	case "mbps", "pct":
		if s.Rate <= 0 || s.RateUnit == "pct" && s.Rate > 100 {
			emuLab.mu.Unlock()
			return EmuTrafficView{}, errors.New("the rate is more than 0 (and at most 100 % of the line rate)")
		}
		if s.SizeMode == "inc" || s.SizeMode == "random" {
			s.SizeMax = min(9000, max(s.Size, s.SizeMax))
		}
		s.PPS = int(math.Round(s.PPSFor(l.TesterByID(s.From).LineMbps())))
	default:
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("the rate unit is pps, mbps or pct")
	}
	s.PPS = min(emuMaxPPS(l, s), max(1, orInt(s.PPS, 100)))
	if s.Seconds <= 0 && s.Count <= 0 {
		s.Seconds = 10
	}
	if s.Kind == "session" { // until its sessions are done, at most this long
		s.Count = 0
		s.Seconds = orInt(s.Seconds, 60)
	}
	s.VLAN, s.Prio = min(4094, max(0, s.VLAN)), min(7, max(0, s.Prio))
	s.DSCP, s.Burst = min(63, max(0, s.DSCP)), max(0, s.Burst)
	switch s.SizeMode {
	case "", "imix":
	case "inc", "random":
		s.SizeMax = min(9000, max(s.Size, s.SizeMax))
	default:
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("the sizes are fixed, inc, random or imix")
	}
	for i, m := range s.Mods {
		if !slices.Contains([]string{"srcMac", "dstMac", "srcIp", "dstIp", "vlan", "sport", "dport"}, m.Field) {
			emuLab.mu.Unlock()
			return EmuTrafficView{}, fmt.Errorf("a changing field is srcMac, dstMac, srcIp, dstIp, vlan, sport or dport, not %q", m.Field)
		}
		s.Mods[i].Mode, s.Mods[i].Count, s.Mods[i].Step = orStr(m.Mode, "inc"), max(1, m.Count), orInt(m.Step, 1)
	}
	if err := emuStreamFrame(&s); err != nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, err
	}
	if err := emuTrexCheck(l, &s); err != nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, err
	}
	if s.ID == "" {
		s.ID = "s" + randHex(3)
		s.Name = orStr(strings.TrimSpace(s.Name), fmt.Sprintf("stream-%d", len(l.Streams)+1))
		l.Streams = append(l.Streams, s)
	} else if i := slices.IndexFunc(l.Streams, func(x emu.Stream) bool { return x.ID == s.ID }); i >= 0 {
		s.Name = orStr(strings.TrimSpace(s.Name), l.Streams[i].Name)
		l.Streams[i] = s
	} else {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("no such stream")
	}
	_ = a.emuSave()
	emuLab.mu.Unlock()
	return a.EmuTraffic()
}

// emuStreamFrame checks (and tidies) what a stream does to its frames: a custom frame's hex, its user-defined
// fields, its fault, its fuzzed range and what should happen to its frames.
func emuStreamFrame(s *emu.Stream) error {
	if s.Kind == "custom" {
		b, err := emu.ParseFrameHex(s.Hex)
		if err != nil || len(b) < 14 {
			return errors.New("a custom frame needs its bytes in hex, at least the MAC addresses and the EtherType (14 bytes)")
		}
		if len(b) > 9000 {
			return errors.New("a custom frame is at most 9,000 bytes")
		}
		s.Hex = hex.EncodeToString(b)
		if s.Size < len(b)+4 {
			s.Size = min(9000, max(60, len(b)+4))
		}
	} else {
		s.Hex, s.NoSig = "", false
	}
	if len(s.UDFs) > 8 {
		return errors.New("a stream has at most 8 user-defined fields")
	}
	for i, u := range s.UDFs {
		u.Width = min(4, max(1, orInt(u.Width, 1)))
		u.Mode = orStr(u.Mode, "inc")
		if !slices.Contains([]string{"inc", "dec", "random", "list"}, u.Mode) {
			return fmt.Errorf("a user-defined field counts inc, dec, random or from a list, not %q", u.Mode)
		}
		if u.Offset < 0 || u.Offset+u.Width > 9000 {
			return errors.New("a user-defined field's offset is in the frame (0 is the first byte of the destination MAC)")
		}
		if u.Mode == "list" && (len(u.Values) == 0 || len(u.Values) > 256) {
			return errors.New("a user-defined field from a list needs 1 to 256 values")
		}
		u.Count, u.Step = min(1000000, max(1, orInt(u.Count, 256))), cmp.Or(u.Step, 1)
		s.UDFs[i] = u
	}
	if s.Bad != "" {
		b := emu.BadFrame(s.Bad)
		switch {
		case b == nil:
			return fmt.Errorf("no such fault: %q", s.Bad)
		case b.TCP && s.Kind != "tcp" && s.Kind != "custom":
			return fmt.Errorf("%s needs a TCP stream", b.Name)
		case s.Kind == "pcap":
			return errors.New("a capture's frames are replayed as they are: no fault")
		}
	}
	if s.Fuzz != nil {
		if s.Fuzz.From < 0 || s.Fuzz.To != 0 && s.Fuzz.To <= s.Fuzz.From || s.Fuzz.To > 9000 {
			return errors.New("the fuzzed bytes run from an offset to a later one (0: up to the counting signature)")
		}
		if s.Kind == "pcap" {
			return errors.New("a capture's frames are replayed as they are: not fuzzed")
		}
	}
	if !slices.Contains([]string{"", "drop", "any"}, s.Expect) {
		return errors.New("its frames should arrive (\"\"), be dropped (drop) or only be counted (any)")
	}
	return emuCheckEncap(s)
}

// emuCheckEncap refuses headers that cannot go around a stream's frames, and drops empty ones.
func emuCheckEncap(s *emu.Stream) error {
	e := s.Encap
	if e == nil {
		return nil
	}
	if e.Outer == nil && len(e.MPLS) == 0 && e.GRE == nil && e.VXLAN == nil && len(e.V6Ext) == 0 {
		s.Encap = nil
		return nil
	}
	if s.Kind == "pcap" {
		return errors.New("a capture's frames are replayed as they are: no headers added")
	}
	ip4 := func(a string) bool { x := net.ParseIP(a); return x != nil && x.To4() != nil }
	if o := e.Outer; o != nil {
		o.TPID = cmp.Or(o.TPID, 0x88A8)
		if !slices.Contains([]int{0x88A8, 0x8100, 0x9100}, o.TPID) || o.VLAN < 1 || o.VLAN > 4094 || o.Prio < 0 || o.Prio > 7 {
			return errors.New("an outer tag is TPID 0x88A8, 0x8100 or 0x9100, VLAN 1-4094 and priority 0-7")
		}
	}
	if len(e.MPLS) > 3 {
		return errors.New("a stream carries at most 3 MPLS labels")
	}
	for _, m := range e.MPLS {
		if m.Label < 0 || m.Label > 1048575 || m.TC < 0 || m.TC > 7 || m.TTL < 0 || m.TTL > 255 {
			return errors.New("an MPLS label is 0-1048575, its TC 0-7 and TTL 0-255")
		}
	}
	tunnels := 0
	if g := e.GRE; g != nil {
		tunnels++
		if !ip4(g.Dst) || g.Src != "" && !ip4(g.Src) || g.Key < 0 || g.Key > 0xFFFFFFFF {
			return errors.New("a GRE tunnel needs the IPv4 address of its far end (and its own, if not the test port's)")
		}
	}
	if x := e.VXLAN; x != nil {
		tunnels++
		if !ip4(x.Dst) || x.Src != "" && !ip4(x.Src) || x.VNI < 1 || x.VNI > 16777215 || x.Port < 0 || x.Port > 65535 {
			return errors.New("a VXLAN tunnel needs a VNI (1-16777215) and the IPv4 address of the VTEP")
		}
	}
	switch {
	case tunnels > 1:
		return errors.New("a stream goes in one tunnel: GRE or VXLAN")
	case tunnels > 0 && len(e.MPLS) > 0:
		return errors.New("MPLS labels and a tunnel do not go together here")
	case (tunnels > 0 || len(e.MPLS) > 0) && (s.Kind == "raw" || s.Kind == "arp"):
		return errors.New("MPLS and tunnels carry IP: not a raw or ARP stream")
	case len(e.V6Ext) > 4:
		return errors.New("at most 4 IPv6 extension headers")
	}
	if len(e.V6Ext) > 0 && !strings.Contains(s.DstIP, ":") {
		return errors.New("IPv6 extension headers need an IPv6 stream")
	}
	for _, h := range e.V6Ext {
		if !slices.Contains([]string{"hbh", "dst", "rt", "frag"}, h.Type) || h.Offset < 0 || h.Offset > 8191 {
			return errors.New("an IPv6 extension header is hbh, dst, rt or frag (offset 0-8191)")
		}
		for _, a := range strings.Fields(strings.ReplaceAll(h.Segs, ",", " ")) {
			if x := net.ParseIP(a); x == nil || x.To4() != nil {
				return fmt.Errorf("%q is not an IPv6 segment", a)
			}
		}
	}
	return nil
}

// EmuStreamRemove deletes a stream.
func (a *App) EmuStreamRemove(id string) (EmuTrafficView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	if l := emuLab.f.Lab; l != nil {
		l.Streams = slices.DeleteFunc(l.Streams, func(s emu.Stream) bool { return s.ID == id })
		_ = a.emuSave()
	}
	emuLab.mu.Unlock()
	return a.EmuTraffic()
}

// EmuTrafficStart starts streams (none named: every one not left out) with fresh counters.
func (a *App) EmuTrafficStart(ids []string) (EmuTrafficView, error) {
	defer emuAs("user")()
	emuTests.Lock()
	busy := emuTests.cur != nil
	emuTests.Unlock()
	if busy {
		return EmuTrafficView{}, errors.New("a test is running: stop it first")
	}
	return a.emuTrafficStart(ids, false)
}

// emuTrafficStartQuiet starts a test's trial (not logged in the Events).
func (a *App) emuTrafficStartQuiet(ids []string) (EmuTrafficView, error) {
	return a.emuTrafficStart(ids, true)
}

func (a *App) emuTrafficStart(ids []string, quiet bool) (EmuTrafficView, error) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil || len(l.Streams) == 0 {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("add a stream first")
	}
	on := map[string]bool{}
	var names []string
	for _, s := range l.Streams {
		if len(ids) == 0 && !s.Off || slices.Contains(ids, s.ID) || slices.Contains(ids, s.Name) {
			on[s.ID] = true
			names = append(names, s.Name)
		}
	}
	if len(on) == 0 {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("no such stream")
	}
	emuTraf.run, emuTraf.started, emuTraf.on, emuTraf.last, emuTraf.final = max(emuTraf.run+1, uint32(time.Now().Unix())), time.Now(), on, nil, false
	emuTraf.prev, emuTraf.hist = nil, nil
	emuTraf.ports, emuTraf.pprev, emuTraf.phist = nil, nil, nil
	if !quiet {
		emuEv("", "info", "traffic started: "+strings.Join(names, ", "))
	}
	run := emuTraf.run
	emuLab.mu.Unlock()
	if err := a.emuTrafficPush(); err != nil {
		return EmuTrafficView{}, err
	}
	a.emuTrexStart(run, on)
	return a.EmuTraffic()
}

// EmuTrafficStop stops sending (the counters stay for the report).
func (a *App) EmuTrafficStop() (EmuTrafficView, error) {
	defer emuAs("user")()
	v, _ := a.EmuTraffic()
	emuLab.mu.Lock()
	run := emuTraf.run
	emuTraf.last, emuTraf.ports = v.Results, v.Ports
	emuTraf.on = map[string]bool{}
	emuLab.mu.Unlock()
	if run == 0 {
		return v, nil
	}
	// the stopped configuration: no stream on, the same run (traffic.py keeps the counts)
	a.emuTrexStop()
	if err := a.emuTrafficPush(); err != nil {
		return v, err
	}
	emuLab.mu.Lock()
	emuEv("", "info", "traffic stopped")
	emuLab.mu.Unlock()
	return a.EmuTraffic()
}

// emuTrafficRows are the report's rows.
func emuTrafficRows(v EmuTrafficView) [][]string {
	name := map[string]string{}
	for _, t := range v.Testers {
		name[t.ID] = t.Name
	}
	res := map[string]EmuStreamResult{}
	for _, r := range v.Results {
		res[r.ID] = r
	}
	rows := [][]string{{"stream", "from", "to", "kind", "vlan", "size", "pps", "sent", "expected", "received", "loss %", "latency avg ms", "latency max ms", "result", "why", "latency min ms", "jitter ms", "out of order", "duplicates", "dscp", "sizes", "latency p50 ms", "latency p95 ms", "latency p99 ms", "latency", "flows", "flows with loss"}}
	for _, s := range v.Streams {
		r := res[s.ID]
		lat, latMax, latMin, jit, p50, p95, p99 := "-", "-", "-", "-", "-", "-", "-"
		if r.LatAvg >= 0 {
			lat, latMax, latMin, jit = fmt.Sprintf("%.3f", r.LatAvg), fmt.Sprintf("%.3f", r.LatMax), fmt.Sprintf("%.3f", r.LatMin), fmt.Sprintf("%.3f", r.Jitter)
		}
		if r.P50 > 0 {
			p50, p95, p99 = fmt.Sprintf("%.3f", r.P50), fmt.Sprintf("%.3f", r.P95), fmt.Sprintf("%.3f", r.P99)
		}
		sizes := fmt.Sprint(s.Size)
		switch s.SizeMode {
		case "imix":
			sizes = "IMIX"
		case "inc", "random":
			sizes = fmt.Sprintf("%d-%d %s", s.Size, s.SizeMax, s.SizeMode)
		}
		to := orStr(name[s.To], orStr(s.DstIP, "all"))
		rows = append(rows, []string{s.Name, name[s.From], to, s.Kind, fmt.Sprint(s.VLAN), fmt.Sprint(s.Size), fmt.Sprint(s.PPS), fmt.Sprint(r.TX), fmt.Sprint(r.Expected), fmt.Sprint(r.RX), fmt.Sprintf("%.2f", r.Loss), lat, latMax, orStr(strings.ToUpper(r.Result), "-"), r.Why,
			latMin, jit, fmt.Sprint(r.OOO), fmt.Sprint(r.Dup), fmt.Sprint(s.DSCP), sizes, p50, p95, p99, map[bool]string{true: "round trip", false: "one way"}[s.RTT], fmt.Sprint(r.FlowsN), fmt.Sprint(r.FlowsBad)})
	}
	return rows
}

// EmuTrafficReportText is the report as text (for the connector's AI agent).
func (a *App) EmuTrafficReportText() (string, error) {
	v, err := a.EmuTraffic()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if v.Running {
		b.WriteString("Still running - the numbers are not final.\n")
	}
	for i, r := range emuTrafficRows(v) {
		if i == 0 {
			continue
		}
		fmt.Fprintf(&b, "- %s (%s → %s, %s, VLAN %s, DSCP %s, %s B, %s pps): sent %s, received %s of %s, loss %s %%, latency (%s) %s ms min / %s avg / %s p50 / %s p95 / %s p99 / %s max, jitter %s ms, %s out of order, %s duplicates", r[0], r[1], r[2], r[3], r[4], r[19], r[20], r[6], r[7], r[9], r[8], r[10], r[24], r[15], r[11], r[21], r[22], r[23], r[12], r[16], r[17], r[18])
		if r[25] != "0" {
			fmt.Fprintf(&b, ", %s flows (%s with loss)", r[25], r[26])
		}
		fmt.Fprintf(&b, ": %s %s\n", r[13], r[14])
	}
	if b.Len() == 0 {
		return "No stream yet.", nil
	}
	return b.String(), nil
}

// EmuTrafficReport saves the report on this computer, by the file's extension: .pdf (printed by a Chromium browser),
// .xlsx (an Excel workbook with charts), .csv (the streams table), or .html (anything else).
func (a *App) EmuTrafficReport(path string) error {
	f := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if !slices.Contains([]string{"pdf", "xlsx", "csv"}, f) {
		f = "html"
	}
	b, _, err := a.emuTrafficReportAs(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// emuTrafficReportAs is the report as csv, html, pdf, xlsx, json (the streams and their results) or text, and its
// media type.
func (a *App) emuTrafficReportAs(format string) ([]byte, string, error) {
	switch format {
	case "text":
		t, err := a.EmuTrafficReportText()
		return []byte(t), "text/plain; charset=utf-8", err
	case "html", "pdf", "xlsx":
		r, err := a.emuReportData()
		if err != nil {
			return nil, "", err
		}
		switch format {
		case "xlsx":
			b, err := emuReportXLSX(r)
			return b, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", err
		case "pdf":
			b, err := a.emuHTMLToPDF(emuReportHTML(r))
			return b, "application/pdf", err
		}
		return []byte(emuReportHTML(r)), "text/html; charset=utf-8", nil
	}
	v, err := a.EmuTraffic()
	if err != nil {
		return nil, "", err
	}
	if format == "json" {
		j, err := json.MarshalIndent(v, "", "  ")
		return j, "application/json", err
	}
	var b strings.Builder
	for _, r := range emuTrafficRows(v) { // csv
		for i := range r {
			if strings.ContainsAny(r[i], ",\"\n") {
				r[i] = `"` + strings.ReplaceAll(r[i], `"`, `""`) + `"`
			}
		}
		b.WriteString(strings.Join(r, ",") + "\n")
	}
	return []byte(b.String()), "text/csv; charset=utf-8", nil
}

// emuSortedTesters is for the connector: test ports by name.
func emuSortedTesters(ts []emu.Tester) []emu.Tester {
	out := append([]emu.Tester(nil), ts...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
