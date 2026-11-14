package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

func mcpLines(t *testing.T, out string) []map[string]any {
	var l []map[string]any
	for _, s := range strings.Split(strings.TrimSpace(out), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatalf("not JSON: %q", s)
		}
		l = append(l, m)
	}
	return l
}

const mcpIn = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_sessions","arguments":{"filter":"core"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_tabs","arguments":{}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"read_tab","arguments":{"tab":"nope"}}}
`

func TestMCPBridgeWithoutApp(t *testing.T) {
	var out bytes.Buffer
	if err := runMCPBridge(strings.NewReader(mcpIn), &out, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	l := mcpLines(t, out.String())
	if len(l) != 5 { // the notification gets no answer
		t.Fatalf("%d answers: %s", len(l), out.String())
	}
	if l[0]["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "termwright" {
		t.Fatalf("initialize: %v", l[0])
	}
	if n := len(l[1]["result"].(map[string]any)["tools"].([]any)); n != len(mcpTools)+len(mcpLabTools) {
		t.Fatalf("tools: %d", n)
	}
	r := l[2]["result"].(map[string]any)
	if r["isError"] != true || !strings.Contains(out.String(), "Termwright is not running") {
		t.Fatalf("call without the app: %v", r)
	}
}

func TestMCPServer(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(filepath.Join(dir, "Sessions"))
	_, err := st.Save(session.Session{Name: "core-sw1", Folder: "Datacenter/Core", Hostname: "10.10.0.1", Protocol: session.ProtoSSH2, DeviceType: "VOSS"}, "")
	if err != nil {
		t.Fatal(err)
	}
	st.Save(session.Session{Name: "edge-1", Folder: "Lab", Hostname: "10.20.0.2", Protocol: session.ProtoSSH2, DeviceType: "EXOS"}, "")
	s := cfg.Get()
	s.AI.MCP = true
	cfg.Save(s)
	a := &App{cfg: cfg, store: st}
	a.mcpApply()
	defer a.mcpStop()
	b, _ := os.ReadFile(mcpFile(dir))
	var ep mcpEndpoint
	if json.Unmarshal(b, &ep) != nil || ep.Token == "" || !strings.HasPrefix(ep.URL, "http://127.0.0.1:") {
		t.Fatalf("mcp.json: %s", b)
	}
	if fi, _ := os.Stat(mcpFile(dir)); fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("mcp.json readable by others: %v", fi.Mode())
	}
	// through the bridge
	var out bytes.Buffer
	if err := runMCPBridge(strings.NewReader(mcpIn), &out, dir); err != nil {
		t.Fatal(err)
	}
	l := mcpLines(t, out.String())
	text := func(i int) string {
		return l[i]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	}
	if !strings.Contains(text(2), "Datacenter/Core/core-sw1 · SSH2 10.10.0.1 · VOSS") || strings.Contains(text(2), "edge-1") {
		t.Fatalf("list_sessions: %q", text(2))
	}
	if !strings.Contains(text(3), "No terminal tab") {
		t.Fatalf("list_tabs: %q", text(3))
	}
	if l[4]["result"].(map[string]any)["isError"] != true {
		t.Fatalf("read_tab of a missing tab: %v", l[4])
	}
	// no token, a web page, a GET
	post := func(tok, origin string) int {
		req, _ := http.NewRequest("POST", ep.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if c := post("", ""); c != 401 {
		t.Fatalf("no token: %d", c)
	}
	if c := post("wrong", ""); c != 401 {
		t.Fatalf("wrong token: %d", c)
	}
	if c := post(ep.Token, "http://evil.example.com"); c != 403 {
		t.Fatalf("from a web page: %d", c)
	}
	if c := post(ep.Token, ""); c != 200 {
		t.Fatalf("good: %d", c)
	}
	// turned off: stopped and the file says so
	s.AI.MCP = false
	cfg.Save(s)
	a.mcpApply()
	if b, _ := os.ReadFile(mcpFile(dir)); !strings.Contains(string(b), "stopped") {
		t.Fatalf("after stop: %s", b)
	}
	if _, err := http.Post(ep.URL, "application/json", strings.NewReader("{}")); err == nil {
		t.Fatal("still listening")
	}
}

func TestMCPRisky(t *testing.T) {
	for _, c := range []string{"configure vlan 10 add ports 1", "conf t", "reboot", "no shutdown", "delete config", "clear counters", "save config", "disable port 3", "copy run start", "unconfigure switch all"} {
		if !mcpRisky.MatchString(c) {
			t.Errorf("%q should ask with a warning", c)
		}
	}
	for _, c := range []string{"show port", "show running-config", "sh vlan", "ping 10.1.1.1", "display version", "show configuration"} {
		if mcpRisky.MatchString(c) {
			t.Errorf("%q is read-only", c)
		}
	}
}

func TestMCPAddToDesktop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", "")
	os.MkdirAll(filepath.Join(home, ".config", "Claude"), 0o700)
	p := claudeDesktopConfig()
	os.WriteFile(p, []byte(`{"mcpServers":{"other":{"command":"x"}},"theme":"dark"}`), 0o600)
	a := &App{}
	if err := a.MCPAddToDesktop(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	var c map[string]any
	json.Unmarshal(b, &c)
	srv := c["mcpServers"].(map[string]any)
	tw := srv["termwright"].(map[string]any)
	if srv["other"] == nil || c["theme"] != "dark" || tw["args"].([]any)[0] != "mcp" || tw["command"] == "" {
		t.Fatalf("config: %s", b)
	}
	if _, err := os.Stat(p + ".termwright-backup"); err != nil {
		t.Fatal("no backup")
	}
	if !a.MCPState().InDesktop {
		t.Fatal("not seen as added")
	}
}

// The settings page shows whether Claude Code has the connector from its own settings file.
func TestClaudeCodeHas(t *testing.T) {
	dir := t.TempDir()
	if claudeCodeHas(dir) {
		t.Fatal("no file: not added")
	}
	_ = os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"mcpServers":{"other":{"command":"x"}}}`), 0o600)
	if claudeCodeHas(dir) {
		t.Fatal("another connector only: not added")
	}
	_ = os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"projects":{},"mcpServers":{"termwright":{"command":"/x/termwright","args":["mcp"]}}}`), 0o600)
	if !claudeCodeHas(dir) {
		t.Fatal("termwright listed: added")
	}
}
