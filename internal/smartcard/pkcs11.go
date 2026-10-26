//go:build cgo

package smartcard

import (
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strings"
	"sync"

	"github.com/miekg/pkcs11"
	"golang.org/x/crypto/ssh"
)

// Available reports whether this build can use smart cards.
const Available = true

// Manager keeps the loaded PKCS#11 libraries and the cards' login state.
type Manager struct {
	mu   sync.Mutex
	mods map[string]*module
	// EachTime forgets the PIN after every signature.
	EachTime func() bool
}

type module struct {
	ctx    *pkcs11.Ctx
	mu     sync.Mutex
	tokens map[uint]*token
}

type token struct {
	mu       sync.Mutex
	sess     pkcs11.SessionHandle
	open     bool
	loggedIn bool
	pin      string
	info     pkcs11.TokenInfo
}

// NewManager creates a manager.
func NewManager() *Manager { return &Manager{mods: map[string]*module{}} }

func (m *Manager) module(lib string) (*module, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if md := m.mods[lib]; md != nil {
		return md, nil
	}
	ctx := pkcs11.New(lib)
	if ctx == nil {
		return nil, fmt.Errorf("cannot load the PKCS#11 library %s", lib)
	}
	if err := ctx.Initialize(); err != nil && !isErr(err, pkcs11.CKR_CRYPTOKI_ALREADY_INITIALIZED) {
		ctx.Destroy()
		return nil, fmt.Errorf("%s: %w", lib, err)
	}
	md := &module{ctx: ctx, tokens: map[uint]*token{}}
	m.mods[lib] = md
	return md, nil
}

func (md *module) token(slot uint) *token {
	md.mu.Lock()
	defer md.mu.Unlock()
	t := md.tokens[slot]
	if t == nil {
		t = &token{}
		md.tokens[slot] = t
	}
	return t
}

// session opens the token's session (call with t.mu held).
func (t *token) session(ctx *pkcs11.Ctx, slot uint) error {
	if t.open {
		return nil
	}
	info, err := ctx.GetTokenInfo(slot)
	if err != nil {
		return err
	}
	sh, err := ctx.OpenSession(slot, pkcs11.CKF_SERIAL_SESSION)
	if err != nil {
		return err
	}
	t.sess, t.open, t.info, t.loggedIn = sh, true, info, false
	return nil
}

func (t *token) reset(ctx *pkcs11.Ctx) {
	if t.open {
		_ = ctx.CloseSession(t.sess)
	}
	t.open, t.loggedIn = false, false
}

func isErr(err error, codes ...uint) bool {
	var pe pkcs11.Error
	if !errors.As(err, &pe) {
		return false
	}
	for _, c := range codes {
		if uint(pe) == c {
			return true
		}
	}
	return false
}

// gone: the card was removed or the session was lost.
func gone(err error) bool {
	return isErr(err, pkcs11.CKR_SESSION_HANDLE_INVALID, pkcs11.CKR_SESSION_CLOSED, pkcs11.CKR_DEVICE_REMOVED,
		pkcs11.CKR_TOKEN_NOT_PRESENT, pkcs11.CKR_USER_NOT_LOGGED_IN, pkcs11.CKR_DEVICE_ERROR, pkcs11.CKR_SLOT_ID_INVALID)
}

// List lists the cards and keys of each library.
func (m *Manager) List(libs []string) []Report {
	var out []Report
	for _, lib := range libs {
		r := Report{Library: lib, Tokens: []string{}, Keys: []Key{}}
		keys, tokens, err := m.keys(lib)
		if err != nil {
			r.Error = err.Error()
		}
		r.Keys, r.Tokens = append(r.Keys, keys...), append(r.Tokens, tokens...)
		out = append(out, r)
	}
	return out
}

func (m *Manager) keys(lib string) ([]Key, []string, error) {
	md, err := m.module(lib)
	if err != nil {
		return nil, nil, err
	}
	slots, err := md.ctx.GetSlotList(true)
	if err != nil {
		return nil, nil, err
	}
	var keys []Key
	var tokens, errs []string
	for _, slot := range slots {
		if ti, err := md.ctx.GetTokenInfo(slot); err != nil || ti.Flags&pkcs11.CKF_TOKEN_INITIALIZED == 0 {
			continue // empty reader slot or a blank card
		}
		t := md.token(slot)
		t.mu.Lock()
		ks, label, err := t.keys(md.ctx, slot, lib)
		if err != nil && gone(err) {
			t.reset(md.ctx)
			ks, label, err = t.keys(md.ctx, slot, lib)
		}
		t.mu.Unlock()
		if err != nil {
			errs = append(errs, fmt.Sprintf("card in slot %d: %v", slot, err))
			continue
		}
		tokens = append(tokens, label)
		keys = append(keys, ks...)
	}
	if len(tokens) == 0 && len(errs) > 0 {
		return nil, nil, errors.New(strings.Join(errs, "; "))
	}
	return keys, tokens, nil
}

func (t *token) keys(ctx *pkcs11.Ctx, slot uint, lib string) ([]Key, string, error) {
	if err := t.session(ctx, slot); err != nil {
		return nil, "", err
	}
	label := strings.TrimSpace(t.info.Label)
	serial := strings.TrimSpace(t.info.SerialNumber)
	byID := map[string]*Key{}
	get := func(id []byte) *Key {
		h := hex.EncodeToString(id)
		k := byID[h]
		if k == nil {
			k = &Key{Library: lib, Token: label, Serial: serial, Slot: slot, ID: h}
			byID[h] = k
		}
		return k
	}
	pubs, err := find(ctx, t.sess, []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PUBLIC_KEY)})
	if err != nil {
		return nil, label, err
	}
	for _, o := range pubs {
		id, lbl := idLabel(ctx, t.sess, o)
		pub, err := publicKey(ctx, t.sess, o)
		if err != nil || pub == nil {
			continue
		}
		k := get(id)
		k.pub = pub
		if lbl != "" {
			k.Label = lbl
		}
	}
	certs, err := find(ctx, t.sess, []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_CERTIFICATE)})
	if err == nil {
		for _, o := range certs {
			id, lbl := idLabel(ctx, t.sess, o)
			a, err := ctx.GetAttributeValue(t.sess, o, []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_VALUE, nil)})
			if err != nil || len(a) == 0 {
				continue
			}
			c, err := x509.ParseCertificate(a[0].Value)
			if err != nil {
				continue
			}
			k := get(id)
			if k.pub == nil {
				k.pub = c.PublicKey
			}
			if k.Label == "" {
				k.Label = lbl
			}
			k.Subject = c.Subject.CommonName
		}
	}
	var out []Key
	for _, k := range byID {
		if k.pub == nil {
			continue
		}
		sp, err := ssh.NewPublicKey(k.pub)
		if err != nil {
			continue // a key type SSH cannot use
		}
		switch p := k.pub.(type) {
		case *rsa.PublicKey:
			k.Bits = p.N.BitLen()
		case *ecdsa.PublicKey:
			k.Bits = p.Curve.Params().BitSize
		}
		describe(k, sp)
		out = append(out, *k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, label, nil
}

func find(ctx *pkcs11.Ctx, sh pkcs11.SessionHandle, tmpl []*pkcs11.Attribute) ([]pkcs11.ObjectHandle, error) {
	if err := ctx.FindObjectsInit(sh, tmpl); err != nil {
		return nil, err
	}
	defer func() { _ = ctx.FindObjectsFinal(sh) }()
	var out []pkcs11.ObjectHandle
	for {
		os, _, err := ctx.FindObjects(sh, 64)
		if err != nil {
			return out, err
		}
		if len(os) == 0 {
			return out, nil
		}
		out = append(out, os...)
	}
}

func idLabel(ctx *pkcs11.Ctx, sh pkcs11.SessionHandle, o pkcs11.ObjectHandle) ([]byte, string) {
	var id []byte
	var label string
	if a, err := ctx.GetAttributeValue(sh, o, []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_ID, nil)}); err == nil && len(a) > 0 {
		id = a[0].Value
	}
	if a, err := ctx.GetAttributeValue(sh, o, []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_LABEL, nil)}); err == nil && len(a) > 0 {
		label = strings.TrimSpace(string(a[0].Value))
	}
	return id, label
}

var (
	oidP256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 7}
	oidP384 = asn1.ObjectIdentifier{1, 3, 132, 0, 34}
	oidP521 = asn1.ObjectIdentifier{1, 3, 132, 0, 35}
)

func publicKey(ctx *pkcs11.Ctx, sh pkcs11.SessionHandle, o pkcs11.ObjectHandle) (crypto.PublicKey, error) {
	a, err := ctx.GetAttributeValue(sh, o, []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_KEY_TYPE, nil)})
	if err != nil || len(a) == 0 {
		return nil, err
	}
	switch kt := bytesToUint(a[0].Value); kt {
	case pkcs11.CKK_RSA:
		a, err := ctx.GetAttributeValue(sh, o, []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_MODULUS, nil), pkcs11.NewAttribute(pkcs11.CKA_PUBLIC_EXPONENT, nil)})
		if err != nil || len(a) < 2 {
			return nil, err
		}
		e := new(big.Int).SetBytes(a[1].Value)
		if !e.IsInt64() || e.Int64() > 1<<31 {
			return nil, errors.New("bad RSA exponent")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(a[0].Value), E: int(e.Int64())}, nil
	case pkcs11.CKK_EC:
		a, err := ctx.GetAttributeValue(sh, o, []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_EC_PARAMS, nil), pkcs11.NewAttribute(pkcs11.CKA_EC_POINT, nil)})
		if err != nil || len(a) < 2 {
			return nil, err
		}
		return ecPublicKey(a[0].Value, a[1].Value)
	default:
		return nil, nil
	}
}

func ecPublicKey(params, point []byte) (crypto.PublicKey, error) {
	var oid asn1.ObjectIdentifier
	if _, err := asn1.Unmarshal(params, &oid); err != nil {
		return nil, err
	}
	var curve elliptic.Curve
	var dh ecdh.Curve
	switch {
	case oid.Equal(oidP256):
		curve, dh = elliptic.P256(), ecdh.P256()
	case oid.Equal(oidP384):
		curve, dh = elliptic.P384(), ecdh.P384()
	case oid.Equal(oidP521):
		curve, dh = elliptic.P521(), ecdh.P521()
	default:
		return nil, nil // other curves are not used by SSH
	}
	size := (curve.Params().BitSize + 7) / 8
	raw := point
	if len(raw) != 1+2*size || raw[0] != 4 {
		var inner []byte
		if _, err := asn1.Unmarshal(point, &inner); err != nil {
			return nil, err
		}
		raw = inner
	}
	if _, err := dh.NewPublicKey(raw); err != nil { // checks the point is on the curve
		return nil, err
	}
	return &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(raw[1 : 1+size]), Y: new(big.Int).SetBytes(raw[1+size:])}, nil
}

func bytesToUint(b []byte) uint {
	// CK_ULONG in host byte order (little endian on the supported platforms)
	var v uint
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | uint(b[i])
	}
	return v
}

// Signers returns SSH signers for the keys that match want ("" / "any" = all,
// or a SHA256 fingerprint). The PIN is asked (pin) at the first signature.
func (m *Manager) Signers(libs []string, want string, pin PinFunc) ([]ssh.Signer, error) {
	var out []ssh.Signer
	var errs []string
	for _, r := range m.List(libs) {
		if r.Error != "" {
			errs = append(errs, r.Error)
		}
		for _, k := range r.Keys {
			if !k.Matches(want) {
				continue
			}
			id, _ := hex.DecodeString(k.ID)
			s, err := ssh.NewSignerFromSigner(&signer{m: m, key: k, id: id, pin: pin})
			if err == nil {
				out = append(out, s)
			}
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return out, nil
}

// Forget logs out of every card (the PINs are asked again).
func (m *Manager) Forget() {
	m.mu.Lock()
	mods := make([]*module, 0, len(m.mods))
	for _, md := range m.mods {
		mods = append(mods, md)
	}
	m.mu.Unlock()
	for _, md := range mods {
		md.mu.Lock()
		ts := make([]*token, 0, len(md.tokens))
		for _, t := range md.tokens {
			ts = append(ts, t)
		}
		md.mu.Unlock()
		for _, t := range ts {
			t.mu.Lock()
			if t.open && t.loggedIn {
				_ = md.ctx.Logout(t.sess)
			}
			t.loggedIn, t.pin = false, ""
			t.mu.Unlock()
		}
	}
}

// LoggedIn reports how many cards have their PIN entered.
func (m *Manager) LoggedIn() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, md := range m.mods {
		md.mu.Lock()
		for _, t := range md.tokens {
			if t.loggedIn {
				n++
			}
		}
		md.mu.Unlock()
	}
	return n
}

// ErrPinNeeded: no PIN could be asked (background runs).
var ErrPinNeeded = errors.New("smart card PIN needed")

type signer struct {
	m   *Manager
	key Key
	id  []byte
	pin PinFunc
}

func (s *signer) Public() crypto.PublicKey { return s.key.pub }

var digestInfo = map[crypto.Hash][]byte{
	crypto.SHA1:   {0x30, 0x21, 0x30, 0x09, 0x06, 0x05, 0x2b, 0x0e, 0x03, 0x02, 0x1a, 0x05, 0x00, 0x04, 0x14},
	crypto.SHA256: {0x30, 0x31, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01, 0x05, 0x00, 0x04, 0x20},
	crypto.SHA384: {0x30, 0x41, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x02, 0x05, 0x00, 0x04, 0x30},
	crypto.SHA512: {0x30, 0x51, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x03, 0x05, 0x00, 0x04, 0x40},
}

// Sign signs a digest on the card (crypto.Signer).
func (s *signer) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	var mech uint
	data := digest
	switch s.key.pub.(type) {
	case *rsa.PublicKey:
		if _, pss := opts.(*rsa.PSSOptions); pss {
			return nil, errors.New("RSA-PSS is not supported for smart card keys")
		}
		prefix, ok := digestInfo[opts.HashFunc()]
		if !ok {
			return nil, fmt.Errorf("hash %v is not supported", opts.HashFunc())
		}
		mech, data = pkcs11.CKM_RSA_PKCS, append(append([]byte(nil), prefix...), digest...)
	case *ecdsa.PublicKey:
		mech = pkcs11.CKM_ECDSA
	default:
		return nil, errors.New("unsupported key type")
	}
	md, err := s.m.module(s.key.Library)
	if err != nil {
		return nil, err
	}
	var sig []byte
	for attempt := 0; attempt < 2; attempt++ {
		slot, ok := s.slot(md)
		if !ok {
			return nil, fmt.Errorf("insert the smart card %s", s.key.Token)
		}
		t := md.token(slot)
		t.mu.Lock()
		sig, err = s.signOn(md.ctx, t, slot, mech, data)
		if err != nil && gone(err) && attempt == 0 {
			t.reset(md.ctx)
			t.mu.Unlock()
			continue
		}
		t.mu.Unlock()
		break
	}
	if err != nil {
		return nil, err
	}
	if mech == pkcs11.CKM_ECDSA {
		if len(sig)%2 != 0 {
			return nil, errors.New("bad ECDSA signature from the card")
		}
		n := len(sig) / 2
		return asn1.Marshal(struct{ R, S *big.Int }{new(big.Int).SetBytes(sig[:n]), new(big.Int).SetBytes(sig[n:])})
	}
	return sig, nil
}

// slot finds the card again by its serial number (slots change when cards
// are removed and inserted).
func (s *signer) slot(md *module) (uint, bool) {
	slots, err := md.ctx.GetSlotList(true)
	if err != nil {
		return 0, false
	}
	for _, sl := range slots {
		if sl == s.key.Slot {
			if ti, err := md.ctx.GetTokenInfo(sl); err == nil && strings.TrimSpace(ti.SerialNumber) == s.key.Serial {
				return sl, true
			}
		}
	}
	for _, sl := range slots {
		if ti, err := md.ctx.GetTokenInfo(sl); err == nil && strings.TrimSpace(ti.SerialNumber) == s.key.Serial {
			return sl, true
		}
	}
	return 0, false
}

func (s *signer) signOn(ctx *pkcs11.Ctx, t *token, slot uint, mech uint, data []byte) ([]byte, error) {
	if err := t.session(ctx, slot); err != nil {
		return nil, err
	}
	if err := s.login(ctx, t, pkcs11.CKU_USER); err != nil {
		return nil, err
	}
	objs, err := find(ctx, t.sess, []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PRIVATE_KEY), pkcs11.NewAttribute(pkcs11.CKA_ID, s.id)})
	if err != nil {
		return nil, err
	}
	if len(objs) == 0 {
		return nil, fmt.Errorf("the private key %s is not on the card %s", s.key.Label, s.key.Token)
	}
	if err := ctx.SignInit(t.sess, []*pkcs11.Mechanism{pkcs11.NewMechanism(mech, nil)}, objs[0]); err != nil {
		return nil, err
	}
	// keys that need the PIN for every signature (e.g. PIV slot 9c)
	if a, err := ctx.GetAttributeValue(t.sess, objs[0], []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_ALWAYS_AUTHENTICATE, nil)}); err == nil &&
		len(a) > 0 && len(a[0].Value) > 0 && a[0].Value[0] != 0 {
		if err := s.contextLogin(ctx, t); err != nil {
			_, _ = ctx.Sign(t.sess, data) // ends the operation
			return nil, err
		}
	}
	sig, err := ctx.Sign(t.sess, data)
	if err == nil && s.m.EachTime != nil && s.m.EachTime() {
		_ = ctx.Logout(t.sess)
		t.loggedIn, t.pin = false, ""
	}
	return sig, err
}

func (s *signer) askPin(t *token, retry bool) (string, error) {
	if s.pin == nil {
		return "", ErrPinNeeded
	}
	return s.pin(s.key, retry)
}

func (s *signer) login(ctx *pkcs11.Ctx, t *token, user uint) error {
	if t.loggedIn {
		return nil
	}
	if t.info.Flags&pkcs11.CKF_PROTECTED_AUTHENTICATION_PATH != 0 { // PIN pad on the reader
		err := ctx.Login(t.sess, user, "")
		if err == nil || isErr(err, pkcs11.CKR_USER_ALREADY_LOGGED_IN) {
			t.loggedIn = true
			return nil
		}
		return err
	}
	retry := false
	for i := 0; i < 3; i++ {
		pin, err := s.askPin(t, retry)
		if err != nil {
			return err
		}
		err = ctx.Login(t.sess, user, pin)
		switch {
		case err == nil, isErr(err, pkcs11.CKR_USER_ALREADY_LOGGED_IN):
			t.loggedIn = true
			if s.m.EachTime == nil || !s.m.EachTime() {
				t.pin = pin
			}
			return nil
		case isErr(err, pkcs11.CKR_PIN_INCORRECT, pkcs11.CKR_PIN_LEN_RANGE):
			retry = true
		case isErr(err, pkcs11.CKR_PIN_LOCKED):
			return fmt.Errorf("the PIN of %s is locked (too many wrong PINs): unblock it with the card's PUK", s.key.Token)
		default:
			return err
		}
	}
	return fmt.Errorf("wrong PIN for %s", s.key.Token)
}

func (s *signer) contextLogin(ctx *pkcs11.Ctx, t *token) error {
	pin := t.pin
	if pin == "" {
		var err error
		if pin, err = s.askPin(t, false); err != nil {
			return err
		}
	}
	return ctx.Login(t.sess, pkcs11.CKU_CONTEXT_SPECIFIC, pin)
}
