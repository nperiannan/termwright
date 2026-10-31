package term

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
)

type recEmitter struct {
	mu       sync.Mutex
	data     strings.Builder
	states   []string
	onPrompt func(Prompt)
	onXfer   func(Transfer)
	onScript func(map[string]string)
	onEvent  func(name string, d any)
}

func (r *recEmitter) Emit(name string, d any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.onEvent != nil {
		r.onEvent(name, d)
	}
	switch {
	case strings.HasPrefix(name, "term:data:"):
		b, _ := base64.StdEncoding.DecodeString(d.(string))
		r.data.Write(b)
	case strings.HasPrefix(name, "term:status:"):
		r.states = append(r.states, d.(map[string]string)["state"])
	case name == "prompt":
		if r.onPrompt != nil {
			go r.onPrompt(d.(Prompt))
		}
	case name == "xfer:progress":
		if r.onXfer != nil {
			r.onXfer(d.(Transfer))
		}
	case name == "script:status":
		if r.onScript != nil {
			r.onScript(d.(map[string]string))
		}
	}
}

func (r *recEmitter) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data.String()
}

// startSSHServer runs a tiny SSH server that echoes input with a prefix.
func startSSHServer(t *testing.T, password string) string {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if c.User() == "admin" && string(p) == password {
			return nil, nil
		}
		return nil, fmt.Errorf("denied")
	}, PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		// accept keys listed in $HOME/.ssh/authorized_keys (used by the key-setup test)
		data, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".ssh", "authorized_keys"))
		for len(data) > 0 {
			pub, _, _, rest, err := ssh.ParseAuthorizedKey(data)
			if err != nil {
				break
			}
			if c.User() == "admin" && bytes.Equal(pub.Marshal(), k.Marshal()) {
				return nil, nil
			}
			data = rest
		}
		return nil, fmt.Errorf("denied")
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					if ch.ChannelType() == "direct-tcpip" {
						var d struct {
							Host  string
							Port  uint32
							OHost string
							OPort uint32
						}
						ssh.Unmarshal(ch.ExtraData(), &d)
						rc, err := net.Dial("tcp", net.JoinHostPort(d.Host, fmt.Sprint(d.Port)))
						if err != nil {
							ch.Reject(ssh.ConnectionFailed, err.Error())
							continue
						}
						c, creqs, _ := ch.Accept()
						go ssh.DiscardRequests(creqs)
						go func() { io.Copy(c, rc); c.Close() }()
						go func() { io.Copy(rc, c); rc.Close() }()
						continue
					}
					c, creqs, _ := ch.Accept()
					go func() {
						for r := range creqs {
							switch r.Type {
							case "subsystem":
								r.Reply(true, nil)
								go func() {
									srv, _ := sftp.NewServer(c)
									srv.Serve()
									c.Close()
								}()
							case "shell":
								r.Reply(true, nil)
								go shellEcho(c)
							case "exec":
								var cmdline struct{ Cmd string }
								ssh.Unmarshal(r.Payload, &cmdline)
								r.Reply(true, nil)
								go func() {
									cmd := exec.Command("sh", "-c", cmdline.Cmd)
									cmd.Stdout = c
									cmd.Stderr = c.Stderr()
									// Like sshd: the exit of the command ends the channel even
									// if the client has not closed its stdin.
									in, _ := cmd.StdinPipe()
									go func() { io.Copy(in, c); in.Close() }()
									code := 0
									if err := cmd.Run(); err != nil {
										code = 1
									}
									c.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{uint32(code)}))
									c.Close()
								}()
							default:
								r.Reply(r.Type == "pty-req" || r.Type == "window-change", nil)
							}
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func shellEcho(c ssh.Channel) {
	io.WriteString(c, "Welcome\r\nrouter> ")
	buf := make([]byte, 1024)
	for {
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		io.WriteString(c, "echo:"+string(buf[:n]))
		if strings.Contains(string(buf[:n]), "exit") {
			c.Close()
			return
		}
	}
}

func TestSSHEndToEnd(t *testing.T) {
	addr := startSSHServer(t, "s3cret")
	host, port, _ := net.SplitHostPort(addr)
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	em.onPrompt = func(p Prompt) {
		switch p.Kind {
		case "hostkey":
			m.Prompts().Answer(p.ID, PromptReply{OK: true, Choice: "Accept & Save"})
		case "credentials":
			m.Prompts().Answer(p.ID, PromptReply{OK: true, Values: []string{"s3cret"}})
		}
	}
	s := session.Defaults()
	s.Hostname, s.Username = host, "admin"
	fmt.Sscan(port, &s.Port)
	s.AuthMethods = "password"
	s.LogonActions = []session.LogonAction{{Expect: "router>", Send: `show ver\r`}}
	ti := m.Create(s)
	if err := m.Start(ti.ID, 100, 30); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return strings.Contains(em.output(), "echo:show ver\r") })
	if err := m.Write(ti.ID, []byte("exit\r")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		em.mu.Lock()
		defer em.mu.Unlock()
		return len(em.states) > 0 && em.states[len(em.states)-1] == StateDisconnected
	})
	// host key was saved: reconnecting must not prompt for the host key again
	prompted := false
	em.onPrompt = func(p Prompt) {
		if p.Kind == "hostkey" {
			prompted = true
		}
		m.Prompts().Answer(p.ID, PromptReply{OK: true, Values: []string{"s3cret"}})
	}
	m.Start(ti.ID, 100, 30)
	waitFor(t, func() bool { return strings.Count(em.output(), "Welcome") == 2 })
	if prompted {
		t.Fatal("host key prompted twice")
	}
	m.CloseAll()
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout waiting for condition")
}

func TestTelnetNegotiation(t *testing.T) {
	a, b := net.Pipe()
	tc := &telnetConn{c: a, term: "xterm", cols: 80, rows: 24, raw: make([]byte, 1024), telnet: true}
	go func() {
		b.Write([]byte{tIAC, tDO, optNAWS, 'h', 'i', tIAC, tIAC})
	}()
	replies := make(chan []byte, 4)
	go func() {
		buf := make([]byte, 64)
		n, _ := b.Read(buf)
		replies <- append([]byte(nil), buf[:n]...)
		n, _ = b.Read(buf)
		replies <- append([]byte(nil), buf[:n]...)
	}()
	buf := make([]byte, 16)
	n, _ := tc.Read(buf)
	got := string(buf[:n])
	for len(got) < 3 {
		n, _ = tc.Read(buf)
		got += string(buf[:n])
	}
	if got != "hi\xff" {
		t.Fatalf("%q", got)
	}
	if r := <-replies; string(r) != string([]byte{tIAC, tWILL, optNAWS}) {
		t.Fatalf("reply %v", r)
	}
	if r := <-replies; string(r) != string([]byte{tIAC, tSB, optNAWS, 0, 80, 0, 24, tIAC, tSE}) {
		t.Fatalf("naws %v", r)
	}
}

func TestUnescape(t *testing.T) {
	if got := Unescape(`a\r\n\t\x03\\z`); got != "a\r\n\t\x03\\z" {
		t.Fatalf("%q", got)
	}
}

// The client opens the negotiation (character mode) and does not answer its own
// requests again when the server agrees, so the two sides cannot loop.
func TestTelnetActiveNegotiation(t *testing.T) {
	a, b := net.Pipe()
	tc := &telnetConn{c: a, term: "xterm", cols: 80, rows: 24, raw: make([]byte, 1024), telnet: true}
	got := make(chan []byte, 8)
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := b.Read(buf)
			if err != nil {
				close(got)
				return
			}
			got <- append([]byte(nil), buf[:n]...)
		}
	}()
	go tc.start()
	first := <-got
	want := []byte{tIAC, tWILL, optNAWS, tIAC, tWILL, optTType, tIAC, tDO, optEcho, tIAC, tWILL, optSGA, tIAC, tDO, optSGA}
	if string(first) != string(want) {
		t.Fatalf("start sent %v", first)
	}
	// the server agrees to SGA both ways and to echo: nothing is answered
	tc.negotiate(tDO, optSGA)
	tc.negotiate(tWILL, optSGA)
	tc.negotiate(tWILL, optEcho)
	// it refuses an option the client did not ask for, and asks for one it does not support
	tc.negotiate(tDO, 39)
	if r := <-got; string(r) != string([]byte{tIAC, tWONT, 39}) {
		t.Fatalf("expected only WONT 39, got %v", r)
	}
	a.Close()
}
