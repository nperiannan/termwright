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

// An L2 switch's ports (8-24), its VLANs and management address, a hub's 8 ports, and a cloud's ports.
func TestEmuHubPortsVLANsCloud(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(filepath.Join(dir, "Sessions"))
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = true, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	emuLab.f.Host = "Lab/kvm-1"
	emuLab.f.Images = []emu.Image{{ID: "i1", Name: "EXOS-VM", NICs: 13, PortName: "{n}"}}
	vms := []emu.VM{}
	for i := range 10 {
		vms = append(vms, emu.VM{ID: "v" + string(rune('a'+i)), Name: "sw" + string(rune('a'+i)), Image: "i1", Index: i})
	}
	emuLab.f.Lab = &emu.Lab{ID: "a1", VMs: vms, LanIf: "ens160"}
	v, err := a.EmuHubAdd("switch", "", 0, 0)
	if err != nil || v.Hubs[0].Ports != 8 {
		t.Fatalf("an L2 switch starts with 8 ports: %v %+v", err, v.Hubs)
	}
	if _, err := a.EmuHubSetPorts("l2sw-1", 25); err == nil {
		t.Error("25 ports")
	}
	if v, err = a.EmuHubSetPorts("l2sw-1", 12); err != nil || v.Hubs[0].Ports != 12 {
		t.Fatalf("12 ports: %v", err)
	}
	h, _ := a.EmuHubAdd("hub", "", 0, 0)
	if h.Hubs[1].Ports != 8 {
		t.Errorf("a hub has 8 ports: %d", h.Hubs[1].Ports)
	}
	if _, err := a.EmuHubSetPorts("hub-1", 12); err == nil {
		t.Error("a hub's ports changed")
	}
	for i := range 8 {
		if _, err := a.EmuHubConnect("hub-1", vms[i].ID, "1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.EmuHubConnect("hub-1", vms[8].ID, "1"); err == nil || !strings.Contains(err.Error(), "all its 8 ports") {
		t.Errorf("a 9th port on a hub: %v", err)
	}
	if v, err = a.EmuHubConnect("l2sw-1", "sw"+"a", "2"); err != nil || v.Hubs[0].Legs[0].At != 1 {
		t.Fatalf("its first port: %v %+v", err, v.Hubs[0].Legs)
	}
	if _, err := a.EmuHubPortSet("l2sw-1", 13, "access", 10, nil); err == nil {
		t.Error("port 13 of 12")
	}
	if _, err := a.EmuHubPortSet("l2sw-1", 2, "trunk", 0, []int{20, 10, 10}); err != nil {
		t.Fatal(err)
	}
	if v, err = a.EmuHubSetMgmt("l2sw-1", "10.20.0.5", 10); err != nil || v.Hubs[0].Cfg.MgmtIP != "10.20.0.5/24" || v.Hubs[0].Cfg.Rev != 2 || len(v.Hubs[0].Cfg.Ports["2"].VLANs) != 2 {
		t.Fatalf("management: %v %+v", err, v.Hubs[0].Cfg)
	}
	if _, err := a.EmuHubSetMgmt("l2sw-1", "fd00::5/64", 1); err == nil {
		t.Error("an IPv6 management address")
	}
	if _, err := a.EmuSetUplink("vj", "3", true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EmuSetUplink("vj", "4", true); err == nil || !strings.Contains(err.Error(), "all its 1 ports") {
		t.Errorf("a second cable on a 1-port cloud: %v", err)
	}
	if v, err = a.EmuSetCloudPorts("Lab/kvm-1", 2); err != nil || v.CloudPorts["Lab/kvm-1"] != 2 {
		t.Fatalf("cloud ports: %v %v", err, v.CloudPorts)
	}
	if _, err := a.EmuSetUplink("vj", "4", true); err != nil {
		t.Errorf("the second port: %v", err)
	}
	if _, err := a.EmuSetCloudPorts("Lab/kvm-1", 1); err == nil {
		t.Error("fewer cloud ports than in use")
	}
	if _, err := a.EmuSetCloudPorts("Lab/kvm-1", 9); err == nil {
		t.Error("9 cloud ports")
	}
}
