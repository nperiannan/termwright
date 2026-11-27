package app

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/termwright/termwright/internal/testrec"
	"github.com/termwright/termwright/internal/webrun"
)

// The "web" tool: an AI (Claude through MCP, the in-app assistant, a script through the local API) drives a
// real browser with Playwright - open a web app or a page, read what is on it (a snapshot of its buttons,
// links and fields, each with a target), act on it - and every action that worked is kept, so the session
// becomes a web test (or a web step added to a test, next to device steps). The person can also take over:
// watch records what they do in the same browser.

var aiWeb struct {
	sync.Mutex
	s        *webrun.Session
	step     testrec.WebStep // the opened app or page (its Login, URL, browser, insecure)
	rec      []webrun.Action // the actions that worked, in order
	snap     webrun.Snapshot // the last snapshot (ref numbers point into it)
	prod     bool
	opened   time.Time
	password string
	vars     map[string]string // values given for {{names}} in act (the saved test gets them as its vars)
}

var webAITool = mcpTool{"web", "Drive a real browser with Playwright and record it as a Termwright web test - like a Playwright MCP server whose session is kept. " +
	"open starts a browser on a saved web app session (app: its path, e.g. Web Apps/ExtremeCloud IQ - it logs in by itself with the session's saved login) or on a url; show=false runs it without a window. " +
	"snapshot lists what the page offers: headings, an open dialog and each visible button, link and field as [n] role \"name\" → target (value, options, checked, disabled). " +
	"act does one action (do, target or ref = the [n] of the last snapshot, value) or several (actions); do is one of goto, click, dblclick, fill, type, clear, press, select, check, uncheck, hover, scroll, wait, expect, expect_not, expect_value, expect_count, expect_url, expect_title, read, screenshot, upload, dialog, tab, back, reload. " +
	"Put passwords as {{password}} (the web app session's), never the real one. Every action that works is recorded (with fallback targets for self-healing); expect actions become the test's checks. act answers with the results and a new snapshot. " +
	"text gives the page's text; recorded lists the recording; undo drops the last n recorded actions (the page stays as it is); watch on|off records what the person does in the browser; " +
	"save writes the recording as a web test (file, name; a new test) or as a web step added to a test (append_to, position start|end) - add device steps with the tests tool; close closes the browser (the recording stays until save or discard); discard forgets it. " +
	"Opening and acting on a production web app ask first (other web apps follow the AI settings, as the other tools do).",
	mcpSchema(map[string]any{
		"action":    map[string]any{"type": "string", "enum": []string{"open", "snapshot", "act", "text", "recorded", "undo", "watch", "save", "close", "discard", "status"}, "description": "what to do"},
		"app":       map[string]any{"type": "string", "description": "open: a saved web app session (or any session whose login fills {{username}} / {{password}})"},
		"url":       map[string]any{"type": "string", "description": "open: the page (with app: a path or page of the app; without app: a full address)"},
		"browser":   map[string]any{"type": "string", "enum": []string{"chromium", "firefox", "webkit"}, "description": "open: the browser (default the app's, else chromium)"},
		"insecure":  map[string]any{"type": "boolean", "description": "open: accept self-signed certificates (device web pages)"},
		"show":      map[string]any{"type": "boolean", "description": "open: show the browser window (default true, so the person sees what is done)"},
		"do":        map[string]any{"type": "string", "description": "act: the action"},
		"target":    map[string]any{"type": "string", "description": "act: what to act on - a target from the snapshot (role=button[name=\"Save\"], label=Serial number, testid=…, CSS)"},
		"ref":       map[string]any{"type": "integer", "description": "act: the [n] of an element of the last snapshot (instead of target)"},
		"value":     map[string]any{"type": "string", "description": "act: the text, key, option, address, seconds, expected text or variable name"},
		"actions":   map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": "act: several actions in order ({do, target or ref, value}); it stops at the first that fails"},
		"snapshot":  map[string]any{"type": "boolean", "description": "act: answer with a new snapshot (default true)"},
		"vars":      map[string]any{"type": "object", "description": "act: values for {{names}} in the actions now ({\"serial\": \"SN123\"}); the recording keeps {{serial}} and a new test gets these as its defaults"},
		"n":         map[string]any{"type": "integer", "description": "undo: how many (default 1)"},
		"on":        map[string]any{"type": "boolean", "description": "watch: on or off"},
		"file":      map[string]any{"type": "string", "description": "save: the new test's file, e.g. cases/onboard/onboard-switch.yaml"},
		"name":      map[string]any{"type": "string", "description": "save: the test's (or the step's) name"},
		"append_to": map[string]any{"type": "string", "description": "save: an existing test to add the web step to (instead of file)"},
		"position":  map[string]any{"type": "string", "enum": []string{"end", "start"}, "description": "save with append_to: where the step goes (default end)"},
		"tags":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "save: tags of a new test"},
	}, "action")}

func init() { netTools = append(netTools, webAITool) }

func (a *App) webAI(ctx context.Context, args map[string]any, ask netAsker) (string, error) {
	act := strings.ToLower(argStr(args, "action"))
	aiWeb.Lock()
	defer aiWeb.Unlock()
	if aiWeb.s != nil {
		select {
		case <-aiWeb.s.Closed():
			aiWeb.s = nil
		default:
		}
	}
	need := func() error {
		if aiWeb.s == nil {
			return errors.New("no browser is open: open one first (action open, with app or url)")
		}
		return nil
	}
	switch act {
	case "status", "":
		if aiWeb.s == nil {
			return fmt.Sprintf("No browser is open. %d actions recorded.", len(aiWeb.rec)), nil
		}
		return fmt.Sprintf("A browser is open on %s since %s; %d actions recorded.", orStr(aiWeb.step.Login, aiWeb.step.URL), aiWeb.opened.Format("15:04"), len(aiWeb.rec)), nil
	case "open":
		app, u := strings.TrimSpace(argStr(args, "app")), strings.TrimSpace(argStr(args, "url"))
		if app == "" && u == "" {
			return "", errors.New("give app (a saved web app session) or url")
		}
		prod := false
		if app != "" {
			s, err := a.store.Get(app)
			if err != nil {
				return "", fmt.Errorf("no saved session %q (the web app session's path, e.g. Web Apps/CRM)", app)
			}
			prod = s.Environment == "production"
		}
		if err := ask("Open a browser for the AI", "The AI drives a browser on "+orStr(app, u)+" and records what it does as a web test.", prod); err != nil {
			return "", err
		}
		if aiWeb.s != nil {
			aiWeb.s.Close()
			aiWeb.s = nil
		}
		w := testrec.WebStep{Login: app, URL: u, Browser: argStr(args, "browser"), Insecure: webArgBool(args, "insecure")}
		w2, x, shots := a.webExtra(w)
		x.Headless = webArgFalse(args, "show")
		s, err := webRunner().Open(context.Background(), w2, shots, x)
		if err != nil {
			return "", err
		}
		if aiWeb.step.Login != w.Login || aiWeb.step.URL != w.URL { // another app: a new recording
			aiWeb.rec = nil
		}
		aiWeb.s, aiWeb.step, aiWeb.prod, aiWeb.opened = s, w, prod, time.Now()
		if aiWeb.vars == nil || len(aiWeb.rec) == 0 {
			aiWeb.vars = map[string]string{}
		}
		aiWeb.password = ""
		if app != "" {
			if sess, err := a.store.Get(app); err == nil {
				aiWeb.password, _ = a.secret(sess.Path)
			}
		}
		s.OnRec = func(ac webrun.Action) { // what the person does while watch is on
			go func() { // not here: the browser's reader must not wait for a tool call that waits for it
				aiWeb.Lock()
				switch ac.Do {
				case "__undo":
					if len(aiWeb.rec) > 0 {
						aiWeb.rec = aiWeb.rec[:len(aiWeb.rec)-1]
					}
				case "__done":
				default:
					aiWeb.rec = append(aiWeb.rec, ac)
				}
				aiWeb.Unlock()
				emitter{a}.Emit("web:ai", map[string]any{"recorded": ac})
			}()
		}
		emitter{a}.Emit("web:ai", map[string]any{"open": true, "app": orStr(app, u)})
		return "Opened " + orStr(app, u) + ".\n" + a.webAISnap(), nil
	case "snapshot":
		if err := need(); err != nil {
			return "", err
		}
		return a.webAISnap(), nil
	case "text":
		if err := need(); err != nil {
			return "", err
		}
		u, title, text, err := aiWeb.s.Page()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s - %s\n\n%s", title, u, text), nil
	case "act":
		if err := need(); err != nil {
			return "", err
		}
		var acts []webrun.Action
		if l, ok := args["actions"].([]any); ok {
			for _, x := range l {
				m, _ := x.(map[string]any)
				ac, err := webAIAction(m)
				if err != nil {
					return "", err
				}
				acts = append(acts, ac)
			}
		} else {
			ac, err := webAIAction(args)
			if err != nil {
				return "", err
			}
			acts = []webrun.Action{ac}
		}
		if aiWeb.prod {
			var l []string
			for _, ac := range acts {
				l = append(l, webAIWords(ac))
			}
			if err := ask("The AI acts on a production web app", orStr(aiWeb.step.Login, aiWeb.step.URL)+": "+strings.Join(l, "; "), true); err != nil {
				return "", err
			}
		}
		w := aiWeb.step
		w.URL, w.Actions = "", acts
		vars := map[string]string{}
		if m, ok := args["vars"].(map[string]any); ok {
			for k, v := range m {
				vars[k] = fmt.Sprint(v)
				aiWeb.vars[k] = fmt.Sprint(v)
			}
		}
		for k, v := range aiWeb.vars {
			if _, ok := vars[k]; !ok {
				vars[k] = v
			}
		}
		w = testrec.FillWeb(w, vars) // {{serial}}: this run's value; the recording keeps {{serial}}
		w2, _, _ := a.webExtra(w)    // {{password}}, {{username}} and blocks filled in as in a run
		res, err := aiWeb.s.Run(w2.Actions, len(aiWeb.rec))
		var b strings.Builder
		done := 0
		for i, r := range res {
			if i >= len(acts) {
				break
			}
			if r.OK {
				ac := acts[i]
				if aiWeb.password != "" && ac.Value == aiWeb.password { // never the password itself in a test
					ac.Value = "{{password}}"
				}
				if r.Healed != "" {
					ac.Target = r.Healed
				}
				aiWeb.rec = append(aiWeb.rec, ac)
				done++
				fmt.Fprintf(&b, "✓ %s%s\n", webAIWords(acts[i]), orStr(webAINote(r.Note), ""))
			} else {
				fmt.Fprintf(&b, "✗ %s: %s\n", webAIWords(acts[i]), r.Error)
			}
		}
		if err != nil && len(res) == 0 {
			fmt.Fprintf(&b, "✗ %s\n", err)
		}
		fmt.Fprintf(&b, "(%d recorded, %d in all)\n", done, len(aiWeb.rec))
		emitter{a}.Emit("web:ai", map[string]any{"count": len(aiWeb.rec)})
		if !webArgFalse(args, "snapshot") {
			time.Sleep(300 * time.Millisecond) // let the page settle
			b.WriteString("\n" + a.webAISnap())
		}
		return b.String(), nil
	case "recorded":
		if len(aiWeb.rec) == 0 {
			return "Nothing recorded yet.", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "On %s:\n", orStr(aiWeb.step.Login, aiWeb.step.URL))
		for i, ac := range aiWeb.rec {
			fmt.Fprintf(&b, "%d. %s\n", i+1, webAIWords(ac))
		}
		return b.String(), nil
	case "undo":
		n := argInt(args, "n", 1, 10000)
		if n <= 0 {
			n = 1
		}
		n = min(n, len(aiWeb.rec))
		aiWeb.rec = aiWeb.rec[:len(aiWeb.rec)-n]
		return fmt.Sprintf("Dropped %d; %d recorded. The page is as it was - act again if it must change back.", n, len(aiWeb.rec)), nil
	case "watch":
		if err := need(); err != nil {
			return "", err
		}
		on := webArgBool(args, "on")
		if err := aiWeb.s.Record(on); err != nil {
			return "", err
		}
		if on {
			return "Recording what the person does in the browser; watch off (or snapshot / recorded) to see it.", nil
		}
		return fmt.Sprintf("Stopped; %d recorded.", len(aiWeb.rec)), nil
	case "save":
		if len(aiWeb.rec) == 0 {
			return "", errors.New("nothing recorded: act first")
		}
		name := strings.TrimSpace(argStr(args, "name"))
		st := testrec.Step{Name: orStr(name, "web on "+path.Base(orStr(aiWeb.step.Login, aiWeb.step.URL)))}
		w := aiWeb.step
		w.Actions = append([]webrun.Action(nil), aiWeb.rec...)
		st.Web = &w
		yb, _ := yaml.Marshal([]testrec.Step{st})
		if to := strings.TrimSpace(argStr(args, "append_to")); to != "" {
			f, err := a.testsFolder()
			if err != nil {
				return "", err
			}
			c, err := f.Load(to)
			if err != nil {
				return "", err
			}
			if err := ask("Add the AI's web step to "+to, fmt.Sprintf("%d actions on %s", len(w.Actions), orStr(w.Login, w.URL)), false); err != nil {
				return "", err
			}
			if strings.EqualFold(argStr(args, "position"), "start") {
				c.Steps = append([]testrec.Step{st}, c.Steps...)
			} else {
				c.Steps = append(c.Steps, st)
			}
			rel, err := a.TestSave(to, c)
			if err != nil {
				return "", err
			}
			aiWeb.rec = nil
			return "Added the web step to " + rel + ":\n" + string(yb), nil
		}
		file := strings.TrimSpace(argStr(args, "file"))
		if file == "" {
			return "", errors.New("give file (a new test, e.g. cases/onboard/onboard-switch.yaml) or append_to (an existing test)")
		}
		tags := argStrs(args, "tags")
		if len(tags) == 0 {
			tags = []string{"web", "ai"}
		}
		c := testrec.Case{Name: orStr(name, strings.TrimSuffix(path.Base(file), ".yaml")), Tags: tags, Steps: []testrec.Step{st}}
		if len(aiWeb.vars) > 0 { // the values used while recording are the test's defaults (a testbed or run can change them)
			c.Vars = map[string]string{}
			for k, v := range aiWeb.vars {
				c.Vars[k] = v
			}
		}
		if err := ask("Save the AI's web test "+file, fmt.Sprintf("%d actions on %s", len(w.Actions), orStr(w.Login, w.URL)), false); err != nil {
			return "", err
		}
		rel, err := a.TestSave(file, c)
		if err != nil {
			return "", err
		}
		aiWeb.rec, aiWeb.vars = nil, map[string]string{}
		return "Saved " + rel + " (its web app is a role: a testbed can bring its own). The step:\n" + string(yb), nil
	case "close":
		if aiWeb.s != nil {
			aiWeb.s.Close()
			aiWeb.s = nil
		}
		emitter{a}.Emit("web:ai", map[string]any{"open": false})
		return fmt.Sprintf("Closed; %d actions recorded (save or discard them).", len(aiWeb.rec)), nil
	case "discard":
		n := len(aiWeb.rec)
		aiWeb.rec, aiWeb.vars = nil, map[string]string{}
		return fmt.Sprintf("Forgot %d recorded actions.", n), nil
	}
	return "", fmt.Errorf("unknown action %q", act)
}

func webAIAction(m map[string]any) (webrun.Action, error) {
	ac := webrun.Action{Do: strings.ToLower(strings.TrimSpace(argStr(m, "do"))), Target: strings.TrimSpace(argStr(m, "target")), Value: webArgRaw(m, "value")}
	if ac.Do == "" {
		return ac, errors.New("act: give do (click, fill, select, expect …)")
	}
	if r := webArgInt(m, "ref"); r > 0 {
		if r > len(aiWeb.snap.Elements) {
			return ac, fmt.Errorf("no [%d] in the last snapshot (take a new one)", r)
		}
		e := aiWeb.snap.Elements[r-1]
		ac.Target, ac.Alt = e.Target, e.Alt
	}
	return ac, nil
}

func webAIWords(ac webrun.Action) string {
	s := ac.Do
	if ac.Target != "" {
		s += " " + ac.Target
	}
	if ac.Value != "" {
		v := ac.Value
		if strings.Contains(strings.ToLower(ac.Target), "pass") && v != "{{password}}" {
			v = "••••"
		}
		s += " = " + strconv.Quote(v)
	}
	return s
}

func webAINote(n string) string {
	if n == "" {
		return ""
	}
	return " (" + n + ")"
}

// webAISnap is the page's snapshot as text; [n] can be used as ref in act.
func (a *App) webAISnap() string {
	sn, err := aiWeb.s.Snapshot()
	if err != nil {
		return "(no snapshot: " + err.Error() + ")"
	}
	aiWeb.snap = sn
	var b strings.Builder
	fmt.Fprintf(&b, "Page: %s - %s\n", sn.Title, sn.URL)
	if sn.Dialog != "" {
		fmt.Fprintf(&b, "Dialog open: %s\n", sn.Dialog)
	}
	if len(sn.Headings) > 0 {
		fmt.Fprintf(&b, "Headings: %s\n", strings.Join(sn.Headings, " | "))
	}
	for i, e := range sn.Elements {
		fmt.Fprintf(&b, "[%d] %s", i+1, e.Role)
		if e.Name != "" {
			fmt.Fprintf(&b, " %q", e.Name)
		}
		fmt.Fprintf(&b, " → %s", e.Target)
		switch {
		case e.Secret:
			b.WriteString(" (password: use {{password}})")
		case e.Value != "":
			fmt.Fprintf(&b, " (value %q)", e.Value)
		}
		if len(e.Options) > 0 {
			fmt.Fprintf(&b, " options: %s", strings.Join(e.Options, " | "))
		}
		if e.Checked {
			b.WriteString(" (checked)")
		}
		if e.Disabled {
			b.WriteString(" (disabled)")
		}
		b.WriteByte('\n')
	}
	if len(sn.Elements) == 0 {
		b.WriteString("(nothing to act on is visible - text gives the page's text)\n")
	}
	return b.String()
}

func webArgBool(m map[string]any, k string) bool {
	switch v := m[k].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || v == "on" || v == "1"
	case float64:
		return v != 0
	}
	return false
}

// webArgFalse: the argument is given and false.
func webArgFalse(m map[string]any, k string) bool {
	if _, ok := m[k]; !ok {
		return false
	}
	return !webArgBool(m, k)
}

func webArgInt(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(strings.Trim(v, "[]")))
		return n
	}
	return 0
}

// webArgRaw: a value as given (spaces kept: they may be what is typed).
func webArgRaw(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// WebAIPrepare opens the AI's browser for a web step being edited ("Ask AI"): at the step's page, with its
// actions done (not recorded), so the AI goes on from where the step ends. What it does with the web tool is
// recorded; WebAITake gives it back to add to the step.
func (a *App) WebAIPrepare(w testrec.WebStep, vars map[string]string) error {
	aiWeb.Lock()
	defer aiWeb.Unlock()
	if aiWeb.s != nil {
		aiWeb.s.Close()
		aiWeb.s = nil
	}
	w = testrec.FillWeb(w, vars) // {{xiq}} (a role) is its session
	acts := w.Actions
	w.Actions = nil
	w2, x, shots := a.webExtra(w)
	x.RecVars = vars
	s, err := webRunner().Open(context.Background(), w2, shots, x)
	if err != nil {
		return err
	}
	if len(acts) > 0 {
		run := w
		run.URL, run.Actions = "", acts
		r2, _, _ := a.webExtra(run)
		res, err := s.Run(r2.Actions, 0)
		for _, r := range res {
			if !r.OK {
				err = fmt.Errorf("action %d does not work now (%s): fix it first, or ask the AI from there", r.I+1, r.Error)
			}
		}
		if err != nil {
			s.Close()
			return err
		}
	}
	aiWeb.s, aiWeb.step, aiWeb.opened, aiWeb.rec, aiWeb.prod = s, testrec.WebStep{Login: w.Login, URL: w.URL, Browser: w.Browser, Insecure: w.Insecure}, time.Now(), nil, false
	aiWeb.vars = map[string]string{}
	for k, v := range vars {
		aiWeb.vars[k] = v
	}
	aiWeb.password = ""
	if w.Login != "" {
		if sess, err := a.store.Get(w.Login); err == nil {
			aiWeb.password, _ = a.secret(sess.Path)
		}
	}
	emitter{a}.Emit("web:ai", map[string]any{"open": true, "app": orStr(w.Login, w.URL), "count": 0})
	return nil
}

// WebAITake gives the actions the AI recorded since WebAIPrepare and closes its browser.
func (a *App) WebAITake() []webrun.Action {
	aiWeb.Lock()
	defer aiWeb.Unlock()
	out := append([]webrun.Action(nil), aiWeb.rec...)
	aiWeb.rec = nil
	if aiWeb.s != nil {
		aiWeb.s.Close()
		aiWeb.s = nil
	}
	emitter{a}.Emit("web:ai", map[string]any{"open": false, "count": 0})
	return out
}
