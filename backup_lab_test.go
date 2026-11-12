package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

// A backup keeps the lab: the emulated lab (with its icons, management and
// Fast start choices), its saved topologies and the device packs.
func TestBackupHasLab(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load(filepath.Join(dir, "data"))
	st, _ := session.NewStore(filepath.Join(dir, "data", "Sessions"))
	a := &App{cfg: cfg, store: st}
	for _, f := range []string{"lab/emu.json", "lab/topologies/EP1.json", "lab/packs/demo.vm"} {
		p := filepath.Join(cfg.DataDir(), f)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := a.BackupConfig()
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	have := map[string]bool{}
	for _, f := range z.File {
		have[f.Name] = true
	}
	for _, want := range []string{"Termwright/lab/emu.json", "Termwright/lab/topologies/EP1.json", "Termwright/lab/packs/demo.vm"} {
		if !have[want] {
			t.Errorf("no %s in the backup: %v", want, have)
		}
	}
}
