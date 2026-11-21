package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/testrec"
)

// A test's restore: [config, lab] - the devices' state is saved before the test and put back after
// its cleanup. config uses the device type's save and restore commands ({file} a name for the saved
// copy); lab takes a checkpoint of an emulated lab switch and restores it.

func (d *testDriver) SaveState(ctx context.Context, kinds, sessions []string) ([]testrec.StepResult, func(context.Context) []testrec.StepResult) {
	return d.a.testsSaveState(ctx, kinds, sessions)
}

func (a *App) testsSaveState(ctx context.Context, kinds, sessions []string) ([]testrec.StepResult, func(context.Context) []testrec.StepResult) {
	var out []testrec.StepResult
	var undo []func(context.Context) testrec.StepResult
	res := func(name, target string, start time.Time, err error) testrec.StepResult {
		r := testrec.StepResult{Name: name, Kind: "restore", Target: target, Ms: time.Since(start).Milliseconds(), Passed: err == nil}
		if err != nil {
			r.Error = err.Error()
		}
		return r
	}
	for _, kind0 := range kinds {
		for _, path := range sessions {
			start := time.Now()
			kind := kind0
			if kind == "auto" { // the run's saved state: a lab switch's checkpoint, else its configuration
				kind = "config"
				if emuVMOfSession(path) != "" {
					kind = "lab"
				}
			}
			switch kind {
			case "lab":
				id := emuVMOfSession(path)
				if id == "" {
					out = append(out, res("lab checkpoint", path, start, errors.New("not a session of an emulated lab switch")))
					continue
				}
				ck, err := a.emuCheckpoint(id, "before a test", "")
				out = append(out, res("lab checkpoint", path, start, err))
				if err == nil {
					tag := ck.Tag
					undo = append(undo, func(context.Context) testrec.StepResult {
						t0 := time.Now()
						err := a.emuRestore(id, tag)
						if err == nil {
							_, _ = a.EmuCheckpointRemove(id, tag)
						}
						return res("lab checkpoint back", path, t0, err)
					})
				}
			case "config":
				s, err := a.store.Get(path)
				if err != nil {
					out = append(out, res("save configuration", path, start, fmt.Errorf("no saved session %q", path)))
					continue
				}
				dt, ok := a.deviceTypeNamed(s.DeviceType)
				saveCmds, restoreCmds := dt.TestState()
				if !ok || len(saveCmds) == 0 || len(restoreCmds) == 0 {
					out = append(out, res("save configuration", path, start, fmt.Errorf("the device type %q has no save and restore commands for tests (Global Options → Device Types)", orDash(s.DeviceType))))
					continue
				}
				file := "tw-test-" + randHex(3)
				fill := func(l []string) []string {
					r := make([]string, len(l))
					for i, c := range l {
						r[i] = strings.ReplaceAll(c, "{file}", file)
					}
					return r
				}
				_, err = a.terms.CaptureYes(ctx, s, fill(saveCmds), "")
				out = append(out, res("save configuration ("+file+")", path, start, err))
				if err == nil {
					sess, cmds := s, fill(restoreCmds)
					undo = append(undo, func(ctx context.Context) testrec.StepResult {
						t0 := time.Now()
						_, err := a.terms.CaptureYes(ctx, sess, cmds, "")
						return res("restore configuration ("+file+")", sess.Path, t0, err)
					})
				}
			default:
				out = append(out, res("restore "+kind, path, start, fmt.Errorf("unknown restore %q (config or lab)", kind)))
			}
		}
	}
	return out, func(ctx context.Context) []testrec.StepResult {
		var r []testrec.StepResult
		for i := len(undo) - 1; i >= 0; i-- {
			r = append(r, undo[i](ctx))
		}
		return r
	}
}

// deviceTypeNamed finds a device type by name (built-in defaults apply through its methods).
func (a *App) deviceTypeNamed(name string) (session.DeviceType, bool) {
	for _, d := range a.cfg.Get().DeviceTypes {
		if strings.EqualFold(d.Name, name) {
			return d, true
		}
	}
	return session.DeviceType{Name: name}, false
}

// Saved states of the generated tests (save_state → token → restore_state).
var (
	stateMu  sync.Mutex
	stateFns = map[string]func(context.Context) []testrec.StepResult{}
)

func (a *App) testsStateTool(ctx context.Context, act string, args map[string]any) (string, error) {
	type out struct {
		Token   string               `json:"token,omitempty"`
		Results []testrec.StepResult `json:"results"`
	}
	if act == "save_state" {
		res, back := a.testsSaveState(ctx, argStrs(args, "kinds"), argStrs(args, "sessions"))
		tok := randHex(6)
		stateMu.Lock()
		stateFns[tok] = back
		stateMu.Unlock()
		j, _ := json.Marshal(out{Token: tok, Results: res})
		return string(j), nil
	}
	tok := argStr(args, "token")
	stateMu.Lock()
	back := stateFns[tok]
	delete(stateFns, tok)
	stateMu.Unlock()
	if back == nil {
		return "", fmt.Errorf("no saved state %q", tok)
	}
	j, _ := json.Marshal(out{Results: back(ctx)})
	return string(j), nil
}

// DeviceError: the line in which the session's device refused a command (its device type's error
// patterns), "" when it did not.
func (d *testDriver) DeviceError(path, out string) string {
	s, err := d.a.store.Get(path)
	if err != nil || s.DeviceType == "" {
		return ""
	}
	dt, _ := d.a.deviceTypeNamed(s.DeviceType)
	for _, re := range dt.Errors() {
		if loc := re.FindStringIndex(out); loc != nil {
			start := strings.LastIndex(out[:loc[0]], "\n") + 1
			end := strings.Index(out[loc[0]:], "\n")
			if end < 0 {
				end = len(out) - loc[0]
			}
			return strings.TrimSpace(out[start : loc[0]+end])
		}
	}
	return ""
}

// Baseline saves the devices' configuration before a run whose failed cleanups are answered by
// restoring it (lab switches: a checkpoint).
func (d *testDriver) Baseline(ctx context.Context, sessions []string) []testrec.StepResult {
	res, back := d.a.testsSaveState(ctx, []string{"auto"}, sessions)
	d.baseline, d.baselineSessions = back, sessions
	return res
}

// Recover brings the devices back after a failed cleanup: mode restore puts back what Baseline saved,
// mode factory runs the device type's factory-default commands; then it waits until each device logs in
// again (it may restart).
func (d *testDriver) Recover(ctx context.Context, mode string, sessions []string) []testrec.StepResult {
	d.EndCase() // the case's logins are gone after a restart
	var out []testrec.StepResult
	if mode == "restore" {
		if d.baseline == nil {
			return []testrec.StepResult{{Name: "restore the saved configuration", Error: "nothing was saved before the run"}}
		}
		out = d.baselineFor(ctx, sessions)
	} else {
		for _, p := range sessions {
			start := time.Now()
			r := testrec.StepResult{Name: "factory default", Kind: "recovery", Target: p}
			s, err := d.a.store.Get(p)
			if err == nil && d.a.isProtected(s) {
				err = errors.New("not on a production session")
			}
			var cmds []string
			if err == nil {
				dt, _ := d.a.deviceTypeNamed(s.DeviceType)
				if cmds = dt.Factory(); len(cmds) == 0 {
					err = fmt.Errorf("the device type %q has no factory-default commands (Global Options → Device Types)", orDash(s.DeviceType))
				}
			}
			if err == nil {
				_, err = d.a.terms.CaptureYes(ctx, s, cmds, "")
				if err != nil && strings.Contains(err.Error(), "closed") {
					err = nil // it restarted while the command ran
				}
			}
			r.Ms, r.Passed = time.Since(start).Milliseconds(), err == nil
			if err != nil {
				r.Error = err.Error()
			}
			out = append(out, r)
		}
	}
	// back up: each device must log in again (after a restart that can take minutes)
	for _, p := range sessions {
		start := time.Now()
		r := testrec.StepResult{Name: "logs in again", Kind: "recovery", Target: p}
		if err := d.waitUp(ctx, p, 15*time.Minute); err != nil {
			r.Error = err.Error()
		} else {
			r.Passed = true
		}
		r.Ms = time.Since(start).Milliseconds()
		out = append(out, r)
	}
	return out
}

// baselineFor restores what Baseline saved for these sessions (the others stay saved for later).
func (d *testDriver) baselineFor(ctx context.Context, sessions []string) []testrec.StepResult {
	var out []testrec.StepResult
	for _, r := range d.baseline(ctx) {
		out = append(out, r)
	}
	// saved again for a later failure: the next restore needs a fresh saved state
	all := d.baselineSessions
	res, back := d.a.testsSaveState(ctx, []string{"auto"}, all)
	d.baseline = back
	for _, r := range res {
		if !r.Passed {
			out = append(out, r)
		}
	}
	return out
}

// waitUp waits until the session logs in (the device came back after a restart).
func (d *testDriver) waitUp(ctx context.Context, path string, limit time.Duration) error {
	s, err := d.a.store.Get(path)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(limit)
	time.Sleep(20 * time.Second) // let a restart begin
	var last error
	for time.Now().Before(deadline) {
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		c, err := d.a.terms.OpenCapture(cctx, s, "")
		cancel()
		if err == nil {
			c.Close()
			return nil
		}
		last = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(20 * time.Second)
	}
	return fmt.Errorf("it did not log in again within %s: %v", limit, last)
}
