package main

import (
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
