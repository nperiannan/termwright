// Package sshkey generates OpenSSH key pairs and builds the remote commands
// that add a public key to a server's authorized keys (Linux/macOS/BSD and
// Windows OpenSSH).
package sshkey

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
)

// Generate creates a key pair at path (private, 0600) and path+".pub".
// kind is ed25519, rsa (4096 bits) or ecdsa (P-256). An empty passphrase
// leaves the private key unencrypted. It returns the public key line.
func Generate(kind, path, comment, passphrase string) (string, error) {
	if path == "" {
		return "", errors.New("no key file name")
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists - choose another name or use the existing key", path)
	}
	var priv crypto.PrivateKey
	var err error
	switch kind {
	case "", "ed25519":
		_, priv, err = ed25519.GenerateKey(rand.Reader)
	case "rsa":
		priv, err = rsa.GenerateKey(rand.Reader, 4096)
	case "ecdsa":
		priv, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	default:
		return "", fmt.Errorf("unknown key type %q", kind)
	}
	if err != nil {
		return "", err
	}
	comment = CleanComment(comment)
	var block *pem.Block
	if passphrase != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, comment, []byte(passphrase))
	} else {
		block, err = ssh.MarshalPrivateKey(priv, comment)
	}
	if err != nil {
		return "", err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return "", err
	}
	line := AuthorizedLine(signer.PublicKey(), comment)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(path+".pub", []byte(line+"\n"), 0o644); err != nil {
		return "", err
	}
	return line, nil
}

// PublicLine returns the public key line for a private key file: from
// path+".pub" when present, otherwise derived from the private key.
func PublicLine(path, passphrase string) (string, error) {
	if data, err := os.ReadFile(path + ".pub"); err == nil {
		pub, comment, _, _, err := ssh.ParseAuthorizedKey(data)
		if err == nil {
			return AuthorizedLine(pub, comment), nil
		}
	}
	if data, err := os.ReadFile(path); err == nil && IsPPK(data) {
		pub, comment, err := PPKPublicKey(data) // no passphrase needed
		if err != nil {
			return "", err
		}
		return AuthorizedLine(pub, comment), nil
	}
	signer, err := LoadSigner(path, passphrase)
	if err != nil {
		return "", err
	}
	return AuthorizedLine(signer.PublicKey(), filepath.Base(path)), nil
}

// LoadSigner reads a private key file (with an optional passphrase).
func LoadSigner(path, passphrase string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := ParseKey(data, passphrase)
	var pme *ssh.PassphraseMissingError
	if errors.As(err, &pme) {
		return nil, errors.New("the key is protected by a passphrase - enter it")
	}
	return s, err
}

var badComment = regexp.MustCompile(`[^A-Za-z0-9@._+-]+`)

// CleanComment keeps a key comment safe to embed in shell and PowerShell commands.
func CleanComment(c string) string {
	c = strings.Trim(badComment.ReplaceAllString(c, "_"), "_")
	if len(c) > 80 {
		c = c[:80]
	}
	return c
}

// AuthorizedLine formats "type base64 comment" with a sanitized comment.
func AuthorizedLine(pub ssh.PublicKey, comment string) string {
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if c := CleanComment(comment); c != "" {
		line += " " + c
	}
	return line
}

// Normalize validates a public key line and returns it in safe form.
func Normalize(line string) (string, error) {
	pub, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(line)))
	if err != nil {
		return "", fmt.Errorf("not a valid public key: %w", err)
	}
	return AuthorizedLine(pub, comment), nil
}

// PosixInstallCommand appends the key read from stdin to ~/.ssh/authorized_keys
// (creating it with the right permissions) unless it is already there. It is
// wrapped in sh -c so it works whatever the login shell is.
const PosixInstallCommand = `sh -c 'umask 077; d="$HOME/.ssh"; f="$d/authorized_keys"; ` +
	`mkdir -p "$d" && chmod 700 "$d" && touch "$f" && chmod 600 "$f" && read -r k && ` +
	`if grep -qxF "$k" "$f"; then echo "TERMWRIGHT-PRESENT $f"; else ` +
	`{ if [ -s "$f" ] && [ -n "$(tail -c1 "$f")" ]; then echo; fi; printf "%s\n" "$k"; } >> "$f" && echo "TERMWRIGHT-ADDED $f"; fi; ` +
	`command -v restorecon >/dev/null 2>&1 && restorecon -R "$d" >/dev/null 2>&1; true'`

// WindowsInstallCommand returns a PowerShell command (for Windows OpenSSH) that
// adds key to the user's authorized_keys, or for members of Administrators to
// %ProgramData%\ssh\administrators_authorized_keys with the ACL sshd requires.
func WindowsInstallCommand(key string) string {
	script := `$ErrorActionPreference='Stop'
$k='` + strings.ReplaceAll(key, "'", "''") + `'
$admin = [bool]((whoami /groups) -match 'S-1-5-32-544')
if ($admin) { $f = Join-Path $env:ProgramData 'ssh\administrators_authorized_keys' }
else { $d = Join-Path $env:USERPROFILE '.ssh'; New-Item -ItemType Directory -Force -Path $d | Out-Null; $f = Join-Path $d 'authorized_keys' }
if (-not (Test-Path $f)) { New-Item -ItemType File -Force -Path $f | Out-Null }
$lines = @(Get-Content -Path $f)
if ($lines -contains $k) { "TERMWRIGHT-PRESENT $f" } else { Add-Content -Path $f -Value $k -Encoding ascii; "TERMWRIGHT-ADDED $f" }
if ($admin) { icacls.exe $f /inheritance:r /grant '*S-1-5-32-544:F' /grant '*S-1-5-18:F' | Out-Null }
`
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return "powershell -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(b)
}

// DefaultPath suggests ~/.ssh/termwright_<host>_<kind> for a new key.
func DefaultPath(host, kind string) string {
	h, _ := os.UserHomeDir()
	host = strings.Trim(regexp.MustCompile(`[^A-Za-z0-9.-]+`).ReplaceAllString(host, "_"), "_")
	if host == "" {
		host = "key"
	}
	if kind == "" {
		kind = "ed25519"
	}
	return filepath.Join(h, ".ssh", "termwright_"+host+"_"+kind)
}

// PublicKeyOf returns the public key stored in an OpenSSH private key file
// ("openssh-key-v1", readable without the passphrase) or in path+".pub".
func PublicKeyOf(path string, data []byte) (ssh.PublicKey, error) {
	if b, err := os.ReadFile(path + ".pub"); err == nil {
		if k, _, _, _, err := ssh.ParseAuthorizedKey(b); err == nil {
			return k, nil
		}
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "OPENSSH PRIVATE KEY" {
		return nil, errors.New("not an OpenSSH private key")
	}
	const magic = "openssh-key-v1\x00"
	b := block.Bytes
	if !bytes.HasPrefix(b, []byte(magic)) {
		return nil, errors.New("not an OpenSSH private key")
	}
	b = b[len(magic):]
	readStr := func() ([]byte, bool) {
		if len(b) < 4 {
			return nil, false
		}
		n := int(binary.BigEndian.Uint32(b))
		if len(b) < 4+n {
			return nil, false
		}
		s := b[4 : 4+n]
		b = b[4+n:]
		return s, true
	}
	for i := 0; i < 3; i++ { // cipher, kdf, kdf options
		if _, ok := readStr(); !ok {
			return nil, errors.New("truncated key")
		}
	}
	if len(b) < 4 || binary.BigEndian.Uint32(b) < 1 {
		return nil, errors.New("no key in file")
	}
	b = b[4:]
	pub, ok := readStr()
	if !ok {
		return nil, errors.New("truncated key")
	}
	return ssh.ParsePublicKey(pub)
}

// IsSecurityKey reports whether k is a FIDO2/U2F hardware key ("sk-…").
func IsSecurityKey(k ssh.PublicKey) bool {
	return k != nil && strings.HasPrefix(k.Type(), "sk-")
}

// CertSigner pairs signer with the OpenSSH certificate in certPath (or
// keyPath+"-cert.pub" when certPath is ""). ok is false when there is none.
func CertSigner(keyPath, certPath string, signer ssh.Signer) (ssh.Signer, bool, error) {
	if certPath == "" {
		certPath = keyPath + "-cert.pub"
	}
	b, err := os.ReadFile(certPath)
	if err != nil {
		return nil, false, nil
	}
	k, _, _, _, err := ssh.ParseAuthorizedKey(b)
	if err != nil {
		return nil, false, fmt.Errorf("certificate %s: %w", certPath, err)
	}
	cert, ok := k.(*ssh.Certificate)
	if !ok {
		return nil, false, fmt.Errorf("%s is not an SSH certificate", certPath)
	}
	cs, err := ssh.NewCertSigner(cert, signer)
	if err != nil {
		return nil, false, fmt.Errorf("certificate %s does not match the key: %w", certPath, err)
	}
	return cs, true, nil
}
