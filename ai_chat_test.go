package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

var tinyPNG = base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nfake"))

func TestAIChatOpenAIImagesAndTurns(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": "ok"}}}})
	}))
	defer srv.Close()
	keyring.MockInit()
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	s := cfg.Get()
	s.AI = config.AISettings{Provider: "openai", BaseURL: srv.URL, Model: "m", HideHosts: true}
	cfg.Save(s)
	t.Setenv("HOME", dir)
	st, _ := session.NewStore(filepath.Join(dir, "S"))
	a := &App{cfg: cfg, store: st}
	if _, err := a.AIChat(AIChatRequest{Messages: []AIMsg{{Role: "user", Text: "x"}}}); err == nil {
		t.Fatal("no key accepted")
	}
	a.AISetKey("openai", "k")
	ans, err := a.AIChat(AIChatRequest{Agent: true, Tabs: []string{"access-sw7 (EXOS, connected)"}, DeviceType: "EXOS", Messages: []AIMsg{
		{Role: "user", Text: "ping 10.1.1.5 fails\nsnmp-server community TopSecret RO"},
		{Role: "assistant", Text: "Check the route to 10.1.1.5."},
		{Role: "user", Text: "What is this?", Images: []AIImage{{Name: "a.png", MediaType: "image/png", Data: tinyPNG}, {Name: "x", MediaType: "text/plain", Data: "eA=="}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body)
	js := string(raw)
	if strings.Contains(js, "TopSecret") || strings.Count(js, "[host-1]") != 2 || !strings.Contains(js, "data:image/png;base64,") || strings.Contains(js, "text/plain") ||
		!strings.Contains(js, "AGENT MODE") || !strings.Contains(js, "access-sw7 (EXOS, connected)") || ans.Sent != "What is this?" {
		t.Fatalf("sent %s / %q", js, ans.Sent)
	}
	if m := body["messages"].([]any); len(m) != 4 { // system + 3 turns
		t.Fatalf("%d messages", len(m))
	}
}

func TestAIChatClaudeCodePictures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "claude")
	os.WriteFile(fake, []byte("#!/bin/sh\necho \"ARGS: $*\"\necho \"CWD: $(pwd)\"\nls\necho \"STDIN: $(cat)\"\n"), 0o755)
	cfg, _ := config.Load(dir)
	s := cfg.Get()
	s.AI = config.AISettings{Provider: "claudecode", BaseURL: fake}
	cfg.Save(s)
	st, _ := session.NewStore(filepath.Join(dir, "S"))
	a := &App{cfg: cfg, store: st}
	ans, err := a.AIChat(AIChatRequest{Messages: []AIMsg{{Role: "user", Text: "first"}, {Role: "assistant", Text: "answer one"}, {Role: "user", Text: "look", Images: []AIImage{{MediaType: "image/png", Data: tinyPNG}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ans.Text, "--tools Read --allowedTools Read") || !strings.Contains(ans.Text, "picture-1.png") || !strings.Contains(ans.Text, "termwright-ai-") ||
		!strings.Contains(ans.Text, "[Earlier - you]\nanswer one") || !strings.Contains(ans.Text, "Now, my message:\nlook") {
		t.Fatalf("%s", ans.Text)
	}
	// without pictures: no tools at all
	ans, _ = a.AIChat(AIChatRequest{Messages: []AIMsg{{Role: "user", Text: "hi"}}})
	if strings.Contains(ans.Text, "--tools Read") || !strings.Contains(ans.Text, "--tools") {
		t.Fatalf("tools: %s", ans.Text)
	}
}

func TestAIUsageAndTools(t *testing.T) {
	if txt, u, err := claudeCodeResult(`{"type":"result","is_error":false,"result":"hello","usage":{"input_tokens":10,"cache_read_input_tokens":2000,"cache_creation_input_tokens":300,"output_tokens":42}}`); err != nil || txt != "hello" || u.In != 2310 || u.Out != 42 {
		t.Fatalf("%q %+v %v", txt, u, err)
	}
	if _, _, err := claudeCodeResult(`{"type":"result","is_error":true,"result":"Invalid API key"}`); err == nil {
		t.Fatal("error not reported")
	}
	if _, u, _ := claudeCodeResult(`{"type":"result","result":"x","usage":{},"modelUsage":{"claude-haiku-4-5":{"outputTokens":12},"claude-sonnet-5-5":{"outputTokens":300}}}`); u.Model != "claude-sonnet-5-5" {
		t.Fatalf("model %q", u.Model)
	}
	if txt, u, _ := claudeCodeResult("plain answer"); txt != "plain answer" || u != nil {
		t.Fatal("plain text")
	}
	sys := aiChatSystem(AIChatRequest{Agent: true, Tools: []string{"run"}})
	if !strings.Contains(sys, "run <tab name>") || strings.Contains(sys, "open <saved") || !strings.Contains(sys, "```termwright\nrun") {
		t.Fatalf("run only: %s", sys)
	}
	sys = aiChatSystem(AIChatRequest{Agent: true, Tools: []string{"run", "open", "log", "record"}, Folders: []string{"Datacenter/Rack 4"}, Perm: "auto"})
	for _, w := range []string{"find <text>", "log <tab name>: on|off", "record <tab name>", "Datacenter/Rack 4", "autonomously", "never ask for, show or type passwords"} {
		if !strings.Contains(sys, w) {
			t.Fatalf("missing %q: %s", w, sys)
		}
	}
	if sys := aiChatSystem(AIChatRequest{Agent: true, Tools: []string{"none"}}); strings.Contains(sys, "AGENT MODE") {
		t.Fatal("agent without tools")
	}
}

func TestClaudeCodeModelsAndEffort(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	junk := strings.Repeat("x", 5<<20) // across the read chunks
	os.WriteFile(bin, []byte(`a="claude-opus-4-8";b='claude-sonnet-5-5'`+junk+"`claude-opus-5-5` \"claude-opus-5\" \"claude-haiku-4-5-20251001\" \"claude-sonnet-3-7\" claude-opus-9-9-x \"claude-opus-4\" \"claude-opus-4-0\""), 0o755)
	got := strings.Join(claudeCodeModelIDs(bin), " ")
	if got != "claude-opus-5-5 claude-opus-5 claude-opus-4-8 claude-opus-4 claude-sonnet-5-5" && got != "claude-opus-5-5 claude-opus-5 claude-opus-4-8 claude-opus-4-0 claude-sonnet-5-5" {
		t.Fatalf("models: %s", got)
	}
	a := strings.Join(claudeCodeArgs("opus", "high", "sys", true, false), " ")
	if !strings.Contains(a, "--model opus --effort high") {
		t.Fatalf("args: %s", a)
	}
	// an older Claude Code without --effort: asked again without it
	fake := filepath.Join(dir, "old")
	os.WriteFile(fake, []byte("#!/bin/sh\ncase \"$*\" in *--effort*) echo \"error: unknown option '--effort'\" >&2; exit 1;; esac\necho '{\"type\":\"result\",\"result\":\"fine\",\"usage\":{}}'\n"), 0o755)
	if out, _, err := claudeCodeRun(context.Background(), fake, "", "max", "s", "q", ""); err != nil || out != "fine" {
		t.Fatalf("fallback: %q %v", out, err)
	}
	if out, _, _ := claudeCodeRun(context.Background(), fake, "", "bogus", "s", "q", ""); out != "fine" {
		t.Fatal("an unknown effort is not passed on")
	}
}

// Agent mode asks for a checklist plan for a task of several steps; Plan mode only looks and plans; Ask has neither.
func TestAIChatSystemPlan(t *testing.T) {
	r := AIChatRequest{Agent: true, Tools: []string{"run"}, Plan: "list"}
	if s := aiChatSystem(r); !strings.Contains(s, "plan [ ] <step>") || strings.Contains(s, "PLAN MODE") {
		t.Fatal("agent: no plan instructions")
	}
	r.Plan = "only"
	if s := aiChatSystem(r); !strings.Contains(s, "PLAN MODE") || !strings.Contains(s, "Run the Plan") {
		t.Fatal("plan mode")
	}
	r.Agent = false
	if s := aiChatSystem(r); strings.Contains(s, "plan [ ]") {
		t.Fatal("ask mode has no plan")
	}
}
