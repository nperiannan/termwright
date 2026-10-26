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

// DefaultPrompt matches the command prompt of most devices and shells.
const DefaultPrompt = `[>#$%\]] ?$`

var reMore = regexp.MustCompile(`(?i)(--+ ?more ?--+|<--- more --->|press any key to continue|\(q\)uit|^\s*more\s*$)\s*$`)

// Capture connects to s in the background (no tab is shown), logs in like a
// tab would (device type steps, logon actions, saved passwords), runs the
// commands one after another and returns their output without the command
// echo and the prompts. Nothing is asked: when the device needs a password
// or a host key decision that is not saved, it fails with ErrNeedsInput.
func (m *Manager) Capture(ctx context.Context, s session.Session, commands []string, prompt string) (string, error) {
	if prompt == "" {
		prompt = DefaultPrompt
	}
	re, err := regexp.Compile(`(?m)` + prompt)
	if err != nil {
		return "", fmt.Errorf("prompt pattern: %w", err)
	}
	s.LogOnConnect, s.AutoReconnect, s.Triggers = false, false, nil
	s.Cols, s.Rows = 400, 50
	ti := m.Create(s)
	id := ti.ID
	m.quiet.Store(id, true)
	defer func() {
		_ = m.Close(id)
		m.quiet.Delete(id)
	}()
	t, err := m.get(id)
	if err != nil {
		return "", err
	}
	var mu sync.Mutex
	var out strings.Builder
	var strip logging.Stripper
	last := time.Now()
	unsub := t.subscribe(func(p []byte) {
		mu.Lock()
		out.Write(strip.Strip(p))
		last = time.Now()
		mu.Unlock()
	})
	defer unsub()
	snapshot := func() (string, time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		return out.String(), time.Since(last)
	}
	if err := m.Start(id, 400, 50); err != nil {
		return "", err
	}
	// atPrompt: the output ends with the prompt and has been quiet for a moment
	atPrompt := func(from int, quiet time.Duration) (bool, string) {
		txt, idle := snapshot()
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
			_ = m.Write(id, []byte(" "))
			return false, ""
		}
		return re.MatchString(lastLine), lastLine
	}
	wait := func(from int, limit time.Duration, what string) (string, error) {
		deadline := time.Now().Add(limit)
		for {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			t.mu.Lock()
			state, logon := t.state, t.logon
			t.mu.Unlock()
			if state == StateError || state == StateDisconnected {
				msg := m.lastStatus(id)
				if msg == "" {
					msg = "the connection was closed"
				}
				return "", errors.New(msg)
			}
			if state == StateConnected && (logon == nil || logon.done()) {
				if ok, p := atPrompt(from, 900*time.Millisecond); ok {
					return p, nil
				}
			}
			if time.Now().After(deadline) {
				txt, _ := snapshot()
				lastLine := strings.TrimSpace(lastLineOf(txt))
				if lastLine == "" {
					return "", fmt.Errorf("no prompt after %s (nothing received)", what)
				}
				return "", fmt.Errorf("no prompt after %s (last line: %q) - check the device type's prompt pattern", what, clip(lastLine, 80))
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	promptLine, err := wait(0, 60*time.Second, "logging in")
	if err != nil {
		return "", err
	}
	var res strings.Builder
	for _, c := range commands {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		before, _ := snapshot()
		if err := m.Write(id, []byte(c+"\r")); err != nil {
			return "", err
		}
		if promptLine, err = wait(len(before), 3*time.Minute, `"`+c+`"`); err != nil {
			return "", err
		}
		txt, _ := snapshot()
		res.WriteString(cleanOutput(txt[len(before):], c, promptLine))
	}
	return res.String(), nil
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
