package snmp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"hash"
	"strings"
)

// Auth is a USM authentication protocol (RFC 3414, RFC 7860).
type Auth string

// Priv is a USM privacy protocol (RFC 3414 DES, RFC 3826 AES-128; AES-192/256 with Blumenthal key extension).
type Priv string

const (
	AuthNone   Auth = ""
	AuthMD5    Auth = "MD5"
	AuthSHA    Auth = "SHA"
	AuthSHA224 Auth = "SHA-224"
	AuthSHA256 Auth = "SHA-256"
	AuthSHA384 Auth = "SHA-384"
	AuthSHA512 Auth = "SHA-512"

	PrivNone   Priv = ""
	PrivDES    Priv = "DES"
	PrivAES    Priv = "AES"
	PrivAES192 Priv = "AES-192"
	PrivAES256 Priv = "AES-256"
)

func (a Auth) hash() (func() hash.Hash, int, error) {
	switch strings.ToUpper(strings.ReplaceAll(string(a), "_", "-")) {
	case "MD5":
		return md5.New, 12, nil
	case "SHA", "SHA1", "SHA-1":
		return sha1.New, 12, nil
	case "SHA-224", "SHA224":
		return sha256.New224, 16, nil
	case "SHA-256", "SHA256":
		return sha256.New, 24, nil
	case "SHA-384", "SHA384":
		return sha512.New384, 32, nil
	case "SHA-512", "SHA512":
		return sha512.New, 48, nil
	}
	return nil, 0, fmt.Errorf("snmp: unknown authentication protocol %q", a)
}

// PasswordKey turns a password into a key (RFC 3414 A.2: the password repeated to 1 MB, hashed).
func PasswordKey(a Auth, password string) ([]byte, error) {
	h, _, err := a.hash()
	if err != nil {
		return nil, err
	}
	if len(password) < 8 {
		return nil, errors.New("snmp: a v3 password needs at least 8 characters")
	}
	d := h()
	pw := []byte(password)
	buf := make([]byte, 64)
	for n, i := 0, 0; n < 1048576; n += 64 {
		for j := range buf {
			buf[j] = pw[i%len(pw)]
			i++
		}
		d.Write(buf)
	}
	return d.Sum(nil), nil
}

// LocalKey localizes a key to an engine: H(Ku || engineID || Ku).
func LocalKey(a Auth, ku, engineID []byte) ([]byte, error) {
	h, _, err := a.hash()
	if err != nil {
		return nil, err
	}
	d := h()
	d.Write(ku)
	d.Write(engineID)
	d.Write(ku)
	return d.Sum(nil), nil
}

// privKeyLen: how many key bytes a privacy protocol uses.
func (p Priv) keyLen() (int, error) {
	switch strings.ToUpper(string(p)) {
	case "DES":
		return 16, nil // 8 key + 8 pre-IV
	case "AES", "AES128", "AES-128":
		return 16, nil
	case "AES192", "AES-192":
		return 24, nil
	case "AES256", "AES-256":
		return 32, nil
	}
	return 0, fmt.Errorf("snmp: unknown privacy protocol %q", p)
}

// extendKey makes a localized key long enough (Blumenthal: K || H(K) || H(H(K))…).
func extendKey(a Auth, k []byte, n int) []byte {
	h, _, _ := a.hash()
	out := append([]byte(nil), k...)
	for len(out) < n {
		d := h()
		d.Write(out)
		out = append(out, d.Sum(nil)...)
	}
	return out[:n]
}

// usm: one user's security state against one engine.
type usm struct {
	user     string
	auth     Auth
	priv     Priv
	authKey  []byte
	privKey  []byte
	engineID []byte
	boots    int64
	time     int64
	salt     uint64
}

// localize sets the keys for an engine from the passwords.
func (u *usm) localize(authPass, privPass string) error {
	if u.auth == AuthNone {
		return nil
	}
	ku, err := PasswordKey(u.auth, authPass)
	if err != nil {
		return err
	}
	if u.authKey, err = LocalKey(u.auth, ku, u.engineID); err != nil {
		return err
	}
	if u.priv == PrivNone {
		return nil
	}
	n, err := u.priv.keyLen()
	if err != nil {
		return err
	}
	kp, err := PasswordKey(u.auth, privPass)
	if err != nil {
		return err
	}
	lk, err := LocalKey(u.auth, kp, u.engineID)
	if err != nil {
		return err
	}
	u.privKey = extendKey(u.auth, lk, n)
	return nil
}

func (u *usm) authLen() int {
	if u.auth == AuthNone {
		return 0
	}
	_, n, _ := u.auth.hash()
	return n
}

func (u *usm) mac(msg []byte) []byte {
	h, n, _ := u.auth.hash()
	m := hmac.New(h, u.authKey)
	m.Write(msg)
	return m.Sum(nil)[:n]
}

func (u *usm) encrypt(plain []byte) (cipherText, salt []byte, err error) {
	u.salt++
	switch strings.ToUpper(string(u.priv)) {
	case "DES":
		salt = make([]byte, 8)
		be32(salt[0:4], uint32(u.boots))
		be32(salt[4:8], uint32(u.salt))
		iv := make([]byte, 8)
		for i := range iv {
			iv[i] = u.privKey[8+i] ^ salt[i]
		}
		blk, err := des.NewCipher(u.privKey[:8])
		if err != nil {
			return nil, nil, err
		}
		p := append([]byte(nil), plain...)
		if r := len(p) % 8; r != 0 {
			p = append(p, make([]byte, 8-r)...)
		}
		out := make([]byte, len(p))
		cipher.NewCBCEncrypter(blk, iv).CryptBlocks(out, p)
		return out, salt, nil
	default: // AES
		salt = make([]byte, 8)
		be32(salt[0:4], uint32(u.salt>>32))
		be32(salt[4:8], uint32(u.salt))
		blk, err := aes.NewCipher(u.privKey)
		if err != nil {
			return nil, nil, err
		}
		out := make([]byte, len(plain))
		cipher.NewCFBEncrypter(blk, u.aesIV(salt)).XORKeyStream(out, plain)
		return out, salt, nil
	}
}

func (u *usm) decrypt(ct, salt []byte, boots, tm int64) ([]byte, error) {
	if len(salt) != 8 {
		return nil, errors.New("snmp: bad privacy parameters")
	}
	switch strings.ToUpper(string(u.priv)) {
	case "DES":
		if len(ct)%8 != 0 {
			return nil, errors.New("snmp: bad DES data")
		}
		iv := make([]byte, 8)
		for i := range iv {
			iv[i] = u.privKey[8+i] ^ salt[i]
		}
		blk, err := des.NewCipher(u.privKey[:8])
		if err != nil {
			return nil, err
		}
		out := make([]byte, len(ct))
		cipher.NewCBCDecrypter(blk, iv).CryptBlocks(out, ct)
		return out, nil
	default:
		blk, err := aes.NewCipher(u.privKey)
		if err != nil {
			return nil, err
		}
		iv := make([]byte, 16)
		be32(iv[0:4], uint32(boots))
		be32(iv[4:8], uint32(tm))
		copy(iv[8:], salt)
		out := make([]byte, len(ct))
		cipher.NewCFBDecrypter(blk, iv).XORKeyStream(out, ct)
		return out, nil
	}
}

func (u *usm) aesIV(salt []byte) []byte {
	iv := make([]byte, 16)
	be32(iv[0:4], uint32(u.boots))
	be32(iv[4:8], uint32(u.time))
	copy(iv[8:], salt)
	return iv
}

func be32(b []byte, v uint32) { b[0], b[1], b[2], b[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v) }
