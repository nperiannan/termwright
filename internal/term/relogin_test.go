package term

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/session"
)

// A console that restarts while connected: the login prompt comes back by
// itself and "Log in again" logs in a second time; without it, it does not.
func TestLoginAgainAfterRestart(t *testing.T) {
	reloginTyped, reloginGap = 50*time.Millisecond, 100*time.Millisecond
	defer func() { reloginTyped, reloginGap = 10*time.Second, 30*time.Second }()
	for _, again := range []bool{true, false} {
		srv, _ := net.Listen("tcp", "127.0.0.1:0")
		logins := make(chan string, 4)
		go func() {
			c, err := srv.Accept()
			if err != nil {
				return
			}
			r := bufio.NewReader(c)
			login := func(prompt string) {
				io.WriteString(c, prompt)
				u, _ := r.ReadString('\r')
				io.WriteString(c, "\r\npassword: ")
				p, _ := r.ReadString('\r')
				logins <- fmt.Sprint(strings.Contains(u, "admin"), strings.Contains(p, "pw"))
				io.WriteString(c, "\r\nsw # ")
			}
			login("Connected\r\n\r\nlogin: ")
			time.Sleep(300 * time.Millisecond)
			login("\r\nRebooting...\r\nBooting the switch\r\n\r\nlogin: ")
		}()
		em := &recEmitter{}
		m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
		m.SavedPassword = func(string) (string, bool) { return "pw", true }
		h, p, _ := net.SplitHostPort(srv.Addr().String())
		s := session.Defaults()
		s.Path, s.Protocol, s.Hostname, s.Username, s.LoginAgain = "Lab/sw", session.ProtoTelnet, h, "admin", again
		fmt.Sscan(p, &s.Port)
		ti := m.Create(s)
		m.Start(ti.ID, 80, 24)
		if got := <-logins; got != "true true" {
			t.Fatalf("first login sent %s", got)
		}
		select {
		case got := <-logins:
			if !again || got != "true true" {
				t.Fatalf("again=%v: second login %s", again, got)
			}
		case <-time.After(1500 * time.Millisecond):
			if again {
				t.Fatalf("no login after the restart; output %q", em.output())
			}
		}
		m.CloseAll()
		srv.Close()
	}
}

// "exit" typed by the user: the login prompt that follows is left alone.
func TestLoginAgainNotAfterExit(t *testing.T) {
	reloginTyped, reloginGap = 2*time.Second, 50*time.Millisecond
	defer func() { reloginTyped, reloginGap = 10*time.Second, 30*time.Second }()
	srv, _ := net.Listen("tcp", "127.0.0.1:0")
	defer srv.Close()
	logins := make(chan bool, 4)
	go func() {
		c, err := srv.Accept()
		if err != nil {
			return
		}
		r := bufio.NewReader(c)
		io.WriteString(c, "login: ")
		r.ReadString('\r')
		io.WriteString(c, "\r\npassword: ")
		r.ReadString('\r')
		logins <- true
		io.WriteString(c, "\r\nsw # ")
		r.ReadString('\r') // exit
		io.WriteString(c, "\r\nlogin: ")
		if u, _ := r.ReadString('\r'); strings.Contains(u, "admin") {
			logins <- true
		}
	}()
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	m.SavedPassword = func(string) (string, bool) { return "pw", true }
	h, p, _ := net.SplitHostPort(srv.Addr().String())
	s := session.Defaults()
	s.Path, s.Protocol, s.Hostname, s.Username, s.LoginAgain = "Lab/sw", session.ProtoTelnet, h, "admin", true
	fmt.Sscan(p, &s.Port)
	ti := m.Create(s)
	m.Start(ti.ID, 80, 24)
	<-logins
	waitFor(t, func() bool { return strings.Contains(em.output(), "sw # ") })
	time.Sleep(100 * time.Millisecond)
	m.Write(ti.ID, []byte("exit\r"))
	select {
	case <-logins:
		t.Fatal("logged in again after exit")
	case <-time.After(800 * time.Millisecond):
	}
	m.CloseAll()
}
