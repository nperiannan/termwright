package session

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/securecrt"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "Sessions"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSaveRenamePreservesUnknownKeys(t *testing.T) {
	src, _ := os.ReadFile("../securecrt/testdata/switch.ini")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Config", "Sessions", "Lab"), 0o700)
	os.WriteFile(filepath.Join(dir, "Config", "Sessions", "Lab", "switch.ini"), src, 0o600)

	st := newStore(t)
	res, err := st.Import(dir, ImportOptions{})
	if err != nil || len(res.Imported) != 1 || res.Imported[0] != "Lab/switch" {
		t.Fatalf("import: %v %+v", err, res)
	}
	// byte-identical copy
	got, _ := os.ReadFile(filepath.Join(st.Root(), "Lab", "switch.ini"))
	if !bytes.Equal(got, src) {
		t.Fatal("imported file changed")
	}
	s, err := st.Get("Lab/switch")
	if err != nil {
		t.Fatal(err)
	}
	if s.Hostname != "192.0.2.10" || s.Port != 22 || s.Username != "admin" || !s.LogOnConnect {
		t.Fatalf("mapping: %+v", s)
	}
	s.Hostname = "198.51.100.1"
	s.Name = "core-1"
	s.Folder = "Lab/Core"
	saved, err := st.Save(s, "Lab/switch")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Path != "Lab/Core/core-1" || st.Exists("Lab/switch") {
		t.Fatalf("rename failed: %s", saved.Path)
	}
	f, _ := st.ReadRaw("Lab/Core/core-1")
	if f.Str("Password V2", "") != "02:abcdef0123456789" || f.Get("Future Type") == nil {
		t.Fatal("unknown keys lost on save")
	}
	if f.Str("Hostname", "") != "198.51.100.1" {
		t.Fatal("hostname not updated")
	}
	// second import renames on conflict
	res, _ = st.Import(dir, ImportOptions{})
	if len(res.Renamed) != 0 {
		t.Fatalf("unexpected rename %+v", res)
	}
	res, _ = st.Import(dir, ImportOptions{})
	if len(res.Renamed) != 1 || res.Renamed[0] != "Lab/switch (2)" {
		t.Fatalf("expected rename %+v", res)
	}
	tree, err := st.Tree()
	if err != nil || len(tree.Children) != 1 || tree.Children[0].Name != "Lab" {
		t.Fatalf("tree %v %+v", err, tree)
	}
}

func TestExportFormats(t *testing.T) {
	st := newStore(t)
	for _, n := range []string{"a", "b"} {
		s := Defaults()
		s.Name, s.Folder, s.Hostname = n, "Group", n+".example"
		if _, err := st.Save(s, ""); err != nil {
			t.Fatal(err)
		}
	}
	out := t.TempDir()
	for _, format := range []string{"dir", "zip", "xml"} {
		dest := filepath.Join(out, "export-"+format)
		if format != "dir" {
			dest += "." + format
		}
		n, err := st.Export(nil, dest, format)
		if err != nil || n != 2 {
			t.Fatalf("%s: %v %d", format, err, n)
		}
		st2 := newStore(t)
		res, err := st2.Import(dest, ImportOptions{})
		if err != nil || len(res.Imported) != 2 {
			t.Fatalf("%s reimport: %v %+v", format, err, res)
		}
		s, err := st2.Get("Group/b")
		if err != nil || s.Hostname != "b.example" {
			t.Fatalf("%s: %v %+v", format, err, s)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "export-dir", "Sessions", "Group", "__FolderData__.ini")); err != nil {
		t.Fatal("folder data missing")
	}
}

func TestSSHConfigImport(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config")
	os.WriteFile(cfg, []byte("Host *\n  User x\nHost web1\n  HostName 10.1.1.1\n  User deploy\n  Port 2222\n"), 0o600)
	st := newStore(t)
	res, err := st.Import(cfg, ImportOptions{TargetFolder: "ssh"})
	if err != nil || len(res.Imported) != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	s, _ := st.Get("ssh/web1")
	if s.Hostname != "10.1.1.1" || s.Port != 2222 || s.Username != "deploy" {
		t.Fatalf("%+v", s)
	}
}

func TestPathSafety(t *testing.T) {
	st := newStore(t)
	s := Defaults()
	s.Name, s.Folder = "x", "../../etc"
	if _, err := st.Save(s, ""); err == nil {
		t.Fatal("expected error for traversal")
	}
	if err := st.DeleteFolder(""); err == nil {
		t.Fatal("deleting root must fail")
	}
}

func TestV2FieldsRoundTrip(t *testing.T) {
	st := newStore(t)
	s := Defaults()
	s.Name, s.Hostname = "x", "h"
	s.JumpSession = "Lab/bastion"
	s.PortForwards = []PortForward{{Type: "local", BindPort: 8080, DestHost: "web", DestPort: 80, Name: "web|ui"}, {Type: "dynamic", BindAddr: "0.0.0.0", BindPort: 1080}}
	s.Triggers = []Trigger{{Pattern: "--More--", Send: " "}, {Pattern: `\[y/n\]`, Regex: true, Send: `y\r`}}
	s.ProxyType, s.ProxyHost, s.ProxyPort = "socks5", "p", 1080
	s.AutoReconnect, s.X11Forward, s.AgentForward = true, true, true
	s.KeywordSet, s.FontFamily, s.FontSize, s.Backspace = "Network", "Menlo", 15, "bs"
	if _, err := st.Save(s, ""); err != nil {
		t.Fatal(err)
	}
	g, err := st.Get("x")
	if err != nil {
		t.Fatal(err)
	}
	g.Path, g.Name, g.Folder = s.Path, s.Name, s.Folder
	if fmt.Sprintf("%+v", g) != fmt.Sprintf("%+v", s) {
		t.Fatalf("round trip:\n%+v\n%+v", g, s)
	}
	f, _ := st.ReadRaw("x")
	if f.Str("Firewall Name", "") != "Session:Lab/bastion" {
		t.Fatal("firewall key")
	}
	g.JumpSession = ""
	st.Save(g, "x")
	f, _ = st.ReadRaw("x")
	if f.Str("Firewall Name", "") != "None" {
		t.Fatal("firewall reset")
	}
}

func TestSecureCRTPortForwardTable(t *testing.T) {
	data := "S:\"Protocol Name\"=SSH2\r\nS:\"Hostname\"=gw\r\nZ:\"Port Forward Table V2\"=00000003\r\n localhost:8080:webserver.internal.net:80::0:0::\r\n localhost:3306:10.0.0.15:3306::0:0::\r\n D1080::::0:0::\r\n"
	f, err := securecrt.Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	s := FromFile("gw", f)
	if len(s.PortForwards) != 3 {
		t.Fatalf("%+v", s.PortForwards)
	}
	a, c := s.PortForwards[0], s.PortForwards[2]
	if a.Type != "local" || a.BindAddr != "127.0.0.1" || a.BindPort != 8080 || a.DestHost != "webserver.internal.net" || a.DestPort != 80 {
		t.Fatalf("local %+v", a)
	}
	if c.Type != "dynamic" || c.BindPort != 1080 {
		t.Fatalf("dynamic %+v", c)
	}
	// saving writes Termwright's own list and keeps the original table untouched
	s.Apply(f)
	if len(f.List("Port Forward Table V2")) != 3 || len(f.List("Termwright Port Forwards")) != 3 {
		t.Fatal("tables")
	}
}

func TestSecureCRTForwardTableV3(t *testing.T) {
	// first two lines are exactly what SecureCRT 9.x wrote for forwards named "dynamic" and "local"
	data := "S:\"Protocol Name\"=SSH2\r\nZ:\"Port Forward Table V3\"=00000004\r\n 1|dynamic|192.0.2.180,1080|0|socks,|||\r\n 1|local|192.0.2.18,8080|0||80||\r\n 1|web|,8443|1|web.lab|443||\r\n 0|old|,9000|0||22||\r\nZ:\"Reverse Forward Table V3\"=00000001\r\n 1|back|,2222|1|10.0.0.9|22||\r\nD:\"Log Timestamp Each Line\"=00000000\r\nS:\"Custom Log Message Each Line\"=[%h]\r\n"
	f, err := securecrt.Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	s := FromFile("x", f)
	if len(s.PortForwards) != 4 {
		t.Fatalf("%+v", s.PortForwards)
	}
	d, l, w, r := s.PortForwards[0], s.PortForwards[1], s.PortForwards[2], s.PortForwards[3]
	if d.Type != "dynamic" || d.Name != "dynamic" || d.BindAddr != "192.0.2.180" || d.BindPort != 1080 {
		t.Fatalf("dynamic %+v", d)
	}
	if l.Type != "local" || l.Name != "local" || l.BindAddr != "192.0.2.18" || l.BindPort != 8080 || l.DestHost != "localhost" || l.DestPort != 80 {
		t.Fatalf("local %+v", l)
	}
	if w.BindAddr != "127.0.0.1" || w.DestHost != "web.lab" || w.DestPort != 443 || w.Name != "web" {
		t.Fatalf("named %+v", w)
	}
	if r.Type != "remote" || r.BindPort != 2222 || r.DestHost != "10.0.0.9" || r.DestPort != 22 {
		t.Fatalf("reverse %+v", r)
	}
	if s.LogEachLine != "" {
		t.Fatalf("timestamp flag off must disable the prefix, got %q", s.LogEachLine)
	}
	s.LogEachLine = "[%h:%m] "
	s.Apply(f)
	if !f.Bool("Log Timestamp Each Line", false) || f.Str("Custom Log Message Each Line", "") != "[%h:%m] " {
		t.Fatal("timestamp flag not written")
	}
}

func TestSecureCRTLogonKeys(t *testing.T) {
	src := "D:\"Use Script File\"=00000001\r\nS:\"Shell Command\"=ssh -i ~/.ssh/aws_key ec2-user@10.0.0.5\r\nD:\"Use Shell Command\"=00000001\r\n" +
		"D:\"Send Initial Carriage Return\"=00000001\r\nD:\"Use Login Script\"=00000000\r\nS:\"Script Arguments\"=DUT1, DUT2\r\n" +
		"Z:\"Login Script V4\"=00000001\r\n 03:abcdef\r\nS:\"Script Filename V2\"=login.js\r\n"
	f, err := securecrt.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	s := FromFile("x", f)
	if !s.UseRemoteCommand || s.RemoteCommand != "ssh -i ~/.ssh/aws_key ec2-user@10.0.0.5" || !s.InitialCR || s.AutomateLogon ||
		!s.UseLogonScript || s.LogonScript != "login.js" || s.LogonArgs != "DUT1, DUT2" {
		t.Fatalf("%+v", s)
	}
	s.UseRemoteCommand, s.AutomateLogon = false, true
	s.Apply(f)
	out := string(f.Bytes())
	for _, want := range []string{`D:"Use Shell Command"=00000000`, `D:"Use Login Script"=00000001`, " 03:abcdef", `S:"Shell Command"=ssh -i`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
}

func TestEnvironmentAndTreeFields(t *testing.T) {
	st, _ := NewStore(t.TempDir())
	s := Session{Name: "sw1", Folder: "Prod", Protocol: ProtoSSH2, Hostname: "10.0.0.1", Port: 22, DeviceType: "VOSS",
		Environment: "production", JumpSession: "Cloud/jump"}
	if _, err := st.Save(s, ""); err != nil {
		t.Fatal(err)
	}
	if g, _ := st.Get("Prod/sw1"); g.Environment != "production" {
		t.Fatalf("environment %q", g.Environment)
	}
	root, _ := st.Tree()
	n := root.Children[0].Children[0]
	if n.DeviceType != "VOSS" || n.Environment != "production" || n.Jump != "Cloud/jump" {
		t.Fatalf("%+v", n)
	}
}
