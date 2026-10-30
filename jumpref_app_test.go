package main

import (
	"path/filepath"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

func TestJumpRefsFollowAMove(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := session.NewStore(filepath.Join(dir, "Sessions"))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: st, cfg: cfg}
	save := func(name, folder, proto, jump string) {
		t.Helper()
		if _, err := st.Save(session.Session{Name: name, Folder: folder, Protocol: proto, Hostname: "10.0.0.1", JumpSession: jump}, ""); err != nil {
			t.Fatal(err)
		}
	}
	save("jump", "lab", session.ProtoSSH2, "")
	save("vm-rdp", "lab", session.ProtoRDP, "lab/jump")
	save("vm-vnc", "lab", session.ProtoVNC, "lab/jump")
	save("x", "other", session.ProtoSSH2, "")
	set := cfg.Get()
	set.FolderDefaults = []config.FolderDefault{{Folder: "lab/vms", JumpSession: "lab/jump"}}
	if err := cfg.Save(set); err != nil {
		t.Fatal(err)
	}

	if us := a.JumpUsers([]string{"lab/jump"}, false, nil); len(us) != 3 { // 2 sessions + the folder default
		t.Fatalf("users = %+v", us)
	}
	if got := a.JumpUsers([]string{"lab/jump"}, false, []string{"lab/vm-rdp"}); len(got) != 2 {
		t.Fatalf("except: %+v", got)
	}
	n, err := a.RepointJumps(map[string]string{"lab/jump": "new/jump"})
	if err != nil || n != 3 {
		t.Fatalf("repointed %d: %v", n, err)
	}
	if s, _ := st.Get("lab/vm-rdp"); s.JumpSession != "new/jump" {
		t.Errorf("jump = %q", s.JumpSession)
	}
	if cfg.Get().FolderDefaults[0].JumpSession != "new/jump" {
		t.Error("folder default not repointed")
	}
	if len(a.JumpUsers([]string{"lab/jump"}, false, nil)) != 0 {
		t.Error("the old path is still used")
	}
	// a chain: what uses the sessions that use it
	save("deep", "lab", session.ProtoSSH2, "lab/vm-rdp")
	if got := a.JumpUsers([]string{"new/jump"}, true, nil); len(got) != 4 {
		t.Errorf("deep users = %+v", got)
	}
}

func TestClearJumpDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(filepath.Join(dir, "Sessions"))
	a := &App{store: st, cfg: cfg}
	set := cfg.Get()
	set.FolderDefaults = []config.FolderDefault{{Folder: "a", JumpSession: "lab/jump"}, {Folder: "b", JumpSession: "lab/other"}}
	_ = cfg.Save(set)
	if n, err := a.ClearJumpDefaults([]string{"lab/jump"}); err != nil || n != 1 {
		t.Fatalf("cleared %d: %v", n, err)
	}
	d := cfg.Get().FolderDefaults
	if d[0].JumpSession != "" || d[1].JumpSession != "lab/other" {
		t.Errorf("defaults = %+v", d)
	}
}
