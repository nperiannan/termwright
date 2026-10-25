package term

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/logging"
)

// recorder turns typed commands into a replayable script: before each command
// it waits for the prompt that was on screen when the user pressed Enter.
type recorder struct {
	mu      sync.Mutex
	strip   logging.Stripper
	partial string // last (unterminated) output line, i.e. the prompt
	line    []rune
	steps   []string
	started time.Time
}

func (r *recorder) output(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	txt := string(r.strip.Strip(p))
	if i := strings.LastIndexAny(txt, "\r\n"); i >= 0 {
		r.partial = txt[i+1:]
	} else {
		r.partial += txt
	}
	if len(r.partial) > 200 {
		r.partial = r.partial[len(r.partial)-200:]
	}
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (r *recorder) input(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range string(p) {
		switch c {
		case '\r', '\n':
			prompt := strings.TrimSpace(r.partial)
			// the prompt includes what the shell echoed back; drop the typed text
			if typed := strings.TrimSpace(string(r.line)); typed != "" {
				prompt = strings.TrimSpace(strings.TrimSuffix(prompt, typed))
			}
			if prompt != "" {
				r.steps = append(r.steps, fmt.Sprintf("term.waitFor(%s, 30000);", jsString(prompt)))
			}
			r.steps = append(r.steps, fmt.Sprintf("term.sendLine(%s);", jsString(string(r.line))))
			r.line = r.line[:0]
		case 0x7f, '\b':
			if n := len(r.line); n > 0 {
				r.line = r.line[:n-1]
			}
		case 0x03:
			r.steps = append(r.steps, `term.send("\u0003");`)
			r.line = r.line[:0]
		default:
			if c >= 0x20 {
				r.line = append(r.line, c)
			}
		}
	}
}

// StartRecording begins recording typed commands on a tab.
func (m *Manager) StartRecording(tabID string) error {
	t, err := m.get(tabID)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.recorder = &recorder{started: time.Now()}
	t.mu.Unlock()
	return nil
}

// StopRecording ends recording and returns the generated JavaScript.
func (m *Manager) StopRecording(tabID string) (string, error) {
	t, err := m.get(tabID)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	r := t.recorder
	t.recorder = nil
	name := title(t.sess)
	t.mu.Unlock()
	if r == nil {
		return "", errors.New("not recording")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "// Recorded by Termwright from %q on %s\n", name, r.started.Format("2006-01-02 15:04"))
	b.WriteString("// API: term.send / sendLine / waitFor / waitForAny / readUntil, sleep, log, prompt (see docs/SCRIPTING.md)\n\n")
	for _, s := range r.steps {
		b.WriteString(s + "\n")
	}
	return b.String(), nil
}
