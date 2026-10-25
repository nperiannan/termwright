package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixMovedPaths(t *testing.T) {
	old, nw := filepath.Join("/x", "OpenCRT"), filepath.Join("/x", "Termwright")
	s := Settings{SessionsDir: filepath.Join(old, "Config", "Sessions"), LogDir: "/elsewhere/logs", DownloadDir: old}
	if !fixMovedPaths(&s, old, nw) || s.SessionsDir != filepath.Join(nw, "Config", "Sessions") || s.LogDir != "/elsewhere/logs" || s.DownloadDir != nw {
		t.Fatalf("%+v", s)
	}
	s2 := Settings{SessionsDir: "/x/OpenCRTother/Sessions"}
	if fixMovedPaths(&s2, old, nw) {
		t.Fatal("prefix of a different folder was rewritten")
	}
}

func TestLoadAfterMove(t *testing.T) {
	base := t.TempDir()
	old, nw := filepath.Join(base, "OpenCRT"), filepath.Join(base, "Termwright")
	os.MkdirAll(old, 0o700)
	m, _ := Load(old)
	s := m.Get()
	s.SessionsDir = filepath.Join(old, "Config", "Sessions")
	m.Save(s)
	if err := os.Rename(old, nw); err != nil {
		t.Fatal(err)
	}
	movedFrom = old
	m2, err := Load(nw)
	if err != nil || m2.Get().SessionsDir != filepath.Join(nw, "Config", "Sessions") || movedFrom != "" {
		t.Fatal(err, m2.Get().SessionsDir)
	}
}

func TestMigrateUpdateRepo(t *testing.T) {
	for _, c := range []struct{ repo, ch, wantRepo, wantCh string }{
		{"nperiannan/opencrt", "nightly", PublicRepo, "stable"},
		{"nperiannan/termwright-private", "stable", PublicRepo, "stable"},
		{"", "nightly", PublicRepo, "nightly"},
		{"me/fork", "nightly", "me/fork", "nightly"},
	} {
		s := Settings{UpdateRepo: c.repo, UpdateChannel: c.ch}
		migrateUpdateRepo(&s)
		if s.UpdateRepo != c.wantRepo || s.UpdateChannel != c.wantCh {
			t.Errorf("%q/%q -> %q/%q", c.repo, c.ch, s.UpdateRepo, s.UpdateChannel)
		}
	}
}
