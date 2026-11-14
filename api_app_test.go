package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/session"
)

// The local API: only with its setting and the key; the lab, its tools, a tool run, the traffic report.
func TestLabAPI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(filepath.Join(dir, "Sessions"))
	s := cfg.Get()
	s.AI.API = true
	cfg.Save(s)
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = true, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	emuLab.f.Host = "Lab/kvm-1"
	emuLab.f.Images = []emu.Image{{ID: "i1", Name: "EXOS-VM", NICs: 9, PortName: "{n}"}}
	emuLab.f.Lab = &emu.Lab{ID: "a1", VMs: []emu.VM{{ID: "v1", Name: "core-1", Image: "i1"}}}
	a.mcpApply()
	defer a.mcpStop()
	b, _ := os.ReadFile(mcpFile(dir))
	var ep mcpEndpoint
	_ = json.Unmarshal(b, &ep)
	base := strings.TrimSuffix(ep.URL, "/mcp") + "/api/v1/lab"
	call := func(method, path, key, body string) (int, string) {
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(out)
	}
	if c, _ := call("GET", "/tools", "wrong", ""); c != 401 {
		t.Fatalf("a wrong key: %d", c)
	}
	if c, out := call("GET", "/tools", ep.Token, ""); c != 200 || !strings.Contains(out, `"traffic"`) || !strings.Contains(out, `"link_check"`) {
		t.Fatalf("tools %d %s", c, out)
	}
	if c, out := call("POST", "/tools/images", ep.Token, `{}`); c != 200 || !strings.Contains(out, "EXOS-VM") {
		t.Fatalf("a tool %d %s", c, out)
	}
	if c, out := call("POST", "/tools/format_disk", ep.Token, `{}`); c != 404 || !strings.Contains(out, "No lab tool") || !strings.Contains(out, `"status":404`) {
		t.Fatalf("an unknown tool %d %s", c, out)
	}
	if c, out := call("GET", "/traffic/report?format=csv", ep.Token, ""); c != 200 || !strings.HasPrefix(out, "stream,from,to") {
		t.Fatalf("report %d %s", c, out)
	}
	if c, _ := call("POST", "/traffic/streams", ep.Token, `not json`); c != 400 {
		t.Fatalf("not JSON: %d", c)
	}
	if c, out := call("POST", "/traffic/streams", ep.Token, `{"from":"x","color":"red"}`); c != 400 || !strings.Contains(out, "color") {
		t.Fatalf("an unknown field: %d %s", c, out)
	}
	if c, _ := call("DELETE", "/traffic", ep.Token, ""); c != 405 {
		t.Fatalf("a wrong method: %d", c)
	}
	if c, out := call("DELETE", "/traffic/streams/nope", ep.Token, ""); c != 404 || !strings.Contains(out, "No stream nope") {
		t.Fatalf("no such stream: %d %s", c, out)
	}
	req0, _ := http.NewRequest("GET", strings.TrimSuffix(base, "/lab")+"/openapi.json", nil)
	if resp, err := http.DefaultClient.Do(req0); err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("the OpenAPI document without a key: %v", resp)
	}
	// the connector stays off while only the API is on
	req, _ := http.NewRequest("POST", ep.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer "+ep.Token)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 403 {
		t.Fatalf("the connector answered with only the API on: %v", resp)
	}
	s.AI.API = false
	cfg.Save(s)
	if c, _ := call("GET", "/tools", ep.Token, ""); c != 403 {
		t.Fatalf("the API answered while off: %d", c)
	}
}

// termwright lab … → the API request.
func TestLabCLIRequest(t *testing.T) {
	for _, c := range []struct {
		args         []string
		method, path string
		body         map[string]any
	}{
		{[]string{"traffic", "add_port", "switch=leaf-1", "port=5", "vlan=10", "groups=239.1.1.1,239.1.1.2"}, "POST", "/tools/traffic", map[string]any{"action": "add_port", "switch": "leaf-1", "port": "5", "vlan": 10.0, "groups": []any{"239.1.1.1", "239.1.1.2"}}},
		{[]string{"traffic", "start", "voice", "video"}, "POST", "/tools/traffic", map[string]any{"action": "start", "streams": []string{"voice", "video"}}},
		{[]string{"traffic", "report", "--csv"}, "GET", "/traffic/report?format=csv", nil},
		{[]string{"power", "action=start", "switches=a,b"}, "POST", "/tools/power", map[string]any{"action": "start", "switches": []any{"a", "b"}}},
		{[]string{"traffic", "add_stream", `mods=[{"field":"srcIp","count":4}]`}, "POST", "/tools/traffic", map[string]any{"action": "add_stream", "mods": []any{map[string]any{"field": "srcIp", "count": 4.0}}}},
	} {
		m, p, b, _ := labCLIRequest(c.args)
		if m != c.method || p != c.path || c.body != nil && !reflect.DeepEqual(b, c.body) {
			t.Fatalf("%v → %s %s %#v", c.args, m, p, b)
		}
	}
	if _, p, b, _ := labCLIRequest([]string{"nope"}); p != "" || b["error"] == nil {
		t.Fatal("an unknown tool")
	}
}
