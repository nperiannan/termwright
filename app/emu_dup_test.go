package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"

	"github.com/termwright/termwright/internal/emu"
)

func TestEmuNextName(t *testing.T) {
	used := map[string]bool{"5520-24t-1": true, "5520-24t-2": true, "core": true, "sw01": true, "5520-24t-10": true}
	for in, want := range map[string]string{"5520-24t-1": "5520-24t-11", "core": "core-2", "sw01": "sw02", "leaf": "leaf-2"} {
		if got := emuNextName(in, used); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestEmuDupVM(t *testing.T) {
	serials := map[string]bool{"2238Q-40017": true, "2238Q-40018": true, "2238Q-50001": true}
	x, _ := macNum("00:04:96:aa:10:00")
	macs := []uint64{x}
	v := emu.VM{ID: "v1", Name: "5520-24t-1", Image: "i", Index: 3, CPUs: 2, RAM: 2048, Serial: "2238Q-40017", BaseMAC: "00:04:96:aa:10:00",
		HWSerial: "X", Day0Done: true, FastKey: "k", Addrs: map[int]string{1: "10.0.0.1/24"}, X: 100, Y: 50,
		Persona: &emu.Persona{Values: map[string]string{"a": "b"}, Applied: "h", Slots: []emu.Slot{{Num: 1, Model: "5520-24T", Serial: "2238Q-40017", Master: true}, {Num: 2, Model: "5520-24T", Serial: "2238Q-50001"}}}}
	c := emuDupVM(v, "5520-24t-2", 9, &emu.Image{}, serials, &macs)
	if c.ID == v.ID || c.Index != 9 || c.Name != "5520-24t-2" || c.CPUs != 2 || c.RAM != 2048 {
		t.Fatalf("copy: %+v", c)
	}
	if c.Serial != "2238Q-40019" || c.Persona.Slots[0].Serial != "2238Q-40019" || c.Persona.Slots[1].Serial != "2238Q-50002" {
		t.Errorf("serials: %s %s %s", c.Serial, c.Persona.Slots[0].Serial, c.Persona.Slots[1].Serial)
	}
	if c.BaseMAC != "00:04:96:aa:10:40" {
		t.Errorf("mac: %s", c.BaseMAC)
	}
	if c.HWSerial != "" || c.Day0Done || c.FastKey != "" || c.Addrs != nil || c.Persona.Applied != "" || c.Persona.Values["a"] != "b" {
		t.Errorf("copied what it should not: %+v", c)
	}
	c.Persona.Values["a"] = "z"
	if v.Persona.Values["a"] != "b" {
		t.Error("the persona is shared")
	}
}

func TestEmuDuplicate(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = false, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.f.Host = "kvm-1"
	emuLab.f.Sessions, emuLab.f.Consoles = map[string]string{}, map[string]string{}
	emuLab.f.Images = []emu.Image{{ID: "i", Name: "fake", NICs: 9}}
	emuLab.f.Lab = &emu.Lab{ID: "a1", Dir: "termwright-lab", Mgmt: "forward", MgmtNet: "10.99.0.0/24", Base: 40000,
		VMs: []emu.VM{{ID: "v1", Name: "core-1", Image: "i", Index: 0, Serial: "S0001", X: 100, Y: 60},
			{ID: "v2", Name: "5520-24t-1", Image: "i", Index: 1, Serial: "S0100", BaseMAC: "00:04:96:aa:10:00", X: 300, Y: 60},
			{ID: "v3", Name: "5520-24t-2", Image: "i", Index: 2, Serial: "S0101", X: 500, Y: 60}},
		Cables: []emu.Cable{{A: "v1", APort: 1, B: "v2", BPort: 1}, {A: "v2", APort: 2, B: "v3", BPort: 2}}}
	emuLab.mu.Unlock()

	if _, err := a.EmuDuplicate(EmuDupRequest{IDs: []string{"v2"}, Cables: true}); err != nil && !strings.Contains(err.Error(), "host") {
		t.Fatal(err)
	}
	l := emuLab.f.Lab
	c := l.VMs[3]
	if c.Name != "5520-24t-3" || c.Serial != "S0102" || c.BaseMAC != "00:04:96:aa:10:40" || c.Index != 3 || c.MgmtIP == "" {
		t.Fatalf("copy %+v", c)
	}
	// cabled like the original: to core-1 (its next free port) and to 5520-24t-2
	var to []string
	for _, k := range l.Cables[2:] {
		to = append(to, fmt.Sprintf("%s:%d-%s:%d", k.A, k.APort, k.B, k.BPort))
	}
	if strings.Join(to, " ") != c.ID+":1-v1:2 "+c.ID+":2-v3:1" {
		t.Fatalf("cables %v", to)
	}
	// two together, twice: the cable between them is copied, the others are not
	if _, err := a.EmuDuplicate(EmuDupRequest{IDs: []string{"v1", "v2"}, Count: 2}); err != nil && !strings.Contains(err.Error(), "host") {
		t.Fatal(err)
	}
	var names []string
	for _, v := range l.VMs[4:] {
		names = append(names, v.Name)
	}
	if strings.Join(names, " ") != "core-2 5520-24t-4 core-3 5520-24t-5" || len(l.Cables) != 6 {
		t.Fatalf("%v %d", names, len(l.Cables))
	}
	if k := l.Cables[4]; k.A != l.VMs[4].ID || k.B != l.VMs[5].ID || k.APort != 1 || k.BPort != 1 {
		t.Fatalf("pod cable %+v", k)
	}
}
