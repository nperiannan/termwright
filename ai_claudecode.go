package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The "claudecode" AI service: the Claude Code program on this computer,
// signed in with the user's own Claude account - no API key. Termwright runs
// it once per question in print mode, with no tools (it cannot read files or
// run commands), and reads the answer.

// claudeCodePath finds the claude program: the path in the settings, then
// PATH, then the usual install places (a program started from the Dock or
// Start menu does not see the shell's PATH).
func claudeCodePath(given string) (string, error) {
	if g := strings.TrimSpace(given); g != "" {
		if fi, err := os.Stat(g); err == nil && !fi.IsDir() {
			return g, nil
		}
		return "", fmt.Errorf("the Claude Code program %s was not found", g)
	}
	names := []string{"claude"}
	if runtime.GOOS == "windows" {
		names = []string{"claude.exe", "claude.cmd"}
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	home, _ := os.UserHomeDir()
	var dirs []string
	switch runtime.GOOS {
	case "windows":
		dirs = []string{filepath.Join(os.Getenv("APPDATA"), "npm"), filepath.Join(home, ".local", "bin"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "claude")}
	default:
		dirs = []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, ".claude", "local"), "/opt/homebrew/bin", "/usr/local/bin",
			filepath.Join(home, ".npm-global", "bin"), filepath.Join(home, ".bun", "bin"), "/usr/bin"}
	}
	for _, d := range dirs {
		for _, n := range names {
			p := filepath.Join(d, n)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, nil
			}
		}
	}
	if runtime.GOOS != "windows" { // as the login shell sees it (nvm, asdf, …)
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/zsh"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, sh, "-lc", "command -v claude").Output(); err == nil {
			if p := strings.TrimSpace(string(out)); p != "" && filepath.IsAbs(p) {
				return p, nil
			}
		}
	}
	return "", errors.New("Claude Code was not found on this computer - install it (claude.com/claude-code) and sign in once by running claude in a terminal, or give its path")
}

// claudeCodeArgs: print mode, plain text, no tools, nothing saved, only our system prompt.
func claudeCodeArgs(model, sys string, withTools bool) []string {
	a := []string{"-p", "--output-format", "text", "--no-session-persistence", "--strict-mcp-config", "--system-prompt", sys}
	if withTools {
		a = append(a, "--tools", "")
	} else {
		a = append(a, "--disallowedTools", "Bash,Edit,Write,MultiEdit,NotebookEdit,WebFetch,WebSearch,Read,Glob,Grep,Task")
	}
	if model != "" {
		a = append(a, "--model", model)
	}
	return a
}

var claudeCodeTimeout = 3 * time.Minute

func claudeCodeChat(ctx context.Context, path, model, sys, user string) (string, error) {
	bin, err := claudeCodePath(path)
	if err != nil {
		return "", err
	}
	run := func(withTools bool) (string, string, error) {
		c, cancel := context.WithTimeout(ctx, claudeCodeTimeout)
		defer cancel()
		cmd := exec.CommandContext(c, bin, claudeCodeArgs(model, sys, withTools)...)
		cmd.Dir = os.TempDir() // not a project folder: nothing of the user's is in reach
		cmd.Stdin = strings.NewReader(user)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		hideWindow(cmd)
		err := cmd.Run()
		if c.Err() == context.DeadlineExceeded {
			return "", "", errors.New("Claude Code did not answer in time")
		}
		return out.String(), errb.String(), err
	}
	out, stderr, err := run(true)
	if err != nil && strings.Contains(stderr+out, "unknown option") { // an older Claude Code without --tools
		out, stderr, err = run(false)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = strings.TrimSpace(out)
		}
		low := strings.ToLower(msg)
		if strings.Contains(low, "login") || strings.Contains(low, "log in") || strings.Contains(low, "not authenticated") || strings.Contains(low, "/login") {
			return "", errors.New("Claude Code is not signed in - run claude in a terminal once and sign in with your Claude account")
		}
		if len(msg) > 400 {
			msg = msg[:400] + "…"
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New("Claude Code: " + msg)
	}
	if strings.TrimSpace(out) == "" {
		return "", errors.New("Claude Code gave no answer")
	}
	return strings.TrimSpace(out), nil
}

func runQuiet(ctx context.Context, bin string, args ...string) error {
	c := exec.CommandContext(ctx, bin, args...)
	hideWindow(c)
	return c.Run()
}

func runOutput(ctx context.Context, bin string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, bin, args...)
	hideWindow(c)
	out, err := c.CombinedOutput()
	return string(out), err
}
