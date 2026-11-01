package twcore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/term"
)

// Screen receives what a terminal session prints; the app implements it
// (gomobile turns it into a Java interface). Output is called from a Go
// thread, in order; Closed once, when the session ends.
type Screen interface {
	Output(data []byte)
	Closed(reason string)
}

// termRequest is what Open needs: the device, its login, and the jump host to
// go through when there is one.
type termRequest struct {
	Protocol   string    `json:"protocol"` // "SSH2" or "Telnet"
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	User       string    `json:"user"`
	Password   string    `json:"password"`
	Key        string    `json:"key"` // private key (PEM / OpenSSH)
	Passphrase string    `json:"passphrase"`
	HostKey    string    `json:"hostKey"` // SSH: the key accepted before ("" = first time, the key is returned)
	Jump       *jumpHost `json:"jump"`
	Cols       int       `json:"cols"`
	Rows       int       `json:"rows"`
	Term       string    `json:"term"`
}

// JumpHostKeyChanged starts the error when the jump host's key is not the one
// accepted before (HostKeyChanged is the device's own).
const JumpHostKeyChanged = "jump " + HostKeyChanged

// Terminal is an open session.
type Terminal struct {
	mu      sync.Mutex
	w       io.Writer
	resize  func(cols, rows int) error
	brk     func() error
	close   func()
	hostKey string
	closed  bool
	// Telnet: answer the first login and password prompts with the saved login
	user, password string
	sentUser       bool
	sentPassword   bool
	window         []byte
}

// Open connects to the device and starts sending what it prints to screen.
// For SSH, a device seen for the first time is refused with HostKeyChanged and
// its key, for the app to show and save (then opened again with hostKey set).
func Open(reqJSON string, screen Screen) (*Terminal, error) {
	var r termRequest
	if err := json.Unmarshal([]byte(reqJSON), &r); err != nil {
		return nil, err
	}
	if r.Host == "" {
		return nil, errors.New("no address")
	}
	if r.Cols <= 0 || r.Rows <= 0 {
		r.Cols, r.Rows = 80, 24
	}
	if r.Term == "" {
		r.Term = "xterm-256color"
		if r.Protocol == "Telnet" {
			r.Term = "xterm" // what the desktop sends; some switches do not know xterm-256color
		}
	}
	var jump *ssh.Client
	if r.Jump != nil && r.Jump.Host != "" {
		c, _, err := dialJump(r.Jump)
		if err != nil {
			if strings.HasPrefix(err.Error(), HostKeyChanged) {
				return nil, errors.New("jump " + err.Error())
			}
			return nil, errors.New("jump host " + r.Jump.Host + ": " + err.Error())
		}
		jump = c
	}
	dial := func(addr string) (net.Conn, error) {
		if jump != nil {
			return jump.Dial("tcp", addr)
		}
		return net.DialTimeout("tcp", addr, 15*time.Second)
	}
	var t *Terminal
	var rd io.Reader
	var err error
	switch r.Protocol {
	case "Telnet":
		t, rd, err = openTelnet(&r, dial)
	default:
		t, rd, err = openSSH(&r, dial)
	}
	if err != nil {
		if jump != nil {
			_ = jump.Close()
		}
		return nil, err
	}
	inner := t.close
	t.close = func() {
		inner()
		if jump != nil {
			_ = jump.Close()
		}
	}
	go t.pump(rd, screen)
	return t, nil
}

func openSSH(r *termRequest, dial func(string) (net.Conn, error)) (*Terminal, io.Reader, error) {
	port := r.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(r.Host, strconv.Itoa(port))
	seen := ""
	cfg, err := clientConfig(r.User, r.Password, r.Key, r.Passphrase, func(k ssh.PublicKey) error {
		seen = fingerprint(k)
		if r.HostKey == "" || r.HostKey != seen {
			return errors.New(HostKeyChanged + seen)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	nc, err := dial(addr)
	if err != nil {
		return nil, nil, err
	}
	_ = nc.SetDeadline(time.Now().Add(20 * time.Second))
	cc, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		_ = nc.Close()
		if seen != "" && strings.Contains(err.Error(), HostKeyChanged) {
			return nil, nil, errors.New(HostKeyChanged + seen)
		}
		return nil, nil, err
	}
	_ = nc.SetDeadline(time.Time{})
	client := ssh.NewClient(cc, chans, reqs)
	s, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}
	if err := s.RequestPty(r.Term, r.Rows, r.Cols, modes); err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	in, _ := s.StdinPipe()
	out, _ := s.StdoutPipe()
	s.Stderr = nil
	if err := s.Shell(); err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	t := &Terminal{w: in, hostKey: seen,
		resize: func(c, rw int) error { return s.WindowChange(rw, c) },
		brk: func() error { // RFC 4335: a break of 500 ms
			_, err := s.SendRequest("break", false, ssh.Marshal(struct{ Ms uint32 }{500}))
			return err
		},
		close: func() { _ = s.Close(); _ = client.Close() }}
	// a keepalive every 30 s: home routers and mobile networks drop a quiet connection
	go func() {
		tk := time.NewTicker(30 * time.Second)
		defer tk.Stop()
		for range tk.C {
			if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				return // closed
			}
		}
	}()
	return t, out, nil
}

func openTelnet(r *termRequest, dial func(string) (net.Conn, error)) (*Terminal, io.Reader, error) {
	s := session.Session{Protocol: "Telnet", Hostname: r.Host, Port: r.Port, TermType: r.Term}
	c, err := term.DialTelnet(context.Background(), s, r.Cols, r.Rows, func(_ context.Context, addr string) (net.Conn, string, error) {
		nc, err := dial(addr)
		return nc, "", err
	})
	if err != nil {
		return nil, nil, err
	}
	t := &Terminal{w: c, resize: c.Resize, close: func() { _ = c.Close() }, user: r.User, password: r.Password}
	if b, ok := c.(interface{ SendBreak() error }); ok {
		t.brk = b.SendBreak
	}
	return t, c, nil
}

// clientConfig is an SSH login: the key and/or the password (also for
// keyboard-interactive prompts), and the check of the server's key.
func clientConfig(user, password, key, passphrase string, check func(ssh.PublicKey) error) (*ssh.ClientConfig, error) {
	var auth []ssh.AuthMethod
	if strings.TrimSpace(key) != "" {
		var signer ssh.Signer
		var err error
		if passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(key), []byte(passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(key))
		}
		if err != nil {
			return nil, errors.New("the private key could not be read: " + err.Error())
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if password != "" {
		auth = append(auth, ssh.Password(password), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			a := make([]string, len(qs))
			for i := range a {
				a[i] = password
			}
			return a, nil
		}))
	}
	return &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error { return check(k) },
		Timeout:         15 * time.Second,
	}, nil
}

func (t *Terminal) pump(rd io.Reader, screen Screen) {
	buf := make([]byte, 32*1024)
	reason := ""
	for {
		n, err := rd.Read(buf)
		if n > 0 {
			b := append([]byte(nil), buf[:n]...)
			t.autoLogin(b)
			screen.Output(b)
		}
		if err != nil {
			if err != io.EOF {
				reason = err.Error()
			}
			break
		}
	}
	t.mu.Lock()
	was := t.closed
	t.closed = true
	t.mu.Unlock()
	if was {
		reason = ""
	}
	t.close()
	screen.Closed(reason)
}

// autoLogin answers a Telnet device's first login and password prompts with
// the saved login (SSH logs in before the shell starts).
func (t *Terminal) autoLogin(b []byte) {
	t.mu.Lock()
	if t.user == "" && t.password == "" || t.sentPassword {
		t.mu.Unlock()
		return
	}
	t.window = append(t.window, b...)
	if len(t.window) > 256 {
		t.window = t.window[len(t.window)-256:]
	}
	send := ""
	switch term.BasicPrompt(string(t.window)) {
	case "user":
		if t.user != "" && !t.sentUser {
			t.sentUser, send = true, t.user
		}
	case "password":
		if t.password != "" {
			t.sentPassword, send = true, t.password
		}
	}
	if send != "" {
		t.window = t.window[:0]
	}
	t.mu.Unlock()
	if send != "" {
		_, _ = t.w.Write([]byte(send + "\r"))
	}
}

// Write sends what was typed. Typing before the device asked for the login
// means the user logs in themselves: the Telnet auto-login stands down.
func (t *Terminal) Write(data []byte) error {
	t.mu.Lock()
	if !t.sentUser && !t.sentPassword {
		t.sentUser, t.sentPassword = true, true
	}
	t.mu.Unlock()
	_, err := t.w.Write(data)
	return err
}

// Resize tells the device the terminal's new size.
func (t *Terminal) Resize(cols, rows int) {
	if cols > 0 && rows > 0 {
		_ = t.resize(cols, rows)
	}
}

// SendBreak sends a break (console servers use it, e.g. to stop a boot).
func (t *Terminal) SendBreak() error {
	if t.brk == nil {
		return errors.New("this connection cannot send a break")
	}
	return t.brk()
}

// HostKey is the SSH key of the device (to save after the first connection).
func (t *Terminal) HostKey() string { return t.hostKey }

// Close ends the session; Closed follows with an empty reason.
func (t *Terminal) Close() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.mu.Unlock()
	t.close()
}
