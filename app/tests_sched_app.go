package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/termwright/termwright/internal/integrations"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/testrec"
)

// Scheduled test runs: Settings.TestSchedules, checked every minute while Termwright is open; the
// last run of each is kept in test-schedules.json in the data folder.

// TestScheduleState is a schedule's last run.
type TestScheduleState struct {
	// Last: when it last ran.
	Last time.Time `json:"last"`
	// Passed tests of the last run.
	Passed int `json:"passed"`
	// Failed tests of the last run.
	Failed int `json:"failed"`
	// Dir: the last run's report folder, relative to the tests folder.
	Dir string `json:"dir,omitempty"`
	// Error: why the last run could not start.
	Error string `json:"error,omitempty"`
}

// TestScheduleView is a schedule with its last and next run, for the Tests tab.
type TestScheduleView struct {
	testrec.Schedule
	// State: the last run.
	State TestScheduleState `json:"state"`
	// Next: when it runs next (empty when off).
	Next string `json:"next,omitempty"`
	// When: "daily 06:30", "every 4 hours".
	When string `json:"when"`
}

var schedMu sync.Mutex

func (a *App) schedFile() string { return filepath.Join(a.cfg.DataDir(), "test-schedules.json") }

func (a *App) schedStates() map[string]TestScheduleState {
	m := map[string]TestScheduleState{}
	if b, err := os.ReadFile(a.schedFile()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func (a *App) schedSetState(id string, st TestScheduleState) {
	schedMu.Lock()
	defer schedMu.Unlock()
	m := a.schedStates()
	m[id] = st
	b, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(a.schedFile(), b, 0o600)
}

// TestSchedules lists the schedules with their last and next run.
func (a *App) TestSchedules() []TestScheduleView {
	out := []TestScheduleView{}
	if a.cfg == nil {
		return out
	}
	st, now := a.schedStates(), time.Now()
	for _, s := range a.cfg.Get().TestSchedules {
		v := TestScheduleView{Schedule: s, State: st[s.ID], When: s.Text()}
		if n := s.Next(st[s.ID].Last, now); !n.IsZero() {
			v.Next = n.Format(time.RFC3339)
		}
		out = append(out, v)
	}
	return out
}

// TestSaveSchedule adds a schedule (no ID) or replaces the one with its ID; it returns the saved one.
func (a *App) TestSaveSchedule(s testrec.Schedule) (testrec.Schedule, error) {
	if err := a.ready(); err != nil {
		return s, err
	}
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		s.Name = "Tests " + s.Text()
	}
	if msg := s.Check(); msg != "" {
		return s, errors.New(msg)
	}
	cfg := a.cfg.Get()
	list := append([]testrec.Schedule(nil), cfg.TestSchedules...)
	found := false
	for i := range list {
		if s.ID != "" && list[i].ID == s.ID {
			list[i], found = s, true
		}
	}
	if !found {
		if s.ID == "" {
			b := make([]byte, 6)
			_, _ = rand.Read(b)
			s.ID = hex.EncodeToString(b)
		}
		list = append(list, s)
	}
	cfg.TestSchedules = list
	return s, a.cfg.Save(cfg)
}

// TestRemoveSchedule removes a schedule.
func (a *App) TestRemoveSchedule(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	cfg := a.cfg.Get()
	var list []testrec.Schedule
	for _, s := range cfg.TestSchedules {
		if s.ID != id {
			list = append(list, s)
		}
	}
	if len(list) == len(cfg.TestSchedules) {
		return fmt.Errorf("no schedule %s", id)
	}
	cfg.TestSchedules = list
	return a.cfg.Save(cfg)
}

// TestRunScheduleNow runs a schedule's tests now (in the background); its state is updated as after
// a scheduled run.
func (a *App) TestRunScheduleNow(id string) error {
	for _, s := range a.cfg.Get().TestSchedules {
		if s.ID == id {
			go a.runSchedule(context.Background(), s)
			return nil
		}
	}
	return fmt.Errorf("no schedule %s", id)
}

func (a *App) testsScheduler(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if a.ready() != nil {
				continue
			}
			st := a.schedStates()
			for _, s := range a.cfg.Get().TestSchedules {
				if s.Due(st[s.ID].Last, now) && a.runSchedule(ctx, s) {
					break // one run at a time; the next due schedule waits a minute
				}
			}
		}
	}
}

// runSchedule runs one schedule; false when tests were already running (it is tried again later).
func (a *App) runSchedule(ctx context.Context, s testrec.Schedule) bool {
	rctx, cancel := context.WithTimeout(ctx, 6*time.Hour)
	defer cancel()
	rep, err := a.runTests(rctx, TestRunRequest{Files: s.Files, Testbed: s.Testbed, Confirmed: s.Production, OnCleanupFail: s.OnCleanupFail, Cleanup: s.Cleanup, Tags: s.Tags, Rerun: s.Rerun, RunTimeout: s.RunTimeout * 60, scheduled: true}, nil)
	if err != nil && len(rep.Results) == 0 && strings.Contains(err.Error(), "already running") {
		return false
	}
	state := TestScheduleState{Last: time.Now(), Passed: rep.Passed, Failed: rep.Failed, Dir: rep.Dir}
	if err != nil && len(rep.Results) == 0 {
		state.Error = err.Error()
	}
	a.schedSetState(s.ID, state)
	emitter{a}.Emit("tests:schedule", s.ID)
	a.afterRun(rep, true)
	m := integrations.Message{Title: "Scheduled tests " + s.Name, Failed: state.Error != "" || rep.Failed > 0 || rep.Skipped > 0}
	switch {
	case state.Error != "":
		m.Text = "did not run: " + state.Error
	default:
		m.Text = fmt.Sprintf("%d passed, %d failed, %d skipped (%s, testbed %s)", rep.Passed, rep.Failed, rep.Skipped, (time.Duration(rep.Ms) * time.Millisecond).Round(time.Second), orDash(s.Testbed))
		for _, r := range rep.Results {
			if !r.Passed && len(m.Lines) < 20 {
				m.Lines = append(m.Lines, r.Case+": "+orDash(testrec.FirstProblem(r)))
			}
		}
		if rep.Dir != "" {
			m.Lines = append(m.Lines, "Report: "+rep.Dir)
		}
	}
	a.notifyResults(m)
	switch {
	case state.Error != "":
		emitter{a}.Emit("toast", map[string]string{"level": "error", "message": "Scheduled tests " + s.Name + ": " + state.Error})
		_ = a.Notify("Scheduled tests did not run", s.Name+": "+state.Error)
	case rep.Failed > 0:
		msg := fmt.Sprintf("%s: %d of %d failed", s.Name, rep.Failed, rep.Passed+rep.Failed)
		emitter{a}.Emit("toast", map[string]string{"level": "error", "message": "Scheduled tests " + msg})
		_ = a.Notify("Scheduled tests failed", msg)
	}
	return true
}
