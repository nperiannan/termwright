package main

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
)

func fakeFW(t *testing.T, outs map[string]string, have ...string) {
	t.Helper()
	run, look := fwRun, fwLook
	t.Cleanup(func() { fwRun, fwLook = run, look })
	fwRun = func(name string, args ...string) (string, error) {
		k := name + " " + strings.Join(args, " ")
		for p, o := range outs {
			if strings.HasPrefix(k, p) {
				return o, nil
			}
		}
		return "", errors.New("not found")
	}
	fwLook = func(f string) (string, error) {
		for _, h := range have {
			if h == f {
				return "/usr/bin/" + f, nil
			}
		}
		return "", errors.New("no")
	}
}

func TestFirewallMac(t *testing.T) {
	const sf = "/usr/libexec/ApplicationFirewall/socketfilterfw"
	exe := "/Applications/Termwright.app/Contents/MacOS/Termwright"
	fakeFW(t, map[string]string{sf + " --getglobalstate": "Firewall is enabled. (State = 1)", sf + " --getblockall": "Firewall has block all state set to disabled.", sf + " --getappblocked": "The application /Applications/Termwright.app is blocked.\n"})
	st := firewallCheck("darwin", exe, 69, 21, 8080)
	if st.On != "on" || st.App != "blocked" || !st.CanFix || !st.Sudo || !strings.Contains(st.Fix, "sudo "+sf+" --unblockapp '/Applications/Termwright.app'") {
		t.Fatalf("%+v", st)
	}
	fakeFW(t, map[string]string{sf + " --getglobalstate": "Firewall is disabled. (State = 0)"})
	if st := firewallCheck("darwin", exe, 69, 21, 8080); st.On != "off" || st.App != "allowed" {
		t.Fatalf("off: %+v", st)
	}
	fakeFW(t, map[string]string{sf + " --getglobalstate": "Firewall is enabled. (State = 1)", sf + " --getblockall": "disabled", sf + " --getappblocked": "The application is not part of the firewall"})
	if st := firewallCheck("darwin", exe, 69, 21, 8080); st.App != "ask" {
		t.Fatalf("ask: %+v", st)
	}
}

func TestFirewallWindows(t *testing.T) {
	fakeFW(t, map[string]string{"powershell": `{"profiles":[{"Name":"Domain","Enabled":true},{"Name":"Private","Enabled":true},{"Name":"Public","Enabled":true}],"category":"Public","actions":["Block"]}`})
	st := firewallCheck("windows", `C:\Program Files\Termwright\Termwright.exe`, 69, 21, 8080)
	if st.On != "on" || st.App != "blocked" || len(st.Notes) != 1 || !strings.Contains(st.Fix, "New-NetFirewallRule") {
		t.Fatalf("%+v", st)
	}
	fakeFW(t, map[string]string{"powershell": `{"profiles":{"Name":"Private","Enabled":true},"category":"Private","actions":[]}`})
	if st := firewallCheck("windows", `C:\T.exe`, 69, 21, 8080); st.App != "ask" || len(st.Notes) != 0 {
		t.Fatalf("ask: %+v", st)
	}
}

func TestFirewallLinux(t *testing.T) {
	fakeFW(t, map[string]string{"systemctl is-active ufw": "active\n"}, "ufw", "pkexec")
	st := firewallCheck("linux", "/usr/bin/termwright", 69, 21, 8080)
	if st.Name != "ufw" || st.On != "on" || !st.CanFix || !strings.Contains(st.Fix, "sudo ufw allow 69/udp") || strings.Contains(st.Fix, "sudo sudo") {
		t.Fatalf("%+v", st)
	}
	// other ports: the command follows them; every line has sudo
	fakeFW(t, map[string]string{"systemctl is-active ufw": "active\n"}, "ufw", "sudo")
	st = firewallCheck("linux", "/x", 1069, 2121, 8088)
	if st.Fix != "sudo ufw allow 1069/udp\nsudo ufw allow 2121/tcp\nsudo ufw allow 8088/tcp" || !st.Sudo || !st.CanFix {
		t.Fatalf("ports: %q %+v", st.Fix, st)
	}
	fakeFW(t, map[string]string{"firewall-cmd --state": "running"}, "firewall-cmd", "pkexec")
	st = firewallCheck("linux", "/x", 1069, 21, 8080)
	if !strings.Contains(st.Fix, "sudo firewall-cmd --add-port=1069/udp --add-port=21/tcp --add-port=8080/tcp") || !strings.Contains(st.Fix, "sudo firewall-cmd --permanent --add-port=1069/udp") || st.Sudo {
		t.Fatalf("firewalld ports: %q", st.Fix)
	}
	if st = firewallCheck("linux", "/x", 69, 21, 8080); !strings.Contains(st.Fix, "--add-service=tftp") {
		t.Fatalf("firewalld 69: %q", st.Fix)
	}
	fakeFW(t, map[string]string{"firewall-cmd --state": "not running"}, "firewall-cmd")
	if st := firewallCheck("linux", "/x", 69, 21, 8080); st.Name != "firewalld" || st.On != "off" {
		t.Fatalf("firewalld off: %+v", st)
	}
	fakeFW(t, map[string]string{})
	if st := firewallCheck("linux", "/x", 69, 21, 8080); st.Name != "" {
		t.Fatalf("none: %+v", st)
	}
}

// Allow with a sudo password runs the commands with sudo (the password on standard input), for the ports typed.
func TestFirewallAllowSudo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UAC there")
	}
	fakeFW(t, map[string]string{"systemctl is-active ufw": "active\n", "firewall-cmd --state": "running", "/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate": "Firewall is enabled. (State = 1)"}, "ufw", "sudo")
	old := fwSudo
	defer func() { fwSudo = old }()
	var gotPw, gotCmd string
	fwSudo = func(pw, cmd string) (string, error) { gotPw, gotCmd = pw, cmd; return "", nil }
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	a := &App{cfg: cfg}
	if _, err := a.FirewallAllowWith(1069, 2121, 8088, "pw1"); err != nil {
		t.Fatal(err)
	}
	if gotPw != "pw1" || gotCmd == "" || strings.Contains(gotCmd, "sudo ") {
		t.Fatalf("%q %q", gotPw, gotCmd)
	}
	if runtime.GOOS == "linux" && gotCmd != "ufw allow 1069/udp && ufw allow 2121/tcp && ufw allow 8088/tcp" {
		t.Fatalf("linux: %q", gotCmd)
	}
	fwSudo = func(pw, cmd string) (string, error) { return "Sorry, try again.\nsudo: 1 incorrect password attempt", errors.New("exit 1") }
	if _, err := a.FirewallAllowWith(1069, 2121, 8088, "bad"); err == nil || !strings.Contains(err.Error(), "not accepted") {
		t.Fatal(err)
	}
}
