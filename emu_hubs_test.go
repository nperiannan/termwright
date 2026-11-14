package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/session"
)

// Hubs and L2 switches on a lab in memory: names, legs on free ports only, one host, the free ports and cables
// that see them, and taking them off.
func TestEmuHubs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(filepath.Join(dir, "Sessions"))
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = true, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	emuLab.f.Host = "Lab/kvm-1"
	emuLab.f.Images = []emu.Image{{ID: "i1", Name: "EXOS-VM", NICs: 9, PortName: "{n}"}}
	emuLab.f.Lab = &emu.Lab{ID: "a1", VMs: []emu.VM{{ID: "v1", Name: "sw1", Image: "i1"}, {ID: "v2", Name: "sw2", Image: "i1", Index: 1}, {ID: "v3", Name: "sw3", Image: "i1", Index: 2, Host: "Lab/kvm-2"}},
		Cables: []emu.Cable{{A: "v1", APort: 1, B: "v2", BPort: 1}}}
	v, err := a.EmuHubAdd("switch", "", 100, 200)
	if err != nil || len(v.Hubs) != 1 || v.Hubs[0].Name != "l2sw-1" || v.Hubs[0].Kind != "switch" {
		t.Fatalf("add: %v %+v", err, v.Hubs)
	}
	if _, err := a.EmuHubAdd("hub", "sw1", 0, 0); err == nil {
		t.Error("a hub took a switch's name")
	}
	if _, err := a.EmuHubConnect("l2sw-1", "sw1", "1"); err == nil || !strings.Contains(err.Error(), "not free") {
		t.Errorf("a cabled port: %v", err)
	}
	if v, err = a.EmuHubConnect("l2sw-1", "sw1", "2"); err != nil || len(v.Hubs[0].Legs) != 1 || v.Hubs[0].Legs[0].PortName != "2" || v.Hubs[0].Legs[0].Name != "sw1" {
		t.Fatalf("connect: %v %+v", err, v.Hubs)
	}
	if _, err := a.EmuHubConnect("l2sw-1", "v3", "2"); err == nil || !strings.Contains(err.Error(), "one host") {
		t.Errorf("another host: %v", err)
	}
	if free := a.EmuFreePorts("v1"); strings.Contains(strings.Join(free, ","), "2,") || free[0] != "3" {
		t.Errorf("free ports %v", free)
	}
	if _, err := a.EmuConnect(EmuLinkView{A: "v1", APort: "2", B: "v2", BPort: "2"}); err == nil {
		t.Error("a cable on a hub port")
	}
	if l := emuLab.f.Lab; len(l.DownLinks(l.VMs[0], &emuLab.f.Images[0])) != 6 { // 8 data ports: 1 cabled, 1 on the hub
		t.Errorf("down links %v", l.DownLinks(l.VMs[0], &emuLab.f.Images[0]))
	}
	if v, err = a.EmuHubDisconnect(v.Hubs[0].ID, "v1", 2); err != nil || len(v.Hubs[0].Legs) != 0 {
		t.Fatalf("disconnect: %v %+v", err, v.Hubs)
	}
	if v, err = a.EmuHubRemove("l2sw-1"); err != nil || len(v.Hubs) != 0 {
		t.Fatalf("remove: %v %+v", err, v.Hubs)
	}
}
