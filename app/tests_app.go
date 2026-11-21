package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/apiclient"
	"github.com/termwright/termwright/internal/term"
	"github.com/termwright/termwright/internal/testrec"
)

// Tests: YAML test cases in a tests folder (Global Options → tests folder; default "tests" in the
// data folder), run against saved sessions and API sessions, with JUnit XML and HTML reports.

// TestsFolder returns the tests folder (made with cases/, lib/, testbeds/ and reports/ when missing).
func (a *App) TestsFolder() (string, error) {
	f, err := a.testsFolder()
	return f.Root, err
}

func (a *App) testsFolder() (testrec.Folder, error) {
	if err := a.ready(); err != nil {
		return testrec.Folder{}, err
	}
	d := a.cfg.Get().TestsDir
	if a.testsDir != "" {
		d = a.testsDir // termwright tests --folder
	}
	if d == "" {
		d = filepath.Join(a.cfg.DataDir(), "tests")
	}
	f := testrec.Folder{Root: d}
	return f, f.Init()
}

// TestCases lists the tests in the tests folder.
func (a *App) TestCases() ([]testrec.CaseInfo, error) {
	f, err := a.testsFolder()
	if err != nil {
		return nil, err
	}
	return f.List(), nil
}

// TestCase reads one test (file relative to the tests folder).
func (a *App) TestCase(file string) (testrec.Case, error) {
	f, err := a.testsFolder()
	if err != nil {
		return testrec.Case{}, err
	}
	return f.Load(file)
}

// TestSave writes a test; it returns its file relative to the tests folder. A test holding a
// password, key or community is refused.
func (a *App) TestSave(file string, c testrec.Case) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	b, err := testrec.Marshal(c)
	if err != nil {
		return "", err
	}
	if _, n := aiRedact(string(b), false); n > 0 {
		return "", errors.New("the test seems to hold a password, key or community: name a saved session instead - its login is in the credential store")
	}
	rel, err := f.Save(file, c)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteGo(rel); err != nil {
		return rel, fmt.Errorf("saved %s, but its Go test was not written: %w", rel, err)
	}
	return rel, nil
}

// TestGenerateGo writes a test's Go file (and go.mod, lib/) again; it returns the Go file.
func (a *App) TestGenerateGo(file string) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	return f.WriteGo(file)
}

// TestRunRequest runs tests.
type TestRunRequest struct {
	// Files: tests relative to the tests folder (none: all of them).
	Files []string `json:"files,omitempty"`
	// Tags: only tests with one of these tags ("!tag" leaves its tests out).
	Tags []string `json:"tags,omitempty"`
	// Testbeds: run on these testbeds at the same time (RunTestsParallel); ForceLock takes over a
	// testbed someone else has locked.
	Testbeds  []string `json:"testbeds,omitempty"`
	ForceLock bool     `json:"forceLock,omitempty"`
	scheduled bool     // a schedule's run: afterRun is called by the scheduler
	// Vars: values for {{variables}} that win over the tests' own.
	Vars map[string]string `json:"vars,omitempty"`
	// Testbed: a file in testbeds/ whose values are used as {{variables}} (Vars win).
	Testbed string `json:"testbed,omitempty"`
	// OnCleanupFail: when a test's cleanup fails - "stop" (default: the remaining tests are skipped),
	// "restore" (the configuration saved before the run is put back, the device may restart) or
	// "factory" (the device type's factory-default commands; never on production sessions).
	OnCleanupFail string `json:"onCleanupFail,omitempty"`
	// Cleanup: "" (each test's cleanup steps), "off" (no cleanup: tests build on each other) or
	// "auto" (the cleanup steps; without them the undo of the configuration commands that ran).
	Cleanup string `json:"cleanup,omitempty"`
	// Confirmed: the person agreed to run steps on production sessions.
	Confirmed bool `json:"confirmed,omitempty"`
}

// errTestsProduction is returned when a run touches production sessions and was not confirmed.
var errTestsProduction = errors.New("these tests use production sessions")

var (
	testsRun  sync.Mutex          // guards testsBusy
	testsBusy = map[string]bool{} // testbeds ("" = the tests' own sessions) with a run going
	testsMu   sync.Mutex          // guards testsLast
	testsLast *testrec.RunReport
)

// RunTests runs tests one after another; "tests:progress" is emitted after each test. A run that
// uses production sessions needs Confirmed (the error lists them).
func (a *App) RunTests(r TestRunRequest) (testrec.RunReport, error) {
	return a.runTests(context.Background(), r, nil)
}

// RunTestsParallel runs the same tests on several testbeds at once (each locked for its run); it
// returns one report per testbed, in order.
func (a *App) RunTestsParallel(r TestRunRequest) ([]testrec.RunReport, error) {
	return a.runParallel(context.Background(), r, nil)
}

func (a *App) runParallel(ctx context.Context, r TestRunRequest, ask netAsker) ([]testrec.RunReport, error) {
	tbs := r.Testbeds
	if len(tbs) == 0 {
		tbs = []string{r.Testbed}
	}
	out := make([]testrec.RunReport, len(tbs))
	errs := make([]error, len(tbs))
	var wg sync.WaitGroup
	for i, tb := range tbs {
		rr := r
		rr.Testbed, rr.Testbeds = tb, nil
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i], errs[i] = a.runTests(ctx, rr, ask)
		}()
	}
	wg.Wait()
	var msgs []string
	for i, e := range errs {
		if e != nil {
			msgs = append(msgs, orDash(tbs[i])+": "+e.Error())
		}
	}
	if len(msgs) > 0 {
		return out, errors.New(strings.Join(msgs, "; "))
	}
	return out, nil
}

// TestReleaseLock frees a testbed locked by a run that ended without releasing it (or someone
// else's, when they are done).
func (a *App) TestReleaseLock(testbed string) error {
	f, err := a.testsFolder()
	if err != nil {
		return err
	}
	return f.ReleaseLock(testbed)
}

func (a *App) runTests(ctx context.Context, r TestRunRequest, ask netAsker) (testrec.RunReport, error) {
	f, err := a.testsFolder()
	if err != nil {
		return testrec.RunReport{}, err
	}
	if r.Testbed != "" {
		tb, err := f.Testbed(r.Testbed)
		if err != nil {
			return testrec.RunReport{}, err
		}
		for k, v := range r.Vars {
			tb[k] = v
		}
		r.Vars = tb
	}
	if prod := a.testsProduction(f, r.Files, r.Vars); len(prod) > 0 && !r.Confirmed {
		if ask == nil {
			return testrec.RunReport{}, fmt.Errorf("%w: %s", errTestsProduction, strings.Join(prod, ", "))
		}
		if err := ask("Run tests on production sessions", "The tests send commands or requests to production sessions: "+strings.Join(prod, ", "), true); err != nil {
			return testrec.RunReport{}, err
		}
	} else if ask != nil {
		if err := ask("Run tests", "Run "+testsWhich(r.Files)+" (their commands are sent to the devices)", false); err != nil {
			return testrec.RunReport{}, err
		}
	}
	testsRun.Lock()
	if testsBusy[r.Testbed] {
		testsRun.Unlock()
		return testrec.RunReport{}, errors.New("tests are already running on " + orDash(r.Testbed))
	}
	testsBusy[r.Testbed] = true
	testsRun.Unlock()
	defer func() { testsRun.Lock(); delete(testsBusy, r.Testbed); testsRun.Unlock() }()
	if r.Testbed != "" {
		if err := f.TakeLock(r.Testbed, r.ForceLock); err != nil {
			return testrec.RunReport{}, err
		}
		defer f.ReleaseLock(r.Testbed)
	}
	rep, err := f.RunFilesWith(ctx, r.Files, newTestDriver(a), r.Vars, testrec.RunOptions{OnCleanupFail: r.OnCleanupFail, Cleanup: r.Cleanup, Testbed: r.Testbed, Tags: r.Tags}, func(res testrec.Result) {
		emitter{a}.Emit("tests:progress", res)
	})
	if err == nil || len(rep.Results) > 0 {
		testsMu.Lock()
		testsLast = &rep
		testsMu.Unlock()
		if !r.scheduled {
			go a.afterRun(rep, false)
		}
	}
	return rep, err
}

func testsWhich(files []string) string {
	if len(files) == 0 {
		return "all tests"
	}
	return strings.Join(files, ", ")
}

// testsProduction lists the production sessions the tests use.
func (a *App) testsProduction(f testrec.Folder, files []string, vars map[string]string) []string {
	if len(files) == 0 {
		for _, c := range f.List() {
			files = append(files, c.File)
		}
	}
	var out []string
	for _, file := range files {
		c, err := f.Load(file)
		if err != nil {
			continue
		}
		v := map[string]string{}
		for k, x := range c.Vars {
			v[k] = x
		}
		for k, x := range vars {
			v[k] = x
		}
		for _, st := range c.Steps {
			p := ""
			if st.Run != nil {
				p = testrec.Fill(st.Run.Session, v)
			} else if st.HTTP != nil {
				p = testrec.Fill(st.HTTP.Session, v)
			} else if st.SNMP != nil {
				p = testrec.Fill(st.SNMP.Session, v)
			}
			if p == "" || slices.Contains(out, p) {
				continue
			}
			if s, err := a.store.Get(p); err == nil && strings.EqualFold(s.Environment, "production") {
				out = append(out, p)
			}
		}
	}
	return out
}

// TestLastRun returns the last run's report (nil before the first run).
func (a *App) TestLastRun() *testrec.RunReport {
	testsMu.Lock()
	defer testsMu.Unlock()
	return testsLast
}

// testDriver runs test steps with Termwright: commands log in to saved sessions in the background
// (as Run and Collect does) and keep that login for the rest of the case; requests go through the
// API client. Without own (the generated tests' single steps) logins come from the app's pool,
// which logs out after a while unused.
type testDriver struct {
	a   *App
	own map[string]*term.CaptureConn
	// baseline puts back the configuration saved before the run (on a failed cleanup: restore)
	baseline         func(context.Context) []testrec.StepResult
	baselineSessions []string
}

func newTestDriver(a *App) *testDriver { return &testDriver{a: a, own: map[string]*term.CaptureConn{}} }

// EndCase logs out of the case's sessions.
func (d *testDriver) EndCase() {
	for k, c := range d.own {
		c.Close()
		delete(d.own, k)
	}
}

func (d *testDriver) Command(ctx context.Context, session, cmd, prompt string) (string, error) {
	s, err := d.a.store.Get(session)
	if err != nil {
		return "", fmt.Errorf("no saved session %q", session)
	}
	key := s.Path + "\x00" + prompt
	for try := 0; ; try++ {
		var c *term.CaptureConn
		release := func(bool) {}
		if d.own != nil {
			if c = d.own[key]; c == nil || !c.Alive() {
				if c != nil {
					c.Close()
				}
				if c, err = d.a.terms.OpenCapture(ctx, s, prompt); err != nil {
					return "", err
				}
				d.own[key] = c
			}
			release = func(bad bool) {
				if bad {
					c.Close()
					delete(d.own, key)
				}
			}
		} else if c, release, err = testPool.get(ctx, d.a, s, prompt); err != nil {
			return "", err
		}
		out, err := c.Run(ctx, cmd)
		bad := err != nil && ctx.Err() == nil
		release(bad)
		if bad && try == 0 && !c.Alive() {
			continue // the connection went away (the device restarted, a timeout): log in again once
		}
		return out, err
	}
}

func (d *testDriver) HTTP(ctx context.Context, h testrec.HTTPStep) (testrec.HTTPAnswer, error) {
	req := apiclient.Request{Method: h.Method, URL: h.URL}
	for k, v := range h.Headers {
		req.Headers = append(req.Headers, apiclient.KV{Key: k, Value: v})
	}
	slices.SortFunc(req.Headers, func(x, y apiclient.KV) int { return strings.Compare(x.Key, y.Key) })
	if b := strings.TrimSpace(h.Body); b != "" {
		req.Body = apiclient.Body{Mode: "text", Raw: h.Body}
		if b[0] == '{' || b[0] == '[' {
			req.Body.Mode = "json"
		}
	}
	// a production run was confirmed before it started
	res, err := d.a.httpSend(ctx, HTTPSendRequest{Session: h.Session, Environment: h.Environment, Collection: h.Collection, Request: req, Confirmed: true})
	if err != nil {
		return testrec.HTTPAnswer{}, err
	}
	if res.Status == 0 && res.Error != "" {
		return testrec.HTTPAnswer{}, errors.New(res.Error)
	}
	return testrec.HTTPAnswer{Status: res.Status, Body: res.Body}, nil
}

func (d *testDriver) SNMP(ctx context.Context, sn testrec.SNMPStep) ([]testrec.SNMPValue, error) {
	r := SNMPRequest{Session: sn.Session, Op: sn.Op, OIDs: sn.OIDs, Confirmed: true} // production was asked before the run
	for _, v := range sn.Values {
		r.Values = append(r.Values, SNMPValue{OID: v.OID, Type: v.Type, Value: v.Value})
	}
	res, err := d.a.snmpRun(ctx, r)
	if err != nil {
		return nil, err
	}
	out := make([]testrec.SNMPValue, 0, len(res.Rows))
	for _, x := range res.Rows {
		out = append(out, testrec.SNMPValue{OID: x.OID, Name: x.Name, Value: x.Value, Display: x.Display})
	}
	return out, nil
}

// testsTool is the "tests" network tool (AI assistant, MCP, POST /tools/tests).
func (a *App) testsTool(ctx context.Context, args map[string]any, ask netAsker) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	switch act := strings.ToLower(argStr(args, "action")); act {
	case "list", "":
		var b strings.Builder
		for _, c := range f.List() {
			fmt.Fprintf(&b, "%s  %q  %d steps", c.File, c.Name, c.Steps)
			if len(c.Tags) > 0 {
				fmt.Fprintf(&b, "  [%s]", strings.Join(c.Tags, ", "))
			}
			if c.Error != "" {
				b.WriteString("  ERROR: " + c.Error)
			}
			b.WriteByte('\n')
		}
		if b.Len() == 0 {
			return "No tests yet in " + filepath.Join(f.Root, "cases") + ". Write one with save.", nil
		}
		return b.String(), nil
	case "show":
		c, err := f.Load(argStr(args, "file"))
		if err != nil {
			return "", err
		}
		y, err := testrec.Marshal(c)
		return string(y), err
	case "save":
		c, err := testrec.Parse([]byte(argStr(args, "yaml")))
		if err != nil {
			return "", fmt.Errorf("the YAML is not a test: %w", err)
		}
		file := argStr(args, "file")
		if file == "" {
			return "", errors.New("give file, e.g. cases/smoke.yaml")
		}
		if err := ask("Save the test "+file, fmt.Sprintf("%s: %d steps", file, len(c.Steps)), false); err != nil {
			return "", err
		}
		rel, err := a.TestSave(file, c)
		if err != nil {
			return "", err
		}
		return "Saved " + rel + ".", nil
	case "run":
		files := argStrs(args, "files")
		if x := argStr(args, "file"); x != "" {
			files = append(files, x)
		}
		vars := map[string]string{}
		if m, ok := args["vars"].(map[string]any); ok {
			for k, v := range m {
				vars[k] = fmt.Sprint(v)
			}
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		req := TestRunRequest{Files: files, Vars: vars, Testbed: argStr(args, "testbed"), OnCleanupFail: argStr(args, "on_cleanup_fail"), Cleanup: argStr(args, "cleanup"), Tags: argStrs(args, "tags"), ForceLock: args["force_lock"] == true}
		if tbs := argStrs(args, "testbeds"); len(tbs) > 1 {
			req.Testbed, req.Testbeds = "", tbs
			reps, err := a.runParallel(ctx, req, ask)
			var b strings.Builder
			for _, r := range reps {
				if r.Dir != "" {
					fmt.Fprintf(&b, "Testbed %s:\n%s\n", r.Testbed, testsReportText(f, r))
				}
			}
			if err != nil {
				fmt.Fprintf(&b, "Errors: %v\n", err)
			}
			return b.String(), nil
		}
		rep, err := a.runTests(ctx, req, ask)
		if err != nil && len(rep.Results) == 0 {
			return "", err
		}
		return testsReportText(f, rep), nil
	case "send_qtest":
		d := argStr(args, "dir")
		if d == "" {
			if l := a.TestLastRun(); l != nil {
				d = l.Dir
			}
		}
		return a.TestSendQTest(d, argStrs(args, "files"))
	case "defect":
		d := argStr(args, "dir")
		if d == "" {
			if l := a.TestLastRun(); l != nil {
				d = l.Dir
			}
		}
		if err := ask("Open a Jira defect", "Open a Jira defect for "+argStr(args, "file"), false); err != nil {
			return "", err
		}
		kl, err := a.TestCreateDefect(d, argStr(args, "file"))
		if err != nil {
			return "", err
		}
		return "Opened " + strings.Join(kl, " "), nil
	case "history":
		var b strings.Builder
		for _, h := range func() []testrec.TestHistory {
			f, _ := a.testsFolder()
			return f.History(argStr(args, "file"), argInt(args, "limit", 30, 500))
		}() {
			fmt.Fprintf(&b, "%s: %d passed, %d failed, %d skipped, %d flips, avg %d ms\n", h.File, h.Passed, h.Failed, h.Skipped, h.Flips, h.AvgMs)
			for _, v := range h.ByVersion {
				fmt.Fprintf(&b, "  %s: %d passed, %d failed\n", v.Version, v.Passed, v.Failed)
			}
			for i, r := range h.Runs {
				if i == 5 {
					break
				}
				st := map[bool]string{true: "passed", false: "FAILED"}[r.Passed]
				if r.Skipped {
					st = "skipped"
				}
				fmt.Fprintf(&b, "  %s %s %s\n", r.At.Local().Format("2006-01-02 15:04"), st, r.Problem)
			}
		}
		if b.Len() == 0 {
			return "No history yet: run tests first.", nil
		}
		return b.String(), nil
	case "generate":
		files := argStrs(args, "files")
		if x := argStr(args, "file"); x != "" {
			files = append(files, x)
		}
		if len(files) == 0 {
			for _, c := range f.List() {
				files = append(files, c.File)
			}
		}
		var b strings.Builder
		for _, file := range files {
			g, err := f.WriteGo(file)
			if err != nil {
				fmt.Fprintf(&b, "%s: %v\n", file, err)
				continue
			}
			fmt.Fprintf(&b, "%s → %s\n", file, g)
		}
		fmt.Fprintf(&b, "Each test is written as Go (_test.go), Python (test_*.py) and TCL (.test.tcl). Run them in %s with: go test ./cases/...  (JUnit: go test -json ./cases/... | go run ./lib/junit > junit.xml)  or  python lib/python/run_tests.py --junit junit.xml  (or pytest cases)  or  tclsh lib/tcl/run_tests.tcl -junit junit.xml", f.Root)
		return b.String(), nil
	case "step":
		// one step for the generated Go tests: they check the answer themselves
		var st testrec.Step
		raw, _ := json.Marshal(args["step"])
		n := 0
		target := ""
		if json.Unmarshal(raw, &st) == nil {
			if st.Run != nil {
				n, target = n+1, st.Run.Session
			}
			if st.HTTP != nil {
				n, target = n+1, st.HTTP.Session
			}
			if st.SNMP != nil {
				n, target = n+1, st.SNMP.Session
			}
		}
		if n != 1 {
			return "", errors.New("step needs one of run {session, command}, http {url} and snmp {session, op, oids}")
		}
		if s, err := a.store.Get(target); err == nil && strings.EqualFold(s.Environment, "production") {
			if err := ask("Test step on a production session", target+": "+testStepText(st), true); err != nil {
				return "", err
			}
		}
		ans := testrec.RunOne(ctx, st, &testDriver{a: a})
		j, _ := json.Marshal(ans)
		return string(j), nil
	case "new_testbed":
		if err := ask("Write a testbed", "testbeds/"+argStr(args, "name")+".yaml from the sessions of "+argStr(args, "folder"), false); err != nil {
			return "", err
		}
		rel, err := a.TestsNewTestbed(argStr(args, "name"), argStr(args, "folder"))
		if err != nil {
			return "", err
		}
		txt, _ := f.ReadText(rel)
		return "Wrote " + rel + ":\n" + txt, nil
	case "schedules":
		var b strings.Builder
		for _, s := range a.TestSchedules() {
			fmt.Fprintf(&b, "%s  %q  %s  tests: %s  testbed: %s", s.ID, s.Name, s.When, testsWhich(s.Files), orDash(s.Testbed))
			if s.Off {
				b.WriteString("  (off)")
			}
			if !s.State.Last.IsZero() {
				fmt.Fprintf(&b, "  last %s: %d passed, %d failed", s.State.Last.Format("2006-01-02 15:04"), s.State.Passed, s.State.Failed)
			}
			if s.Next != "" {
				b.WriteString("  next " + s.Next)
			}
			b.WriteByte('\n')
		}
		if b.Len() == 0 {
			return "No schedules.", nil
		}
		return b.String(), nil
	case "schedule":
		s := testrec.Schedule{ID: argStr(args, "id"), Name: argStr(args, "name"), Files: argStrs(args, "files"), Testbed: argStr(args, "testbed"),
			Repeat: argStr(args, "repeat"), At: argStr(args, "at"), Day: argInt(args, "day", 0, 6), Hours: argInt(args, "hours", 0, 168)}
		if s.Production = argStr(args, "production") == "true"; s.Production {
			if err := ask("Schedule tests on production sessions", "The schedule "+s.Name+" may send commands to production sessions without asking.", true); err != nil {
				return "", err
			}
		} else if err := ask("Schedule tests", s.Name+": "+s.Text(), false); err != nil {
			return "", err
		}
		saved, err := a.TestSaveSchedule(s)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Saved schedule %s (%s): %s.", saved.ID, saved.Name, saved.Text()), nil
	case "unschedule":
		if err := ask("Remove a test schedule", argStr(args, "id"), false); err != nil {
			return "", err
		}
		if err := a.TestRemoveSchedule(argStr(args, "id")); err != nil {
			return "", err
		}
		return "Removed.", nil
	case "save_state", "restore_state":
		// used by the generated tests (restore: config or lab)
		return a.testsStateTool(ctx, act, args)
	case "results":
		r := a.TestLastRun()
		if r == nil {
			return "No tests have run yet.", nil
		}
		return testsReportText(f, *r), nil
	default:
		return "", fmt.Errorf("unknown action %q (list, show, run, save, generate, step, new_testbed, schedules, schedule, unschedule, save_state, restore_state, results)", act)
	}
}

func testStepText(st testrec.Step) string {
	if st.Run != nil {
		return st.Run.Command
	}
	if st.SNMP != nil {
		return "SNMP " + st.SNMP.Op + " " + strings.Join(st.SNMP.OIDs, " ")
	}
	return strings.TrimSpace(st.HTTP.Method + " " + st.HTTP.URL)
}

func testsReportText(f testrec.Folder, rep testrec.RunReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d passed, %d failed", rep.Passed, rep.Failed)
	if rep.Skipped > 0 {
		fmt.Fprintf(&b, ", %d skipped", rep.Skipped)
	}
	fmt.Fprintf(&b, " in %.1f s. Report: %s\n", float64(rep.Ms)/1000, filepath.Join(f.Root, rep.Dir, "report.html"))
	for _, st := range rep.Recovery {
		fmt.Fprintf(&b, "  %s recovery: %s %s", map[bool]string{true: "✓", false: "✗"}[st.Passed], st.Name, st.Target)
		if st.Error != "" {
			b.WriteString(": " + st.Error)
		}
		b.WriteByte('\n')
	}
	for _, r := range rep.Results {
		word := map[bool]string{true: "PASS", false: "FAIL"}[r.Passed]
		if r.Skipped {
			word = "SKIP"
		} else if r.CleanupFailed {
			word = "FAIL (cleanup)"
		}
		fmt.Fprintf(&b, "\n%s %s (%s)\n", word, r.Case, r.File)
		if r.Error != "" {
			b.WriteString("  " + r.Error + "\n")
		}
		if r.CleanupFailed {
			b.WriteString("  The cleanup failed: the device may not be clean for the next test.\n")
		}
		for _, st := range r.Steps {
			m := "✓"
			switch {
			case st.Skipped:
				m = "-"
			case !st.Passed:
				m = "✗"
			}
			fmt.Fprintf(&b, "  %s %s%s", m, testrec.PhaseLabel(st.Phase), st.Name)
			if st.Error != "" {
				b.WriteString(": " + st.Error)
			}
			b.WriteByte('\n')
			for _, c := range st.Checks {
				if !c.Passed {
					fmt.Fprintf(&b, "      expected %s, got %q\n", c.What, c.Got)
				}
			}
			if !st.Passed && !st.Skipped && st.Output != "" {
				o := st.Output
				if len(o) > 1500 {
					o = o[:1500] + "…"
				}
				b.WriteString("      output: " + strings.ReplaceAll(strings.TrimSpace(o), "\n", "\n      ") + "\n")
			}
		}
	}
	return b.String()
}
