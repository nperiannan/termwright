package term

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
)

func TestResolveAlgos(t *testing.T) {
	// nothing asked for: the library's own defaults
	if l, err := resolveAlgos(algoKex, "", false); l != nil || err != nil {
		t.Fatalf("default: %v %v", l, err)
	}
	usual, _ := algoSets(algoKex)
	// legacy: the usual list first, the weak ones after it
	l, err := resolveAlgos(algoKex, "", true)
	if err != nil || !slices.Equal(l[:len(usual)], usual) || !slices.Contains(l, ssh.InsecureKeyExchangeDH1SHA1) {
		t.Fatalf("legacy: %v %v", l, err)
	}
	if slices.Index(l, ssh.InsecureKeyExchangeDH1SHA1) < slices.Index(l, ssh.KeyExchangeCurve25519) {
		t.Fatal("a weak algorithm is preferred over a modern one")
	}
	// exact list, add, front, remove (with a pattern)
	if l, _ := resolveAlgos(algoCipher, "aes128-cbc, aes256-ctr", false); !slices.Equal(l, []string{"aes128-cbc", "aes256-ctr"}) {
		t.Fatalf("exact: %v", l)
	}
	if l, _ := resolveAlgos(algoCipher, "+3des-cbc", false); l[len(l)-1] != "3des-cbc" {
		t.Fatalf("append: %v", l)
	}
	if l, _ := resolveAlgos(algoMAC, "^hmac-sha1", false); l[0] != "hmac-sha1" || len(l) < 3 {
		t.Fatalf("prepend: %v", l)
	}
	if l, _ := resolveAlgos(algoKex, "-ecdh-*", false); slices.ContainsFunc(l, func(a string) bool { return strings.HasPrefix(a, "ecdh-") }) || !slices.Contains(l, ssh.KeyExchangeCurve25519) {
		t.Fatalf("remove: %v", l)
	}
	// a name Termwright cannot use is refused, naming it
	if _, err := resolveAlgos(algoKex, "+diffie-hellman-group9-sha1", false); err == nil || !strings.Contains(err.Error(), "group9") {
		t.Fatalf("unknown name: %v", err)
	}
	if _, err := resolveAlgos(algoCipher, "-*", false); err == nil {
		t.Fatal("an empty list was accepted")
	}
}

// legacyServer is an SSH server such as an old switch: it offers only weak algorithms.
func legacyServer(t *testing.T) string {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{
		Config: ssh.Config{
			KeyExchanges: []string{ssh.InsecureKeyExchangeDH1SHA1},
			Ciphers:      []string{ssh.InsecureCipherAES128CBC},
			MACs:         []string{ssh.HMACSHA1},
		},
		PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			if c.User() == "admin" && string(p) == "pw" {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					ch.Reject(ssh.Prohibited, "test")
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// The case of issue #4: "no common algorithm for key exchange ... peer offered:
// [diffie-hellman-group1-sha1]".
func TestLegacyAlgorithmServer(t *testing.T) {
	addr := legacyServer(t)
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	autoAnswer(m, em, "pw")
	connect := func(s session.Session) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cl, _, cleanup, err := m.sshClient(ctx, &Tab{id: "x", sess: s}, s, 0)
		if err == nil {
			cleanup()
			cl.Close()
		}
		return err
	}

	s := sshSession(addr)
	err := connect(s)
	if err == nil || !strings.Contains(err.Error(), "no common algorithm") || !strings.Contains(err.Error(), "Allow legacy algorithms") {
		t.Fatalf("by default the login must fail and say what to do: %v", err)
	}

	s.LegacyAlgorithms = true
	if err := connect(s); err != nil {
		t.Fatalf("with legacy algorithms allowed: %v", err)
	}

	// custom lists do the same, one by one
	s = sshSession(addr)
	s.KexAlgorithms, s.Ciphers, s.MACs = "+"+ssh.InsecureKeyExchangeDH1SHA1, "+"+ssh.InsecureCipherAES128CBC, "+"+ssh.HMACSHA1
	if err := connect(s); err != nil {
		t.Fatalf("with custom lists: %v", err)
	}
	s.Ciphers = "+arcfour-nonsense"
	if err := connect(s); err == nil || !strings.Contains(err.Error(), "arcfour-nonsense") {
		t.Fatalf("an unknown name must be reported: %v", err)
	}
}
