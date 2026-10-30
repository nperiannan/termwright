package term

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
)

// lineWriter splits what a command prints into lines (LF or CRLF; a lone CR
// ends a line too, as progress meters redraw with it).
type lineWriter struct {
	mu  sync.Mutex
	buf []byte
	fn  func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if line != "" {
			w.fn(line)
		}
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.fn(string(w.buf))
		w.buf = nil
	}
}

// ExecStream runs cmd on the SSH connection of a connected terminal tab, in a
// channel of its own: nothing is typed or shown in the tab and no second login
// happens. stdin is sent to the command (and then closed); what the command
// prints (stdout and stderr together) is passed line by line to onLine. It
// returns the command's exit status. Cancelling ctx ends the command.
func (m *Manager) ExecStream(ctx context.Context, tabID, cmd string, stdin []byte, onLine func(string)) (int, error) {
	client, err := m.sshClientOf(tabID)
	if err != nil {
		return -1, err
	}
	return execClient(ctx, client, cmd, stdin, onLine)
}

// ExecOn runs cmd over an SSH login of its own to session s (no tab, nothing is
// typed anywhere), like ExecStream. Prompts for a password or a host key use
// tabID. The login is closed afterwards.
func (m *Manager) ExecOn(ctx context.Context, tabID string, s session.Session, cmd string, stdin []byte, onLine func(string)) (int, error) {
	client, _, cleanup, err := m.sshClient(ctx, &Tab{id: tabID, sess: s}, s, 0)
	if err != nil {
		return -1, err
	}
	defer func() { _ = client.Close(); cleanup() }()
	return execClient(ctx, client, cmd, stdin, onLine)
}

// Exec runs cmd on the SSH connection to the tunnel's jump host, like ExecStream.
// It fails when the tunnel has no jump host.
func (tu *Tunnel) Exec(ctx context.Context, cmd string, stdin []byte, onLine func(string)) (int, error) {
	if tu.jc == nil {
		return -1, errors.New("this connection has no jump host to run a command on")
	}
	return execClient(ctx, tu.jc, cmd, stdin, onLine)
}

func execClient(ctx context.Context, client *ssh.Client, cmd string, stdin []byte, onLine func(string)) (int, error) {
	sess, err := client.NewSession()
	if err != nil {
		return -1, err
	}
	defer sess.Close()
	lw := &lineWriter{fn: onLine}
	sess.Stdout = lw
	sess.Stderr = lw
	in, err := sess.StdinPipe()
	if err != nil {
		return -1, err
	}
	if err := sess.Start(cmd); err != nil {
		return -1, err
	}
	go func() {
		_, _ = in.Write(stdin)
		_ = in.Close()
	}()
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = sess.Close()
		<-done
		lw.flush()
		return -1, errors.New("cancelled")
	}
	lw.flush()
	if err == nil {
		return 0, nil
	}
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		return ee.ExitStatus(), nil
	}
	return -1, err
}

// ShellQuote quotes s for a POSIX shell.
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// CanExec reports whether Exec can run a command (the tunnel goes through a jump host).
func (tu *Tunnel) CanExec() bool { return tu.jc != nil }
