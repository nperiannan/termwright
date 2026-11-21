package term

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/logging"
	"github.com/termwright/termwright/internal/session"
)

// ErrNeedsInput is returned to a background connection (configuration
// backups) when the device asks for something only a person can answer.
var ErrNeedsInput = errors.New("needs an answer")

// CaptureError is a failed capture with what the device sent until then.
type CaptureError struct {
	Err        error
	Transcript string
}

func (e *CaptureError) Error() string { return e.Err.Error() }
func (e *CaptureError) Unwrap() error { return e.Err }

// DefaultPrompt matches the command prompt of most devices and shells.
const DefaultPrompt = `[>#$%\]] ?$`

var reMore = regexp.MustCompile(`(?i)(--+ ?more ?--+|<--- more --->|press any key to continue|\(q\)uit|^\s*more\s*$)\s*$`)

type progressKey struct{}

// WithProgress returns a context for Capture/CaptureEach that reports how far a background capture is:
// fn(0, n) once the login (and the device type's after-login steps) is done, then fn(k, n) after the k-th of
// the n commands has finished. It is called from the capturing goroutine.
func WithProgress(ctx context.Context, fn func(done, total int)) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

// Capture connects to s in the background (no tab is shown), logs in like a
// tab would (device type steps, logon actions, saved passwords), runs the
// commands one after another and returns their output without the command
// echo and the prompts. Nothing is asked: when the device needs a password
// or a host key decision that is not saved, it fails with ErrNeedsInput.
func (m *Manager) Capture(ctx context.Context, s session.Session, commands []string, prompt string) (string, error) {
	outs, err := m.captureEach(ctx, s, commands, prompt, false)
	return strings.Join(outs, ""), err
}

// CaptureEach is Capture with the output of each command separately (empty
// commands are skipped and have no entry).
func (m *Manager) CaptureEach(ctx context.Context, s session.Session, commands []string, prompt string) ([]string, error) {
	return m.captureEach(ctx, s, commands, prompt, false)
}

// CaptureYes is CaptureEach that answers yes to a device's yes/no question ("(y/N)") - for commands that change
// the device and ask first, like saving its configuration. Only for commands the user approved.
func (m *Manager) CaptureYes(ctx context.Context, s session.Session, commands []string, prompt string) ([]string, error) {
	return m.captureEach(ctx, s, commands, prompt, true)
}

func (m *Manager) captureEach(ctx context.Context, s session.Session, commands []string, prompt string, yes bool) ([]string, error) {
	var transcript func() string
	outs, err := m.capture(ctx, s, commands, prompt, &transcript, yes)
	if err != nil && transcript != nil {
		return nil, &CaptureError{Err: err, Transcript: transcript()}
	}
	return outs, err
}

// reYesNo is a yes/no question at the end of a line: "(y/N)", "[y/n]", "(yes/no)".
var reYesNo = regexp.MustCompile(`(?i)[(\[]\s*(y/n|yes/no)\s*[)\]]\s*[:?]?\s*$`)

// reConfirm: "[confirm]" or a question with its default in brackets ("Destination filename [x.cfg]?"),
// answered with Enter; reSaveFirst: "Save? [yes/no]" before a reload, answered no (the reload is meant
// to drop the changes).
var (
	reConfirm   = regexp.MustCompile(`(?i)(\[confirm\]|\[[^\]\n]*\]\s*\?)\s*$`)
	reSaveFirst = regexp.MustCompile(`(?i)(modified|changed).*save\??\s*[(\[]\s*yes/no\s*[)\]]\s*:?\s*$`)
)

// CaptureConn is a background login kept open so several commands run on it one after another
// (the tests reuse one per session instead of logging in for every step). Not safe for parallel use.
type CaptureConn struct {
	m          *Manager
	id         string
	t          *Tab
	mu         sync.Mutex
	out        strings.Builder
	tap        func(string) // sees the device's output as it arrives (a test run's live log)
	strip      logging.Stripper
	last       time.Time
	unsub      func()
	re, defRe  *regexp.Regexp
	prompt     string
	loose      bool
	exact      string
	yes        bool
	answered   int
	promptLine string
	closed     bool
}

// OpenCapture logs in to s in the background like Capture (device type steps, logon actions,
// saved passwords; nothing is asked) and keeps the connection for Run until Close.
func (m *Manager) OpenCapture(ctx context.Context, s session.Session, prompt string) (*CaptureConn, error) {
	var tr func() string
	c, err := m.openCapture(ctx, s, prompt, false, &tr)
	if err != nil && tr != nil {
		return nil, &CaptureError{Err: err, Transcript: tr()}
	}
	return c, err
}

func (m *Manager) openCapture(ctx context.Context, s session.Session, prompt string, yes bool, transcript *func() string) (*CaptureConn, error) {
	if prompt == "" {
		prompt = DefaultPrompt
	}
	re, err := regexp.Compile(`(?m)` + prompt)
	if err != nil {
		return nil, fmt.Errorf("prompt pattern: %w", err)
	}
	s.LogOnConnect, s.AutoReconnect, s.Triggers = false, false, nil
	s.Cols, s.Rows = 400, 50
	ti := m.Create(s)
	c := &CaptureConn{m: m, id: ti.ID, re: re, defRe: regexp.MustCompile(DefaultPrompt), prompt: prompt, yes: yes, last: time.Now()}
	m.quiet.Store(c.id, true)
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	if c.t, err = m.get(c.id); err != nil {
		return nil, err
	}
	c.unsub = c.t.subscribe(func(p []byte) {
		c.mu.Lock()
		txt := c.strip.Strip(p)
		c.out.Write(txt)
		c.last = time.Now()
		tap := c.tap
		c.mu.Unlock()
		if tap != nil && len(txt) > 0 {
			tap(string(txt))
		}
	})
	*transcript = func() string { s, _ := c.snapshot(); return s }
	if err := m.Start(c.id, 400, 50); err != nil {
		return nil, err
	}
	if c.promptLine, err = c.wait(ctx, 0, 60*time.Second, "logging in"); err != nil {
		return nil, err
	}
	// After-login steps one at a time, each only when its expected text is in
	// the prompt: "enable" is skipped when the account starts at "#", and
	// nothing is typed before the device is ready for it. Meanwhile any common
	// prompt is accepted ("config terminal" changes it to "sw:1(config)#").
	c.loose = true
	if dt, found := m.deviceOf(c.t.sess); found {
		for _, a := range dt.After() {
			if exp := Unescape(a.Expect); exp != "" && !strings.Contains(c.promptLine, exp) {
				continue
			}
			before, _ := c.snapshot()
			c.t.mu.Lock()
			conn := c.t.conn
			c.t.mu.Unlock()
			if conn == nil {
				return nil, errors.New("the connection was closed")
			}
			m.runLogon(c.t, conn, []session.LogonAction{a})
			if c.promptLine, err = c.wait(ctx, len(before), 45*time.Second, `after-login step "`+a.Send+`"`); err != nil {
				return nil, err
			}
		}
	}
	// from now on: the prompt the device showed after the after-login steps
	c.loose, c.exact = false, strings.TrimSpace(c.promptLine)
	ok = true
	return c, nil
}

func (c *CaptureConn) snapshot() (string, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.out.String(), time.Since(c.last)
}

// atPrompt: the output ends with the prompt and has been quiet for a moment.
func (c *CaptureConn) atPrompt(from int, quiet time.Duration) (bool, string) {
	txt, idle := c.snapshot()
	if from > len(txt) {
		from = len(txt)
	}
	tail := strings.TrimRight(strings.ReplaceAll(txt[from:], "\r", ""), "\n")
	if idle < quiet {
		return false, ""
	}
	lines := strings.Split(tail, "\n")
	lastLine := lines[len(lines)-1]
	if reMore.MatchString(lastLine) {
		_ = c.m.Write(c.id, []byte(" "))
		return false, ""
	}
	if c.yes && len(txt) > c.answered { // once per question
		switch {
		case reSaveFirst.MatchString(lastLine):
			c.answered = len(txt)
			_ = c.m.Write(c.id, []byte("no\r"))
			return false, ""
		case reYesNo.MatchString(lastLine):
			c.answered = len(txt)
			_ = c.m.Write(c.id, []byte("y\r"))
			return false, ""
		case reConfirm.MatchString(lastLine):
			c.answered = len(txt)
			_ = c.m.Write(c.id, []byte("\r"))
			return false, ""
		}
	}
	ok := c.re.MatchString(lastLine) || (c.loose && c.defRe.MatchString(lastLine)) || (c.exact != "" && strings.TrimSpace(lastLine) == c.exact)
	return ok, lastLine
}

func (c *CaptureConn) wait(ctx context.Context, from int, limit time.Duration, what string) (string, error) {
	deadline := time.Now().Add(limit)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		c.t.mu.Lock()
		state, logon := c.t.state, c.t.logon
		c.t.mu.Unlock()
		if state == StateError || state == StateDisconnected {
			msg := c.m.lastStatus(c.id)
			if msg == "" {
				msg = "the connection was closed"
			}
			return "", errors.New(msg)
		}
		if state == StateConnected && (logon == nil || logon.done()) {
			// a prompt shaped like the last one ("Stack.5 #" after "Stack.4 #") needs only a short quiet
			// moment; an unknown one waits longer so output that pauses is not cut
			if c.promptLine != "" {
				if ok, p := c.atPrompt(from, 200*time.Millisecond); ok && sameShape(p, c.promptLine) {
					return p, nil
				}
			}
			if ok, p := c.atPrompt(from, 900*time.Millisecond); ok {
				return p, nil
			}
		}
		if time.Now().After(deadline) {
			txt, _ := c.snapshot()
			lastLine := strings.TrimSpace(lastLineOf(txt))
			if lastLine == "" {
				return "", fmt.Errorf("no prompt after %s (nothing received)", what)
			}
			return "", fmt.Errorf("no prompt after %s (last line: %q does not match the prompt pattern %s)", what, clip(lastLine, 120), c.prompt)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// Run sends one command and returns its output without the echo and the prompt.
func (c *CaptureConn) Run(ctx context.Context, cmd string) (string, error) {
	cmd = strings.TrimSpace(cmd)
	if c.closed {
		return "", errors.New("the connection was closed")
	}
	if cmd == "" {
		return "", nil
	}
	before, _ := c.snapshot()
	if err := c.m.Write(c.id, []byte(cmd+"\r")); err != nil {
		return "", err
	}
	p, err := c.wait(ctx, len(before), 3*time.Minute, `"`+cmd+`"`)
	if err != nil {
		return "", err
	}
	c.promptLine = p
	txt, _ := c.snapshot()
	return cleanOutput(txt[len(before):], cmd, p), nil
}

// Alive: the connection is still up.
func (c *CaptureConn) Alive() bool {
	if c.closed || c.t == nil {
		return false
	}
	c.t.mu.Lock()
	defer c.t.mu.Unlock()
	return c.t.state == StateConnected
}

// Close logs out (closes the background connection).
func (c *CaptureConn) Close() {
	if c.closed {
		return
	}
	c.closed = true
	if c.unsub != nil {
		c.unsub()
	}
	_ = c.m.Close(c.id)
	c.m.quiet.Delete(c.id)
}

func (m *Manager) capture(ctx context.Context, s session.Session, commands []string, prompt string, transcript *func() string, yes bool) ([]string, error) {
	c, err := m.openCapture(ctx, s, prompt, yes, transcript)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var res []string
	total := 0
	for _, cmd := range commands {
		if strings.TrimSpace(cmd) != "" {
			total++
		}
	}
	prog, _ := ctx.Value(progressKey{}).(func(done, total int))
	if prog != nil {
		prog(0, total)
	}
	for _, cmd := range commands {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		out, err := c.Run(ctx, cmd)
		if err != nil {
			return nil, err
		}
		res = append(res, out)
		if prog != nil {
			prog(len(res), total)
		}
	}
	return res, nil
}

// cleanOutput removes the echoed command (first line) and the prompt (last
// line) from a command's output and normalises line ends.
func cleanOutput(s, cmd, prompt string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && strings.Contains(lines[0], cmd) {
		lines = lines[1:]
	}
	for len(lines) > 0 && (strings.TrimSpace(lines[len(lines)-1]) == "" || strings.TrimSpace(lines[len(lines)-1]) == strings.TrimSpace(prompt)) {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func lastLineOf(s string) string {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r", ""), "\n")
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// SetTap gives the device's output, as it arrives, to f (nil stops it).
func (c *CaptureConn) SetTap(f func(string)) {
	c.mu.Lock()
	c.tap = f
	c.mu.Unlock()
}

// sameShape: two prompt lines that differ only in their numbers ("* Slot-1 Stack.5 #", "* Slot-1 Stack.6 #").
func sameShape(a, b string) bool {
	norm := func(s string) string {
		var out []rune
		dig := false
		for _, r := range strings.TrimSpace(s) {
			if r >= '0' && r <= '9' {
				if !dig {
					out = append(out, '9')
				}
				dig = true
				continue
			}
			dig = false
			out = append(out, r)
		}
		return string(out)
	}
	return a != "" && norm(a) == norm(b)
}
