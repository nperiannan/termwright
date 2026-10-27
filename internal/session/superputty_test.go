package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const superPuTTYSample = `<?xml version="1.0" encoding="utf-8"?>
<ArrayOfSessionData xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">
  <SessionData SessionId="Lab/Rack 1/sw1" SessionName="sw1" ImageKey="computer" Host="192.0.2.10" Port="23" Proto="Telnet" PuttySession="Default Settings" Username="" ExtraArgs="" SPSLFileName="" RemotePath="" LocalPath="" />
  <SessionData SessionId="Lab/Rack 1/sw2" SessionName="sw2" ImageKey="computer" Host="192.0.2.11" Port="2222" Proto="SSH" PuttySession="Default Settings" Username="admin" ExtraArgs="-pw &quot;s3cret x&quot; -i C:\keys\lab.ppk" />
  <SessionData SessionId="Lab/con" SessionName="con" ImageKey="computer" Host="COM3" Port="115200" Proto="Serial" PuttySession="Lab" Username="" ExtraArgs="" />
  <SessionData SessionId="old/rl" SessionName="rl" ImageKey="computer" Host="192.0.2.12" Port="513" Proto="Rlogin" PuttySession="Default Settings" Username="" ExtraArgs="" />
</ArrayOfSessionData>`

func TestParseSuperPuTTY(t *testing.T) {
	list, skipped, err := ParseSuperPuTTY([]byte(superPuTTYSample))
	if err != nil || len(list) != 3 || len(skipped) != 1 {
		t.Fatalf("%d sessions, skipped %v, err %v", len(list), skipped, err)
	}
	a, b, c := list[0].Session, list[1], list[2].Session
	if a.Folder != "Lab/Rack 1" || a.Name != "sw1" || a.Protocol != ProtoTelnet || a.Hostname != "192.0.2.10" || a.Port != 23 || list[0].Password != "" {
		t.Fatalf("telnet: %+v", a)
	}
	if b.Session.Protocol != ProtoSSH2 || b.Session.Port != 2222 || b.Session.Username != "admin" || b.Password != "s3cret x" ||
		b.Session.IdentityFile != `C:\keys\lab.ppk` || !b.Session.SavePassword {
		t.Fatalf("ssh: %+v pw %q", b.Session, b.Password)
	}
	if c.Protocol != ProtoSerial || c.ComPort != "COM3" || c.BaudRate != 115200 || len(c.Description) != 1 {
		t.Fatalf("serial: %+v", c)
	}
}

func TestImportSuperPuTTYPasswordsStayOutOfFiles(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Sessions.xml")
	if err := os.WriteFile(src, []byte(superPuTTYSample), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(filepath.Join(dir, "Sessions"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Import(src, ImportOptions{TargetFolder: "SuperPuTTY"})
	if err != nil || len(res.Imported) != 3 || res.Passwords["SuperPuTTY/Lab/Rack 1/sw2"] != "s3cret x" || len(res.Passwords) != 1 {
		t.Fatalf("res %+v err %v", res, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "Sessions", "SuperPuTTY", "Lab", "Rack 1", "sw2.ini"))
	if len(b) == 0 || containsBytes(b, "s3cret") {
		t.Fatalf("session file: %q", b)
	}
	got, err := s.Get("SuperPuTTY/Lab/Rack 1/sw2")
	if err != nil || got.Port != 2222 || !got.SavePassword {
		t.Fatalf("%+v %v", got, err)
	}
}

func containsBytes(b []byte, s string) bool { return strings.Contains(string(b), s) }
