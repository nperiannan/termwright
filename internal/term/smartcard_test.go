//go:build cgo && !windows

package term

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/smartcard"
)

// TestSmartCardLogin logs in with a key on a SoftHSM2 token (skipped when
// SoftHSM2 / OpenSC's pkcs11-tool are not installed).
func TestSmartCardLogin(t *testing.T) {
	lib := "/usr/lib/softhsm/libsofthsm2.so"
	if _, err := os.Stat(lib); err != nil {
		t.Skip("SoftHSM2 not installed")
	}
	for _, tool := range []string{"softhsm2-util", "pkcs11-tool"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not installed")
		}
	}
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "tokens"), 0o700)
	conf := filepath.Join(dir, "softhsm2.conf")
	_ = os.WriteFile(conf, []byte("directories.tokendir = "+filepath.Join(dir, "tokens")+"\n"), 0o600)
	t.Setenv("SOFTHSM2_CONF", conf)
	for _, args := range [][]string{
		{"softhsm2-util", "--init-token", "--free", "--label", "Card", "--so-pin", "87654321", "--pin", "246810"},
		{"pkcs11-tool", "--module", lib, "--token-label", "Card", "--login", "--pin", "246810", "--keypairgen", "--key-type", "EC:prime256v1", "--id", "9a", "--label", "auth"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	cards := smartcard.NewManager()
	keys := cards.List([]string{lib})[0].Keys
	if len(keys) != 1 {
		t.Fatalf("keys %+v", keys)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	_ = os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	_ = os.WriteFile(filepath.Join(home, ".ssh", "authorized_keys"), []byte(keys[0].AuthorizedKey+"\n"), 0o600)
	t.Setenv("SSH_AUTH_SOCK", "")

	addr := startSSHServer(t, "not-used")
	host, port, _ := net.SplitHostPort(addr)
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	m.SmartCards = cards
	m.SmartCardLibraries = func() []string { return []string{lib} }
	var pins atomic.Int32
	em.onPrompt = func(p Prompt) {
		switch {
		case p.Kind == "hostkey":
			m.Prompts().Answer(p.ID, PromptReply{OK: true, Choice: "Accept & Save"})
		case p.Title == "Smart Card PIN":
			pins.Add(1)
			m.Prompts().Answer(p.ID, PromptReply{OK: true, Values: []string{"246810"}})
		default:
			m.Prompts().Answer(p.ID, PromptReply{OK: false})
		}
	}
	s := session.Defaults()
	s.Hostname, s.Username = host, "admin"
	fmt.Sscan(port, &s.Port)
	s.AuthMethods = "publickey"
	s.SmartCard = keys[0].Fingerprint
	for i := 0; i < 2; i++ { // the second login uses the PIN already entered
		ti := m.Create(s)
		if err := m.Start(ti.ID, 100, 30); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool {
			return m.Write(ti.ID, []byte(fmt.Sprintf("hi%d\r", i))) == nil && strings.Contains(em.output(), fmt.Sprintf("hi%d", i))
		})
		_ = m.Close(ti.ID)
	}
	if pins.Load() != 1 {
		t.Fatalf("PIN asked %d times", pins.Load())
	}
}
