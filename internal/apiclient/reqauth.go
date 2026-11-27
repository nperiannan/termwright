package apiclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Request-level authentications beyond bearer, basic and API key: Digest, OAuth 2.0 (client credentials and
// password grants) and AWS Signature v4 - the ones Postman offers that REST APIs of devices and clouds use.

// ---- Digest (RFC 7616): the server's 401 challenge is answered on a second try ----

// digestChallenge reads a WWW-Authenticate: Digest header ("" fields when there is none).
func digestChallenge(h http.Header) map[string]string {
	for _, v := range h.Values("Www-Authenticate") {
		if len(v) > 7 && strings.EqualFold(v[:7], "digest ") {
			out := map[string]string{}
			for _, p := range splitParams(v[7:]) {
				k, val, _ := strings.Cut(p, "=")
				out[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(val), `"`)
			}
			return out
		}
	}
	return nil
}

// splitParams splits a=b, c="d,e" on the commas outside quotes.
func splitParams(s string) []string {
	var out []string
	q, start := false, 0
	for i, c := range s {
		switch c {
		case '"':
			q = !q
		case ',':
			if !q {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// digestHeader answers a Digest challenge for req (MD5 or SHA-256, qop auth).
func digestHeader(ch map[string]string, req *http.Request, user, pass string, body []byte) string {
	alg := strings.ToUpper(ch["algorithm"])
	h := func(s string) string {
		if strings.HasPrefix(alg, "SHA-256") {
			x := sha256.Sum256([]byte(s))
			return hex.EncodeToString(x[:])
		}
		x := md5.Sum([]byte(s))
		return hex.EncodeToString(x[:])
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	cnonce, nc := hex.EncodeToString(b), "00000001"
	uri := req.URL.RequestURI()
	ha1 := h(user + ":" + ch["realm"] + ":" + pass)
	if strings.HasSuffix(alg, "-SESS") {
		ha1 = h(ha1 + ":" + ch["nonce"] + ":" + cnonce)
	}
	qop := ""
	for _, q := range strings.Split(ch["qop"], ",") {
		if q = strings.TrimSpace(q); q == "auth" || (q == "auth-int" && qop == "") {
			qop = q
		}
	}
	ha2 := h(req.Method + ":" + uri)
	if qop == "auth-int" {
		ha2 = h(req.Method + ":" + uri + ":" + h(string(body)))
	}
	var resp string
	if qop == "" {
		resp = h(ha1 + ":" + ch["nonce"] + ":" + ha2)
	} else {
		resp = h(ha1 + ":" + ch["nonce"] + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	}
	s := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`, user, ch["realm"], ch["nonce"], uri, resp)
	if ch["algorithm"] != "" {
		s += ", algorithm=" + ch["algorithm"]
	}
	if qop != "" {
		s += fmt.Sprintf(`, qop=%s, nc=%s, cnonce="%s"`, qop, nc, cnonce)
	}
	if ch["opaque"] != "" {
		s += fmt.Sprintf(`, opaque="%s"`, ch["opaque"])
	}
	return s
}

// ---- OAuth 2.0: a token from the token URL, kept until it expires ----

func oauth2Token(ctx context.Context, a *ReqAuth, t *Target, c *http.Client, fresh bool) (string, error) {
	v := func(s string) string { return strings.TrimSpace(Resolve(s, t.Vars)) }
	if tok := v(a.Token); tok != "" && !strings.Contains(tok, "{{") {
		return tok, nil // an access token typed or kept in a variable
	}
	tu, id, sec := v(a.TokenURL), v(a.ClientID), v(a.ClientSecret)
	if tu == "" || id == "" {
		return "", errors.New("OAuth 2.0: the token URL and the client ID are needed")
	}
	grant := a.Grant
	if grant == "" {
		grant = "client_credentials"
	}
	key := "oauth2|" + tu + "|" + id + "|" + grant + "|" + v(a.User) + "|" + v(a.Scope)
	return t.Tokens.Get(key, fresh, func() (string, time.Duration, error) {
		form := url.Values{"grant_type": {grant}}
		if s := v(a.Scope); s != "" {
			form.Set("scope", s)
		}
		if grant == "password" {
			form.Set("username", v(a.User))
			form.Set("password", v(a.Password))
		}
		if a.ClientAuth == "body" {
			form.Set("client_id", id)
			form.Set("client_secret", sec)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tu, strings.NewReader(form.Encode()))
		if err != nil {
			return "", 0, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		if a.ClientAuth != "body" {
			req.SetBasicAuth(url.QueryEscape(id), url.QueryEscape(sec))
		}
		resp, err := c.Do(req)
		if err != nil {
			return "", 0, fmt.Errorf("OAuth 2.0 token: %w", err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var tr struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   any    `json:"expires_in"`
			Error       string `json:"error"`
			Desc        string `json:"error_description"`
		}
		if json.Unmarshal(data, &tr) != nil || tr.AccessToken == "" {
			if tr.Error != "" {
				return "", 0, fmt.Errorf("OAuth 2.0 token: %s %s", tr.Error, tr.Desc)
			}
			return "", 0, fmt.Errorf("OAuth 2.0 token: the token URL answered %s without an access_token", resp.Status)
		}
		life := time.Duration(0)
		switch e := tr.ExpiresIn.(type) {
		case float64:
			life = time.Duration(e) * time.Second
		case string:
			var n int
			if _, err := fmt.Sscan(e, &n); err == nil {
				life = time.Duration(n) * time.Second
			}
		}
		if life == 0 {
			life = jwtLife(tr.AccessToken, time.Hour)
		}
		return tr.AccessToken, life, nil
	})
}

// ---- AWS Signature v4 ----

func awsSign(req *http.Request, a *ReqAuth, t *Target, body []byte, now time.Time) error {
	v := func(s string) string { return strings.TrimSpace(Resolve(s, t.Vars)) }
	ak, sk, region, service, st := v(a.AccessKey), v(a.SecretKey), v(a.Region), v(a.Service), v(a.SessionToken)
	if ak == "" || sk == "" {
		return errors.New("AWS Signature: the access key and the secret key are needed")
	}
	if region == "" {
		region = "us-east-1"
	}
	if service == "" {
		service = "execute-api"
	}
	t.Secrets = append(t.Secrets, sk)
	if st != "" {
		t.Secrets = append(t.Secrets, st)
	}
	amz := now.UTC().Format("20060102T150405Z")
	day := amz[:8]
	ph := sha256.Sum256(body)
	payload := hex.EncodeToString(ph[:])
	req.Header.Set("X-Amz-Date", amz)
	if service == "s3" { // S3 wants the payload's hash as a header too
		req.Header.Set("X-Amz-Content-Sha256", payload)
	}
	if st != "" {
		req.Header.Set("X-Amz-Security-Token", st)
	}
	host := req.URL.Host
	if req.Host != "" {
		host = req.Host
	}
	// canonical headers: host and every x-amz-* and content-type header
	hs := map[string]string{"host": host}
	for k, vs := range req.Header {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-") || lk == "content-type" {
			hs[lk] = strings.Join(vs, ",")
		}
	}
	names := make([]string, 0, len(hs))
	for k := range hs {
		names = append(names, k)
	}
	sort.Strings(names)
	var ch bytes.Buffer
	for _, k := range names {
		ch.WriteString(k + ":" + strings.TrimSpace(hs[k]) + "\n")
	}
	signed := strings.Join(names, ";")
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	q := req.URL.Query()
	qk := make([]string, 0, len(q))
	for k := range q {
		qk = append(qk, k)
	}
	sort.Strings(qk)
	var qs []string
	for _, k := range qk {
		vals := q[k]
		sort.Strings(vals)
		for _, x := range vals {
			qs = append(qs, awsEscape(k)+"="+awsEscape(x))
		}
	}
	creq := strings.Join([]string{req.Method, path, strings.Join(qs, "&"), ch.String(), signed, payload}, "\n")
	ch2 := sha256.Sum256([]byte(creq))
	scope := day + "/" + region + "/" + service + "/aws4_request"
	sts := "AWS4-HMAC-SHA256\n" + amz + "\n" + scope + "\n" + hex.EncodeToString(ch2[:])
	mac := func(k []byte, s string) []byte { m := hmac.New(sha256.New, k); m.Write([]byte(s)); return m.Sum(nil) }
	key := mac(mac(mac(mac([]byte("AWS4"+sk), day), region), service), "aws4_request")
	sig := hex.EncodeToString(mac(key, sts))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+ak+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
	return nil
}

// awsEscape: RFC 3986 escaping as AWS wants it (space as %20, ~ kept).
func awsEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(url.QueryEscape(s), "+", "%20"), "%7E", "~")
}

// requestBody reads a built request's body again (for signing and Digest's auth-int).
func requestBody(req *http.Request) []byte {
	if req.GetBody == nil {
		return nil
	}
	rc, err := req.GetBody()
	if err != nil {
		return nil
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return b
}
