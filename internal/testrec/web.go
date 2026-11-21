package testrec

import (
	"context"
	"fmt"

	"github.com/termwright/termwright/internal/webrun"
)

// WebStep drives a browser with Playwright (webrun.Step: url, browser, show, insecure, actions).
type WebStep = webrun.Step

// WebAction is one thing to do on the page.
type WebAction = webrun.Action

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
func WebActionText(a WebAction) string { return webrun.ActionText(a) }
