package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/checklist"
	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/term"
)

// CheckResult is one device of a checklist phase (event "check:progress").
type CheckResult struct {
	List    string `json:"list"`
	Run     string `json:"run"`
	Phase   string `json:"phase"`
	Path    string `json:"path"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Changed *bool  `json:"changed,omitempty"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
}

var (
	checkMu   sync.Mutex
	checkRuns = map[string]context.CancelFunc{}
)

// ChecklistDir is the folder the checklist runs are saved in.
func (a *App) ChecklistDir() string {
	if d := strings.TrimSpace(a.cfg.Get().ChecklistDir); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Documents", "Termwright Checklists")
}

func (a *App) checklists() *checklist.Store { return &checklist.Store{Dir: a.ChecklistDir()} }

func (a *App) checklistNamed(name string) (config.Checklist, error) {
	for _, c := range a.cfg.Get().Checklists {
		if c.Name == name {
			return c, nil
		}
	}
	return config.Checklist{}, fmt.Errorf("no checklist %q (save it first)", name)
}

// ChecklistRuns lists a checklist's runs, newest first.
func (a *App) ChecklistRuns(name string) ([]*checklist.Run, error) { return a.checklists().Runs(name) }

// ChecklistRun runs a checklist's commands on its devices in the background:
// phase "before" starts a new run (runID "") or repeats the before snapshot
// of a run; phase "after" takes the after snapshot of a run and marks which
// devices changed. It returns the run id.
func (a *App) ChecklistRun(name, runID, phase, note string) (string, error) {
	c, err := a.checklistNamed(name)
	if err != nil {
		return "", err
	}
	ph := checklist.Phase(phase)
	if ph != checklist.Before && ph != checklist.After {
		return "", errors.New("phase must be before or after")
	}
	st := a.checklists()
	if runID == "" {
		if ph == checklist.After {
			return "", errors.New("take the before snapshot first")
		}
		r, err := st.NewRun(name, note, time.Now())
		if err != nil {
			return "", err
		}
		runID = r.ID
	}
	return a.runOnDevices(st, c, name, runID, ph, "check")
}

// runOnDevices runs the checklist's commands on its devices in the
// background (four at a time), records each device in st and emits
// "<event>:progress" per device and "<event>:done" at the end.
func (a *App) runOnDevices(st *checklist.Store, c config.Checklist, name, runID string, ph checklist.Phase, event string) (string, error) {
	paths, err := a.BackupSessions(c.Targets)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", errors.New("no sessions in the checklist")
	}
	key := event + "\x00" + name + "\x00" + runID
	ctx, cancel := context.WithCancel(context.Background())
	checkMu.Lock()
	if checkRuns[key] != nil {
		checkMu.Unlock()
		cancel()
		return "", errors.New("this run is already in progress")
	}
	checkRuns[key] = cancel
	checkMu.Unlock()
	go func() {
		defer func() {
			checkMu.Lock()
			delete(checkRuns, key)
			checkMu.Unlock()
			cancel()
		}()
		var mu sync.Mutex
		done, failed, changed := 0, 0, 0
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		for _, p := range paths {
			if ctx.Err() != nil {
				break
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(p string) {
				defer func() { <-sem; wg.Done() }()
				out, log, err := a.checkOne(ctx, p, c)
				d, rerr := st.Record(name, runID, p, ph, out, err, log, time.Now())
				res := CheckResult{List: name, Run: runID, Phase: string(ph), Path: p, OK: err == nil && rerr == nil}
				if err == nil {
					err = rerr
				}
				if err != nil {
					res.Error = err.Error()
				}
				if d != nil {
					res.Changed = d.Changed
				}
				mu.Lock()
				done++
				if !res.OK {
					failed++
				} else if res.Changed != nil && *res.Changed {
					changed++
				}
				res.Done, res.Total = done, len(paths)
				mu.Unlock()
				emitter{a}.Emit(event+":progress", res)
			}(p)
		}
		wg.Wait()
		emitter{a}.Emit(event+":done", map[string]any{"list": name, "run": runID, "phase": string(ph), "total": len(paths), "done": done,
			"failed": failed, "changed": changed, "cancelled": ctx.Err() != nil})
	}()
	return runID, nil
}

// ChecklistCancel stops a running checklist phase.
func (a *App) ChecklistCancel(name, runID string) {
	checkMu.Lock()
	if c := checkRuns["check\x00"+name+"\x00"+runID]; c != nil {
		c()
	}
	checkMu.Unlock()
}

// checkOne runs the checklist's commands for the session's device type and
// returns the formatted output, or the error with what the device sent.
func (a *App) checkOne(ctx context.Context, p string, c config.Checklist) (string, string, error) {
	s, err := a.store.Get(p)
	if err != nil {
		return "", "", err
	}
	if s, err = a.consoleSession(a.withFolderDefaults(s)); err != nil {
		return "", "", err
	}
	prompt := ""
	if s.DeviceType != "" && a.terms.DeviceType != nil {
		if dt, ok := a.terms.DeviceType(s.DeviceType); ok {
			prompt = dt.Prompt
		}
	}
	cmds := c.For(s.DeviceType)
	if len(cmds) == 0 {
		if s.DeviceType == "" {
			return "", "", errors.New("no commands for sessions without a device type in this checklist")
		}
		return "", "", fmt.Errorf("no commands for device type %s in this checklist", s.DeviceType)
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	outs, err := a.terms.CaptureEach(cctx, s, cmds, prompt)
	if err != nil {
		var ce *term.CaptureError
		if errors.As(err, &ce) {
			return "", "Checklist on " + p + " failed: " + err.Error() + "\n\nWhat the device sent:\n\n" + ce.Transcript, err
		}
		return "", "", err
	}
	return checklist.Format(cmds, outs), "", nil
}

// ShowChecklistFolder opens the checklist folder in the file manager.
func (a *App) ShowChecklistFolder() error {
	d := a.ChecklistDir()
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	return a.ShowInFolder(d)
}
