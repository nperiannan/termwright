package apiclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A device-like API: login gives a token (X-Auth-Token style), the token expires once (401), then works.
func fakeAPI(t *testing.T) (*httptest.Server, *int32) {
	var logins int32
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b["username"] != "admin" || b["password"] != "s3cret!" {
			w.WriteHeader(401)
			return
		}
		n := atomic.AddInt32(&logins, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"token": "tok" + string(rune('0'+n))}, "expires_in": 3600})
	})
	mux.HandleFunc("/rest/ports", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") != "tok2" { // the first token is refused: the client logs in again
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc"})
		_, _ = io.WriteString(w, `{"ports":[{"name":"1:1","oper":"UP"},{"name":"1:2","oper":"DOWN"}],"q":"`+r.URL.Query().Get("site")+`"}`)
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"method": r.Method, "ct": r.Header.Get("Content-Type"), "body": string(b), "key": r.Header.Get("X-API-Key"), "auth": r.Header.Get("Authorization")})
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		id, sec, _ := r.BasicAuth()
		_ = r.ParseForm()
		if id != "cid" || sec != "csec" || r.Form.Get("grant_type") != "client_credentials" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error_description":"bad client"}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"oat","expires_in":600}`)
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s, &logins
}

func TestLoginTokenRenewTestsAndVars(t *testing.T) {
	s, logins := fakeAPI(t)
	tg := &Target{BaseURL: "{{host}}", Vars: map[string]string{"host": s.URL, "site": "lab"}, Tokens: &Tokens{}, TokenKey: "k", Jar: NewJar(),
		Auth: Auth{Type: "login", Login: "POST /auth/token", User: "admin", Secret: "s3cret!", KeyName: "X-Auth-Token"}}
	r := Request{Method: "GET", URL: "/rest/ports", Params: []KV{{Key: "site", Value: "{{site}}"}},
		PreScript: `pm.variables.set("want", "DOWN")`,
		Tests: `pm.test("status", () => pm.response.to.have.status(200));
pm.test("1:2 down", function() { const j = pm.response.json(); pm.expect(j.ports[1].oper).to.equal(pm.variables.get("want")); pm.expect(j.ports).to.have.lengthOf(2); });
pm.test("fails", () => pm.expect(1).to.be.above(5));
pm.variables.set("first", pm.response.json().ports[0].name); console.log("ok", 1)`}
	res := Run(context.Background(), r, tg)
	if res.Status != 200 || res.Error != "" || !strings.Contains(res.Body, `"q":"lab"`) {
		t.Fatalf("%+v", res)
	}
	if *logins != 2 {
		t.Fatalf("logins %d (expected a second after the 401)", *logins)
	}
	if len(res.Tests) != 3 || !res.Tests[0].Pass || !res.Tests[1].Pass || res.Tests[2].Pass || !strings.Contains(res.Tests[2].Error, "above 5") {
		t.Fatalf("tests %+v", res.Tests)
	}
	if tg.Vars["first"] != "1:1" || res.Vars["want"] != "DOWN" || len(res.Logs) != 1 || res.Logs[0] != "ok 1" {
		t.Fatalf("vars %v %v logs %v", tg.Vars, res.Vars, res.Logs)
	}
	for _, h := range res.ReqHeaders {
		if h.Key == "X-Auth-Token" && h.Value != "••••••••" {
			t.Fatalf("token shown: %v", h)
		}
	}
	if len(res.Cookies) != 1 || res.Cookies[0].Name != "sid" {
		t.Fatalf("cookies %+v", res.Cookies)
	}
	// the token is reused
	res = Send(context.Background(), r, tg)
	if res.Status != 200 || *logins != 2 {
		t.Fatalf("reuse: %d logins %d", res.Status, *logins)
	}
}

func TestAuthKindsAndBodies(t *testing.T) {
	s, _ := fakeAPI(t)
	echo := func(a Auth, b Body) map[string]any {
		res := Send(context.Background(), Request{Method: "POST", URL: s.URL + "/echo", Body: b}, &Target{Auth: a, Vars: map[string]string{"n": "5"}, Secrets: []string{"hidden-value"}})
		var m map[string]any
		if err := json.Unmarshal([]byte(res.Body), &m); err != nil {
			t.Fatalf("%v %+v", err, res)
		}
		return m
	}
	if m := echo(Auth{Type: "apikey", Secret: "K1"}, Body{Mode: "json", Raw: `{"n":{{n}}}`}); m["key"] != "K1" || m["body"] != `{"n":5}` || m["ct"] != "application/json" {
		t.Fatal(m)
	}
	if m := echo(Auth{Type: "basic", User: "u", Secret: "p"}, Body{Mode: "urlencoded", Form: []KV{{Key: "a", Value: "1"}, {Key: "b", Value: "x", Off: true}}}); m["auth"] != "Basic dTpw" || m["body"] != "a=1" {
		t.Fatal(m)
	}
	if m := echo(Auth{Type: "oauth2", TokenURL: s.URL + "/oauth/token", ClientID: "cid", Secret: "csec"}, Body{Mode: "graphql", Raw: "{ ports }"}); m["auth"] != "Bearer oat" || !strings.Contains(m["body"].(string), `"query":"{ ports }"`) {
		t.Fatal(m)
	}
	res := Send(context.Background(), Request{URL: s.URL + "/echo", Auth: "none"}, &Target{Auth: Auth{Type: "oauth2", TokenURL: s.URL + "/oauth/token", ClientID: "x", Secret: "y"}})
	if res.Status != 200 {
		t.Fatalf("auth none still logged in: %+v", res)
	}
	res = Send(context.Background(), Request{URL: s.URL + "/echo"}, &Target{Auth: Auth{Type: "oauth2", TokenURL: s.URL + "/oauth/token", ClientID: "x", Secret: "y"}})
	if !strings.Contains(res.Error, "bad client") {
		t.Fatalf("%+v", res)
	}
	if _, err := FullURL(Request{URL: "/x/{{missing}}"}, &Target{BaseURL: "http://h"}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatal(err)
	}
}

func TestRunnerWithData(t *testing.T) {
	s, _ := fakeAPI(t)
	reqs := []Request{{Name: "echo", Method: "POST", URL: "/echo", Body: Body{Mode: "text", Raw: "{{v}}"}, Tests: `pm.test("got", () => pm.expect(pm.response.json().body).to.equal(pm.iterationData.get("v")))`},
		{Name: "missing", URL: "/nothing"}}
	tg := &Target{BaseURL: s.URL, Vars: map[string]string{}}
	var seen int
	res := RunCollection(context.Background(), reqs, tg, RunOptions{Data: []map[string]string{{"v": "a"}, {"v": "b"}}, Each: func(RunStep) { seen++ }})
	if len(res.Steps) != 4 || seen != 4 || res.Passed != 4 || res.Tests != 2 {
		t.Fatalf("%+v", res) // a 404 answers: it passes (no 5xx, no failed test)
	}
	res = RunCollection(context.Background(), []Request{{URL: "/echo", Tests: `pm.test("x", () => pm.expect(true).to.be.false)`}, {URL: "/echo"}}, tg, RunOptions{StopOnFail: true})
	if !res.Stopped || len(res.Steps) != 1 || res.Failed != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestImportCurlPostmanOpenAPI(t *testing.T) {
	c, err := Import(`curl -X PATCH 'https://sw/rest/ports/1:1' -H 'Content-Type: application/json' \
  -H "X-Auth-Token: abc" --data-raw '{"description":"uplink"}' -k`, "")
	if err != nil || len(c.Requests) != 1 {
		t.Fatal(err, c)
	}
	r := c.Requests[0]
	if r.Method != "PATCH" || r.URL != "https://sw/rest/ports/1:1" || r.Body.Mode != "json" || r.Body.Raw != `{"description":"uplink"}` || len(r.Headers) != 1 || r.Headers[0].Key != "X-Auth-Token" {
		t.Fatalf("%+v", r)
	}
	r, _ = ParseCurl(`curl https://h/login -d user=a -d pass=b`)
	if r.Method != "POST" || r.Body.Mode != "urlencoded" || len(r.Body.Form) != 2 {
		t.Fatalf("%+v", r)
	}
	pm := `{"info":{"name":"Lab","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
	 "variable":[{"key":"baseUrl","value":"https://10.0.0.1"}],
	 "item":[{"name":"Ports","item":[{"name":"List","request":{"method":"GET","url":{"raw":"{{baseUrl}}/ports?x=1","query":[{"key":"x","value":"1"}]},
	   "header":[{"key":"Accept","value":"application/json"}]},"event":[{"listen":"test","script":{"exec":["pm.test('ok', ()=>{});"]}}]}]},
	  {"name":"Set","request":{"method":"PUT","url":"{{baseUrl}}/p","body":{"mode":"raw","raw":"{\"a\":1}","options":{"raw":{"language":"json"}}},"auth":{"type":"bearer","bearer":[{"key":"token","value":"{{tok}}"}]}}}]}`
	c, err = Import(pm, "")
	if err != nil || c.Name != "Lab" || len(c.Requests) != 2 || c.Vars[0].Key != "baseUrl" {
		t.Fatal(err, c)
	}
	if r := c.Requests[0]; r.Folder != "Ports" || r.URL != "{{baseUrl}}/ports" || len(r.Params) != 1 || !strings.Contains(r.Tests, "pm.test") {
		t.Fatalf("%+v", r)
	}
	if r := c.Requests[1]; r.Body.Mode != "json" || r.Auth != "bearer" || r.AuthData.Token != "{{tok}}" {
		t.Fatalf("%+v", r)
	}
	// and back to Postman
	b, err := ExportPostman(c)
	if err != nil || !strings.Contains(string(b), `"name": "Ports"`) || !strings.Contains(string(b), `"listen": "test"`) {
		t.Fatal(err, string(b))
	}
	if again, err := Import(string(b), ""); err != nil || len(again.Requests) != 2 || again.Requests[0].Folder != "Ports" {
		t.Fatal(err, again)
	}
	oas := `openapi: 3.0.0
info: {title: Switch API}
servers: [{url: "https://{host}/rest", variables: {host: {default: 10.0.0.1}}}]
paths:
  /vlans/{id}:
    parameters: [{name: id, in: path, required: true, schema: {type: integer, example: 10}}]
    get: {summary: Get a VLAN, tags: [VLANs]}
    put:
      operationId: putVlan
      tags: [VLANs]
      parameters: [{name: dry, in: query, schema: {type: boolean}}]
      requestBody:
        content:
          application/json:
            schema: {$ref: "#/components/schemas/Vlan"}
components:
  schemas:
    Vlan: {type: object, properties: {name: {type: string, example: voice}, tag: {type: integer}}}
`
	c, err = Import(oas, "")
	if err != nil || c.Name != "Switch API" || len(c.Requests) != 2 {
		t.Fatal(err, c)
	}
	if c.Vars[0].Value != "https://10.0.0.1/rest" || c.Vars[1].Key != "id" || c.Vars[1].Value != "10" {
		t.Fatalf("%+v", c.Vars)
	}
	put := c.Requests[1]
	if put.Name != "putVlan" || put.URL != "{{baseUrl}}/vlans/{{id}}" || put.Folder != "VLANs" || put.Body.Mode != "json" || !strings.Contains(put.Body.Raw, `"name": "voice"`) {
		t.Fatalf("%+v", put)
	}
}

func TestCodeAndStore(t *testing.T) {
	tg := &Target{BaseURL: "https://sw", Auth: Auth{Type: "bearer", Secret: "TOPSECRET"}, Vars: map[string]string{"p": "1:1"}}
	r := Request{Method: "PATCH", URL: "/ports/{{p}}", Headers: []KV{{Key: "Accept", Value: "application/json"}}, Body: Body{Mode: "json", Raw: `{"d":"it's"}`}}
	for _, l := range []string{"curl", "python", "javascript", "powershell"} {
		code, err := Code(r, tg, l)
		if err != nil || strings.Contains(code, "TOPSECRET") || !strings.Contains(code, "https://sw/ports/1:1") || !strings.Contains(code, "API_TOKEN") {
			t.Fatalf("%s: %v\n%s", l, err, code)
		}
	}
	st := Store{Dir: t.TempDir()}
	c, err := st.Save(Collection{Name: "EXOS RESTCONF", Requests: []Request{{URL: "/a"}, {ID: "r1", URL: "/b"}}})
	if err != nil || c.ID != "exos-restconf" || c.Requests[0].ID != "r2" || c.Requests[0].Method != "GET" {
		t.Fatal(err, c)
	}
	if l := st.List(); len(l) != 1 || l[0].Requests != 2 {
		t.Fatal(l)
	}
	if f, err := st.Find("exos restconf"); err != nil || f.ID != c.ID {
		t.Fatal(err)
	}
	if _, err := st.Get("../x"); err == nil {
		t.Fatal("path escape")
	}
	if id := st.NewID("EXOS RESTCONF"); id != "exos-restconf-2" {
		t.Fatal(id)
	}
}
