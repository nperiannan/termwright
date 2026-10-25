// Package secrets stores saved session passwords in the operating system's
// credential store (macOS Keychain, Windows Credential Manager, Secret Service
// on Linux). Passwords are never written to session files.
package secrets

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const service = "Termwright"

// legacyService is the keychain service used before the app was renamed
// (OpenCRT). Passwords found there are copied to the new service on first use.
const legacyService = "OpenCRT"

// emptyMarker stands for a saved empty password (some keychains refuse "").
const emptyMarker = "\x00termwright-empty-password"

// Lookup returns the saved password for a key and whether one is saved; an
// intentionally empty password returns ("", true).
func Lookup(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	pw, err := keyring.Get(service, key)
	if err != nil {
		old, lerr := keyring.Get(legacyService, key)
		if lerr != nil {
			return "", false
		}
		_ = keyring.Set(service, key, old)
		pw = old
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
	_ = keyring.Delete(legacyService, sessionPath)
	err := keyring.Delete(service, sessionPath)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
