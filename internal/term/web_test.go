package term

import (
	"io"
	"net"
	"testing"
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
