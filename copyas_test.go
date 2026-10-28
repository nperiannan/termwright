package main

import (
	"testing"

	"github.com/termwright/termwright/internal/session"
)

func TestCopySessionAs(t *testing.T) {
	st, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: st}
	orig := session.Session{Name: "core-sw1", Folder: "Lab", Protocol: session.ProtoSSH2, Hostname: "10.0.0.1", Port: 22, Username: "admin"}
	if _, err := st.Save(orig, ""); err != nil {
		t.Fatal(err)
	}
	c, err := a.CopySessionAs("Lab/core-sw1", "sftp")
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != "Lab/core-sw1 (SFTP)" || c.OpenAs != "sftp" || c.Hostname != "10.0.0.1" || c.Username != "admin" {
		t.Fatalf("sftp copy: %+v", c)
	}
	// a second copy gets a number; the SFTP copy back as SSH drops the suffix
	c2, err := a.CopySessionAs("Lab/core-sw1", "sftp")
	if err != nil || c2.Path != "Lab/core-sw1 (SFTP) 2" {
		t.Fatalf("second copy: %v %+v", err, c2)
	}
	c3, err := a.CopySessionAs("Lab/core-sw1 (SFTP)", "")
	if err != nil || c3.Path != "Lab/core-sw1 (SSH)" || c3.OpenAs != "" {
		t.Fatalf("ssh copy: %v %+v", err, c3)
	}
	if _, err := st.Get("Lab/core-sw1"); err != nil {
		t.Fatal("original is gone")
	}
	tel := session.Session{Name: "t1", Protocol: session.ProtoTelnet, Hostname: "x"}
	if _, err := st.Save(tel, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CopySessionAs("t1", "sftp"); err == nil {
		t.Fatal("telnet copied as sftp")
	}
}
