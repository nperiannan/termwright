package app

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/apiclient"
	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/session"
)

// The API client (REST): API sessions (Protocol "API") hold the base URL, authentication and variables;
// their password / key / token / client secret and the values of secret variables are in the credential
// store (api-secret:, api-vars: + the path). Collections are JSON files in <data>/api/collections.

func httpSecretKey(path string) string { return "api-secret:" + path }
func httpVarsKey(path string) string   { return "api-vars:" + path }

var (
	httpTokens = &apiclient.Tokens{}
	httpJarsMu sync.Mutex
	httpJars   = map[string]http.CookieJar{}
	httpHistMu sync.Mutex
	httpHist   []HTTPHistoryItem
	httpLoaded bool
)

func (a *App) httpStore() apiclient.Store {
	return apiclient.Store{Dir: filepath.Join(a.cfg.DataDir(), "api", "collections")}
}

// HTTPSecretState says which secrets an API session has saved (never the secrets).
type HTTPSecretState struct {
	// Secret: the password, API key, token or client secret is saved.
	Secret bool `json:"secret"`
	// Vars: secret variables with a saved value ("name" and, when it has one, "name@prod").
	Vars []string `json:"vars"`
}

type httpVarSecret struct {
	Value string `json:"v,omitempty"`
	Prod  string `json:"p,omitempty"`
}

func httpVarSecrets(path string) map[string]httpVarSecret {
	m := map[string]httpVarSecret{}
	if v, ok := secrets.Lookup(httpVarsKey(path)); ok {
		_ = json.Unmarshal([]byte(v), &m)
	}
	return m
}

// HTTPSecrets says which secrets an API session has saved.
func (a *App) HTTPSecrets(path string) HTTPSecretState {
	_, ok := secrets.Lookup(httpSecretKey(path))
	st := HTTPSecretState{Secret: ok, Vars: []string{}}
	for k, v := range httpVarSecrets(path) {
		if v.Value != "" {
			st.Vars = append(st.Vars, k)
		}
		if v.Prod != "" {
			st.Vars = append(st.Vars, k+"@prod")
		}
	}
	return st
}

// HTTPVarSecretIn is a secret variable's values: nil keeps one, "" removes it.
type HTTPVarSecretIn struct {
	Name  string  `json:"name"`  // the variable
	Value *string `json:"value"` // its value (nil keeps, "" removes)
	Prod  *string `json:"prod"`
}

// HTTPSecretsIn saves an API session's secrets: nil keeps one, "" removes it. Keep lists the secret
// variables the session still has (the others are forgotten).
type HTTPSecretsIn struct {
	Path   string            `json:"path"`   // the API session
	Secret *string           `json:"secret"` // password, API key, token or client secret (nil keeps, "" removes)
	Vars   []HTTPVarSecretIn `json:"vars"`   // secret variables' values
	Keep   []string          `json:"keep"`
}

// HTTPSetSecrets saves an API session's secrets in the credential store; its tokens are logged in again.
func (a *App) HTTPSetSecrets(in HTTPSecretsIn) error {
	if in.Path == "" {
		return errors.New("save the session first")
	}
	if in.Secret != nil {
		if *in.Secret == "" {
			_ = secrets.Delete(httpSecretKey(in.Path))
		} else if err := secrets.Set(httpSecretKey(in.Path), *in.Secret); err != nil {
			return err
		}
	}
	m := httpVarSecrets(in.Path)
	if in.Keep != nil {
		keep := map[string]bool{}
		for _, k := range in.Keep {
			keep[k] = true
		}
		for k := range m {
			if !keep[k] {
				delete(m, k)
			}
		}
	}
	for _, v := range in.Vars {
		cur := m[v.Name]
		if v.Value != nil {
			cur.Value = *v.Value
		}
		if v.Prod != nil {
			cur.Prod = *v.Prod
		}
		if cur == (httpVarSecret{}) {
			delete(m, v.Name)
		} else {
			m[v.Name] = cur
		}
	}
	if len(m) == 0 {
		_ = secrets.Delete(httpVarsKey(in.Path))
	} else {
		b, _ := json.Marshal(m)
		if err := secrets.Set(httpVarsKey(in.Path), string(b)); err != nil {
			return err
		}
	}
	httpTokens.DropPrefix(in.Path + "|")
	return nil
}

// HTTPSendRequest sends a request with an API session (or none).
type HTTPSendRequest struct {
	// Session: an API session's path (base URL, authentication, variables); "" sends the request as it is.
	Session string `json:"session,omitempty"`
	// Env: "" (the session's own values) or "production" (the variables' production values).
	Env string `json:"env,omitempty"`
	// Collection: its variables are used too (id or name).
	Collection string `json:"collection,omitempty"`
	// Request to send.
	Request apiclient.Request `json:"request"`
	// Environment: the id or name of an environment whose variables win over the session's ("" none).
	Environment string `json:"environment,omitempty"`
	// Vars: values that win over the session's and the collection's (set by earlier scripts).
	Vars map[string]string `json:"vars,omitempty"`
	// Timeout in seconds (default 30).
	Timeout int `json:"timeout,omitempty"`
	// NoRedirects: answer 3xx instead of following.
	NoRedirects bool `json:"noRedirects,omitempty"`
	// Confirmed: the person agreed to a change on a production session.
	Confirmed bool `json:"confirmed,omitempty"`
}

// HTTPDraftTest tries unsaved API session settings (the session editor's Test): GET url (default /).
type HTTPDraftTest struct {
	Session session.Session `json:"session"`
	// Path: the saved session's path, whose saved secrets are used when Secret is empty.
	Path string `json:"path,omitempty"`
	// Secret typed in the editor (not saved).
	Secret string `json:"secret,omitempty"`
	URL    string `json:"url,omitempty"`
}

// HTTPTest sends GET to unsaved API session settings, logging in afresh.
func (a *App) HTTPTest(d HTTPDraftTest) (apiclient.Response, error) {
	t, _, _, err := a.httpTargetDraft(HTTPSendRequest{Session: d.Path}, &d.Session, d.Secret)
	if err != nil {
		return apiclient.Response{}, err
	}
	u := d.URL
	if u == "" {
		u = "/"
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return apiclient.Send(ctx, apiclient.Request{Method: "GET", URL: u}, t), nil
}

var errHTTPConfirm = errors.New("this is a production session: a change (POST, PUT, PATCH, DELETE) asks the user first")

// httpTarget makes the target of a request; prod: the session is protected or the Production values are used.
func (a *App) httpTarget(r HTTPSendRequest) (*apiclient.Target, *session.Session, bool, error) {
	return a.httpTargetDraft(r, nil, "")
}

func (a *App) httpTargetDraft(r HTTPSendRequest, draft *session.Session, typed string) (*apiclient.Target, *session.Session, bool, error) {
	t := &apiclient.Target{Vars: map[string]string{}, Tokens: httpTokens, Timeout: time.Duration(min(max(r.Timeout, 0), 600)) * time.Second, NoRedirects: r.NoRedirects}
	if a.ctx != nil { // OAuth 2.0's authorization code and implicit grants sign in in the browser
		ctx := a.ctx
		t.Browse = func(u string) error { wruntime.BrowserOpenURL(ctx, u); return nil }
	}
	a.httpEnvVars(apiclient.GlobalsID, t)
	if r.Collection != "" {
		c, err := a.httpStore().Find(r.Collection)
		if err != nil {
			return nil, nil, false, err
		}
		for _, v := range c.Vars {
			if !v.Off {
				t.Vars[v.Key] = v.Value
			}
		}
		t.CollAuth, t.CollAuthData, t.CollPre, t.CollTests = c.Auth, c.AuthData, c.PreScript, c.Tests
	}
	var sp *session.Session
	prodEnv := strings.EqualFold(r.Env, "production")
	if draft != nil || r.Session != "" {
		var s session.Session
		if draft != nil {
			s = *draft
		} else {
			if err := a.ready(); err != nil {
				return nil, nil, false, err
			}
			var err error
			if s, err = a.store.Get(r.Session); err != nil {
				return nil, nil, false, fmt.Errorf("no saved session %q", r.Session)
			}
		}
		sp = &s
		path := s.Path
		if draft != nil {
			path = r.Session
		}
		sec, _ := secrets.Lookup(httpSecretKey(path))
		if typed != "" {
			sec = typed
		}
		vs := httpVarSecrets(path)
		if s.APIBaseURL != "" { // the session's address wins over a collection's (as an environment does in Postman)
			t.Vars["baseUrl"] = s.APIBaseURL
		}
		for _, v := range s.APIVars {
			val := v.Value
			if v.Secret {
				val = vs[v.Name].Value
			}
			if prodEnv {
				p := v.Prod
				if v.Secret {
					p = vs[v.Name].Prod
				}
				if p != "" {
					val = p
				}
			}
			t.Vars[v.Name] = val
			if v.Secret && val != "" {
				t.Secrets = append(t.Secrets, val)
			}
		}
		if t.Vars["baseUrl"] != "" {
			t.BaseURL = "{{baseUrl}}"
		}
		t.Auth = apiclient.Auth{Type: s.APIAuth, KeyName: s.APIKeyName, KeyIn: s.APIKeyIn, Login: s.APILogin, TokenPath: s.APITokenPath,
			TokenURL: s.APITokenURL, ClientID: s.APIClientID, Scope: s.APIScope, User: s.Username, Secret: sec}
		t.Insecure, t.CAFile = s.APIInsecure, s.APICAFile
		t.TokenKey = path + "|" + strings.ToLower(r.Env)
		if draft != nil {
			t.Tokens, t.TokenKey = nil, "" // a test logs in afresh
		}
		httpJarsMu.Lock()
		if httpJars[path] == nil {
			httpJars[path] = apiclient.NewJar()
		}
		t.Jar = httpJars[path]
		httpJarsMu.Unlock()
	} else {
		httpJarsMu.Lock()
		if httpJars[""] == nil {
			httpJars[""] = apiclient.NewJar()
		}
		t.Jar = httpJars[""]
		httpJarsMu.Unlock()
	}
	if r.Environment != "" {
		if err := a.httpEnvVars(r.Environment, t); err != nil {
			return nil, nil, false, err
		}
	}
	if t.BaseURL == "" && t.Vars["baseUrl"] != "" {
		t.BaseURL = "{{baseUrl}}"
	}
	for k, v := range r.Vars {
		t.Vars[k] = v
	}
	prod := prodEnv || (sp != nil && a.store != nil && a.isProtected(*sp))
	return t, sp, prod, nil
}

// HTTPSend sends a request (its pre-request script and tests run too). A change on a production session
// needs Confirmed.
func (a *App) HTTPSend(r HTTPSendRequest) (apiclient.Response, error) {
	return a.httpSend(a.ctx, r)
}

func (a *App) httpSend(ctx context.Context, r HTTPSendRequest) (apiclient.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	t, _, prod, err := a.httpTarget(r)
	if err != nil {
		return apiclient.Response{}, err
	}
	if prod && apiclient.Changes(strings.ToUpper(r.Request.Method)) && !r.Confirmed {
		return apiclient.Response{}, errHTTPConfirm
	}
	t.ScriptSend = httpScriptGuard(prod, r.Confirmed)
	res := apiclient.Run(ctx, r.Request, t)
	a.httpKeepSet(r.Environment, r.Collection, res)
	a.httpRemember(r, res)
	return res, nil
}

// HTTPHistoryItem is a request sent (no answer bodies, secrets masked).
type HTTPHistoryItem struct {
	At         time.Time `json:"at"`                   // when it was sent
	Session    string    `json:"session,omitempty"`    // the API session
	Env        string    `json:"env,omitempty"`        // "" or production
	Collection string    `json:"collection,omitempty"` // the collection it came from
	// Environment chosen when it was sent.
	Environment string            `json:"environment,omitempty"`
	Request     apiclient.Request `json:"request"` // the request (the body cut at 16 KB)
	URL         string            `json:"url"`     // the URL sent (secrets masked)
	Status      int               `json:"status"`  // the answer's status (0: not sent)
	Ms          int64             `json:"ms"`      // how long it took
	Size        int               `json:"size"`    // the answer's size in bytes
	Error       string            `json:"error,omitempty"`
}

func (a *App) httpHistFile() string { return filepath.Join(a.cfg.DataDir(), "api", "history.json") }

func (a *App) httpLoadHist() {
	if httpLoaded {
		return
	}
	httpLoaded = true
	if b, err := os.ReadFile(a.httpHistFile()); err == nil {
		_ = json.Unmarshal(b, &httpHist)
	}
}

func (a *App) httpRemember(r HTTPSendRequest, res apiclient.Response) {
	httpHistMu.Lock()
	defer httpHistMu.Unlock()
	a.httpLoadHist()
	rq := r.Request
	if rq.AuthData != nil {
		ad := *rq.AuthData
		ad.Scrub()
		rq.AuthData = &ad
	}
	if len(rq.Body.Raw) > 16<<10 {
		rq.Body.Raw = rq.Body.Raw[:16<<10]
	}
	httpHist = append([]HTTPHistoryItem{{At: time.Now(), Session: r.Session, Env: r.Env, Environment: r.Environment, Collection: r.Collection, Request: rq, URL: res.URL,
		Status: res.Status, Ms: res.Ms, Size: res.Size, Error: res.Error}}, httpHist...)
	if len(httpHist) > 300 {
		httpHist = httpHist[:300]
	}
	if a.cfg == nil {
		return
	}
	b, _ := json.Marshal(httpHist)
	_ = os.MkdirAll(filepath.Dir(a.httpHistFile()), 0o700)
	_ = os.WriteFile(a.httpHistFile(), b, 0o600)
}

// HTTPHistory is the requests sent, newest first.
func (a *App) HTTPHistory() []HTTPHistoryItem {
	httpHistMu.Lock()
	defer httpHistMu.Unlock()
	a.httpLoadHist()
	return append([]HTTPHistoryItem{}, httpHist...)
}

// HTTPClearHistory forgets the requests sent.
func (a *App) HTTPClearHistory() {
	httpHistMu.Lock()
	httpHist, httpLoaded = nil, true
	httpHistMu.Unlock()
	_ = os.Remove(a.httpHistFile())
}

// HTTPCollections lists the collections.
func (a *App) HTTPCollections() []apiclient.CollectionInfo {
	l := a.httpStore().List()
	if l == nil {
		l = []apiclient.CollectionInfo{}
	}
	return l
}

// HTTPCollection is one collection (id or name).
func (a *App) HTTPCollection(id string) (apiclient.Collection, error) { return a.httpStore().Find(id) }

// HTTPSaveCollection saves a collection (a new one gets an id from its name).
func (a *App) HTTPSaveCollection(c apiclient.Collection) (apiclient.Collection, error) {
	return a.httpStore().Save(c)
}

// HTTPDeleteCollection deletes a collection's file.
func (a *App) HTTPDeleteCollection(id string) error { return a.httpStore().Delete(id) }

// HTTPImport reads a cURL command, a Postman collection or an OpenAPI / Swagger document and saves it as a
// collection (using session when given).
func (a *App) HTTPImport(text, name, session string) (apiclient.Collection, error) {
	if apiclient.IsEnvironment(text) {
		return apiclient.Collection{}, errors.New("this is a Postman environment, not a collection: import it with Environments → Import")
	}
	c, err := apiclient.Import(text, name)
	if err != nil {
		return c, err
	}
	c.ID = ""
	if session != "" {
		c.Session = session
	}
	return a.httpStore().Save(c)
}

// HTTPParseCurl reads one cURL command as a request (nothing is saved).
func (a *App) HTTPParseCurl(text string) (apiclient.Request, error) { return apiclient.ParseCurl(text) }

// HTTPImportFile asks for a file and imports it.
func (a *App) HTTPImportFile(session string) (apiclient.Collection, error) {
	p, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Import Postman, OpenAPI or cURL",
		Filters: []wruntime.FileFilter{{DisplayName: "Collections and API documents (*.json, *.yaml, *.yml, *.txt, *.sh)", Pattern: "*.json;*.yaml;*.yml;*.txt;*.sh;*.curl"}}})
	if err != nil || p == "" {
		return apiclient.Collection{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return apiclient.Collection{}, err
	}
	return a.HTTPImport(string(b), "", session)
}

// HTTPExportText is a collection as Termwright JSON ("" or "termwright") or Postman v2.1 ("postman").
func (a *App) HTTPExportText(id, format string) (string, error) {
	c, err := a.httpStore().Find(id)
	if err != nil {
		return "", err
	}
	if format == "postman" {
		b, err := apiclient.ExportPostman(c)
		return string(b), err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return string(b), nil
}

// HTTPExportFile saves a collection to a file the person chooses.
func (a *App) HTTPExportFile(id, format string) (string, error) {
	text, err := a.HTTPExportText(id, format)
	if err != nil {
		return "", err
	}
	name := id + ".json"
	if format == "postman" {
		name = id + ".postman_collection.json"
	}
	p, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "Export Collection", DefaultFilename: name, CanCreateDirectories: true})
	if err != nil || p == "" {
		return "", err
	}
	return p, os.WriteFile(p, []byte(text), 0o600)
}

// HTTPCode is the request as code: curl, python, javascript or powershell (secrets as environment variables).
func (a *App) HTTPCode(r HTTPSendRequest, lang string) (string, error) {
	t, _, _, err := a.httpTarget(r)
	if err != nil {
		return "", err
	}
	return apiclient.Code(r.Request, t, lang)
}

// HTTPTokenState says whether a login / OAuth token is held for a session and until when.
type HTTPTokenState struct {
	Held    bool   `json:"held"` // a token is held (in memory only)
	Expires string `json:"expires,omitempty"`
}

// HTTPToken says whether a token is held (in memory only) for a session and environment.
func (a *App) HTTPToken(path, env string) HTTPTokenState {
	e := httpTokens.Expires(path + "|" + strings.ToLower(env))
	if e.IsZero() {
		return HTTPTokenState{}
	}
	return HTTPTokenState{Held: true, Expires: e.Format(time.RFC3339)}
}

// HTTPRunRequest runs a collection (or some of its requests).
type HTTPRunRequest struct {
	// Collection: id or name.
	Collection string `json:"collection"`
	// Session: the API session (default: the collection's).
	Session string `json:"session,omitempty"`
	// Env: "" or "production".
	Env string `json:"env,omitempty"`
	// Environment: the id or name of an environment ("" none).
	Environment string `json:"environment,omitempty"`
	// Requests: ids or names to run, in the collection's order ("" = all).
	Requests []string `json:"requests,omitempty"`
	// Folder: only the requests in this folder (and below).
	Folder string `json:"folder,omitempty"`
	// Iterations: how many times (default 1); Data rows override it.
	Iterations int `json:"iterations,omitempty"`
	// Data: CSV (first line = names) or a JSON array of objects; each row is one iteration's variables.
	Data string `json:"data,omitempty"`
	// DelayMs between requests.
	DelayMs int `json:"delayMs,omitempty"`
	// StopOnFail: stop at the first failing request.
	StopOnFail bool `json:"stopOnFail,omitempty"`
	// Confirmed: the person agreed to the changes on a production session.
	Confirmed bool `json:"confirmed,omitempty"`
}

// HTTPRun runs a collection's requests with their scripts and tests; each step is also sent as an "http:run" event.
func (a *App) HTTPRun(r HTTPRunRequest) (apiclient.RunResult, error) {
	return a.httpRun(a.ctx, r)
}

func (a *App) httpRun(ctx context.Context, r HTTPRunRequest) (apiclient.RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c, err := a.httpStore().Find(r.Collection)
	if err != nil {
		return apiclient.RunResult{}, err
	}
	sess := r.Session
	if sess == "" {
		sess = c.Session
	}
	t, _, prod, err := a.httpTarget(HTTPSendRequest{Session: sess, Env: r.Env, Collection: c.ID, Environment: r.Environment})
	if err != nil {
		return apiclient.RunResult{}, err
	}
	var reqs []apiclient.Request
	if len(r.Requests) > 0 {
		for _, id := range r.Requests {
			rq, ok := c.Request(id)
			if !ok {
				return apiclient.RunResult{}, fmt.Errorf("no request %q in %s", id, c.Name)
			}
			reqs = append(reqs, rq)
		}
	} else {
		for _, rq := range c.Requests {
			if r.Folder == "" || rq.Folder == r.Folder || strings.HasPrefix(rq.Folder, r.Folder+"/") {
				reqs = append(reqs, rq)
			}
		}
	}
	if len(reqs) == 0 {
		return apiclient.RunResult{}, errors.New("no requests to run")
	}
	data, err := runData(r.Data)
	if err != nil {
		return apiclient.RunResult{}, err
	}
	o := apiclient.RunOptions{Iterations: r.Iterations, Data: data, Delay: time.Duration(min(max(r.DelayMs, 0), 60000)) * time.Millisecond, StopOnFail: r.StopOnFail,
		Allow: func(rq apiclient.Request) error {
			if prod && apiclient.Changes(strings.ToUpper(rq.Method)) && !r.Confirmed {
				return errHTTPConfirm
			}
			return nil
		},
		Done:    func(_ apiclient.Request, res apiclient.Response) { a.httpKeepSet(r.Environment, r.Collection, res) },
		EnvName: a.httpEnvName(r.Environment),
		Each: func(st apiclient.RunStep) {
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "http:run", st)
			}
		}}
	t.ScriptSend = httpScriptGuard(prod, r.Confirmed)
	return apiclient.RunCollection(ctx, reqs, t, o), nil
}

// runData reads a runner's data: CSV with a header line, or a JSON array of objects.
func runData(s string) ([]map[string]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []map[string]string
	if strings.HasPrefix(s, "[") {
		var l []map[string]any
		if err := json.Unmarshal([]byte(s), &l); err != nil {
			return nil, fmt.Errorf("data: %w", err)
		}
		for _, m := range l {
			row := map[string]string{}
			for k, v := range m {
				if sv, ok := v.(string); ok {
					row[k] = sv
				} else {
					b, _ := json.Marshal(v)
					row[k] = string(b)
				}
			}
			out = append(out, row)
		}
		return out, nil
	}
	recs, err := csv.NewReader(strings.NewReader(s)).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("data: %w", err)
	}
	if len(recs) < 2 {
		return nil, errors.New("data: a header line and at least one row")
	}
	for _, rec := range recs[1:] {
		row := map[string]string{}
		for i, k := range recs[0] {
			if i < len(rec) {
				row[strings.TrimSpace(k)] = rec[i]
			}
		}
		out = append(out, row)
	}
	return out, nil
}
