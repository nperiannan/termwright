package main

import (
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

func TestIsAddressName(t *testing.T) {
	for _, c := range []struct {
		name, host string
		want       bool
	}{{"10.1.1.1", "10.1.1.1", true}, {"10.1.1.2", "sw1.lab", true}, {"sw1.lab", "sw1.lab", true}, {"fe80::1", "", true}, {"core-sw1", "10.1.1.1", false}, {"", "10.1.1.1", false}} {
		if got := IsAddressName(c.name, c.host); got != c.want {
			t.Errorf("%q/%q = %v", c.name, c.host, got)
		}
	}
}

func TestRenameSessionsAfterDevices(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	for _, s := range []session.Session{
		{Name: "10.0.0.1", Folder: "Lab", Hostname: "10.0.0.1", Protocol: session.ProtoSSH2},
		{Name: "old-name", Folder: "Lab", Hostname: "10.0.0.2", Protocol: session.ProtoSSH2},
		{Name: "10.0.0.3", Folder: "Lab", Hostname: "10.0.0.3", Protocol: session.ProtoSSH2, Team: "t1/d1"},
		{Name: "taken", Folder: "Lab", Hostname: "10.0.0.4", Protocol: session.ProtoSSH2},
		{Name: "10.0.0.5", Folder: "Lab", Hostname: "10.0.0.5", Protocol: session.ProtoSSH2},
	} {
		if _, err := st.Save(s, ""); err != nil {
			t.Fatal(err)
		}
	}
	res := a.RenameSessions([]SessionRename{{"Lab/10.0.0.1", "lab-x465-1 (X465-48T)"}, {"Lab/old-name", "core-vsp-1"}, {"Lab/10.0.0.3", "x"}, {"Lab/10.0.0.5", "taken"}})
	if res[0].Path != "Lab/lab-x465-1 (X465-48T)" || res[1].Path != "Lab/core-vsp-1" || res[2].Error == "" || res[3].Error == "" {
		t.Fatalf("%+v", res)
	}
	s, _ := st.Get("Lab/core-vsp-1")
	if len(s.Description) == 0 || s.Description[0] != "Was named old-name" {
		t.Fatalf("description %q", s.Description)
	}
	s, _ = st.Get("Lab/lab-x465-1 (X465-48T)")
	if s.Hostname != "10.0.0.1" || strings.Join(s.Description, "") != "" {
		t.Fatalf("renamed %+v", s)
	}
}
