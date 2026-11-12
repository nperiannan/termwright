package emu

import (
	"strings"
	"testing"
)

// A boot that stopped at a menu is answered: only while the menu is the end of the log.
func TestBootStop(t *testing.T) {
	im := &Image{BootAnswers: []Answer{{Match: "c) continue with boot process", Send: "c"}}}
	menu := "loading...\n 8b) pacman autostart delay...\"5\"\n\nr) reboot\n\nc) continue with boot process\n~> "
	if a, ok := im.BootStop(menu); !ok || a.Send != "c" {
		t.Fatal("menu not seen")
	}
	if _, ok := im.BootStop(menu + strings.Repeat("Starting EXOS services...\n", 30)); ok {
		t.Fatal("answered a menu that is long gone")
	}
	im.BootAnswers = append(im.BootAnswers, Answer{Match: "/bin/sh: can't access tty; job control turned off", Send: "exit"})
	shell := "*** do_conditional_shell ***\n\n\nBusyBox v1.35.0 () built-in shell (ash)\nEnter 'help' for a list of built-in commands.\n\n/bin/sh: can't access tty; job control turned off\n~ # \n~ # "
	if a, ok := im.BootStop(menu + strings.Repeat("Mounting ro filesystem at /exos\n", 20) + shell); !ok || a.Send != "exit" {
		t.Fatal("shell not seen")
	}
	if _, ok := (&Image{}).BootStop(menu); ok {
		t.Fatal("no answers, no stop")
	}
}

// A new switch never gets an address another switch on the host already has
// (b kept .147 from before a switch was removed; c's slot gives .147 too).
func TestFreeAddress(t *testing.T) {
	l := (&Lab{Mgmt: "lan", LanFixed: true, MgmtNet: "10.234.179.146/24", CloudGw: "10.234.179.1",
		VMs: []VM{{ID: "a", Index: 0, MgmtIP: "10.234.179.146/24"}, {ID: "b", Index: 2, MgmtIP: "10.234.179.147/24"}, {ID: "c", Index: 1}}}).For("", "")
	if ip, _ := l.FreeAddress(l.VMs[2]); ip != "10.234.179.148/24" {
		t.Fatal(ip)
	}
	if ip, _ := l.FreeAddress(l.VMs[0]); ip != "10.234.179.146/24" {
		t.Fatal("a keeps its own", ip)
	}
}
