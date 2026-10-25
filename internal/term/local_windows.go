//go:build windows

package term

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/UserExistsError/conpty"

	"github.com/termwright/termwright/internal/session"
)

type localConn struct {
	c    *conpty.ConPty
	info string
	once sync.Once
}

func (l *localConn) Read(p []byte) (int, error)  { return l.c.Read(p) }
func (l *localConn) Write(p []byte) (int, error) { return l.c.Write(p) }
func (l *localConn) Info() string                { return l.info }
func (l *localConn) Resize(cols, rows int) error { return l.c.Resize(cols, rows) }
func (l *localConn) Close() error {
	var err error
	l.once.Do(func() { err = l.c.Close() })
	return err
}

func startLocal(s session.Session, cols, rows int) (Conn, error) {
	cmdLine := strings.TrimSpace(s.LocalCommand)
	if cmdLine == "" {
		cmdLine = "powershell.exe -NoLogo"
		if _, err := os.Stat(os.Getenv("SystemRoot") + `\System32\WindowsPowerShell\v1.0\powershell.exe`); err != nil {
			cmdLine = os.Getenv("COMSPEC")
		}
	}
	opts := []conpty.ConPtyOption{conpty.ConPtyDimensions(cols, rows)}
	if s.LocalDir != "" {
		opts = append(opts, conpty.ConPtyWorkDir(expandHome(s.LocalDir)))
	} else if h, err := os.UserHomeDir(); err == nil {
		opts = append(opts, conpty.ConPtyWorkDir(h))
	}
	c, err := conpty.Start(cmdLine, opts...)
	if err != nil {
		return nil, err
	}
	lc := &localConn{c: c, info: "Local " + cmdLine}
	go func() {
		_, _ = c.Wait(context.Background())
		_ = lc.Close() // unblock readers when the shell exits
	}()
	return lc, nil
}
