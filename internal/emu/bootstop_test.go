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
	if _, ok := (&Image{}).BootStop(menu); ok {
		t.Fatal("no answers, no stop")
	}
}
