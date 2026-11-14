package term

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/session"
)

// The proxy takes a SOCKS5 CONNECT with a host name and dials it with its
// dialer (the SSH server resolves the name), then copies both ways.
func TestWebProxySocksByName(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); c.Close() }()
		}
	}()
	var asked string
	wp := &WebProxy{conns: map[net.Conn]bool{}, dial: func(a string) (net.Conn, error) {
		asked = a
		return net.Dial("tcp", echo.Addr().String())
	}}
	if err := wp.listen(); err != nil {
		t.Fatal(err)
	}
	defer wp.Close()
	c, err := net.Dial("tcp", wp.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	name := "nas.lan"
	req := append([]byte{5, 1, 0, 5, 1, 0, 3, byte(len(name))}, name...)
	req = append(req, 0, 80)
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 12)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != 5 || reply[1] != 0 || reply[3] != 0 {
		t.Fatalf("handshake reply %v", reply)
	}
	if asked != "nas.lan:80" {
		t.Fatalf("dialed %q", asked)
	}
	_, _ = c.Write([]byte("ping"))
	b := make([]byte, 4)
	if _, err := io.ReadFull(c, b); err != nil || string(b) != "ping" {
		t.Fatalf("echo %q %v", b, err)
	}
	wp.Close()
	if _, err := net.Dial("tcp", wp.Addr); err == nil {
		t.Fatal("still listening after Close")
	}
}

// A device reached without a jump host or proxy: its web interface opens through a proxy of Termwright's own that
// connects straight from this computer (as its SSH session does), not through the system's web proxy.
func TestOpenWebProxyDirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "device web page") }))
	defer srv.Close()
	m := NewManager(nopEmitter{}, "", func() string { return "" })
	wp, err := m.OpenWebProxy(context.Background(), "", session.Session{Name: "sw1", Protocol: session.ProtoSSH2, Hostname: "127.0.0.1"}, "", true)
	if err != nil || wp == nil {
		t.Fatalf("proxy %v, %v", wp, err)
	}
	defer wp.Close()
	if !wp.Direct || wp.Via != "" {
		t.Fatalf("direct %v via %q", wp.Direct, wp.Via)
	}
	// a SOCKS5 CONNECT to the web server, then a plain HTTP request
	c, err := net.Dial("tcp", wp.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	_, _ = c.Write([]byte{5, 1, 0})
	r := make([]byte, 2)
	if _, err := io.ReadFull(c, r); err != nil || r[1] != 0 {
		t.Fatalf("greeting %v %v", r, err)
	}
	req := append([]byte{5, 1, 0, 1}, net.ParseIP(host).To4()...)
	p := make([]byte, 2)
	var n int
	for _, ch := range port {
		n = n*10 + int(ch-'0')
	}
	binary.BigEndian.PutUint16(p, uint16(n))
	_, _ = c.Write(append(req, p...))
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil || rep[1] != 0 {
		t.Fatalf("connect %v %v", rep, err)
	}
	_, _ = io.WriteString(c, "GET / HTTP/1.0\r\nHost: x\r\n\r\n")
	b, _ := io.ReadAll(c)
	if !strings.Contains(string(b), "device web page") {
		t.Fatalf("got %q", b)
	}
	if wp.Used() == 0 {
		t.Fatal("the connection is not counted")
	}
}

// Probe: a port that takes the connection, and one that refuses it.
func TestWebProxyProbe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
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
			_ = c.Close()
		}
	}()
	m := NewManager(nopEmitter{}, "", func() string { return "" })
	wp, err := m.OpenWebProxy(context.Background(), "", session.Session{Name: "sw1", Protocol: session.ProtoSSH2, Hostname: "127.0.0.1"}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer wp.Close()
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	gone := closed.Addr().String()
	_ = closed.Close()
	if !wp.Probe(ln.Addr().String(), 3*time.Second) || wp.Probe(gone, 3*time.Second) {
		t.Fatal("probe")
	}
}
