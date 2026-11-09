package main

import (
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
