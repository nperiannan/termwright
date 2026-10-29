package term

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
)

// RouteClient returns an SSH connection to the server a device is reached
// through - its jump host, or the jump station of its "ssh … host" remote
// command (tab viaTab's connection when it is connected) - and a function
// that ends a login of its own. It returns a nil client when the device is
// reached directly (or through a proxy).
func (m *Manager) RouteClient(ctx context.Context, promptTab string, s session.Session, viaTab string) (*ssh.Client, func(), string, error) {
	t := &Tab{id: promptTab, sess: s}
	login := func(ls session.Session) (*ssh.Client, func(), error) {
		c, _, cleanup, err := m.sshClient(ctx, t, ls, 1)
		if err != nil {
			return nil, nil, err
		}
		return c, func() {
			_ = c.Close()
			if cleanup != nil {
				cleanup()
			}
		}, nil
	}
	if s.JumpSession != "" {
		if m.LoadSession == nil {
			return nil, nil, "", errors.New("jump hosts are not available")
		}
		js, err := m.LoadSession(s.JumpSession)
		if err != nil {
			return nil, nil, "", fmt.Errorf("jump host %q: %w", s.JumpSession, err)
		}
		if js.Protocol != session.ProtoSSH2 {
			return nil, nil, "", fmt.Errorf("jump host %q must be an SSH2 session", s.JumpSession)
		}
		c, rel, err := login(js)
		if err != nil {
			return nil, nil, "", fmt.Errorf("jump host %s: %w", js.Name, err)
		}
		return c, rel, js.Name, nil
	}
	if _, hop := HopOf(s); hop && s.Protocol == session.ProtoSSH2 {
		if viaTab != "" {
			if c, err := m.sshClientOf(viaTab); err == nil {
				return c, func() {}, s.Hostname, nil
			}
		}
		c, rel, err := login(s) // the jump station itself
		if err != nil {
			return nil, nil, "", err
		}
		return c, rel, s.Hostname, nil
	}
	return nil, func() {}, "", nil
}
