package team

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeIdP is an OpenID Connect provider like Entra ID: discovery, keys, an
// authorize endpoint that signs the user in at once, and a token endpoint
// that checks PKCE.
type fakeIdP struct {
	srv   *httptest.Server
	key   *rsa.PrivateKey
	mu    sync.Mutex
	codes map[string][2]string // code → nonce, challenge
	email string
	sub   string
	bad   bool // sign with another key
}

func newIdP(t *testing.T) *fakeIdP {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	f := &fakeIdP{key: k, codes: map[string][2]string{}, email: "anisha@example.com", sub: "oid-anisha"}
	mux := http.NewServeMux()
	f.srv = httptest.NewTLSServer(mux)
	iss := f.srv.URL + "/tenant/v2.0"
	mux.HandleFunc("/tenant/v2.0/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": iss, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token", "jwks_uri": f.srv.URL + "/keys"})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "k1",
			"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes())}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code := randToken(12)
		f.mu.Lock()
		f.codes[code] = [2]string{q.Get("nonce"), q.Get("code_challenge")}
		f.mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?code="+code+"&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		c, ok := f.codes[r.Form.Get("code")]
		delete(f.codes, r.Form.Get("code"))
		f.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != c[1] {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "PKCE"})
			return
		}
		claims := map[string]any{"iss": iss, "aud": r.Form.Get("client_id"), "exp": time.Now().Add(time.Hour).Unix(), "nonce": c[0],
			"oid": f.sub, "email": f.email, "name": "Anisha R", "groups": []string{"g-lab"}}
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": f.sign(claims)})
	})
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIdP) sign(claims map[string]any) string {
	h, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	p, _ := json.Marshal(claims)
	in := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	sum := sha256.Sum256([]byte(in))
	k := f.key
	if f.bad {
		k, _ = rsa.GenerateKey(rand.Reader, 2048)
	}
	sig, _ := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, sum[:])
	return in + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// browse follows the sign-in like a browser and returns the last page's status.
func browse(t *testing.T, u string) int {
	t.Helper()
	b := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // test servers
	resp, err := b.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestSingleSignOn(t *testing.T) {
	idp := newIdP(t)
	oidcHTTP = idp.srv.Client()
	defer func() { oidcHTTP = &http.Client{Timeout: 20 * time.Second} }()
	s, code, ts := start(t)
	ctx := context.Background()
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	own, _ := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "Natarajan"})
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	tm, _ := oc.Team(ctx)
	cfg := SSOConfig{Issuer: idp.srv.URL + "/tenant/v2.0", ClientID: "app-1", Label: "Microsoft", Domains: []string{"@Example.com"}, Groups: []string{"g-lab"}, RenewDays: 30}
	if err := oc.call(ctx, "POST", "/api/admin/sso", cfg, nil); err != nil {
		t.Fatal(err)
	}
	if info, _ := anon.Info(ctx); info.SSO != "Microsoft" || info.InviteToo {
		t.Fatalf("info %+v", info)
	}
	if _, err := anon.Join(ctx, JoinRequest{Invite: tm.Invite, Name: "x"}); err == nil || !strings.Contains(err.Error(), "single sign-on") {
		t.Fatalf("an invite join got in although single sign-on only: %v", err)
	}

	// join with the company account
	join := func(st SSOStart) (Joined, error) {
		u, req, sec, err := anon.SSOBegin(ctx, st)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		var j Joined
		var jerr error
		go func() {
			defer close(done)
			for {
				x, waiting, err := anon.SSOCollect(ctx, req, sec)
				if !waiting {
					j, jerr = x, err
					return
				}
			}
		}()
		browse(t, u)
		<-done
		return j, jerr
	}
	j, err := join(SSOStart{Name: "ignored", User: "anisha", Machine: "AN-PC"})
	if err != nil {
		t.Fatal(err)
	}
	ac := NewClient(addr(ts), s.Fingerprint(), j.Token)
	team, err := ac.Team(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var me Member
	for _, m := range team.Members {
		if m.ID == j.MemberID {
			me = m
		}
	}
	if me.Name != "Anisha R" || me.Email != "anisha@example.com" || !me.SSO || me.Role != RoleMember {
		t.Fatalf("member %+v", me)
	}
	// a wrong secret collects nothing
	if _, _, err := anon.SSOCollect(ctx, "nope", "nope"); err == nil {
		t.Fatal("collected a sign-in without its secret")
	}
	// another domain is refused
	idp.email = "someone@gmail.com"
	if _, err := join(SSOStart{Name: "x"}); err == nil || !strings.Contains(err.Error(), "example.com") {
		t.Fatalf("an outside account got in: %v", err)
	}
	// a token signed with another key is refused
	idp.email, idp.bad = "anisha@example.com", true
	if _, err := join(SSOStart{Name: "x"}); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("a forged token got in: %v", err)
	}
	idp.bad = false

	// after 30 days: sign in again
	s.mu.Lock()
	s.now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	s.mu.Unlock()
	if _, err := ac.Team(ctx); !NeedsSSO(err) {
		t.Fatalf("no sign-in asked after 31 days: %v", err)
	}
	j2, err := join(SSOStart{Member: j.MemberID, User: "anisha", Machine: "AN-PC"})
	if err != nil || j2.MemberID != j.MemberID {
		t.Fatalf("signing in again: %+v %v", j2, err)
	}
	if _, err := NewClient(addr(ts), s.Fingerprint(), j2.Token).Team(ctx); err != nil {
		t.Fatalf("after signing in again: %v", err)
	}
	if _, err := ac.Team(ctx); err == nil {
		t.Fatal("the old token still works")
	}
	// another account cannot take over the member
	idp.sub = "oid-mallory"
	if _, err := join(SSOStart{Member: j.MemberID}); err == nil {
		t.Fatal("another account signed in as the member")
	}
}
