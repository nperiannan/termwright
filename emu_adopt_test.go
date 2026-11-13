package main

import (
	"testing"

	"github.com/termwright/termwright/internal/emu"
)

// A switch found running (Termwright restarted) still needs its console typed when its day 0 or its fixed address never was.
func TestEmuNeedsConsole(t *testing.T) {
	fixed := &emu.Lab{Mgmt: "lan", LanFixed: true}
	dhcp := &emu.Lab{Mgmt: "lan"}
	voss := &emu.Image{Day0: []string{"enable"}, AddrSet: []string{"enable", "configure terminal"}}
	plain := &emu.Image{}
	for _, c := range []struct {
		name string
		l    *emu.Lab
		v    emu.VM
		im   *emu.Image
		want bool
	}{
		{"nothing typed yet", fixed, emu.VM{MgmtIP: "10.0.0.5/24"}, voss, true},
		{"day 0 done, address never typed", fixed, emu.VM{Day0Done: true, MgmtIP: "10.0.0.5/24"}, voss, true},
		{"address typed", fixed, emu.VM{Day0Done: true, MgmtIP: "10.0.0.5/24", AddrOn: "10.0.0.5/24"}, voss, false},
		{"address changed since", fixed, emu.VM{Day0Done: true, MgmtIP: "10.0.0.6/24", AddrOn: "10.0.0.5/24"}, voss, true},
		{"DHCP, day 0 done", dhcp, emu.VM{Day0Done: true}, voss, false},
		{"image without commands", fixed, emu.VM{MgmtIP: "10.0.0.5/24"}, plain, false},
		{"no lab", nil, emu.VM{}, voss, false},
	} {
		if got := emuNeedsConsole(c.l, c.v, c.im); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
