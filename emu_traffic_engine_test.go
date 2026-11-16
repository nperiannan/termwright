package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/trex"
)

// TRex ports as test ports, against a fake TRex server: the server checked, two ports used, a stream with a changing
// address sent there (its frame, field program and flow statistics as TRex takes them), and its results and report.
func TestTrafficTrexFake(t *testing.T) {
	f, err := trex.NewFake()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(filepath.Join(dir, "Sessions"))
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f, emuLab.trafHosts = true, emuFile{}, map[string]bool{}
	emuTraf.trex, emuTraf.engNote = nil, nil
	defer func() {
		emuLab.loaded, emuLab.f, emuTraf.trex, emuTraf.engNote, emuTraf.run = false, emuFile{}, nil, nil, 0
	}()
	emuLab.f.Host = "Lab/kvm-1"
	emuLab.f.Lab = &emu.Lab{ID: "a1"}
	host, port, _ := net.SplitHostPort(f.Addr())
	p, _ := strconv.Atoi(port)
	e, err := a.EmuTrexSave(EmuTrex{Name: "trex-1", Addr: host, Port: p})
	if err != nil || len(e.Trex) != 1 || e.Trex[0].State != "connected" || e.Trex[0].Version != "v3.04" || len(e.Trex[0].Ports) != 2 || e.Trex[0].Ports[0].Speed != 10000 {
		t.Fatalf("server %+v %v", e, err)
	}
	id := e.Trex[0].ID
	for i, cab := range []string{"core-1 1/1", "core-1 1/2"} {
		if e, err = a.EmuTrexPort(id, i, true, cab); err != nil || !e.Trex[0].Ports[i].Use {
			t.Fatalf("port %d: %+v %v", i, e, err)
		}
	}
	tv, _ := a.EmuTraffic()
	if len(tv.Testers) != 2 || tv.Testers[0].MAC != "0c:42:a1:00:00:10" || tv.Testers[1].Trex != id+"/1" {
		t.Fatalf("testers %+v", tv.Testers)
	}
	for i := range tv.Testers {
		x := tv.Testers[i]
		x.IP = "10.9.0." + strconv.Itoa(i+1)
		if _, err := a.EmuTesterSet(x); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.EmuStreamSave(emu.Stream{Name: "s", From: tv.Testers[0].ID, To: tv.Testers[1].ID, Kind: "session"}); err == nil || !strings.Contains(err.Error(), "TRex") {
		t.Fatalf("a session stream on TRex ports: %v", err)
	}
	tv, err = a.EmuStreamSave(emu.Stream{Name: "line rate", From: tv.Testers[0].ID, To: tv.Testers[1].ID, Kind: "udp", Size: 128, PPS: 5000000, Count: 5000,
		Mods: []emu.Mod{{Field: "srcIp", Mode: "inc", Count: 10}}})
	if err != nil || tv.Streams[0].PPS != 5000000 {
		t.Fatalf("stream: %v %+v", err, tv.Streams)
	}
	if _, err := a.EmuTrafficStart(nil); err != nil {
		t.Fatal(err)
	}
	if f.Called("acquire") != 2 || f.Called("start_traffic") != 1 || len(f.Streams[0]) != 2 {
		t.Fatalf("calls %v, streams %d", f.Calls, len(f.Streams[0]))
	}
	s0, l0 := f.Streams[0][0], f.Streams[0][1]
	j, _ := json.Marshal(s0)
	for _, w := range []string{`"flags":3`, `"rule_type":"stats"`, `"stream_id":100`, `"type":"flow_var"`, `"max_value":168361994`, `"pkt_offset":26`, `"fix_checksum_ipv4"`, `"total_pkts":5000`} {
		if !strings.Contains(string(j), w) {
			t.Fatalf("stream has no %s: %s", w, j)
		}
	}
	if fs := l0["flow_stats"].(map[string]any); fs["rule_type"] != "latency" || fs["stream_id"] != float64(103) {
		t.Fatalf("latency stream %v", l0)
	}
	b, _ := base64.StdEncoding.DecodeString(s0["packet"].(map[string]any)["binary"].(string))
	if len(b) != 124 || net.HardwareAddr(b[0:6]).String() != "0c:42:a1:00:00:11" || binary.BigEndian.Uint16(b[12:]) != 0x0800 || emuCsum(b[14:34], 0) != 0 || binary.BigEndian.Uint16(b[16:]) != 110 || binary.BigEndian.Uint16(b[38:]) != 90 {
		t.Fatalf("frame %x", b)
	}
	tv, _ = a.EmuTraffic()
	r := tv.Results[0]
	if r.TX != 5000 || r.RX != 5000 || r.Result != "pass" || r.LatAvg != 0.0125 || r.LatMax != 0.04 {
		t.Fatalf("result %+v", r)
	}
	if len(tv.Ports) != 2 || tv.Ports[1].Got != 5000 {
		t.Fatalf("ports %+v", tv.Ports)
	}
	html, _, err := a.emuTrafficReportAs("html")
	if err != nil || !strings.Contains(string(html), "TRex trex-1 port 0, cabled to core-1 1/1") || !strings.Contains(string(html), `stroke-dasharray="5 3"`) {
		t.Fatalf("report: %v", err)
	}
	if _, err := a.EmuTrafficStop(); err != nil || f.Called("stop_traffic") == 0 {
		t.Fatalf("stop: %v", err)
	}
	if e, err = a.EmuTrexRemove(id); err != nil || len(e.Trex) != 0 {
		t.Fatal(err)
	}
	if tv, _ = a.EmuTraffic(); len(tv.Testers) != 0 || len(tv.Streams) != 0 {
		t.Fatalf("left %+v", tv)
	}
}

// A host's spare network card as a test port: in that host's traffic.json (with the fast engine), run as root.
func TestTrafficNICConfig(t *testing.T) {
	l := &emu.Lab{ID: "a1", Engine: "fast", Testers: []emu.Tester{{ID: "t1", Name: "Test port 1", Host: "Lab/kvm-2", NIC: "enp3s0", MAC: "02:74:77:00:00:01", IP: "10.1.0.1/24"},
		{ID: "t2", Name: "Test port 2", Trex: "x1/0", MAC: "02:74:77:00:00:02"}}}
	b, n := l.TrafficConfig(func(emu.VM) bool { return true }, "Lab/kvm-2", 1, nil)
	if n != 1 || !strings.Contains(string(b), `"nic":"enp3s0"`) || !strings.Contains(string(b), `"fast":true`) {
		t.Fatalf("%d %s", n, b)
	}
	if _, n := l.TrafficConfig(func(emu.VM) bool { return true }, "Lab/kvm-1", 1, nil); n != 0 {
		t.Fatal("another host's card")
	}
	sc := l.TrafficScript(b, true)
	for _, w := range []string{"sudo -n true", "$SU ./tw-fast", "$SU python3 tw-traffic.py", "traffic.mode"} {
		if !strings.Contains(sc, w) {
			t.Fatalf("script has no %s", w)
		}
	}
}

// A session stream's result from its sessions, for each thing it should do.
func TestTrafficSessionResult(t *testing.T) {
	c := func() *EmuSessionResult {
		return &EmuSessionResult{Tried: 100, OK: 98, Refused: 1, Timeout: 1, Done: 98, SetupAvg: 1.5, SetupMax: 4, Secs: 2}
	}
	for _, x := range []struct {
		exp, res string
		loss     float64
		c        *EmuSessionResult
	}{
		{"", "pass", 5, c()},
		{"", "fail", 1, c()},
		{"refused", "fail", 0, c()},
		{"refused", "pass", 0, &EmuSessionResult{Tried: 10, Refused: 10}},
		{"dropped", "pass", 0, &EmuSessionResult{Tried: 10, Timeout: 10}},
		{"dropped", "fail", 0, &EmuSessionResult{Tried: 10, Refused: 1, Timeout: 9}},
	} {
		r := EmuStreamResult{}
		emuSessResult(&r, emu.Stream{Kind: "session", MaxLoss: x.loss, Session: &emu.Session{Expect: x.exp}}, x.c, "")
		if r.Result != x.res {
			t.Fatalf("%q, loss %g: %s (%s)", x.exp, x.loss, r.Result, r.Why)
		}
	}
	r := EmuStreamResult{}
	emuSessResult(&r, emu.Stream{Kind: "session", MaxLoss: 5}, c(), "")
	if r.TX != 100 || r.RX != 98 || r.Loss != 2 || r.LatAvg != 1.5 || r.Sessions.CPS != 49 {
		t.Fatalf("%+v %+v", r, r.Sessions)
	}
}
