package main

import (
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/session"
)

func TestEmuChangeWhat(t *testing.T) {
	if w := emuChangeWhat("Claude wants to change CPU limits", "Claude (through the Termwright connector) asks to set the CPU of pc-1, pc-2."); w != "Set the CPU of pc-1, pc-2" {
		t.Fatalf("%q", w)
	}
	if w := emuChangeWhat("Claude wants to cable", ""); w != "Claude wants to cable" {
		t.Fatalf("%q", w)
	}
}

// A lab tool run through the connector is not asked and is kept in the history; the plan to revert
// it says what would be set back.
func TestEmuHistoryRecordAndPlan(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.mu.Lock()
	emuLab.loaded, emuLab.f = true, emuFile{Lab: &emu.Lab{VMs: []emu.VM{{ID: "v1", Name: "pc-1"}, {ID: "v2", Name: "pc-2"}}}}
	emuLab.mu.Unlock()
	emuHist.mu.Lock()
	emuHist.loaded, emuHist.list = false, nil
	emuHist.mu.Unlock()
	defer func() {
		emuLab.mu.Lock()
		emuLab.loaded, emuLab.f = false, emuFile{}
		emuLab.mu.Unlock()
		emuHist.mu.Lock()
		emuHist.loaded, emuHist.list = false, nil
		emuHist.mu.Unlock()
	}()
	if _, err := a.emuMCP("lab_cpu", map[string]any{"switches": []any{"pc-1"}, "request_m": float64(250), "limit_m": float64(1500)}); err != nil {
		t.Fatal(err)
	}
	cs := a.EmuChanges()
	if len(cs) != 1 || cs[0].Who != "Claude" || cs[0].Tool != "lab_cpu" || !strings.HasPrefix(cs[0].What, "Set the CPU of pc-1") || !cs[0].Revertible {
		t.Fatalf("history %+v", cs)
	}
	// a cable and a second device added after it
	emuLab.mu.Lock()
	emuLab.f.Lab.Cables = append(emuLab.f.Lab.Cables, emu.Cable{A: "v1", APort: 1, B: "v2", BPort: 1})
	emuLab.mu.Unlock()
	plan, err := a.EmuRevertPlan(cs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(plan, " | ")
	if !strings.Contains(all, "unplug the cable pc-1 port 1 - pc-2 port 1") || !strings.Contains(all, "CPU of pc-1 back to request none, limit none") {
		t.Fatalf("plan: %s", all)
	}
	// the history is kept on disk
	emuHist.mu.Lock()
	emuHist.loaded, emuHist.list = false, nil
	emuHist.mu.Unlock()
	if cs := a.EmuChanges(); len(cs) != 1 {
		t.Fatalf("after reading the file again: %+v", cs)
	}
	if _, err := a.EmuRevertPlan("nope"); err == nil {
		t.Fatal("an unknown change has a plan")
	}
	// a list-only call is not a change
	if _, err := a.emuMCP("lab_history", map[string]any{"action": "list"}); err != nil {
		t.Fatal(err)
	}
	if cs := a.EmuChanges(); len(cs) != 1 {
		t.Fatalf("lab_history was recorded: %+v", cs)
	}
}

// The AI panel's own lab commands are kept with the lab before them.
func TestEmuAgentChange(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.mu.Lock()
	emuLab.loaded, emuLab.f = true, emuFile{Lab: &emu.Lab{VMs: []emu.VM{{ID: "v1", Name: "core-1"}}}}
	emuLab.mu.Unlock()
	emuHist.mu.Lock()
	emuHist.loaded, emuHist.list = false, nil
	emuHist.mu.Unlock()
	defer func() {
		emuLab.mu.Lock()
		emuLab.loaded, emuLab.f = false, emuFile{}
		emuLab.mu.Unlock()
		emuHist.mu.Lock()
		emuHist.loaded, emuHist.list = false, nil
		emuHist.mu.Unlock()
	}()
	id := a.EmuAgentChangeBegin("lab stop core-1", "stop")
	a.EmuAgentChangeEnd(id, "ok")
	a.EmuAgentChangeEnd(id, "ok") // twice: kept once
	cs := a.EmuChanges()
	if len(cs) != 1 || cs[0].Who != "AI agent" || cs[0].Tool != "lab_stop" || cs[0].What != "Lab stop core-1" || !cs[0].Revertible || cs[0].Result != "ok" {
		t.Fatalf("%+v", cs)
	}
}
