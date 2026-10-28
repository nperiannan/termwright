package term

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
)

// Tunnel reaches one address for a session, through the session's jump host
// (one SSH login, shared by every connection) or proxy, or directly. With
// Listen it also forwards a local port there, for programs such as an RDP
// client that cannot use the jump host themselves.
type Tunnel struct {
	m       *Manager
	tab     *Tab
	sess    session.Session
	addr    string
	jc      *ssh.Client
	cleanup func()
	// Via describes the route ("" = direct), e.g. "via jump-station".
	Via string

	mu     sync.Mutex
	ln     net.Listener
	conns  map[net.Conn]bool
	closed bool
}

// OpenTunnel prepares connections to addr for session s. Prompts (jump host
// password, host key) and status messages use tabID.
func (m *Manager) OpenTunnel(ctx context.Context, s session.Session, tabID, addr string) (*Tunnel, error) {
	t := &Tab{id: tabID, sess: s}
	tu := &Tunnel{m: m, tab: t, sess: s, addr: addr, conns: map[net.Conn]bool{}}
	if s.JumpSession != "" {
		if m.LoadSession == nil {
			return nil, errors.New("jump hosts are not available")
		}
		js, err := m.LoadSession(s.JumpSession)
		if err != nil {
			return nil, fmt.Errorf("jump host %q: %w", s.JumpSession, err)
		}
		if js.Protocol != session.ProtoSSH2 {
			return nil, fmt.Errorf("jump host %q must be an SSH2 session", s.JumpSession)
		}
		m.status(t, StateConnecting, "Connecting via jump host "+js.Name+"...")
		m.step(t, "route")
		jc, _, cleanup, err := m.sshClient(ctx, t, js, 1)
		if err != nil {
			return nil, fmt.Errorf("jump host %s: %w", js.Name, err)
		}
		tu.jc, tu.cleanup, tu.Via = jc, cleanup, "via "+js.Name
	} else if s.ProxyType != "" {
		tu.Via = "via " + s.ProxyType + " proxy " + s.ProxyHost
	}
	return tu, nil
}

// Dial opens one connection to the tunnel's address.
func (tu *Tunnel) Dial(ctx context.Context) (net.Conn, error) {
	if tu.jc != nil {
		c, err := tu.jc.Dial("tcp", tu.addr)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", tu.Via, err)
		}
		return c, nil
	}
	c, _, err := tu.m.dialNet(ctx, tu.tab, tu.sess, tu.addr, 0)
	return c, err
}

// Direct reports whether programs can connect to the address themselves.
func (tu *Tunnel) Direct() bool { return tu.jc == nil && tu.sess.ProxyType == "" }

// Listen forwards a local port (127.0.0.1, chosen by the system) to the
// address and returns it.
func (tu *Tunnel) Listen() (string, error) {
	tu.mu.Lock()
	defer tu.mu.Unlock()
	if tu.ln != nil {
		return tu.ln.Addr().String(), nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	tu.ln = ln
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go tu.forward(c)
		}
	}()
	return ln.Addr().String(), nil
}

func (tu *Tunnel) forward(c net.Conn) {
	if !tu.track(c, true) {
		_ = c.Close()
		return
	}
	defer tu.track(c, false)
	defer c.Close()
	d, err := tu.Dial(context.Background())
	if err != nil {
		tu.m.emit.Emit("toast", map[string]string{"level": "error", "message": "Tunnel: " + err.Error()})
		return
	}
	defer d.Close()
	Pipe(c, d)
}

func (tu *Tunnel) track(c net.Conn, add bool) bool {
	tu.mu.Lock()
	defer tu.mu.Unlock()
	if add {
		if tu.closed {
			return false
		}
		tu.conns[c] = true
	} else {
		delete(tu.conns, c)
	}
	return true
}

// Connections returns how many forwarded connections are open.
func (tu *Tunnel) Connections() int {
	tu.mu.Lock()
	defer tu.mu.Unlock()
	return len(tu.conns)
}

// Close stops the local port, open connections and the jump host login.
func (tu *Tunnel) Close() {
	tu.mu.Lock()
	if tu.closed {
		tu.mu.Unlock()
		return
	}
	tu.closed = true
	if tu.ln != nil {
		_ = tu.ln.Close()
	}
	for c := range tu.conns {
		_ = c.Close()
	}
	tu.mu.Unlock()
	if tu.jc != nil {
		_ = tu.jc.Close()
	}
	if tu.cleanup != nil {
		tu.cleanup()
	}
}

// Pipe copies both ways until either side closes.
func Pipe(a, b io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	cp := func(dst io.WriteCloser, src io.Reader) {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}
