package term

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/termwright/termwright/internal/session"
)

// TestChangedHostKey: with "Accept changed SSH host keys" on, a host whose saved key differs connects without
// asking and the new key replaces the old; off, the warning asks.
func TestChangedHostKey(t *testing.T) {
	addr := startSSHServer(t, "s3cret")
	host, port, _ := net.SplitHostPort(addr)
	for _, accept := range []bool{true, false} {
		em := &recEmitter{}
		m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
		m.AcceptChangedKeys = func() bool { return accept }
		pub, _, _ := ed25519.GenerateKey(rand.Reader)
		old, _ := ssh.NewPublicKey(pub)
		os.WriteFile(m.knownHostsFile(), []byte(knownhosts.Line([]string{knownhosts.Normalize(addr)}, old)+"\n"), 0o600)
		asked := false
		em.onPrompt = func(p Prompt) {
			if p.Kind == "hostkey" {
				asked = true
				m.Prompts().Answer(p.ID, PromptReply{OK: true, Choice: "Accept Once"})
				return
			}
			m.Prompts().Answer(p.ID, PromptReply{OK: true, Values: []string{"s3cret"}})
		}
		s := session.Defaults()
		s.Hostname, s.Username, s.AuthMethods = host, "admin", "password"
		fmt.Sscan(port, &s.Port)
		ti := m.Create(s)
		if err := m.Start(ti.ID, 100, 30); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { return strings.Contains(em.output(), "Welcome") })
		b, _ := os.ReadFile(m.knownHostsFile())
		if accept && (asked || strings.Contains(string(b), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(old))))) {
			t.Fatalf("accept: asked %v, old key kept: %s", asked, b)
		}
		if !accept && !asked {
			t.Fatal("not asked with the option off")
		}
		m.CloseAll()
	}
}
