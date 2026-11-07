package term

import (
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/termwright/termwright/internal/session"
)

// moshCommand is the command line that connects an SSH session with the mosh
// client of this computer: mosh logs in with ssh (port, key) and then talks to
// mosh-server on the host over UDP, so the session survives sleep, roaming and
// a changing address.
func moshCommand(s session.Session, look func(string) (string, error)) (string, error) {
	if s.Hostname == "" {
		return "", errors.New("no hostname")
	}
	bin := ""
	for _, c := range []string{"mosh", "/opt/homebrew/bin/mosh", "/usr/local/bin/mosh", "/usr/bin/mosh"} {
		if p, err := look(c); err == nil {
			bin = p
			break
		}
	}
	wsl := false
	if bin == "" && runtime.GOOS == "windows" {
		if _, err := look("wsl.exe"); err == nil {
			bin, wsl = "wsl.exe -e mosh", true
		}
	}
	if bin == "" {
		hint := "install it (macOS: brew install mosh; Debian/Ubuntu: sudo apt install mosh; Fedora/RHEL: sudo dnf install mosh)"
		if runtime.GOOS == "windows" {
			hint = "on Windows install mosh in WSL (wsl --install, then sudo apt install mosh there)"
		}
		return "", errors.New("Mosh needs the mosh program on this computer: " + hint + ". The host needs mosh-server, and UDP ports 60000-61000 open")
	}
	ssh := []string{"ssh"}
	if s.Port != 0 && s.Port != 22 {
		ssh = append(ssh, "-p", strconv.Itoa(s.Port))
	}
	if s.IdentityFile != "" && !wsl {
		ssh = append(ssh, "-i", cmdQuote(expandHome(s.IdentityFile)))
	}
	target := s.Hostname
	if s.Username != "" {
		target = s.Username + "@" + s.Hostname
	}
	cmd := cmdQuote(bin)
	if wsl {
		cmd = bin
	}
	if len(ssh) > 1 {
		cmd += " --ssh=" + cmdQuote(strings.Join(ssh, " "))
	}
	return cmd + " " + cmdQuote(target), nil
}

// startMosh runs mosh for an SSH session in a terminal of this computer.
func startMosh(s session.Session, cols, rows int) (Conn, error) {
	cmd, err := moshCommand(s, exec.LookPath)
	if err != nil {
		return nil, err
	}
	l := s
	l.LocalCommand, l.LocalDir = cmd, ""
	if runtime.GOOS != "windows" {
		// GUI apps on macOS start without the shell's PATH: mosh calls ssh and mosh-client
		l.LocalCommand = "PATH=\"$PATH:/opt/homebrew/bin:/usr/local/bin\" " + cmd
	}
	c, err := startLocal(l, cols, rows)
	if err != nil {
		return nil, err
	}
	return &infoConn{Conn: c, info: "Mosh " + s.Hostname}, nil
}

// infoConn shows its own connection info.
type infoConn struct {
	Conn
	info string
}

func (c *infoConn) Info() string { return c.info }

func cmdQuote(v string) string {
	if v != "" && strings.IndexFunc(v, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@%+=:,./-_", r))
	}) < 0 {
		return v
	}
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}
