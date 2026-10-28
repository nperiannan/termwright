package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindImportSources(t *testing.T) {
	home := t.TempDir()
	mk := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := FindImportSources(home, "", "darwin"); len(got) != 0 {
		t.Fatalf("empty home: %v", got)
	}
	cfg := filepath.Join(home, "Library", "Application Support", "VanDyke", "SecureCRT", "Config")
	mk(filepath.Join(cfg, "Sessions", "sw1.ini"), "S:\"Hostname\"=10.0.0.1\n")
	mk(filepath.Join(home, ".putty", "sessions", "r1"), "HostName\\10.0.0.2\\\n")
	mk(filepath.Join(home, ".ssh", "config"), "Host x\n  HostName 10.0.0.3\n")
	mk(filepath.Join(home, ".ssh", "empty"), "")
	got := FindImportSources(home, "", "darwin")
	want := []string{"SecureCRT", "PuTTY", "OpenSSH"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i, w := range want {
		if got[i].App != w {
			t.Errorf("%d: %s, want %s", i, got[i].App, w)
		}
	}
	if got[0].Path != cfg {
		t.Errorf("SecureCRT path %s", got[0].Path)
	}

	// Windows: AppData for SecureCRT and MobaXterm, Documents for SuperPuTTY
	app := filepath.Join(home, "AppData", "Roaming")
	mk(filepath.Join(app, "VanDyke", "Config", "Sessions", "a.ini"), "x")
	mk(filepath.Join(home, "Documents", "SuperPuTTY", "Sessions.xml"), "<x/>")
	mk(filepath.Join(app, "MobaXterm", "MobaXterm.ini"), "[Bookmarks]\n")
	got = FindImportSources(home, app, "windows")
	apps := map[string]bool{}
	for _, s := range got {
		apps[s.App] = true
	}
	for _, w := range []string{"SecureCRT", "SuperPuTTY", "MobaXterm", "OpenSSH"} {
		if !apps[w] {
			t.Errorf("windows: %s not found in %v", w, got)
		}
	}
	if apps["PuTTY"] {
		t.Error("windows: PuTTY keeps its sessions in the registry, not a folder")
	}
}
