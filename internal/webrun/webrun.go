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
	I       int    `json:"i"`
	OK      bool   `json:"ok"`
	Note    string `json:"note"`
	Error   string `json:"error"`
	Shot    string `json:"shot"`
	Trace   string `json:"trace"`
	Done    bool   `json:"done"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Text    string `json:"text"`
	Dialogs []string
	Reads   []struct{ Name, Value string }
	Rec     *Action `json:"rec"`
	Healed  string  `json:"healed"`
	Pic     string  `json:"pic"`
	Video   string  `json:"video"`
	Pick    *Pick   `json:"pick"`
	Ack     *int    `json:"ack"`
	Closed  bool    `json:"closed"`
}

func (r Runner) start(ctx context.Context, job map[string]any) (*exec.Cmd, *bytes.Buffer, error) {
	st := r.Status()
	if st.Node == "" || !st.Ready {
		if st.Node == "" {
			return nil, nil, errors.New("web steps need Node.js: install it from nodejs.org (or brew install node), then click Set Up Playwright")
		}
		return nil, nil, errors.New("Playwright is not set up on this computer yet: click Set Up Playwright (it installs Playwright " + PlaywrightVersion + " and its Chromium once, about 150 MB)")
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "runner.js"), runnerJS, 0o644); err != nil {
		return nil, nil, err
	}
	in, _ := json.Marshal(job)
	c := exec.CommandContext(ctx, st.Node, filepath.Join(r.Dir, "runner.js"))
	c.Dir, c.Env, c.Stdin = r.Dir, r.env(st.Node), bytes.NewReader(append(in, '\n'))
	var stderr bytes.Buffer
	c.Stderr = &stderr
	return c, &stderr, nil
}

// Run runs a web step; screenshots, downloads and traces go to shots. The output is the action log,
// the values read ("read name = value") and the page's text.
func (r Runner) Run(ctx context.Context, w Step, shots string, timeout int, x ...Extra) (string, error) {
	var ex Extra
	if len(x) > 0 {
		ex = x[0]
	}
	for _, a := range w.Actions {
		if strings.Contains(a.Value, "{{password}}") || strings.Contains(a.Value, "{{username}}") {
			return "", errors.New(ActionText(a) + ": {{password}} and {{username}} come from the step's Login session - choose one")
		}
	}
	c, stderr, err := r.start(ctx, ex.job(w, shots, timeout))
	if err != nil {
		return "", err
	}
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
		for _, d := range l.Dialogs {
			fmt.Fprintf(&log, "dialog: %s\n", d)
		}
		switch {
		case l.Done:
			for _, rd := range l.Reads {
				fmt.Fprintf(&log, "read %s = %s\n", rd.Name, rd.Value)
			}
			fmt.Fprintf(&log, "page: %s  %s\n--- page text ---\n%s\n", l.Title, l.URL, l.Text)
		case l.OK:
			fmt.Fprintf(&log, "✓ %s", what)
			if l.Note != "" && !(l.I >= 0 && l.I < len(w.Actions) && w.Actions[l.I].Secret) {
				fmt.Fprintf(&log, "  → %s", l.Note)
			}
			log.WriteByte('\n')
			if l.Healed != "" && l.I >= 0 && l.I < len(w.Actions) {
				fmt.Fprintf(&log, "healed %d: %s → %s\n", l.I+1, w.Actions[l.I].Target, l.Healed)
			}
			if l.Pic != "" {
				fmt.Fprintf(&log, "picture: %s\n", l.Pic)
			}
		default:
			fmt.Fprintf(&log, "✗ %s: %s\n", what, l.Error)
			if l.Shot != "" {
				fmt.Fprintf(&log, "screenshot: %s\n", l.Shot)
			}
			if l.Trace != "" {
				fmt.Fprintf(&log, "trace: %s  (npx playwright show-trace <file>)\n", l.Trace)
			}
			if l.Video != "" {
				fmt.Fprintf(&log, "video: %s\n", l.Video)
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

// Drive is what a recording test does in the page (click, fill, select, altclick).
type Drive struct {
	Do     string `json:"do"`
	Target string `json:"target"`
	Value  string `json:"value,omitempty"`
}

// Record opens the step's page in a visible browser and returns the actions the person does there, as
// they happen (each also goes to onAction), until the window is closed. drive is for tests.
func (r Runner) Record(ctx context.Context, w Step, onAction func(Action), x Extra, drive ...Drive) ([]Action, error) {
	job := x.job(w, "", 30)
	job["record"] = true
	if len(drive) > 0 {
		job["drive"] = drive
	}
	c, stderr, err := r.start(ctx, job)
	if err != nil {
		return nil, err
	}
	pipe, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	var acts []Action
	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	var failed string
	for sc.Scan() {
		var l line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if l.Rec != nil {
			acts = append(acts, *l.Rec)
			if onAction != nil {
				onAction(*l.Rec)
			}
		}
		if l.Error != "" {
			failed = l.Error
		}
	}
	werr := c.Wait()
	if failed != "" {
		return acts, errors.New(failed)
	}
	if werr != nil && len(acts) == 0 && ctx.Err() == nil {
		return acts, fmt.Errorf("the browser stopped: %v %s", werr, strings.TrimSpace(stderr.String()))
	}
	return acts, nil
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
	Insecure bool `yaml:"insecure,omitempty" json:"insecure,omitempty"`
	// State: keep the login (cookies and storage) under this name - the next web steps with the same
	// state start logged in.
	State string `yaml:"state,omitempty" json:"state,omitempty"`
	// Login: a saved session whose user name and password answer the site's HTTP login (basic auth).
	Login string `yaml:"login,omitempty" json:"login,omitempty"`
	// Viewport: the page size, e.g. 1280x800.
	Viewport string `yaml:"viewport,omitempty" json:"viewport,omitempty"`
	// Trace: record a Playwright trace (reports/web-shots/trace-….zip: npx playwright show-trace).
	Trace bool `yaml:"trace,omitempty" json:"trace,omitempty"`
	// Pictures: "" - a picture after every action (in the report); "off" - only when an action fails.
	Pictures string   `yaml:"pictures,omitempty" json:"pictures,omitempty"`
	Actions  []Action `yaml:"actions,omitempty" json:"actions,omitempty"`
}

// Extra is what the runner gets besides the step: where files and states are, the HTTP login, the web
// app's recorded login (done when the app asks to log in), and the name the pictures start with.
type Extra struct {
	Root, StateDir         string
	HTTPUser, HTTPPassword string
	Login                  []Action
	Prefix                 string
	NoVideo                bool
}

func (x Extra) job(w Step, shots string, timeout int) map[string]any {
	pre := x.Prefix
	if pre == "" {
		pre = "web-" + time.Now().Format("150405.000")
	}
	return map[string]any{"step": w, "shots": shots, "timeout": timeout, "root": x.Root, "stateDir": x.StateDir,
		"httpUser": x.HTTPUser, "httpPassword": x.HTTPPassword, "login": x.Login, "prefix": pre,
		"pictures": w.Pictures != "off" && shots != "", "video": !x.NoVideo && !w.Show && shots != ""}
}

// Pick is the element a person clicked to choose a target: the target and its fallbacks.
type Pick struct {
	Target string   `json:"target"`
	Alt    []string `json:"alt,omitempty"`
	Text   string   `json:"text,omitempty"`
	Tag    string   `json:"tag,omitempty"`
}

// Action is one thing to do on the page.
type Action struct {
	// Do: what to do - see Actions.
	Do string `yaml:"do" json:"do"`
	// Target: what to act on - a CSS selector, text=Sign in, role=button[name="Save"], label=User name,
	// placeholder=Search, testid=save, title=…, alt=…, an XPath (//...); frame=#main >> … looks in a frame.
	// expect / expect_not without a target: the text anywhere on the page.
	Target string `yaml:"target,omitempty" json:"target,omitempty"`
	// Value: fill / type: the text; press: the key (Enter, Tab, Control+A); select: the option; goto: the
	// address; wait: seconds or a part of the address; expect with a target: the text it must contain;
	// expect_value / expect_count / expect_url / expect_title: what it must be; read: the variable name;
	// upload: files of the tests folder; tab: 1, 2… or last or a part of the address; screenshot: a name.
	Value string `yaml:"value,omitempty" json:"value,omitempty"`
	// Secret: the value is a password (from {{secret:Session}}): it is never logged.
	Secret bool `yaml:"-" json:"secret,omitempty"`
	// Alt: other ways to find the target, tried when it is not there (the GUI changed): the run says
	// "healed" and which one matched.
	Alt []string `yaml:"alt,omitempty" json:"alt,omitempty"`
	// With: a block's values (do: block, target: the block's name).
	With map[string]string `yaml:"with,omitempty" json:"with,omitempty"`
}

// Actions are what a web step can do, with what each needs.
var Actions = []struct{ Do, Needs, Says string }{
	{"goto", "value", "open an address"}, {"click", "target", "click"}, {"dblclick", "target", "double-click"}, {"rightclick", "target", "right-click"},
	{"fill", "target value", "type into a field (replaces its text)"}, {"type", "target value", "type key by key"}, {"clear", "target", "empty a field"},
	{"press", "value", "press a key, on a target or the page"}, {"select", "target value", "choose an option"},
	{"check", "target", "tick"}, {"uncheck", "target", "untick"}, {"hover", "target", "move the mouse over"}, {"focus", "target", "focus"},
	{"scroll", "", "scroll to a target, or down"}, {"upload", "target value", "choose files to upload"}, {"download", "target", "click and save the download"},
	{"dialog", "value", "accept, dismiss or answer alerts and confirms"}, {"tab", "value", "switch to another tab or popup"}, {"close_tab", "", "close the tab"},
	{"reload", "", "reload"}, {"back", "", "go back"}, {"wait", "", "wait seconds, for a target, or for an address"}, {"wait_gone", "target", "wait until it is gone"},
	{"expect", "", "check it shows"}, {"expect_not", "", "check it is gone"}, {"expect_value", "target value", "check a field's value"},
	{"expect_count", "target value", "check how many there are"}, {"expect_checked", "target", "check it is ticked"}, {"expect_enabled", "target", "check it is enabled"},
	{"expect_url", "value", "check the address"}, {"expect_title", "value", "check the title"}, {"read", "target value", "read a text into a variable"},
	{"screenshot", "", "save a screenshot"}, {"block", "target", "do the actions of a saved block"},
}

// ActionText is an action in a few words, as the log shows it.
func ActionText(a Action) string {
	parts := []string{strings.ToLower(a.Do)}
	if a.Target != "" {
		parts = append(parts, a.Target)
	}
	if a.Value != "" {
		if a.Secret || (a.Do == "fill" || a.Do == "type") && looksSecret(a.Target) {
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

// WithLogin fills {{username}} and {{password}} in the actions' values with a login (the password marked
// secret, so it is never logged) - the step's Login session gives them.
func WithLogin(w Step, user, password string) Step {
	acts := make([]Action, len(w.Actions))
	for i, a := range w.Actions {
		if strings.Contains(a.Value, "{{password}}") {
			a.Value, a.Secret = strings.ReplaceAll(a.Value, "{{password}}", password), true
		}
		a.Value = strings.ReplaceAll(a.Value, "{{username}}", user)
		acts[i] = a
	}
	w.Actions = acts
	return w
}

// RunnerJS is the Node.js script that runs web steps (exported suites carry it).
func RunnerJS() []byte { return runnerJS }
