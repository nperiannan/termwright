// Package vault encrypts exported passwords and verifies the master password.
//
// Files are JSON: {"version":1,"kdf":"scrypt","n":..,"r":..,"p":..,"salt":..,"nonce":..,"data":..}
// with data = AES-256-GCM(scrypt(passphrase, salt), JSON payload).
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/scrypt"
)

const (
	scryptN = 1 << 15
	scryptR = 8
	scryptP = 1
)

type envelope struct {
	Version int    `json:"version"`
	KDF     string `json:"kdf"`
	N       int    `json:"n"`
	R       int    `json:"r"`
	P       int    `json:"p"`
	Salt    string `json:"salt"`
	Nonce   string `json:"nonce"`
	Data    string `json:"data"`
}

// ErrBadPassphrase is returned when decryption fails.
var ErrBadPassphrase = errors.New("wrong passphrase or damaged file")

func random(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// Seal encrypts payload (any JSON-encodable value) with a passphrase.
func Seal(passphrase string, payload any) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("passphrase must not be empty")
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	salt := random(16)
	key, err := scrypt.Key([]byte(passphrase), salt, scryptN, scryptR, scryptP, 32)
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := random(gcm.NonceSize())
	env := envelope{Version: 1, KDF: "scrypt", N: scryptN, R: scryptR, P: scryptP,
		Salt: base64.StdEncoding.EncodeToString(salt), Nonce: base64.StdEncoding.EncodeToString(nonce),
		Data: base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, plain, []byte("opencrt-vault-v1")))}
	return json.MarshalIndent(env, "", "  ")
}

// Open decrypts a file created by Seal into out.
func Open(passphrase string, data []byte, out any) error {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Version != 1 || env.KDF != "scrypt" {
		return errors.New("not a Termwright password vault file")
	}
	if env.N <= 1 || env.N > 1<<20 || env.R <= 0 || env.R > 32 || env.P <= 0 || env.P > 16 {
		return errors.New("unsupported vault parameters")
	}
	salt, err1 := base64.StdEncoding.DecodeString(env.Salt)
	nonce, err2 := base64.StdEncoding.DecodeString(env.Nonce)
	ct, err3 := base64.StdEncoding.DecodeString(env.Data)
	if err1 != nil || err2 != nil || err3 != nil {
		return ErrBadPassphrase
	}
	key, err := scrypt.Key([]byte(passphrase), salt, env.N, env.R, env.P, 32)
	if err != nil {
		return err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	if len(nonce) != gcm.NonceSize() {
		return ErrBadPassphrase
	}
	plain, err := gcm.Open(nil, nonce, ct, []byte("opencrt-vault-v1"))
	if err != nil {
		return ErrBadPassphrase
	}
	return json.Unmarshal(plain, out)
}

// HashPassword returns a verifier string "scrypt$N$r$p$salt$hash".
func HashPassword(pw string) (string, error) {
	salt := random(16)
	h, err := scrypt.Key([]byte(pw), salt, scryptN, scryptR, scryptP, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("scrypt$%d$%d$%d$%s$%s", scryptN, scryptR, scryptP,
		base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(h)), nil
}

// CheckPassword verifies pw against a HashPassword verifier.
func CheckPassword(pw, verifier string) bool {
	f := strings.Split(verifier, "$")
	if len(f) != 6 || f[0] != "scrypt" {
		return false
	}
	n, _ := strconv.Atoi(f[1])
	r, _ := strconv.Atoi(f[2])
	p, _ := strconv.Atoi(f[3])
	salt, err1 := base64.StdEncoding.DecodeString(f[4])
	want, err2 := base64.StdEncoding.DecodeString(f[5])
	if err1 != nil || err2 != nil || n <= 1 || n > 1<<20 || r <= 0 || p <= 0 {
		return false
	}
	got, err := scrypt.Key([]byte(pw), salt, n, r, p, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}
