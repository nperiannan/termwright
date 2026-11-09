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
	// version folders: nvm, the native installer, and the copy Claude Desktop keeps for its Code tab
	var globs []string
	switch runtime.GOOS {
	case "windows":
		globs = []string{filepath.Join(os.Getenv("APPDATA"), "Claude", "claude-code", "*", "claude.exe"), filepath.Join(os.Getenv("LOCALAPPDATA"), "AnthropicClaude", "*", "claude.exe")}
	default:
		globs = []string{filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "claude"), filepath.Join(home, ".volta", "bin", "claude"),
			filepath.Join(home, ".local", "share", "claude", "versions", "*"), filepath.Join(home, ".asdf", "shims", "claude"), filepath.Join(home, ".npm", "bin", "claude")}
	}
	if p := newestFile(globs); p != "" {
		return p, nil
	}
	vmOnly := false
	for _, root := range claudeDesktopDirs(home) {
		if p := findClaudeIn(root, 6); p != "" {
			if isClaudeCode(p) {
				return p, nil
			}
			// claude-code-vm: the Linux copy Claude Desktop runs in its own sandbox - not for this computer
			vmOnly = vmOnly || strings.Contains(p, "claude-code-vm")
		}
	}
	if runtime.GOOS != "windows" { // as the user's shell sees it (nvm and friends set in .zshrc)
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/zsh"
		}
		for _, flags := range []string{"-lc", "-ilc"} {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			out, _ := exec.CommandContext(ctx, sh, flags, "command -v claude").Output()
			cancel()
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if p := strings.TrimSpace(lines[len(lines)-1]); filepath.IsAbs(p) && isExec(p) {
				return p, nil
			}
		}
	}
	if vmOnly {
		return "", errors.New("only Claude Desktop's own copy of Claude Code was found - it runs inside Claude Desktop's sandbox, not on this computer. Install Claude Code (curl -fsSL https://claude.ai/install.sh | bash), run claude once to sign in, then press Check")
	}
	return "", errors.New("Claude Code was not found on this computer - install it (claude.com/claude-code) and sign in once by running claude in a terminal, or give its path")
}

// newestFile is the most recently changed executable file the patterns match.
func newestFile(patterns []string) string {
	best, bestT := "", time.Time{}
	for _, g := range patterns {
		m, _ := filepath.Glob(g)
		for _, p := range m {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && isExec(p) && fi.ModTime().After(bestT) {
				best, bestT = p, fi.ModTime()
			}
		}
	}
	return best
}

// isClaudeCode: the program answers --version as Claude Code does.
func isClaudeCode(p string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, p, "--version")
	hideWindow(c)
	out, err := c.Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), "claude code")
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || fi.Mode().Perm()&0o111 != 0
}

// claudeDesktopDirs: where Claude Desktop keeps its files (and its own Claude Code).
func claudeDesktopDirs(home string) []string {
	switch runtime.GOOS {
	case "windows":
		return []string{filepath.Join(os.Getenv("APPDATA"), "Claude"), filepath.Join(os.Getenv("LOCALAPPDATA"), "AnthropicClaude")}
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "Claude"), "/Applications/Claude.app/Contents/Resources", filepath.Join(home, "Applications", "Claude.app", "Contents", "Resources")}
	}
	return []string{filepath.Join(home, ".config", "Claude")}
}

// findClaudeIn looks a few folders deep for the newest claude program.
func findClaudeIn(root string, depth int) string {
	name := "claude"
	if runtime.GOOS == "windows" {
		name = "claude.exe"
	}
	best, bestT := "", time.Time{}
	base := strings.Count(filepath.Clean(root), string(filepath.Separator))
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if strings.Count(p, string(filepath.Separator))-base >= depth || strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "Cache" || d.Name() == "IndexedDB" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == name && isExec(p) {
			if fi, err := d.Info(); err == nil && fi.ModTime().After(bestT) {
				best, bestT = p, fi.ModTime()
			}
		}
		return nil
	})
	return best
}

// ClaudeCodeFind tells the settings page where Claude Code is (given: the path typed, "" = search).
func (a *App) ClaudeCodeFind(given string) (string, error) { return claudeCodePath(given) }

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
