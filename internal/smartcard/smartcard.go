// Package smartcard uses SSH keys on smart cards and security tokens (PIV,
// CAC, YubiKey PIV, HSMs) through their PKCS#11 library. The private key
// never leaves the card: signatures are made on the card after the PIN is
// entered. PINs are kept in memory only.
package smartcard

import (
	"crypto"
	"errors"
	"os"
	"runtime"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ErrUnavailable is returned when this build has no PKCS#11 support.
var ErrUnavailable = errors.New("smart cards are not supported by this build")

// Key is a key on a card.
type Key struct {
	Library       string `json:"library"`
	Token         string `json:"token"`  // token (card) label
	Serial        string `json:"serial"` // token serial number
	Slot          uint   `json:"slot"`
	ID            string `json:"id"` // CKA_ID, hex
	Label         string `json:"label"`
	Type          string `json:"type"` // SSH key type, e.g. ecdsa-sha2-nistp256
	Bits          int    `json:"bits"`
	Fingerprint   string `json:"fingerprint"`   // SHA256:…
	AuthorizedKey string `json:"authorizedKey"` // OpenSSH public key line for authorized_keys
	Subject       string `json:"subject,omitempty"`
	pub           crypto.PublicKey
}

// PinFunc asks for a token's PIN; retry is true after a wrong PIN.
type PinFunc func(k Key, retry bool) (string, error)

// Report is the result of listing the keys of one library.
type Report struct {
	Library string   `json:"library"`
	Error   string   `json:"error,omitempty"`
	Tokens  []string `json:"tokens"`
	Keys    []Key    `json:"keys"`
}

// Matches reports whether a key is the one a session asks for: "" or "any"
// = every key, otherwise the SHA256 fingerprint (or "token/label").
func (k Key) Matches(want string) bool {
	want = strings.TrimSpace(want)
	if want == "" || strings.EqualFold(want, "any") {
		return true
	}
	if strings.HasPrefix(want, "SHA256:") {
		return want == k.Fingerprint
	}
	return strings.EqualFold(want, k.Token+"/"+k.Label) || strings.EqualFold(want, k.Label)
}

func describe(k *Key, pub ssh.PublicKey) {
	k.Type = pub.Type()
	k.Fingerprint = ssh.FingerprintSHA256(pub)
	comment := k.Label
	if comment == "" {
		comment = "card"
	}
	k.AuthorizedKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " " + strings.ReplaceAll(comment, " ", "_")
}

// knownLibraries are the usual places of PKCS#11 libraries for smart cards.
func knownLibraries() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Library/OpenSC/lib/opensc-pkcs11.so",
			"/usr/local/lib/opensc-pkcs11.so",
			"/opt/homebrew/lib/opensc-pkcs11.so",
			"/usr/local/lib/libykcs11.dylib",
			"/opt/homebrew/lib/libykcs11.dylib",
			"/Library/Frameworks/eToken.framework/Versions/A/libeToken.dylib",
		}
	case "windows":
		pf := os.Getenv("ProgramFiles")
		if pf == "" {
			pf = `C:\Program Files`
		}
		return []string{
			pf + `\OpenSC Project\OpenSC\pkcs11\opensc-pkcs11.dll`,
			os.Getenv("SystemRoot") + `\System32\opensc-pkcs11.dll`,
			pf + `\Yubico\Yubico PIV Tool\bin\libykcs11.dll`,
			os.Getenv("SystemRoot") + `\System32\eTPKCS11.dll`,
		}
	default:
		return []string{
			"/usr/lib/x86_64-linux-gnu/opensc-pkcs11.so",
			"/usr/lib/aarch64-linux-gnu/opensc-pkcs11.so",
			"/usr/lib64/opensc-pkcs11.so",
			"/usr/lib/opensc-pkcs11.so",
			"/usr/lib/x86_64-linux-gnu/libykcs11.so",
			"/usr/lib64/libykcs11.so",
			"/usr/lib/libykcs11.so",
			"/usr/lib/libeTPkcs11.so",
		}
	}
}

// DefaultLibraries returns the PKCS#11 libraries installed in their usual
// places (OpenSC, YubiKey, SafeNet).
func DefaultLibraries() []string {
	var out []string
	for _, p := range knownLibraries() {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			out = append(out, p)
		}
	}
	return out
}
