//go:build !cgo

package smartcard

import (
	"errors"

	"golang.org/x/crypto/ssh"
)

// Available reports whether this build can use smart cards.
const Available = false

// ErrPinNeeded: no PIN could be asked (background runs).
var ErrPinNeeded = errors.New("smart card PIN needed")

// Manager is a stand-in: this build has no PKCS#11 support (built without cgo).
type Manager struct{ EachTime func() bool }

// NewManager creates a manager.
func NewManager() *Manager { return &Manager{} }

// List reports that smart cards are not available.
func (m *Manager) List(libs []string) []Report {
	out := make([]Report, 0, len(libs))
	for _, l := range libs {
		out = append(out, Report{Library: l, Error: ErrUnavailable.Error(), Tokens: []string{}, Keys: []Key{}})
	}
	return out
}

// Signers reports that smart cards are not available.
func (m *Manager) Signers(libs []string, want string, pin PinFunc) ([]ssh.Signer, error) {
	return nil, ErrUnavailable
}

// Forget does nothing.
func (m *Manager) Forget() {}

// LoggedIn is always 0.
func (m *Manager) LoggedIn() int { return 0 }
