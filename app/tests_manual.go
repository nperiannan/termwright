package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/testrec"
)

type manualAnswer struct {
	passed bool
	note   string
}

var (
	manualMu   sync.Mutex
	manualWait = map[string]chan manualAnswer{}
)

// CanAsk: manual steps are asked only in runs a person started in the window.
func (d *testDriver) CanAsk() bool { return d.interactive && d.a.ctx != nil }

// AskManual shows a manual step ("tests:manual") and waits for the person's answer (TestManualAnswer).
func (d *testDriver) AskManual(ctx context.Context, name, instruction, expected string) (bool, string, error) {
	id := randHex(6)
	ch := make(chan manualAnswer, 1)
	manualMu.Lock()
	manualWait[id] = ch
	manualMu.Unlock()
	defer func() { manualMu.Lock(); delete(manualWait, id); manualMu.Unlock() }()
	emitter{d.a}.Emit("tests:manual", map[string]string{"id": id, "name": name, "instruction": instruction, "expected": expected})
	_ = d.a.Notify("A test waits for you", instruction)
	select {
	case a := <-ch:
		return a.passed, a.note, nil
	case <-ctx.Done():
		emitter{d.a}.Emit("tests:manual-done", id)
		return false, "", errors.New("no answer")
	}
}

// TestManualAnswer answers a manual step: result "passed", "failed" or "skipped", with a note.
func (a *App) TestManualAnswer(id, result, note string) error {
	manualMu.Lock()
	ch := manualWait[id]
	manualMu.Unlock()
	if ch == nil {
		return errors.New("the step is no longer waiting")
	}
	if result == "skipped" {
		note = "skipped by the person" + map[bool]string{true: ": " + note, false: ""}[note != ""]
	}
	ch <- manualAnswer{passed: result != "failed", note: note}
	return nil
}

// TestImportManual reads manual test cases from an Excel (.xlsx) or CSV file the person picks and
// saves them as tests (existing files are left alone). device: the session for rows without one.
func (a *App) TestImportManual(device string) ([]string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return nil, err
	}
	p, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Import Manual Test Cases",
		Filters: []wruntime.FileFilter{{DisplayName: "Test cases (*.xlsx, *.csv)", Pattern: "*.xlsx;*.csv"}}})
	if err != nil || p == "" {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var cs []testrec.ImportedCase
	if strings.EqualFold(filepath.Ext(p), ".xlsx") {
		rows, err := testrec.XLSXRows(b)
		if err != nil {
			return nil, err
		}
		cs, err = testrec.ImportRows(rows, device)
		if err != nil {
			return nil, err
		}
	} else if cs, err = testrec.ImportManual(strings.NewReader(string(b)), device); err != nil {
		return nil, err
	}
	var out []string
	for _, c := range cs {
		if _, err := f.ReadText(c.File); err == nil {
			out = append(out, c.File+" (already there - not changed)")
			continue
		}
		rel, err := f.Save(c.File, c.Case)
		if err != nil {
			return out, fmt.Errorf("%s: %v", c.File, err)
		}
		f.WriteGo(rel)
		out = append(out, rel)
	}
	return out, nil
}
