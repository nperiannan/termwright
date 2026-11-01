package twcore

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type testScreen struct {
	mu     sync.Mutex
	out    strings.Builder
	closed chan string
}

func newScreen() *testScreen { return &testScreen{closed: make(chan string, 1)} }

func (s *testScreen) Output(b []byte) { s.mu.Lock(); s.out.Write(b); s.mu.Unlock() }
func (s *testScreen) Closed(r string) { s.closed <- r }
func (s *testScreen) text() string    { s.mu.Lock(); defer s.mu.Unlock(); return s.out.String() }

func waitFor(t *testing.T, s *testScreen, want string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if strings.Contains(s.text(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %q in %q", want, s.text())
}

// An SSH shell: the device's key is returned the first time, then the session
// opens with a terminal, what is typed reaches the device and its output the screen.
func TestOpenSSH(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if c.User() == "admin" && string(p) == "pw" {
			return nil, nil
		}
		return nil, os.ErrPermission
	}}
	cfg.AddHostKey(signer)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	sizes := make(chan string, 4)
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
				for nch := range chans {
					ch, rq, _ := nch.Accept()
					go func() {
						for r := range rq {
							switch r.Type {
							case "pty-req", "window-change":
								sizes <- r.Type
								r.Reply(true, nil)
							case "shell":
								r.Reply(true, nil)
								ch.Write([]byte("switch # "))
								go func() { // echo, upper case, until "exit"
									b := make([]byte, 64)
									for {
										n, err := ch.Read(b)
										if err != nil || strings.Contains(string(b[:n]), "exit") {
											ch.Close()
											return
										}
										ch.Write([]byte(strings.ToUpper(string(b[:n]))))
									}
								}()
							default:
								r.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	req := termRequest{Protocol: "SSH2", Host: host, Port: p, User: "admin", Password: "pw", Cols: 100, Rows: 30}
	b, _ := json.Marshal(req)
	if _, err := Open(string(b), newScreen()); err == nil || !strings.HasPrefix(err.Error(), HostKeyChanged+"SHA256:") {
		t.Fatalf("first connection: %v", err)
	}
	req.HostKey = fingerprint(signer.PublicKey())
	b, _ = json.Marshal(req)
	scr := newScreen()
	term, err := Open(string(b), scr)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, scr, "switch # ")
	if term.HostKey() != req.HostKey {
		t.Fatal(term.HostKey())
	}
	_ = term.Write([]byte("show vlan"))
	waitFor(t, scr, "SHOW VLAN")
	term.Resize(120, 40)
	if <-sizes != "pty-req" || <-sizes != "window-change" {
		t.Fatal("sizes")
	}
	_ = term.Write([]byte("exit"))
	select {
	case r := <-scr.closed:
		if r != "" {
			t.Fatalf("closed: %q", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("not closed")
	}
	// a wrong password is an authentication error (the app asks for the login)
	req.Password = "nope"
	b, _ = json.Marshal(req)
	if _, err := Open(string(b), newScreen()); err == nil || !strings.Contains(err.Error(), "unable to authenticate") {
		t.Fatalf("wrong password: %v", err)
	}
}

// Telnet: the desktop's negotiation, and the saved login answers the prompts.
func TestOpenTelnetLogsIn(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	got := make(chan string, 4)
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		defer nc.Close()
		read := func() string { // the next line typed, without telnet commands
			var line []byte
			b := make([]byte, 1)
			for {
				if _, err := nc.Read(b); err != nil {
					return string(line)
				}
				switch {
				case b[0] == 255: // IAC cmd opt
					c := make([]byte, 2)
					_, _ = nc.Read(c)
				case b[0] == '\r':
					return string(line)
				case b[0] >= ' ':
					line = append(line, b[0])
				}
			}
		}
		nc.Write([]byte("\r\nExtremeXOS\r\nlogin: "))
		got <- read()
		nc.Write([]byte("\r\npassword: "))
		got <- read()
		nc.Write([]byte("\r\nswitch # "))
		time.Sleep(200 * time.Millisecond)
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	b, _ := json.Marshal(termRequest{Protocol: "Telnet", Host: host, Port: p, User: "admin", Password: "pw"})
	scr := newScreen()
	term, err := Open(string(b), scr)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if u, pw := <-got, <-got; u != "admin" || pw != "pw" {
		t.Fatalf("login %q / %q", u, pw)
	}
	waitFor(t, scr, "switch # ")
}
