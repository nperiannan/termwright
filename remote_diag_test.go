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

func findingTexts(f []DiagFinding) string {
	var b strings.Builder
	for _, x := range f {
		b.WriteString(x.Level + ": " + x.Text + " | " + x.Detail + "\n")
	}
	return b.String()
}

const diagBlocked = `Termwright remote desktop diagnosis
Session:    testgen-vm (VNC) (VNC)

== From this computer: connection test ==
connect:  OK in 88 ms
handshake: server says RFB 003.003
security:  the server refuses the connection: Too many security failures
=> TigerVNC has blocked this address after several wrong passwords

== On the machine ==
== Machine ==
sudo:   not available (logs that need root are skipped; the commands to run are shown)
--- sshd forwarding (needed to reach 127.0.0.1 through this SSH session)
(settings in the files above; none listed = the default, forwarding allowed)
--- Firewall
(needs sudo - run on the machine: sudo ufw status)
== VNC (TigerVNC) - port 5902 (display :2), user nperiannan-vnc ==
this machine to itself: 127.0.0.1:5902 accepts connections
what the server says first: RFB 003.003   (a working VNC server says RFB 003.008; RFB 003.003 means it has blocked this address after wrong passwords - restart the service or wait)
--- service
tigervnc@2.service: active, enabled
== End ==
`

func TestAnalyzeBlockedVNC(t *testing.T) {
	f, fix := analyze(diagBlocked, "VNC")
	got := findingTexts(f)
	if fix != "VNC" || len(f) < 2 || f[0].Level != "error" || !strings.Contains(f[0].Text, "blocked this address") {
		t.Fatalf("fix %q\n%s", fix, got)
	}
	if !strings.Contains(got, "need sudo") {
		t.Fatalf("the skipped sudo checks are not mentioned:\n%s", got)
	}
	r := withResult(diagBlocked, f)
	i, j := strings.Index(r, "== Result =="), strings.Index(r, "== From this computer")
	if i < 0 || j < 0 || i > j || !strings.HasPrefix(r, "Termwright remote desktop diagnosis") || !strings.Contains(r, "PROBLEM: The VNC server has blocked") {
		t.Fatalf("the result is not at the top:\n%s", r)
	}
}

func TestAnalyzeOK(t *testing.T) {
	rep := `Session: x

== From this computer: connection test ==
connect:  OK in 41 ms
handshake: server says RFB 003.008
security:  the server offers VNC Authentication (2)
=> Termwright's viewer can use one of these

== On the machine ==
== VNC (TigerVNC) - port 5902 (display :2), user u ==
this machine to itself: 127.0.0.1:5902 accepts connections
what the server says first: RFB 003.008   (a working VNC server says RFB 003.008; RFB 003.003 means it has blocked this address after wrong passwords - restart the service or wait)
tigervnc@2.service: active, enabled
== End ==
`
	f, fix := analyze(rep, "VNC")
	if fix != "" || len(f) != 1 || f[0].Level != "ok" {
		t.Fatalf("fix %q\n%s", fix, findingTexts(f))
	}
}

func TestAnalyzeProblems(t *testing.T) {
	for name, tc := range map[string]struct {
		rep, want, fix string
	}{
		"stopped":       {"== VNC (TigerVNC) - port 5902 (display :2), user u ==\nthis machine to itself: 127.0.0.1:5902 refuses connections\ntigervnc@2.service: inactive, enabled\n", "Nothing listens on the VNC port", "VNC"},
		"unit failed":   {"== RDP (xrdp) - port 3389, user u ==\nxrdp: failed, enabled\nxrdp-sesman: active, enabled\n", "The service xrdp is failed", "RDP"},
		"no forwarding": {"== From this computer: connection test ==\nconnect:  FAILED after 3 ms: open failed\n=> the SSH server refuses port forwarding\n== On the machine ==\n== Machine ==\n--- sshd forwarding\nallowtcpforwarding no \n", "The machine's RDP port cannot be reached", ""},
		"bad login":     {"== VNC (TigerVNC) - port 5902 (display :2), user u ==\n SConnection: Authentication failed\n", "failed logins", "VNC"},
		"user missing":  {"== RDP (xrdp) - port 3389, user ghost ==\nthe user ghost does not exist on this machine\n", "user ghost does not exist", ""},
	} {
		f, fix := analyze(tc.rep, "RDP")
		if got := findingTexts(f); !strings.Contains(got, tc.want) || fix != tc.fix {
			t.Errorf("%s: fix %q\n%s", name, fix, got)
		}
	}
	// a failed login followed by a good one is no problem
	if f, _ := analyze("== VNC (TigerVNC) - port 5902 (display :2), user u ==\n SConnection: Authentication failed\n SConnection: Authentication successful\n", "VNC"); strings.Contains(findingTexts(f), "failed logins") {
		t.Fatal("old failure reported")
	}
}

func TestChecksOf(t *testing.T) {
	c := checksOf(diagBlocked)
	got := findingTexts(c)
	for _, want := range []string{"error: The server answers RFB 003.003", "ok: The VNC service listens on the machine", "ok: Service tigervnc@2.service is active", "ok: The SSH server allows port forwarding", "info: sudo is not available", "ok: The port can be reached from this computer (in 88 ms)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
