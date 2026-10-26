//go:build cgo

package smartcard

import (
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// The tests use SoftHSM2 (a software token) and OpenSC's pkcs11-tool; they
// are skipped when these are not installed.
func softHSM(t *testing.T) string {
	t.Helper()
	var lib string
	for _, p := range []string{"/usr/lib/softhsm/libsofthsm2.so", "/usr/lib/x86_64-linux-gnu/softhsm/libsofthsm2.so",
		"/opt/homebrew/lib/softhsm/libsofthsm2.so", "/usr/local/lib/softhsm/libsofthsm2.so"} {
		if _, err := os.Stat(p); err == nil {
			lib = p
			break
		}
	}
	if lib == "" {
		t.Skip("SoftHSM2 not installed")
	}
	for _, tool := range []string{"softhsm2-util", "pkcs11-tool"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not installed")
		}
	}
	dir := t.TempDir()
	conf := filepath.Join(dir, "softhsm2.conf")
	if err := os.MkdirAll(filepath.Join(dir, "tokens"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, []byte("directories.tokendir = "+filepath.Join(dir, "tokens")+"\nobjectstore.backend = file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOFTHSM2_CONF", conf)
	run := func(name string, args ...string) {
		if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	run("softhsm2-util", "--init-token", "--free", "--label", "PIV Card", "--so-pin", "87654321", "--pin", "123456")
	run("pkcs11-tool", "--module", lib, "--token-label", "PIV Card", "--login", "--pin", "123456", "--keypairgen", "--key-type", "EC:prime256v1", "--id", "01", "--label", "PIV AUTH key")
	run("pkcs11-tool", "--module", lib, "--token-label", "PIV Card", "--login", "--pin", "123456", "--keypairgen", "--key-type", "rsa:2048", "--id", "02", "--label", "RSA key")
	return lib
}

func TestSoftHSM(t *testing.T) {
	lib := softHSM(t)
	m := NewManager()
	reps := m.List([]string{lib})
	if len(reps) != 1 || reps[0].Error != "" || len(reps[0].Keys) != 2 || reps[0].Tokens[0] != "PIV Card" {
		t.Fatalf("%+v", reps)
	}
	ec, rsaKey := reps[0].Keys[0], reps[0].Keys[1]
	if ec.Type != "ecdsa-sha2-nistp256" || ec.Label != "PIV AUTH key" || rsaKey.Type != "ssh-rsa" || rsaKey.Bits != 2048 {
		t.Fatalf("%+v / %+v", ec, rsaKey)
	}
	if !strings.HasPrefix(ec.AuthorizedKey, "ecdsa-sha2-nistp256 AAAA") || !strings.HasPrefix(ec.Fingerprint, "SHA256:") {
		t.Fatalf("%+v", ec)
	}

	// no PIN can be asked: background runs fail clearly
	ss, err := m.Signers([]string{lib}, "", nil)
	if err != nil || len(ss) != 2 {
		t.Fatalf("%v %d", err, len(ss))
	}
	if _, err := ss[0].Sign(rand.Reader, []byte("x")); !errors.Is(err, ErrPinNeeded) {
		t.Fatalf("want ErrPinNeeded, got %v", err)
	}

	// a wrong PIN first, then the right one; asked once for both keys
	asked := 0
	pin := func(k Key, retry bool) (string, error) {
		asked++
		if !retry {
			return "000000", nil
		}
		return "123456", nil
	}
	ss, err = m.Signers([]string{lib}, "", pin)
	if err != nil || len(ss) != 2 {
		t.Fatal(err)
	}
	data := []byte("session id and more")
	for _, s := range ss {
		sig, err := s.Sign(rand.Reader, data)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.PublicKey().Verify(data, sig); err != nil {
			t.Fatalf("%s: %v", s.PublicKey().Type(), err)
		}
	}
	if asked != 2 || m.LoggedIn() != 1 {
		t.Fatalf("asked %d times, logged in %d", asked, m.LoggedIn())
	}
	testRSASHA2(t, lib)
	m.Forget()
	if m.LoggedIn() != 0 {
		t.Fatal("still logged in")
	}
}

func testRSASHA2(t *testing.T, lib string) {
	m := NewManager()
	pin := func(Key, bool) (string, error) { return "123456", nil }
	var want string
	for _, k := range m.List([]string{lib})[0].Keys {
		if k.Type == "ssh-rsa" {
			want = k.Fingerprint
		}
	}
	ss, err := m.Signers([]string{lib}, want, pin)
	if err != nil || len(ss) != 1 {
		t.Fatal(err, len(ss))
	}
	as, ok := ss[0].(ssh.AlgorithmSigner)
	if !ok {
		t.Fatal("not an AlgorithmSigner")
	}
	data := []byte("hello")
	for _, algo := range []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSA} {
		sig, err := as.SignWithAlgorithm(rand.Reader, data, algo)
		if err != nil {
			t.Fatal(algo, err)
		}
		if sig.Format != algo {
			t.Fatalf("format %s want %s", sig.Format, algo)
		}
		if err := ss[0].PublicKey().Verify(data, sig); err != nil {
			t.Fatal(algo, err)
		}
	}
}
