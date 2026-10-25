package term

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/sshkey"
)

// Real OpenSSH server that trusts a user CA (set up by hand):
// TW_CERT_TEST=<dir with user, user-cert.pub>, sshd on 127.0.0.1:2301, user tw.
func TestCertificateLogin(t *testing.T) {
	dir := os.Getenv("TW_CERT_TEST")
	if dir == "" {
		t.Skip("set TW_CERT_TEST to a folder with user / user-cert.pub and run sshd with TrustedUserCAKeys on :2301")
	}
	t.Setenv("SSH_AUTH_SOCK", "")
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	autoAnswer(m, em, "")
	s := session.Defaults()
	s.Hostname, s.Port, s.Username, s.AuthMethods = "127.0.0.1", 2301, "tw", "publickey"
	s.IdentityFile = filepath.Join(dir, "user")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cl, _, cleanup, err := m.sshClient(ctx, &Tab{id: "c", sess: s}, s, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	defer cl.Close()
	// with an explicit certificate file elsewhere
	other := t.TempDir()
	b, _ := os.ReadFile(filepath.Join(dir, "user-cert.pub"))
	os.WriteFile(filepath.Join(other, "my.cert"), b, 0o644)
	k, _ := os.ReadFile(filepath.Join(dir, "user"))
	os.WriteFile(filepath.Join(other, "key"), k, 0o600)
	s.IdentityFile, s.CertificateFile = filepath.Join(other, "key"), filepath.Join(other, "my.cert")
	cl2, _, cleanup2, err := m.sshClient(ctx, &Tab{id: "c2", sess: s}, s, 0)
	if err != nil {
		t.Fatal(err)
	}
	cleanup2()
	cl2.Close()
}

// The key and certificate helpers, and security-key detection.
func TestCertAndSecurityKeyHelpers(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "k")
	pub, err := sshkey.Generate("ed25519", f, "test", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = pub
	data, _ := os.ReadFile(f)
	signer, _ := sshkey.ParseKey(data, "")
	pk, err := sshkey.PublicKeyOf(filepath.Join(dir, "missing"), data) // from the private file itself
	if err != nil || string(pk.Marshal()) != string(signer.PublicKey().Marshal()) || sshkey.IsSecurityKey(pk) {
		t.Fatalf("public key of private file: %v", err)
	}
	if _, ok, err := sshkey.CertSigner(f, "", signer); ok || err != nil {
		t.Fatal("no certificate expected")
	}
	// a certificate signed by a test CA
	caKey, _ := sshkey.Generate("ed25519", filepath.Join(dir, "ca"), "ca", "")
	_ = caKey
	cad, _ := os.ReadFile(filepath.Join(dir, "ca"))
	ca, _ := sshkey.ParseKey(cad, "")
	cert := &ssh.Certificate{Key: signer.PublicKey(), CertType: ssh.UserCert, KeyId: "t", ValidPrincipals: []string{"tw"},
		ValidBefore: ssh.CertTimeInfinity}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(f+"-cert.pub", ssh.MarshalAuthorizedKey(cert), 0o644)
	cs, ok, err := sshkey.CertSigner(f, "", signer)
	if !ok || err != nil || !strings.Contains(cs.PublicKey().Type(), "cert") {
		t.Fatalf("cert signer: %v %v", ok, err)
	}
	// an sk-ssh-ed25519 public key is recognised as a security key
	var blob []byte
	put := func(b []byte) { blob = binary.BigEndian.AppendUint32(blob, uint32(len(b))); blob = append(blob, b...) }
	put([]byte("sk-ssh-ed25519@openssh.com"))
	put(make([]byte, 32))
	put([]byte("ssh:"))
	skPub, err := ssh.ParsePublicKey(blob)
	if err != nil || !sshkey.IsSecurityKey(skPub) {
		t.Fatalf("sk key: %v", err)
	}
	// not loaded in an agent: the user is asked, cancelling skips the key
	t.Setenv("SSH_AUTH_SOCK", "")
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	asked := 0
	em.onPrompt = func(p Prompt) { asked++; m.Prompts().Answer(p.ID, PromptReply{OK: false}) }
	if sg := m.securityKeySigner(context.Background(), &Tab{id: "x"}, "/k", skPub); sg != nil || asked != 1 {
		t.Fatalf("expected one prompt and no signer, got %v %d", sg, asked)
	}
}
