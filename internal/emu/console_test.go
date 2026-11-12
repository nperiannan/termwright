package emu

import (
	"net"
	"testing"
	"time"
)

// A one-key boot menu answer goes without Enter (an Enter would stay in the buffer for the next prompt).
func TestConsoleKey(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewConsole(a)
	defer c.Close()
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 16)
		_ = b.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _ := b.Read(buf)
		got <- string(buf[:n])
	}()
	if err := c.Key("c"); err != nil {
		t.Fatal(err)
	}
	if s := <-got; s != "c" {
		t.Fatalf("sent %q", s)
	}
}
