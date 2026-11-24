package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
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

// webExtra is where a web step's files are and its login: the Login session's user name and password
// fill {{username}} / {{password}} and answer HTTP logins; states (State) are kept in the data folder.
func (a *App) webExtra(w testrec.WebStep) (testrec.WebStep, webrun.Extra, string) {
	shots := filepath.Join(".", "web-shots")
	x := webrun.Extra{}
	if f, err := a.testsFolder(); err == nil {
		shots, x.Root = filepath.Join(f.Root, "reports", "web-shots"), f.Root
	}
	if d, err := config.DataDir(); err == nil {
		x.StateDir = filepath.Join(d, "web-state")
	}
	if f, err := a.testsFolder(); err == nil {
		if acts, err := f.ExpandBlocks(w.Actions); err == nil {
			w.Actions = acts
		}
	}
	if w.Login != "" {
		if s, err := a.store.Get(w.Login); err == nil {
			pw, ok := a.secret(s.Path)
			if !ok && s.DeviceType != "" {
				pw, _ = a.secret("devicetype:" + strings.ToLower(s.DeviceType))
			}
			if s.Protocol == session.ProtoWeb {
				w, x = webrun.ForApp(w, webrun.App{Name: s.Path, URL: s.WebURL, Browser: s.WebBrowser, Viewport: s.WebViewport,
					Insecure: s.WebInsecure, User: s.Username, Password: pw, Login: webrun.ParseLogin(s.WebLogin)}, x)
			} else {
				x.HTTPUser, x.HTTPPassword = s.Username, pw
				w = webrun.WithLogin(w, s.Username, pw)
			}
		}
	}
	return w, x, shots
}

// Web runs a web test step with Playwright; screenshots go to reports/web-shots in the tests folder.
func (d *testDriver) Web(ctx context.Context, w testrec.WebStep) (string, error) {
	timeout := 30
	if dl, ok := ctx.Deadline(); ok {
		timeout = max(2, min(timeout, int(time.Until(dl).Seconds())))
	}
	w, x, shots := d.a.webExtra(w)
	return webRunner().Run(ctx, w, shots, timeout, x)
}

var webRecMu sync.Mutex

// WebRecord opens the step's page in a browser and records what the person does there until the window is
// closed: the actions (each also sent as a "web:rec" event while recording).
func (a *App) WebRecord(w testrec.WebStep) ([]webrun.Action, error) {
	if !webRecMu.TryLock() {
		return nil, errors.New("a recording is already open: close its browser window first")
	}
	defer webRecMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	w.Actions = nil
	w2, x, _ := a.webExtra(w)
	return webRunner().Record(ctx, w2, func(ac webrun.Action) { emitter{a}.Emit("web:rec", ac) }, x)
}

// WebLeftovers lists the test browsers (and web runners) left running from earlier web steps; those of a
// recording or step-by-step browser open now are not listed.
func (a *App) WebLeftovers() []webrun.Leftover { return webRunner().Leftovers() }

// WebCloseLeftovers closes them and says how many.
func (a *App) WebCloseLeftovers() int { return webRunner().CloseLeftovers() }

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
