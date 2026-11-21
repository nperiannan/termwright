package integrations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQTestSubmit(t *testing.T) {
	var log map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "bearer tok" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/api/v3/projects/7/search":
			io.WriteString(w, `{"items":[{"id":55,"pid":"TC-9"}]}`)
		case r.URL.Path == "/api/v3/projects/7/test-runs" && r.URL.Query().Get("parentId") == "3":
			io.WriteString(w, `{"id":81,"pid":"TR-81"}`)
		case r.URL.Path == "/api/v3/projects/7/test-runs/81/auto-test-logs":
			json.NewDecoder(r.Body).Decode(&log)
			io.WriteString(w, `{}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := QTestClient{Cfg: QTest{BaseURL: srv.URL, Project: 7, Cycle: 3}, Token: "tok"}
	pid, err := c.Submit(context.Background(), QTestResult{TestCase: "TC-9", Name: "vlan", Passed: false, Start: time.Now(), End: time.Now(), Defect: "NET-1", Attach: []Attachment{{Name: "r.html", ContentType: "text/html", Data: []byte("x")}}})
	if err != nil || pid != "TR-81" || log["status"] != "FAILED" || !strings.HasPrefix(log["note"].(string), "Defect: NET-1") {
		t.Fatal(pid, err, log)
	}
	if _, err := (QTestClient{Cfg: QTest{BaseURL: srv.URL, Project: 7}, Token: "bad"}).Check(context.Background()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatal(err)
	}
}

func TestJiraCreate(t *testing.T) {
	var got map[string]map[string]any
	attached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/rest/api/2/issue":
			json.NewDecoder(r.Body).Decode(&got)
			io.WriteString(w, `{"key":"NET-5"}`)
		case "/rest/api/2/issue/NET-5/attachments":
			attached = r.Header.Get("X-Atlassian-Token") == "no-check"
			io.WriteString(w, `[]`)
		}
	}))
	defer srv.Close()
	c := JiraClient{Cfg: Jira{BaseURL: srv.URL, User: "me@example.com", Project: "NET"}, Token: "t"}
	key, link, err := c.Create(context.Background(), Defect{Summary: "VLAN test failed", Attach: []Attachment{{Name: "a.txt", Data: []byte("x")}}})
	if err != nil || key != "NET-5" || link != srv.URL+"/browse/NET-5" || !attached || got["fields"]["issuetype"].(map[string]any)["name"] != "Bug" {
		t.Fatal(key, link, err, attached, got)
	}
}

func TestGitArgs(t *testing.T) {
	a, e := GitArgs(Git{Host: "https://github.com/", User: "me"}, "secret")
	if len(a) != 4 || !strings.HasPrefix(a[1], "credential.https://github.com.helper=") || strings.Contains(strings.Join(a, " "), "secret") || e[1] != "TW_GIT_TOKEN=secret" {
		t.Fatal(a, e)
	}
	if a, _ := GitArgs(Git{Host: "x"}, ""); a != nil {
		t.Fatal(a)
	}
}
