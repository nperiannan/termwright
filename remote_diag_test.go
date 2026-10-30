package main

import (
	"net"
	"os/exec"
	"strings"
	"testing"
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
