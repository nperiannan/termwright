package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// Traffic tests: RFC 2544 (throughput, latency, frame loss rate, back-to-back),
// RFC 2889 address caching (learning) and convergence (traffic while a cable is
// pulled or a switch stops), run by Termwright as a series of trials on the
// traffic generator. One test runs at a time; its streams are made for each
// trial and removed after (they are marked Test and not shown with the user's).

// EmuTestRow is one line of a test's results.
type EmuTestRow struct {
	Dir       string  `json:"dir,omitempty"`       // the direction (From → To), when the test runs both ways
	Size      int     `json:"size,omitempty"`      // frame bytes
	PPS       float64 `json:"pps,omitempty"`       // throughput: the highest rate without loss; frame-loss: the rate tried
	Mbps      float64 `json:"mbps,omitempty"`      // the rate in Mbit/s of frames (pps × size × 8)
	Load      float64 `json:"load,omitempty"`      // frame-loss: % of the top rate
	Loss      float64 `json:"loss"`                // % lost
	LatMin    float64 `json:"latMin,omitempty"`    // ms
	LatAvg    float64 `json:"latAvg,omitempty"`    // ms
	LatMax    float64 `json:"latMax,omitempty"`    // ms
	Jitter    float64 `json:"jitter,omitempty"`    // ms
	Burst     int     `json:"burst,omitempty"`     // back-to-back: the longest burst without loss (frames)
	Addresses int     `json:"addresses,omitempty"` // learning: the most source MACs learned without flooding
	Lost      int     `json:"lost,omitempty"`      // convergence: frames lost
	OutageMs  float64 `json:"outageMs,omitempty"`  // convergence: lost frames ÷ rate, in ms
	Retrans   int     `json:"retrans,omitempty"`   // iperf TCP: retransmitted segments
	Trials    int     `json:"trials,omitempty"`    // trials it took
	Note      string  `json:"note,omitempty"`      // what happened (convergence: the action), or why there is no number
}

// EmuTestRun is a run of a test: its progress, then its results.
type EmuTestRun struct {
	ID      string       `json:"id"`              // the run
	Test    string       `json:"test"`            // the test's id
	Name    string       `json:"name"`            // the test's name
	Kind    string       `json:"kind"`            // the test's kind
	Status  string       `json:"status"`          // running, done, failed, stopped
	Step    string       `json:"step,omitempty"`  // what it does now
	Started string       `json:"started"`         // RFC 3339
	Ended   string       `json:"ended,omitempty"` // RFC 3339
	Rows    []EmuTestRow `json:"rows"`            // the results so far
	Log     []string     `json:"log,omitempty"`   // every trial and its outcome
	Error   string       `json:"error,omitempty"` // why it failed
}

var emuTests struct {
	sync.Mutex
	cur    *EmuTestRun
	cancel context.CancelFunc
	runs   []EmuTestRun // the last ones, newest last
}

var emuTestKinds = []string{"throughput", "latency", "frame-loss", "back-to-back", "learning", "convergence", "iperf"}

// EmuTests lists the saved tests.
func (a *App) EmuTests() []emu.TrafficTest {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	a.emuLoad()
	if emuLab.f.Lab == nil {
		return []emu.TrafficTest{}
	}
	return append([]emu.TrafficTest{}, emuLab.f.Lab.Tests...)
}

// EmuTestSave adds a test (no id) or changes one.
func (a *App) EmuTestSave(t emu.TrafficTest) ([]emu.TrafficTest, error) {
	defer emuAs("user")()
	if !slices.Contains(emuTestKinds, t.Kind) {
		return nil, fmt.Errorf("a test is %s", strings.Join(emuTestKinds, ", "))
	}
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	l := emuLab.f.Lab
	if l == nil {
		return nil, errors.New("there is no emulated lab")
	}
	vmID := func(n string) string {
		for _, v := range l.VMs {
			if v.ID == n || strings.EqualFold(v.Name, n) {
				return v.ID
			}
		}
		return ""
	}
	if t.Kind == "iperf" {
		t.Server, t.Client = vmID(t.Server), vmID(t.Client)
		if t.Server == "" || t.Client == "" || t.Server == t.Client {
			return nil, errors.New("an iperf test needs two host VMs of the lab (with iperf3): the server and the client")
		}
		if t.Proto != "" && t.Proto != "tcp" && t.Proto != "udp" {
			return nil, errors.New("proto is tcp or udp")
		}
	} else if l.TesterByID(t.From) == nil || l.TesterByID(t.To) == nil || t.From == t.To {
		return nil, errors.New("choose two test ports: the one the frames go from and the one they go to")
	}
	if t.Kind == "learning" && (l.TesterByID(t.Monitor) == nil || t.Monitor == t.From || t.Monitor == t.To) {
		return nil, errors.New("a learning test needs a third test port that watches for flooded frames")
	}
	if t.Kind == "convergence" {
		if !slices.Contains([]string{"pull", "stop", "restart"}, t.Action) {
			return nil, errors.New("a convergence test pulls a cable, or stops or restarts a switch: action is pull, stop or restart")
		}
		if l.VMByID(t.Switch) == nil {
			found := false
			for _, v := range l.VMs {
				if strings.EqualFold(v.Name, t.Switch) {
					t.Switch, found = v.ID, true
				}
			}
			if !found {
				return nil, errors.New("no switch " + t.Switch)
			}
		}
		if t.Action == "pull" && t.Port == "" {
			return nil, errors.New("give the port whose cable is pulled")
		}
	}
	for _, sz := range t.Sizes {
		if sz < 60 || sz > 9000 {
			return nil, fmt.Errorf("a frame size is 60-9000 bytes, not %d", sz)
		}
	}
	t.Name = strings.TrimSpace(t.Name)
	if t.ID == "" {
		t.ID = "x" + randHex(3)
		t.Name = orStr(t.Name, fmt.Sprintf("%s-%d", t.Kind, len(l.Tests)+1))
		l.Tests = append(l.Tests, t)
	} else if i := slices.IndexFunc(l.Tests, func(x emu.TrafficTest) bool { return x.ID == t.ID }); i >= 0 {
		t.Name = orStr(t.Name, l.Tests[i].Name)
		l.Tests[i] = t
	} else {
		return nil, errors.New("no such test")
	}
	if err := a.emuSave(); err != nil {
		return nil, err
	}
	return append([]emu.TrafficTest{}, l.Tests...), nil
}

// EmuTestRemove deletes a saved test.
func (a *App) EmuTestRemove(id string) ([]emu.TrafficTest, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	l := emuLab.f.Lab
	if l == nil {
		return nil, errors.New("there is no emulated lab")
	}
	n := len(l.Tests)
	l.Tests = slices.DeleteFunc(l.Tests, func(x emu.TrafficTest) bool { return x.ID == id })
	if len(l.Tests) == n {
		return nil, errors.New("no such test")
	}
	return append([]emu.TrafficTest{}, l.Tests...), a.emuSave()
}

// EmuTestRuns: the test running now (if one is) and the last runs, newest first.
func (a *App) EmuTestRuns() []EmuTestRun {
	emuTests.Lock()
	defer emuTests.Unlock()
	var out []EmuTestRun
	if emuTests.cur != nil {
		out = append(out, emuTestCopy(*emuTests.cur))
	}
	for i := len(emuTests.runs) - 1; i >= 0; i-- {
		out = append(out, emuTests.runs[i])
	}
	return out
}

func emuTestCopy(r EmuTestRun) EmuTestRun {
	r.Rows, r.Log = append([]EmuTestRow{}, r.Rows...), append([]string{}, r.Log...)
	return r
}

// EmuTestRun starts a saved test; it runs in the background (EmuTestRuns shows it).
func (a *App) EmuTestRun(id string) (EmuTestRun, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	var t *emu.TrafficTest
	if l := emuLab.f.Lab; l != nil {
		for i := range l.Tests {
			if l.Tests[i].ID == id || strings.EqualFold(l.Tests[i].Name, id) {
				x := l.Tests[i]
				t = &x
			}
		}
	}
	running := emuTraf.run != 0 && !emuTraf.final
	emuLab.mu.Unlock()
	if t == nil {
		return EmuTestRun{}, errors.New("no such test")
	}
	emuTests.Lock()
	if emuTests.cur != nil {
		emuTests.Unlock()
		return EmuTestRun{}, errors.New(emuTests.cur.Name + " is running: stop it first")
	}
	if running {
		if v, _ := a.EmuTraffic(); v.Running {
			emuTests.Unlock()
			return EmuTestRun{}, errors.New("traffic is running: stop it first")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &EmuTestRun{ID: "r" + randHex(3), Test: t.ID, Name: t.Name, Kind: t.Kind, Status: "running", Started: time.Now().Format(time.RFC3339), Rows: []EmuTestRow{}}
	emuTests.cur, emuTests.cancel = r, cancel
	out := emuTestCopy(*r)
	emuTests.Unlock()
	who := emuWho()
	emuEv("", "info", fmt.Sprintf("test %s (%s) started", t.Name, t.Kind))
	emuGo(func() {
		defer emuAs(who)()
		err := a.emuTestDo(ctx, *t)
		a.emuTestCleanup()
		emuTests.Lock()
		cur := emuTests.cur
		cur.Ended, cur.Step = time.Now().Format(time.RFC3339), ""
		switch {
		case ctx.Err() != nil:
			cur.Status = "stopped"
		case err != nil:
			cur.Status, cur.Error = "failed", err.Error()
		default:
			cur.Status = "done"
		}
		emuTests.runs = append(emuTests.runs, emuTestCopy(*cur))
		if len(emuTests.runs) > 20 {
			emuTests.runs = emuTests.runs[len(emuTests.runs)-20:]
		}
		emuTests.cur, emuTests.cancel = nil, nil
		emuTests.Unlock()
		lvl := map[string]string{"done": "ok", "failed": "error", "stopped": "warn"}[cur.Status]
		emuEv("", lvl, fmt.Sprintf("test %s %s%s", cur.Name, cur.Status, map[bool]string{true: ": " + cur.Error, false: ""}[cur.Error != ""]))
	})
	return out, nil
}

// EmuTestStop stops the running test (its trial's traffic too).
func (a *App) EmuTestStop() error {
	emuTests.Lock()
	c := emuTests.cancel
	emuTests.Unlock()
	if c == nil {
		return errors.New("no test is running")
	}
	c()
	return nil
}

func emuTestStep(f string, x ...any) {
	emuTests.Lock()
	defer emuTests.Unlock()
	if emuTests.cur != nil {
		s := fmt.Sprintf(f, x...)
		emuTests.cur.Step = s
		emuTests.cur.Log = append(emuTests.cur.Log, time.Now().Format("15:04:05")+" "+s)
	}
}

func emuTestRow(r EmuTestRow) {
	emuTests.Lock()
	defer emuTests.Unlock()
	if emuTests.cur != nil {
		emuTests.cur.Rows = append(emuTests.cur.Rows, r)
	}
}

// emuTestCleanup removes the trial streams.
func (a *App) emuTestCleanup() {
	_, _ = a.EmuTrafficStop()
	emuLab.mu.Lock()
	if l := emuLab.f.Lab; l != nil {
		l.Streams = slices.DeleteFunc(l.Streams, func(s emu.Stream) bool { return s.Test != "" })
		_ = a.emuSave()
	}
	emuLab.mu.Unlock()
}

// emuTrial runs streams once and gives their results by stream id (after the frames on their way arrived).
func (a *App) emuTrial(ctx context.Context, t emu.TrafficTest, ss []emu.Stream, mid func(time.Duration)) (map[string]EmuStreamResult, error) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return nil, errors.New("there is no emulated lab")
	}
	l.Streams = slices.DeleteFunc(l.Streams, func(s emu.Stream) bool { return s.Test != "" })
	var ids []string
	longest := 0.0
	for i := range ss {
		ss[i].ID, ss[i].Test = "z"+randHex(3), t.ID
		ss[i].Name = fmt.Sprintf("%s#%d", t.Name, i+1)
		ss[i].Kind = orStr(ss[i].Kind, "udp")
		ss[i].VLAN = orInt(ss[i].VLAN, t.VLAN)
		ss[i].Size = min(9000, max(60, orInt(ss[i].Size, 128)))
		ss[i].PPS = max(1, ss[i].PPS)
		ids = append(ids, ss[i].ID)
		n := float64(orInt(ss[i].Count, ss[i].Seconds*ss[i].PPS)) / float64(ss[i].PPS)
		longest = math.Max(longest, n)
		l.Streams = append(l.Streams, ss[i])
	}
	emuLab.mu.Unlock()
	if _, err := a.emuTrafficStartQuiet(ids); err != nil {
		return nil, err
	}
	start := time.Now()
	limit := time.Duration(longest*float64(time.Second)) + 40*time.Second
	for {
		select {
		case <-ctx.Done():
			_, _ = a.EmuTrafficStop()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
		if mid != nil {
			mid(time.Since(start))
		}
		v, err := a.emuTrafficAll()
		if err != nil {
			return nil, err
		}
		emuLab.mu.Lock()
		final := emuTraf.final
		emuLab.mu.Unlock()
		if final || time.Since(start) > limit {
			out := map[string]EmuStreamResult{}
			for _, r := range v.Results {
				if slices.Contains(ids, r.ID) {
					out[r.ID] = r
				}
			}
			return out, nil
		}
	}
}

func emuLossOf(r EmuStreamResult) float64 {
	if r.Expected == 0 {
		return 100
	}
	return math.Max(0, float64(r.Expected-r.RX)*100/float64(r.Expected))
}

func (a *App) emuTestDo(ctx context.Context, t emu.TrafficTest) error {
	sizes := t.Sizes
	if len(sizes) == 0 {
		sizes = []int{64, 128, 256, 512, 1024, 1280, 1518}
	}
	if t.Kind == "convergence" || t.Kind == "learning" {
		sizes = sizes[:1]
	}
	trial := max(2, orInt(t.Trial, 5))
	maxPPS := orInt(t.MaxPPS, 10000)
	res := math.Max(0.1, orFloat(t.Resolution, 2)) / 100
	names := a.emuTesterNames()
	dirs := [][2]string{{t.From, t.To}}
	if t.Both && t.Kind != "back-to-back" && t.Kind != "learning" {
		dirs = append(dirs, [2]string{t.To, t.From})
	}
	dirName := func(d [2]string) string { return names[d[0]] + " → " + names[d[1]] }
	streams := func(size, pps, count, burst int) []emu.Stream {
		var ss []emu.Stream
		for _, d := range dirs {
			ss = append(ss, emu.Stream{From: d[0], To: d[1], Size: size, PPS: pps, Count: count, Burst: burst, MaxLoss: 100})
		}
		return ss
	}
	// pass: every direction lost no more than the tolerance
	ok := func(rs map[string]EmuStreamResult) bool {
		if len(rs) == 0 {
			return false
		}
		for _, r := range rs {
			if r.TX == 0 || emuLossOf(r) > t.Loss {
				return false
			}
		}
		return true
	}
	mbps := func(pps float64, size int) float64 { return math.Round(pps*float64(size)*8/1e4) / 100 }
	search := func(what string, top int, try func(n int) (bool, error)) (int, int, error) {
		return emuSearch(top, res, func(n int) (bool, error) {
			emuTestStep("%s: trying %d", what, n)
			return try(n)
		})
	}
	switch t.Kind {
	case "throughput", "latency":
		for _, size := range sizes {
			var best int
			var trials int
			var err error
			if t.Kind == "throughput" || t.Rate == 0 {
				best, trials, err = search(fmt.Sprintf("%d B throughput", size), maxPPS, func(n int) (bool, error) {
					rs, err := a.emuTrial(ctx, t, streams(size, n, n*trial, 0), nil)
					return ok(rs), err
				})
				if err != nil {
					return err
				}
			} else {
				best = t.Rate
			}
			if t.Kind == "throughput" {
				for _, d := range dirs {
					emuTestRow(EmuTestRow{Dir: dirName(d), Size: size, PPS: float64(best), Mbps: mbps(float64(best), size), Trials: trials, Note: map[bool]string{true: "no rate without loss", false: ""}[best == 0]})
				}
				continue
			}
			if best == 0 {
				emuTestRow(EmuTestRow{Size: size, Note: "no rate without loss: latency not measured"})
				continue
			}
			emuTestStep("%d B latency at %d pps", size, best)
			rs, err := a.emuTrial(ctx, t, streams(size, best, best*max(trial, 10), 0), nil)
			if err != nil {
				return err
			}
			i := 0
			for _, d := range dirs {
				r := emuResultFor(rs, i)
				i++
				emuTestRow(EmuTestRow{Dir: dirName(d), Size: size, PPS: float64(best), Mbps: mbps(float64(best), size), Loss: emuLossOf(r), LatMin: r.LatMin, LatAvg: r.LatAvg, LatMax: r.LatMax, Jitter: r.Jitter, Trials: trials + 1})
			}
		}
	case "frame-loss":
		top := orInt(t.Rate, maxPPS)
		for _, size := range sizes {
			clean := 0
			for load := 100; load >= 10 && clean < 2; load -= 10 {
				pps := max(1, top*load/100)
				emuTestStep("%d B frame loss at %d %% (%d pps)", size, load, pps)
				rs, err := a.emuTrial(ctx, t, streams(size, pps, pps*trial, 0), nil)
				if err != nil {
					return err
				}
				worst := 0.0
				for i, d := range dirs {
					r := emuResultFor(rs, i)
					worst = math.Max(worst, emuLossOf(r))
					emuTestRow(EmuTestRow{Dir: dirName(d), Size: size, PPS: float64(pps), Mbps: mbps(float64(pps), size), Load: float64(load), Loss: math.Round(emuLossOf(r)*100) / 100})
				}
				if worst <= t.Loss {
					clean++
				} else {
					clean = 0
				}
			}
		}
	case "back-to-back":
		for _, size := range sizes {
			best, trials, err := search(fmt.Sprintf("%d B burst", size), orInt(t.Max, 10000), func(n int) (bool, error) {
				rs, err := a.emuTrial(ctx, t, streams(size, maxPPS, n, n), nil)
				return ok(rs), err
			})
			if err != nil {
				return err
			}
			emuTestRow(EmuTestRow{Dir: dirName(dirs[0]), Size: size, Burst: best, Trials: trials, Note: map[bool]string{true: "the longest burst tried", false: ""}[best == orInt(t.Max, 10000)]})
		}
	case "learning":
		emuLab.mu.Lock()
		var from *emu.Tester
		if l := emuLab.f.Lab; l != nil {
			if x := l.TesterByID(t.From); x != nil {
				c := *x
				from = &c
			}
		}
		emuLab.mu.Unlock()
		if from == nil {
			return errors.New("no test port " + t.From)
		}
		size := sizes[0]
		best, trials, err := search("addresses learned", orInt(t.Max, 4096), func(n int) (bool, error) {
			pps := min(maxPPS, max(100, n))
			// learn: n frames from n source MACs (From's MAC + 0…n-1)
			if _, err := a.emuTrial(ctx, t, []emu.Stream{{From: t.From, To: t.To, Size: size, PPS: pps, Count: n, MaxLoss: 100, Mods: []emu.Mod{{Field: "srcMac", Mode: "inc", Count: n, Step: 1}}}}, nil); err != nil {
				return false, err
			}
			// then frames to every one of them: learned ones reach From only; any at Monitor were flooded
			rs, err := a.emuTrial(ctx, t, []emu.Stream{{From: t.To, To: t.From, Size: size, PPS: pps, Count: n, MaxLoss: 100, DstMAC: from.MAC, Mods: []emu.Mod{{Field: "dstMac", Mode: "inc", Count: n, Step: 1}}}}, nil)
			if err != nil {
				return false, err
			}
			r := emuResultFor(rs, 0)
			return r.RX >= n && r.Other == 0, nil
		})
		if err != nil {
			return err
		}
		emuTestRow(EmuTestRow{Dir: dirName(dirs[0]), Size: size, Addresses: best, Trials: trials, Note: "learned without flooding to " + names[t.Monitor]})
	case "iperf":
		return a.emuIperf(ctx, t)
	case "convergence":
		rate := orInt(t.Rate, 1000)
		secs := orInt(t.Seconds, 30)
		at := time.Duration(orInt(t.At, 5)) * time.Second
		hold := time.Duration(t.Hold) * time.Second
		emuLab.mu.Lock()
		port, sw := t.Port, t.Switch
		swName := sw
		if l := emuLab.f.Lab; l != nil {
			if v := l.VMByID(sw); v != nil {
				swName = v.Name
			}
		}
		emuLab.mu.Unlock()
		did, undone := false, false
		var actErr error
		act := func(el time.Duration) {
			if !did && el >= at {
				did = true
				switch t.Action {
				case "pull":
					emuTestStep("pulling the cable of %s %s", swName, port)
					_, actErr = a.EmuSetLink(sw, port, false)
				case "stop":
					emuTestStep("stopping %s", swName)
					_, actErr = a.EmuStop([]string{sw})
				case "restart":
					emuTestStep("restarting %s", swName)
					_, actErr = a.EmuRestart([]string{sw})
				}
			}
			if did && !undone && hold > 0 && el >= at+hold && t.Action != "restart" {
				undone = true
				switch t.Action {
				case "pull":
					emuTestStep("plugging the cable of %s %s in again", swName, port)
					_, _ = a.EmuSetLink(sw, port, true)
				case "stop":
					emuTestStep("starting %s again", swName)
					_, _ = a.EmuStart([]string{sw})
				}
			}
		}
		emuTestStep("traffic at %d pps for %d s; %s at %d s", rate, secs, t.Action, int(at.Seconds()))
		rs, err := a.emuTrial(ctx, t, streams(sizes[0], rate, rate*secs, 0), act)
		if t.Action == "pull" && did && !undone { // a pulled cable is plugged in again at the end of the test
			_, _ = a.EmuSetLink(sw, port, true)
		}
		if err != nil {
			return err
		}
		if actErr != nil {
			return fmt.Errorf("the %s did not work: %w", t.Action, actErr)
		}
		for i, d := range dirs {
			r := emuResultFor(rs, i)
			lost := max(0, r.Expected-r.RX)
			emuTestRow(EmuTestRow{Dir: dirName(d), Size: sizes[0], PPS: float64(rate), Loss: math.Round(emuLossOf(r)*100) / 100, Lost: lost, OutageMs: math.Round(float64(lost) * 1000 / float64(rate)), Note: fmt.Sprintf("%s %s %s at %d s", t.Action, swName, port, int(at.Seconds()))})
		}
	}
	return nil
}

// emuIperf runs iperf3 between two host VMs of the lab, on their consoles.
func (a *App) emuIperf(ctx context.Context, t emu.TrafficTest) error {
	emuLab.mu.Lock()
	var srv, cli emu.VM
	if l := emuLab.f.Lab; l != nil {
		if v := l.VMByID(t.Server); v != nil {
			srv = *v
		}
		if v := l.VMByID(t.Client); v != nil {
			cli = *v
		}
	}
	emuLab.mu.Unlock()
	if srv.ID == "" || cli.ID == "" {
		return errors.New("the server or client VM is gone")
	}
	ip := t.ServerIP
	for _, p := range slices.Sorted(maps.Keys(srv.Addrs)) { // across the switches: its first data port with a fixed address
		if a := srv.Addrs[p]; ip == "" && a != "dhcp" {
			ip = strings.Split(a, "/")[0]
		}
	}
	ip = orStr(ip, strings.Split(srv.MgmtIP, "/")[0])
	if ip == "" {
		return errors.New("give the server's address (serverIp): " + srv.Name + " has no management address")
	}
	secs := orInt(t.Seconds, 10)
	missing := func(out, name string) error {
		if strings.Contains(out, "not found") || strings.Contains(out, "No such file") {
			return fmt.Errorf("iperf3 is not on %s: install it there (Alpine: apk add iperf3, Debian/Ubuntu: apt install iperf3)", name)
		}
		return nil
	}
	emuTestStep("starting the iperf3 server on %s", srv.Name)
	out, err := a.emuConsoleRun(srv.ID, []string{"pkill iperf3; iperf3 -s -D -1 && echo tw-iperf-ok"}, 90*time.Second)
	if e := missing(out, srv.Name); e != nil {
		return e
	}
	if err != nil {
		return fmt.Errorf("%s: %w", srv.Name, err)
	}
	cmd := fmt.Sprintf("iperf3 -c %s -t %d -i 0 -J", ip, secs)
	if t.Proto == "udp" {
		cmd += " -u -b " + orStr(t.Bandwidth, "1M")
	} else if t.Bandwidth != "" {
		cmd += " -b " + t.Bandwidth
	}
	if t.Parallel > 1 {
		cmd += " -P " + strconv.Itoa(t.Parallel)
	}
	if t.Reverse {
		cmd += " -R"
	}
	emuTestStep("%s on %s", cmd, cli.Name)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	out, err = a.emuConsoleRun(cli.ID, []string{cmd}, time.Duration(secs+90)*time.Second)
	if e := missing(out, cli.Name); e != nil {
		return e
	}
	if err != nil {
		return fmt.Errorf("%s: %w", cli.Name, err)
	}
	r, err := emuIperfParse(out)
	if err != nil {
		return err
	}
	dir := cli.Name + " → " + srv.Name
	if t.Reverse {
		dir = srv.Name + " → " + cli.Name
	}
	r.Dir, r.Note = dir, strings.ToUpper(orStr(t.Proto, "tcp"))+" to "+ip
	emuTestRow(r)
	return nil
}

// emuIperfParse reads iperf3 -J output (as a console showed it) into a result row.
func emuIperfParse(out string) (EmuTestRow, error) {
	i, j := strings.Index(out, "{"), strings.LastIndex(out, "}")
	if i < 0 || j < i {
		return EmuTestRow{}, fmt.Errorf("iperf3 printed no result: %s", strings.TrimSpace(emu.CleanConsole(out)))
	}
	type sum struct {
		BitsPerSecond float64 `json:"bits_per_second"`
		Retransmits   float64 `json:"retransmits"`
		JitterMs      float64 `json:"jitter_ms"`
		LostPercent   float64 `json:"lost_percent"`
		LostPackets   int     `json:"lost_packets"`
		Packets       int     `json:"packets"`
	}
	var r struct {
		Error string `json:"error"`
		End   struct {
			SumSent     sum `json:"sum_sent"`
			SumReceived sum `json:"sum_received"`
			Sum         sum `json:"sum"`
		} `json:"end"`
	}
	raw := strings.NewReplacer("\r\n", "\n", "\r", "").Replace(out[i : j+1])
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return EmuTestRow{}, fmt.Errorf("iperf3's result could not be read: %w", err)
	}
	if r.Error != "" {
		return EmuTestRow{}, errors.New("iperf3: " + r.Error)
	}
	row := EmuTestRow{}
	if r.End.Sum.JitterMs > 0 || r.End.Sum.Packets > 0 { // udp
		row.Mbps, row.Jitter, row.Loss, row.Lost = r.End.Sum.BitsPerSecond/1e6, r.End.Sum.JitterMs, r.End.Sum.LostPercent, r.End.Sum.LostPackets
	} else {
		row.Mbps, row.Retrans = r.End.SumReceived.BitsPerSecond/1e6, int(r.End.SumSent.Retransmits)
	}
	row.Mbps = math.Round(row.Mbps*100) / 100
	return row, nil
}

// emuSearch finds the highest n in [1, top] for which try passes, by halving the gap until it is
// below res (a fraction) of the result; 0 when even 1 fails. It gives the trials it took too.
func emuSearch(top int, res float64, try func(n int) (bool, error)) (int, int, error) {
	lo, hi, trials := 0, top, 0
	n := top
	for {
		trials++
		pass, err := try(n)
		if err != nil {
			return 0, trials, err
		}
		if pass {
			lo = n
		} else {
			hi = n
		}
		if lo == top || hi-lo <= max(1, int(float64(max(lo, 1))*res)) {
			return lo, trials, nil
		}
		n = lo + (hi-lo+1)/2
	}
}

// emuResultFor: the result of the i-th stream of a trial (in the order they were made).
func emuResultFor(rs map[string]EmuStreamResult, i int) EmuStreamResult {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	if l := emuLab.f.Lab; l != nil {
		k := 0
		for _, s := range l.Streams {
			if s.Test == "" {
				continue
			}
			if k == i {
				return rs[s.ID]
			}
			k++
		}
	}
	return EmuStreamResult{}
}

func (a *App) emuTesterNames() map[string]string {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	out := map[string]string{}
	if l := emuLab.f.Lab; l != nil {
		for _, t := range l.Testers {
			out[t.ID] = t.Name
		}
	}
	return out
}

func orFloat(v, d float64) float64 {
	if v == 0 {
		return d
	}
	return v
}

// EmuTestRunReport is a run's results as csv, html, json or text, and its media type.
func (a *App) EmuTestRunReport(id, format string) ([]byte, string, error) {
	var r *EmuTestRun
	for _, x := range a.EmuTestRuns() {
		if x.ID == id {
			x := x
			r = &x
			break
		}
	}
	if r == nil {
		return nil, "", errors.New("no such run")
	}
	if format == "json" {
		b, err := json.MarshalIndent(r, "", "  ")
		return b, "application/json", err
	}
	head := []string{"direction", "frame bytes", "rate pps", "Mbit/s", "load %", "loss %", "latency min ms", "latency avg ms", "latency max ms", "jitter ms", "burst", "addresses", "lost", "outage ms", "trials", "note"}
	f := func(v float64) string {
		if v == 0 {
			return ""
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	i := func(v int) string {
		if v == 0 {
			return ""
		}
		return strconv.Itoa(v)
	}
	rows := [][]string{head}
	for _, x := range r.Rows {
		rows = append(rows, []string{x.Dir, i(x.Size), f(x.PPS), f(x.Mbps), f(x.Load), strconv.FormatFloat(x.Loss, 'f', -1, 64), f(x.LatMin), f(x.LatAvg), f(x.LatMax), f(x.Jitter), i(x.Burst), i(x.Addresses), i(x.Lost), f(x.OutageMs), i(x.Trials), x.Note})
	}
	var b strings.Builder
	switch format {
	case "csv":
		w := csv.NewWriter(&b)
		_ = w.WriteAll(rows)
		return []byte(b.String()), "text/csv; charset=utf-8", nil
	case "html":
		fmt.Fprintf(&b, "<!doctype html><meta charset=utf-8><title>%s</title><style>body{font:14px system-ui,sans-serif;margin:24px;color:#222}table{border-collapse:collapse}td,th{border-bottom:1px solid #ddd;padding:6px 10px;text-align:left;white-space:nowrap}th{color:#666;font-weight:500}</style>", html.EscapeString(r.Name))
		fmt.Fprintf(&b, "<h2>%s</h2><p>%s test · %s · started %s%s · Termwright emulated lab</p><table>", html.EscapeString(r.Name), html.EscapeString(r.Kind), html.EscapeString(r.Status), html.EscapeString(r.Started), map[bool]string{true: " · " + html.EscapeString(r.Error), false: ""}[r.Error != ""])
		for k, row := range rows {
			b.WriteString("<tr>")
			for _, c := range row {
				tag := map[bool]string{true: "th", false: "td"}[k == 0]
				fmt.Fprintf(&b, "<%s>%s</%s>", tag, html.EscapeString(c), tag)
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</table>")
		return []byte(b.String()), "text/html; charset=utf-8", nil
	}
	fmt.Fprintf(&b, "%s (%s): %s", r.Name, r.Kind, r.Status)
	if r.Step != "" {
		b.WriteString(" - " + r.Step)
	}
	if r.Error != "" {
		b.WriteString(" - " + r.Error)
	}
	b.WriteString("\n")
	for _, row := range rows[1:] {
		var parts []string
		for k, c := range row {
			if c != "" {
				parts = append(parts, head[k]+" "+c)
			}
		}
		b.WriteString("- " + strings.Join(parts, ", ") + "\n")
	}
	return []byte(b.String()), "text/plain; charset=utf-8", nil
}

// EmuTestReportSave saves a run's results on this computer: .csv, or .html (anything else).
func (a *App) EmuTestReportSave(id, path string) error {
	b, _, err := a.EmuTestRunReport(id, map[bool]string{true: "csv", false: "html"}[strings.EqualFold(filepath.Ext(path), ".csv")])
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
