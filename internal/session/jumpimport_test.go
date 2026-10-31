package session

import (
	"os"
	"path/filepath"
	"strings"
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

func TestImportTemplateCSV(t *testing.T) {
	list, notes, err := ParseCSVNotes([]byte(ImportTemplateCSV))
	if err != nil || len(notes) != 0 || len(list) != 9 {
		t.Fatalf("%v, %d sessions, notes: %v", err, len(list), notes)
	}
	// into a folder: every session lands there and the jump hosts follow
	dir := t.TempDir()
	tmp := dir + "/t.csv"
	if err := os.WriteFile(tmp, []byte(ImportTemplateCSV), 0o600); err != nil {
		t.Fatal(err)
	}
	st, _ := NewStore(t.TempDir())
	res, err := st.Import(tmp, ImportOptions{TargetFolder: "From CSV"})
	if err != nil || len(res.Imported) != 9 || len(res.Errors) != 0 || len(res.Notes) != 0 {
		t.Fatalf("%v %+v", err, res)
	}
	vnc, err := st.Get("From CSV/Lab/lab-pc (VNC)")
	if err != nil || vnc.Protocol != ProtoVNC || vnc.Port != 5902 || vnc.Hostname != "127.0.0.1" || vnc.JumpSession != "From CSV/Lab/lab-pc" || !st.Exists(vnc.JumpSession) {
		t.Fatalf("%+v %v", vnc, err)
	}
	if telnet, _ := st.Get("From CSV/Datacenter/Access/access-sw7"); telnet.JumpSession != "From CSV/Datacenter/jump-station" {
		t.Fatalf("telnet jump %q", telnet.JumpSession)
	}
}

func TestCSVNotes(t *testing.T) {
	csv := "name,host,protocol,port,environment,colour,jump_host\n" +
		"a,10.0.0.1,ssh1,22,pre prod!,red,\n" +
		"b,,ssh,22,,,\n" +
		"c,10.0.0.3,rdp,abc,test,,nowhere/x\n"
	list, notes, err := ParseCSVNotes([]byte(csv))
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %d", err, len(list))
	}
	all := strings.Join(notes, "\n")
	for _, want := range []string{`column "colour" is not used`, `row 2 (a): protocol "ssh1" is not known`, `row 2 (a): environment "pre prod!" is ignored`, "row 3 (b): no host, skipped", `row 4 (c): port "abc"`} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	st, _ := NewStore(t.TempDir())
	tmp := t.TempDir() + "/x.csv"
	_ = os.WriteFile(tmp, []byte(csv), 0o600)
	res, _ := st.Import(tmp, ImportOptions{})
	if !strings.Contains(strings.Join(res.Notes, "\n"), `jump host "nowhere/x" is not in the file and not in your sessions`) {
		t.Errorf("no note about the missing jump host: %v", res.Notes)
	}
}

func TestCSVEnvironments(t *testing.T) {
	for in, want := range map[string]string{"Production": "production", "prod": "production", "DEV": "development", "lab": "test",
		"stage": "staging", "pre-prod": "pre-prod", "customer-a": "customer-a", "pre prod!": "", "": ""} {
		if got := envID(in); got != want {
			t.Errorf("envID(%q) = %q, want %q", in, got, want)
		}
	}
}
