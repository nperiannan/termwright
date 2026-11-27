package testrec

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
var WebActions = func() []string {
	var out []string
	for _, a := range webrun.Actions {
		out = append(out, a.Do)
	}
	return out
}()

func FillWeb(w WebStep, v map[string]string) WebStep {
	w.URL, w.Login, w.State = Fill(w.URL, v), Fill(w.Login, v), Fill(w.State, v)
	acts := make([]WebAction, len(w.Actions))
	for i, a := range w.Actions {
		acts[i] = WebAction{Do: a.Do, Target: Fill(a.Target, v), Value: Fill(a.Value, v), Secret: a.Secret}
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

// webLoginRoles makes each web step's Login session a role of the test, like a device: the step logs in
// with {{role}} and the test's Vars say which session that is by default. A testbed (or the run) can then
// put its own web app in that role. Logins that are already {{variables}} stay as they are.
func webLoginRoles(c *Case) {
	byPath := map[string]string{}
	for k, v := range c.Vars {
		if !strings.Contains(k, ".") && !strings.Contains(v, "{{") {
			if old, ok := byPath[v]; !ok || k < old {
				byPath[v] = k
			}
		}
	}
	var walk func([]Step)
	walk = func(l []Step) {
		for i := range l {
			if w := l[i].Web; w != nil {
				p := strings.TrimSpace(w.Login)
				if p != "" && !strings.Contains(p, "{{") {
					role, ok := byPath[p]
					if !ok {
						role = webRoleName(p, c.Vars)
						if c.Vars == nil {
							c.Vars = map[string]string{}
						}
						c.Vars[role], byPath[p] = p, role
					}
					ws := *w
					ws.Login = "{{" + role + "}}"
					l[i].Web = &ws
				}
			}
			if l[i].Loop != nil {
				lp := *l[i].Loop
				lp.Steps = append([]Step(nil), lp.Steps...)
				walk(lp.Steps)
				l[i].Loop = &lp
			}
		}
	}
	for _, l := range []*[]Step{&c.Setup, &c.Steps, &c.Cleanup} {
		*l = append([]Step(nil), (*l)...)
		walk(*l)
	}
}

// webRoleName: "Web Apps/ExtremeCloud IQ" -> "extremecloud_iq" (not one of the test's variables yet).
func webRoleName(path string, vars map[string]string) string {
	n := path[strings.LastIndex(path, "/")+1:]
	var b strings.Builder
	us := false
	for _, r := range strings.ToLower(n) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			us = false
		} else if !us && b.Len() > 0 {
			b.WriteByte('_')
			us = true
		}
	}
	base := strings.TrimRight(b.String(), "_")
	if base == "" || base[0] >= '0' && base[0] <= '9' {
		base = "app_" + base
	}
	name := base
	for i := 2; ; i++ {
		if _, used := vars[name]; !used {
			return name
		}
		name = base + "_" + strconv.Itoa(i)
	}
}

// literalWebLogins turns {{role}} logins back into their sessions (code that needs the session itself:
// the Playwright export).
func literalWebLogins(c Case) Case {
	var walk func([]Step) []Step
	walk = func(l []Step) []Step {
		out := append([]Step(nil), l...)
		for i := range out {
			if w := out[i].Web; w != nil {
				if r := strings.TrimSpace(w.Login); strings.HasPrefix(r, "{{") && strings.HasSuffix(r, "}}") {
					if v, ok := c.Vars[strings.TrimSpace(r[2:len(r)-2])]; ok && v != "" {
						ws := *w
						ws.Login = v
						out[i].Web = &ws
					}
				}
			}
			if out[i].Loop != nil {
				lp := *out[i].Loop
				lp.Steps = walk(lp.Steps)
				out[i].Loop = &lp
			}
		}
		return out
	}
	c.Setup, c.Steps, c.Cleanup = walk(c.Setup), walk(c.Steps), walk(c.Cleanup)
	return c
}
