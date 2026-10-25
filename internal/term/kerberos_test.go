package term

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/session"
)

func TestSPNAndPrincipal(t *testing.T) {
	if spnFromTarget("host@server.example.com") != "host/server.example.com" || spnFromTarget("srv1") != "host/srv1" {
		t.Fatal("spn")
	}
	if u, r := splitPrincipal("alice@corp.example.com"); u != "alice" || r != "CORP.EXAMPLE.COM" {
		t.Fatal(u, r)
	}
	t.Setenv("KRB5CCNAME", "FILE:/tmp/x")
	if defaultCCachePath() != "/tmp/x" {
		t.Fatal(defaultCCachePath())
	}
	t.Setenv("KRB5CCNAME", "API:123")
	if defaultCCachePath() != "" {
		t.Fatal("API cache is not a file")
	}
}

// Integration test against a real MIT KDC and OpenSSH server (set up by hand):
// TW_KRB_TEST=host:port, user tw@TEST.LOCAL with password twpass.
func TestKerberosLogin(t *testing.T) {
	addr := os.Getenv("TW_KRB_TEST")
	if addr == "" {
		t.Skip("set TW_KRB_TEST=127.0.0.1:2299 with a KDC and a GSSAPI sshd")
	}
	run := func(t *testing.T, s session.Session) {
		em := &recEmitter{}
		m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
		asked := 0
		em.onPrompt = func(p Prompt) {
			if p.Kind == "credentials" {
				asked++
			}
			m.Prompts().Answer(p.ID, PromptReply{OK: true, Choice: "Accept Once", Values: []string{"twpass"}})
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cl, _, cleanup, err := m.sshClient(ctx, &Tab{id: "k", sess: s}, s, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		defer cl.Close()
		sess, _ := cl.NewSession()
		out, _ := sess.Output("id -un")
		if strings.TrimSpace(string(out)) != "tw" {
			t.Fatalf("id: %q", out)
		}
		t.Logf("password prompts: %d", asked)
	}
	s := session.Defaults()
	s.Hostname, s.Username, s.AuthMethods = "localhost", "tw", "gssapi"
	s.Port = 2299
	t.Run("password login", func(t *testing.T) {
		t.Setenv("KRB5CCNAME", "FILE:/nonexistent/cc")
		kerberosClients.Clear()
		run(t, s)
	})
	t.Run("kinit cache", func(t *testing.T) {
		cc := t.TempDir() + "/cc"
		t.Setenv("KRB5CCNAME", "FILE:"+cc)
		c := exec.Command("kinit", "tw@TEST.LOCAL")
		c.Stdin = strings.NewReader("twpass\n")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("kinit: %v %s", err, out)
		}
		kerberosClients.Clear()
		s2 := s
		s2.KerberosPrincipal = "tw@TEST.LOCAL"
		run(t, s2)
	})
}
