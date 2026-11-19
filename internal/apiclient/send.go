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
	"strconv"
	"strings"
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
}

// NewJar is a cookie jar for a session.
func NewJar() http.CookieJar { j, _ := cookiejar.New(nil); return j }

var varRe = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// Resolve replaces {{name}} with its value; {{$guid}}, {{$timestamp}}, {{$isoTimestamp}}, {{$randomInt}}
// are made each time. Unknown names stay as they are.
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
			switch k {
			case "$guid", "$randomUUID":
				return uuid()
			case "$timestamp":
				return strconv.FormatInt(time.Now().Unix(), 10)
			case "$isoTimestamp":
				return time.Now().UTC().Format(time.RFC3339)
			case "$randomInt":
				b := make([]byte, 2)
				_, _ = rand.Read(b)
				return strconv.Itoa(int(b[0])<<8 | int(b[1])) // 0..65535
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

func (t *Target) client() (*http.Client, error) {
	tc := &tls.Config{InsecureSkipVerify: t.Insecure} //nolint:gosec // the user's choice for lab devices
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
	to := t.Timeout
	if to <= 0 {
		to = 30 * time.Second
	}
	c := &http.Client{Transport: tr, Timeout: to, Jar: t.Jar}
	if t.NoRedirects {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
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
	case "json", "text", "xml":
		shown = Resolve(r.Body.Raw, t.Vars)
		body = strings.NewReader(shown)
		ctype = map[string]string{"json": "application/json", "text": "text/plain", "xml": "application/xml"}[r.Body.Mode]
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
	start := time.Now()
	res := Response{Method: strings.ToUpper(r.Method)}
	if res.Method == "" {
		res.Method = "GET"
	}
	c, err := t.client()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, shown, err := build(ctx, r, t)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.URL, res.ReqBody = t.mask(req.URL.String()), t.mask(shown)
		authed := false
		if r.Auth != "none" {
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
		if resp.StatusCode == http.StatusUnauthorized && authed && attempt == 0 && t.Auth.renews() {
			t.Tokens.Drop(t.TokenKey) // the token expired early: log in again, once
			continue
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
