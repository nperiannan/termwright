package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

// Two computers share a sync folder: sessions added, changed and deleted on
// one show up on the other, settings merge, team folders stay out, and
// nothing changes back and forth once they agree.
func TestSyncBetweenComputers(t *testing.T) {
	shared := t.TempDir()
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
		a := &App{store: st, cfg: cfg}
		s := cfg.Get()
		s.SyncFolder = shared
		s.Teams = []config.TeamConn{{ID: "t1", Name: "ENI-EP1-CHN"}}
		_ = cfg.Save(s)
		_ = cfg.SetTeams(s.Teams)
		return a
	}
	sync := func(x *App) SyncStatus {
		x.syncOnce()
		st := x.SyncState()
		if st.Error != "" {
			t.Fatal(st.Error)
		}
		return st
	}
	tick := func() { time.Sleep(20 * time.Millisecond) }
	a, b := newApp(), newApp()

	// added on A
	if _, err := a.store.Save(session.Session{Name: "sw1", Folder: "Lab", Protocol: session.ProtoSSH2, Hostname: "10.0.0.1"}, ""); err != nil {
		t.Fatal(err)
	}
	_, _ = a.store.Save(session.Session{Name: "team-sw", Folder: "ENI-EP1-CHN", Protocol: session.ProtoSSH2, Hostname: "10.9.9.9"}, "")
	s := a.cfg.Get()
	s.Credentials = []config.Credential{{Name: "lab", Username: "admin"}}
	_ = a.cfg.Save(s)
	sync(a)
	tick()
	if st := sync(b); st.Took != 1 || len(st.Peers) != 1 {
		t.Fatalf("B took %d from %v", st.Took, st.Peers)
	}
	if _, err := b.store.Get("Lab/sw1"); err != nil {
		t.Fatal("not synced to B: ", err)
	}
	if b.store.Exists("ENI-EP1-CHN/team-sw") {
		t.Fatal("a team folder was synced")
	}
	if c := b.cfg.Get().Credentials; len(c) != 1 || c[0].Username != "admin" {
		t.Fatalf("account not synced: %+v", c)
	}

	// changed on B
	tick()
	sw, _ := b.store.Get("Lab/sw1")
	sw.Hostname = "10.0.0.2"
	if _, err := b.store.Save(sw, sw.Path); err != nil {
		t.Fatal(err)
	}
	tick()
	sync(b)
	tick()
	sync(a)
	if got, _ := a.store.Get("Lab/sw1"); got.Hostname != "10.0.0.2" {
		t.Fatalf("change not synced back: %q", got.Hostname)
	}

	// nothing changes any more: no rewrites either way
	zipA := filepath.Join(shared, syncDirName, a.cfg.Get().SyncID+".zip")
	fi1, _ := os.Stat(zipA)
	for i := 0; i < 3; i++ {
		tick()
		if st := sync(b); st.Took != 0 {
			t.Fatalf("round %d: B took %d again", i, st.Took)
		}
		tick()
		if st := sync(a); st.Took != 0 {
			t.Fatalf("round %d: A took %d again", i, st.Took)
		}
	}
	if fi2, _ := os.Stat(zipA); !fi2.ModTime().Equal(fi1.ModTime()) {
		t.Fatal("A rewrote its sync file without a change")
	}

	// deleted on A
	tick()
	if err := a.store.Delete("Lab/sw1"); err != nil {
		t.Fatal(err)
	}
	tick()
	sync(a)
	tick()
	sync(b)
	if b.store.Exists("Lab/sw1") {
		t.Fatal("the deletion did not reach B")
	}
	tick()
	sync(a)
	if a.store.Exists("Lab/sw1") {
		t.Fatal("the deleted session came back to A")
	}
}
