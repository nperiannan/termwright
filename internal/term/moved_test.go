package term

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/session"
)

func TestMACCheck(t *testing.T) {
	addr := startSSHServer(t, "s3cret")
	host, port, _ := net.SplitHostPort(addr)
	old := macOf
	t.Cleanup(func() { macOf = old })
	var seen atomic.Value
	seen.Store("aa:bb:cc:00:00:02")
	macOf = func(netip.Addr) string { return seen.Load().(string) }

	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	var answer atomic.Value
	answer.Store("")
	var asked atomic.Int32
	em.onPrompt = func(p Prompt) {
		switch {
		case p.Title == "Another Device at This Address":
			asked.Add(1)
			a := answer.Load().(string)
			m.Prompts().Answer(p.ID, PromptReply{OK: a != "", Choice: a})
		case p.Kind == "hostkey":
			m.Prompts().Answer(p.ID, PromptReply{OK: true, Choice: "Accept & Save"})
		default:
			m.Prompts().Answer(p.ID, PromptReply{OK: true, Values: []string{"s3cret"}})
		}
	}
	s := session.Defaults()
	s.Hostname, s.Username, s.Path, s.Name = host, "admin", "Lab/sw1", "sw1"
	fmt.Sscan(port, &s.Port)
	s.AuthMethods = "password"
	s.MAC = "AA-BB-CC-00-00-01"

	connect := func() (string, error) {
		ti := m.Create(s)
		defer m.Close(ti.ID)
		if err := m.Start(ti.ID, 80, 24); err != nil {
			return "", err
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			info, _ := m.Info(ti.ID)
			if info.State == StateConnected {
				return info.State, nil
			}
			if info.State == StateError {
				return info.State, errors.New(m.lastStatus(ti.ID))
			}
			time.Sleep(20 * time.Millisecond)
		}
		return "", errors.New("timeout")
	}
	// another MAC, cancelled: no login, a clear error
	if _, err := connect(); err == nil || !strings.Contains(err.Error(), "another device") {
		t.Fatalf("want another-device error, got %v", err)
	}
	// another MAC, "Connect Anyway"
	answer.Store("Connect Anyway")
	if st, err := connect(); err != nil || st != StateConnected {
		t.Fatalf("%s %v", st, err)
	}
	// the saved MAC: no question
	seen.Store("aa:bb:cc:00:00:01")
	n := asked.Load()
	if st, err := connect(); err != nil || st != StateConnected || asked.Load() != n {
		t.Fatalf("%s %v asked %d", st, err, asked.Load()-n)
	}
}

func TestHostKeyStatus(t *testing.T) {
	addr := startSSHServer(t, "x")
	m := NewManager(nopEmitter{}, t.TempDir(), func() string { return t.TempDir() })
	key, err := FetchHostKey(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	if st := m.HostKeyStatus("10.9.9.9:22", key); st != "unknown" {
		t.Fatal(st)
	}
	if err := m.TrustHostKey("10.9.9.9:22", key); err != nil {
		t.Fatal(err)
	}
	if st := m.HostKeyStatus("10.9.9.9:22", key); st != "same" {
		t.Fatal(st)
	}
	other, _ := FetchHostKey(context.Background(), startSSHServer(t, "y"))
	if st := m.HostKeyStatus("10.9.9.9:22", other); st != "different" {
		t.Fatal(st)
	}
}
