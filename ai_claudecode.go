package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// claudeCodeArgs: print mode, plain text, nothing saved, only our system
// prompt, and no tools - or only Read, for pictures in their own folder.
func claudeCodeArgs(model, effort, sys string, withTools, read bool) []string {
	a := []string{"-p", "--output-format", "json", "--no-session-persistence", "--strict-mcp-config", "--system-prompt", sys}
	switch {
	case withTools && read:
		a = append(a, "--tools", "Read", "--allowedTools", "Read")
	case withTools:
		a = append(a, "--tools", "")
	default:
		dis := "Bash,Edit,Write,MultiEdit,NotebookEdit,WebFetch,WebSearch,Glob,Grep,Task"
		if !read {
			dis += ",Read"
		}
		a = append(a, "--disallowedTools", dis)
	}
	if model != "" {
		a = append(a, "--model", model)
	}
	if effort != "" {
		a = append(a, "--effort", effort)
	}
	return a
}

// ClaudeCodeEfforts are the effort levels Claude Code takes ("" = the model's default).
var ClaudeCodeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

func validEffort(e string) string {
	for _, x := range ClaudeCodeEfforts {
		if e == x {
			return e
		}
	}
	return ""
}

var claudeCodeTimeout = 3 * time.Minute

func claudeCodeChat(ctx context.Context, path, model, effort, sys, user string) (string, error) {
	out, _, err := claudeCodeRun(ctx, path, model, effort, sys, user, "")
	return out, err
}

// claudeCodeResult reads the JSON Claude Code prints (plain text from an older one).
func claudeCodeResult(out string) (string, *AIUsage, error) {
	var v struct {
		Type    string `json:"type"`
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
		Usage   struct {
			In     int `json:"input_tokens"`
			Out    int `json:"output_tokens"`
			CacheR int `json:"cache_read_input_tokens"`
			CacheW int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		Models map[string]struct {
			Out int `json:"outputTokens"`
		} `json:"modelUsage"`
	}
	t := strings.TrimSpace(out)
	if !strings.HasPrefix(t, "{") || json.Unmarshal([]byte(t), &v) != nil || v.Type == "" {
		return t, nil, nil
	}
	if v.IsError {
		return "", nil, errors.New("Claude Code: " + v.Result)
	}
	u := &AIUsage{In: v.Usage.In + v.Usage.CacheR + v.Usage.CacheW, Out: v.Usage.Out}
	best := -1
	for name, m := range v.Models { // the model that wrote the answer: the most output
		if m.Out > best || (m.Out == best && name < u.Model) {
			best, u.Model = m.Out, name
		}
	}
	return strings.TrimSpace(v.Result), u, nil
}

// claudeCodeRun asks once; picDir (may be "") holds pictures it may read.
func claudeCodeRun(ctx context.Context, path, model, effort, sys, user, picDir string) (string, *AIUsage, error) {
	bin, err := claudeCodePath(path)
	if err != nil {
		return "", nil, err
	}
	effort = validEffort(effort)
	run := func(withTools bool) (string, string, error) {
		c, cancel := context.WithTimeout(ctx, claudeCodeTimeout)
		defer cancel()
		cmd := exec.CommandContext(c, bin, claudeCodeArgs(model, effort, sys, withTools, picDir != "")...)
		cmd.Dir = os.TempDir() // not a project folder: nothing of the user's is in reach
		if picDir != "" {
			cmd.Dir = picDir
		}
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
	if err != nil && effort != "" && strings.Contains(stderr+out, "unknown option") && strings.Contains(stderr+out, "effort") { // older: no --effort
		effort = ""
		out, stderr, err = run(true)
	}
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
			return "", nil, errors.New("Claude Code is not signed in. " + claudeSignInHint(runtime.GOOS, bin, os.Getenv("PATH")))
		}
		if len(msg) > 400 {
			msg = msg[:400] + "…"
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", nil, errors.New("Claude Code: " + msg)
	}
	text, usage, err := claudeCodeResult(out)
	if err != nil {
		return "", nil, err
	}
	if text == "" {
		return "", nil, errors.New("Claude Code gave no answer")
	}
	return text, usage, nil
}

// claudeSignInCommand is what to type in a terminal to sign Claude Code in, on this computer's system: "claude" when
// the terminal finds it on its PATH, else its whole path (quoted when it has spaces; PowerShell runs a quoted path with &).
func claudeSignInCommand(goos, bin, path string) string {
	sep, list := ":", path
	if goos == "windows" {
		sep = ";"
	}
	dir := filepath.Dir(bin)
	for _, d := range strings.Split(list, sep) {
		if d != "" && filepath.Clean(d) == filepath.Clean(dir) {
			return "claude"
		}
	}
	if goos == "windows" {
		return "& '" + strings.ReplaceAll(bin, "'", "''") + "'"
	}
	if strings.ContainsAny(bin, " '\"$") {
		return "'" + strings.ReplaceAll(bin, "'", `'\''`) + "'"
	}
	return bin
}

// claudeSignInHint says where to type it, in this system's words.
func claudeSignInHint(goos, bin, path string) string {
	cmd := claudeSignInCommand(goos, bin, path)
	switch goos {
	case "darwin":
		return "Open Terminal and run: " + cmd + " - it asks you to sign in with your Claude account once."
	case "windows":
		return "Open PowerShell and run: " + cmd + " - it asks you to sign in with your Claude account once."
	default:
		return "Open a terminal and run: " + cmd + " - it asks you to sign in with your Claude account once."
	}
}

// ClaudeCodeSignIn is the command that signs Claude Code in on this computer (for the AI panel's Run it button).
func (a *App) ClaudeCodeSignIn() (string, error) {
	bin, err := claudeCodePath(a.cfg.Get().AI.BaseURL)
	if err != nil {
		return "", err
	}
	return claudeSignInCommand(runtime.GOOS, bin, os.Getenv("PATH")), nil
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

// ---- the models the installed Claude Code knows ----

var ccPrefix = []byte("claude-")
var ccModelRe = regexp.MustCompile("^[\"'`]claude-(fable|opus|sonnet|haiku)-(\\d+)(?:-(\\d+))?[\"'`]")

var ccModels struct {
	sync.Mutex
	key  string
	list []string
}

// claudeCodeModelIDs reads the model names built into the claude program
// (newest first per family, from version 4 on) - no question is sent, so it
// costs nothing. Kept until the program changes.
func claudeCodeModelIDs(bin string) []string {
	st, err := os.Stat(bin)
	if err != nil {
		return nil
	}
	key := fmt.Sprintf("%s|%d|%d", bin, st.Size(), st.ModTime().UnixNano())
	ccModels.Lock()
	defer ccModels.Unlock()
	if ccModels.key == key {
		return ccModels.list
	}
	f, err := os.Open(bin)
	if err != nil {
		return nil
	}
	defer f.Close()
	type ver struct {
		fam        string
		maj, minor int
	}
	seen := map[string]ver{}
	buf := make([]byte, 4<<20)
	var tail []byte
	for {
		n, err := f.Read(buf)
		if n > 0 {
			chunk := append(append([]byte{}, tail...), buf[:n]...)
			for i := 0; ; {
				k := bytes.Index(chunk[i:], ccPrefix)
				if k < 0 {
					break
				}
				at := i + k
				i = at + len(ccPrefix)
				if at == 0 {
					continue
				}
				m := ccModelRe.FindSubmatch(chunk[at-1 : min(len(chunk), at+40)])
				if m == nil {
					continue
				}
				maj, _ := strconv.Atoi(string(m[2]))
				minor := -1
				if len(m[3]) > 0 {
					minor, _ = strconv.Atoi(string(m[3]))
				}
				if maj < 4 || maj > 99 || minor > 99 {
					continue
				}
				id := "claude-" + string(m[1]) + "-" + string(m[2])
				if minor >= 0 {
					id += "-" + string(m[3])
				}
				seen[id] = ver{string(m[1]), maj, minor}
			}
			if len(chunk) > 64 {
				tail = append([]byte{}, chunk[len(chunk)-64:]...)
			}
		}
		if err != nil {
			break
		}
	}
	order := map[string]int{"fable": 0, "opus": 1, "sonnet": 2, "haiku": 3}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := seen[out[i]], seen[out[j]]
		if a.fam != b.fam {
			return order[a.fam] < order[b.fam]
		}
		if a.maj != b.maj {
			return a.maj > b.maj
		}
		return max(a.minor, 0) > max(b.minor, 0)
	})
	// "claude-opus-5" and "claude-opus-5-0" are the same model
	keep := out[:0]
	have := map[string]bool{}
	for _, id := range out {
		v := seen[id]
		k := fmt.Sprintf("%s-%d-%d", v.fam, v.maj, max(v.minor, 0))
		if !have[k] {
			have[k] = true
			keep = append(keep, id)
		}
	}
	ccModels.key, ccModels.list = key, keep
	return keep
}
