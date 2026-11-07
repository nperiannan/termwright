package term

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/session"
)

func TestMoshCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX quoting")
	}
	found := func(p string) (string, error) {
		if p == "/opt/homebrew/bin/mosh" {
			return p, nil
		}
		return "", errors.New("not found")
	}
	cmd, err := moshCommand(session.Session{Hostname: "10.0.0.5", Port: 2222, Username: "admin", IdentityFile: "/k/id ed"}, found)
	if err != nil || cmd != `/opt/homebrew/bin/mosh --ssh='ssh -p 2222 -i '\''/k/id ed'\''' admin@10.0.0.5` {
		t.Fatalf("cmd %q %v", cmd, err)
	}
	if cmd, _ := moshCommand(session.Session{Hostname: "h"}, found); cmd != "/opt/homebrew/bin/mosh h" {
		t.Fatalf("plain %q", cmd)
	}
	none := func(string) (string, error) { return "", errors.New("no") }
	if _, err := moshCommand(session.Session{Hostname: "h"}, none); err == nil || !strings.Contains(err.Error(), "brew install mosh") {
		t.Fatalf("missing mosh: %v", err)
	}
}

func TestTelnetOverTLS(t *testing.T) {
	ts := httptest.NewUnstartedServer(nil) // only for its self-signed certificate
	cert := ts.TLS
	_ = cert
	ts.StartTLS()
	pair := ts.TLS.Certificates[0]
	ts.Close()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = c.Write([]byte("Login: ")); time.Sleep(time.Second); c.Close() }()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	dial := func(ctx context.Context, addr string) (net.Conn, string, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		return c, "", err
	}
	s := session.Session{Hostname: host, TelnetTLS: true}
	_, _ = port, s
	s.Port = atoiPort(port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := dialTelnet(ctx, s, 80, 24, dial); err == nil || !strings.Contains(err.Error(), "Accept any certificate") {
		t.Fatalf("an untrusted certificate was accepted: %v", err)
	}
	s.TLSAnyCert = true
	c, err := dialTelnet(ctx, s, 80, 24, dial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !strings.HasPrefix(c.Info(), "Telnet over TLS ") {
		t.Fatalf("info %q", c.Info())
	}
	buf := make([]byte, 64)
	n, _ := c.Read(buf)
	if !strings.Contains(string(buf[:n]), "Login") {
		t.Fatalf("read %q", buf[:n])
	}
}

func atoiPort(p string) int {
	n := 0
	for _, c := range p {
		n = n*10 + int(c-'0')
	}
	return n
}
