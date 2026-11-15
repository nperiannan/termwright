// Package clirun logs in to a network device over SSH or Telnet, runs show commands at its command line
// and returns what they print. The team server reads a device's model, serial number and MAC address with
// it. Nothing is typed but the login, the command that turns paging off, the commands and exit.
package clirun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Target is a device and the login to use.
type Target struct {
	Protocol string // "SSH2" (or "") or "Telnet"
	Host     string
	Port     int // 0 = the protocol's
	User     string
	Password string
	HostKey  string // the SSH host key (SHA256 fingerprint) seen before; "" = take the one it has
}

// ErrLogin: the device refused the user and password.
var ErrLogin = errors.New("the login was refused")

// HostKeyError: the device's SSH host key is not the one seen before.
type HostKeyError struct{ Want, Got string }

func (e *HostKeyError) Error() string {
	return "its SSH host key changed (" + e.Got + ", was " + e.Want + ")"
}

var (
	promptRe = regexp.MustCompile(`[>#$%]\s*$`)
	moreRe   = regexp.MustCompile(`(?i)(-+\s*more\s*-+|press any key|<space>|q to quit)[^\n]*$`)
	ansiRe   = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b[()][A-Z0-9]|\x1b[=>78]|[\x00\x07\x08]`)
	loginRe  = regexp.MustCompile(`(?i)(login|user ?name)\s*:\s*$`)
	passRe   = regexp.MustCompile(`(?i)password\s*:\s*$`)
	badRe    = regexp.MustCompile(`(?i)(incorrect|login failed|authentication failed|access denied|invalid (user|login|password))`)
)

// Quiet is how long a prompt must stand with nothing after it to count as the end of the output.
var Quiet = 400 * time.Millisecond

// Run logs in, sends paging (when not ""), runs cmds one by one and logs out. outs has what each command
// printed; hostKey is the SSH host key's fingerprint ("" for Telnet).
func Run(ctx context.Context, t Target, paging string, cmds []string) (outs []string, hostKey string, err error) {
	telnet := strings.EqualFold(t.Protocol, "telnet")
	port := t.Port
	if port == 0 {
		port = map[bool]int{true: 23, false: 22}[telnet]
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(port))
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, "", err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	var r io.Reader
	var w io.Writer
	enter := "\r"
	if telnet {
		tr := &telnetConn{c: conn}
		r, w, enter = tr, conn, "\r\n"
	} else {
		cfg := &ssh.ClientConfig{User: t.User, Timeout: 10 * time.Second,
			Auth: []ssh.AuthMethod{ssh.Password(t.Password), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				a := make([]string, len(qs))
				for i := range a {
					a[i] = t.Password
				}
				return a, nil
			})},
			HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
				hostKey = ssh.FingerprintSHA256(k)
				if t.HostKey != "" && t.HostKey != hostKey {
					return &HostKeyError{Want: t.HostKey, Got: hostKey}
				}
				return nil
			}}
		sc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
		if err != nil {
			var he *HostKeyError
			if errors.As(err, &he) {
				return nil, hostKey, he
			}
			if strings.Contains(err.Error(), "unable to authenticate") {
				return nil, hostKey, ErrLogin
			}
			return nil, hostKey, err
		}
		cl := ssh.NewClient(sc, chans, reqs)
		defer cl.Close()
		sess, err := cl.NewSession()
		if err != nil {
			return nil, hostKey, err
		}
		defer sess.Close()
		if err := sess.RequestPty("vt100", 50, 250, ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
			return nil, hostKey, err
		}
		in, _ := sess.StdinPipe()
		out, _ := sess.StdoutPipe()
		if err := sess.Shell(); err != nil {
			return nil, hostKey, err
		}
		r, w = out, in
	}
	c := newCLI(r, w)
	if telnet {
		if err := c.login(ctx, t.User, t.Password, enter); err != nil {
			return nil, "", err
		}
	} else if _, err := c.wait(ctx, 20*time.Second); err != nil {
		return nil, hostKey, fmt.Errorf("no prompt after the login (%w)", err)
	}
	if paging != "" {
		c.reset()
		c.send(paging + enter)
		if _, err := c.wait(ctx, 15*time.Second); err != nil {
			return nil, hostKey, fmt.Errorf("%s: %w", paging, err)
		}
	}
	for _, cmd := range cmds {
		c.reset()
		c.send(cmd + enter)
		out, err := c.wait(ctx, 30*time.Second)
		if err != nil {
			return outs, hostKey, fmt.Errorf("%s: %w", cmd, err)
		}
		outs = append(outs, body(out))
	}
	c.send("exit" + enter)
	return outs, hostKey, nil
}

// body is a command's output without the line that echoes it and the prompt after it.
func body(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

type cli struct {
	data chan []byte
	done chan error
	w    io.Writer
	buf  strings.Builder
}

func newCLI(r io.Reader, w io.Writer) *cli {
	c := &cli{data: make(chan []byte, 64), done: make(chan error, 1), w: w}
	go func() {
		for {
			b := make([]byte, 4096)
			n, err := r.Read(b)
			if n > 0 {
				c.data <- b[:n]
			}
			if err != nil {
				c.done <- err
				return
			}
		}
	}()
	return c
}

func (c *cli) send(s string) { _, _ = io.WriteString(c.w, s) }
func (c *cli) reset()        { c.buf.Reset() }

func (c *cli) lastLine() string {
	s := c.buf.String()
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// take adds what came, answering a pager's "more" with a space.
func (c *cli) take(b []byte) {
	c.buf.WriteString(ansiRe.ReplaceAllString(strings.ReplaceAll(string(b), "\r", ""), ""))
	if l := c.lastLine(); moreRe.MatchString(l) {
		s := c.buf.String()
		c.buf.Reset()
		c.buf.WriteString(s[:len(s)-len(l)])
		c.send(" ")
	}
}

// wait reads until the last line is a prompt with nothing after it for Quiet; returns what came.
func (c *cli) wait(ctx context.Context, limit time.Duration) (string, error) {
	return c.until(ctx, limit, func(l string) bool { return promptRe.MatchString(l) })
}

func (c *cli) until(ctx context.Context, limit time.Duration, match func(string) bool) (string, error) {
	end := time.NewTimer(limit)
	defer end.Stop()
	quiet := time.NewTimer(time.Hour)
	defer quiet.Stop()
	for {
		select {
		case b := <-c.data:
			c.take(b)
			if match(c.lastLine()) {
				quiet.Reset(Quiet)
			} else {
				quiet.Stop()
			}
		case <-quiet.C:
			return c.buf.String(), nil
		case err := <-c.done:
			for len(c.data) > 0 {
				c.take(<-c.data)
			}
			if match(c.lastLine()) {
				return c.buf.String(), nil
			}
			return c.buf.String(), fmt.Errorf("the connection closed (%v)", err)
		case <-end.C:
			return c.buf.String(), errors.New("timed out waiting for the prompt")
		case <-ctx.Done():
			return c.buf.String(), ctx.Err()
		}
	}
}

// login answers a Telnet login: user name, password, then a prompt.
func (c *cli) login(ctx context.Context, user, pw, enter string) error {
	sentPw := false
	for i := 0; i < 6; i++ {
		out, err := c.until(ctx, 20*time.Second, func(l string) bool {
			return loginRe.MatchString(l) || passRe.MatchString(l) || promptRe.MatchString(l)
		})
		if err != nil {
			return fmt.Errorf("no login prompt (%w)", err)
		}
		l := c.lastLine()
		c.reset()
		switch {
		case sentPw && (loginRe.MatchString(l) || badRe.MatchString(out)):
			return ErrLogin
		case loginRe.MatchString(l):
			c.send(user + enter)
		case passRe.MatchString(l):
			if sentPw {
				return ErrLogin
			}
			c.send(pw + enter)
			sentPw = true
		default:
			return nil // a prompt
		}
	}
	return ErrLogin
}

// telnetConn reads a Telnet connection without its option talk, refusing every option but echo and
// suppress-go-ahead from the server.
type telnetConn struct {
	c     net.Conn
	state int // 0 data, 1 IAC, 2 IAC verb, 3 subnegotiation, 4 IAC in subnegotiation
	verb  byte
}

func (t *telnetConn) Read(p []byte) (int, error) {
	b := make([]byte, len(p))
	n, err := t.c.Read(b)
	o := 0
	for _, x := range b[:n] {
		switch t.state {
		case 0:
			if x == 255 {
				t.state = 1
			} else {
				p[o] = x
				o++
			}
		case 1:
			switch x {
			case 255:
				p[o] = x
				o++
				t.state = 0
			case 251, 252, 253, 254: // WILL WONT DO DONT
				t.verb, t.state = x, 2
			case 250:
				t.state = 3
			default:
				t.state = 0
			}
		case 2:
			switch t.verb {
			case 251:
				if x == 1 || x == 3 {
					_, _ = t.c.Write([]byte{255, 253, x})
				} else {
					_, _ = t.c.Write([]byte{255, 254, x})
				}
			case 253:
				_, _ = t.c.Write([]byte{255, 252, x})
			}
			t.state = 0
		case 3:
			if x == 255 {
				t.state = 4
			}
		case 4:
			if x == 240 {
				t.state = 0
			} else {
				t.state = 3
			}
		}
	}
	return o, err
}
