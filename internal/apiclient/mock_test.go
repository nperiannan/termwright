package apiclient

import (
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestMock(t *testing.T) {
	c := Collection{ID: "c", Name: "Lab", Requests: []Request{
		{Method: "GET", URL: "{{baseUrl}}/rest/vlans/:id", Examples: []Example{{Name: "ok", Status: 200, Body: `{"id":7}`}}},
		{Method: "GET", URL: "https://sw/rest/vlans/default", Examples: []Example{{Name: "d", Status: 200, Body: `{"id":1}`}}},
		{Method: "POST", URL: "/rest/vlans", Examples: []Example{{Name: "made", Status: 201, Body: `{"id":9}`}, {Name: "bad", Status: 400, Body: `{}`}}},
		{Method: "DELETE", URL: "/rest/x"},
	}}
	m, err := NewMock(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(0); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	get := func(meth, p, ex string) (int, string) {
		rq, _ := http.NewRequest(meth, fmt.Sprintf("http://127.0.0.1:%d%s", m.Port, p), nil)
		if ex != "" {
			rq.Header.Set("x-mock-example", ex)
		}
		r, err := http.DefaultClient.Do(rq)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, string(b)
	}
	if s, b := get("GET", "/rest/vlans/42", ""); s != 200 || b != `{"id":7}` {
		t.Fatal(s, b)
	}
	if s, b := get("GET", "/rest/vlans/default", ""); s != 200 || b != `{"id":1}` {
		t.Fatal("specific path first", s, b)
	}
	if s, _ := get("POST", "/rest/vlans", "bad"); s != 400 {
		t.Fatal(s)
	}
	if s, _ := get("DELETE", "/rest/x", ""); s != 404 {
		t.Fatal(s)
	}
	if h, mi := m.Counts(); h != 3 || mi != 1 {
		t.Fatal(h, mi)
	}
}
