package term

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// Through a session: why a page does not answer is known before the browser opens, and a name the far end
// cannot resolve (a switch without a DNS client) is resolved here.
func TestWebProxyReach(t *testing.T) {
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
			c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	var tried []string
	wp := &WebProxy{conns: map[net.Conn]bool{}, dial: func(a string) (net.Conn, error) {
		tried = append(tried, a)
		if strings.HasPrefix(a, "localhost:") { // the far end knows no names
			return nil, errors.New("ssh: rejected: connect failed (unknown host)")
		}
		return net.Dial("tcp", a)
	}}
	if err := wp.Reach("localhost:"+port, 5*time.Second); err != nil {
		t.Fatalf("resolved here: %v (tried %v)", err, tried)
	}
	if len(tried) < 2 || !strings.HasSuffix(tried[1], ":"+port) || strings.HasPrefix(tried[1], "localhost") {
		t.Fatalf("tried %v", tried)
	}
	wp.dial = func(string) (net.Conn, error) {
		return nil, errors.New("ssh: rejected: administratively prohibited (open failed)")
	}
	if err := wp.Reach("example.com:443", 5*time.Second); err == nil || !strings.Contains(err.Error(), "prohibited") {
		t.Fatalf("refused: %v", err)
	}
	wp.dial = func(string) (net.Conn, error) { time.Sleep(time.Second); return nil, errors.New("late") }
	if err := wp.Reach("example.com:443", 100*time.Millisecond); err == nil || !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("slow: %v", err)
	}
}
