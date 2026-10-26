package main

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/term"
)

func TestRDPFile(t *testing.T) {
	s := session.Session{Name: "dc01", Username: "admin", RDPDomain: "CORP", RDPSize: "1600x900"}
	f := rdpFile("127.0.0.1:50123", s)
	for _, want := range []string{"full address:s:127.0.0.1:50123\r\n", `username:s:CORP\admin`, "screen mode id:i:1", "desktopwidth:i:1600", "desktopheight:i:900"} {
		if !strings.Contains(f, want) {
			t.Errorf("missing %q in\n%s", want, f)
		}
	}
	s.RDPFullScreen, s.RDPSize = true, "bad"
	if f := rdpFile("h:3389", s); !strings.Contains(f, "screen mode id:i:2") || strings.Contains(f, "desktopwidth") {
		t.Errorf("full screen:\n%s", f)
	}
	a := strings.Join(freerdpArgs("h:3389", session.Session{Name: "x", Username: "u", RDPDomain: "D", RDPSize: "1280x720"}, true), " ")
	if !strings.Contains(a, "/v:h:3389") || !strings.Contains(a, "/u:u") || !strings.Contains(a, "/d:D") || !strings.Contains(a, "/size:1280x720") || !strings.Contains(a, "/from-stdin") {
		t.Errorf("freerdp args: %s", a)
	}
}

// The VNC WebSocket carries bytes both ways to the device.
func TestVNCWebSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = c.Write([]byte("RFB 003.008\n"))
		buf := make([]byte, 32)
		n, _ := c.Read(buf)
		_, _ = c.Write(append([]byte("got:"), buf[:n]...))
		time.Sleep(200 * time.Millisecond)
		c.Close()
	}()
	m := term.NewManager(nopEmit{}, t.TempDir(), func() string { return t.TempDir() })
	tu, err := m.OpenTunnel(context.Background(), session.Session{Protocol: "VNC"}, "x", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := startWS(); err != nil {
		t.Fatal(err)
	}
	remoteMu.Lock()
	remotes["t1"] = &remoteConn{info: RemoteInfo{ID: "t1", Kind: "vnc"}, tunnel: tu, token: "tok"}
	remoteMu.Unlock()
	defer func() { remoteMu.Lock(); delete(remotes, "t1"); remoteMu.Unlock() }()
	ws, _, err := websocket.DefaultDialer.Dial("ws://"+wsAddr+"/vnc/tok", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_, msg, err := ws.ReadMessage()
	if err != nil || string(msg) != "RFB 003.008\n" {
		t.Fatalf("greeting %q %v", msg, err)
	}
	_ = ws.WriteMessage(websocket.BinaryMessage, []byte("hello"))
	_, msg, err = ws.ReadMessage()
	if err != nil || string(msg) != "got:hello" {
		t.Fatalf("echo %q %v", msg, err)
	}
	if _, _, err := websocket.DefaultDialer.Dial("ws://"+wsAddr+"/vnc/wrong", nil); err == nil {
		t.Fatal("unknown token accepted")
	}
}

// A tunnel's local port forwards to the address.
func TestTunnelListen(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_, _ = c.Write([]byte("ok"))
			c.Close()
		}
	}()
	m := term.NewManager(nopEmit{}, t.TempDir(), func() string { return t.TempDir() })
	tu, _ := m.OpenTunnel(context.Background(), session.Session{}, "x", ln.Addr().String())
	defer tu.Close()
	addr, err := tu.Listen()
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Read(buf); err != nil || string(buf) != "ok" {
		t.Fatalf("%q %v", buf, err)
	}
}

type nopEmit struct{}

func (nopEmit) Emit(string, any) {}
