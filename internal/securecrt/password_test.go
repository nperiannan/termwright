package securecrt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func encryptV3(t *testing.T, pw, passphrase string) string {
	salt := []byte("0123456789abcdef")
	kdf, err := bcryptPBKDF([]byte(passphrase), salt, 16, 48)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(pw))
	plain := binary.LittleEndian.AppendUint32(nil, uint32(len(pw)))
	plain = append(append(plain, pw...), sum[:]...)
	for len(plain)%aes.BlockSize != 0 {
		plain = append(plain, 0)
	}
	block, _ := aes.NewCipher(kdf[:32])
	cipher.NewCBCEncrypter(block, kdf[32:]).CryptBlocks(plain, plain)
	return "03:" + hex.EncodeToString(append(salt, plain...))
}

func TestSavedPassword(t *testing.T) {
	for _, pw := range []string{"s3cret!", "", "pässwörd with spaces", "x"} {
		f, _ := Parse([]byte("S:\"Hostname\"=h\r\nS:\"Password V2\"=" + EncryptV2(pw, "") + "\r\n"))
		got, ok, err := SavedPassword(f, "")
		if pw == "" {
			if err != nil || got != "" {
				t.Fatalf("empty: %q %v %v", got, ok, err)
			}
			continue
		}
		if !ok || err != nil || got != pw {
			t.Fatalf("v2 %q: %q %v %v", pw, got, ok, err)
		}
		if got, err := DecryptV1(EncryptV1(pw)); err != nil || got != pw {
			t.Fatalf("v1 %q: %q %v", pw, got, err)
		}
	}
	// a configuration passphrase
	v := EncryptV2("pw1", "config pass")
	if _, err := DecryptV2(v, ""); err != ErrNeedPassphrase {
		t.Fatalf("missing passphrase: %v", err)
	}
	if _, err := DecryptV2(v, "wrong"); err == nil || err == ErrNeedPassphrase {
		t.Fatalf("wrong passphrase: %v", err)
	}
	if got, err := DecryptV2(encryptV3(t, "no-pass", ""), ""); err != nil || got != "no-pass" {
		t.Fatalf("03 without passphrase: %q %v", got, err)
	}
	if got, err := DecryptV2(v, "config pass"); err != nil || got != "pw1" {
		t.Fatalf("%q %v", got, err)
	}
	v3 := encryptV3(t, "pw3", "config pass")
	if _, err := DecryptV2(v3, ""); err != ErrNeedPassphrase {
		t.Fatalf("03 without passphrase: %v", err)
	}
	if got, err := DecryptV2(v3, "config pass"); err != nil || got != "pw3" {
		t.Fatalf("03: %q %v", got, err)
	}
	// old style only
	f, _ := Parse([]byte("S:\"Password\"=" + EncryptV1("old") + "\r\n"))
	if got, ok, err := SavedPassword(f, ""); !ok || err != nil || got != "old" {
		t.Fatalf("v1 file: %q %v %v", got, ok, err)
	}
	// plain text (exported without password encryption)
	f, _ = Parse([]byte("S:\"Password V2\"=Plain Pass 1\r\n"))
	if got, ok, err := SavedPassword(f, ""); !ok || err != nil || got != "Plain Pass 1" {
		t.Fatalf("plain: %q %v %v", got, ok, err)
	}
	// no password
	f, _ = Parse([]byte("S:\"Hostname\"=h\r\n"))
	if _, ok, err := SavedPassword(f, ""); ok || err != nil {
		t.Fatal("no password expected")
	}
}
