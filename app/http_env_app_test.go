package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/termwright/termwright/internal/apiclient"
	"github.com/termwright/termwright/internal/config"
)

// Environments: a Postman environment imports with its secret in the credential store, wins over the
// collection's variables, scripts keep pm.environment.set values, and the variables list says where
// each value comes from without giving secrets.
func TestHTTPEnvironments(t *testing.T) {
	keyring.MockInit()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"auth":"`+r.Header.Get("Authorization")+`","path":"`+r.URL.Path+`"}`)
	}))
	defer srv.Close()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{cfg: cfg}
	e, err := a.HTTPImportEnvironment(`{"name":"dl2r1","values":[{"key":"ep1","value":"`+strings.TrimPrefix(srv.URL, "http://")+`","enabled":true},
		{"key":"Bearer","value":"tok-987654","type":"secret","enabled":true}],"_postman_variable_scope":"environment"}`, "")
	if err != nil || len(e.Vars) != 2 || e.Vars[1].Value != "" || !e.Vars[1].HasValue {
		t.Fatal(err, e)
	}
	c, err := a.HTTPSaveCollection(apiclient.Collection{Name: "RADIUS", Vars: []apiclient.KV{{Key: "ep1", Value: "nowhere.invalid"}},
		Auth: "bearer", AuthData: &apiclient.ReqAuth{Token: "{{Bearer}}"},
		Requests: []apiclient.Request{{Name: "Create", Method: "GET", URL: "http://{{ep1}}/modify", Tests: `pm.environment.set("seen", pm.response.json().path);`}}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.HTTPSend(HTTPSendRequest{Collection: c.ID, Environment: "dl2r1", Request: c.Requests[0]})
	if err != nil || res.Status != 200 || !strings.Contains(res.Body, "Bearer tok-987654") {
		t.Fatal(err, res)
	}
	for _, h := range res.ReqHeaders {
		if strings.Contains(h.Value, "tok-987654") {
			t.Fatal("secret shown", h)
		}
	}
	envs := a.HTTPEnvironments()
	if len(envs) != 2 || envs[0].ID != apiclient.GlobalsID || envs[1].Vars[2].Key != "seen" || envs[1].Vars[2].Value != "/modify" {
		t.Fatalf("%+v", envs)
	}
	vs, err := a.HTTPVariables(HTTPSendRequest{Collection: c.ID, Environment: e.ID, Vars: map[string]string{"x": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]HTTPVar{}
	for _, v := range vs {
		got[v.Name] = v
	}
	if got["ep1"].Source != "environment" || got["Bearer"].Value != "" || !got["Bearer"].Secret || got["x"].Source != "local" {
		t.Fatalf("%+v", vs)
	}
	// saving with an empty secret keeps it
	envs[1].Vars[0].Value = srv.URL
	if _, err := a.HTTPSaveEnvironment(envs[1]); err != nil {
		t.Fatal(err)
	}
	if res, _ := a.HTTPSend(HTTPSendRequest{Collection: c.ID, Environment: e.ID, Request: apiclient.Request{URL: "{{ep1}}/x"}}); !strings.Contains(res.Body, "tok-987654") {
		t.Fatal(res)
	}
	if out, err := a.httpTool(t.Context(), map[string]any{"action": "environments"}, nil); err != nil || strings.Contains(out, "tok-987654") || !strings.Contains(out, "(secret, set)") {
		t.Fatal(err, out)
	}
	if l := a.HTTPCookies(""); l == nil {
		t.Fatal("cookies")
	}
}
