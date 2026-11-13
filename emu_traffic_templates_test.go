package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/session"
)

// The test library on a lab in memory: a ready test put on two test ports, the lab kept as a template and put in
// again with the roles swapped, and a pcap stream (its capture read, kept and counted).
func TestTrafficTemplatesAndPcap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(filepath.Join(dir, "Sessions"))
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = true, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	emuLab.f.Host = "Lab/kvm-1"
	emuLab.f.Images = []emu.Image{{ID: "i1", Name: "EXOS-VM", NICs: 9, PortName: "{n}"}}
	emuLab.f.Lab = &emu.Lab{ID: "a1", VMs: []emu.VM{{ID: "v1", Name: "core-1", Image: "i1"}, {ID: "v2", Name: "core-2", Image: "i1"}},
		Testers: []emu.Tester{{ID: "t1", Name: "Test port 1", VM: "v1", Port: 5, MAC: "02:74:77:00:00:01", IP: "10.1.0.1/24"}, {ID: "t2", Name: "Test port 2", VM: "v2", Port: 5, MAC: "02:74:77:00:00:02", IP: "10.1.0.2/24", Speed: 10000}}}
	for _, tp := range a.EmuTrafficTemplates() {
		for _, s := range tp.Streams {
			if !tp.Builtin || strings.IndexByte("ABC", s.From[0]) >= tp.Ports || s.To != "" && strings.IndexByte("ABC", s.To[0]) >= tp.Ports {
				t.Fatalf("%s: roles %s %s of %d", tp.Name, s.From, s.To, tp.Ports)
			}
		}
		if len(tp.Roles) != tp.Ports {
			t.Fatalf("%s: %d roles told for %d ports", tp.Name, len(tp.Roles), tp.Ports)
		}
	}
	tv, err := a.EmuTrafficTemplateApply("Unicast both ways", []string{"Test port 1", "t2"})
	if err != nil || len(tv.Streams) != 2 || tv.Streams[0].PPS != 2350 || tv.Streams[1].PPS != 23496 || tv.Streams[1].From != "t2" {
		t.Fatalf("apply: %v %+v", err, tv.Streams)
	}
	if _, err := a.EmuTrafficTemplateApply("b-rfc2544", []string{"t1", "t2"}); err != nil || len(a.EmuTests()) != 2 || a.EmuTests()[0].From != "t1" {
		t.Fatalf("tests: %v %+v", err, a.EmuTests())
	}
	if _, err := a.EmuTrafficTemplateApply("b-rtt", []string{"t1", "t1"}); err == nil {
		t.Error("one test port for two roles")
	}
	tp, err := a.EmuTrafficTemplateSave("mine", "both ways and 2544", nil)
	if err != nil || tp.Ports != 2 || len(tp.Streams) != 2 || len(tp.Tests) != 2 || tp.Roles[0] != "was Test port 1" {
		t.Fatalf("save: %v %+v", err, tp)
	}
	if _, err := a.EmuTrafficTemplateSave("IMIX both ways", "", nil); err == nil {
		t.Error("a template of yours took a built-in name")
	}
	tv, err = a.EmuTrafficTemplateApply("mine", []string{"t2", "t1"})
	if err != nil || len(tv.Streams) != 4 || tv.Streams[2].Name != "A to B (2)" || tv.Streams[2].From != "t2" || len(a.EmuTests()) != 4 || a.EmuTests()[3].Name != "RFC 2544 latency (2)" {
		t.Fatalf("apply mine: %v %+v %+v", err, tv.Streams, a.EmuTests())
	}
	if err := a.EmuTrafficTemplateRemove("mine"); err != nil || len(a.EmuTrafficTemplates()) != len(emuBuiltinTemplates()) {
		t.Fatalf("remove: %v", err)
	}
	// a pcap stream: classic pcap, big-endian here; kept little-endian with its frames counted
	var pc bytes.Buffer
	_ = binary.Write(&pc, binary.BigEndian, []uint32{0xa1b2c3d4, 0x00020004, 0, 0, 65535, 1})
	for i := 0; i < 3; i++ {
		fr := append([]byte{2, 0, 0, 0, 0, 9, 2, 0, 0, 0, 0, 8, 0x88, 0xb5}, bytes.Repeat([]byte{byte(i)}, 50)...)
		_ = binary.Write(&pc, binary.BigEndian, []uint32{100, uint32(i * 500000), uint32(len(fr)), uint32(len(fr))})
		pc.Write(fr)
	}
	f := filepath.Join(dir, "call.pcap")
	_ = os.WriteFile(f, pc.Bytes(), 0o644)
	tv, err = a.EmuStreamSave(emu.Stream{Name: "replay", From: "t1", To: "t2", Kind: "pcap", Pcap: f, PcapTiming: true})
	if err != nil {
		t.Fatal(err)
	}
	s := tv.Streams[len(tv.Streams)-1]
	if s.PcapID == "" || s.PcapFrames != 3 || s.PcapSecs != 1 {
		t.Fatalf("pcap stream %+v", s)
	}
	if b, err := os.ReadFile(filepath.Join(a.emuPcapDir(), s.PcapID+".pcap")); err != nil || binary.LittleEndian.Uint32(b) != 0xa1b2c3d4 {
		t.Fatalf("kept capture: %v", err)
	}
	_ = os.Remove(f) // moved away: the kept copy still serves a change of its other settings
	s.Loops = 3
	if _, err := a.EmuStreamSave(s); err != nil {
		t.Fatalf("change without the file: %v", err)
	}
	_ = os.WriteFile(f, []byte("\x0a\x0d\x0d\x0a not pcap"), 0o644)
	if _, err := a.EmuStreamSave(emu.Stream{Name: "ng", From: "t1", Kind: "pcap", Pcap: f}); err == nil || !strings.Contains(err.Error(), "pcapng") {
		t.Fatalf("pcapng: %v", err)
	}
	cfgB, _ := emuLab.f.Lab.TrafficConfig(func(emu.VM) bool { return true }, 1, map[string]bool{})
	if !strings.Contains(string(cfgB), `"pcapId":"`+s.PcapID+`"`) || !strings.Contains(string(cfgB), `"toMac":"02:74:77:00:00:02"`) {
		t.Fatalf("config: %s", cfgB)
	}
}
