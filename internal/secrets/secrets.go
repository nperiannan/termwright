// Package secrets stores saved session passwords in the operating system's
// credential store (macOS Keychain, Windows Credential Manager, Secret Service
// on Linux). Passwords are never written to session files.
package secrets

import (
	"errors"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

const service = "Termwright"

// emptyMarker stands for a saved empty password (some keychains refuse "").
const emptyMarker = "\x00termwright-empty-password"

// EnvName is the environment variable that can hold a secret on a computer without a credential
// store (a CI machine running "termwright tests"): TERMWRIGHT_SECRET_ and the key in capitals with
// every other character as _ (Lab/core-sw1 → TERMWRIGHT_SECRET_LAB_CORE_SW1).
func EnvName(key string) string {
	b := []byte(strings.ToUpper(key))
	for i, c := range b {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			b[i] = '_'
		}
	}
	return "TERMWRIGHT_SECRET_" + string(b)
}

// Lookup returns the saved password for a key and whether one is saved; an
// intentionally empty password returns ("", true). When the credential store has
// none, the EnvName environment variable is used if it is set.
func Lookup(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	pw, err := keyring.Get(service, key)
	if err != nil {
		if v, ok := os.LookupEnv(EnvName(key)); ok {
			return v, true
		}
		return "", false
	}
	if pw == emptyMarker {
		return "", true
	}
	return pw, true
}

// Get returns the saved password for a session, or "" if none.
func Get(sessionPath string) string {
	pw, _ := Lookup(sessionPath)
	return pw
}

// Set saves a password for a session.
func Set(sessionPath, password string) error {
	if sessionPath == "" {
		return errors.New("session has no path")
	}
	if password == "" {
		password = emptyMarker
	}
	return keyring.Set(service, sessionPath, password)
}

// Delete removes a saved password; missing entries are not an error.
func Delete(sessionPath string) error {
	err := keyring.Delete(service, sessionPath)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
