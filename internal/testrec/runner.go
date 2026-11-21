package testrec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/termwright/termwright/internal/apiclient"
	"github.com/termwright/termwright/internal/textfsm"
)

// schemaRoot is the tests folder schema files are read from (set by a folder's run).
var schemaRoot atomic.Value

func init() { schemaRoot.Store(".") }

// Driver runs the steps: the app runs commands on saved sessions and sends HTTP requests; tests
// use a fake one.
type Driver interface {
	// Command sends cmd on the saved session and returns its output (no echo, no prompt).
	Command(ctx context.Context, session, cmd, prompt string) (string, error)
	// HTTP sends a request.
	HTTP(ctx context.Context, r HTTPStep) (HTTPAnswer, error)
	// SNMP runs an SNMP operation.
	SNMP(ctx context.Context, r SNMPStep) ([]SNMPValue, error)
}

// HTTPAnswer is what an HTTP step got.
type HTTPAnswer struct {
	Status int    // the status code (0: nothing came back)
	Body   string // the body as text
}

// Result is one run of a case.
type Result struct {
	// Case: its name.
	Case string `json:"case"`
	// File: the case file, relative to the tests folder.
	File string `json:"file,omitempty"`
	// Start of the run.
	Start time.Time `json:"start"`
	// Ms: how long the whole case took.
	Ms int64 `json:"ms"`
	// Passed: every step (setup and cleanup too) passed.
	Passed bool `json:"passed"`
	// Log: the test's run log (every step: sent, the full answer, checks) in the run's folder.
	Log string `json:"log,omitempty"`
	// QTestRun: the qTest test run the result was sent to (TR-81); Defect / DefectURL: the Jira issue
	// opened for it.
	QTestRun  string `json:"qtestRun,omitempty"`
	Defect    string `json:"defect,omitempty"`
	DefectURL string `json:"defectUrl,omitempty"`
	// Versions: the software each device of the case ran (session → version).
	Versions map[string]string `json:"versions,omitempty"`
	// CleanupFailed: a cleanup step (or the restore) failed - the device may not be clean; the case
	// failed because of its cleanup.
	CleanupFailed bool `json:"cleanupFailed,omitempty"`
	// Skipped: the case did not run (Error says why: an earlier cleanup failed).
	Skipped bool `json:"skipped,omitempty"`
	// Error: why the case could not run at all (a bad file).
	Error string `json:"error,omitempty"`
	// Steps: one per step that ran (steps after a failure are Skipped).
	Steps []StepResult `json:"steps"`
}

// StepResult is one step of a run.
type StepResult struct {
	// Name of the step.
	Name string `json:"name"`
	// Kind: run, http or snmp.
	Kind string `json:"kind"`
	// Phase: setup, cleanup or restore ("" = the test's own steps).
	Phase string `json:"phase,omitempty"`
	// Target: the session or the URL.
	Target string `json:"target,omitempty"`
	// Sent: the command or "METHOD url" sent (variables filled in).
	Sent string `json:"sent,omitempty"`
	// Status: the HTTP status.
	Status int `json:"status,omitempty"`
	// Output: what came back (cut at 64 KB).
	Output string `json:"output,omitempty"`
	// Ms: how long the step took.
	Ms int64 `json:"ms"`
	// Passed: it ran and every check passed.
	Passed bool `json:"passed"`
	// Skipped: an earlier step failed.
	Skipped bool `json:"skipped,omitempty"`
	// Error: why it could not run.
	Error string `json:"error,omitempty"`
	// Rows: the values an SNMP step got.
	Rows []SNMPValue `json:"rows,omitempty"`
	// Unchecked: it passed without checks - it ran, nothing was checked.
	Unchecked bool `json:"unchecked,omitempty"`
	// Expected: the step's expected result in words.
	Expected string `json:"expected,omitempty"`
	// Table: the parsed output (steps with parse).
	Table *textfsm.Table `json:"table,omitempty"`
	// Checks: one per expect.
	Checks []CheckResult `json:"checks,omitempty"`
}

// CheckResult is one expect of a step.
type CheckResult struct {
	// What was checked, e.g. `status = 200`.
	What string `json:"what"`
	// Passed: it held.
	Passed bool `json:"passed"`
	// Got: the value found ("" when not found).
	Got string `json:"got,omitempty"`
}

const maxOut = 64 << 10

// DeviceErrors is a Driver that knows the error messages of the devices' types: DeviceError returns
// the line of output in which the device refused a command ("" when it did not).
type DeviceErrors interface {
	DeviceError(session, output string) string
}

// CaseEnder is a Driver that keeps logins open while a case runs (one per session instead of one
// per step); EndCase is called when the case is done so it logs out.
type CaseEnder interface{ EndCase() }

// Run runs a case. vars win over the case's own.
func Run(ctx context.Context, c Case, d Driver, vars map[string]string) Result {
	return RunWith(ctx, c, d, vars, RunOptions{})
}

// Undoer gives the command that undoes a configuration command on a session (its device type's
// undo rules; "" = none needed or unknown) - for RunOptions.Cleanup "auto".
type Undoer interface {
	UndoOf(session, command string) string
}

// Cleanup modes of RunOptions.
const (
	CleanupSteps = ""     // run each test's cleanup steps and restore (the default)
	CleanupOff   = "off"  // no cleanup and no restore: the next test starts from what this one left
	CleanupAuto  = "auto" // the cleanup steps; a test without them undoes the configuration commands that ran
)

// RunWith runs a case with run options (the cleanup mode).
func RunWith(ctx context.Context, c Case, d Driver, vars map[string]string, opt RunOptions) Result {
	if opt.Cleanup == CleanupOff {
		c.Restore = nil
	}
	if e, ok := d.(CaseEnder); ok {
		defer e.EndCase()
	}
	r := Result{Case: c.Name, Start: time.Now(), Passed: true}
	v := map[string]string{}
	for k, x := range c.Vars {
		v[k] = x
	}
	for k, x := range vars {
		v[k] = x
	}
	// restore: save the devices' state first; put it back after the cleanup
	var restore func(context.Context) []StepResult
	if len(c.Restore) > 0 {
		rs, ok := d.(Restorer)
		if !ok {
			r.Passed, r.Error = false, "this runner cannot save and restore devices"
			r.Ms = time.Since(r.Start).Milliseconds()
			return r
		}
		var save []StepResult
		save, restore = rs.SaveState(ctx, c.Restore, Sessions(c, v))
		for i := range save {
			save[i].Phase = "restore"
			r.Steps = append(r.Steps, save[i])
			if !save[i].Passed {
				r.Passed = false
			}
		}
	}
	failed := !r.Passed
	// watching and the configuration before the test
	var mark any
	if c.Watch != nil {
		if w, ok := d.(Watcher); ok {
			var err error
			if mark, err = w.WatchMark(c.Watch); err != nil {
				r.Steps = append(r.Steps, StepResult{Name: "watch syslog and SNMP notifications", Kind: "watch", Error: err.Error()})
				r.Passed, failed = false, true
			}
		} else {
			r.Steps = append(r.Steps, StepResult{Name: "watch", Kind: "watch", Error: "this runner cannot watch syslog or SNMP notifications"})
			r.Passed, failed = false, true
		}
	}
	before := map[string]string{}
	sess := Sessions(c, v)
	snap, canSnap := d.(Snapshotter)
	if c.ConfigDiff && opt.Cleanup != CleanupOff && !failed {
		if !canSnap {
			r.Steps = append(r.Steps, StepResult{Name: "save the configuration to compare", Kind: "config", Phase: "setup", Error: "this runner cannot read configurations"})
			r.Passed, failed = false, true
		}
		for _, s := range sess {
			if !canSnap {
				break
			}
			cfg, err := snap.ConfigSnapshot(ctx, s)
			if err != nil {
				r.Steps = append(r.Steps, StepResult{Name: "read the configuration of " + s, Kind: "config", Target: s, Phase: "setup", Error: err.Error()})
				r.Passed, failed = false, true
				break
			}
			before[s] = cfg
		}
	}
	run := func(list []Step, phase string, always bool) {
		for i, st := range list {
			if (failed && !always) || (ctx.Err() != nil && !always) {
				r.Steps = append(r.Steps, StepResult{Name: Fill(stepName(st, i), v), Kind: stepKind(st), Phase: phase, Skipped: true})
				continue
			}
			sctx := ctx
			if always && ctx.Err() != nil { // the run was stopped: the cleanup still gets its time
				var cancel context.CancelFunc
				sctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
			}
			if opt.OnStepStart != nil {
				opt.OnStepStart(opt.file, r.Case, PhaseLabel(phase)+Fill(stepName(st, i), v))
			}
			for _, sr := range execStep(sctx, st, i, d, v) {
				sr.Phase = phase
				r.Steps = append(r.Steps, sr)
				if opt.OnStep != nil {
					opt.OnStep(opt.file, r.Case, sr)
				}
				if !sr.Passed && !sr.Skipped {
					r.Passed = false
					if phase == "cleanup" {
						r.CleanupFailed = true
					} else if !st.ContinueOnFail {
						failed = true
					}
				}
			}
		}
	}
	run(c.Setup, "setup", false)
	run(c.Steps, "", false)
	switch {
	case opt.Cleanup == CleanupOff:
		for i, st := range c.Cleanup {
			r.Steps = append(r.Steps, StepResult{Name: Fill(stepName(st, i), v), Kind: stepKind(st), Phase: "cleanup", Skipped: true, Error: "cleanup is off for this run"})
		}
	case opt.Cleanup == CleanupAuto && len(c.Cleanup) == 0:
		run(autoUndo(c, r.Steps, d, v), "cleanup", true)
	default:
		run(c.Cleanup, "cleanup", true)
	}
	if mark != nil {
		hits, err := d.(Watcher).WatchHits(mark, *c.Watch, sess)
		sr := StepResult{Name: "nothing unwanted in syslog or SNMP notifications", Kind: "watch", Passed: err == nil && len(hits) == 0, Output: strings.Join(hits, "\n")}
		switch {
		case err != nil:
			sr.Error = err.Error()
		case len(hits) > 0:
			sr.Error = fmt.Sprintf("%d unwanted while the test ran: %s", len(hits), hits[0])
		}
		r.Steps = append(r.Steps, sr)
		r.Passed = r.Passed && sr.Passed
	}
	if len(before) > 0 {
		var ign []*regexp.Regexp
		for _, p := range c.ConfigIgnore {
			if re, err := regexp.Compile(p); err == nil {
				ign = append(ign, re)
			}
		}
		for _, s := range sess {
			old, ok := before[s]
			if !ok {
				continue
			}
			sr := StepResult{Name: "configuration back as before: " + s, Kind: "config", Target: s, Phase: "cleanup"}
			now, err := snap.ConfigSnapshot(context.Background(), s)
			if err != nil {
				sr.Error = err.Error()
			} else if d := ConfigDiff(old, now, ign); d != "" {
				sr.Error, sr.Output = "the configuration differs after the cleanup", d
			} else {
				sr.Passed = true
			}
			r.Steps = append(r.Steps, sr)
			if !sr.Passed {
				r.Passed, r.CleanupFailed = false, true
			}
		}
	}
	if restore != nil {
		rctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		for _, sr := range restore(rctx) {
			sr.Phase = "restore"
			r.Steps = append(r.Steps, sr)
			if !sr.Passed {
				r.Passed, r.CleanupFailed = false, true
			}
		}
	}
	if len(c.Steps) == 0 {
		r.Passed, r.Error = false, "the test has no steps"
	}
	r.Ms = time.Since(r.Start).Milliseconds()
	return r
}

// autoUndo: the undo of every configuration command of the test's setup and steps that ran without an
// error, the last first.
func autoUndo(c Case, done []StepResult, d Driver, v map[string]string) []Step {
	u, ok := d.(Undoer)
	if !ok {
		return nil
	}
	var out []Step
	ran := map[string]bool{}
	for _, sr := range done {
		if sr.Kind == "run" && !sr.Skipped && sr.Error == "" {
			ran[sr.Target+"\x00"+sr.Sent] = true
		}
	}
	for _, list := range [][]Step{c.Setup, c.Steps} {
		for _, st := range list {
			if st.Run == nil {
				continue
			}
			s, cmd := Fill(st.Run.Session, v), Fill(st.Run.Command, v)
			if !ran[s+"\x00"+cmd] {
				continue
			}
			if x := u.UndoOf(s, cmd); x != "" {
				out = append([]Step{{Name: "undo: " + cmd, Run: &RunStep{Session: s, Command: x}}}, out...)
			}
		}
	}
	return out
}

// Restorer is a Driver that can save the devices' state before a test and put it back after it
// (Case.Restore). SaveState returns its results (one per device and kind) and the function that
// restores; that function returns the restore's results.
type Restorer interface {
	SaveState(ctx context.Context, kinds, sessions []string) ([]StepResult, func(context.Context) []StepResult)
}

// Sessions are the saved sessions a case's run and snmp steps use (variables filled in), in order.
func Sessions(c Case, v map[string]string) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(list []Step)
	walk = func(list []Step) {
		for _, st := range list {
			if st.Loop != nil {
				walk(st.Loop.Steps)
				continue
			}
			p := ""
			if st.Run != nil {
				p = st.Run.Session
			} else if st.SNMP != nil {
				p = st.SNMP.Session
			}
			if p = Fill(p, v); p != "" && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	for _, list := range [][]Step{c.Setup, c.Steps, c.Cleanup} {
		walk(list)
	}
	return out
}

// RunParams runs a case once per Params entry (once when it has none); each result is named
// "case [name=value …]". vars win over the params, which win over the case's own vars.
func RunParams(ctx context.Context, c Case, d Driver, vars map[string]string) []Result {
	return RunParamsWith(ctx, c, d, vars, RunOptions{})
}

// RunParamsWith is RunParams with run options.
func RunParamsWith(ctx context.Context, c Case, d Driver, vars map[string]string, opt RunOptions) []Result {
	if len(c.Params) == 0 {
		return []Result{RunWith(ctx, c, d, vars, opt)}
	}
	var out []Result
	for _, p := range c.Params {
		v := map[string]string{}
		for k, x := range p {
			v[k] = x
		}
		for k, x := range vars {
			v[k] = x
		}
		r := RunWith(ctx, c, d, v, opt)
		r.Case = c.Name + " " + ParamName(p)
		out = append(out, r)
		if ctx.Err() != nil {
			break
		}
	}
	return out
}

// ParamName is "[k=v, …]" with the keys sorted.
func ParamName(p map[string]string) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		keys[i] = k + "=" + p[k]
	}
	return "[" + strings.Join(keys, ", ") + "]"
}

// RoleName makes a variable name for a session path: Lab/core-sw1 → core_sw1.
func RoleName(path string) string {
	base := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		base = path[i+1:]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "_"):
			b.WriteByte('_')
		}
	}
	n := strings.Trim(b.String(), "_")
	if n == "" || n[0] >= '0' && n[0] <= '9' {
		n = "dev_" + n
	}
	return n
}

func stepKind(s Step) string {
	if s.HTTP != nil {
		return "http"
	}
	if s.SNMP != nil {
		return "snmp"
	}
	if s.Traffic != nil {
		return "traffic"
	}
	if s.Upgrade != nil {
		return "upgrade"
	}
	if s.Manual != nil {
		return "manual"
	}
	return "run"
}

func stepName(s Step, i int) string {
	switch {
	case s.Name != "":
		return s.Name
	case s.Loop != nil:
		return "loop " + s.Loop.Var
	case s.Traffic != nil:
		return "traffic test " + s.Traffic.Test
	case s.Upgrade != nil:
		return "upgrade to " + s.Upgrade.Image
	case s.Manual != nil:
		return "manual: " + s.Manual.Instruction
	case actions(s) == 0 && s.Wait > 0:
		return fmt.Sprintf("wait %g s", s.Wait)
	case s.Run != nil:
		return s.Run.Command
	case s.SNMP != nil:
		return snmpText(*s.SNMP)
	case s.HTTP != nil:
		m := strings.ToUpper(strings.TrimSpace(s.HTTP.Method))
		if m == "" {
			m = "GET"
		}
		return m + " " + s.HTTP.URL
	}
	return fmt.Sprintf("step %d", i+1)
}

func runStep(ctx context.Context, st Step, i int, d Driver, v map[string]string) StepResult {
	sr := StepResult{Name: Fill(stepName(st, i), v), Kind: stepKind(st), Expected: Fill(st.Expected, v)}
	to := time.Duration(st.Timeout) * time.Second
	if to <= 0 {
		to = 60 * time.Second
		if st.Manual != nil {
			to = 24 * time.Hour
		}
		if st.Traffic != nil || st.Upgrade != nil {
			to = 30 * time.Minute
		}
	}
	sctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	start := time.Now()
	var out string
	var err error
	var rows []SNMPValue
	switch {
	case actions(st) > 1:
		err = errors.New("a step has one of run, http and snmp")
	case st.Run != nil:
		sr.Target, sr.Sent = Fill(st.Run.Session, v), Fill(st.Run.Command, v)
		out, err = d.Command(sctx, sr.Target, sr.Sent, st.Run.Prompt)
		if de, ok := d.(DeviceErrors); ok && err == nil && !st.Run.IgnoreErrors {
			if line := de.DeviceError(sr.Target, out); line != "" {
				err = fmt.Errorf("the device refused the command: %s", line)
			}
		}
		if err == nil && st.Run.Parse != "" {
			if p, ok := d.(Parser); ok {
				sr.Table, err = p.ParseOutput(sr.Target, sr.Sent, st.Run.Parse, out)
			} else {
				err = errors.New("parse: this runner cannot parse output")
			}
		}
	case st.HTTP != nil:
		h := fillHTTP(*st.HTTP, v)
		if h.Method == "" {
			h.Method = "GET"
		}
		sr.Target, sr.Sent = h.Session, h.Method+" "+h.URL
		var a HTTPAnswer
		a, err = d.HTTP(sctx, h)
		sr.Status, out = a.Status, a.Body
	case st.SNMP != nil:
		sn := fillSNMP(*st.SNMP, v)
		sr.Target, sr.Sent = sn.Session, snmpText(sn)
		rows, err = d.SNMP(sctx, sn)
		sr.Rows, out = rows, SNMPOutput(rows)
	case st.Manual != nil:
		sr.Target, sr.Sent = "person", Fill(st.Manual.Instruction, v)
		mk, ok := d.(ManualAsker)
		if !ok || !mk.CanAsk() {
			sr.Skipped, sr.Passed, sr.Error = true, true, "skipped: a manual step needs a person (run it from the Tests tab)"
			sr.Ms = time.Since(start).Milliseconds()
			return sr
		}
		passed, note, aerr := mk.AskManual(sctx, sr.Name, sr.Sent, Fill(st.Expected, v))
		out = note
		switch {
		case aerr != nil:
			err = aerr
		case !passed:
			err = errors.New("the person marked it failed" + map[bool]string{true: ": " + note, false: ""}[note != ""])
		}
	case st.Upgrade != nil:
		sr.Target, sr.Sent = Fill(st.Upgrade.Session, v), "upgrade to "+Fill(st.Upgrade.Image, v)
		if u, ok := d.(Upgrader); ok {
			out, err = u.Upgrade(sctx, sr.Target, Fill(st.Upgrade.Image, v))
		} else {
			err = errors.New("this runner cannot upgrade devices")
		}
	case st.Traffic != nil:
		sr.Target, sr.Sent = "lab", "traffic test "+Fill(st.Traffic.Test, v)
		if tr, ok := d.(TrafficRunner); ok {
			out, err = tr.Traffic(sctx, Fill(st.Traffic.Test, v))
		} else {
			err = errors.New("this runner has no lab traffic tests")
		}
	default:
		err = errors.New("the step has nothing to do (run, http or snmp)")
	}
	sr.Ms = time.Since(start).Milliseconds()
	if len(out) > maxOut {
		sr.Output = out[:maxOut] + "\n… (cut)"
	} else {
		sr.Output = out
	}
	if err != nil {
		if sctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("no answer in %s", to)
		}
		sr.Error = err.Error()
		return sr
	}
	sr.Passed = true
	sr.Unchecked = len(st.Expect) == 0 && st.Manual == nil
	for _, e := range st.Expect {
		var c CheckResult
		if e.Table != nil {
			c = checkTable(*e.Table, sr.Table, v)
		} else {
			c = check(e, out, sr.Status, sr.Ms, v, rows)
		}
		sr.Checks = append(sr.Checks, c)
		sr.Passed = sr.Passed && c.Passed
	}
	for _, s := range st.Save {
		val, ok := extract(s.Field, s.JSON, out)
		if s.OID != "" {
			var r SNMPValue
			r, ok = FindOID(rows, Fill(s.OID, v))
			val = r.Display
		}
		if s.Column != "" {
			ok = false
			if rs := TableRows(sr.Table, s.Where, v); len(rs) > 0 {
				val, ok = lookupCol(rs[0], s.Column)
			}
		}
		if ok && s.Var != "" {
			v[s.Var] = val
		}
	}
	return sr
}

var varRe = regexp.MustCompile(`\{\{\s*([\w.\-]+)\s*\}\}`)

// Fill replaces {{name}} with its value; unknown names stay as they are.
func Fill(s string, v map[string]string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	return varRe.ReplaceAllStringFunc(s, func(m string) string {
		if x, ok := v[varRe.FindStringSubmatch(m)[1]]; ok {
			return x
		}
		return m
	})
}

func fillHTTP(h HTTPStep, v map[string]string) HTTPStep {
	h.Session, h.Environment, h.Collection, h.URL, h.Body = Fill(h.Session, v), Fill(h.Environment, v), Fill(h.Collection, v), Fill(h.URL, v), Fill(h.Body, v)
	h.Method = strings.ToUpper(strings.TrimSpace(h.Method))
	if len(h.Headers) > 0 {
		m := make(map[string]string, len(h.Headers))
		for k, x := range h.Headers {
			m[k] = Fill(x, v)
		}
		h.Headers = m
	}
	return h
}

func check(e Expect, out string, status int, ms int64, v map[string]string, rows []SNMPValue) CheckResult {
	switch {
	case e.Contains != "":
		t := Fill(e.Contains, v)
		ok := HasText(out, t)
		return CheckResult{What: fmt.Sprintf("contains %q", t), Passed: ok, Got: map[bool]string{true: "", false: "not in the output"}[ok]}
	case e.NotContains != "":
		t := Fill(e.NotContains, v)
		ok := !HasText(out, t)
		return CheckResult{What: fmt.Sprintf("does not contain %q", t), Passed: ok, Got: map[bool]string{true: "", false: "it is in the output"}[ok]}
	case e.Matches != "":
		re, err := regexp.Compile(Fill(e.Matches, v))
		if err != nil {
			return CheckResult{What: "matches " + e.Matches, Got: "bad pattern: " + err.Error()}
		}
		return CheckResult{What: "matches " + re.String(), Passed: re.MatchString(out), Got: re.FindString(out)}
	case e.Status != 0:
		return CheckResult{What: fmt.Sprintf("status = %d", e.Status), Passed: status == e.Status, Got: strconv.Itoa(status)}
	case e.Schema != "":
		sch, name := e.Schema, "the schema"
		if !strings.HasPrefix(strings.TrimSpace(sch), "{") {
			name = sch
			b, err := os.ReadFile(filepath.Join(schemaRoot.Load().(string), filepath.Clean("/" + sch)))
			if err != nil {
				return CheckResult{What: "fits " + name, Got: err.Error()}
			}
			sch = string(b)
		}
		bad := apiclient.ValidateSchema(sch, out)
		return CheckResult{What: "fits " + name, Passed: bad == "", Got: bad}
	case e.OID != "":
		r, ok := FindOID(rows, Fill(e.OID, v))
		c := compare("oid "+e.OID, r.Display, ok, e, v)
		if !c.Passed && ok && (e.Equals != nil || e.NotEquals != nil || e.Greater != nil || e.Less != nil) {
			if c2 := compare("oid "+e.OID, r.Value, ok, e, v); c2.Passed {
				c2.Got = r.Display
				return c2
			}
		}
		return c
	case e.MaxMs != 0 && e.Field == "" && e.JSON == "":
		return CheckResult{What: fmt.Sprintf("took ≤ %d ms", e.MaxMs), Passed: ms <= e.MaxMs, Got: fmt.Sprintf("%d ms", ms)}
	case e.Field != "" || e.JSON != "" || false:
		subj := "json " + e.JSON
		if e.Field != "" {
			subj = "field " + e.Field
		}
		got, ok := extract(Fill(e.Field, v), Fill(e.JSON, v), out)
		return compare(subj, got, ok, e, v)
	}
	return CheckResult{What: "empty check", Got: "set contains, matches, field, json, status or maxMs"}
}

func compare(subj, got string, found bool, e Expect, v map[string]string) CheckResult {
	c := CheckResult{What: subj, Got: got, Passed: found}
	var parts []string
	if e.Exists != nil {
		c.Passed = found == *e.Exists
		parts = append(parts, fmt.Sprintf("exists = %v", *e.Exists))
	}
	if e.Equals != nil {
		w := Fill(*e.Equals, v)
		c.Passed = c.Passed && found && got == w
		parts = append(parts, fmt.Sprintf("= %q", w))
	}
	if e.NotEquals != nil {
		w := Fill(*e.NotEquals, v)
		c.Passed = c.Passed && got != w
		parts = append(parts, fmt.Sprintf("≠ %q", w))
	}
	if e.Greater != nil || e.Less != nil {
		n, err := strconv.ParseFloat(strings.TrimSpace(got), 64)
		if err != nil {
			c.Passed = false
		}
		if e.Greater != nil {
			c.Passed = c.Passed && n > *e.Greater
			parts = append(parts, "> "+strconv.FormatFloat(*e.Greater, 'g', -1, 64))
		}
		if e.Less != nil {
			c.Passed = c.Passed && n < *e.Less
			parts = append(parts, "< "+strconv.FormatFloat(*e.Less, 'g', -1, 64))
		}
	}
	if len(parts) > 0 {
		c.What += " " + strings.Join(parts, " and ")
	} else {
		c.What += " found"
	}
	return c
}

// extract finds a value: field is a regular expression whose first group (else the match) is the
// value; path is a JSON path into out.
func extract(field, path, out string) (string, bool) {
	if field != "" {
		re, err := regexp.Compile(field)
		if err != nil {
			return "", false
		}
		m := re.FindStringSubmatch(out)
		if m == nil {
			return "", false
		}
		if len(m) > 1 {
			return m[1], true
		}
		return m[0], true
	}
	if path == "" {
		return "", false
	}
	var doc any
	if json.Unmarshal([]byte(out), &doc) != nil {
		return "", false
	}
	x, ok := JSONPath(doc, path)
	if !ok {
		return "", false
	}
	switch t := x.(type) {
	case string:
		return t, true
	case nil:
		return "null", true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(t), true
	}
	b, _ := json.Marshal(x)
	return string(b), true
}

var pathPart = regexp.MustCompile(`([^.\[\]]+)|\[(\d+|-1)\]|\["([^"]*)"\]`)

// JSONPath walks a decoded JSON document: a.b[0].c, $.a, ["key with dots"], [-1] for the last item
// and length (the number of items or keys).
func JSONPath(doc any, path string) (any, bool) {
	path = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(path), "$"), ".")
	cur := doc
	for _, m := range pathPart.FindAllStringSubmatch(path, -1) {
		switch {
		case m[2] != "":
			a, ok := cur.([]any)
			if !ok {
				return nil, false
			}
			i, _ := strconv.Atoi(m[2])
			if i < 0 {
				i = len(a) + i
			}
			if i < 0 || i >= len(a) {
				return nil, false
			}
			cur = a[i]
		default:
			k := m[1]
			if m[3] != "" {
				k = m[3]
			}
			switch t := cur.(type) {
			case map[string]any:
				x, ok := t[k]
				if !ok {
					if k == "length" {
						return float64(len(t)), true
					}
					return nil, false
				}
				cur = x
			case []any:
				if k != "length" {
					return nil, false
				}
				return float64(len(t)), true
			default:
				return nil, false
			}
		}
	}
	return cur, true
}

// StepAnswer is what RunOne got: the generated tests check it themselves.
type StepAnswer struct {
	// Output: the command's output or the HTTP body.
	Output string `json:"output"`
	// Status: the HTTP status.
	Status int `json:"status,omitempty"`
	// Ms: how long it took.
	Ms int64 `json:"ms"`
	// Error: why it could not run.
	Error string `json:"error,omitempty"`
	// Rows: the values an SNMP step got.
	Rows []SNMPValue `json:"rows,omitempty"`
	// Table: the parsed output (run steps with parse).
	Table *textfsm.Table `json:"table,omitempty"`
}

// RunOne runs one step's action (variables already filled in) without its checks - for the
// generated tests, which send their steps to Termwright one by one.
func RunOne(ctx context.Context, st Step, d Driver) StepAnswer {
	one := Step{Run: st.Run, HTTP: st.HTTP, SNMP: st.SNMP, Traffic: st.Traffic, Upgrade: st.Upgrade, Timeout: st.Timeout}
	var sr StepResult
	if st.Retry != nil { // the generated tests' retry: Termwright repeats the step until its checks pass
		one.Expect, one.Retry = st.Expect, st.Retry
		sr = retryStep(ctx, one, 0, d, map[string]string{})
		if strings.HasPrefix(sr.Error, "the checks did not pass") {
			sr.Error = "" // the generated test checks again and says what was wrong
		}
	} else {
		sr = runStep(ctx, one, 0, d, map[string]string{})
	}
	return StepAnswer{Output: sr.Output, Status: sr.Status, Ms: sr.Ms, Error: sr.Error, Rows: sr.Rows, Table: sr.Table}
}

func actions(s Step) int {
	n := 0
	for _, b := range []bool{s.Run != nil, s.HTTP != nil, s.SNMP != nil, s.Traffic != nil, s.Upgrade != nil, s.Manual != nil} {
		if b {
			n++
		}
	}
	return n
}

func snmpText(s SNMPStep) string {
	op := strings.ToLower(s.Op)
	if op == "" {
		op = "get"
	}
	if op == "set" {
		var p []string
		for _, x := range s.Values {
			p = append(p, x.OID+" = "+x.Value)
		}
		return "set " + strings.Join(p, ", ")
	}
	return op + " " + strings.Join(s.OIDs, " ")
}

func fillSNMP(s SNMPStep, v map[string]string) SNMPStep {
	s.Session = Fill(s.Session, v)
	s.Op = strings.ToLower(strings.TrimSpace(s.Op))
	if s.Op == "" {
		s.Op = "get"
	}
	oids := make([]string, len(s.OIDs))
	for i, o := range s.OIDs {
		oids[i] = Fill(o, v)
	}
	s.OIDs = oids
	vals := make([]SNMPSetValue, len(s.Values))
	for i, x := range s.Values {
		vals[i] = SNMPSetValue{OID: Fill(x.OID, v), Type: x.Type, Value: Fill(x.Value, v)}
	}
	s.Values = vals
	return s
}

// SNMPOutput is an SNMP step's values as text, one "name = value" line each, for contains, matches
// and field checks.
func SNMPOutput(rows []SNMPValue) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.Name + " = " + r.Display + "\n")
	}
	return b.String()
}

// FindOID finds a value by name (ifOperStatus.5, IF-MIB::ifOperStatus.5, ifOperStatus = the first
// instance) or numeric OID.
func FindOID(rows []SNMPValue, oid string) (SNMPValue, bool) {
	oid = strings.TrimPrefix(strings.TrimSpace(oid), ".")
	for _, r := range rows {
		if r.OID == oid || r.Name == oid {
			return r, true
		}
	}
	for _, r := range rows {
		n := r.Name
		if _, after, ok := strings.Cut(n, "::"); ok && !strings.Contains(oid, "::") {
			n = after
		}
		if n == oid || strings.HasPrefix(n, oid+".") || strings.HasPrefix(r.OID, oid+".") {
			return r, true
		}
	}
	return SNMPValue{}, false
}

var spaceRun = regexp.MustCompile(`\s+`)

// HasText: out contains t, with spacing and line breaks not counted (text selected over several
// lines of a device's aligned output is still found: "Primary IP: 200.1.1.1/24").
func HasText(out, t string) bool {
	if strings.Contains(out, t) {
		return true
	}
	n := func(s string) string { return strings.TrimSpace(spaceRun.ReplaceAllString(s, " ")) }
	return strings.Contains(n(out), n(t))
}

// TrafficRunner runs saved lab traffic tests (traffic steps); it returns their results as JSON.
type TrafficRunner interface {
	Traffic(ctx context.Context, test string) (string, error)
}

// Watcher watches syslog and SNMP notifications while a case runs (Case.Watch).
type Watcher interface {
	// WatchMark notes where the messages are now (an error: the syslog server or trap receiver is off).
	WatchMark(w *Watch) (any, error)
	// WatchHits returns the messages since the mark that match (one line each).
	WatchHits(mark any, w Watch, sessions []string) ([]string, error)
}

// Snapshotter reads a device's configuration (Case.ConfigDiff).
type Snapshotter interface {
	ConfigSnapshot(ctx context.Context, session string) (string, error)
}

// ConfigDiff lists the lines removed (-) and added (+) between two configurations; lines matching
// ignore (and blank lines) do not count. "" = the same.
func ConfigDiff(old, now string, ignore []*regexp.Regexp) string {
	norm := func(s string) []string {
		var out []string
		for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
			l = strings.TrimRight(l, " \t")
			if strings.TrimSpace(l) == "" {
				continue
			}
			skip := false
			for _, re := range ignore {
				skip = skip || re.MatchString(l)
			}
			if !skip {
				out = append(out, l)
			}
		}
		return out
	}
	a, b := norm(old), norm(now)
	count := map[string]int{}
	for _, l := range b {
		count[l]++
	}
	var out []string
	for _, l := range a {
		if count[l] > 0 {
			count[l]--
		} else {
			out = append(out, "- "+l)
		}
	}
	count = map[string]int{}
	for _, l := range a {
		count[l]++
	}
	for _, l := range b {
		if count[l] > 0 {
			count[l]--
		} else {
			out = append(out, "+ "+l)
		}
	}
	return strings.Join(out, "\n")
}

// Upgrader installs software images (upgrade steps); it returns the version the device runs then.
type Upgrader interface {
	Upgrade(ctx context.Context, session, image string) (string, error)
}

// Versioner tells which software the devices run (recorded with each result and in the history).
type Versioner interface {
	Versions(ctx context.Context, sessions []string) map[string]string
}

// ManualAsker asks a person for a manual step's result.
type ManualAsker interface {
	CanAsk() bool
	AskManual(ctx context.Context, name, instruction, expected string) (passed bool, note string, err error)
}
