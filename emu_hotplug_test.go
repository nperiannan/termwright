package main

import (
	"testing"

	"github.com/termwright/termwright/internal/emu"
)

// The patch panel joins a cable's ports while at least one end started without it, from where each end's
// QEMU sends to where it listens; not between hosts, not when both started with it.
func TestPatchPairs(t *testing.T) {
	old := emuLab.f
	defer func() { emuLab.runCables, emuLab.f = nil, old }()
	l := &emu.Lab{ID: "p", Base: 20000, VMs: []emu.VM{{ID: "a", Index: 0}, {ID: "b", Index: 1}, {ID: "c", Index: 2, Host: "Lab/kvm-2"}}}
	emuLab.f = emuFile{Host: "Lab/kvm-1", Lab: l}
	l.Cables = []emu.Cable{{A: "a", APort: 2, B: "b", BPort: 3}, {A: "a", APort: 4, B: "c", BPort: 1}}
	up := map[string]float64{"a": 1, "b": 1, "c": 1}
	emuLab.runCables = map[string]map[int]string{"a": {}, "b": {}}
	a, b := l.VMs[0], l.VMs[1]
	ali, ase := l.TesterPorts(a, 2)
	bli, bse := l.TesterPorts(b, 3)
	p := emuPatchPairs(l, up)
	if len(p) != 1 || len(p["Lab/kvm-1"]) != 1 || p["Lab/kvm-1"][0] != [4]int{ali, ase, bli, bse} {
		t.Fatalf("neither started with it: %v", p)
	}
	emuLab.runCables["a"] = map[int]string{2: "b:3"}
	if p := emuPatchPairs(l, up); p["Lab/kvm-1"][0] != [4]int{l.UDPPort(b, 3), l.UDPPort(a, 2), bli, bse} {
		t.Fatalf("a started with it: %v", p)
	}
	emuLab.runCables["b"] = map[int]string{3: "a:2"}
	if p := emuPatchPairs(l, up); len(p) != 0 {
		t.Fatalf("both started with it: %v", p)
	}
	emuLab.runCables = map[string]map[int]string{}
	delete(up, "b")
	if p := emuPatchPairs(l, up); len(p) != 0 {
		t.Fatalf("b is not running: %v", p)
	}
	emuLab.runCables = nil
	emuCablesSeen(l, map[string]float64{"a": 1})
	if emuLab.runCables["a"][4] != "c:1" || emuLab.runCables["b"] != nil {
		t.Fatalf("seen: %v", emuLab.runCables)
	}
}
