package term

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/session"
)

func autoAnswer(m *Manager, em *recEmitter, pw string) {
	em.onPrompt = func(p Prompt) {
		m.Prompts().Answer(p.ID, PromptReply{OK: true, Choice: "Accept Once", Values: []string{pw}})
	}
}

func sshSession(addr string) session.Session {
	host, port, _ := net.SplitHostPort(addr)
	s := session.Defaults()
	s.Hostname, s.Username, s.AuthMethods = host, "admin", "password"
	fmt.Sscan(port, &s.Port)
	return s
}

// echoServer is a plain TCP service used as a forwarding target.
func echoServer(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.WriteString(c, "hello-target\n"); c.Close() }()
		}
	}()
	return ln.Addr().String()
}

func freePort(t *testing.T) int {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func readLine(t *testing.T, c net.Conn) string {
	l, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(l)
}

func TestPortForwardsAndJumpHost(t *testing.T) {
	target := echoServer(t)
	th, tp, _ := net.SplitHostPort(target)
	var tport int
	fmt.Sscan(tp, &tport)
	jumpAddr := startSSHServer(t, "pw")
	finalAddr := startSSHServer(t, "pw")

	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	autoAnswer(m, em, "pw")
	jump := sshSession(jumpAddr)
	jump.Path, jump.Name = "jumpbox", "jumpbox"
	m.LoadSession = func(p string) (session.Session, error) {
		if p == "jumpbox" {
			return jump, nil
		}
		return session.Session{}, fmt.Errorf("no session %s", p)
	}
	lport, dport := freePort(t), freePort(t)
	s := sshSession(finalAddr)
	s.JumpSession = "jumpbox"
	s.PortForwards = []session.PortForward{
		{Type: "local", BindPort: lport, DestHost: th, DestPort: tport},
		{Type: "dynamic", BindPort: dport},
	}
	ti := m.Create(s)
	m.Start(ti.ID, 80, 24)
	waitFor(t, func() bool { return strings.Contains(em.output(), "router>") })
	info, _ := m.Info(ti.ID)
	if !strings.Contains(info.Info, "via jumpbox") {
		t.Fatalf("info %q", info.Info)
	}

	// local forward
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", lport))
	if err != nil {
		t.Fatal(err)
	}
	if got := readLine(t, c); got != "hello-target" {
		t.Fatalf("local forward got %q", got)
	}
	c.Close()

	// dynamic SOCKS5 forward
	c, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", dport))
	if err != nil {
		t.Fatal(err)
	}
	c.Write([]byte{5, 1, 0})
	buf := make([]byte, 2)
	io.ReadFull(c, buf)
	req := []byte{5, 1, 0, 3, byte(len(th))}
	req = append(req, th...)
	req = append(req, byte(tport>>8), byte(tport))
	c.Write(req)
	rep := make([]byte, 10)
	io.ReadFull(c, rep)
	if rep[1] != 0 {
		t.Fatalf("socks reply %v", rep)
	}
	if got := readLine(t, c); got != "hello-target" {
		t.Fatalf("socks got %q", got)
	}
	c.Close()
	m.CloseAll()
	if _, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", lport)); err == nil {
		t.Fatal("listener still open after close")
	}
}

func TestHTTPProxyTelnetAndTriggers(t *testing.T) {
	// fake telnet-ish server that prompts and echoes
	srv, _ := net.Listen("tcp", "127.0.0.1:0")
	defer srv.Close()
	go func() {
		c, err := srv.Accept()
		if err != nil {
			return
		}
		io.WriteString(c, "Username: ")
		r := bufio.NewReader(c)
		l, _ := r.ReadString('\r')
		io.WriteString(c, "\r\ngot["+strings.TrimSpace(l)+"]\r\n--More--")
		l, _ = r.ReadString(' ')
		io.WriteString(c, "\r\nend "+fmt.Sprint(len(l))+"\r\n")
	}()
	// HTTP CONNECT proxy
	proxyLn, _ := net.Listen("tcp", "127.0.0.1:0")
	defer proxyLn.Close()
	go func() {
		c, err := proxyLn.Accept()
		if err != nil {
			return
		}
		req, err := http.ReadRequest(bufio.NewReader(c))
		if err != nil || req.Method != "CONNECT" {
			c.Close()
			return
		}
		rc, _ := net.Dial("tcp", req.Host)
		io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
		pipe(c, rc)
	}()
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	h, p, _ := net.SplitHostPort(srv.Addr().String())
	ph, pp, _ := net.SplitHostPort(proxyLn.Addr().String())
	s := session.Defaults()
	s.Protocol, s.Hostname = session.ProtoTelnet, h
	fmt.Sscan(p, &s.Port)
	s.ProxyType, s.ProxyHost = "http", ph
	fmt.Sscan(pp, &s.ProxyPort)
	s.Triggers = []session.Trigger{{Pattern: "Username:", Send: `admin\r`}, {Pattern: `-+More-+`, Regex: true, Send: " "}}
	ti := m.Create(s)
	m.Start(ti.ID, 80, 24)
	waitFor(t, func() bool { return strings.Contains(em.output(), "end ") })
	if !strings.Contains(em.output(), "got[admin]") {
		t.Fatalf("output %q", em.output())
	}
	m.CloseAll()
}

func TestAutoReconnect(t *testing.T) {
	addr := startSSHServer(t, "pw")
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	autoAnswer(m, em, "pw")
	s := sshSession(addr)
	s.AutoReconnect = true
	ti := m.Create(s)
	m.Start(ti.ID, 80, 24)
	waitFor(t, func() bool { return strings.Contains(em.output(), "router>") })
	m.Write(ti.ID, []byte("exit\r")) // server closes the channel
	waitFor(t, func() bool { return strings.Count(em.output(), "Welcome") == 2 })
	// a user disconnect must not reconnect
	m.Disconnect(ti.ID)
	m.CloseAll()
}
