package term

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/termwright/termwright/internal/session"
)

// WebProxy is a local SOCKS5 proxy (127.0.0.1 only) for a browser window:
// every connection goes out from an SSH server - a session's own server, the
// device's jump host or the jump station of an "ssh … host" remote command -
// which also resolves the host names, so internal names work.
type WebProxy struct {
	Addr string // 127.0.0.1:port
	Via  string // e.g. "jump-station"

	ln      net.Listener
	dial    func(addr string) (net.Conn, error)
	release func()

	mu     sync.Mutex
	conns  map[net.Conn]bool
	closed bool
}

// OpenWebProxy starts a WebProxy.
//
//   - device false: out through the session's own SSH server - the
//     connection of tab viaTab when it is connected, otherwise a login of
//     its own (through the session's jump host when it has one).
//   - device true: to reach the device's web interface - through its jump
//     host, the jump station of its "ssh … host" remote command (tab viaTab
//     when connected) or its proxy. It returns nil, nil when the device is
//     reached directly: the browser needs no proxy then.
//
// Prompts (passwords, host keys) are shown for promptTab ("" = no tab).
func (m *Manager) OpenWebProxy(ctx context.Context, promptTab string, s session.Session, viaTab string, device bool) (*WebProxy, error) {
	t := &Tab{id: promptTab, sess: s}
	wp := &WebProxy{conns: map[net.Conn]bool{}}
	login := func(ls session.Session) error {
		c, _, cleanup, err := m.sshClient(ctx, t, ls, 1)
		if err != nil {
			return err
		}
		wp.dial = func(a string) (net.Conn, error) { return c.Dial("tcp", a) }
		wp.release = func() {
			_ = c.Close()
			if cleanup != nil {
				cleanup()
			}
		}
		return nil
	}
	useTab := func() bool {
		if viaTab == "" {
			return false
		}
		c, err := m.sshClientOf(viaTab)
		if err != nil {
			return false
		}
		wp.dial = func(a string) (net.Conn, error) { return c.Dial("tcp", a) }
		return true
	}
	_, hop := HopOf(s)
	switch {
	case !device:
		if s.Protocol != session.ProtoSSH2 {
			return nil, errors.New("a web page can be opened through SSH2 sessions only")
		}
		wp.Via = s.Name
		if wp.Via == "" {
			wp.Via = s.Hostname
		}
		if !useTab() {
			if err := login(s); err != nil {
				return nil, err
			}
		}
	case s.JumpSession != "":
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
		wp.Via = js.Name
		if err := login(js); err != nil {
			return nil, fmt.Errorf("jump host %s: %w", js.Name, err)
		}
	case hop && s.Protocol == session.ProtoSSH2:
		wp.Via = s.Hostname
		if !useTab() {
			if err := login(s); err != nil { // the jump station itself (the remote command is not run)
				return nil, err
			}
		}
	case s.ProxyType != "":
		wp.Via = s.ProxyType + " proxy " + s.ProxyHost
		wp.dial = func(a string) (net.Conn, error) {
			c, _, err := m.dialNet(context.Background(), t, s, a, 0)
			return c, err
		}
	default:
		return nil, nil
	}
	if err := wp.listen(); err != nil {
		wp.Close()
		return nil, err
	}
	return wp, nil
}

func (wp *WebProxy) listen() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	wp.ln, wp.Addr = ln, ln.Addr().String()
	go serve(ln, wp.handle)
	return nil
}

func (wp *WebProxy) handle(c net.Conn) {
	if !wp.track(c, true) {
		c.Close()
		return
	}
	defer wp.track(c, false)
	target, err := socksHandshake(c)
	if err != nil {
		c.Close()
		return
	}
	rc, err := wp.dial(target)
	if err != nil {
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}) // connection refused
		c.Close()
		return
	}
	if !wp.track(rc, true) {
		c.Close()
		rc.Close()
		return
	}
	defer wp.track(rc, false)
	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		c.Close()
		rc.Close()
		return
	}
	pipe(c, rc)
}

func (wp *WebProxy) track(c net.Conn, add bool) bool {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	if add {
		if wp.closed {
			return false
		}
		wp.conns[c] = true
	} else {
		delete(wp.conns, c)
	}
	return true
}

// Close stops the proxy, its connections and its own SSH login.
func (wp *WebProxy) Close() {
	wp.mu.Lock()
	if wp.closed {
		wp.mu.Unlock()
		return
	}
	wp.closed = true
	if wp.ln != nil {
		_ = wp.ln.Close()
	}
	for c := range wp.conns {
		_ = c.Close()
	}
	wp.mu.Unlock()
	if wp.release != nil {
		wp.release()
	}
}
