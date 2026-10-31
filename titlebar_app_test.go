package main

import "testing"

func TestParseTitleBarAction(t *testing.T) {
	for in, want := range map[string]string{"Maximize\n": "maximize", "Fill": "maximize", "Minimize\n": "minimize", "None": "none", "": "maximize"} {
		if got := parseTitleBarAction(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
