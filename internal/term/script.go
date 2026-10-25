package term

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/termwright/termwright/internal/logging"
)

// scriptRun is one running JavaScript automation script.
type scriptRun struct {
	id     string
	tabID  string
	name   string
	ctx    context.Context
	cancel context.CancelFunc
	vm     *goja.Runtime
}

// ScriptAPI documents the objects available to scripts (see docs/SCRIPTING.md):
//
//	term.send(text)                 send text (no implicit Enter)
//	term.sendLine(text)             send text + Enter
//	term.waitFor(pattern, [ms])     wait for text or RegExp; true / false on timeout
//	term.waitForAny([p...], [ms])   index of the first pattern seen, -1 on timeout
//	term.readUntil(pattern, [ms])   text received before the pattern (null on timeout)
//	term.screen()                   recent output (plain text)
//	term.connected()                true while connected
//	term.waitConnected([ms])        wait until the tab is connected
//	openSession(path)               open a saved session in a new tab -> term object
//	tabs()                          list of {id, title, state}
//	sleep(ms), log(msg), prompt(msg, [default], [secret]) -> string|null, confirm(msg) -> bool
//	scriptPath                      path of the running script file

// termObj buffers a tab's output for wait functions.
type termObj struct {
	m      *Manager
	tab    *Tab
	run    *scriptRun
	mu     sync.Mutex
	buf    string
	recent string
	strip  logging.Stripper
	notify chan struct{}
	unsub  func()
}

func (m *Manager) newTermObj(run *scriptRun, t *Tab) *termObj {
	o := &termObj{m: m, tab: t, run: run, notify: make(chan struct{}, 1)}
	o.unsub = t.subscribe(func(p []byte) {
		o.mu.Lock()
		txt := string(o.strip.Strip(p))
		o.buf += txt
		if len(o.buf) > 1<<20 {
			o.buf = o.buf[len(o.buf)-(1<<20):]
		}
		o.recent += txt
		if len(o.recent) > 16384 {
			o.recent = o.recent[len(o.recent)-16384:]
		}
		o.mu.Unlock()
		select {
		case o.notify <- struct{}{}:
		default:
		}
	})
	return o
}

// matcher converts a JS string or RegExp to a Go regexp.
func (o *termObj) matcher(v goja.Value) (*regexp.Regexp, error) {
	if obj, ok := v.(*goja.Object); ok && obj.ClassName() == "RegExp" {
		src := obj.Get("source").String()
		flags := obj.Get("flags").String()
		if strings.Contains(flags, "i") {
			src = "(?i)" + src
		}
		return regexp.Compile(src)
	}
	return regexp.Compile(regexp.QuoteMeta(v.String()))
}

// wait blocks until one of res matches the buffered output. It returns the
// index of the matching pattern and the text before the match.
func (o *termObj) wait(res []*regexp.Regexp, timeout time.Duration) (int, string, error) {
	var deadline <-chan time.Time
	if timeout > 0 {
		tm := time.NewTimer(timeout)
		defer tm.Stop()
		deadline = tm.C
	}
	for {
		o.mu.Lock()
		best, start, end := -1, -1, -1
		for i, re := range res {
			if loc := re.FindStringIndex(o.buf); loc != nil && (best < 0 || loc[0] < start) {
				best, start, end = i, loc[0], loc[1]
			}
		}
		if best >= 0 {
			before := o.buf[:start]
			o.buf = o.buf[end:]
			o.mu.Unlock()
			return best, before, nil
		}
		o.mu.Unlock()
		select {
		case <-o.notify:
		case <-deadline:
			return -1, "", nil
		case <-o.run.ctx.Done():
			return -1, "", ErrCancelled
		}
	}
}

func ms(v goja.Value) time.Duration {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return 0
	}
	return time.Duration(v.ToInteger()) * time.Millisecond
}

func (o *termObj) js(vm *goja.Runtime) *goja.Object {
	obj := vm.NewObject()
	throw := func(err error) { panic(vm.NewGoError(err)) }
	_ = obj.Set("id", o.tab.id)
	_ = obj.Set("send", func(s string) {
		if err := o.tab.rawWrite([]byte(s)); err != nil {
			throw(err)
		}
	})
	_ = obj.Set("sendLine", func(s string) {
		if err := o.tab.rawWrite([]byte(s + "\r")); err != nil {
			throw(err)
		}
	})
	_ = obj.Set("waitFor", func(p goja.Value, t goja.Value) bool {
		re, err := o.matcher(p)
		if err != nil {
			throw(err)
		}
		i, _, err := o.wait([]*regexp.Regexp{re}, ms(t))
		if err != nil {
			throw(err)
		}
		return i == 0
	})
	_ = obj.Set("waitForAny", func(ps []goja.Value, t goja.Value) int {
		var res []*regexp.Regexp
		for _, p := range ps {
			re, err := o.matcher(p)
			if err != nil {
				throw(err)
			}
			res = append(res, re)
		}
		i, _, err := o.wait(res, ms(t))
		if err != nil {
			throw(err)
		}
		return i
	})
	_ = obj.Set("readUntil", func(p goja.Value, t goja.Value) goja.Value {
		re, err := o.matcher(p)
		if err != nil {
			throw(err)
		}
		i, before, err := o.wait([]*regexp.Regexp{re}, ms(t))
		if err != nil {
			throw(err)
		}
		if i < 0 {
			return goja.Null()
		}
		return vm.ToValue(before)
	})
	_ = obj.Set("screen", func() string {
		o.mu.Lock()
		defer o.mu.Unlock()
		return o.recent
	})
	_ = obj.Set("clear", func() {
		o.mu.Lock()
		o.buf = ""
		o.mu.Unlock()
	})
	_ = obj.Set("connected", func() bool { return o.tab.isConnected() })
	_ = obj.Set("waitConnected", func(t goja.Value) bool {
		d := ms(t)
		if d == 0 {
			d = 60 * time.Second
		}
		end := time.Now().Add(d)
		for time.Now().Before(end) {
			if o.tab.isConnected() {
				return true
			}
			select {
			case <-o.run.ctx.Done():
				throw(ErrCancelled)
			case <-time.After(100 * time.Millisecond):
			}
		}
		return false
	})
	return obj
}

// RunScriptFile runs a JavaScript file against a tab. args is a command line
// (quotes allowed) exposed to the script as the scriptArgs array.
func (m *Manager) RunScriptFile(tabID, file, args string) (string, error) {
	file = expandHome(file)
	switch strings.ToLower(filepath.Ext(file)) {
	case ".py":
		return m.runPython(tabID, file, splitArgs(args))
	case ".vbs":
		return m.runVBScript(tabID, file, splitArgs(args))
	}
	code, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return m.runScript(tabID, file, string(code), splitArgs(args))
}

// splitArgs splits a command line on spaces, honouring "double" and 'single' quotes.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	inArg := false
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ' || r == '\t':
			if inArg {
				out = append(out, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if inArg {
		out = append(out, cur.String())
	}
	return out
}

// RunScript runs JavaScript code against a tab and returns the run id.
// Progress is reported with "script:status" events.
func (m *Manager) RunScript(tabID, name, code string) (string, error) {
	return m.runScript(tabID, name, code, nil)
}

func (m *Manager) runScript(tabID, name, code string, args []string) (string, error) {
	t, err := m.get(tabID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	vm := goja.New()
	run := &scriptRun{id: newID(), tabID: tabID, name: filepath.Base(name), ctx: ctx, cancel: cancel, vm: vm}
	main := m.newTermObj(run, t)
	objs := []*termObj{main}
	var objMu sync.Mutex
	throw := func(err error) { panic(vm.NewGoError(err)) }

	_ = vm.Set("term", main.js(vm))
	_ = vm.Set("scriptPath", name)
	if args == nil {
		args = []string{}
	}
	_ = vm.Set("scriptArgs", args)
	_ = vm.Set("sleep", func(v goja.Value) {
		select {
		case <-time.After(ms(v)):
		case <-ctx.Done():
			throw(ErrCancelled)
		}
	})
	_ = vm.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, a := range call.Arguments {
			parts[i] = a.String()
		}
		m.emit.Emit("script:log", map[string]string{"runId": run.id, "tabId": tabID, "message": strings.Join(parts, " ")})
		return goja.Undefined()
	})
	_ = vm.Set("prompt", func(msg string, def goja.Value, secret bool) goja.Value {
		d := ""
		if def != nil && !goja.IsUndefined(def) && !goja.IsNull(def) {
			d = def.String()
		}
		r, err := m.prompts.Ask(ctx, Prompt{TabID: tabID, Kind: "credentials", Title: "Script: " + run.name, Message: msg,
			Fields: []PromptField{{Label: "Value", Secret: secret, Value: d}}})
		if err != nil {
			return goja.Null()
		}
		return vm.ToValue(r.Values[0])
	})
	_ = vm.Set("confirm", func(msg string) bool {
		_, err := m.prompts.Ask(ctx, Prompt{TabID: tabID, Kind: "confirm", Title: "Script: " + run.name, Message: msg, Choices: []string{"OK"}})
		return err == nil
	})
	_ = vm.Set("tabs", func() []map[string]string {
		m.mu.Lock()
		defer m.mu.Unlock()
		var out []map[string]string
		for _, x := range m.tabs {
			x.mu.Lock()
			out = append(out, map[string]string{"id": x.id, "title": title(x.sess), "state": x.state})
			x.mu.Unlock()
		}
		return out
	})
	_ = vm.Set("openSession", func(path string) *goja.Object {
		if m.LoadSession == nil {
			throw(errors.New("sessions are not available"))
		}
		s, err := m.LoadSession(path)
		if err != nil {
			throw(err)
		}
		ti := m.Create(s)
		nt, _ := m.get(ti.ID)
		o := m.newTermObj(run, nt)
		objMu.Lock()
		objs = append(objs, o)
		objMu.Unlock()
		m.emit.Emit("tab:opened", ti) // the UI attaches a terminal and starts it
		return o.js(vm)
	})

	m.mu.Lock()
	m.scripts[run.id] = run
	m.mu.Unlock()
	stop := context.AfterFunc(ctx, func() { vm.Interrupt(ErrCancelled) })
	status := func(state, msg string) {
		m.emit.Emit("script:status", map[string]string{"runId": run.id, "tabId": tabID, "name": run.name, "state": state, "message": msg})
	}
	status("running", "")
	go func() {
		defer func() {
			stop()
			cancel()
			objMu.Lock()
			for _, o := range objs {
				o.unsub()
			}
			objMu.Unlock()
			m.mu.Lock()
			delete(m.scripts, run.id)
			m.mu.Unlock()
		}()
		_, err := vm.RunScript(name, code)
		var ie *goja.InterruptedError
		switch {
		case err == nil:
			status("done", "")
		case errors.As(err, &ie) || errors.Is(err, ErrCancelled) || ctx.Err() != nil:
			status("cancelled", "")
		default:
			status("error", fmt.Sprint(err))
		}
	}()
	return run.id, nil
}

// CancelScript stops a running script.
func (m *Manager) CancelScript(runID string) {
	m.mu.Lock()
	r := m.scripts[runID]
	m.mu.Unlock()
	if r != nil {
		r.cancel()
	}
}
