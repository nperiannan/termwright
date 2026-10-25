package sshkey

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Fixtures in testdata were made with PuTTYgen 0.81 (throwaway test keys).
func TestPPK(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.ppk")
	if len(files) < 13 {
		t.Fatalf("fixtures missing: %d", len(files))
	}
	for _, f := range files {
		data, _ := os.ReadFile(f)
		base := strings.SplitN(filepath.Base(f), "-", 3)
		want, _ := os.ReadFile("testdata/" + base[0] + "-" + strings.TrimSuffix(base[1], ".ppk") + ".pub.expected")
		wantPub, _, _, _, err := ssh.ParseAuthorizedKey(want)
		if err != nil {
			t.Fatal(f, err)
		}
		enc := strings.Contains(f, "-enc")
		s, err := ParsePPK(data, "")
		var pme *ssh.PassphraseMissingError
		if enc {
			if !errors.As(err, &pme) || string(pme.PublicKey.Marshal()) != string(wantPub.Marshal()) {
				t.Fatalf("%s: want PassphraseMissingError, got %v", f, err)
			}
			if _, err := ParsePPK(data, "wrong"); !errors.Is(err, ErrBadPassphrase) {
				t.Fatalf("%s: wrong passphrase: %v", f, err)
			}
			if s, err = ParsePPK(data, "secret pass"); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
		} else if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if string(s.PublicKey().Marshal()) != string(wantPub.Marshal()) {
			t.Fatalf("%s: public key differs", f)
		}
		sig, err := s.Sign(nil, []byte("hello"))
		if err != nil || s.PublicKey().Verify([]byte("hello"), sig) != nil {
			t.Fatalf("%s: signature does not verify: %v", f, err)
		}
		if line, err := PublicLine(f, ""); err != nil || !strings.HasPrefix(line, s.PublicKey().Type()) {
			t.Fatalf("%s: PublicLine %q %v", f, line, err)
		}
	}
	// tampered file
	data, _ := os.ReadFile("testdata/ed25519-v3.ppk")
	bad := strings.Replace(string(data), "Comment: test", "Comment: Test", 1)
	if _, err := ParsePPK([]byte(bad), ""); err == nil {
		t.Fatal("tampered key accepted")
	}
}
