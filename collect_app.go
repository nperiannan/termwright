package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/checklist"
	"github.com/termwright/termwright/internal/config"
)

// Run and Collect: commands on many devices, one output file per device; the
// window extracts values into a table and exports it as CSV.

// CollectDir is the folder the collected outputs are saved in.
func (a *App) CollectDir() string {
	if d := strings.TrimSpace(a.cfg.Get().CollectDir); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Documents", "Termwright Collected")
}

func (a *App) collects() *checklist.Store { return &checklist.Store{Dir: a.CollectDir()} }

// CollectRuns lists a collection's runs, newest first.
func (a *App) CollectRuns(name string) ([]*checklist.Run, error) { return a.collects().Runs(name) }

// CollectRun runs a collection's commands on its devices in the background
// (events "collect:progress" and "collect:done") and returns the run id.
func (a *App) CollectRun(name, note string) (string, error) {
	var c *config.Collection
	for _, x := range a.cfg.Get().Collections {
		if x.Name == name {
			x := x
			c = &x
		}
	}
	if c == nil {
		return "", fmt.Errorf("no collection %q (save it first)", name)
	}
	st := a.collects()
	r, err := st.NewRun(name, note, time.Now())
	if err != nil {
		return "", err
	}
	return a.runOnDevices(st, config.Checklist{Name: c.Name, Targets: c.Targets, Commands: c.Commands}, name, r.ID, "output", "collect")
}

// CollectCancel stops a running collection.
func (a *App) CollectCancel(name, runID string) {
	checkMu.Lock()
	if c := checkRuns["collect\x00"+name+"\x00"+runID]; c != nil {
		c()
	}
	checkMu.Unlock()
}

// ShowCollectFolder opens the collect folder in the file manager.
func (a *App) ShowCollectFolder() error {
	d := a.CollectDir()
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	return a.ShowInFolder(d)
}

// SaveTextAs asks where to save text (e.g. a CSV export) and writes it.
func (a *App) SaveTextAs(title, name, text string) (string, error) {
	p, err := a.ChooseSaveFile(title, name, nil)
	if err != nil || p == "" {
		return "", err
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		return "", err
	}
	return p, nil
}
