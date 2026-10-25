//go:build !windows

package term

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/creack/pty"

	"github.com/termwright/termwright/internal/session"
)

type localConn struct {
	f    *os.File
	cmd  *exec.Cmd
	info string
	once sync.Once
}

func (l *localConn) Read(p []byte) (int, error)  { return l.f.Read(p) }
func (l *localConn) Write(p []byte) (int, error) { return l.f.Write(p) }
func (l *localConn) Info() string                { return l.info }
func (l *localConn) Resize(cols, rows int) error {
	return pty.Setsize(l.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
func (l *localConn) Close() error {
	l.once.Do(func() {
		_ = l.f.Close()
		if l.cmd.Process != nil {
			_ = l.cmd.Process.Kill()
		}
		go func() { _ = l.cmd.Wait() }()
	})
	return nil
}

func startLocal(s session.Session, cols, rows int) (Conn, error) {
	var cmd *exec.Cmd
	if strings.TrimSpace(s.LocalCommand) != "" {
		cmd = exec.Command("/bin/sh", "-c", s.LocalCommand)
	} else {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/bash"
			if runtime.GOOS == "darwin" {
				sh = "/bin/zsh"
			}
		}
		cmd = exec.Command(sh, "-l")
	}
	cmd.Env = append(os.Environ(), "TERM="+termType(s), "COLORTERM=truecolor", "TERM_PROGRAM=Termwright")
	if s.LocalDir != "" {
		cmd.Dir = expandHome(s.LocalDir)
	} else if h, err := os.UserHomeDir(); err == nil {
		cmd.Dir = h
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &localConn{f: f, cmd: cmd, info: "Local " + strings.Join(cmd.Args, " ")}, nil
}
