package securecrt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/blowfish"
)

// Saved session passwords in SecureCRT session files and XML exports, so that
// a user's own sessions can be migrated with their passwords:
//
//	S:"Password V2"=02:<hex>   AES-256-CBC, key SHA-256(configuration passphrase), zero IV
//	S:"Password V2"=03:<hex>   16-byte salt + AES-256-CBC, key and IV from bcrypt_pbkdf(passphrase, salt, 16 rounds)
//	S:"Password"=u<hex>        older versions: two Blowfish-CBC layers with fixed keys
//
// V2 plaintext: uint32 length (little endian), the UTF-8 password, SHA-256 of the password.

// Password key names.
const (
	PasswordV2Key = "Password V2"
	PasswordV1Key = "Password"
)

// ErrNeedPassphrase means the password is protected by a configuration passphrase.
var ErrNeedPassphrase = errors.New("protected by a SecureCRT configuration passphrase")

// badPassword asks for the configuration passphrase when none was given.
func badPassword(prefix, passphrase string) error {
	if passphrase == "" {
		return ErrNeedPassphrase
	}
	return errBadPassword
}

// errBadPassword means the value could not be decrypted (wrong passphrase or unknown format).
var errBadPassword = errors.New("cannot decrypt the saved password (wrong configuration passphrase?)")

// SavedPassword returns the session's saved password. ok is false when the
// session has none. configPassphrase is SecureCRT's configuration passphrase
// ("" when none was set).
func SavedPassword(f *File, configPassphrase string) (pw string, ok bool, err error) {
	if v := f.Str(PasswordV2Key, ""); v != "" {
		if !v2Format.MatchString(strings.TrimSpace(v)) {
			return v, true, nil // written in plain text (export without password encryption)
		}
		pw, err = DecryptV2(strings.TrimSpace(v), configPassphrase)
		return pw, err == nil, err
	}
	if v := f.Str(PasswordV1Key, ""); v != "" {
		if !v1Format.MatchString(strings.TrimSpace(v)) {
			return v, true, nil
		}
		pw, err = DecryptV1(strings.TrimSpace(v))
		return pw, err == nil, err
	}
	return "", false, nil
}

var (
	v2Format = regexp.MustCompile(`^(0[0-9]:[0-9A-Fa-f]*|[0-9A-Fa-f]{32,})$`)
	v1Format = regexp.MustCompile(`^u[0-9A-Fa-f]{16,}$`)
)

// DecryptV2 decrypts a "Password V2" value ("02:…" or "03:…").
func DecryptV2(v, passphrase string) (string, error) {
	prefix, data, found := strings.Cut(v, ":")
	if !found {
		prefix, data = "02", v
	}
	raw, err := hex.DecodeString(strings.TrimSpace(data))
	if err != nil {
		return "", errBadPassword
	}
	var key, iv []byte
	switch prefix {
	case "02":
		k := sha256.Sum256([]byte(passphrase))
		key, iv = k[:], make([]byte, aes.BlockSize)
	case "03":
		if len(raw) < 16 {
			return "", errBadPassword
		}
		kdf, err := bcryptPBKDF([]byte(passphrase), raw[:16], 16, 32+aes.BlockSize)
		if err != nil {
			return "", err
		}
		key, iv, raw = kdf[:32], kdf[32:], raw[16:]
	default:
		return "", errors.New("unknown password format " + prefix)
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return "", errBadPassword
	}
	block, _ := aes.NewCipher(key)
	plain := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, raw)
	if len(plain) < 4 {
		return "", errBadPassword
	}
	n := int(binary.LittleEndian.Uint32(plain))
	if n < 0 || 4+n+sha256.Size > len(plain) {
		return "", badPassword(prefix, passphrase)
	}
	pw := plain[4 : 4+n]
	sum := sha256.Sum256(pw)
	if !bytes.Equal(sum[:], plain[4+n:4+n+sha256.Size]) {
		return "", badPassword(prefix, passphrase)
	}
	return string(pw), nil
}

// EncryptV2 produces a "02:" value (used by tests and for round trips).
func EncryptV2(pw, passphrase string) string {
	k := sha256.Sum256([]byte(passphrase))
	sum := sha256.Sum256([]byte(pw))
	plain := binary.LittleEndian.AppendUint32(nil, uint32(len(pw)))
	plain = append(append(plain, pw...), sum[:]...)
	for len(plain)%aes.BlockSize != 0 {
		plain = append(plain, 0)
	}
	block, _ := aes.NewCipher(k[:])
	cipher.NewCBCEncrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(plain, plain)
	return "02:" + hex.EncodeToString(plain)
}

var (
	v1Key1 = []byte{0x24, 0xa6, 0x3d, 0xde, 0x5b, 0xd3, 0xb3, 0x82, 0x9c, 0x7e, 0x06, 0xf4, 0x08, 0x16, 0xaa, 0x07}
	v1Key2 = []byte{0x5f, 0xb0, 0x45, 0xa2, 0x94, 0x17, 0xd9, 0x16, 0xc6, 0xc6, 0xa2, 0xff, 0x06, 0x41, 0x82, 0xf7}
)

// DecryptV1 decrypts an old-style "Password" value ("u" + hex).
func DecryptV1(v string) (string, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(v), "u"))
	if err != nil || len(raw) <= 8 || len(raw)%blowfish.BlockSize != 0 {
		return "", errBadPassword
	}
	c1, _ := blowfish.NewCipher(v1Key1)
	c2, _ := blowfish.NewCipher(v1Key2)
	iv := make([]byte, blowfish.BlockSize)
	step1 := make([]byte, len(raw))
	cipher.NewCBCDecrypter(c1, iv).CryptBlocks(step1, raw)
	inner := step1[4 : len(step1)-4]
	if len(inner)%blowfish.BlockSize != 0 {
		return "", errBadPassword
	}
	step2 := make([]byte, len(inner))
	cipher.NewCBCDecrypter(c2, iv).CryptBlocks(step2, inner)
	var u []uint16
	for i := 0; i+1 < len(step2); i += 2 {
		c := uint16(step2[i]) | uint16(step2[i+1])<<8
		if c == 0 {
			return string(utf16.Decode(u)), nil
		}
		u = append(u, c)
	}
	return "", errBadPassword
}

// EncryptV1 produces an old-style value (tests only).
func EncryptV1(pw string) string {
	var plain []byte
	for _, c := range utf16.Encode([]rune(pw)) {
		plain = append(plain, byte(c), byte(c>>8))
	}
	plain = append(plain, 0, 0)
	for len(plain)%blowfish.BlockSize != 0 {
		plain = append(plain, 0)
	}
	c1, _ := blowfish.NewCipher(v1Key1)
	c2, _ := blowfish.NewCipher(v1Key2)
	iv := make([]byte, blowfish.BlockSize)
	cipher.NewCBCEncrypter(c2, iv).CryptBlocks(plain, plain)
	outer := append(append([]byte{1, 2, 3, 4}, plain...), 5, 6, 7, 8)
	cipher.NewCBCEncrypter(c1, iv).CryptBlocks(outer, outer)
	return "u" + hex.EncodeToString(outer)
}
