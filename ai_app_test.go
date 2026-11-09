package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

func TestAIRedact(t *testing.T) {
	in := "username admin password 7 0822455D0A16\nenable secret 5 $1$mERr$hx5rVt7rPNoS4wqbXKX7m0\nsnmp-server community s3cr3tComm RO\n" +
		"configure snmp add community readonly lab-public\nPassword:\nrouter>\nkey: AKIAIOSFODNN7EXAMPLE\n" +
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----\nInterface 1/1 is up, mgmt 10.127.34.11 (core-sw1.lab.example.com) 00:11:22:33:44:55"
	out, n := aiRedact(in, false)
	for _, leak := range []string{"0822455D0A16", "hx5rVt7rPNoS4wqbXKX7m0", "s3cr3tComm", "lab-public", "AKIAIOSFODNN7EXAMPLE", "MIIEow"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q not removed:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "router>") || !strings.Contains(out, "Password:") || !strings.Contains(out, "10.127.34.11") || n < 6 {
		t.Fatalf("too much or too little removed (%d):\n%s", n, out)
	}
	out, _ = aiRedact("ping 10.0.0.1 from 10.0.0.2, again 10.0.0.1 at core-sw1.lab.example.com mac 00:11:22:33:44:55", true)
	if strings.Contains(out, "10.0.0") || strings.Contains(out, "example.com") || strings.Contains(out, "00:11") ||
		!strings.HasPrefix(out, "ping [host-") || strings.Count(out, out[5:13]) != 2 {
		t.Fatalf("hosts: %s", out)
	}
}

func TestAICommands(t *testing.T) {
	got := aiCommands("Use this:\n```\nshow lldp neighbor\n# comment\nshow lldp neighbor detail\n```\nor\n```bash\nshow isis adjacency\n```")
	if strings.Join(got, "|") != "show lldp neighbor|show lldp neighbor detail|show isis adjacency" {
		t.Fatalf("%q", got)
	}
}

// The three providers' request and answer formats, against fake services.
func TestAIProviders(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		got["_path"], got["_auth"] = r.URL.Path, r.Header.Get("Authorization")+r.Header.Get("x-api-key")
		switch r.URL.Path {
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"openai answer"}}]}`))
		case "/api/chat":
			_, _ = w.Write([]byte(`{"message":{"content":"ollama answer"}}`))
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"old","created":1},{"id":"text-embedding-3","created":5},{"id":"new","created":9}]}`))
		default:
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
		}
	}))
	defer srv.Close()
	ctx := t.Context()
	s := config.AISettings{Provider: "openai", BaseURL: srv.URL + "/v1"}
	if out, err := aiChat(ctx, s, "k1", "m", "sys", "hi"); err != nil || out != "openai answer" || got["_auth"] != "Bearer k1" || got["_path"] != "/v1/chat/completions" {
		t.Fatalf("openai: %q %v %v", out, err, got)
	}
	s = config.AISettings{Provider: "ollama", BaseURL: srv.URL}
	if out, err := aiChat(ctx, s, "", "llama", "sys", "hi"); err != nil || out != "ollama answer" || got["stream"] != false {
		t.Fatalf("ollama: %q %v %v", out, err, got)
	}
	s = config.AISettings{Provider: "openai", BaseURL: srv.URL}
	a := &App{}
	_ = a
	if _, err := aiChat(ctx, config.AISettings{Provider: "openai", BaseURL: srv.URL + "/wrong"}, "k", "m", "s", "u"); err == nil || !strings.Contains(err.Error(), "check the API key") {
		t.Fatalf("error text: %v", err)
	}
}

// AIAsk end to end against a simulated Ollama: the model is found, the
// secret never arrives, and the commands come back.
func TestAIAskEndToEnd(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3.1:8b"}]}`))
		case "/api/chat":
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = w.Write([]byte(`{"message":{"content":"The port is down.\n` + "```" + `\nshow interfaces gigabitEthernet 1/1\n` + "```" + `"}}`))
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Get()
	s.AI = config.AISettings{Provider: "ollama", BaseURL: srv.URL}
	_ = cfg.Save(s)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	ans, err := a.AIAsk(AIRequest{Kind: "explain", Text: "%LINK-3-DOWN 1/1\nsnmp-server community TopSecret RO", DeviceType: "VOSS", Protocol: "SSH2"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "TopSecret") || !strings.Contains(string(raw), "VOSS") || body["model"] != "llama3.1:8b" {
		t.Fatalf("sent: %s", raw)
	}
	if len(ans.Commands) != 1 || ans.Commands[0] != "show interfaces gigabitEthernet 1/1" || ans.Removed != 1 || ans.Model != "llama3.1:8b" {
		t.Fatalf("answer %+v", ans)
	}
}

func TestAIQuestion(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		got = b.Messages[len(b.Messages)-1].Content
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": "ok"}})
	}))
	defer srv.Close()
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	s := cfg.Get()
	s.AI = config.AISettings{Provider: "ollama", BaseURL: srv.URL, Model: "m", HideHosts: true}
	_ = cfg.Save(s)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	ans, err := a.AIAsk(AIRequest{Kind: "explain", Question: "Why can 10.1.1.5 not reach the server?", Text: "ping 10.1.1.5 failed\nsnmp-server community TopSecret RO", DeviceType: "EXOS"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "My question: Why can [host-1] not reach") || !strings.Contains(got, "ping [host-1] failed") || strings.Contains(got, "TopSecret") || strings.Contains(ans.Sent, "My question") {
		t.Fatalf("sent %q / %q", got, ans.Sent)
	}
	// a question alone
	if _, err := a.AIAsk(AIRequest{Kind: "explain", Question: "What does LACP do?"}); err != nil || got != "My question: What does LACP do?" {
		t.Fatalf("question only: %q %v", got, err)
	}
	// a very long text is cut to its end
	long := strings.Repeat("x", aiMaxText+500) + "\nTHE END"
	a.AIAsk(AIRequest{Kind: "explain", Text: long})
	if !strings.Contains(got, "THE END") || len(got) > aiMaxText+1000 {
		t.Fatalf("long text: %d", len(got))
	}
}

func TestAIClaudeCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	dir := t.TempDir()
	// a stand-in for the claude program: prints its arguments and what it read
	fake := filepath.Join(dir, "claude")
	os.WriteFile(fake, []byte("#!/bin/sh\necho \"ARGS: $*\"\necho \"STDIN: $(cat)\"\n"), 0o755)
	cfg, _ := config.Load(dir)
	s := cfg.Get()
	s.AI = config.AISettings{Provider: "claudecode", BaseURL: fake}
	_ = cfg.Save(s)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	if !a.AIState().Ready {
		t.Fatal("not ready with the program found")
	}
	ans, err := a.AIAsk(AIRequest{Kind: "explain", Question: "Why is port 3 down?", Text: "3 Default D R\nsnmp-server community TopSecret RO", DeviceType: "EXOS"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ans.Text, "-p --output-format json") || !strings.Contains(ans.Text, "--tools") || !strings.Contains(ans.Text, "Device type: EXOS") ||
		!strings.Contains(ans.Text, "STDIN: My question: Why is port 3 down?") || strings.Contains(ans.Text, "TopSecret") || ans.Model != "Claude Code" || strings.Contains(ans.Text, "--model") {
		t.Fatalf("answer %q (%s)", ans.Text, ans.Model)
	}
	s.AI.Model = "opus"
	_ = cfg.Save(s)
	if ans, _ := a.AIAsk(AIRequest{Kind: "suggest", Text: "show lldp neighbours"}); !strings.Contains(ans.Text, "--model opus") {
		t.Fatalf("model: %q", ans.Text)
	}
	// not signed in
	os.WriteFile(fake, []byte("#!/bin/sh\necho 'Invalid API key · Please run /login' >&2\nexit 1\n"), 0o755)
	if _, err := a.AIAsk(AIRequest{Kind: "suggest", Text: "x"}); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Fatalf("login error: %v", err)
	}
	// not installed
	s.AI.BaseURL = filepath.Join(dir, "nope")
	_ = cfg.Save(s)
	if a.AIState().Ready {
		t.Fatal("ready without the program")
	}
}

func TestFindClaudeCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	root := t.TempDir()
	// the copy Claude Desktop keeps: <support>/Claude/claude-code/<version>/claude
	for _, v := range []string{"2.0.1", "2.0.9"} {
		d := filepath.Join(root, "claude-code", v)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "claude"), []byte("#!/bin/sh\necho '"+v+" (Claude Code)'\n"), 0o755)
		time.Sleep(20 * time.Millisecond)
	}
	os.MkdirAll(filepath.Join(root, "logs"), 0o755)
	os.WriteFile(filepath.Join(root, "logs", "claude"), []byte("not a program"), 0o644)
	p := findClaudeIn(root, 6)
	if !strings.HasSuffix(p, filepath.Join("2.0.9", "claude")) || !isClaudeCode(p) {
		t.Fatalf("found %q", p)
	}
	os.WriteFile(filepath.Join(root, "other"), []byte("#!/bin/sh\necho something else\n"), 0o755)
	if isClaudeCode(filepath.Join(root, "other")) {
		t.Fatal("another program taken for Claude Code")
	}
}
