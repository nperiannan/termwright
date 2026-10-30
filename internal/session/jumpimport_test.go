package session

import (
	"path/filepath"
	"testing"
)

// RDP and VNC sessions reach a machine through an SSH session used as jump host;
// the export and import must keep that reference working wherever the sessions land.
func jumpFixture(t *testing.T) (*Store, string) {
	t.Helper()
	src, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ssh := src.NewSession()
	ssh.Name, ssh.Folder, ssh.Hostname, ssh.Username = "testgen-vm", "Bangalore Server", "10.234.179.142", "nperiannan"
	if _, err := src.Save(ssh, ""); err != nil {
		t.Fatal(err)
	}
	for _, k := range []struct {
		proto, name string
		port        int
	}{{"RDP", "testgen-vm (RDP)", 3389}, {"VNC", "testgen-vm (VNC)", 5902}} {
		s := src.NewSession()
		s.Name, s.Folder, s.Protocol, s.Hostname, s.Port = k.name, "Bangalore Server", k.proto, "127.0.0.1", k.port
		s.Username, s.JumpSession = "nperiannan-"+k.proto, "Bangalore Server/testgen-vm"
		if _, err := src.Save(s, ""); err != nil {
			t.Fatal(err)
		}
	}
	zip := filepath.Join(t.TempDir(), "out.zip")
	if _, err := src.Export([]string{"Bangalore Server"}, zip, "zip"); err != nil {
		t.Fatal(err)
	}
	return src, zip
}

func TestImportKeepsJumpHost(t *testing.T) {
	_, zip := jumpFixture(t)
	for name, tc := range map[string]struct{ target, wantJump string }{
		"same place":    {"", "Bangalore Server/testgen-vm"},
		"target folder": {"Imported", "Imported/Bangalore Server/testgen-vm"},
	} {
		t.Run(name, func(t *testing.T) {
			dst, _ := NewStore(t.TempDir())
			res, err := dst.Import(zip, ImportOptions{TargetFolder: tc.target})
			if err != nil || len(res.Imported) != 3 {
				t.Fatalf("%v %+v", err, res)
			}
			for _, p := range res.Imported {
				s, _ := dst.Get(p)
				if s.Protocol == "VNC" || s.Protocol == "RDP" {
					if s.JumpSession != tc.wantJump || s.Hostname != "127.0.0.1" || s.Port == 0 {
						t.Errorf("%s: jump %q host %q port %d, want jump %q", p, s.JumpSession, s.Hostname, s.Port, tc.wantJump)
					}
					if !dst.Exists(s.JumpSession) {
						t.Errorf("%s: its jump host %q does not exist", p, s.JumpSession)
					}
				}
			}
		})
	}
	t.Run("imported twice", func(t *testing.T) {
		dst, _ := NewStore(t.TempDir())
		if _, err := dst.Import(zip, ImportOptions{}); err != nil {
			t.Fatal(err)
		}
		res, err := dst.Import(zip, ImportOptions{Conflict: "rename"})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range res.Imported {
			s, _ := dst.Get(p)
			if (s.Protocol == "VNC" || s.Protocol == "RDP") && !dst.Exists(s.JumpSession) {
				t.Errorf("%s: jump %q does not exist", p, s.JumpSession)
			}
			// the copy of a VNC session goes through the copy of its SSH session
			if s.Protocol == "VNC" && s.JumpSession == "Bangalore Server/testgen-vm" {
				t.Errorf("%s still uses the first import's jump host", p)
			}
		}
	})
}
