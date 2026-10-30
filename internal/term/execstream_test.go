package term

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// A background command on a tab's connection: stdin reaches it, its output
// comes back line by line, and the exit status is returned. The tab itself
// shows none of it.
func TestExecStream(t *testing.T) {
	m, em, tab := connectedTab(t)
	defer m.CloseAll()
	before := em.output()

	var mu sync.Mutex
	var lines []string
	collect := func(l string) { mu.Lock(); lines = append(lines, l); mu.Unlock() }

	// stdin: the first line is dropped (the "sudo password"), the second is echoed back
	cmd := "sh -c " + ShellQuote(`read -r a; read -r b; echo "got:$b"; echo err-line >&2; exit 3`)
	code, err := m.ExecStream(context.Background(), tab, cmd, []byte("first\nsecret value\n"), collect)
	if err != nil || code == 0 { // the test server reports any failure as 1
		t.Fatalf("code %d err %v", code, err)
	}
	mu.Lock()
	got := strings.Join(lines, "|")
	mu.Unlock()
	if !strings.Contains(got, "got:secret value") || !strings.Contains(got, "err-line") {
		t.Fatalf("lines %q", got)
	}
	if after := em.output(); strings.Contains(after[len(before):], "got:") {
		t.Fatal("the background command showed up in the terminal tab")
	}

	code, err = m.ExecStream(context.Background(), tab, "true", nil, collect)
	if err != nil || code != 0 {
		t.Fatalf("true: %d %v", code, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.ExecStream(ctx, tab, "sleep 30", nil, collect); err == nil {
		t.Fatal("a cancelled command returned no error")
	}
	if _, err := m.ExecStream(context.Background(), "nope", "true", nil, collect); err == nil {
		t.Fatal("unknown tab accepted")
	}
}

func TestShellQuote(t *testing.T) {
	if got := ShellQuote(`a'b c`); got != `'a'\''b c'` {
		t.Fatal(got)
	}
}

func TestLoginPasswordKept(t *testing.T) {
	m, _, tab := connectedTab(t) // logs in by typing the password "pw" at the prompt
	defer m.CloseAll()
	if pw, ok := m.LoginPassword(tab); !ok || pw != "pw" {
		t.Fatalf("the typed login password was not kept: %q %v", pw, ok)
	}
	if _, ok := m.LoginPassword("nope"); ok {
		t.Fatal("unknown tab has a password")
	}
}
