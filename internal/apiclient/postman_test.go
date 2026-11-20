package apiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSetNextRequestLoopAndSkip(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"path":"` + r.URL.Path + `","auth":"` + r.Header.Get("Authorization") + `"}`))
	}))
	defer srv.Close()
	reqs := []Request{
		{ID: "a", Name: "Create", Method: "POST", URL: srv.URL + "/c",
			PreScript: `var n = parseInt(pm.collectionVariables.get("n") || "0") + 1; pm.collectionVariables.set("n", n);`,
			Tests:     `if (parseInt(pm.collectionVariables.get("n")) < 3) postman.setNextRequest("Create"); else pm.execution.setNextRequest(null);`},
		{ID: "b", Name: "Never", URL: srv.URL + "/never"},
	}
	res := RunCollection(context.Background(), reqs, &Target{}, RunOptions{})
	if len(res.Steps) != 3 || hits.Load() != 3 || res.Failed != 0 {
		t.Fatalf("%+v hits %d", res, hits.Load())
	}
	skip := Request{Name: "S", URL: srv.URL + "/s", PreScript: `pm.execution.skipRequest();`}
	if r := Run(context.Background(), skip, &Target{}); !r.Skipped || hits.Load() != 3 {
		t.Fatalf("%+v", r)
	}
}

func TestOwnAuthPathVarsAndSendRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/r" {
			http.Redirect(w, r, "/vlans/10", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{"path":"` + r.URL.Path + `","auth":"` + r.Header.Get("Authorization") + `","key":"` + r.Header.Get("X-K") + `"}`))
	}))
	defer srv.Close()
	tg := &Target{Vars: map[string]string{"tok": "s3cr3t", "id": "10"}}
	r := Request{Method: "GET", URL: srv.URL + "/vlans/:id", PathVars: []KV{{Key: "id", Value: "{{id}}"}}, Auth: "bearer", AuthData: &ReqAuth{Token: "{{tok}}"}}
	res := Run(context.Background(), r, tg)
	if !strings.Contains(res.Body, `"path":"/vlans/10"`) || !strings.Contains(res.Body, "Bearer s3cr3t") || strings.Contains(strings.Join(func() []string {
		var l []string
		for _, h := range res.ReqHeaders {
			l = append(l, h.Value)
		}
		return l
	}(), " "), "s3cr3t") {
		t.Fatalf("%+v", res)
	}
	// the collection's API key for a request that inherits
	tg.CollAuth, tg.CollAuthData = "apikey", &ReqAuth{Key: "X-K", Value: "{{tok}}"}
	if res := Run(context.Background(), Request{URL: srv.URL + "/x"}, tg); !strings.Contains(res.Body, `"key":"s3cr3t"`) {
		t.Fatalf("%+v", res)
	}
	// no redirects
	if res := Run(context.Background(), Request{URL: srv.URL + "/r", Settings: &Settings{NoRedirects: true}}, &Target{}); res.Status != 302 {
		t.Fatalf("%+v", res)
	}
	// pm.sendRequest, pm.environment.set
	s := Request{URL: srv.URL + "/main", PreScript: `pm.sendRequest("` + srv.URL + `/token", function (err, res) { pm.environment.set("got", res.json().path); });`}
	res = Run(context.Background(), s, &Target{})
	if res.EnvSet["got"] != "/token" || res.Status != 200 {
		t.Fatalf("%+v", res)
	}
	if PathVarNames("https://h/a/:x/b/:y?q=:z")[1] != "y" {
		t.Fatal("path var names")
	}
}

func TestEnvironmentImportExport(t *testing.T) {
	e, err := ImportEnvironment(`{"name":"dl2r1","values":[{"key":"ep1-environment","value":"10.1.1.1","enabled":true},{"key":"Bearer","value":"x","type":"secret","enabled":true}],"_postman_variable_scope":"environment"}`, "")
	if err != nil || e.Name != "dl2r1" || len(e.Vars) != 2 || !e.Vars[1].Secret {
		t.Fatal(err, e)
	}
	if !IsEnvironment(`{"name":"a","values":[]}`) || IsEnvironment(`{"info":{},"item":[]}`) {
		t.Fatal("IsEnvironment")
	}
	st := EnvStore{Dir: t.TempDir()}
	saved, err := st.Save(e)
	if err != nil || saved.Vars[1].Value != "" {
		t.Fatal(err, saved)
	}
	if got, _ := st.Find("DL2R1"); got.ID != saved.ID || len(st.List()) != 1 {
		t.Fatal(got)
	}
	if g, _ := st.Get(GlobalsID); g.Name != "Globals" {
		t.Fatal(g)
	}
	if !strings.Contains(string(ExportEnvironment(saved)), `"type": "secret"`) {
		t.Fatal("export")
	}
	// typed secrets are not saved in a collection
	cs := Store{Dir: t.TempDir()}
	c, _ := cs.Save(Collection{Name: "c", Requests: []Request{{URL: "/", Auth: "bearer", AuthData: &ReqAuth{Token: "typed"}}, {URL: "/", Auth: "bearer", AuthData: &ReqAuth{Token: "{{tok}}"}}}})
	if c.Requests[0].AuthData.Token != "" || c.Requests[1].AuthData.Token != "{{tok}}" {
		t.Fatal(c.Requests)
	}
}
