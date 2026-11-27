package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/testrec"
)

// The real web tool, driven as an AI would: open a saved web app, read it, log in with {{password}},
// onboard a device by serial number, check it, and save it into a test that also configures the switch.
func TestWebAIToolRecordsATest(t *testing.T) {
	pwPath := os.Getenv("TW_PLAYWRIGHT")
	if pwPath == "" {
		t.Skip("TW_PLAYWRIGHT not set")
	}
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("TERMWRIGHT_HOME", dir) // webRunner: <data>/playwright
	os.MkdirAll(filepath.Join(dir, "playwright", "node_modules"), 0o755)
	os.Symlink(pwPath, filepath.Join(dir, "playwright", "node_modules", "playwright"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sid"); err != nil || c.Value != "ok" {
			if r.Method == "POST" && r.FormValue("email") == "qa@lab" && r.FormValue("password") == "s3cret!" {
				http.SetCookie(w, &http.Cookie{Name: "sid", Value: "ok", Path: "/"})
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			fmt.Fprint(w, `<html><title>Sign in</title><body><h1>Sign in</h1><form method="post"><label for="e">Email</label><input id="e" name="email"><label for="p">Password</label><input id="p" name="password" type="password"><button>Log In</button></form></body></html>`)
			return
		}
		if r.URL.Path == "/devices" {
			fmt.Fprint(w, `<html><title>Devices</title><body><h1>Devices</h1><label for="sn">Serial number</label><input id="sn">
<button data-testid="onboard" onclick="document.getElementById('out').textContent='Onboarded '+document.getElementById('sn').value">Onboard</button><p id="out"></p></body></html>`)
			return
		}
		fmt.Fprint(w, `<html><title>Home</title><body><h1>Dashboard</h1><a href="/devices">Devices</a></body></html>`)
	}))
	defer srv.Close()
	st, err := session.NewStore(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(session.Session{Name: "Lab GUI", Folder: "Web Apps", Protocol: session.ProtoWeb, WebURL: srv.URL, Username: "qa@lab"}, ""); err != nil {
		t.Fatal(err)
	}
	tests := filepath.Join(dir, "tests")
	os.MkdirAll(filepath.Join(tests, "cases"), 0o755)
	os.WriteFile(filepath.Join(tests, "cases", "onboard.yaml"), []byte("name: onboard\nvars: { dut: Lab/x465-1 }\nsteps:\n  - run: { session: \"{{dut}}\", command: configure iqagent server ipaddress 10.1.1.5 }\n"), 0o644)
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{cfg: cfg, store: st, testsDir: tests}
	if err := secrets.Set("Web Apps/Lab GUI", "s3cret!"); err != nil {
		t.Fatal(err)
	}
	asked := 0
	ask := func(string, string, bool) error { asked++; return nil }
	call := func(args map[string]any) string {
		t.Helper()
		out, err := a.webAI(context.Background(), args, ask)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	ref := func(snap, name string) float64 { // the [n] of an element in a snapshot
		for _, l := range strings.Split(snap, "\n") {
			var n int
			if _, err := fmt.Sscanf(l, "[%d]", &n); err == nil && strings.Contains(l, "\""+name+"\"") {
				return float64(n)
			}
		}
		t.Fatalf("no %q in\n%s", name, snap)
		return 0
	}
	out := call(map[string]any{"action": "open", "app": "Web Apps/Lab GUI", "show": false})
	if !strings.Contains(out, "Page: Sign in") || !strings.Contains(out, "(password: use {{password}})") || asked != 1 {
		t.Fatalf("open:\n%s", out)
	}
	out = call(map[string]any{"action": "act", "actions": []any{
		map[string]any{"do": "fill", "ref": ref(out, "Email"), "value": "{{username}}"},
		map[string]any{"do": "fill", "ref": ref(out, "Password"), "value": "{{password}}"},
		map[string]any{"do": "click", "ref": ref(out, "Log In")},
	}})
	if strings.Count(out, "✓") != 3 || !strings.Contains(out, "Headings: Dashboard") {
		t.Fatalf("login:\n%s", out)
	}
	out = call(map[string]any{"action": "act", "do": "click", "ref": ref(out, "Devices")})
	out = call(map[string]any{"action": "act", "vars": map[string]any{"serial": "SN123"}, "actions": []any{
		map[string]any{"do": "fill", "ref": ref(out, "Serial number"), "value": "{{serial}}"},
		map[string]any{"do": "click", "ref": ref(out, "Onboard")},
		map[string]any{"do": "expect", "target": "#out", "value": "Onboarded SN124"}, // wrong on purpose
	}})
	if strings.Count(out, "✓") != 2 || !strings.Contains(out, "✗ expect #out") {
		t.Fatalf("onboard:\n%s", out)
	}
	call(map[string]any{"action": "act", "do": "expect", "target": "#out", "value": "Onboarded SN123"})
	out = call(map[string]any{"action": "recorded"})
	if !strings.Contains(out, `fill label=Password = "{{password}}"`) || !strings.Contains(out, `fill label=Serial number = "{{serial}}"`) || strings.Count(out, "\n") != 8 {
		t.Fatalf("recorded:\n%s", out)
	}
	out = call(map[string]any{"action": "save", "append_to": "cases/onboard.yaml", "name": "Onboard it in the GUI"})
	b, _ := os.ReadFile(filepath.Join(tests, "cases", "onboard.yaml"))
	yml := string(b)
	if strings.Contains(yml, "s3cret") || !strings.Contains(yml, "configure iqagent") || !strings.Contains(yml, "Onboard it in the GUI") || !strings.Contains(yml, "testid=onboard") {
		t.Fatalf("saved:\n%s", yml)
	}
	c, err := testrec.Folder{Root: tests}.Load("cases/onboard.yaml")
	if err != nil || len(c.Steps) != 2 || c.Steps[1].Web == nil || c.Steps[1].Web.Login != "{{lab_gui}}" || c.Vars["lab_gui"] != "Web Apps/Lab GUI" || len(c.Steps[1].Web.Actions) != 7 {
		t.Fatalf("%v %+v", err, c)
	}
	// a second recording saved as a new test gets the values as its defaults
	call(map[string]any{"action": "act", "vars": map[string]any{"serial": "SN9"}, "actions": []any{map[string]any{"do": "fill", "target": "label=Serial number", "value": "{{serial}}"}}})
	call(map[string]any{"action": "save", "file": "cases/gui/serial.yaml"})
	c, err = testrec.Folder{Root: tests}.Load("cases/gui/serial.yaml")
	if err != nil || c.Vars["serial"] != "SN9" || c.Steps[0].Web.Actions[0].Value != "{{serial}}" {
		t.Fatalf("%v %+v", err, c)
	}
	if out := call(map[string]any{"action": "close"}); !strings.Contains(out, "Closed") {
		t.Fatal(out)
	}
	if _, err := a.webAI(context.Background(), map[string]any{"action": "snapshot"}, ask); err == nil {
		t.Fatal("snapshot after close")
	}
}
