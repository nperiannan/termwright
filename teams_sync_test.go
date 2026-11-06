package main

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/team"
)

func TestTeamFolderFollowsTheInventory(t *testing.T) {
	keyring.MockInit()
	srv, code, err := team.Open(t.TempDir(), "Lab B", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.TLS = srv.TLSConfig()
	ts.StartTLS()
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "https://")

	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(filepath.Join(dir, "Sessions"))
	a := &App{store: st, cfg: cfg}
	tc, err := a.teamJoin(addr, srv.Fingerprint(), team.JoinRequest{OwnerCode: code}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		teamSyncMu.Lock()
		for _, c := range teamSyncs {
			c()
		}
		teamSyncs = map[string]context.CancelFunc{}
		teamSyncMu.Unlock()
	}()
	_, c, _ := a.teamConn(tc.ID)
	ctx := context.Background()
	if _, err := c.Push(ctx, []team.Device{{Name: "core-sw1", Folder: "Core", Host: "10.0.0.1", Protocol: "SSH2", DeviceType: "VOSS"}}); err != nil {
		t.Fatal(err)
	}
	sync := func() {
		t.Helper()
		inv, err := c.Inventory(ctx, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.applyInventory(tc.ID, inv); err != nil {
			t.Fatal(err)
		}
	}
	sync()
	s, err := st.Get("Lab B/Core/core-sw1")
	if err != nil || s.Hostname != "10.0.0.1" || s.DeviceType != "VOSS" || s.Team == "" {
		t.Fatalf("team session %+v %v", s, err)
	}
	// my own login on it stays; a rename and a move on the server follow
	s.Username = "natarajan"
	if _, err := a.SaveSession(s, s.Path); err != nil {
		t.Fatal(err)
	}
	inv, _ := c.Inventory(ctx, 0, 0)
	d := *inv.Devices[0]
	d.Name, d.Folder = "core-switch-1", "Datacenter/Core"
	srvDevices(t, a, tc.ID, d)
	sync()
	if _, err := st.Get("Lab B/Core/core-sw1"); err == nil {
		t.Fatal("the old session is still there")
	}
	s, err = st.Get("Lab B/Datacenter/Core/core-switch-1")
	if err != nil || s.Username != "natarajan" {
		t.Fatalf("after the move %+v %v", s, err)
	}
	// a pasted copy is my own, not a second team device
	cp := s
	cp.Name, cp.Folder = "my copy", ""
	if saved, err := a.SaveSession(cp, ""); err != nil || saved.Team != "" {
		t.Fatalf("copy %+v %v", saved, err)
	}
	// leaving removes the team folder, keeps my own sessions
	if err := a.TeamLeave(tc.ID, true); err != nil {
		t.Fatal(err)
	}
	if st.Exists("Lab B") || !st.Exists("my copy") {
		t.Fatalf("after leaving: team folder %v, own copy %v", st.Exists("Lab B"), st.Exists("my copy"))
	}
}

// srvDevices changes a device on the server through the admin API (as the owner).
func srvDevices(t *testing.T, a *App, id string, d team.Device) {
	t.Helper()
	_, c, _ := a.teamConn(id)
	if err := c.SaveDevices(context.Background(), []team.Device{d}); err != nil {
		t.Fatal(err)
	}
}

// Without a team nothing runs: no sync loop, no connection, no team folder.
func TestNoTeamNoSync(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(filepath.Join(dir, "Sessions"))
	a := &App{store: st, cfg: cfg}
	a.startTeamSync()
	teamSyncMu.Lock()
	n := len(teamSyncs)
	teamSyncMu.Unlock()
	if n != 0 || len(a.Teams()) != 0 {
		t.Fatalf("%d sync loops, %d teams without joining one", n, len(a.Teams()))
	}
}
