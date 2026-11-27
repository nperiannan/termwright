package apiclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptrace"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MaxBody is how much of an answer is kept.
const MaxBody = 5 << 20

// Target is where requests go and how they log in (made from an API session).
type Target struct {
	// BaseURL goes before request paths and is {{baseUrl}}.
	BaseURL string
	// Auth of the session.
	Auth Auth
	// Insecure accepts any TLS certificate; CAFile is trusted as well.
	Insecure bool
	CAFile   string
	// Vars: {{name}} values (session, then collection, then the run's own).
	Vars map[string]string
	// Secrets: values to mask where the request is shown.
	Secrets []string
	// Timeout of one request (default 30 s).
	Timeout time.Duration
	// NoRedirects: answer 3xx instead of following.
	NoRedirects bool
	// Jar keeps cookies between requests (nil: none).
	Jar http.CookieJar
	// Tokens caches login and OAuth tokens (nil: none kept).
	Tokens *Tokens
	// TokenKey: the cache key (session path and environment).
	TokenKey string
	// CollAuth and CollAuthData: the collection's authentication for requests that inherit
	// ("" uses the session's).
	CollAuth     string
	CollAuthData *ReqAuth
	// CollPre and CollTests: the collection's scripts, run before each request's own.
	CollPre, CollTests string
	// Browse opens a URL in the browser (OAuth 2.0's authorization code and implicit grants); nil: none.
	Browse func(url string) error
	// ScriptSend says whether pm.sendRequest may send a request with this method (nil: any).
	ScriptSend func(method string) error
}

// NewJar is a cookie jar for a session that can list and remove its cookies.
func NewJar() *Jar { j, _ := cookiejar.New(nil); return &Jar{in: j, seen: map[string][]*url.URL{}} }

// Jar is a cookie jar that remembers the addresses it kept cookies for.
type Jar struct {
	mu   sync.Mutex
	in   http.CookieJar
	seen map[string][]*url.URL // host → addresses that set cookies
}

// SetCookies keeps cookies (http.CookieJar).
func (j *Jar) SetCookies(u *url.URL, cs []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.in.SetCookies(u, cs)
	c := *u
	c.RawQuery, c.Fragment = "", ""
	for _, x := range j.seen[u.Host] {
		if x.String() == c.String() {
			return
		}
	}
	j.seen[u.Host] = append(j.seen[u.Host], &c)
}

// Cookies to send (http.CookieJar).
func (j *Jar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.in.Cookies(u)
}

// HostCookies are the cookies kept for one host.
type HostCookies struct {
	Host    string   `json:"host"`    // the server
	Cookies []Cookie `json:"cookies"` // name and value of each
}

// List the cookies kept, by host.
func (j *Jar) List() []HostCookies {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []HostCookies
	for h, us := range j.seen {
		got := map[string]Cookie{}
		for _, u := range us {
			for _, c := range j.in.Cookies(u) {
				got[c.Name] = Cookie{Name: c.Name, Value: c.Value, Domain: h}
			}
		}
		if len(got) == 0 {
			continue
		}
		hc := HostCookies{Host: h}
		for _, c := range got {
			hc.Cookies = append(hc.Cookies, c)
		}
		sort.Slice(hc.Cookies, func(a, b int) bool { return hc.Cookies[a].Name < hc.Cookies[b].Name })
		out = append(out, hc)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Host < out[b].Host })
	return out
}

// Remove a cookie of a host ("" name: all of its cookies).
func (j *Jar) Remove(host, name string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, u := range j.seen[host] {
		paths := map[string]bool{"/": true, "": true}
		p := u.Path
		for p != "" && p != "/" {
			paths[p] = true
			p = p[:strings.LastIndex(p, "/")]
		}
		for _, c := range j.in.Cookies(u) {
			if name != "" && c.Name != name {
				continue
			}
			var del []*http.Cookie
			for p := range paths {
				del = append(del, &http.Cookie{Name: c.Name, Path: p, MaxAge: -1})
			}
			j.in.SetCookies(u, del)
		}
	}
}

// Clear removes every cookie.
func (j *Jar) Clear() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.in, _ = cookiejar.New(nil)
	j.seen = map[string][]*url.URL{}
}

var varRe = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// Resolve replaces {{name}} with its value; Postman's dynamic variables ({{$guid}}, {{$timestamp}},
// {{$randomEmail}} ...) are made each time. Unknown names stay as they are.
func Resolve(s string, vars map[string]string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	for i := 0; i < 5 && strings.Contains(s, "{{"); i++ { // a value may hold variables too
		n := varRe.ReplaceAllStringFunc(s, func(m string) string {
			k := strings.TrimSpace(m[2 : len(m)-2])
			if v, ok := vars[k]; ok {
				return v
			}
			if v, ok := dynamicVar(k); ok {
				return v
			}
			return m
		})
		if n == s {
			break
		}
		s = n
	}
	return s
}

func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// Unresolved lists the {{names}} that have no value.
func Unresolved(s string, vars map[string]string) []string {
	var out []string
	for _, m := range varRe.FindAllStringSubmatch(Resolve(s, vars), -1) {
		if !strings.HasPrefix(m[1], "$") {
			out = append(out, m[1])
		}
	}
	return out
}

// FullURL is the request's URL with its variables, the base URL and the query parameters.
func FullURL(r Request, t *Target) (string, error) {
	raw := strings.TrimSpace(Resolve(r.URL, t.Vars))
	if raw == "" {
		return "", errors.New("no URL")
	}
	if !strings.Contains(raw, "://") {
		base := strings.TrimRight(Resolve(t.BaseURL, t.Vars), "/")
		if base == "" {
			if strings.HasPrefix(raw, "/") {
				return "", errors.New("the URL is a path and the session has no base URL")
			}
			raw = "http://" + raw
		} else {
			raw = base + "/" + strings.TrimLeft(raw, "/")
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("no host in %s", raw)
	}
	if left := Unresolved(raw, t.Vars); len(left) > 0 {
		return "", fmt.Errorf("no value for {{%s}}", left[0])
	}
	if len(r.PathVars) > 0 {
		pv := map[string]string{}
		for _, p := range r.PathVars {
			if !p.Off && p.Key != "" {
				pv[p.Key] = Resolve(p.Value, t.Vars)
			}
		}
		segs := strings.Split(u.EscapedPath(), "/")
		for i, sg := range segs {
			if v, ok := pv[strings.TrimPrefix(sg, ":")]; ok && strings.HasPrefix(sg, ":") {
				segs[i] = url.PathEscape(v)
			}
		}
		if p, err := url.PathUnescape(strings.Join(segs, "/")); err == nil {
			u.Path, u.RawPath = p, strings.Join(segs, "/")
		}
	}
	noEnc := r.Settings != nil && r.Settings.NoEncode
	if noEnc {
		var extra []string
		for _, p := range r.Params {
			if !p.Off && p.Key != "" {
				extra = append(extra, Resolve(p.Key, t.Vars)+"="+Resolve(p.Value, t.Vars))
			}
		}
		if len(extra) > 0 {
			u.RawQuery = strings.Trim(u.RawQuery+"&"+strings.Join(extra, "&"), "&")
		}
		return u.String(), nil
	}
	q := u.Query()
	for _, p := range r.Params {
		if !p.Off && p.Key != "" {
			q.Add(Resolve(p.Key, t.Vars), Resolve(p.Value, t.Vars))
		}
	}
	if len(r.Params) > 0 {
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// PathVarNames lists the :name parts of a URL's path, in order.
func PathVarNames(rawURL string) []string {
	p := rawURL
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if i := strings.Index(p, "://"); i >= 0 {
		p = p[i+3:]
		if j := strings.Index(p, "/"); j >= 0 {
			p = p[j:]
		} else {
			p = ""
		}
	}
	var out []string
	for _, sg := range strings.Split(p, "/") {
		if len(sg) > 1 && sg[0] == ':' {
			out = append(out, sg[1:])
		}
	}
	return out
}

func (t *Target) client(r Request) (*http.Client, error) {
	st := Settings{}
	if r.Settings != nil {
		st = *r.Settings
	}
	insecure := t.Insecure
	switch st.Verify {
	case "on":
		insecure = false
	case "off":
		insecure = true
	}
	tc := &tls.Config{InsecureSkipVerify: insecure} //nolint:gosec // the user's choice for lab devices
	if t.CAFile != "" {
		pem, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("CA file: %w", err)
		}
		pool, _ := x509.SystemCertPool()
		if pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA file: no certificate in it")
		}
		tc.RootCAs = pool
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tc
	switch st.HTTPVersion {
	case "1.1":
		tr.ForceAttemptHTTP2 = false
		tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		tc.NextProtos = []string{"http/1.1"}
	case "2":
		tr.ForceAttemptHTTP2 = true
	}
	to := t.Timeout
	if st.TimeoutMs > 0 {
		to = time.Duration(st.TimeoutMs) * time.Millisecond
	}
	if to <= 0 {
		to = 30 * time.Second
	}
	c := &http.Client{Transport: tr, Timeout: to, Jar: t.Jar}
	if st.NoCookies {
		c.Jar = nil
	}
	maxR := st.MaxRedirects
	if maxR <= 0 {
		maxR = 10
	}
	noRed := t.NoRedirects || st.NoRedirects
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if noRed {
			return http.ErrUseLastResponse
		}
		if len(via) > maxR {
			return fmt.Errorf("stopped after %d redirects", maxR)
		}
		first := via[0]
		if st.FollowMethod && req.Method != first.Method {
			req.Method = first.Method
			if first.GetBody != nil {
				if b, err := first.GetBody(); err == nil {
					req.Body, req.GetBody, req.ContentLength = b, first.GetBody, first.ContentLength
					if ct := first.Header.Get("Content-Type"); ct != "" {
						req.Header.Set("Content-Type", ct)
					}
				}
			}
		}
		if st.FollowAuth && req.Header.Get("Authorization") == "" && first.Header.Get("Authorization") != "" {
			req.Header.Set("Authorization", first.Header.Get("Authorization"))
		}
		if st.RemoveReferer {
			req.Header.Del("Referer")
		}
		return nil
	}
	return c, nil
}

// build makes the HTTP request (without authentication).
func build(ctx context.Context, r Request, t *Target) (*http.Request, string, error) {
	u, err := FullURL(r, t)
	if err != nil {
		return nil, "", err
	}
	m := strings.ToUpper(strings.TrimSpace(r.Method))
	if m == "" {
		m = "GET"
	}
	var body io.Reader
	var shown, ctype string
	switch r.Body.Mode {
	case "json", "text", "xml", "html", "javascript":
		shown = Resolve(r.Body.Raw, t.Vars)
		body = strings.NewReader(shown)
		ctype = map[string]string{"json": "application/json", "text": "text/plain", "xml": "application/xml", "html": "text/html", "javascript": "application/javascript"}[r.Body.Mode]
	case "binary":
		f := strings.TrimSpace(Resolve(r.Body.File, t.Vars))
		if f == "" {
			return nil, "", errors.New("binary body: no file chosen")
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, "", fmt.Errorf("binary body: %w", err)
		}
		body, ctype, shown = bytes.NewReader(data), "application/octet-stream", fmt.Sprintf("(file %s, %d bytes)", f, len(data))
	case "graphql":
		var vars any
		if v := strings.TrimSpace(Resolve(r.Body.GraphQLVars, t.Vars)); v != "" {
			if err := json.Unmarshal([]byte(v), &vars); err != nil {
				return nil, "", fmt.Errorf("GraphQL variables: %w", err)
			}
		}
		b, _ := json.Marshal(map[string]any{"query": Resolve(r.Body.Raw, t.Vars), "variables": vars})
		shown, body, ctype = string(b), bytes.NewReader(b), "application/json"
	case "urlencoded":
		f := url.Values{}
		for _, kv := range r.Body.Form {
			if !kv.Off && kv.Key != "" {
				f.Add(Resolve(kv.Key, t.Vars), Resolve(kv.Value, t.Vars))
			}
		}
		shown = f.Encode()
		body, ctype = strings.NewReader(shown), "application/x-www-form-urlencoded"
	case "form":
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for _, kv := range r.Body.Form {
			if kv.Off || kv.Key == "" {
				continue
			}
			v := Resolve(kv.Value, t.Vars)
			if kv.Type == "file" && !strings.HasPrefix(v, "@") {
				v = "@" + v
			}
			if strings.HasPrefix(v, "@") { // @/path/to/file sends the file, as curl -F does
				data, err := os.ReadFile(v[1:])
				if err != nil {
					return nil, "", err
				}
				fw, _ := w.CreateFormFile(Resolve(kv.Key, t.Vars), v[1:])
				_, _ = fw.Write(data)
				continue
			}
			_ = w.WriteField(Resolve(kv.Key, t.Vars), v)
		}
		_ = w.Close()
		shown = buf.String()
		body, ctype = &buf, w.FormDataContentType()
	}
	req, err := http.NewRequestWithContext(ctx, m, u, body)
	if err != nil {
		return nil, "", err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("User-Agent", "Termwright")
	for _, h := range r.Headers {
		if !h.Off && h.Key != "" {
			req.Header.Set(Resolve(h.Key, t.Vars), Resolve(h.Value, t.Vars))
		}
	}
	if len(shown) > 64<<10 {
		shown = shown[:64<<10] + "…"
	}
	return req, shown, nil
}

// Send sends a request: its variables, the session's authentication (a login or OAuth token is fetched and
// renewed by itself; once more after a 401), cookies and timings. Scripts are run by Run, not here.
func Send(ctx context.Context, r Request, t *Target) Response {
	switch strings.ToUpper(r.Method) {
	case "GRPC":
		return sendGRPC(ctx, r, t)
	case "WS":
		return sendWS(ctx, r, t)
	}
	start := time.Now()
	res := Response{Method: strings.ToUpper(r.Method)}
	if res.Method == "" {
		res.Method = "GET"
	}
	c, err := t.client(r)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	var digest map[string]string // the server's Digest challenge, answered on the second try
	var ntlm []byte              // the server's NTLM challenge, answered on the second try (same connection)
	for attempt := 0; attempt < 2; attempt++ {
		req, shown, err := build(ctx, r, t)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.URL, res.ReqBody = t.mask(req.URL.String()), t.mask(shown)
		authed := false
		mode, adata := r.Auth, r.AuthData
		if mode == "" && t.CollAuth != "" {
			mode, adata = t.CollAuth, t.CollAuthData
		}
		switch mode {
		case "none":
		case "bearer", "basic", "apikey":
			if err := ownAuth(req, mode, adata, t); err != nil {
				res.Error = "authentication: " + err.Error()
				res.ReqHeaders = t.headers(req.Header)
				return res
			}
		case "digest", "oauth2", "awsv4", "oauth1", "hawk", "ntlm":
			a := adata
			if a == nil {
				a = &ReqAuth{}
			}
			var aerr error
			switch mode {
			case "digest":
				if digest != nil {
					u, p := strings.TrimSpace(Resolve(a.User, t.Vars)), Resolve(a.Password, t.Vars)
					req.Header.Set("Authorization", digestHeader(digest, req, u, p, requestBody(req)))
					if p != "" {
						t.Secrets = append(t.Secrets, p)
					}
				}
			case "oauth2":
				var tok string
				if tok, aerr = oauth2Token(ctx, a, t, c, attempt > 0); aerr == nil {
					req.Header.Set("Authorization", "Bearer "+tok)
					t.Secrets = append(t.Secrets, tok)
					authed = true
				}
			case "awsv4":
				aerr = awsSign(req, a, t, requestBody(req), time.Now())
			case "oauth1":
				aerr = oauth1Sign(req, a, t, requestBody(req), "", "")
			case "hawk":
				var h string
				if h, aerr = hawkHeader(req, a, t, requestBody(req), "", ""); aerr == nil {
					req.Header.Set("Authorization", h)
				}
			case "ntlm":
				u, p := strings.TrimSpace(Resolve(a.User, t.Vars)), Resolve(a.Password, t.Vars)
				if p != "" {
					t.Secrets = append(t.Secrets, p)
				}
				if ntlm == nil {
					req.Header.Set("Authorization", ntlmNegotiate())
				} else {
					req.Header.Set("Authorization", ntlmAuthenticate(ntlm, u, p, strings.TrimSpace(Resolve(a.Domain, t.Vars)), strings.TrimSpace(Resolve(a.Workstation, t.Vars)), nil, time.Now()))
				}
			}
			if aerr != nil {
				res.Error = "authentication: " + aerr.Error()
				res.ReqHeaders = t.headers(req.Header)
				return res
			}
		default:
			if authed, err = t.Auth.apply(ctx, req, t, c, attempt > 0); err != nil {
				res.Error = "authentication: " + err.Error()
				res.ReqHeaders = t.headers(req.Header)
				return res
			}
		}
		res.ReqHeaders = t.headers(req.Header)
		var tm Timing
		var dnsAt, conAt, tlsAt time.Time
		sent := time.Now()
		trace := &httptrace.ClientTrace{
			DNSStart:          func(httptrace.DNSStartInfo) { dnsAt = time.Now() },
			DNSDone:           func(httptrace.DNSDoneInfo) { tm.DNS = ms(time.Since(dnsAt)) },
			ConnectStart:      func(string, string) { conAt = time.Now() },
			ConnectDone:       func(string, string, error) { tm.Connect = ms(time.Since(conAt)) },
			TLSHandshakeStart: func() { tlsAt = time.Now() },
			TLSHandshakeDone:  func(tls.ConnectionState, error) { tm.TLS = ms(time.Since(tlsAt)) },
			GotFirstResponseByte: func() {
				tm.FirstByte = ms(time.Since(sent))
			},
		}
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		resp, err := c.Do(req)
		if err != nil {
			res.Error = t.mask(err.Error())
			res.Ms = time.Since(start).Milliseconds()
			return res
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
		resp.Body.Close()
		tm.Total = ms(time.Since(sent))
		if resp.StatusCode == http.StatusUnauthorized && authed && attempt == 0 && t.Auth.renews() && mode != "oauth2" {
			t.Tokens.Drop(t.TokenKey) // the token expired early: log in again, once
			continue
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			if mode == "ntlm" && ntlm == nil {
				if ntlm = ntlmChallenge(resp.Header); ntlm != nil {
					continue // answer the challenge on the same connection
				}
			}
			if mode == "digest" && digest == nil {
				if digest = digestChallenge(resp.Header); digest != nil {
					continue // answer the challenge
				}
			}
			if mode == "oauth2" && authed && (adata == nil || strings.TrimSpace(Resolve(adata.Token, t.Vars)) == "") {
				continue // the token ran out early: a new one, once
			}
		}
		res.Status, res.StatusText, res.Proto, res.Timing = resp.StatusCode, resp.Status, resp.Proto, tm
		if err != nil {
			res.Error = err.Error()
		}
		if len(data) > MaxBody {
			data, res.Truncated = data[:MaxBody], true
		}
		res.Size = len(data)
		res.ContentType = resp.Header.Get("Content-Type")
		if utf8.Valid(data) {
			res.Body = string(data)
		} else {
			res.Body, res.Base64 = base64.StdEncoding.EncodeToString(data), true
		}
		res.Headers = sortedKV(resp.Header)
		for _, ck := range resp.Cookies() {
			co := Cookie{Name: ck.Name, Value: ck.Value, Domain: ck.Domain, Path: ck.Path, HTTPOnly: ck.HttpOnly, Secure: ck.Secure}
			if !ck.Expires.IsZero() {
				co.Expires = ck.Expires.UTC().Format(time.RFC3339)
			}
			res.Cookies = append(res.Cookies, co)
		}
		break
	}
	res.Ms = time.Since(start).Milliseconds()
	return res
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func sortedKV(h http.Header) []KV {
	var out []KV
	for k, vs := range h {
		for _, v := range vs {
			out = append(out, KV{Key: k, Value: v})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

var secretHeaders = map[string]bool{"Authorization": true, "Proxy-Authorization": true, "Cookie": true, "X-Auth-Token": true, "X-Api-Key": true}

// headers as shown: authorization and secrets masked.
func (t *Target) headers(h http.Header) []KV {
	out := sortedKV(h)
	for i, kv := range out {
		if secretHeaders[kv.Key] || (t.Auth.Type == "apikey" || t.Auth.Type == "login") && strings.EqualFold(kv.Key, t.Auth.KeyName) {
			if sp := strings.IndexByte(kv.Value, ' '); sp > 0 && sp < 10 {
				out[i].Value = kv.Value[:sp+1] + "••••••••"
			} else {
				out[i].Value = "••••••••"
			}
			continue
		}
		out[i].Value = t.mask(kv.Value)
	}
	return out
}

func (t *Target) mask(s string) string {
	for _, sec := range append(t.Secrets, t.Auth.Secret) {
		if len(sec) >= 3 {
			s = strings.ReplaceAll(s, sec, "••••••••")
			s = strings.ReplaceAll(s, url.QueryEscape(sec), "••••••••")
		}
	}
	return s
}

// ownAuth applies a request's (or collection's) own authentication: a bearer token, basic or an API key.
func ownAuth(req *http.Request, mode string, a *ReqAuth, t *Target) error {
	if a == nil {
		a = &ReqAuth{}
	}
	v := func(s string) string { return strings.TrimSpace(Resolve(s, t.Vars)) }
	switch mode {
	case "bearer":
		tok := v(a.Token)
		if tok == "" || strings.Contains(tok, "{{") {
			return errors.New("the bearer token is empty or its variable has no value")
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		t.Secrets = append(t.Secrets, tok)
	case "basic":
		u, p := v(a.User), v(a.Password)
		if u == "" {
			return errors.New("basic: no user name")
		}
		req.SetBasicAuth(u, p)
		if p != "" {
			t.Secrets = append(t.Secrets, p)
		}
	case "apikey":
		k, val := v(a.Key), v(a.Value)
		if k == "" || val == "" {
			return errors.New("API key: the key name or value is empty")
		}
		if a.In == "query" { // appended: the other parameters stay as typed (and in their order)
			if req.URL.RawQuery != "" {
				req.URL.RawQuery += "&"
			}
			req.URL.RawQuery += url.QueryEscape(k) + "=" + url.QueryEscape(val)
		} else {
			req.Header.Set(k, val)
		}
		t.Secrets = append(t.Secrets, val)
	}
	return nil
}
