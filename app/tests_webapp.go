package app

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/testrec"
	"github.com/termwright/termwright/internal/webrun"
)

// webApp is a Web App session with its password.
func (a *App) webApp(path string) (webrun.App, error) {
	s, err := a.store.Get(path)
	if err != nil {
		return webrun.App{}, err
	}
	if s.Protocol != session.ProtoWeb {
		return webrun.App{}, errors.New(path + " is not a web app session")
	}
	pw, _ := a.secret(s.Path)
	return webrun.App{Name: s.Path, URL: s.WebURL, Browser: s.WebBrowser, Viewport: s.WebViewport, Insecure: s.WebInsecure,
		User: s.Username, Password: pw, Login: webrun.ParseLogin(s.WebLogin)}, nil
}

// WebRecordLogin opens a web app's address in a fresh browser (no saved login) and records the login the
// person does there until the window is closed. The password typed becomes {{password}} and the user
// name {{username}}; save the result in the session's webLogin.
func (a *App) WebRecordLogin(path string, url, user string) ([]webrun.Action, error) {
	if !webRecMu.TryLock() {
		return nil, errors.New("a recording is already open: close its browser window first")
	}
	defer webRecMu.Unlock()
	app, _ := a.webApp(path)
	if url == "" {
		url = app.URL
	}
	if user == "" {
		user = app.User
	}
	if url == "" {
		return nil, errors.New("type the web app's address first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	acts, err := webRunner().Record(ctx, webrun.Step{URL: url, Insecure: app.Insecure, Browser: app.Browser, Viewport: app.Viewport},
		func(ac webrun.Action) { emitter{a}.Emit("web:rec", ac) }, webrun.Extra{})
	for i := range acts {
		if user != "" && acts[i].Value == user && (acts[i].Do == "fill" || acts[i].Do == "type") {
			acts[i].Value = "{{username}}"
		}
		acts[i].Secret = false
	}
	return acts, err
}

// WebTestLogin opens the web app in a hidden browser without a saved login and does its recorded login:
// what happened, in a few lines.
func (a *App) WebTestLogin(path string) (string, error) {
	app, err := a.webApp(path)
	if err != nil {
		return "", err
	}
	if len(app.Login) == 0 {
		return "", errors.New("record the login first")
	}
	app.Name = "" // a fresh browser: no saved login
	w, x := webrun.ForApp(webrun.Step{}, app, webrun.Extra{NoVideo: true})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := webRunner().Run(ctx, w, "", 30, x)
	if err != nil {
		return out, err
	}
	if !strings.Contains(out, "logged in") {
		return out, errors.New("the page did not ask to log in: the login's first field (" + firstTarget(app.Login) + ") is not on " + w.URL)
	}
	return out, nil
}

func firstTarget(l []webrun.Action) string {
	for _, a := range l {
		if a.Target != "" {
			return a.Target
		}
	}
	return ""
}

// WebOpenApp opens a web app in a browser, logged in; it stays open until its window is closed.
func (a *App) WebOpenApp(path string) error {
	app, err := a.webApp(path)
	if err != nil {
		return err
	}
	w, x := webrun.ForApp(webrun.Step{}, app, a.webBaseExtra())
	s, err := webRunner().Open(context.Background(), w, "", x)
	if err != nil {
		return err
	}
	go func() { <-s.Closed() }()
	return nil
}

// webBaseExtra is where web files are, without a step.
func (a *App) webBaseExtra() webrun.Extra {
	_, x, _ := a.webExtra(testrec.WebStep{})
	return x
}

// ---- step by step: a browser kept open while a web step is edited ----

var (
	webDbgMu sync.Mutex
	webDbg   *webrun.Session
	webDbgW  testrec.WebStep // the step it was opened for (its app and login)
)

// WebDebugOpen opens a browser for a web step (at its page, logged in) to run its actions one by one;
// "web:debug" says when it closes, "web:rec" brings what is recorded.
func (a *App) WebDebugOpen(w testrec.WebStep, vars map[string]string) error {
	webDbgMu.Lock()
	defer webDbgMu.Unlock()
	if webDbg != nil {
		webDbg.Close()
		webDbg = nil
	}
	w = testrec.FillWeb(w, vars)
	acts := w.Actions
	w.Actions = nil
	w2, x, shots := a.webExtra(w)
	s, err := webRunner().Open(context.Background(), w2, shots, x)
	if err != nil {
		return err
	}
	w.Actions = acts
	webDbg, webDbgW = s, w
	s.OnRec = func(ac webrun.Action) { emitter{a}.Emit("web:rec", ac) }
	go func() {
		<-s.Closed()
		webDbgMu.Lock()
		if webDbg == s {
			webDbg = nil
		}
		webDbgMu.Unlock()
		emitter{a}.Emit("web:debug", map[string]any{"open": false})
	}()
	emitter{a}.Emit("web:debug", map[string]any{"open": true})
	return nil
}

func (a *App) webDebug() (*webrun.Session, error) {
	webDbgMu.Lock()
	defer webDbgMu.Unlock()
	if webDbg == nil {
		return nil, errors.New("open the browser first (Debug in Browser)")
	}
	return webDbg, nil
}

// WebDebugRun does actions in the open browser (from: the number of the first, for the results); it stops
// at the first that fails. Blocks, variables and the app's login are filled in as in a run.
func (a *App) WebDebugRun(acts []webrun.Action, from int, vars map[string]string) ([]webrun.Result, error) {
	s, err := a.webDebug()
	if err != nil {
		return nil, err
	}
	w := webDbgW
	w.URL, w.Actions = "", acts
	w = testrec.FillWeb(w, vars)
	w2, _, _ := a.webExtra(w)
	if len(w2.Actions) != len(acts) { // blocks became their actions: number them as one
		res, err := s.Run(w2.Actions, 0)
		ok := err == nil && len(res) == len(w2.Actions)
		for _, r := range res {
			ok = ok && r.OK
		}
		r := webrun.Result{I: from, OK: ok, Note: "block of " + itoa(len(w2.Actions)) + " actions"}
		if !ok && len(res) > 0 {
			r.Error = res[len(res)-1].Error
		}
		return []webrun.Result{r}, err
	}
	return s.Run(w2.Actions, from)
}

// WebDebugPick waits for an element to be clicked in the open browser: its target and fallbacks (nil: Esc).
func (a *App) WebDebugPick() (*webrun.Pick, error) {
	s, err := a.webDebug()
	if err != nil {
		return nil, err
	}
	return s.Pick()
}

// WebDebugRecord turns recording in the open browser on or off (actions come as "web:rec").
func (a *App) WebDebugRecord(on bool) error {
	s, err := a.webDebug()
	if err != nil {
		return err
	}
	return s.Record(on)
}

// WebDebugRestart goes back to the step's page in the open browser.
func (a *App) WebDebugRestart() error {
	s, err := a.webDebug()
	if err != nil {
		return err
	}
	return s.Restart("")
}

// WebDebugClose closes the open browser.
func (a *App) WebDebugClose() {
	webDbgMu.Lock()
	s := webDbg
	webDbg = nil
	webDbgMu.Unlock()
	if s != nil {
		s.Close()
	}
}

// TestsSaveWebBlock saves web actions as a block (a procedure) other web steps can use.
func (a *App) TestsSaveWebBlock(name, description string, acts []webrun.Action) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		return "", errors.New("give the block a name")
	}
	return f.SaveWebBlock(strings.TrimSpace(name), description, acts)
}

// TestsWebBlocks lists the blocks (procedures that are one web step) with their parameters.
func (a *App) TestsWebBlocks() []testrec.ProcInfo {
	f, err := a.testsFolder()
	if err != nil {
		return nil
	}
	out := []testrec.ProcInfo{}
	for _, p := range f.Procs() {
		if pr, err := f.LoadProc(p.File); err == nil && len(pr.Steps) == 1 && pr.Steps[0].Web != nil {
			p.File = strings.TrimSuffix(strings.TrimPrefix(p.File, "procs/"), ".yaml")
			out = append(out, p)
		}
	}
	return out
}

// webAppHTTP sends a REST step to a web app session: the path goes after its address and the request
// carries the login the web steps keep (the browser's cookies), so API calls run as the logged-in user.
func (a *App) webAppHTTP(ctx context.Context, h testrec.HTTPStep) (testrec.HTTPAnswer, bool, error) {
	s, err := a.store.Get(h.Session)
	if err != nil || s.Protocol != session.ProtoWeb {
		return testrec.HTTPAnswer{}, false, nil
	}
	u := webrun.Join(s.WebURL, h.URL)
	var body io.Reader
	if h.Body != "" {
		body = strings.NewReader(h.Body)
	}
	m := strings.ToUpper(h.Method)
	if m == "" {
		m = "GET"
	}
	req, err := http.NewRequestWithContext(ctx, m, u, body)
	if err != nil {
		return testrec.HTTPAnswer{}, true, err
	}
	if b := strings.TrimSpace(h.Body); b != "" && (b[0] == '{' || b[0] == '[') {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, */*")
	for k, v := range h.Headers {
		req.Header.Set(k, v)
	}
	x := a.webBaseExtra()
	if c := webrun.Cookies(webrun.StatePath(x.StateDir, "app-"+s.Path), u); c != "" && req.Header.Get("Cookie") == "" {
		req.Header.Set("Cookie", c)
	}
	cl := &http.Client{Timeout: 60 * time.Second}
	if s.WebInsecure {
		cl.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // the session says so
	}
	res, err := cl.Do(req)
	if err != nil {
		return testrec.HTTPAnswer{}, true, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	return testrec.HTTPAnswer{Status: res.StatusCode, Body: string(b)}, true, nil
}

// TestsImage is a picture or screenshot of a web step's run (a path in the tests folder, or one it wrote in
// its reports) as a data: URL, for the run view.
func (a *App) TestsImage(p string) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(f.Root, filepath.FromSlash(p))
	}
	rel, err := filepath.Rel(f.Root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("not a file of the tests folder")
	}
	ext := strings.ToLower(filepath.Ext(abs))
	mt := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png"}[ext]
	if mt == "" {
		return "", errors.New("not a picture")
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}

// Facts are a saved session's facts for {{role.device_type}}, {{role.host}}, {{role.model}}, {{role.version}}
// and {{role.name}} in tests.
func (d *testDriver) Facts(path string) map[string]string {
	s, err := d.a.store.Get(path)
	if err != nil {
		return nil
	}
	out := map[string]string{"device_type": s.DeviceType, "host": s.Hostname, "name": s.Name}
	if s.Protocol == session.ProtoWeb {
		out["host"] = s.WebURL
	}
	if st := d.a.cfg.Get().DeviceFacts[path]; st != nil {
		out["model"], out["version"] = st["Model"], st["Version"]
	}
	return out
}
