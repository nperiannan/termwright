package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// The traffic generator: test ports on free switch ports and streams of
// frames between them, sent and counted by traffic.py on each host.

// EmuStreamResult is a stream's counters (all hosts added up) and its verdict.
type EmuStreamResult struct {
	ID       string  `json:"id"`
	TX       int     `json:"tx"`
	RX       int     `json:"rx"`
	Expected int     `json:"expected"` // frames that should arrive (to every other test port for a broadcast)
	Other    int     `json:"other"`    // arrived at a test port it was not for (flooded)
	Loss     float64 `json:"loss"`     // %
	LatAvg   float64 `json:"latAvg"`   // ms (-1 = not measured: the ends on different hosts)
	LatMax   float64 `json:"latMax"`
	Running  bool    `json:"running"`
	Result   string  `json:"result"` // "", pass, fail
	Why      string  `json:"why,omitempty"`
}

// EmuTrafficView is the Traffic tab.
type EmuTrafficView struct {
	Testers []emu.Tester      `json:"testers"`
	Streams []emu.Stream      `json:"streams"`
	Results []EmuStreamResult `json:"results"`
	Running bool              `json:"running"`
	Started string            `json:"started,omitempty"`
	Note    string            `json:"note,omitempty"` // something to do (restart a switch for its test port)
}

var emuTraf struct {
	run     uint32
	started time.Time
	on      map[string]bool
	last    []EmuStreamResult
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
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return errors.New("there is no emulated lab")
	}
	type job struct{ host, script string }
	var jobs []job
	for _, h := range l.Hosts(emuLab.f.Host) {
		lh := a.emuFor(h)
		cfg, n := lh.TrafficConfig(func(v emu.VM) bool { return emuHostOf(v) == h }, emuTraf.run, emuTraf.on)
		if n == 0 && !emuLab.trafHosts[h] {
			continue
		}
		emuLab.trafHosts[h] = n > 0
		jobs = append(jobs, job{h, lh.TrafficScript(cfg)})
	}
	emuLab.mu.Unlock()
	var errs []string
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
	_, _ = h.do(fmt.Sprintf("set_link nic%d %s", port, map[bool]string{true: "on", false: "off"}[up]), 10*time.Second)
	return ""
}

// EmuTraffic is the Traffic tab: test ports, streams and their counters now.
func (a *App) EmuTraffic() (EmuTrafficView, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{Testers: []emu.Tester{}, Streams: []emu.Stream{}, Results: []EmuStreamResult{}}, nil
	}
	v := EmuTrafficView{Testers: append([]emu.Tester{}, l.Testers...), Streams: append([]emu.Stream{}, l.Streams...)}
	var hosts []string
	for _, t := range l.Testers {
		if vm := l.VMByID(t.VM); vm != nil && !slices.Contains(hosts, emuHostOf(*vm)) {
			hosts = append(hosts, emuHostOf(*vm))
		}
	}
	dir := l.VMDir()
	run := emuTraf.run
	emuLab.mu.Unlock()
	type stats struct {
		Run   uint32               `json:"run"`
		TX    map[string]int       `json:"tx"`
		RX    map[string]int       `json:"rx"`
		Other map[string]int       `json:"other"`
		Lat   map[string][]float64 `json:"lat"`
		Err   map[string]string    `json:"err"`
		Busy  map[string]bool      `json:"busy"`
	}
	sum := stats{TX: map[string]int{}, RX: map[string]int{}, Other: map[string]int{}, Lat: map[string][]float64{}, Err: map[string]string{}, Busy: map[string]bool{}}
	if run != 0 || emuTraf.last != nil {
		for _, h := range hosts {
			out, err := a.emuRun(h, "cat "+emu.Q(dir)+"/traffic-stats.json 2>/dev/null", 15*time.Second)
			var st stats
			if err != nil || json.Unmarshal([]byte(strings.TrimSpace(out)), &st) != nil || st.Run != run && run != 0 {
				continue
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
				if len(x) == 3 {
					s := sum.Lat[k]
					if s == nil {
						s = []float64{0, 0, 0}
					}
					s[0], s[1], s[2] = s[0]+x[0], math.Max(s[1], x[1]), s[2]+x[2]
					sum.Lat[k] = s
				}
			}
			for k, e := range st.Err {
				sum.Err[k] = e
			}
			for k, b := range st.Busy {
				sum.Busy[k] = sum.Busy[k] || b
			}
		}
	}
	for _, s := range v.Streams {
		r := EmuStreamResult{ID: s.ID, TX: sum.TX[s.ID], RX: sum.RX[s.ID], Other: sum.Other[s.ID], Running: sum.Busy[s.ID] && run != 0, LatAvg: -1}
		recv := 1
		if s.To == "" && s.Kind != "icmp" { // a broadcast reaches every other test port; a ping to an address comes back
			recv = max(0, len(v.Testers)-1)
		}
		r.Expected = r.TX * recv
		if r.Expected > 0 {
			r.Loss = math.Max(0, float64(r.Expected-r.RX)*100/float64(r.Expected))
		}
		if x := sum.Lat[s.ID]; len(x) == 3 && x[2] > 0 {
			r.LatAvg, r.LatMax = x[0]/x[2], x[1]
		}
		switch e := sum.Err[s.ID]; {
		case e != "":
			r.Result, r.Why = "fail", e
		case r.Running || r.TX == 0:
		case r.Loss > s.MaxLoss:
			r.Result, r.Why = "fail", fmt.Sprintf("loss %.1f %% > %.1f %%", r.Loss, s.MaxLoss)
		case s.MaxLatency > 0 && r.LatAvg > s.MaxLatency:
			r.Result, r.Why = "fail", fmt.Sprintf("latency %.2f ms > %.2f ms", r.LatAvg, s.MaxLatency)
		default:
			r.Result = "pass"
		}
		v.Results = append(v.Results, r)
	}
	if v.Results == nil {
		v.Results = []EmuStreamResult{}
	}
	emuLab.mu.Lock()
	if run != 0 {
		v.Running = slices.ContainsFunc(v.Results, func(r EmuStreamResult) bool { return r.Running }) || time.Since(emuTraf.started) < 3*time.Second
		v.Started = emuTraf.started.Format(time.RFC3339)
		if !v.Running && time.Since(emuTraf.started) > 3*time.Second {
			emuTraf.last = v.Results
		}
	}
	emuLab.mu.Unlock()
	return v, nil
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
	if ip != "" && !strings.Contains(ip, "/") {
		ip += "/24"
	}
	t := emu.Tester{ID: "t" + randHex(3), Name: emuTesterName(l), VM: vmID, Port: n, MAC: "02:74:77:" + randHex(1) + ":" + randHex(1) + ":" + randHex(1), IP: ip, GW: gw, VLAN: vlan}
	t.MAC = strings.ToLower(t.MAC)
	l.Testers = append(l.Testers, t)
	_ = a.emuSave()
	emuEv(vmID, "info", fmt.Sprintf("%s on port %s%s", t.Name, emuImage(v.Image).PortNameOf(n), map[bool]string{true: " (" + ip + ")", false: ""}[ip != ""]))
	emuLab.mu.Unlock()
	note := a.emuTesterLink(vmID, n, true)
	err := a.emuTrafficPush()
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
	if t.IP != "" && !strings.Contains(t.IP, "/") {
		t.IP += "/24"
	}
	moved := cur.X != t.X || cur.Y != t.Y
	cur.Name, cur.IP, cur.GW, cur.VLAN, cur.X, cur.Y = orStr(strings.TrimSpace(t.Name), cur.Name), t.IP, t.GW, t.VLAN, t.X, t.Y
	_ = a.emuSave()
	emuLab.mu.Unlock()
	if !moved {
		if err := a.emuTrafficPush(); err != nil {
			return EmuTrafficView{}, err
		}
	}
	return a.EmuTraffic()
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
	if !slices.Contains([]string{"udp", "icmp", "tcp", "arp", "raw"}, s.Kind) {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("the kind is udp, icmp, tcp, arp or raw")
	}
	if s.Kind != "arp" && s.Kind != "raw" && s.To == "" && s.DstIP == "" {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("an IP stream needs a destination test port or address")
	}
	if s.Kind != "arp" && s.Kind != "raw" && (l.TesterByID(s.From).IP == "" && s.SrcIP == "") {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("give the test port it goes from an IP address (or the stream a source address)")
	}
	s.Size = min(9000, max(60, orInt(s.Size, 128)))
	s.PPS = min(100000, max(1, orInt(s.PPS, 100)))
	if s.Seconds <= 0 && s.Count <= 0 {
		s.Seconds = 10
	}
	s.VLAN, s.Prio = min(4094, max(0, s.VLAN)), min(7, max(0, s.Prio))
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
	emuTraf.run, emuTraf.started, emuTraf.on, emuTraf.last = uint32(time.Now().Unix()), time.Now(), on, nil
	emuEv("", "info", "traffic started: "+strings.Join(names, ", "))
	emuLab.mu.Unlock()
	if err := a.emuTrafficPush(); err != nil {
		return EmuTrafficView{}, err
	}
	return a.EmuTraffic()
}

// EmuTrafficStop stops sending (the counters stay for the report).
func (a *App) EmuTrafficStop() (EmuTrafficView, error) {
	defer emuAs("user")()
	v, _ := a.EmuTraffic()
	emuLab.mu.Lock()
	run := emuTraf.run
	emuTraf.last = v.Results
	emuTraf.on = map[string]bool{}
	emuLab.mu.Unlock()
	if run == 0 {
		return v, nil
	}
	// the stopped configuration: no stream on, the same run (traffic.py keeps the counts)
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
	rows := [][]string{{"stream", "from", "to", "kind", "vlan", "size", "pps", "sent", "expected", "received", "loss %", "latency avg ms", "latency max ms", "result", "why"}}
	for _, s := range v.Streams {
		r := res[s.ID]
		lat, latMax := "-", "-"
		if r.LatAvg >= 0 {
			lat, latMax = fmt.Sprintf("%.3f", r.LatAvg), fmt.Sprintf("%.3f", r.LatMax)
		}
		rows = append(rows, []string{s.Name, name[s.From], orStr(name[s.To], "all"), s.Kind, fmt.Sprint(s.VLAN), fmt.Sprint(s.Size), fmt.Sprint(s.PPS), fmt.Sprint(r.TX), fmt.Sprint(r.Expected), fmt.Sprint(r.RX), fmt.Sprintf("%.2f", r.Loss), lat, latMax, orStr(strings.ToUpper(r.Result), "-"), r.Why})
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
		fmt.Fprintf(&b, "- %s (%s → %s, %s, VLAN %s, %s B, %s pps): sent %s, received %s of %s, loss %s %%, latency %s ms avg / %s max: %s %s\n", r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7], r[9], r[8], r[10], r[11], r[12], r[13], r[14])
	}
	if b.Len() == 0 {
		return "No stream yet.", nil
	}
	return b.String(), nil
}

// EmuTrafficReport saves the report on this computer: .csv, or .html (anything else).
func (a *App) EmuTrafficReport(path string) error {
	v, err := a.EmuTraffic()
	if err != nil {
		return err
	}
	rows := emuTrafficRows(v)
	var b strings.Builder
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		for _, r := range rows {
			for i := range r {
				if strings.ContainsAny(r[i], ",\"\n") {
					r[i] = `"` + strings.ReplaceAll(r[i], `"`, `""`) + `"`
				}
			}
			b.WriteString(strings.Join(r, ",") + "\n")
		}
	} else {
		emuLab.mu.Lock()
		topo := orStr(emuLab.f.Topology, "Untitled")
		emuLab.mu.Unlock()
		pass, fail := 0, 0
		for _, r := range v.Results {
			pass += map[bool]int{true: 1}[r.Result == "pass"]
			fail += map[bool]int{true: 1}[r.Result == "fail"]
		}
		fmt.Fprintf(&b, "<!doctype html><meta charset=utf-8><title>Traffic report - %s</title><style>body{font:14px system-ui,sans-serif;margin:24px;color:#222}table{border-collapse:collapse}td,th{border-bottom:1px solid #ddd;padding:6px 10px;text-align:left;white-space:nowrap}th{color:#666;font-weight:500}.pass{color:#1a7f37;font-weight:600}.fail{color:#cf222e;font-weight:600}</style>", html.EscapeString(topo))
		fmt.Fprintf(&b, "<h2>Traffic report - %s</h2><p>%s · %d passed, %d failed, %d streams · Termwright emulated lab</p><table>", html.EscapeString(topo), time.Now().Format("2006-01-02 15:04"), pass, fail, len(v.Streams))
		for i, r := range rows {
			b.WriteString("<tr>")
			for j, c := range r {
				tag, cls := "td", ""
				if i == 0 {
					tag = "th"
				} else if j == 13 {
					cls = ` class="` + strings.ToLower(c) + `"`
				}
				fmt.Fprintf(&b, "<%s%s>%s</%s>", tag, cls, html.EscapeString(c), tag)
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</table>")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// emuSortedTesters is for the connector: test ports by name.
func emuSortedTesters(ts []emu.Tester) []emu.Tester {
	out := append([]emu.Tester(nil), ts...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
