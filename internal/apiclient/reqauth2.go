package apiclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"golang.org/x/crypto/md4"
)

// More of Postman's authentications: OAuth 2.0 in a browser (authorization code with PKCE, implicit),
// OAuth 1.0, Hawk and NTLM.

// ---- refresh tokens of OAuth 2.0 (memory only) ----

type refreshStore struct {
	mu sync.Mutex
	m  map[string]string
}

var refreshTokens = &refreshStore{m: map[string]string{}}

func (r *refreshStore) get(k string) string { r.mu.Lock(); defer r.mu.Unlock(); return r.m[k] }
func (r *refreshStore) set(k, v string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v == "" {
		delete(r.m, k)
	} else {
		r.m[k] = v
	}
}

func randText(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// BrowserWait is how long the browser sign-in may take.
var BrowserWait = 5 * time.Minute

// browserGrant signs in in the browser: Termwright listens on the redirect address (http://127.0.0.1 or
// localhost), opens the authorization URL, and takes the code (exchanged at the token URL, with PKCE's
// verifier) or, for the implicit grant, the token from the address's #fragment.
func browserGrant(ctx context.Context, a *ReqAuth, t *Target, c *http.Client, tu, id, sec, grant, key string) (string, time.Duration, error) {
	v := func(s string) string { return strings.TrimSpace(Resolve(s, t.Vars)) }
	au := v(a.AuthURL)
	if au == "" {
		return "", 0, errors.New("OAuth 2.0: the authorization URL is needed for this grant")
	}
	if t.Browse == nil {
		return "", 0, errors.New("OAuth 2.0: this grant signs in in a browser, which is not available here; get the token in the API Client, or use an access token")
	}
	redir := v(a.RedirectURI)
	if redir == "" {
		redir = "http://127.0.0.1:0/callback"
	}
	ru, err := url.Parse(redir)
	if err != nil || ru.Scheme != "http" || (ru.Hostname() != "127.0.0.1" && ru.Hostname() != "localhost" && ru.Hostname() != "::1") {
		return "", 0, fmt.Errorf("OAuth 2.0: the callback URL must be http://localhost:PORT/... or http://127.0.0.1:PORT/... so Termwright receives the answer (add it to the app at the provider); %q is not", redir)
	}
	port := ru.Port()
	if port == "" {
		port = "80"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(ru.Hostname(), port))
	if err != nil {
		return "", 0, fmt.Errorf("OAuth 2.0: cannot listen on the callback address %s: %w", ru.Host, err)
	}
	defer ln.Close()
	if port == "0" {
		ru.Host = net.JoinHostPort(ru.Hostname(), strconv.Itoa(ln.Addr().(*net.TCPAddr).Port))
	}
	if ru.Path == "" {
		ru.Path = "/"
	}
	redir = ru.String()
	state := v(a.State)
	if state == "" {
		state = randText(12)
	}
	q := url.Values{"client_id": {id}, "redirect_uri": {redir}, "state": {state}, "response_type": {"code"}}
	if grant == "implicit" {
		q.Set("response_type", "token")
	}
	if s := v(a.Scope); s != "" {
		q.Set("scope", s)
	}
	verifier := ""
	if grant == "authorization_code" && a.PKCE != "" {
		verifier = randText(48)
		ch := verifier
		if a.PKCE != "plain" {
			h := sha256.Sum256([]byte(verifier))
			ch = base64.RawURLEncoding.EncodeToString(h[:])
			q.Set("code_challenge_method", "S256")
		} else {
			q.Set("code_challenge_method", "plain")
		}
		q.Set("code_challenge", ch)
	}
	sep := "?"
	if strings.Contains(au, "?") {
		sep = "&"
	}
	authURL := au + sep + q.Encode()

	type answer struct {
		v   url.Values
		err error
	}
	got := make(chan answer, 1)
	page := func(w http.ResponseWriter, msg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Termwright</title><body style="font:15px system-ui;background:#0f1115;color:#e6e8eb;display:grid;place-items:center;height:90vh"><div>%s</div>`, html.EscapeString(msg))
	}
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ru.Path {
			http.NotFound(w, r)
			return
		}
		qv := r.URL.Query()
		if f := qv.Get("__fragment"); f != "" || qv.Has("__fragment") { // the implicit grant's #fragment, sent by the page below
			fv, _ := url.ParseQuery(f)
			qv = fv
		} else if qv.Get("code") == "" && qv.Get("error") == "" && qv.Get("access_token") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><script>location.replace(location.pathname+'?__fragment='+encodeURIComponent(location.hash.slice(1)))</script>`)
			return
		}
		select {
		case got <- answer{v: qv}:
		default:
		}
		if qv.Get("error") != "" {
			page(w, "Sign-in failed: "+qv.Get("error")+" "+qv.Get("error_description")+". You can close this window.")
			return
		}
		page(w, "Signed in. You can close this window and go back to Termwright.")
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	if err := t.Browse(authURL); err != nil {
		return "", 0, fmt.Errorf("OAuth 2.0: cannot open the browser: %w", err)
	}
	wait, cancel := context.WithTimeout(ctx, BrowserWait)
	defer cancel()
	var ans answer
	select {
	case ans = <-got:
	case <-wait.Done():
		return "", 0, errors.New("OAuth 2.0: the browser sign-in did not finish in time")
	}
	r := ans.v
	if e := r.Get("error"); e != "" {
		return "", 0, fmt.Errorf("OAuth 2.0: %s %s", e, r.Get("error_description"))
	}
	if r.Get("state") != state {
		return "", 0, errors.New("OAuth 2.0: the answer's state is not the one sent (a stale or forged answer)")
	}
	if grant == "implicit" {
		tok := r.Get("access_token")
		if tok == "" {
			return "", 0, errors.New("OAuth 2.0: the answer has no access_token")
		}
		return tok, tokenLife(r.Get("expires_in"), tok), nil
	}
	code := r.Get("code")
	if code == "" {
		return "", 0, errors.New("OAuth 2.0: the answer has no code")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redir}}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return tokenPost(ctx, c, a, tu, id, sec, form, key)
}

// ---- OAuth 1.0 (RFC 5849) ----

func oauthEsc(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '.' || ch == '_' || ch == '~' {
			b.WriteByte(ch)
		} else {
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

// oauth1Sign signs req; nonce and ts are made when empty (tests give them).
func oauth1Sign(req *http.Request, a *ReqAuth, t *Target, body []byte, nonce, ts string) error {
	v := func(s string) string { return strings.TrimSpace(Resolve(s, t.Vars)) }
	ck, cs, tok, tsec := v(a.ConsumerKey), Resolve(a.ConsumerSecret, t.Vars), v(a.Token), Resolve(a.TokenSecret, t.Vars)
	if ck == "" {
		return errors.New("OAuth 1.0: the consumer key is needed")
	}
	for _, s := range []string{cs, tsec} {
		if s != "" {
			t.Secrets = append(t.Secrets, s)
		}
	}
	m := strings.ToUpper(strings.TrimSpace(a.SignatureMethod))
	if m == "" {
		m = "HMAC-SHA1"
	}
	if nonce == "" {
		nonce = strings.NewReplacer("-", "", "_", "").Replace(randText(16))
	}
	if ts == "" {
		ts = strconv.FormatInt(time.Now().Unix(), 10)
	}
	op := [][2]string{{"oauth_consumer_key", ck}, {"oauth_nonce", nonce}, {"oauth_signature_method", m}, {"oauth_timestamp", ts}, {"oauth_version", "1.0"}}
	if tok != "" {
		op = append(op, [2]string{"oauth_token", tok})
	}
	var params [][2]string
	for k, vs := range req.URL.Query() {
		for _, x := range vs {
			params = append(params, [2]string{k, x})
		}
	}
	form := strings.HasPrefix(strings.ToLower(req.Header.Get("Content-Type")), "application/x-www-form-urlencoded")
	if form && len(body) > 0 {
		if fv, err := url.ParseQuery(string(body)); err == nil {
			for k, vs := range fv {
				for _, x := range vs {
					params = append(params, [2]string{k, x})
				}
			}
		}
	}
	params = append(params, op...)
	enc := make([]string, len(params))
	for i, p := range params {
		enc[i] = oauthEsc(p[0]) + "=" + oauthEsc(p[1])
	}
	sort.Strings(enc)
	u := *req.URL
	u.RawQuery, u.Fragment = "", ""
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	if u.Scheme == "http" && strings.HasSuffix(u.Host, ":80") || u.Scheme == "https" && strings.HasSuffix(u.Host, ":443") {
		u.Host = u.Host[:strings.LastIndex(u.Host, ":")]
	}
	base := req.Method + "&" + oauthEsc(u.String()) + "&" + oauthEsc(strings.Join(enc, "&"))
	keyText := oauthEsc(cs) + "&" + oauthEsc(tsec)
	var sig string
	switch m {
	case "PLAINTEXT":
		sig = keyText
	case "HMAC-SHA1", "HMAC-SHA256", "HMAC-SHA512":
		h := map[string]func() hash.Hash{"HMAC-SHA1": sha1.New, "HMAC-SHA256": sha256.New, "HMAC-SHA512": sha512.New}[m]
		mac := hmac.New(h, []byte(keyText))
		mac.Write([]byte(base))
		sig = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	default:
		return fmt.Errorf("OAuth 1.0: signature method %s is not supported (HMAC-SHA1, HMAC-SHA256, HMAC-SHA512, PLAINTEXT)", m)
	}
	op = append(op, [2]string{"oauth_signature", sig})
	switch a.In {
	case "query":
		q := req.URL.RawQuery
		for _, p := range op {
			if q != "" {
				q += "&"
			}
			q += oauthEsc(p[0]) + "=" + oauthEsc(p[1])
		}
		req.URL.RawQuery = q
	case "body":
		if !form && len(body) > 0 {
			return errors.New("OAuth 1.0: the parameters go in the body only with an x-www-form-urlencoded body")
		}
		nb := string(body)
		for _, p := range op {
			if nb != "" {
				nb += "&"
			}
			nb += oauthEsc(p[0]) + "=" + oauthEsc(p[1])
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		setBody(req, []byte(nb))
	default:
		parts := []string{}
		if r := v(a.Realm); r != "" {
			parts = append(parts, `realm="`+oauthEsc(r)+`"`)
		}
		for _, p := range op {
			parts = append(parts, p[0]+`="`+oauthEsc(p[1])+`"`)
		}
		req.Header.Set("Authorization", "OAuth "+strings.Join(parts, ", "))
	}
	return nil
}

func setBody(req *http.Request, b []byte) {
	req.Body = io.NopCloser(bytes.NewReader(b))
	req.ContentLength = int64(len(b))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
}

// ---- Hawk ----

// hawkHeader makes the Authorization: Hawk header; ts and nonce are made when empty (tests give them).
func hawkHeader(req *http.Request, a *ReqAuth, t *Target, body []byte, ts, nonce string) (string, error) {
	v := func(s string) string { return strings.TrimSpace(Resolve(s, t.Vars)) }
	id, key := v(a.HawkID), Resolve(a.HawkKey, t.Vars)
	if id == "" || key == "" {
		return "", errors.New("Hawk: the Hawk Auth ID and Key are needed")
	}
	t.Secrets = append(t.Secrets, key)
	newH := sha256.New
	if strings.EqualFold(a.Algorithm, "sha1") {
		newH = sha1.New
	}
	if ts == "" {
		ts = strconv.FormatInt(time.Now().Unix(), 10)
	}
	if nonce == "" {
		nonce = randText(6)
	}
	hashV := ""
	if a.Payload {
		ct := strings.ToLower(strings.TrimSpace(strings.Split(req.Header.Get("Content-Type"), ";")[0]))
		h := newH()
		h.Write([]byte("hawk.1.payload\n" + ct + "\n"))
		h.Write(body)
		h.Write([]byte("\n"))
		hashV = base64.StdEncoding.EncodeToString(h.Sum(nil))
	}
	port := req.URL.Port()
	if port == "" {
		port = "80"
		if req.URL.Scheme == "https" {
			port = "443"
		}
	}
	ext, app, dlg := v(a.Ext), v(a.App), v(a.Dlg)
	norm := "hawk.1.header\n" + ts + "\n" + nonce + "\n" + strings.ToUpper(req.Method) + "\n" + req.URL.RequestURI() + "\n" + strings.ToLower(req.URL.Hostname()) + "\n" + port + "\n" + hashV + "\n" + strings.NewReplacer(`\`, `\\`, "\n", `\n`).Replace(ext) + "\n"
	if app != "" {
		norm += app + "\n" + dlg + "\n"
	}
	mac := hmac.New(newH, []byte(key))
	mac.Write([]byte(norm))
	h := fmt.Sprintf(`Hawk id="%s", ts="%s", nonce="%s"`, id, ts, nonce)
	if hashV != "" {
		h += `, hash="` + hashV + `"`
	}
	if ext != "" {
		h += `, ext="` + strings.ReplaceAll(ext, `"`, `\"`) + `"`
	}
	h += `, mac="` + base64.StdEncoding.EncodeToString(mac.Sum(nil)) + `"`
	if app != "" {
		h += `, app="` + app + `"`
		if dlg != "" {
			h += `, dlg="` + dlg + `"`
		}
	}
	return h, nil
}

// ---- NTLM (NTLMv2): Negotiate, the server's Challenge on a 401, Authenticate on the same connection ----

const ntlmFlags = 0x00000001 | 0x00000002 | 0x00000004 | 0x00000200 | 0x00008000 | 0x00080000 | 0x00800000 | 0x20000000 | 0x80000000

func ntlmNegotiate() string {
	b := make([]byte, 32)
	copy(b, "NTLMSSP\x00")
	binary.LittleEndian.PutUint32(b[8:], 1)
	binary.LittleEndian.PutUint32(b[12:], ntlmFlags)
	binary.LittleEndian.PutUint32(b[20:], 32) // empty domain and workstation at the end
	binary.LittleEndian.PutUint32(b[28:], 32)
	return "NTLM " + base64.StdEncoding.EncodeToString(b)
}

// ntlmChallenge reads the server's Challenge from WWW-Authenticate: NTLM <base64> (nil when none).
func ntlmChallenge(h http.Header) []byte {
	for _, w := range h.Values("WWW-Authenticate") {
		for _, part := range strings.Split(w, ",") {
			f := strings.Fields(strings.TrimSpace(part))
			if len(f) == 2 && (strings.EqualFold(f[0], "NTLM") || strings.EqualFold(f[0], "Negotiate")) {
				if b, err := base64.StdEncoding.DecodeString(f[1]); err == nil && len(b) >= 32 && string(b[:8]) == "NTLMSSP\x00" && binary.LittleEndian.Uint32(b[8:]) == 2 {
					return b
				}
			}
		}
	}
	return nil
}

func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, x := range u {
		binary.LittleEndian.PutUint16(b[2*i:], x)
	}
	return b
}

func hmacMD5(key []byte, parts ...[]byte) []byte {
	m := hmac.New(md5.New, key)
	for _, p := range parts {
		m.Write(p)
	}
	return m.Sum(nil)
}

// ntowfv2 is NTOWFv2 of MS-NLMP: HMAC-MD5(MD4(UTF-16LE(password)), UTF-16LE(UPPER(user) + domain)).
func ntowfv2(user, pass, domain string) []byte {
	h := md4.New()
	h.Write(utf16le(pass))
	return hmacMD5(h.Sum(nil), utf16le(strings.ToUpper(user)+domain))
}

// ntlmAuthenticate answers a Challenge (clientChallenge and now are given by tests; random and now otherwise).
func ntlmAuthenticate(ch []byte, user, pass, domain, ws string, clientChallenge []byte, now time.Time) string {
	if i := strings.IndexAny(user, `\`); i > 0 && domain == "" { // DOMAIN\user
		domain, user = user[:i], user[i+1:]
	} else if i := strings.Index(user, "@"); i > 0 && domain == "" { // user@domain
		domain, user = user[i+1:], user[:i]
	}
	flags := binary.LittleEndian.Uint32(ch[20:]) & ntlmFlags
	server := ch[24:32]
	var info []byte
	if len(ch) >= 48 {
		l, off := int(binary.LittleEndian.Uint16(ch[40:])), int(binary.LittleEndian.Uint32(ch[44:]))
		if off+l <= len(ch) {
			info = ch[off : off+l]
		}
	}
	if clientChallenge == nil {
		clientChallenge = make([]byte, 8)
		_, _ = rand.Read(clientChallenge)
	}
	ft := uint64(now.UnixNano()/100) + 116444736000000000 // Windows FILETIME
	for i := 0; i+4 <= len(info); {                       // the server's timestamp, when it gave one
		id, l := binary.LittleEndian.Uint16(info[i:]), int(binary.LittleEndian.Uint16(info[i+2:]))
		if id == 0 || i+4+l > len(info) {
			break
		}
		if id == 7 && l == 8 {
			ft = binary.LittleEndian.Uint64(info[i+4:])
		}
		i += 4 + l
	}
	key := ntowfv2(user, pass, domain)
	temp := make([]byte, 28, 28+len(info)+4)
	temp[0], temp[1] = 1, 1
	binary.LittleEndian.PutUint64(temp[8:], ft)
	copy(temp[16:], clientChallenge)
	temp = append(temp, info...)
	temp = append(temp, 0, 0, 0, 0)
	proof := hmacMD5(key, server, temp)
	nt := append(append([]byte{}, proof...), temp...)
	lm := append(hmacMD5(key, server, clientChallenge), clientChallenge...)
	dom, usr, wks := utf16le(domain), utf16le(user), utf16le(ws)
	b := make([]byte, 64)
	copy(b, "NTLMSSP\x00")
	binary.LittleEndian.PutUint32(b[8:], 3)
	off := 64
	put := func(at int, data []byte) {
		binary.LittleEndian.PutUint16(b[at:], uint16(len(data)))
		binary.LittleEndian.PutUint16(b[at+2:], uint16(len(data)))
		binary.LittleEndian.PutUint32(b[at+4:], uint32(off))
		b = append(b, data...)
		off += len(data)
	}
	put(12, lm)
	put(20, nt)
	put(28, dom)
	put(36, usr)
	put(44, wks)
	put(52, nil)
	binary.LittleEndian.PutUint32(b[60:], flags)
	return "NTLM " + base64.StdEncoding.EncodeToString(b)
}
