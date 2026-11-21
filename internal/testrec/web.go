package testrec

import (
	"context"
	"fmt"
	"strings"
)

// WebStep drives a browser with Playwright: it opens URL, then does the actions in order. The step's
// output is the log of the actions and the page's text at the end, so expect checks can look at it.
type WebStep struct {
	// URL: the page to open first (may be empty when the first action is goto).
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// Browser: chromium (default), firefox or webkit.
	Browser string `yaml:"browser,omitempty" json:"browser,omitempty"`
	// Show: show the browser window (default: headless).
	Show bool `yaml:"show,omitempty" json:"show,omitempty"`
	// Insecure: accept self-signed certificates (device web pages).
	Insecure bool        `yaml:"insecure,omitempty" json:"insecure,omitempty"`
	Actions  []WebAction `yaml:"actions,omitempty" json:"actions,omitempty"`
}

// WebAction is one thing to do on the page.
type WebAction struct {
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

// WebDriver is a Driver that can drive a browser.
type WebDriver interface {
	Web(ctx context.Context, w WebStep) (string, error)
}

// WebActions lists the actions a web step can do.
var WebActions = []string{"goto", "click", "fill", "press", "select", "check", "uncheck", "hover", "expect", "expect_not", "wait", "screenshot"}

func fillWeb(w WebStep, v map[string]string) WebStep {
	w.URL = Fill(w.URL, v)
	acts := make([]WebAction, len(w.Actions))
	for i, a := range w.Actions {
		acts[i] = WebAction{Do: a.Do, Target: Fill(a.Target, v), Value: Fill(a.Value, v)}
	}
	w.Actions = acts
	return w
}

// WebText is a web step in a few words.
func WebText(w WebStep) string {
	s := "web"
	if w.URL != "" {
		s += " " + w.URL
	}
	if n := len(w.Actions); n > 0 {
		s += fmt.Sprintf(" (%d actions)", n)
	}
	return s
}

// WebActionText is an action in a few words, as the log shows it.
func WebActionText(a WebAction) string {
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
