package main

import (
	"path/filepath"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

// Sessions and settings move together: export a zip with settings on one
// "computer", import it on another into a folder.
func TestExportImportWithSettings(t *testing.T) {
	newApp := func() *App {
		dir := t.TempDir()
		cfg, err := config.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		st, err := session.NewStore(filepath.Join(dir, "Sessions"))
		if err != nil {
			t.Fatal(err)
		}
		return &App{store: st, cfg: cfg}
	}
	a := newApp()
	s := a.cfg.Get()
	s.Credentials = []config.Credential{{Name: "lab", Username: "admin"}}
	s.FolderDefaults = []config.FolderDefault{{Folder: "Lab", Account: "lab"}}
	s.FontSize = 16
	if err := a.cfg.Save(s); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.Save(session.Session{Name: "sw1", Folder: "Lab", Protocol: session.ProtoSSH2, Hostname: "10.0.0.1"}, ""); err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(t.TempDir(), "out.zip")
	if _, err := a.ExportSessions(nil, zip, "zip", true); err != nil {
		t.Fatal(err)
	}

	b := newApp()
	sum := b.ExportSettingsIn(zip)
	if sum == nil || sum.Accounts != 1 || sum.FolderDefaults != 1 {
		t.Fatalf("summary %+v", sum)
	}
	res, err := b.ImportSessions(zip, session.ImportOptions{TargetFolder: "From A", Settings: []string{config.PartLogins}})
	if err != nil || len(res.Errors) > 0 {
		t.Fatal(err, res.Errors)
	}
	got := b.cfg.Get()
	if len(got.Credentials) != 1 || got.Credentials[0].Username != "admin" || len(got.FolderDefaults) != 1 || got.FolderDefaults[0].Folder != "From A/Lab" {
		t.Fatalf("logins not imported: %+v %+v", got.Credentials, got.FolderDefaults)
	}
	if got.FontSize == 16 {
		t.Fatal("Global Options imported without being chosen")
	}
	if _, err := b.store.Get("From A/Lab/sw1"); err != nil {
		t.Fatal(err)
	}
	if len(res.Settings) == 0 {
		t.Fatal("no settings report")
	}

	// sessions only (no settings in the export)
	plain := filepath.Join(t.TempDir(), "plain.zip")
	if _, err := a.ExportSessions(nil, plain, "zip", false); err != nil {
		t.Fatal(err)
	}
	if b.ExportSettingsIn(plain) != nil {
		t.Fatal("settings found in an export without them")
	}
}
