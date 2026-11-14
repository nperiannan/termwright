package main

import (
	"testing"

	"github.com/termwright/termwright/internal/session"
)

// The built-in device types read their web server's state from their own check output.
func TestWebStateBuiltins(t *testing.T) {
	get := func(name string) (string, string, []string) {
		for _, d := range session.BuiltinDeviceTypes() {
			if d.Name == name {
				_, h, s, on, _, ok := d.Web()
				if !ok {
					t.Fatalf("%s: no web server commands", name)
				}
				return h, s, on
			}
		}
		t.Fatalf("no %s", name)
		return "", "", nil
	}
	h, s, on := get("EXOS")
	for _, c := range []struct {
		out         string
		http, https bool
	}{
		{"CLI idle timeout                 : Enabled (20 minutes)\nWeb access                       : Enabled (tcp port 80)\n", true, false},
		{"Web access                       : Enabled (tcp port 80)\n                                 : Enabled (SSL port 443)\n", true, true},
		{"Web access                       : Disabled\n", false, false},
	} {
		if gh, gs := webState(c.out, h, s); gh != c.http || gs != c.https {
			t.Errorf("EXOS %q: http %v https %v", c.out, gh, gs)
		}
	}
	if len(on) != 3 || on[2] != "enable web https" {
		t.Fatalf("EXOS on: %q", on)
	}
	h, s, _ = get("VOSS")
	for _, c := range []struct {
		out         string
		http, https bool
	}{
		{"Web Server Status         : enabled\nSecure Only               : enabled\nDef Https Port            : 443\n", false, true},
		{"Web Server Status         : enabled\nSecure Only               : disabled\n", true, true},
		{"Web Server Status         : disabled\nSecure Only               : enabled\n", false, false},
	} {
		if gh, gs := webState(c.out, h, s); gh != c.http || gs != c.https {
			t.Errorf("VOSS %q: http %v https %v", c.out, gh, gs)
		}
	}
	if webName("Emulated Lab/5520 24t-3") != "Emulated-Lab-5520-24t-3" || webName("..") != "device" {
		t.Fatal(webName("Emulated Lab/5520 24t-3"))
	}
	if e := webCommandError([]string{"ok\n", "\n% Invalid input detected at '^' marker.\n"}); e != "% Invalid input detected at '^' marker." {
		t.Fatalf("error: %q", e)
	}
}
