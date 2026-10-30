package main

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestProbeVNC(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reply  []byte
		expect string
	}{
		{"vnc auth", []byte{1, 2}, "VNC Authentication (2)"},
		{"vencrypt", []byte{2, 19, 2}, "VeNCrypt (19)"},
		{"refused", []byte{0}, "refuses"},
		{"nothing usable", []byte{1, 77}, "cannot use any"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, sv := net.Pipe()
			go func() {
				defer sv.Close()
				_, _ = sv.Write([]byte("RFB 003.008\n"))
				buf := make([]byte, 12)
				_, _ = sv.Read(buf)
				_, _ = sv.Write(tc.reply)
			}()
			out := strings.Join(probeVNC(cl), "\n")
			if !strings.Contains(out, "RFB 003.008") || !strings.Contains(out, tc.expect) {
				t.Fatalf("report:\n%s", out)
			}
		})
	}
	t.Run("blocked address", func(t *testing.T) {
		cl, sv := net.Pipe()
		go func() {
			defer sv.Close()
			// what TigerVNC writes to an address it has blocked: the old version, a 0, the reason
			msg := "Too many security failures"
			b := append([]byte("RFB 003.003\n"), 0, 0, 0, 0, 0, 0, 0, byte(len(msg)))
			go func() { _, _ = sv.Write(append(b, msg...)) }() // a pipe has no buffer, a real connection has
			_, _ = sv.Read(make([]byte, 12))
			time.Sleep(200 * time.Millisecond)
		}()
		out := strings.Join(probeVNC(cl), "\n")
		if !strings.Contains(out, "RFB 003.003") || !strings.Contains(out, "Too many security failures") || !strings.Contains(out, "blocked this address") {
			t.Fatalf("report:\n%s", out)
		}
	})
	t.Run("not vnc", func(t *testing.T) {
		cl, sv := net.Pipe()
		go func() { defer sv.Close(); _, _ = sv.Write([]byte("SSH-2.0-Open")) }()
		if out := strings.Join(probeVNC(cl), "\n"); !strings.Contains(out, "not a VNC server") {
			t.Fatalf("report:\n%s", out)
		}
	})
}

func TestProbeRDP(t *testing.T) {
	answer := func(sel byte, kind byte) []byte {
		return []byte{3, 0, 0, 0x13, 0x0e, 0xd0, 0, 0, 0x12, 0x34, 0, kind, 0, 8, 0, sel, 0, 0, 0}
	}
	for name, tc := range map[string]struct {
		reply  []byte
		expect string
	}{
		"nla": {answer(2, 2), "NLA"}, "tls": {answer(1, 2), "TLS"}, "failure": {answer(5, 3), "refused"},
	} {
		t.Run(name, func(t *testing.T) {
			cl, sv := net.Pipe()
			go func() {
				defer sv.Close()
				buf := make([]byte, 19)
				n, _ := sv.Read(buf)
				if n != len(rdpRequest) || buf[5] != 0xe0 {
					return
				}
				_, _ = sv.Write(tc.reply)
			}()
			out := strings.Join(probeRDP(cl), "\n")
			if !strings.Contains(out, "confirmed") || !strings.Contains(out, tc.expect) {
				t.Fatalf("report:\n%s", out)
			}
		})
	}
	if out := strings.Join(parseRDPAnswer([]byte("HTTP/1.1 400")), "\n"); !strings.Contains(out, "not RDP") {
		t.Fatalf("report:\n%s", out)
	}
}

// The script reads state only and never prints the sudo password it is given.
func TestDiagScript(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	cmd := exec.Command("bash", "-s", "--", "--rdp", "3389", "--vnc", "5902", "--rdp-user", "nobody", "--vnc-user", "nobody")
	cmd.Env = append(cmd.Environ(), "TW_SUDO=hunter2-secret")
	cmd.Stdin = strings.NewReader(string(diagScript))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{"== Machine ==", "== RDP (xrdp) - port 3389", "== VNC (TigerVNC) - port 5902 (display :2)", "== End =="} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "hunter2-secret") {
		t.Error("the sudo password is in the output")
	}
}

func TestDiagArgs(t *testing.T) {
	if got := diagArgs(3389, 5902, "u-rdp", "u'vnc"); got != `'--rdp' '3389' '--vnc' '5902' '--rdp-user' 'u-rdp' '--vnc-user' 'u'\''vnc'` {
		t.Fatal(got)
	}
	if got := diagArgs(0, 5902, "x", ""); got != `'--vnc' '5902'` {
		t.Fatal(got)
	}
}

func TestHintTunnel(t *testing.T) {
	if !strings.Contains(hintTunnel(errString("ssh: rejected: administratively prohibited (open failed)")), "AllowTcpForwarding") {
		t.Fatal("forwarding hint missing")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestRestartUnits(t *testing.T) {
	if u, err := restartUnits("VNC", 5902); err != nil || len(u) != 1 || u[0] != "tigervnc@2.service" {
		t.Fatal(u, err)
	}
	if u, err := restartUnits("RDP", 3389); err != nil || len(u) != 2 || u[0] != "xrdp.service" {
		t.Fatal(u, err)
	}
	for _, bad := range []struct {
		k string
		p int
	}{{"VNC", 5900}, {"VNC", 80}, {"VNC", 6000}, {"SSH2", 22}} {
		if _, err := restartUnits(bad.k, bad.p); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestRestartScript(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	dir := t.TempDir()
	fake := "#!/bin/sh\necho \"fake systemctl $*\" >> " + dir + "/calls\n[ \"$1\" = is-active ] && echo active\nexit 0\n"
	if err := os.WriteFile(dir+"/systemctl", []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-s", "--", "tigervnc@2.service")
	cmd.Env = append(cmd.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "TW_SUDO=hunter2-secret")
	cmd.Stdin = strings.NewReader(string(restartScript))
	out, err := cmd.CombinedOutput()
	s := string(out)
	if os.Getuid() != 0 {
		// without root and without sudo the script must say what to run instead of failing silently
		if err == nil || !strings.Contains(s, "sudo systemctl restart tigervnc@2.service") {
			t.Fatalf("%v\n%s", err, s)
		}
		return
	}
	if err != nil {
		t.Fatalf("%v\n%s", err, s)
	}
	for _, want := range []string{"restarting: tigervnc@2.service", "tigervnc@2.service: active", "restarted"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "hunter2-secret") {
		t.Error("the sudo password is in the output")
	}
	calls, _ := os.ReadFile(dir + "/calls")
	if !strings.Contains(string(calls), "restart tigervnc@2.service") {
		t.Errorf("systemctl calls: %s", calls)
	}
}
