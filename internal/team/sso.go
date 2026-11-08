package team

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Single sign-on: members join and stay members with their company account,
// through any OpenID Connect provider (Microsoft Entra ID, Okta, Google…).
// The team server is the relying party: the browser signs in at the provider
// and comes back to https://<server>/sso/callback; Termwright, which started
// the sign-in, collects the member's token with a secret only it knows.
// The invite still carries the server's address and certificate fingerprint,
// which is what makes Termwright trust the server.

// SSOConfig is the owner's single sign-on setup (in team.json).
type SSOConfig struct {
	Issuer       string   `json:"issuer"`                 // e.g. https://login.microsoftonline.com/<tenant id>/v2.0
	ClientID     string   `json:"clientId"`               // the app registration's (application) client id
	ClientSecret string   `json:"clientSecret,omitempty"` // "" = a public client with PKCE
	Label        string   `json:"label,omitempty"`        // the button: "Microsoft", "Okta", "Google"…
	Domains      []string `json:"domains,omitempty"`      // allowed e-mail domains (empty = any account the provider signs in)
	Groups       []string `json:"groups,omitempty"`       // allowed group ids (the "groups" claim; empty = any)
	InviteToo    bool     `json:"inviteToo"`              // members may also join with the invite key
	RenewDays    int      `json:"renewDays"`              // sign in again after this many days (0 = never)
}

// ssoPending is a sign-in that Termwright started and waits for.
type ssoPending struct {
	secret, nonce, verifier string
	name, user, machine     string
	member                  string // signing in again: the member it is for
	at                      time.Time
	done                    chan struct{}
	result                  *Joined
	err                     string
	test                    bool // the owner's test from the admin page
	claims                  map[string]any
}

const ssoPendingFor = 10 * time.Minute

// SSORenewMsg: a single sign-on member has to sign in again (401).
const SSORenewMsg = "sign in again with your company account (single sign-on)"

// NeedsSSO: the member has to sign in again with single sign-on.
func NeedsSSO(err error) bool {
	var a *APIError
	return errors.As(err, &a) && a.Status == http.StatusUnauthorized && a.Msg == SSORenewMsg
}

type oidcMeta struct {
	Issuer   string `json:"issuer"`
	Auth     string `json:"authorization_endpoint"`
	Token    string `json:"token_endpoint"`
	JWKS     string `json:"jwks_uri"`
	fetched  time.Time
	keys     map[string]*rsa.PublicKey
	keysTime time.Time
}

var (
	oidcMu    sync.Mutex
	oidcCache = map[string]*oidcMeta{}
	oidcHTTP  = &http.Client{Timeout: 20 * time.Second}
)

func (s *Server) ssoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/sso/start", s.ssoStart)
	mux.HandleFunc("POST /api/sso/claim", s.ssoClaim)
	mux.HandleFunc("GET /sso/login", s.ssoLogin)
	mux.HandleFunc("GET /sso/callback", s.ssoCallback)
	mux.HandleFunc("POST /api/admin/sso", s.auth(s.ssoSave))
	mux.HandleFunc("POST /api/admin/sso/test", s.auth(s.ssoTest))
}

// SSOStart is Termwright's request to sign in (join, or again as a member).
type SSOStart struct {
	Name    string `json:"name"`
	User    string `json:"user"`
	Machine string `json:"machine"`
	Member  string `json:"member,omitempty"` // signing in again
}

// ssoStart: a new sign-in; the answer has the page to open in the browser and
// the secret to collect the result with.
func (s *Server) ssoStart(w http.ResponseWriter, r *http.Request) {
	var b SSOStart
	_ = json.NewDecoder(r.Body).Decode(&b)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.SSO == nil || s.st.SSO.ClientID == "" {
		fail(w, 400, "this team does not use single sign-on")
		return
	}
	id, p := s.newPending()
	p.name, p.user, p.machine, p.member = clean(b.Name, 60), clean(b.User, 60), clean(b.Machine, 80), b.Member
	writeJSON(w, 200, map[string]string{"req": id, "secret": p.secret, "url": "https://" + r.Host + "/sso/login?req=" + url.QueryEscape(id)})
}

func (s *Server) newPending() (string, *ssoPending) {
	now := s.now()
	for k, p := range s.ssoReqs { // forget old ones
		if now.Sub(p.at) > ssoPendingFor {
			delete(s.ssoReqs, k)
		}
	}
	id := randToken(18)
	p := &ssoPending{secret: randToken(24), nonce: randToken(18), verifier: randToken(32), at: now, done: make(chan struct{})}
	s.ssoReqs[id] = p
	return id, p
}

// ssoLogin sends the browser to the provider.
func (s *Server) ssoLogin(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("req")
	s.mu.Lock()
	p, cfg := s.ssoReqs[id], s.st.SSO
	s.mu.Unlock()
	if p == nil || cfg == nil {
		ssoPage(w, 400, "This sign-in link is used or expired", "Start again from Termwright.")
		return
	}
	meta, err := oidcDiscover(cfg.Issuer, false)
	if err != nil {
		ssoPage(w, 502, "The sign-in service cannot be reached", err.Error())
		return
	}
	sum := sha256.Sum256([]byte(p.verifier))
	q := url.Values{
		"client_id": {cfg.ClientID}, "response_type": {"code"}, "scope": {"openid profile email"},
		"redirect_uri": {"https://" + r.Host + "/sso/callback"}, "state": {id}, "nonce": {p.nonce},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "response_mode": {"query"},
	}
	if p.member == "" {
		q.Set("prompt", "select_account")
	}
	sep := "?"
	if strings.Contains(meta.Auth, "?") {
		sep = "&"
	}
	http.Redirect(w, r, meta.Auth+sep+q.Encode(), http.StatusFound)
}

// ssoCallback: the provider sends the browser back with a code.
func (s *Server) ssoCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("state")
	s.mu.Lock()
	p, cfg := s.ssoReqs[id], s.st.SSO
	s.mu.Unlock()
	if p == nil || cfg == nil {
		ssoPage(w, 400, "This sign-in is used or expired", "Start again from Termwright.")
		return
	}
	finish := func(errText string) {
		s.mu.Lock()
		if p.result == nil && p.err == "" {
			p.err = errText
			close(p.done)
		}
		s.mu.Unlock()
	}
	if e := q.Get("error"); e != "" {
		msg := e + ": " + q.Get("error_description")
		finish(msg)
		ssoPage(w, 400, "The sign-in did not work", msg)
		return
	}
	claims, err := oidcExchange(cfg, q.Get("code"), "https://"+r.Host+"/sso/callback", p.verifier, p.nonce)
	if err != nil {
		finish(err.Error())
		ssoPage(w, 400, "The sign-in did not work", err.Error())
		return
	}
	email := claimStr(claims, "email", "preferred_username", "upn")
	if msg := ssoAllowed(cfg, email, claims); msg != "" {
		finish(msg)
		ssoPage(w, 403, "Not allowed in this team", msg)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.test {
		p.claims = claims
		close(p.done)
		ssoPage(w, 200, "The sign-in works", "Signed in as "+email+". Go back to the admin page.")
		return
	}
	sub := claimStr(claims, "oid", "sub") // Entra's object id stays the same across apps
	name := claimStr(claims, "name")
	if name == "" {
		name = p.name
	}
	now := s.now().UTC().Format(time.RFC3339)
	tok := randToken(32)
	var m *memberRec
	for _, x := range s.st.Members {
		if x.SSOSub != "" && x.SSOSub == sub && (p.member == "" || x.ID == p.member) {
			m = x
		}
	}
	if p.member != "" && m == nil {
		p.err = "this account is not the one this member joined with"
		close(p.done)
		ssoPage(w, 403, "Another account", "Sign in with the account you joined the team with.")
		return
	}
	if m == nil {
		m = &memberRec{Member: Member{ID: randToken(9), Name: clean(name, 60), User: p.user, Machine: p.machine, Role: RoleMember, Joined: now, LastSeen: now, Email: email, SSO: true}, SSOSub: sub}
		s.st.Members = append(s.st.Members, m)
		s.log(m.Name, "joined with single sign-on (%s)", email)
	} else {
		s.log(m.Name, "signed in again with single sign-on")
	}
	m.TokenHash, m.SSOAt, m.Email = hashToken(tok), now, email
	if p.user != "" {
		m.User, m.Machine = p.user, p.machine
	}
	if err := s.save(); err != nil {
		p.err = err.Error()
		close(p.done)
		ssoPage(w, 500, "The team server could not save", err.Error())
		return
	}
	p.result = &Joined{MemberID: m.ID, Token: tok, Role: m.Role, Team: s.st.Name}
	close(p.done)
	ssoPage(w, 200, "Signed in to "+s.st.Name, "You can close this page and go back to Termwright.")
}

// ssoClaim: Termwright collects the result of its sign-in (waits up to 50 s).
func (s *Server) ssoClaim(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Req    string `json:"req"`
		Secret string `json:"secret"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	s.mu.Lock()
	p := s.ssoReqs[b.Req]
	s.mu.Unlock()
	if p == nil || subtle.ConstantTimeCompare([]byte(p.secret), []byte(b.Secret)) != 1 {
		fail(w, 404, "no such sign-in (it may have expired - start again)")
		return
	}
	select {
	case <-p.done:
	case <-time.After(50 * time.Second):
		writeJSON(w, 202, map[string]bool{"waiting": true})
		return
	case <-r.Context().Done():
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ssoReqs, b.Req)
	if p.err != "" {
		fail(w, 403, p.err)
		return
	}
	if p.test {
		writeJSON(w, 200, p.claims)
		return
	}
	s.seen[p.result.MemberID] = s.now()
	writeJSON(w, 200, p.result)
}

// ssoSave: the owner sets single sign-on up (or off with an empty client id).
func (s *Server) ssoSave(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if me.Role != RoleOwner {
		fail(w, 403, "only the owner can set up single sign-on")
		return
	}
	var c SSOConfig
	if json.NewDecoder(r.Body).Decode(&c) != nil {
		fail(w, 400, "bad request")
		return
	}
	c.Issuer = strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
	c.ClientID = strings.TrimSpace(c.ClientID)
	if c.ClientID == "" {
		s.st.SSO = nil
		s.log(me.Name, "turned single sign-on off")
	} else {
		if !strings.HasPrefix(c.Issuer, "https://") {
			fail(w, 400, "the issuer is an https:// address, e.g. https://login.microsoftonline.com/<tenant id>/v2.0")
			return
		}
		if c.ClientSecret == "" && s.st.SSO != nil && s.st.SSO.ClientID == c.ClientID {
			c.ClientSecret = s.st.SSO.ClientSecret // kept unless typed again
		}
		var doms []string
		for _, d := range c.Domains {
			if d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@")); d != "" {
				doms = append(doms, d)
			}
		}
		c.Domains = doms
		s.st.SSO = &c
		s.log(me.Name, "set up single sign-on with %s", c.Issuer)
	}
	if err := s.save(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ssoTest: the owner tries the sign-in from the admin page; the claims come back.
func (s *Server) ssoTest(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if me.Role != RoleOwner || s.st.SSO == nil {
		fail(w, 403, "set single sign-on up first")
		return
	}
	id, p := s.newPending()
	p.test = true
	writeJSON(w, 200, map[string]string{"req": id, "secret": p.secret, "url": "/sso/login?req=" + url.QueryEscape(id)})
}

// ssoExpired: a single sign-on member who has to sign in again (mu held).
func (s *Server) ssoExpired(m *memberRec) bool {
	c := s.st.SSO
	if !m.SSO || c == nil || c.RenewDays <= 0 {
		return false
	}
	t, err := time.Parse(time.RFC3339, m.SSOAt)
	return err != nil || s.now().Sub(t) > time.Duration(c.RenewDays)*24*time.Hour
}

// ssoAllowed: "" or why the account may not be in the team.
func ssoAllowed(c *SSOConfig, email string, claims map[string]any) string {
	if len(c.Domains) > 0 {
		at := strings.LastIndex(email, "@")
		ok := false
		for _, d := range c.Domains {
			if at > 0 && strings.EqualFold(email[at+1:], d) {
				ok = true
			}
		}
		if !ok {
			return "Only accounts of " + strings.Join(c.Domains, ", ") + " may join this team (" + orUnknown(email) + ")."
		}
	}
	if len(c.Groups) > 0 {
		have := map[string]bool{}
		if gs, ok := claims["groups"].([]any); ok {
			for _, g := range gs {
				if v, ok := g.(string); ok {
					have[v] = true
				}
			}
		}
		for _, g := range c.Groups {
			if have[strings.TrimSpace(g)] {
				return ""
			}
		}
		return "Your account is not in a group allowed in this team (ask the owner, or IT to add the groups claim)."
	}
	return ""
}

func orUnknown(s string) string {
	if s == "" {
		return "no e-mail address in the sign-in"
	}
	return s
}

func claimStr(c map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := c[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// oidcDiscover reads the provider's configuration and keys (cached for an hour).
func oidcDiscover(issuer string, fresh bool) (*oidcMeta, error) {
	oidcMu.Lock()
	defer oidcMu.Unlock()
	if m := oidcCache[issuer]; m != nil && !fresh && time.Since(m.fetched) < time.Hour {
		return m, nil
	}
	var m oidcMeta
	if err := getJSON(strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", &m); err != nil {
		return nil, fmt.Errorf("the issuer's configuration: %w", err)
	}
	if strings.TrimRight(m.Issuer, "/") != strings.TrimRight(issuer, "/") {
		return nil, fmt.Errorf("the issuer is %s, not %s - use the tenant's own issuer address", m.Issuer, issuer)
	}
	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := getJSON(m.JWKS, &jwks); err != nil {
		return nil, fmt.Errorf("the issuer's keys: %w", err)
	}
	m.keys = map[string]*rsa.PublicKey{}
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil {
			continue
		}
		m.keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	m.fetched = time.Now()
	oidcCache[issuer] = &m
	return &m, nil
}

func getJSON(u string, out any) error {
	resp, err := oidcHTTP.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New(resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// oidcExchange redeems the code and checks the ID token: signature, issuer,
// audience, expiry and nonce. It returns the token's claims.
func oidcExchange(c *SSOConfig, code, redirect, verifier, nonce string) (map[string]any, error) {
	meta, err := oidcDiscover(c.Issuer, false)
	if err != nil {
		return nil, err
	}
	f := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "client_id": {c.ClientID}, "code_verifier": {verifier}}
	if c.ClientSecret != "" {
		f.Set("client_secret", c.ClientSecret)
	}
	resp, err := oidcHTTP.PostForm(meta.Token, f)
	if err != nil {
		return nil, fmt.Errorf("the sign-in service cannot be reached: %w", err)
	}
	defer resp.Body.Close()
	var tr struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
		Desc    string `json:"error_description"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tr)
	if tr.IDToken == "" {
		return nil, fmt.Errorf("the sign-in service gave no ID token: %s %s", tr.Error, tr.Desc)
	}
	return verifyIDToken(c, meta, tr.IDToken, nonce)
}

func verifyIDToken(c *SSOConfig, meta *oidcMeta, tok, nonce string) (map[string]any, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, errors.New("the ID token is damaged")
	}
	hb, err1 := base64.RawURLEncoding.DecodeString(parts[0])
	pb, err2 := base64.RawURLEncoding.DecodeString(parts[1])
	sig, err3 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, errors.New("the ID token is damaged")
	}
	var h struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(hb, &h) != nil || h.Alg != "RS256" {
		return nil, errors.New("the ID token is not signed with RS256")
	}
	key := meta.keys[h.Kid]
	if key == nil { // keys roll over: read them again once
		if m, err := oidcDiscover(c.Issuer, true); err == nil {
			key = m.keys[h.Kid]
		}
	}
	if key == nil {
		return nil, errors.New("the ID token is signed with an unknown key")
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return nil, errors.New("the ID token's signature is wrong")
	}
	var cl map[string]any
	if err := json.Unmarshal(pb, &cl); err != nil {
		return nil, errors.New("the ID token is damaged")
	}
	if iss, _ := cl["iss"].(string); strings.TrimRight(iss, "/") != strings.TrimRight(meta.Issuer, "/") {
		return nil, errors.New("the ID token is from another issuer")
	}
	audOK := false
	switch a := cl["aud"].(type) {
	case string:
		audOK = a == c.ClientID
	case []any:
		for _, x := range a {
			if x == c.ClientID {
				audOK = true
			}
		}
	}
	if !audOK {
		return nil, errors.New("the ID token is for another application")
	}
	exp, _ := cl["exp"].(float64)
	if time.Now().After(time.Unix(int64(exp), 0).Add(2 * time.Minute)) {
		return nil, errors.New("the ID token has expired - check this server's clock")
	}
	if n, _ := cl["nonce"].(string); n != nonce {
		return nil, errors.New("the ID token does not belong to this sign-in")
	}
	return cl, nil
}

// ssoPage is the page the browser shows at the end.
func ssoPage(w http.ResponseWriter, code int, title, text string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	ok := code == 200
	mark, col := "✕", "#dc2626"
	if ok {
		mark, col = "✓", "#16a34a"
	}
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title>
<style>body{font:15px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;display:grid;place-items:center;min-height:90vh;margin:0;background:#f4f5f7;color:#111}
@media(prefers-color-scheme:dark){body{background:#16181d;color:#eee}.c{background:#1f2229!important;border-color:#2c3039!important}}
.c{background:#fff;border:1px solid #e3e5ea;border-radius:14px;padding:28px 34px;max-width:460px;text-align:center}.m{font-size:34px;color:%s}h1{font-size:19px;margin:6px 0}p{opacity:.75;margin:0}</style></head>
<body><div class="c"><div class="m">%s</div><h1>%s</h1><p>%s</p><p style="margin-top:14px;font-size:12px">Termwright team server</p></div></body></html>`,
		html.EscapeString(title), col, mark, html.EscapeString(title), html.EscapeString(text))
}
