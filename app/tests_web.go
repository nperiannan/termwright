package app

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/testrec"
	"github.com/termwright/termwright/internal/webrun"
)

func webRunner() webrun.Runner {
	d, err := config.DataDir()
	if err != nil {
		d = "."
	}
	return webrun.Runner{Dir: filepath.Join(d, "playwright")}
}

// Web runs a web test step with Playwright; screenshots go to reports/web-shots in the tests folder.
func (d *testDriver) Web(ctx context.Context, w testrec.WebStep) (string, error) {
	shots := filepath.Join(".", "web-shots")
	if f, err := d.a.testsFolder(); err == nil {
		shots = filepath.Join(f.Root, "reports", "web-shots")
	}
	timeout := 30
	if dl, ok := ctx.Deadline(); ok {
		timeout = max(2, min(timeout, int(time.Until(dl).Seconds())))
	}
	return webRunner().Run(ctx, w, shots, timeout)
}

// WebStatus says whether web steps can run (Node.js and Playwright installed).
func (a *App) WebStatus() webrun.Status { return webRunner().Status() }

var webSetupMu sync.Mutex

// WebSetup installs Playwright and its Chromium for web steps; its lines come as "web:setup" events.
func (a *App) WebSetup() error {
	if !webSetupMu.TryLock() {
		return nil
	}
	defer webSetupMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	return webRunner().Setup(ctx, func(s string) { emitter{a}.Emit("web:setup", s) })
}
