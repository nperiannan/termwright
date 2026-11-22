// Package webrun runs web test steps with Playwright: a small Node.js runner in Termwright's data folder
// (set up once with npm), driven by JSON on stdin.
package webrun

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed runner.js
var runnerJS []byte

// PlaywrightVersion is the version installed by Setup.
const PlaywrightVersion = "1.48.2"

// Status says whether web steps can run.
type Status struct {
	Node    string `json:"node,omitempty"`    // node's path ("" = not found)
	Version string `json:"version,omitempty"` // node --version
	Ready   bool   `json:"ready"`             // playwright is installed
	Dir     string `json:"dir"`
	Hint    string `json:"hint,omitempty"`
}

// Runner runs web steps; Dir is where playwright is installed.
type Runner struct{ Dir string }

func searchDirs() []string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return []string{`C:\Program Files\nodejs`, filepath.Join(os.Getenv("APPDATA"), "npm")}
	}
	d := []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", filepath.Join(home, ".volta", "bin")}
	if m, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin")); len(m) > 0 {
		d = append(d, m[len(m)-1])
	}
	return d
}

// find looks for a program on PATH and where installers put it (apps started from the Dock have a short PATH).
func find(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	exe := name
	if runtime.GOOS == "windows" {
		exe += map[string]string{"node": ".exe", "npm": ".cmd", "npx": ".cmd"}[name]
	}
	for _, d := range searchDirs() {
		if p := filepath.Join(d, exe); fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func (r Runner) env(node string) []string {
	return append(os.Environ(), "PATH="+filepath.Dir(node)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// Status reports node and playwright.
func (r Runner) Status() Status {
	st := Status{Dir: r.Dir, Node: find("node")}
	if st.Node == "" {
		st.Hint = "Install Node.js (nodejs.org, or brew install node), then Set Up."
		return st
	}
	if b, err := exec.Command(st.Node, "--version").Output(); err == nil {
		st.Version = strings.TrimSpace(string(b))
	}
	st.Ready = fileExists(filepath.Join(r.Dir, "node_modules", "playwright", "package.json"))
	if !st.Ready {
		st.Hint = "Set Up installs Playwright " + PlaywrightVersion + " and its Chromium in " + r.Dir + " (about 150 MB)."
	}
	return st
}

// Setup installs playwright and the Chromium browser in Dir; log gets the installer's lines.
func (r Runner) Setup(ctx context.Context, log func(string)) error {
	node, npm := find("node"), find("npm")
	if node == "" || npm == "" {
		return errors.New("Node.js is not installed: install it from nodejs.org (or brew install node), then try again")
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return err
	}
	if !fileExists(filepath.Join(r.Dir, "package.json")) {
		_ = os.WriteFile(filepath.Join(r.Dir, "package.json"), []byte(`{"name":"termwright-web","private":true}`+"\n"), 0o644)
	}
	run := func(name string, args ...string) error {
		c := exec.CommandContext(ctx, name, args...)
		c.Dir, c.Env = r.Dir, r.env(node)
		p, _ := c.StdoutPipe()
		c.Stderr = c.Stdout
		if err := c.Start(); err != nil {
			return err
		}
		sc := bufio.NewScanner(p)
		for sc.Scan() {
			if log != nil {
				log(sc.Text())
			}
		}
		return c.Wait()
	}
	if log != nil {
		log("npm install playwright@" + PlaywrightVersion)
	}
	if err := run(npm, "install", "--no-fund", "--no-audit", "playwright@"+PlaywrightVersion); err != nil {
		return fmt.Errorf("npm install playwright: %v", err)
	}
	if log != nil {
		log("playwright install chromium")
	}
	if err := run(node, filepath.Join(r.Dir, "node_modules", "playwright", "cli.js"), "install", "chromium"); err != nil {
		return fmt.Errorf("playwright install chromium: %v", err)
	}
	return nil
}

type line struct {
	I     int    `json:"i"`
	OK    bool   `json:"ok"`
	Note  string `json:"note"`
	Error string `json:"error"`
	Shot  string `json:"shot"`
	Done  bool   `json:"done"`
	URL   string `json:"url"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Run runs a web step; screenshots go to shots. The output is the action log and the page's text.
func (r Runner) Run(ctx context.Context, w Step, shots string, timeout int) (string, error) {
	st := r.Status()
	if st.Node == "" || !st.Ready {
		return "", errors.New("web steps need Playwright: open a web step in the editor and click Set Up (" + st.Hint + ")")
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "runner.js"), runnerJS, 0o644); err != nil {
		return "", err
	}
	in, _ := json.Marshal(map[string]any{"step": w, "shots": shots, "timeout": timeout})
	c := exec.CommandContext(ctx, st.Node, filepath.Join(r.Dir, "runner.js"))
	c.Dir, c.Env, c.Stdin = r.Dir, r.env(st.Node), bytes.NewReader(in)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	start := time.Now()
	b, err := c.Output()
	var log strings.Builder
	var failed error
	for _, ln := range strings.Split(string(b), "\n") {
		var l line
		if json.Unmarshal([]byte(ln), &l) != nil {
			continue
		}
		what := "open " + w.URL
		if l.I >= 0 && l.I < len(w.Actions) {
			what = ActionText(w.Actions[l.I])
		}
		switch {
		case l.Done:
			fmt.Fprintf(&log, "page: %s  %s\n--- page text ---\n%s\n", l.Title, l.URL, l.Text)
		case l.OK:
			fmt.Fprintf(&log, "✓ %s", what)
			if l.Note != "" {
				fmt.Fprintf(&log, "  → %s", l.Note)
			}
			log.WriteByte('\n')
		default:
			fmt.Fprintf(&log, "✗ %s: %s\n", what, l.Error)
			if l.Shot != "" {
				fmt.Fprintf(&log, "screenshot: %s\n", l.Shot)
			}
			if l.URL != "" {
				fmt.Fprintf(&log, "page: %s\n", l.URL)
			}
			failed = fmt.Errorf("%s: %s", what, l.Error)
		}
	}
	if failed != nil {
		return log.String(), failed
	}
	if err != nil && log.Len() == 0 {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		return "", fmt.Errorf("the browser runner stopped after %s: %v %s", time.Since(start).Round(time.Second), err, msg)
	}
	return log.String(), nil
}

// Step drives a browser with Playwright: it opens URL, then does the actions in order. The step's
// output is the log of the actions and the page's text at the end, so expect checks can look at it.
type Step struct {
	// URL: the page to open first (may be empty when the first action is goto).
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// Browser: chromium (default), firefox or webkit.
	Browser string `yaml:"browser,omitempty" json:"browser,omitempty"`
	// Show: show the browser window (default: headless).
	Show bool `yaml:"show,omitempty" json:"show,omitempty"`
	// Insecure: accept self-signed certificates (device web pages).
	Insecure bool     `yaml:"insecure,omitempty" json:"insecure,omitempty"`
	Actions  []Action `yaml:"actions,omitempty" json:"actions,omitempty"`
}

// Action is one thing to do on the page.
type Action struct {
	// Do: goto, click, fill, press, select, check, uncheck, hover, expect (the text or element is
	// there), expect_not, wait (seconds, or until the target shows), screenshot.
	Do string `yaml:"do" json:"do"`
	// Target: what to act on - a CSS selector, text=Sign in, role=button[name="Save"], label=User name,
	// placeholder=Search, or an XPath (//...). expect: the text to find when there is no target.
	Target string `yaml:"target,omitempty" json:"target,omitempty"`
	// Value: fill: the text; press: the key (Enter); select: the option; goto: the URL; wait: seconds;
	// expect with a target: the text the element must contain; screenshot: the file name.
	Value string `yaml:"value,omitempty" json:"value,omitempty"`
}

// ActionText is an action in a few words, as the log shows it.
func ActionText(a Action) string {
	parts := []string{strings.ToLower(a.Do)}
	if a.Target != "" {
		parts = append(parts, a.Target)
	}
	if a.Value != "" {
		if a.Do == "fill" && looksSecret(a.Target) {
			parts = append(parts, `"••••"`)
		} else {
			parts = append(parts, fmt.Sprintf("%q", a.Value))
		}
	}
	return strings.Join(parts, " ")
}

func looksSecret(target string) bool {
	t := strings.ToLower(target)
	return strings.Contains(t, "password") || strings.Contains(t, "passwd") || strings.Contains(t, "secret") || strings.Contains(t, "token")
}
