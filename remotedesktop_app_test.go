package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The script that is copied to the Linux machine must be embedded and valid bash.
func TestRemoteDesktopScript(t *testing.T) {
	if len(remoteDesktopScript) < 1000 || !bytes.HasPrefix(remoteDesktopScript, []byte("#!/usr/bin/env bash")) {
		t.Fatal("setup script is not embedded")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	cmd := exec.Command(bash, "-n")
	cmd.Stdin = bytes.NewReader(remoteDesktopScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}
}

func TestRemoteDesktopArgs(t *testing.T) {
	base := RemoteDesktopRun{RDP: true, VNC: true, RDPUser: "ixia-rdp", VNCUser: "ixia-vnc", CreateUsers: true}
	args, err := remoteDesktopArgs(base)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	for _, want := range []string{"--rdp-user ixia-rdp", "--vnc-user ixia-vnc", "--create-users", "--secrets-stdin", "--vnc-display 2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing from %q", want, got)
		}
	}
	if strings.Contains(got, "--force") || strings.Contains(got, "network") {
		t.Fatalf("unexpected options: %q", got)
	}

	net := base
	net.RDPNetwork, net.VNCNetwork, net.CIDR, net.Force = true, true, "10.1.2.0/24", true
	args, _ = remoteDesktopArgs(net)
	got = strings.Join(args, " ")
	for _, want := range []string{"--rdp-network", "--vnc-network", "--allow-cidr 10.1.2.0/24", "--force"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing from %q", want, got)
		}
	}

	// the access is chosen per service: only the chosen one is opened to the network
	one := base
	one.VNCNetwork = true
	args, _ = remoteDesktopArgs(one)
	if got = strings.Join(args, " "); !strings.Contains(got, "--vnc-network") || strings.Contains(got, "--rdp-network") {
		t.Fatalf("vnc only: %q", got)
	}
	one.VNCNetwork, one.RDPNetwork = false, true
	args, _ = remoteDesktopArgs(one)
	if got = strings.Join(args, " "); !strings.Contains(got, "--rdp-network") || strings.Contains(got, "--vnc-network") {
		t.Fatalf("rdp only: %q", got)
	}

	chk := base
	chk.Check = true
	args, _ = remoteDesktopArgs(chk)
	if got = strings.Join(args, " "); !strings.Contains(got, "--check") || strings.Contains(got, "--secrets-stdin") {
		t.Fatalf("check: %q", got)
	}

	onlyRDP := RemoteDesktopRun{RDP: true, RDPUser: "ixia-rdp"}
	args, _ = remoteDesktopArgs(onlyRDP)
	if got = strings.Join(args, " "); !strings.Contains(got, "--no-vnc") || strings.Contains(got, "--vnc-user") {
		t.Fatalf("rdp only: %q", got)
	}

	for _, bad := range []RemoteDesktopRun{
		{},
		{RDP: true, RDPUser: "root"},
		{RDP: true, RDPUser: "Bad Name"},
		{RDP: true, VNC: true, RDPUser: "same", VNCUser: "same"},
		{RDP: true, RDPUser: "ok", RDPNetwork: true, CIDR: "10.0.0.0/8; rm -rf /"},
	} {
		if _, err := remoteDesktopArgs(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestRemoteDesktopCommandAndStdin(t *testing.T) {
	cmd := remoteDesktopCommand("/tmp/termwright-rd-ab12.sh", []string{"--rdp-user", "ixia-rdp", "--secrets-stdin"}, false)
	for _, want := range []string{"sh -c ", "sudo -S -p ", "bash ", "/tmp/termwright-rd-ab12.sh", "rm -f"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("%q missing from %q", want, cmd)
		}
	}
	if chk := remoteDesktopCommand("/tmp/x.sh", []string{"--check"}, true); strings.Contains(chk, "sudo") {
		t.Fatalf("check must not use sudo: %q", chk)
	}
	in := string(remoteDesktopStdin("sudo pw", "acct:pw", "vncpw12"))
	if in != "sudo pw\nTWACCOUNT acct:pw\nTWVNC vncpw12\nTWEND\n" {
		t.Fatalf("stdin %q", in)
	}
	// no password ever appears in the command
	if strings.Contains(cmd, "sudo pw") || strings.Contains(cmd, "acct") {
		t.Fatal("command contains a password")
	}
}

// The command works with a script that reads the passwords the way the real
// one does (sudo's own line is read by sudo; here a stand-in sudo does that).
func TestRemoteDesktopCommandRuns(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/s.sh"
	fake := dir + "/sudo"
	body := "#!/usr/bin/env bash\nwhile IFS= read -r -t 5 line; do\n  case $line in\n    \"TWACCOUNT \"*) a=${line#TWACCOUNT } ;;\n    \"TWVNC \"*) v=${line#TWVNC } ;;\n    TWEND) break ;;\n  esac\ndone\necho \"account=$a vnc=$v args=$*\"\nexit 4\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// stand-in sudo: reads the password line like "sudo -S -p ''", then runs the command
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nread -r _pw\nshift 3\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", remoteDesktopCommand(script, []string{"--rdp-user", "a b"}, false))
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	cmd.Stdin = bytes.NewReader(remoteDesktopStdin("the sudo pw", "acct pw:1", "vnc12345"))
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 4 {
		t.Fatalf("exit: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "account=acct pw:1 vnc=vnc12345 args=--rdp-user a b") {
		t.Fatalf("output %q", out)
	}
	if strings.Contains(string(out), "the sudo pw") {
		t.Fatal("the sudo password reached the script")
	}
	if _, err := os.Stat(script); err == nil {
		t.Fatal("the script was not removed")
	}
}

func TestRemoteDesktopRunNote(t *testing.T) {
	r := &rdRun{rdpUser: "lab-rdp", vncUser: "lab-vnc"}
	for _, l := range []string{
		"[termwright-rd-ab12.sh] Creating user lab-rdp",
		"[termwright-rd-ab12.sh] Creating user lab-vnc",
		"[termwright-rd-ab12.sh] Configuring TigerVNC display :2 (TCP 5902)",
	} {
		r.note(l)
	}
	if !r.rdpNew || !r.vncNew || !r.vncSet {
		t.Fatalf("after creating both: %+v", r)
	}
	r.note("TWREMOVED vnc")
	if !r.rdpNew || r.vncNew || r.vncSet {
		t.Fatalf("a removed VNC must not keep its password: %+v", r)
	}
	r.note("TWREMOVED rdp")
	if r.rdpNew {
		t.Fatalf("a removed RDP must not keep its password: %+v", r)
	}
	// an existing user is not "new": its password is not the one given
	e := &rdRun{rdpUser: "lab-rdp", vncUser: "lab-vnc"}
	e.note("[x] Configuring TigerVNC display :2 (TCP 5902)")
	if e.rdpNew || e.vncNew || !e.vncSet {
		t.Fatalf("existing users: %+v", e)
	}
}
