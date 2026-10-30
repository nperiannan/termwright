package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/termwright/termwright/internal/term"
)

// The script "Set Up Remote Desktop" runs on the Linux machine (Ubuntu 24.04):
// XFCE with TigerVNC and/or xrdp, each only when it is not already running.
//
//go:embed scripts/remote-desktop/setup-remote-desktop.sh
var remoteDesktopScript []byte

// RemoteDesktopRun is what the Set Up Remote Desktop dialog asks for.
type RemoteDesktopRun struct {
	TabID       string `json:"tabId"` // connected SSH2 terminal tab of the machine
	RDP         bool   `json:"rdp"`
	VNC         bool   `json:"vnc"`
	RDPUser     string `json:"rdpUser"`
	VNCUser     string `json:"vncUser"`
	CreateUsers bool   `json:"createUsers"` // create users that do not exist
	Network     bool   `json:"network"`     // listen on all interfaces (default: the machine's own address only)
	CIDR        string `json:"cidr"`        // network to open in the firewall
	Check       bool   `json:"check"`       // only report
	Force       bool   `json:"force"`       // reconfigure services that already run

	// Passwords. "Same" means the saved password of the SSH login.
	SudoSame        bool   `json:"sudoSame"`
	SudoNone        bool   `json:"sudoNone"` // sudo needs no password
	SudoPassword    string `json:"sudoPassword"`
	AccountSame     bool   `json:"accountSame"`
	AccountPassword string `json:"accountPassword"` // of the new users
	VNCSame         bool   `json:"vncSame"`         // the account password (its first 8 characters count)
	VNCPassword     string `json:"vncPassword"`
}

// RemoteDesktopInfo tells the dialog what it can offer.
type RemoteDesktopInfo struct {
	SavedPassword bool   `json:"savedPassword"` // the SSH login's password is saved, so "same as the SSH login" works
	User          string `json:"user"`
}

var linuxUser = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
var cidrRe = regexp.MustCompile(`^[0-9a-fA-F:.]+(/[0-9]{1,3})?$`)

const remoteDesktopVNCDisplay = 2

// remoteDesktopArgs are the script's options for a run.
func remoteDesktopArgs(r RemoteDesktopRun) ([]string, error) {
	if !r.RDP && !r.VNC {
		return nil, errors.New("choose RDP, VNC or both")
	}
	var args []string
	if r.RDP {
		if !linuxUser.MatchString(r.RDPUser) || r.RDPUser == "root" {
			return nil, fmt.Errorf("%q is not a usable user name for RDP", r.RDPUser)
		}
		args = append(args, "--rdp-user", r.RDPUser)
	} else {
		args = append(args, "--no-rdp")
	}
	if r.VNC {
		if !linuxUser.MatchString(r.VNCUser) || r.VNCUser == "root" {
			return nil, fmt.Errorf("%q is not a usable user name for VNC", r.VNCUser)
		}
		args = append(args, "--vnc-user", r.VNCUser)
	} else {
		args = append(args, "--no-vnc")
	}
	if r.RDP && r.VNC && r.RDPUser == r.VNCUser {
		return nil, errors.New("RDP and VNC need different users: one desktop user cannot have both at once")
	}
	args = append(args, "--vnc-display", strconv.Itoa(remoteDesktopVNCDisplay))
	if r.CreateUsers {
		args = append(args, "--create-users")
	}
	if r.Network {
		if r.RDP {
			args = append(args, "--rdp-network")
		}
		if r.VNC {
			args = append(args, "--vnc-network")
		}
		if r.CIDR != "" {
			if !cidrRe.MatchString(r.CIDR) {
				return nil, fmt.Errorf("%q is not a network like 10.1.2.0/24", r.CIDR)
			}
			args = append(args, "--allow-cidr", r.CIDR)
		}
	}
	if r.Check {
		args = append(args, "--check")
	} else {
		args = append(args, "--secrets-stdin")
		if r.Force {
			args = append(args, "--force")
		}
	}
	return args, nil
}

// remoteDesktopCommand is the command run on the machine: the script with sudo
// (the password is read from standard input), removed again afterwards. It is
// wrapped in sh so it does not depend on the login shell.
func remoteDesktopCommand(remote string, args []string, check bool) string {
	q := make([]string, 0, len(args))
	for _, a := range args {
		q = append(q, term.ShellQuote(a))
	}
	run := "sudo -S -p '' bash " + term.ShellQuote(remote) + " " + strings.Join(q, " ")
	if check {
		run = "bash " + term.ShellQuote(remote) + " " + strings.Join(q, " ")
	}
	return "sh -c " + term.ShellQuote(run+"; rc=$?; rm -f "+term.ShellQuote(remote)+"; exit $rc")
}

// remoteDesktopStdin is what the command reads: sudo's password line, then
// the passwords for the script, ended by TWEND.
func remoteDesktopStdin(sudo, account, vnc string) []byte {
	var b strings.Builder
	b.WriteString(sudo + "\n")
	if account != "" {
		b.WriteString("TWACCOUNT " + account + "\n")
	}
	if vnc != "" {
		b.WriteString("TWVNC " + vnc + "\n")
	}
	b.WriteString("TWEND\n")
	return []byte(b.String())
}

// RemoteDesktopInfo says whether the SSH login's password is saved.
func (a *App) RemoteDesktopInfo(tabID string) (RemoteDesktopInfo, error) {
	if err := a.ready(); err != nil {
		return RemoteDesktopInfo{}, err
	}
	ti, err := a.terms.Info(tabID)
	if err != nil {
		return RemoteDesktopInfo{}, err
	}
	_, ok := a.savedLoginPassword(ti.Session.Path)
	return RemoteDesktopInfo{SavedPassword: ok, User: ti.Session.Username}, nil
}

func (a *App) savedLoginPassword(path string) (string, bool) {
	if path == "" || a.terms.SavedPassword == nil {
		return "", false
	}
	pw, ok := a.terms.SavedPassword(path)
	return pw, ok && pw != ""
}

var (
	rdMu   sync.Mutex
	rdRuns = map[string]context.CancelFunc{}
)

// StartRemoteDesktop copies the setup script to the machine of an SSH tab and
// runs it there in the background, over that tab's own connection: nothing is
// typed in the tab. Passwords are sent on the command's standard input, never
// on a command line, and are masked in everything that is reported. It returns
// a run id; the script's output arrives line by line as the event
// "rd:line:<id>" and the end as "rd:done:<id>" ({code, error}).
func (a *App) StartRemoteDesktop(r RemoteDesktopRun) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	args, err := remoteDesktopArgs(r)
	if err != nil {
		return "", err
	}
	ti, err := a.terms.Info(r.TabID)
	if err != nil {
		return "", err
	}
	saved, hasSaved := a.savedLoginPassword(ti.Session.Path)
	pick := func(same bool, typed, what string) (string, error) {
		if !same {
			return typed, nil
		}
		if !hasSaved {
			return "", fmt.Errorf("the SSH login's password is not saved, so the %s cannot be \"the same\": type it", what)
		}
		return saved, nil
	}

	var sudo, account, vnc string
	if !r.Check {
		if !r.SudoNone {
			if sudo, err = pick(r.SudoSame, r.SudoPassword, "sudo password"); err != nil {
				return "", err
			}
			if sudo == "" {
				return "", errors.New("type the sudo password (or choose \"no password needed\")")
			}
		}
		if r.CreateUsers {
			if account, err = pick(r.AccountSame, r.AccountPassword, "password of the new users"); err != nil {
				return "", err
			}
			if account == "" {
				return "", errors.New("type a password for the new users")
			}
		}
		if r.VNC {
			if r.VNCSame {
				vnc = account
				if vnc == "" {
					if vnc, err = pick(r.AccountSame, r.AccountPassword, "VNC password"); err != nil {
						return "", err
					}
				}
			} else {
				vnc = r.VNCPassword
			}
			if len(vnc) < 6 {
				return "", errors.New("the VNC password needs at least 6 characters")
			}
		}
		for _, p := range []string{sudo, account, vnc} {
			if strings.ContainsAny(p, "\r\n") {
				return "", errors.New("passwords cannot contain a line break")
			}
		}
	}

	remote, err := a.uploadRemoteDesktopScript(r.TabID)
	if err != nil {
		return "", err
	}
	id := randHex(6)
	ctx, cancel := context.WithCancel(context.Background())
	rdMu.Lock()
	rdRuns[id] = cancel
	rdMu.Unlock()

	secretsList := []string{sudo, account, vnc}
	mask := func(line string) string {
		for _, s := range secretsList {
			if len(s) >= 3 {
				line = strings.ReplaceAll(line, s, "••••")
			}
		}
		return line
	}
	cmd := remoteDesktopCommand(remote, args, r.Check)
	var stdin []byte
	if !r.Check {
		stdin = remoteDesktopStdin(sudo, account, vnc)
	}
	go func() {
		code, err := a.terms.ExecStream(ctx, r.TabID, cmd, stdin, func(line string) {
			emitter{a}.Emit("rd:line:"+id, mask(line))
		})
		rdMu.Lock()
		delete(rdRuns, id)
		rdMu.Unlock()
		cancel()
		res := map[string]any{"code": code, "error": ""}
		if err != nil {
			res["error"] = mask(err.Error())
		}
		emitter{a}.Emit("rd:done:"+id, res)
	}()
	return id, nil
}

// CancelRemoteDesktop stops a run started by StartRemoteDesktop.
func (a *App) CancelRemoteDesktop(id string) {
	rdMu.Lock()
	cancel := rdRuns[id]
	rdMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// uploadRemoteDesktopScript copies the setup script to the machine of an open
// SSH terminal tab, over that tab's own connection (no second login), and
// returns where it is. The file is private to the login user and is removed
// again by the command that runs it. It holds no passwords.
func (a *App) uploadRemoteDesktopScript(tabID string) (string, error) {
	if len(remoteDesktopScript) == 0 {
		return "", errors.New("the setup script is missing from this build")
	}
	id, _, err := a.terms.SFTPOpen(tabID, "")
	if err != nil {
		return "", fmt.Errorf("could not open a file channel on this SSH session: %w", err)
	}
	defer a.terms.SFTPClose(id)
	remote := "/tmp/termwright-rd-" + randHex(6) + ".sh"
	if err := a.terms.SFTPWriteFile(id, remote, remoteDesktopScript, 0o600); err != nil {
		return "", fmt.Errorf("could not copy the script to %s: %w", remote, err)
	}
	return remote, nil
}
