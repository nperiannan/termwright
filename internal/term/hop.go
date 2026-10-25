package term

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
)

// Hop is a session whose remote command is "ssh … host" (see session.Hop).
type Hop = session.Hop

// HopOf returns the hop of a session that runs "ssh … host" as its remote command.
func HopOf(s session.Session) (Hop, bool) { return session.HopOf(s) }

// sftpThroughHop starts sftp on the hop target over an exec channel of client.
func sftpThroughHop(client *ssh.Client, h Hop) (*sftp.Client, func(), error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	w, err1 := sess.StdinPipe()
	r, err2 := sess.StdoutPipe()
	if err = errors.Join(err1, err2); err != nil {
		_ = sess.Close()
		return nil, nil, err
	}
	var stderr limitedBuf
	sess.Stderr = &stderr
	if err := sess.Start(h.SFTPCommand()); err != nil {
		_ = sess.Close()
		return nil, nil, err
	}
	c, err := sftp.NewClientPipe(r, w, sftp.UseConcurrentWrites(true), sftp.UseConcurrentReads(true))
	if err != nil {
		_ = sess.Close()
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, nil, fmt.Errorf("SFTP to %s through the jump station: %s", h.Target, msg)
	}
	return c, func() { _ = sess.Close() }, nil
}

// hopSession runs SCP's remote command on the hop target.
type hopSession struct {
	*ssh.Session
	hop Hop
}

func (h hopSession) Start(cmd string) error { return h.Session.Start(h.hop.ExecCommand(cmd)) }

// limitedBuf keeps the first 4 KiB written to it.
type limitedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *limitedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if room := 4096 - l.b.Len(); room > 0 {
		l.b.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (l *limitedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

var _ io.Writer = (*limitedBuf)(nil)
