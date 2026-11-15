package main

import (
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/emu"
)

// The assistant finds what Termwright can do in the guides that ship with it, and says what Termwright runs now:
// its version and each KVM host with the VMs on it.
func TestAIHelpAndStatus(t *testing.T) {
	a := &App{}
	if h := a.AIHelp("is another team member using the device"); !strings.Contains(h, "Termwright "+Version) || !strings.Contains(h, "initials") {
		t.Fatalf("team presence not found:\n%s", h)
	}
	if h := a.AIHelp("install from ISO answer installer"); !strings.Contains(h, "Installed from ISO") && !strings.Contains(h, "by hand") {
		t.Fatalf("ISO install not found:\n%s", h)
	}
	if h := a.AIHelp("the and of"); !strings.Contains(h, "Say what to look up") {
		t.Fatalf("no words: %s", h)
	}
	emuLab.mu.Lock()
	emuLab.loaded = true
	emuLab.f.Hosts = []EmuHost{{Session: "Lab/kvm-host", Name: "kvm-host", Info: emu.HostInfo{OS: "Ubuntu 24.04", CPUs: 16, MemTotal: 65536, MemFree: 40960, KVM: "yes", Load: 1.5}}, {Session: "Lab/kvm-2", Name: "kvm-2"}}
	emuLab.f.Host = "Lab/kvm-host"
	emuLab.f.Images = []emu.Image{{ID: "x", Name: "EXOS-VM", Version: "33.6"}}
	emuLab.f.Lab = &emu.Lab{ID: "l", VMs: []emu.VM{{ID: "v1", Name: "leaf-1", Image: "x"}, {ID: "v2", Name: "leaf-2", Image: "x", Host: "Lab/kvm-2"}}}
	emuLab.mu.Unlock()
	defer func() { emuLab.mu.Lock(); emuLab.f = emuFile{}; emuLab.mu.Unlock() }()
	s := a.AIStatus()
	for _, want := range []string{"Termwright " + Version, "Emulated lab: 2 devices, 0 running; 2 KVM host(s).", "- kvm-host [Ubuntu 24.04, 16 CPUs, 64 GB memory (40 GB free), KVM yes, load 1.5] (the lab's host): leaf-1 (EXOS-VM 33.6, stopped)", "- kvm-2: leaf-2 (EXOS-VM 33.6, stopped)"} {
		if !strings.Contains(s, want) {
			t.Fatalf("status lacks %q:\n%s", want, s)
		}
	}
}
