package term

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/smartcard"
)

// cardSigners returns the session's smart card keys (Session Options →
// SSH2 Authentication → Smart card). The PIN is asked at the first signature,
// i.e. only when the server accepts the key.
func (m *Manager) cardSigners(ctx context.Context, t *Tab, s session.Session) []ssh.Signer {
	if strings.TrimSpace(s.SmartCard) == "" || m.SmartCards == nil {
		return nil
	}
	var libs []string
	if m.SmartCardLibraries != nil {
		libs = m.SmartCardLibraries()
	}
	if len(libs) == 0 {
		libs = smartcard.DefaultLibraries()
	}
	if len(libs) == 0 {
		m.emit.Emit("toast", map[string]string{"level": "error", "message": "Smart card: no PKCS#11 library found - add your card's library in Global Options → Smart Cards"})
		return nil
	}
	_, quiet := m.quiet.Load(t.id)
	pin := func(k smartcard.Key, retry bool) (string, error) {
		if quiet {
			return "", fmt.Errorf("%w for %s: connect once in a tab to enter it", smartcard.ErrPinNeeded, k.Token)
		}
		msg := "PIN for the smart card " + k.Token
		if k.Label != "" {
			msg += " (key " + k.Label + ")"
		}
		if retry {
			msg = "Wrong PIN. " + msg
		}
		r, err := m.prompts.Ask(ctx, Prompt{TabID: t.id, Kind: "credentials", Title: "Smart Card PIN", Message: msg,
			Fields: []PromptField{{Label: "PIN", Secret: true}}})
		if err != nil {
			return "", err
		}
		return r.Values[0], nil
	}
	signers, err := m.SmartCards.Signers(libs, s.SmartCard, pin)
	if err != nil {
		m.emit.Emit("toast", map[string]string{"level": "error", "message": "Smart card: " + err.Error()})
		return nil
	}
	if len(signers) == 0 {
		what := "no key"
		if s.SmartCard != "any" {
			what = "the key " + s.SmartCard + " is not"
		}
		m.emit.Emit("toast", map[string]string{"level": "error", "message": "Smart card: " + what + " on the inserted cards"})
		return nil
	}
	// an OpenSSH certificate for a card key (Session Options → certificate file)
	var out []ssh.Signer
	if cp := expandHome(strings.TrimSpace(s.CertificateFile)); cp != "" {
		if b, err := os.ReadFile(cp); err == nil {
			if k, _, _, _, err := ssh.ParseAuthorizedKey(b); err == nil {
				if cert, ok := k.(*ssh.Certificate); ok {
					for _, sg := range signers {
						if cs, err := ssh.NewCertSigner(cert, sg); err == nil {
							out = append(out, cs)
						}
					}
				}
			}
		}
	}
	return append(out, signers...)
}

// cardAgent offers smart card keys through a forwarded agent, next to the
// keys of the local SSH agent (if one runs).
type cardAgent struct {
	mu    sync.Mutex
	local agent.ExtendedAgent // nil without a local agent
	cards []ssh.Signer
}

func (c *cardAgent) card(key ssh.PublicKey) ssh.Signer {
	for _, s := range c.cards {
		if string(s.PublicKey().Marshal()) == string(key.Marshal()) {
			return s
		}
	}
	return nil
}

func (c *cardAgent) List() ([]*agent.Key, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*agent.Key
	for _, s := range c.cards {
		out = append(out, &agent.Key{Format: s.PublicKey().Type(), Blob: s.PublicKey().Marshal(), Comment: "smart card"})
	}
	if c.local != nil {
		if ks, err := c.local.List(); err == nil {
			out = append(out, ks...)
		}
	}
	return out, nil
}

func (c *cardAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return c.SignWithFlags(key, data, 0)
}

func (c *cardAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags agent.SignatureFlags) (*ssh.Signature, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s := c.card(key); s != nil {
		if as, ok := s.(ssh.AlgorithmSigner); ok && key.Type() == ssh.KeyAlgoRSA {
			switch {
			case flags&agent.SignatureFlagRsaSha512 != 0:
				return as.SignWithAlgorithm(nil, data, ssh.KeyAlgoRSASHA512)
			case flags&agent.SignatureFlagRsaSha256 != 0:
				return as.SignWithAlgorithm(nil, data, ssh.KeyAlgoRSASHA256)
			}
		}
		return s.Sign(nil, data)
	}
	if c.local == nil {
		return nil, errors.New("key not found")
	}
	return c.local.SignWithFlags(key, data, flags)
}

func (c *cardAgent) Add(key agent.AddedKey) error {
	if c.local == nil {
		return errors.New("adding keys is not supported")
	}
	return c.local.Add(key)
}

func (c *cardAgent) Remove(key ssh.PublicKey) error {
	if c.local == nil || c.card(key) != nil {
		return errors.New("removing this key is not supported")
	}
	return c.local.Remove(key)
}

func (c *cardAgent) RemoveAll() error {
	if c.local == nil {
		return nil
	}
	return c.local.RemoveAll()
}

func (c *cardAgent) Lock(p []byte) error {
	if c.local == nil {
		return errors.New("locking is not supported")
	}
	return c.local.Lock(p)
}

func (c *cardAgent) Unlock(p []byte) error {
	if c.local == nil {
		return errors.New("locking is not supported")
	}
	return c.local.Unlock(p)
}

func (c *cardAgent) Signers() ([]ssh.Signer, error) {
	out := append([]ssh.Signer(nil), c.cards...)
	if c.local != nil {
		if ss, err := c.local.Signers(); err == nil {
			out = append(out, ss...)
		}
	}
	return out, nil
}

func (c *cardAgent) Extension(t string, contents []byte) ([]byte, error) {
	if c.local == nil {
		return nil, agent.ErrExtensionUnsupported
	}
	return c.local.Extension(t, contents)
}
