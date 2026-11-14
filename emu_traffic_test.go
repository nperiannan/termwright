package main

import (
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/emu"
)

func TestEmuCheckClients(t *testing.T) {
	p := func(n int) *int { return &n }
	for _, c := range []struct {
		c  *emu.Clients
		s  *emu.STP
		ok bool
	}{
		{&emu.Clients{N: 4000, DHCP: true}, &emu.STP{Mode: "rstp", Prio: p(4096)}, true},
		{&emu.Clients{N: 4001}, nil, false},
		{&emu.Clients{N: 5, MAC: "02:63:zz:00:00:00"}, nil, false},
		{&emu.Clients{N: 5, Groups: []string{"10.1.1.1"}}, nil, false},
		{nil, &emu.STP{Mode: "mstp"}, false},
		{nil, &emu.STP{Mode: "stp", Prio: p(100)}, false},
	} {
		if err := emuCheckClients(c.c, c.s); (err == nil) != c.ok {
			t.Errorf("%+v %+v: %v", c.c, c.s, err)
		}
	}
	tr := emu.Tester{STP: &emu.STP{Mode: "stp", TC: 2}}
	if err := emuArgProto(map[string]any{"stp": map[string]any{"mode": "rstp"}, "topology_change": true}, &tr); err != nil || tr.STP.Mode != "rstp" || tr.STP.TC != 3 {
		t.Errorf("%v %+v", err, tr.STP)
	}
	if err := emuArgProto(map[string]any{"clients": map[string]any{"n": 3, "dot1x": map[string]any{"user": "u{n}"}}}, &tr); err != nil || tr.Clients.N != 3 || tr.Clients.Dot1x.User != "u{n}" {
		t.Errorf("%v %+v", err, tr.Clients)
	}
	if err := emuArgProto(map[string]any{"topology_change": true}, &emu.Tester{}); err == nil {
		t.Error("a topology change without spanning tree")
	}
}

// Per test port statistics: a stream's frames are expected where it goes (the port it is to, every other port for a
// broadcast, the ports that joined its group, the sender for a ping to an address); a LAG's members count together.
func TestEmuPortResults(t *testing.T) {
	ts := []emu.Tester{{ID: "a"}, {ID: "b", Groups: []string{"239.1.1.1"}}, {ID: "c", Lag: "l1"}, {ID: "d", Lag: "l1"}}
	ss := []emu.Stream{
		{ID: "u", From: "a", To: "b"},            // unicast a → b
		{ID: "bc", From: "a", Kind: "arp"},       // broadcast: b, c, d
		{ID: "m", From: "a", DstIP: "239.1.1.1"}, // multicast: b joined
		{ID: "p", From: "b", Kind: "icmp"},       // ping to an address: back to b
		{ID: "g", From: "a", To: "c"},            // to the LAG through c
	}
	rs := []EmuStreamResult{{ID: "u", TX: 100}, {ID: "bc", TX: 10}, {ID: "m", TX: 20}, {ID: "p", TX: 5}, {ID: "g", TX: 40}}
	c := map[string]emuPortCount{
		"a": {TX: 170, RX: 3, TXB: 170 * 64, Lat: [3]float64{0, 0, 0}},
		"b": {TX: 5, RX: 125, SRX: map[string]int{"u": 90, "bc": 10, "m": 20, "p": 5}, Lat: [3]float64{12.5, 25, 1.25}, Err: 2},
		"c": {RX: 30, SRX: map[string]int{"bc": 10, "g": 25}},
		"d": {RX: 25, SRX: map[string]int{"bc": 10, "g": 15}},
	}
	ps := emuPortResults(ts, ss, rs, c)
	if len(ps) != 4 {
		t.Fatalf("%d ports", len(ps))
	}
	if a := ps[0]; a.Expected != 0 || a.Loss != 0 || a.LatAvg != -1 || a.TX != 170 || a.TxBytes != 170*64 {
		t.Fatalf("a: %+v", a)
	}
	// b: u (100, got 90), bc (10/10), m (20/20), p (5/5) → 135 expected, 125 got
	if b := ps[1]; b.Expected != 135 || b.Got != 125 || b.Loss != 7.41 || b.LatAvg != 0.5 || b.LatMax != 1.25 || b.Errors != 2 {
		t.Fatalf("b: %+v", b)
	}
	// c: bc (10/10) and g (40, 25 + 15 on its LAG partner d) → 50/50; d: bc only
	if c := ps[2]; c.Expected != 50 || c.Got != 50 || c.Loss != 0 {
		t.Fatalf("c: %+v", c)
	}
	if d := ps[3]; d.Expected != 10 || d.Got != 10 {
		t.Fatalf("d: %+v", d)
	}
}

// The ports' rates come from the counters' change between reads, and keep a minute of history.
func TestEmuPortRates(t *testing.T) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	emuTraf.pprev, emuTraf.phist = nil, nil
	defer func() { emuTraf.pprev, emuTraf.phist = nil, nil }()
	ps := []EmuPortResult{{ID: "a", TX: 0, RX: 0}}
	emuPortRates(ps, 1)
	ps = []EmuPortResult{{ID: "a", TX: 1000, RX: 500, TxBytes: 1000 * 105, RxBytes: 500 * 105}}
	emuPortRates(ps, 3)
	if ps[0].TxRate != 500 || ps[0].RxRate != 250 || ps[0].TxMbps != 0.5 || ps[0].RxMbps != 0.25 {
		t.Fatalf("%+v", ps[0])
	}
	for i := 0; i < 70; i++ {
		emuPortRates([]EmuPortResult{{ID: "a", TX: 1000 + i, RX: 500}}, float64(4+i))
	}
	if n := len(emuTraf.phist["a"]); n != 60 {
		t.Fatalf("history %d", n)
	}
}

// lab_traffic stats: a line per test port, a note before the first run.
func TestEmuPortStatsText(t *testing.T) {
	tv := EmuTrafficView{Testers: []emu.Tester{{ID: "t1", Name: "tp-1", VM: "v1", Port: 5}, {ID: "t2", Name: "tp-2", VM: "v2", Port: 6, PortName: "1:6"}}}
	sw := func(vm string) string { return map[string]string{"v1": "core-1", "v2": "core-2"}[vm] }
	if s := emuPortStatsText(tv, sw); !strings.Contains(s, "No counters yet") {
		t.Fatal(s)
	}
	tv.Ports = []EmuPortResult{{ID: "t1", TX: 1000, RX: 990, TxRate: 100, RxRate: 99, Expected: 1000, Got: 990, Loss: 1, LatAvg: 0.25, Errors: 3}, {ID: "t2", LatAvg: -1}}
	s := emuPortStatsText(tv, sw)
	if !strings.Contains(s, "tp-1") || !strings.Contains(s, "core-1 5") || !strings.Contains(s, "core-2 1:6") || !strings.Contains(s, "1.00") || !strings.Contains(s, "0.250") || strings.Count(s, "\n") != 4 {
		t.Fatal(s)
	}
}
