package sshkey

import (
	"bufio"
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/dsa"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ssh"
)

// PuTTY private key files (.ppk), formats 2 and 3, are read directly so
// keys made with PuTTYgen work without converting them.

// IsPPK reports whether data looks like a PuTTY private key file.
func IsPPK(data []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(data, "\ufeff \r\n\t"), []byte("PuTTY-User-Key-File-"))
}

// ErrBadPassphrase is returned when a .ppk passphrase is wrong (MAC mismatch).
var ErrBadPassphrase = errors.New("wrong passphrase for the PuTTY key (or the file is damaged)")

type ppkFile struct {
	version            int
	alg, enc, comment  string
	pub, priv          []byte
	mac                []byte
	kdf                string
	mem, passes, paral uint32
	salt               []byte
}

func parsePPKFile(data []byte) (*ppkFile, error) {
	p := &ppkFile{}
	sc := bufio.NewScanner(bytes.NewReader(bytes.TrimLeft(data, "\ufeff")))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	readBlock := func(n int) ([]byte, error) {
		var b strings.Builder
		for i := 0; i < n; i++ {
			if !sc.Scan() {
				return nil, errors.New("ppk: truncated key data")
			}
			b.WriteString(strings.TrimSpace(sc.Text()))
		}
		return base64.StdEncoding.DecodeString(b.String())
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		var err error
		switch k {
		case "PuTTY-User-Key-File-2", "PuTTY-User-Key-File-3":
			p.version = int(k[len(k)-1] - '0')
			p.alg = v
		case "Encryption":
			p.enc = v
		case "Comment":
			p.comment = v
		case "Public-Lines", "Private-Lines":
			n, cerr := strconv.Atoi(v)
			if cerr != nil || n < 0 || n > 10000 {
				return nil, fmt.Errorf("ppk: bad %s", k)
			}
			if k == "Public-Lines" {
				p.pub, err = readBlock(n)
			} else {
				p.priv, err = readBlock(n)
			}
		case "Private-MAC":
			p.mac, err = hex.DecodeString(v)
		case "Key-Derivation":
			p.kdf = v
		case "Argon2-Memory", "Argon2-Passes", "Argon2-Parallelism":
			n, cerr := strconv.ParseUint(v, 10, 32)
			if cerr != nil {
				return nil, fmt.Errorf("ppk: bad %s", k)
			}
			switch k {
			case "Argon2-Memory":
				p.mem = uint32(n)
			case "Argon2-Passes":
				p.passes = uint32(n)
			default:
				p.paral = uint32(n)
			}
		case "Argon2-Salt":
			p.salt, err = hex.DecodeString(v)
		case "Private-Hash":
			return nil, errors.New("ppk: format 1 keys are not supported - re-save the key with PuTTYgen")
		}
		if err != nil {
			return nil, fmt.Errorf("ppk: %s: %w", k, err)
		}
	}
	if p.version == 0 || p.pub == nil || p.priv == nil || p.mac == nil {
		return nil, errors.New("ppk: not a complete PuTTY key file")
	}
	if p.enc != "none" && p.enc != "aes256-cbc" {
		return nil, fmt.Errorf("ppk: unsupported encryption %q", p.enc)
	}
	return p, nil
}

func sshString(b []byte) []byte {
	out := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	copy(out[4:], b)
	return out
}

// decrypt checks the MAC and returns the private blob.
func (p *ppkFile) decrypt(passphrase string) ([]byte, error) {
	priv := p.priv
	var macKey []byte
	var cipherKey, iv []byte
	if p.version == 2 {
		h := sha1.Sum([]byte("putty-private-key-file-mac-key" + passphrase))
		macKey = h[:]
		if p.enc != "none" {
			a := sha1.Sum(append([]byte{0, 0, 0, 0}, passphrase...))
			b := sha1.Sum(append([]byte{0, 0, 0, 1}, passphrase...))
			cipherKey, iv = append(a[:], b[:]...)[:32], make([]byte, 16)
		} else {
			// unencrypted v2 keys use the empty passphrase for the MAC
			h := sha1.Sum([]byte("putty-private-key-file-mac-key"))
			macKey = h[:]
		}
	} else if p.enc != "none" {
		var k []byte
		switch p.kdf {
		case "Argon2id":
			k = argon2.IDKey([]byte(passphrase), p.salt, p.passes, p.mem, uint8(p.paral), 80)
		case "Argon2i":
			k = argon2.Key([]byte(passphrase), p.salt, p.passes, p.mem, uint8(p.paral), 80)
		default:
			return nil, fmt.Errorf("ppk: key derivation %q is not supported - re-save the key with Argon2id in PuTTYgen", p.kdf)
		}
		cipherKey, iv, macKey = k[:32], k[32:48], k[48:]
	}
	if cipherKey != nil {
		if len(priv)%aes.BlockSize != 0 {
			return nil, errors.New("ppk: bad encrypted data length")
		}
		blk, err := aes.NewCipher(cipherKey)
		if err != nil {
			return nil, err
		}
		dec := make([]byte, len(priv))
		cipher.NewCBCDecrypter(blk, iv).CryptBlocks(dec, priv)
		priv = dec
	}
	data := bytes.Join([][]byte{sshString([]byte(p.alg)), sshString([]byte(p.enc)), sshString([]byte(p.comment)),
		sshString(p.pub), sshString(priv)}, nil)
	var mac []byte
	if p.version == 2 {
		m := hmac.New(sha1.New, macKey)
		m.Write(data)
		mac = m.Sum(nil)
	} else {
		m := hmac.New(sha256.New, macKey)
		m.Write(data)
		mac = m.Sum(nil)
	}
	if subtle.ConstantTimeCompare(mac, p.mac) != 1 {
		if p.enc == "none" {
			return nil, errors.New("ppk: key file is damaged (MAC mismatch)")
		}
		return nil, ErrBadPassphrase
	}
	return priv, nil
}

type reader struct {
	b   []byte
	err error
}

func (r *reader) str() []byte {
	if r.err != nil || len(r.b) < 4 {
		r.err = errors.New("ppk: truncated key blob")
		return nil
	}
	n := binary.BigEndian.Uint32(r.b)
	if uint64(n) > uint64(len(r.b)-4) {
		r.err = errors.New("ppk: truncated key blob")
		return nil
	}
	s := r.b[4 : 4+n]
	r.b = r.b[4+n:]
	return s
}

func (r *reader) mpint() *big.Int { return new(big.Int).SetBytes(r.str()) }

func (p *ppkFile) privateKey(priv []byte) (crypto.PrivateKey, error) {
	pub := &reader{b: p.pub}
	sec := &reader{b: priv}
	if alg := string(pub.str()); alg != p.alg {
		return nil, errors.New("ppk: key type mismatch")
	}
	var key crypto.PrivateKey
	switch {
	case p.alg == "ssh-rsa":
		e, n := pub.mpint(), pub.mpint()
		d, pp, q, _ := sec.mpint(), sec.mpint(), sec.mpint(), sec.mpint()
		k := &rsa.PrivateKey{PublicKey: rsa.PublicKey{N: n, E: int(e.Int64())}, D: d, Primes: []*big.Int{pp, q}}
		if pub.err == nil && sec.err == nil {
			if err := k.Validate(); err != nil {
				return nil, fmt.Errorf("ppk: %w", err)
			}
			k.Precompute()
		}
		key = k
	case p.alg == "ssh-ed25519":
		pk, seed := pub.str(), sec.str()
		if pub.err == nil && sec.err == nil {
			if len(seed) != ed25519.SeedSize {
				return nil, errors.New("ppk: bad ed25519 key")
			}
			k := ed25519.NewKeyFromSeed(seed)
			if !bytes.Equal(k.Public().(ed25519.PublicKey), pk) {
				return nil, errors.New("ppk: ed25519 public key mismatch")
			}
			key = k
		}
	case strings.HasPrefix(p.alg, "ecdsa-sha2-"):
		_, point, d := pub.str(), pub.str(), sec.mpint()
		var curve elliptic.Curve
		var ec ecdh.Curve
		switch p.alg {
		case "ecdsa-sha2-nistp256":
			curve, ec = elliptic.P256(), ecdh.P256()
		case "ecdsa-sha2-nistp384":
			curve, ec = elliptic.P384(), ecdh.P384()
		case "ecdsa-sha2-nistp521":
			curve, ec = elliptic.P521(), ecdh.P521()
		}
		if pub.err == nil && sec.err == nil {
			if _, err := ec.NewPublicKey(point); err != nil {
				return nil, fmt.Errorf("ppk: %w", err)
			}
			x, y := elliptic.Unmarshal(curve, point) //nolint:staticcheck // point validated above
			k := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y}, D: d}
			key = k
		}
	case p.alg == "ssh-dss":
		pp, q, g, y := pub.mpint(), pub.mpint(), pub.mpint(), pub.mpint()
		x := sec.mpint()
		key = &dsa.PrivateKey{PublicKey: dsa.PublicKey{Parameters: dsa.Parameters{P: pp, Q: q, G: g}, Y: y}, X: x}
	default:
		return nil, fmt.Errorf("ppk: key type %q is not supported", p.alg)
	}
	if pub.err != nil {
		return nil, pub.err
	}
	if sec.err != nil {
		return nil, sec.err
	}
	return key, nil
}

// ParsePPK reads a PuTTY .ppk key. For an encrypted key without a passphrase
// it returns *ssh.PassphraseMissingError (with the public key), like
// ssh.ParsePrivateKey does for OpenSSH keys.
func ParsePPK(data []byte, passphrase string) (ssh.Signer, error) {
	p, err := parsePPKFile(data)
	if err != nil {
		return nil, err
	}
	if p.enc != "none" && passphrase == "" {
		pk, _ := ssh.ParsePublicKey(p.pub)
		return nil, &ssh.PassphraseMissingError{PublicKey: pk}
	}
	priv, err := p.decrypt(passphrase)
	if err != nil {
		return nil, err
	}
	key, err := p.privateKey(priv)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(key)
}

// PPKPublicKey returns the public key and comment of a .ppk file (no passphrase needed).
func PPKPublicKey(data []byte) (ssh.PublicKey, string, error) {
	p, err := parsePPKFile(data)
	if err != nil {
		return nil, "", err
	}
	pk, err := ssh.ParsePublicKey(p.pub)
	return pk, p.comment, err
}

// ParseKey reads an OpenSSH/PEM private key or a PuTTY .ppk key. An empty
// passphrase on an encrypted key yields *ssh.PassphraseMissingError.
func ParseKey(data []byte, passphrase string) (ssh.Signer, error) {
	if IsPPK(data) {
		return ParsePPK(data, passphrase)
	}
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
	}
	return ssh.ParsePrivateKey(data)
}
