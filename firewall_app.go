package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The computer's own firewall, for the built-in file servers: is it on, does it
// let devices reach Termwright, and the command that allows it (run only when
// the user asks, with the system's own administrator prompt).

// FirewallState is what the File Servers window shows.
type FirewallState struct {
	Name    string   `json:"name"`    // "macOS firewall", "Windows Defender Firewall", "ufw", "firewalld", "" (none found)
	On      string   `json:"on"`      // "on", "off", "unknown"
	App     string   `json:"app"`     // "allowed", "blocked", "ask" (the system asks the first time), "unknown"
	Notes   []string `json:"notes"`   // e.g. the network is Public, TFTP uses a new UDP port per file
	Fix     string   `json:"fix"`     // the command that allows Termwright (shown, and run by FirewallAllow)
	CanFix  bool     `json:"canFix"`  // FirewallAllow can run it here (with the system's password prompt)
	Program string   `json:"program"` // what is allowed: the app (macOS, Windows) or ports (Linux)
	Sudo    bool     `json:"sudo"`    // it runs as root with sudo: a sudo password can be typed (empty = the system's own prompt)
	run     []string // the commands as root runs them (Fix shows them with sudo)
}

// sudoFix: the commands as typed in a terminal, one per line, each with sudo.
func sudoFix(cmds []string) string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = "sudo " + c
	}
	return strings.Join(out, "\n")
}

// fwRun runs a command and returns its output (tests replace it).
var fwRun = func(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return runOutput(ctx, name, args...)
}

var fwLook = exec.LookPath

// macBundle: /Applications/Termwright.app for …/Termwright.app/Contents/MacOS/Termwright.
func macBundle(exe string) string {
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i > 0 {
		return exe[:i+4]
	}
	return exe
}

// FirewallCheck looks at the firewall for the servers' ports (UDP for TFTP, TCP for the others).
func (a *App) FirewallCheck() FirewallState { return a.FirewallCheckPorts(0, 0, 0) }

// FirewallCheckPorts is FirewallCheck for the ports typed in the File Servers window, saved or not (0 = the saved one):
// the command to allow them follows each change at once.
func (a *App) FirewallCheckPorts(tftp, ftp, http int) FirewallState {
	s := a.fsSettings()
	exe, _ := os.Executable()
	return firewallCheck(runtime.GOOS, exe, fwPort(tftp, s.TFTPPort), fwPort(ftp, s.FTPPort), fwPort(http, s.HTTPPort))
}

func fwPort(p, saved int) int {
	if p >= 1 && p <= 65535 {
		return p
	}
	return saved
}

func firewallCheck(goos, exe string, tftp, ftp, http int) FirewallState {
	tftpNote := "TFTP sends each file from a new UDP port: allow the program, not only port " + fmt.Sprint(tftp) + "."
	switch goos {
	case "darwin":
		const sf = "/usr/libexec/ApplicationFirewall/socketfilterfw"
		app := macBundle(exe)
		st := FirewallState{Name: "macOS firewall", On: "unknown", App: "unknown", Program: app}
		out, err := fwRun(sf, "--getglobalstate")
		if err != nil && out == "" {
			return st
		}
		low := strings.ToLower(out)
		switch {
		case strings.Contains(low, "disabled") || strings.Contains(low, "state = 0"):
			st.On, st.App = "off", "allowed"
			return st
		case strings.Contains(low, "enabled") || strings.Contains(low, "state = 1") || strings.Contains(low, "state = 2"):
			st.On = "on"
		}
		if b, _ := fwRun(sf, "--getblockall"); strings.Contains(strings.ToLower(b), "enabled") {
			st.App = "blocked"
			st.Notes = append(st.Notes, "Block all incoming connections is on: no program can receive connections, Termwright neither (System Settings → Network → Firewall → Options).")
		} else {
			b, _ := fwRun(sf, "--getappblocked", app)
			lb := strings.ToLower(b)
			switch {
			case strings.Contains(lb, "is blocked"):
				st.App = "blocked"
			case strings.Contains(lb, "permitted") || strings.Contains(lb, "allowed"):
				st.App = "allowed"
			default:
				st.App = "ask" // not in the list: macOS asks when a server starts
			}
		}
		st.run = []string{sf + " --add '" + app + "'", sf + " --unblockapp '" + app + "'"}
		st.Fix, st.CanFix, st.Sudo = sudoFix(st.run), true, true
		return st
	case "windows":
		st := FirewallState{Name: "Windows Defender Firewall", On: "unknown", App: "unknown", Program: exe}
		ps := `$p = Get-NetFirewallProfile -PolicyStore ActiveStore | Select-Object Name,Enabled;` +
			`$c = (Get-NetConnectionProfile | Select-Object -ExpandProperty NetworkCategory) -join ',';` +
			`$r = Get-NetFirewallApplicationFilter -Program '` + strings.ReplaceAll(exe, "'", "''") + `' -ErrorAction SilentlyContinue | Get-NetFirewallRule | Where-Object { $_.Direction -eq 'Inbound' -and $_.Enabled -eq 'True' } | Select-Object -ExpandProperty Action;` +
			`@{ profiles = $p; category = $c; actions = @($r | ForEach-Object { "$_" }) } | ConvertTo-Json -Compress -Depth 3`
		out, err := fwRun("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
		if err != nil && out == "" {
			return st
		}
		var v struct {
			Profiles json.RawMessage `json:"profiles"`
			Category string          `json:"category"`
			Actions  []string        `json:"actions"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(out)), &v) != nil {
			return st
		}
		var profs []struct {
			Name    string
			Enabled any
		}
		if json.Unmarshal(v.Profiles, &profs) != nil {
			var one struct {
				Name    string
				Enabled any
			}
			if json.Unmarshal(v.Profiles, &one) == nil {
				profs = append(profs, one)
			}
		}
		on := false
		for _, p := range profs {
			if fmt.Sprint(p.Enabled) == "true" || fmt.Sprint(p.Enabled) == "1" || fmt.Sprint(p.Enabled) == "True" {
				on = true
			}
		}
		if len(profs) > 0 {
			st.On = map[bool]string{true: "on", false: "off"}[on]
		}
		if !on && len(profs) > 0 {
			st.App = "allowed"
			return st
		}
		allow, block := false, false
		for _, a := range v.Actions {
			switch strings.ToLower(a) {
			case "allow", "2":
				allow = true
			case "block", "4":
				block = true
			}
		}
		switch {
		case block:
			st.App = "blocked"
		case allow:
			st.App = "allowed"
		default:
			st.App = "ask"
		}
		if strings.Contains(strings.ToLower(v.Category), "public") {
			st.Notes = append(st.Notes, "This network is Public: Windows blocks more on it. Allow Termwright for Public networks too, or mark the network Private.")
		}
		st.Fix = "New-NetFirewallRule -DisplayName 'Termwright file servers' -Direction Inbound -Program '" + exe + "' -Action Allow -Profile Any"
		st.CanFix = true
		return st
	default: // Linux and the rest: firewalld or ufw, by ports
		st := FirewallState{On: "unknown", App: "unknown", Program: fmt.Sprintf("UDP %d, TCP %d and %d", tftp, ftp, http)}
		if _, err := fwLook("firewall-cmd"); err == nil {
			st.Name = "firewalld"
			out, _ := fwRun("firewall-cmd", "--state")
			if strings.TrimSpace(out) == "running" {
				st.On = "on"
				// the tftp service (port 69) follows each transfer's extra UDP ports; another port is opened as it is
				add := fmt.Sprintf("--add-port=%d/udp --add-port=%d/tcp --add-port=%d/tcp", tftp, ftp, http)
				if tftp == 69 {
					add = fmt.Sprintf("--add-service=tftp --add-port=%d/tcp --add-port=%d/tcp", ftp, http)
				}
				st.run = []string{"firewall-cmd " + add, "firewall-cmd --permanent " + add} // now, and after a restart
				st.Notes = append(st.Notes, "firewalld's tftp service follows the extra UDP ports of each transfer; FTP passive ports may need a range too.")
			} else {
				st.On, st.App = "off", "allowed"
				return st
			}
		} else if _, err := fwLook("ufw"); err == nil {
			st.Name = "ufw"
			out, _ := fwRun("systemctl", "is-active", "ufw")
			if strings.TrimSpace(out) == "active" {
				st.On = "on"
				st.run = []string{fmt.Sprintf("ufw allow %d/udp", tftp), fmt.Sprintf("ufw allow %d/tcp", ftp), fmt.Sprintf("ufw allow %d/tcp", http)}
				st.Notes = append(st.Notes, tftpNote+" With ufw, allow the devices' network instead (ufw allow from <network>) if TFTP transfers stop after the first packet.")
			} else {
				st.On, st.App = "off", "allowed"
				return st
			}
		} else {
			return st // no firewall program found: nothing to check
		}
		_, pk := fwLook("pkexec")
		_, su := fwLook("sudo")
		st.Fix, st.Sudo = sudoFix(st.run), su == nil
		st.CanFix = pk == nil || su == nil
		return st
	}
}

// FirewallAllow runs the command FirewallCheck showed; the system asks for an
// administrator password itself.
func (a *App) FirewallAllow() (FirewallState, error) { return a.FirewallAllowWith(0, 0, 0, "") }

// FirewallAllowWith runs the command for the ports typed in the File Servers window (0 = the saved one). With a sudo
// password (macOS, Linux) it runs with sudo, the password on its standard input; without one the system asks for an
// administrator password itself (macOS's prompt, polkit's on Linux).
func (a *App) FirewallAllowWith(tftp, ftp, http int, sudoPw string) (FirewallState, error) {
	st := a.FirewallCheckPorts(tftp, ftp, http)
	if !st.CanFix || st.Fix == "" {
		return st, errors.New("this firewall cannot be changed from Termwright - run the command shown as an administrator")
	}
	var err error
	var out string
	root := strings.Join(st.run, " && ")
	switch {
	case runtime.GOOS == "windows":
		arg := "-NoProfile -Command " + st.Fix
		out, err = fwRun("powershell", "-NoProfile", "-Command", "Start-Process powershell -Verb RunAs -Wait -ArgumentList "+psQuote(arg))
	case sudoPw != "" && st.Sudo:
		out, err = fwSudo(sudoPw, root)
		if low := strings.ToLower(out); err != nil && (strings.Contains(low, "incorrect password") || strings.Contains(low, "sorry, try again")) {
			return st, errors.New("the sudo password was not accepted - nothing was changed")
		}
	case runtime.GOOS == "darwin":
		script := "do shell script " + appleQuote(root) + " with administrator privileges"
		out, err = fwRun("osascript", "-e", script)
	default:
		if _, perr := fwLook("pkexec"); perr != nil {
			return st, errors.New("type your sudo password: this computer has no system password prompt (pkexec)")
		}
		out, err = fwRun("pkexec", "sh", "-c", root)
	}
	if err != nil {
		msg := strings.TrimSpace(out)
		if strings.Contains(strings.ToLower(msg), "cancel") || strings.Contains(msg, "-128") {
			return st, errors.New("cancelled - nothing was changed")
		}
		if msg == "" {
			msg = err.Error()
		}
		return st, errors.New("the firewall was not changed: " + msg)
	}
	return a.FirewallCheckPorts(tftp, ftp, http), nil
}

// fwSudo runs a shell command as root with sudo, the password on its standard input (never on a command line).
var fwSudo = func(pw, cmd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "sudo", "-S", "-p", "", "sh", "-c", cmd)
	hideWindow(c)
	c.Stdin = strings.NewReader(pw + "\n")
	out, err := c.CombinedOutput()
	return string(out), err
}

func appleQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// FileServersTestFile puts a small file in the servers' folder for the device
// test and returns its name.
func (a *App) FileServersTestFile() (string, error) {
	const name = "termwright-test.txt"
	dir := a.fsSettings().Folder
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	body := "Termwright file server test - " + time.Now().Format(time.RFC3339) + "\nThis device reached this computer. You can delete this file.\n"
	return name, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
}
