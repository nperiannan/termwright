package term

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/net/proxy"

	"github.com/termwright/termwright/internal/session"
)

// chainConn closes extra resources (e.g. a jump-host client) with the conn.
type chainConn struct {
	net.Conn
	extra []io.Closer
}

func (c *chainConn) Close() error {
	err := c.Conn.Close()
	for _, x := range c.extra {
		_ = x.Close()
	}
	return err
}

// dialNet opens a TCP connection to addr for session s, going through the
// session's jump host (another saved session) or proxy when configured.
// The returned string describes the route for the status bar.
func (m *Manager) dialNet(ctx context.Context, t *Tab, s session.Session, addr string, depth int) (net.Conn, string, error) {
	if s.JumpSession != "" {
		if depth >= 5 {
			return nil, "", errors.New("jump host chain is too long (loop?)")
		}
		if m.LoadSession == nil {
			return nil, "", errors.New("jump hosts are not available")
		}
		js, err := m.LoadSession(s.JumpSession)
		if err != nil {
			return nil, "", fmt.Errorf("jump host %q: %w", s.JumpSession, err)
		}
		if js.Protocol != session.ProtoSSH2 {
			return nil, "", fmt.Errorf("jump host %q must be an SSH2 session", s.JumpSession)
		}
		m.status(t, StateConnecting, "Connecting via jump host "+js.Name+"...")
		jc, _, cleanup, err := m.sshClient(ctx, t, js, depth+1)
		if err != nil {
			return nil, "", fmt.Errorf("jump host %s: %w", js.Name, err)
		}
		c, err := jc.Dial("tcp", addr)
		if err != nil {
			_ = jc.Close()
			cleanup()
			return nil, "", fmt.Errorf("via %s: %w", js.Name, err)
		}
		return &chainConn{Conn: c, extra: []io.Closer{jc, closerFunc(cleanup)}}, "  via " + js.Name, nil
	}
	d := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	switch s.ProxyType {
	case "":
		c, err := d.DialContext(ctx, "tcp", addr)
		return c, "", err
	case "socks5", "http":
		if s.ProxyHost == "" {
			return nil, "", errors.New("proxy host is not set")
		}
		paddr := net.JoinHostPort(s.ProxyHost, strconv.Itoa(s.ProxyPort))
		var pass string
		if s.ProxyUser != "" {
			r, err := m.prompts.Ask(ctx, Prompt{TabID: t.id, Kind: "credentials", Title: "Proxy Password",
				Message: s.ProxyUser + "@" + paddr, Fields: []PromptField{{Label: "Password", Secret: true}}})
			if err != nil {
				return nil, "", err
			}
			pass = r.Values[0]
		}
		via := "  via " + s.ProxyType + " " + paddr
		if s.ProxyType == "http" {
			c, err := httpConnect(ctx, d, paddr, addr, s.ProxyUser, pass)
			return c, via, err
		}
		var auth *proxy.Auth
		if s.ProxyUser != "" {
			auth = &proxy.Auth{User: s.ProxyUser, Password: pass}
		}
		pd, err := proxy.SOCKS5("tcp", paddr, auth, d)
		if err != nil {
			return nil, "", err
		}
		c, err := pd.(proxy.ContextDialer).DialContext(ctx, "tcp", addr)
		return c, via, err
	default:
		return nil, "", fmt.Errorf("unknown proxy type %q", s.ProxyType)
	}
}

type closerFunc func()

func (f closerFunc) Close() error {
	if f != nil {
		f()
	}
	return nil
}

// httpConnect tunnels through an HTTP proxy with CONNECT.
func httpConnect(ctx context.Context, d *net.Dialer, proxyAddr, target, user, pass string) (net.Conn, error) {
	c, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
	if user != "" {
		req += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass)) + "\r\n"
	}
	req += "\r\n"
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	if _, err := io.WriteString(c, req); err != nil {
		c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		c.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		c.Close()
		return nil, fmt.Errorf("proxy refused CONNECT: %s", resp.Status)
	}
	_ = c.SetDeadline(time.Time{})
	if br.Buffered() > 0 {
		return &bufConn{Conn: c, r: br}, nil
	}
	return c, nil
}

type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }
