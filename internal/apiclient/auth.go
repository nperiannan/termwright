package apiclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Auth is how a session logs in. Secret is the password, API key, token or client secret.
type Auth struct {
	// Type: "" none, apikey, bearer, login, basic, oauth2.
	Type string
	// KeyName: apikey: the header or query name; login: the header the token goes in ("" = Authorization: Bearer).
	KeyName string
	// KeyIn: apikey: "" header, query.
	KeyIn string
	// Login: login: "POST /auth/token" (JSON {"username","password"}).
	Login string
	// TokenPath: login: where the token is in the answer ("" finds token, access_token, ...).
	TokenPath string
	// TokenURL, ClientID, Scope: oauth2 (client credentials; password grant when User is set).
	TokenURL string
	ClientID string
	Scope    string
	// User for basic, login and the OAuth password grant.
	User   string
	Secret string
}

func (a Auth) renews() bool { return a.Type == "login" || a.Type == "oauth2" }

// apply adds the authentication; true when it added any.
func (a Auth) apply(ctx context.Context, req *http.Request, t *Target, c *http.Client, fresh bool) (bool, error) {
	switch a.Type {
	case "", "none":
		return false, nil
	case "apikey":
		name := a.KeyName
		if name == "" {
			name = "X-API-Key"
		}
		if a.KeyIn == "query" {
			q := req.URL.Query()
			q.Set(name, a.Secret)
			req.URL.RawQuery = q.Encode()
		} else {
			req.Header.Set(name, a.Secret)
		}
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+a.Secret)
	case "basic":
		req.SetBasicAuth(Resolve(a.User, t.Vars), a.Secret)
	case "login", "oauth2":
		tok, err := t.Tokens.Get(t.TokenKey, fresh, func() (string, time.Duration, error) {
			if a.Type == "login" {
				return a.login(ctx, t, c)
			}
			return a.oauth(ctx, t, c)
		})
		if err != nil {
			return false, err
		}
		if a.Type == "login" && a.KeyName != "" && !strings.EqualFold(a.KeyName, "Authorization") {
			req.Header.Set(a.KeyName, tok)
		} else {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	default:
		return false, fmt.Errorf("unknown authentication %q", a.Type)
	}
	return true, nil
}

// login sends the login request and finds the token in its answer (or an X-Auth-Token header).
func (a Auth) login(ctx context.Context, t *Target, c *http.Client) (string, time.Duration, error) {
	m, p, ok := strings.Cut(strings.TrimSpace(a.Login), " ")
	if !ok {
		m, p = "POST", a.Login
	}
	if p == "" {
		p = "/auth/token"
	}
	body, _ := json.Marshal(map[string]string{"username": Resolve(a.User, t.Vars), "password": a.Secret})
	u, err := FullURL(Request{URL: strings.TrimSpace(p)}, t)
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(m), u, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return "", 0, t.loginErr(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", 0, fmt.Errorf("the login %s %s answered %s", req.Method, req.URL.Path, resp.Status)
	}
	for _, h := range []string{"X-Auth-Token", "X-Subject-Token"} {
		if v := resp.Header.Get(h); v != "" && a.TokenPath == "" {
			return v, jwtLife(v, 0), nil
		}
	}
	var v any
	if json.Unmarshal(data, &v) != nil {
		return "", 0, errors.New("the login answer is not JSON")
	}
	tok := ""
	if a.TokenPath != "" {
		tok, _ = Path(v, a.TokenPath).(string)
	} else {
		for _, k := range []string{"token", "access_token", "accessToken", "id_token", "jwt", "data.token", "data.access_token", "result.token"} {
			if s, ok := Path(v, k).(string); ok && s != "" {
				tok = s
				break
			}
		}
	}
	if tok == "" {
		return "", 0, errors.New("no token in the login answer (set where it is in the session's API page)")
	}
	return tok, jwtLife(tok, expiresIn(v)), nil
}

// oauth gets an OAuth 2.0 token: client credentials, or the password grant when a user is set.
func (a Auth) oauth(ctx context.Context, t *Target, c *http.Client) (string, time.Duration, error) {
	f := url.Values{}
	if u := Resolve(a.User, t.Vars); u != "" {
		// the password grant sends the user's password; the client id goes along without a secret
		f.Set("grant_type", "password")
		f.Set("username", u)
		f.Set("password", a.Secret)
		f.Set("client_id", a.ClientID)
	} else {
		f.Set("grant_type", "client_credentials")
	}
	if a.Scope != "" {
		f.Set("scope", a.Scope)
	}
	tu := Resolve(a.TokenURL, t.Vars)
	if tu == "" {
		return "", 0, errors.New("no OAuth token URL")
	}
	if !strings.Contains(tu, "://") {
		var err error
		if tu, err = FullURL(Request{URL: tu}, t); err != nil {
			return "", 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, "POST", tu, strings.NewReader(f.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if f.Get("grant_type") != "password" {
		req.SetBasicAuth(url.QueryEscape(a.ClientID), url.QueryEscape(a.Secret))
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", 0, t.loginErr(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var v map[string]any
	_ = json.Unmarshal(data, &v)
	if resp.StatusCode/100 != 2 {
		if e, _ := v["error_description"].(string); e != "" {
			return "", 0, fmt.Errorf("the token endpoint answered %s: %s", resp.Status, e)
		}
		return "", 0, fmt.Errorf("the token endpoint answered %s", resp.Status)
	}
	tok, _ := v["access_token"].(string)
	if tok == "" {
		return "", 0, errors.New("no access_token in the token answer")
	}
	return tok, jwtLife(tok, expiresIn(v)), nil
}

func (t *Target) loginErr(err error) error { return errors.New(t.mask(err.Error())) }

func expiresIn(v any) time.Duration {
	for _, k := range []string{"expires_in", "expiresIn", "data.expires_in"} {
		switch n := Path(v, k).(type) {
		case float64:
			return time.Duration(n) * time.Second
		case string:
			if i, err := strconv.Atoi(n); err == nil {
				return time.Duration(i) * time.Second
			}
		}
	}
	return 0
}

// jwtLife: the life given, else a JWT's exp, else 30 minutes.
func jwtLife(tok string, given time.Duration) time.Duration {
	if given > 0 {
		return given
	}
	if p := strings.Split(tok, "."); len(p) == 3 {
		if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(p[1], "=")); err == nil {
			var c struct {
				Exp int64 `json:"exp"`
			}
			if json.Unmarshal(b, &c) == nil && c.Exp > 0 {
				return time.Until(time.Unix(c.Exp, 0))
			}
		}
	}
	return 30 * time.Minute
}

// Path finds a value in decoded JSON: "data.items.0.name".
func Path(v any, path string) any {
	for _, k := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			v = x[k]
		case []any:
			i, err := strconv.Atoi(k)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

// Tokens keeps login and OAuth tokens in memory (never on disk), renewed 5 minutes before they expire.
type Tokens struct {
	mu sync.Mutex
	m  map[string]token
}

type token struct {
	v   string
	exp time.Time
}

// Get returns a cached token or fetches one (always when fresh).
func (ts *Tokens) Get(key string, fresh bool, fetch func() (string, time.Duration, error)) (string, error) {
	if ts == nil {
		v, _, err := fetch()
		return v, err
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.m == nil {
		ts.m = map[string]token{}
	}
	if t, ok := ts.m[key]; ok && !fresh && time.Until(t.exp) > 5*time.Minute {
		return t.v, nil
	}
	v, life, err := fetch()
	if err != nil {
		return "", err
	}
	if life < 6*time.Minute { // short-lived: used until a minute before it ends
		life += 5*time.Minute - time.Minute
	}
	ts.m[key] = token{v, time.Now().Add(life)}
	return v, nil
}

// Drop forgets a token (a 401, a changed password).
func (ts *Tokens) Drop(key string) {
	if ts == nil {
		return
	}
	ts.mu.Lock()
	delete(ts.m, key)
	ts.mu.Unlock()
}

// Expires says when a cached token ends (zero when none).
func (ts *Tokens) Expires(key string) time.Time {
	if ts == nil {
		return time.Time{}
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.m[key].exp
}

// DropPrefix forgets the tokens of a session (all its environments).
func (ts *Tokens) DropPrefix(prefix string) {
	if ts == nil {
		return
	}
	ts.mu.Lock()
	for k := range ts.m {
		if strings.HasPrefix(k, prefix) {
			delete(ts.m, k)
		}
	}
	ts.mu.Unlock()
}
