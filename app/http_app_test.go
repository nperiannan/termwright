package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/termwright/termwright/internal/apiclient"
	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

// An API session with a login and a secret variable: the AI tool sends with it, secrets stay masked,
// a production change asks, collections save, import and run, and the secrets follow a rename.
func TestHTTPSessionToolAndRun(t *testing.T) {
	keyring.MockInit()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			var b map[string]string
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["password"] != "pw-123456" {
				w.WriteHeader(401)
				return
			}
			_, _ = io.WriteString(w, `{"token":"T1"}`)
		default:
			if r.Header.Get("Authorization") != "Bearer T1" {
				w.WriteHeader(401)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			body, _ := io.ReadAll(r.Body)
			_ = json.NewEncoder(w).Encode(map[string]string{"method": r.Method, "path": r.URL.Path, "site": r.URL.Query().Get("site"), "body": string(body)})
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	s, err := st.Save(session.Session{Name: "rest", Folder: "APIs", Protocol: session.ProtoAPI, APIBaseURL: srv.URL, APIAuth: "login", APILogin: "POST /auth/token", Username: "admin",
		APIVars: []session.APIVar{{Name: "site", Value: "lab", Prod: "dc"}, {Name: "key", Secret: true}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	pw, sec := "pw-123456", "k-secret-1"
	if err := a.HTTPSetSecrets(HTTPSecretsIn{Path: s.Path, Secret: &pw, Vars: []HTTPVarSecretIn{{Name: "key", Value: &sec}}}); err != nil {
		t.Fatal(err)
	}
	if st := a.HTTPSecrets(s.Path); !st.Secret || len(st.Vars) != 1 || st.Vars[0] != "key" {
		t.Fatalf("%+v", st)
	}
	ctx := context.Background()
	allow := func(string, string, bool) error { return nil }
	out, err := a.netToolRun(ctx, "http", map[string]any{"action": "send", "session": s.Path, "url": "/ports", "query": map[string]any{"site": "{{site}}", "k": "{{key}}"}}, allow)
	if err != nil || !strings.Contains(out, `"site": "lab"`) || !strings.Contains(out, "200 OK") || strings.Contains(out, sec) {
		t.Fatalf("%v\n%s", err, out)
	}
	out, _ = a.netToolRun(ctx, "http", map[string]any{"action": "send", "session": s.Path, "env": "production", "url": "/ports", "query": map[string]any{"site": "{{site}}"}}, allow)
	if !strings.Contains(out, `"site": "dc"`) {
		t.Fatal(out)
	}
	// production values make it a production request: a change asks
	asked := false
	_, err = a.netToolRun(ctx, "http", map[string]any{"action": "send", "session": s.Path, "env": "production", "method": "post", "url": "/vlans", "body": map[string]any{"id": 10}},
		func(_, _ string, danger bool) error { asked = danger; return errEmuAsk })
	if !asked || err != errEmuAsk {
		t.Fatalf("not asked: %v", err)
	}
	if _, err := a.HTTPSend(HTTPSendRequest{Session: s.Path, Env: "production", Request: apiclient.Request{Method: "DELETE", URL: "/x"}}); err != errHTTPConfirm {
		t.Fatalf("binding: %v", err)
	}
	// save, run, import
	out, err = a.netToolRun(ctx, "http", map[string]any{"action": "save", "collection": "Lab checks", "session": s.Path,
		"request": map[string]any{"name": "ports", "method": "GET", "url": "/ports", "tests": `pm.test("lab", () => pm.expect(pm.response.json().site).to.equal("lab"))`, "params": []any{map[string]any{"key": "site", "value": "{{site}}"}}}}, allow)
	if err != nil || !strings.Contains(out, "1 requests") {
		t.Fatal(err, out)
	}
	out, err = a.netToolRun(ctx, "http", map[string]any{"action": "run", "collection": "lab checks"}, allow)
	if err != nil || !strings.Contains(out, "1 passed, 0 failed, 1 tests") {
		t.Fatal(err, out)
	}
	out, err = a.netToolRun(ctx, "http", map[string]any{"action": "import", "text": "curl -X POST " + srv.URL + "/vlans -d '{\"id\":5}'", "name": "From curl"}, allow)
	if err != nil || !strings.Contains(out, "1 requests") {
		t.Fatal(err, out)
	}
	if l := a.HTTPCollections(); len(l) != 2 {
		t.Fatal(l)
	}
	if h := a.HTTPHistory(); len(h) != 2 || strings.Contains(h[1].URL, sec) || !strings.Contains(h[1].URL, "••") {
		t.Fatal(h)
	}
	code, err := a.HTTPCode(HTTPSendRequest{Session: s.Path, Request: apiclient.Request{URL: "/p?k={{key}}"}}, "curl")
	if err != nil || !strings.Contains(code, "API_TOKEN") {
		t.Fatal(err, code)
	}
	// the secrets follow a rename
	s2 := s
	s2.Name = "rest2"
	saved, err := a.SaveSession(s2, s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if st := a.HTTPSecrets(saved.Path); !st.Secret || len(st.Vars) != 1 {
		t.Fatalf("secrets did not follow: %+v", st)
	}
	if st := a.HTTPSecrets(s.Path); st.Secret {
		t.Fatal("old secret kept")
	}
	// the draft test of the editor uses the typed secret
	res, err := a.HTTPTest(HTTPDraftTest{Session: saved, Path: saved.Path, URL: "/ping"})
	if err != nil || res.Status != 200 {
		t.Fatalf("%v %+v", err, res)
	}
	res, _ = a.HTTPTest(HTTPDraftTest{Session: saved, Path: saved.Path, Secret: "wrong-pass", URL: "/ping"})
	if !strings.Contains(res.Error, "answered 401") {
		t.Fatalf("%+v", res)
	}
}
