package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

func fsFreePort(t *testing.T, network string) int {
	if network == "udp" {
		c, _ := net.ListenPacket("udp", "127.0.0.1:0")
		defer c.Close()
		return c.LocalAddr().(*net.UDPAddr).Port
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestFileServersApp(t *testing.T) {
	keyring.MockInit()
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := session.NewStore(filepath.Join(dir, "Sessions"))
	a := &App{cfg: cfg, store: st}
	defer fsMgr.StopAll()

	v := a.FileServers()
	if v.Settings.TFTPPort != 69 || v.Settings.FTPPort != 21 || v.Settings.HTTPPort != 8080 || v.Settings.FTPUser != "termwright" || v.Settings.IdleMinutes != 30 {
		t.Fatalf("defaults: %+v", v.Settings)
	}
	served := filepath.Join(dir, "served")
	s := v.Settings
	s.Folder, s.Address = served, "127.0.0.1"
	s.TFTPPort, s.FTPPort, s.HTTPPort = fsFreePort(t, "udp"), fsFreePort(t, "tcp"), fsFreePort(t, "tcp")
	s.FTPWrite = true
	if err := a.FileServersSave(s); err != nil {
		t.Fatal(err)
	}
	if bad := s; true {
		bad.FTPUser = "two words"
		if a.FileServersSave(bad) == nil {
			t.Fatal("a user name with a space was saved")
		}
	}
	for _, k := range []string{"tftp", "ftp", "http"} {
		if err := a.FileServersStart(k); err != nil {
			t.Fatal(k, err)
		}
	}
	os.WriteFile(filepath.Join(served, "image.bin"), []byte(strings.Repeat("x", 100000)), 0o644)
	if f := a.FileServersFiles(); len(f) != 1 || f[0].Name != "image.bin" || f[0].Size != 100000 {
		t.Fatalf("files: %+v", f)
	}
	pw, err := a.FileServersPassword()
	if err != nil || len(pw) < 12 {
		t.Fatalf("password %d %v", len(pw), err)
	}
	if pw2, _ := a.FileServersPassword(); pw2 != pw {
		t.Fatal("the password changed")
	}
	if _, err := exec.LookPath("curl"); err == nil {
		for _, u := range []string{
			fmt.Sprintf("tftp://127.0.0.1:%d/image.bin", s.TFTPPort),
			fmt.Sprintf("ftp://termwright:%s@127.0.0.1:%d/image.bin", pw, s.FTPPort),
			fmt.Sprintf("http://127.0.0.1:%d/image.bin", s.HTTPPort),
		} {
			b, err := exec.Command("curl", "-sS", "--max-time", "20", u).Output()
			if err != nil || len(b) != 100000 {
				t.Fatalf("%s: %v %d", strings.Split(u, "@")[0], err, len(b))
			}
		}
		// a new password: the old one no longer logs in
		np, _ := a.FileServersNewPassword()
		if np == pw {
			t.Fatal("same new password")
		}
		if err := exec.Command("curl", "-sS", "--max-time", "10", fmt.Sprintf("ftp://termwright:%s@127.0.0.1:%d/image.bin", pw, s.FTPPort)).Run(); err == nil {
			t.Fatal("the old FTP password still works")
		}
		time.Sleep(100 * time.Millisecond)
		if tr := a.FileServers().Transfers; len(tr) < 3 || tr[len(tr)-1].State != "done" {
			t.Fatalf("transfers: %+v", tr)
		}
	}
	if ad := a.FileServersAddresses("127.0.0.1"); !ad[len(ad)-1].Reaches {
		t.Fatalf("addresses: %+v", ad)
	}
	st2 := a.FileServers().State
	for _, sv := range st2.Servers {
		if !sv.Running || sv.Address != "127.0.0.1" {
			t.Fatalf("state: %+v", sv)
		}
	}
	a.FileServersStop("")
	if fsMgr.Running() {
		t.Fatal("still running")
	}
	// "*" listens on all addresses
	s.Address = "*"
	a.FileServersSave(s)
	if err := a.FileServersStart("http"); err != nil {
		t.Fatal(err)
	}
	if sv := a.FileServers().State.Servers[2]; sv.Address != "*" && sv.Address != "" {
		t.Fatalf("all addresses: %+v", sv)
	}
	a.FileServersStop("http")
}
