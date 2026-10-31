package term

import (
	"testing"

	"github.com/termwright/termwright/internal/session"
)

func TestAccountKey(t *testing.T) {
	m := &Manager{}
	m.CredentialKey = func(name string) string {
		if name == "lab-key" {
			return "~/.ssh/lab_ed25519"
		}
		return ""
	}
	key := session.Session{Hostname: "h", Account: "lab-key"}
	if got := m.accountKey(key); got != "~/.ssh/lab_ed25519" {
		t.Errorf("key account: %q", got)
	}
	if !m.CanLogInUnattended(key) {
		t.Error("a key account logs in without asking")
	}
	own := key
	own.IdentityFile = "~/.ssh/own"
	if got := m.accountKey(own); got != "" {
		t.Errorf("the session's own key wins, got %q", got)
	}
	if got := m.accountKey(session.Session{Hostname: "h", Account: "lab-pw"}); got != "" {
		t.Errorf("password account: %q", got)
	}
	if got := m.accountKey(session.Session{Hostname: "h", Account: "-"}); got != "" {
		t.Errorf("own login: %q", got)
	}
}
