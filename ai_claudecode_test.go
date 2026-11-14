package main

import (
	"runtime"
	"strings"
	"testing"
)

// The sign-in command is the one this computer's terminal runs: claude on its PATH, else the whole path.
func TestClaudeSignInCommand(t *testing.T) {
	for _, c := range []struct{ goos, bin, path, want string }{
		{"darwin", "/Users/n/.local/bin/claude", "/usr/bin:/Users/n/.local/bin", "claude"},
		{"darwin", "/Users/n/.local/bin/claude", "/usr/bin:/bin", "/Users/n/.local/bin/claude"},
		{"darwin", "/Users/n/My Apps/claude", "/usr/bin", "'/Users/n/My Apps/claude'"},
		{"windows", `C:\Users\n\.local\bin\claude.exe`, `C:\Windows;C:\Users\n\.local\bin`, "claude"},
		{"windows", `C:\Program Files\Claude\claude.exe`, `C:\Windows`, `& 'C:\Program Files\Claude\claude.exe'`},
	} {
		if got := claudeSignInCommand(c.goos, c.bin, c.path); got != c.want && !(c.goos == "windows" && runtime.GOOS != "windows" && c.want == "claude") {
			t.Errorf("%s %s: %q, want %q", c.goos, c.bin, got, c.want)
		}
	}
	if h := claudeSignInHint("darwin", "/x/claude", ""); !strings.HasPrefix(h, "Open Terminal and run: /x/claude") {
		t.Error(h)
	}
	if h := claudeSignInHint("windows", `C:\x\claude.exe`, ""); !strings.HasPrefix(h, "Open PowerShell and run: & ") {
		t.Error(h)
	}
}
