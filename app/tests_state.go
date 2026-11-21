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
	for _, kind := range kinds {
		for _, path := range sessions {
			start := time.Now()
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
