package main

import (
	"errors"
	"strings"
	"testing"
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
	if st.On != "on" || st.App != "blocked" || !st.CanFix || !strings.Contains(st.Fix, "--unblockapp '/Applications/Termwright.app'") {
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
	if st.Name != "ufw" || st.On != "on" || !st.CanFix || !strings.Contains(st.Fix, "ufw allow 69/udp") {
		t.Fatalf("%+v", st)
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
