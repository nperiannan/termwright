package apiclient

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"hash"

	"github.com/dop251/goja"
)

// scriptHelpers gives the script sandbox btoa/atob, uuid and the hashes CryptoJS's shim needs (all in Go).
func scriptHelpers(vm *goja.Runtime) {
	_ = vm.Set("__dyn", func(k string) any { // Postman's dynamic variables in replaceIn
		if v, ok := dynamicVar(k); ok {
			return v
		}
		return nil
	})
	raw := func(m map[string]any) []byte {
		if h, ok := m["hex"].(string); ok {
			b, _ := hex.DecodeString(h)
			return b
		}
		s, _ := m["text"].(string)
		return []byte(s)
	}
	newHash := func(a string) func() hash.Hash {
		switch a {
		case "MD5":
			return md5.New
		case "SHA1":
			return sha1.New
		case "SHA512":
			return sha512.New
		}
		return sha256.New
	}
	_ = vm.Set("__b64", func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) })
	_ = vm.Set("__unb64", func(s string) string {
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			b, _ = base64.RawStdEncoding.DecodeString(s)
		}
		return string(b)
	})
	_ = vm.Set("__uuid", uuid)
	_ = vm.Set("__hash", func(a string, m map[string]any) string {
		h := newHash(a)()
		h.Write(raw(m))
		return hex.EncodeToString(h.Sum(nil))
	})
	_ = vm.Set("__hmac", func(a string, m, k map[string]any) string {
		h := hmac.New(newHash(a), raw(k))
		h.Write(raw(m))
		return hex.EncodeToString(h.Sum(nil))
	})
	_ = vm.Set("__hex2b64", func(h string) string { b, _ := hex.DecodeString(h); return base64.StdEncoding.EncodeToString(b) })
	_ = vm.Set("__b642hex", func(s string) string { b, _ := base64.StdEncoding.DecodeString(s); return hex.EncodeToString(b) })
	_ = vm.Set("__hex2text", func(h string) string { b, _ := hex.DecodeString(h); return string(b) })
	_ = vm.Set("__text2hex", func(s string) string { return hex.EncodeToString([]byte(s)) })
}
