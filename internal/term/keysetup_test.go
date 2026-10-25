package term

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/sshkey"
)

func TestKeySetupPosix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // the test server runs commands locally and reads $HOME/.ssh/authorized_keys
	m, _, tab := connectedTab(t)
	defer m.CloseAll()
	keyPath := filepath.Join(t.TempDir(), "id_test")
	line, err := sshkey.Generate("ed25519", keyPath, "nat@mac 'x'", "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.TestKeyLogin(tab, keyPath, "s3cret"); err == nil {
		t.Fatal("key login worked before the key was installed")
	}
	msg, err := m.InstallPublicKey(tab, line)
	if err != nil || !strings.Contains(msg, "added") {
		t.Fatal(msg, err)
	}
	if msg, err := m.InstallPublicKey(tab, line); !strings.Contains(msg, "already") {
		t.Fatal("second install:", msg, err)
	}
	data, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if strings.Count(string(data), "ssh-ed25519") != 1 {
		t.Fatalf("authorized_keys:\n%s", data)
	}
	if st, _ := os.Stat(filepath.Join(home, ".ssh", "authorized_keys")); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if err := m.TestKeyLogin(tab, keyPath, "s3cret"); err != nil {
		t.Fatal(err)
	}
	if err := m.TestKeyLogin(tab, keyPath, "wrong"); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
}

func TestSSHKeyHelpers(t *testing.T) {
	dir := t.TempDir()
	for _, k := range []string{"ed25519", "ecdsa", "rsa"} {
		p := filepath.Join(dir, k)
		line, err := sshkey.Generate(k, p, "c", "")
		if err != nil {
			t.Fatal(k, err)
		}
		if got, err := sshkey.PublicLine(p, ""); err != nil || got != line {
			t.Fatal(k, got, err)
		}
		if _, err := sshkey.Generate(k, p, "c", ""); err == nil {
			t.Fatal("overwrite allowed")
		}
		if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
			t.Fatal("private key mode", st.Mode())
		}
	}
	if _, err := sshkey.Normalize("ssh-ed25519 AAAA; rm -rf /"); err == nil {
		t.Fatal("bad key accepted")
	}
	if c := sshkey.WindowsInstallCommand("ssh-ed25519 AAAAC3 x"); !strings.HasPrefix(c, "powershell -NoProfile -NonInteractive -EncodedCommand ") {
		t.Fatal(c)
	}
}

func TestKeyLoginWithPPK(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m, _, tab := connectedTab(t)
	defer m.CloseAll()
	key := filepath.Join("..", "sshkey", "testdata", "ed25519-v3-enc.ppk")
	line, err := sshkey.PublicLine(key, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.InstallPublicKey(tab, line); err != nil {
		t.Fatal(err)
	}
	if err := m.TestKeyLogin(tab, key, "secret pass"); err != nil {
		t.Fatal(err)
	}
}
