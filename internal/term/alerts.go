package term

import (
	"regexp"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/logging"
)

// AlertRule raises an "alert" event when a line of output in any tab matches.
type AlertRule struct {
	Pattern       string
	Regex         bool
	CaseSensitive bool
	Sound         bool
	Desktop       bool
}

type alertRule struct {
	AlertRule
	re *regexp.Regexp
}

// Alert is sent to the UI as the "alert" event.
type Alert struct {
	TabID   string `json:"tabId"`
	Title   string `json:"title"`
	Line    string `json:"line"`
	Pattern string `json:"pattern"`
	Sound   bool   `json:"sound"`
	Desktop bool   `json:"desktop"`
}

// SetAlerts replaces the alert rules; invalid patterns are returned as errors.
func (m *Manager) SetAlerts(rules []AlertRule) []string {
	var list []alertRule
	var bad []string
	for _, r := range rules {
		if strings.TrimSpace(r.Pattern) == "" {
			continue
		}
		src := r.Pattern
		if !r.Regex {
			src = regexp.QuoteMeta(src)
		}
		if !r.CaseSensitive {
			src = "(?i)" + src
		}
		re, err := regexp.Compile(src)
		if err != nil {
			bad = append(bad, r.Pattern+": "+err.Error())
			continue
		}
		list = append(list, alertRule{r, re})
	}
	m.alertMu.Lock()
	m.alerts = list
	m.alertMu.Unlock()
	return bad
}

// checkAlerts matches complete output lines against the alert rules. Each
// rule fires at most once every 5 seconds per tab.
func (m *Manager) checkAlerts(t *Tab, p []byte) {
	m.alertMu.RLock()
	rules := m.alerts
	m.alertMu.RUnlock()
	if len(rules) == 0 {
		return
	}
	var strip logging.Stripper
	t.mu.Lock()
	text := t.alertBuf + string(strip.Strip(p))
	i := strings.LastIndexAny(text, "\r\n")
	if i < 0 {
		if len(text) > 4096 {
			text = text[len(text)-4096:]
		}
		t.alertBuf = text
		t.mu.Unlock()
		return
	}
	lines, rest := text[:i], text[i+1:]
	if len(rest) > 4096 {
		rest = rest[len(rest)-4096:]
	}
	t.alertBuf = rest
	if t.alertAt == nil {
		t.alertAt = map[int]time.Time{}
	}
	name := title(t.sess)
	var fire []Alert
	for _, line := range strings.FieldsFunc(lines, func(r rune) bool { return r == '\r' || r == '\n' }) {
		for k, r := range rules {
			if !r.re.MatchString(line) || time.Since(t.alertAt[k]) < 5*time.Second {
				continue
			}
			t.alertAt[k] = time.Now()
			if len(line) > 300 {
				line = line[:300]
			}
			fire = append(fire, Alert{TabID: t.id, Title: name, Line: strings.TrimSpace(line), Pattern: r.Pattern, Sound: r.Sound, Desktop: r.Desktop})
		}
	}
	t.mu.Unlock()
	for _, a := range fire {
		m.emit.Emit("alert", a)
	}
}
