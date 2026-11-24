package testrec

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/standalone"
	"github.com/termwright/termwright/internal/webrun"
)

func TestImportWebRows(t *testing.T) {
	cs, err := ImportManual(strings.NewReader("Test,Step,Device,Type,Expected\n"+
		"Add contact,goto /contacts,Apps/CRM,web,\n"+
		"Add contact,fill label=Full name = {{who}},Apps/CRM,web,\n"+
		"Add contact,click #add,Apps/CRM,web,\"Shows \"\"Added\"\"\"\n"+
		"Add contact,show vlan,Lab/sw1,,\n"+
		"Add contact,Open the CRM and smile,Apps/CRM,web,\n"), "")
	if err != nil || len(cs) != 1 {
		t.Fatal(err, cs)
	}
	st := cs[0].Case.Steps
	if len(st) != 3 || st[0].Web == nil || st[0].Web.Login != "Apps/CRM" || len(st[0].Web.Actions) != 4 || st[1].Run == nil || st[2].Manual == nil {
		t.Fatalf("%+v", st)
	}
	a := st[0].Web.Actions
	if a[0].Do != "goto" || a[0].Value != "/contacts" || a[1].Target != "label=Full name" || a[1].Value != "{{who}}" || a[3].Do != "expect" || a[3].Value != "Added" {
		t.Fatalf("%+v", a)
	}
	for in, want := range map[string]WebAction{
		`expect "Networks"`:          {Do: "expect", Value: "Networks"},
		"expect #out = Added Ann":    {Do: "expect", Target: "#out", Value: "Added Ann"},
		"Open dl2.example.com/login": {Do: "goto", Value: "dl2.example.com/login"},
		"press Enter":                {Do: "press", Value: "Enter"},
		"select label=Site = Lab":    {Do: "select", Target: "label=Site", Value: "Lab"},
	} {
		if got, ok := ParseWebAction(in); !ok || got.Do != want.Do || got.Target != want.Target || got.Value != want.Value {
			t.Errorf("%q: %+v %v", in, got, ok)
		}
	}
	if _, ok := ParseWebAction("fill label=Name"); ok {
		t.Error("fill without a value")
	}
}

// TestExportPlaywright exports a test with a web step on a web app (recorded login, a data file, a healed
// target) and - with TW_PW_NODE_MODULES set to a node_modules with @playwright/test - runs it.
func TestExportPlaywright(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sid"); err != nil || c.Value != "ok" {
			fmt.Fprint(w, `<html><title>Login</title><body><label>User <input id="u"></label><label>Password <input type="password" id="p"></label>
<button onclick="if(document.getElementById('u').value==='qa'&&document.getElementById('p').value==='pw1'){document.cookie='sid=ok;path=/';location.reload()}">Sign in</button></body></html>`)
			return
		}
		fmt.Fprint(w, `<html><title>Contacts</title><body><h1>Contacts</h1><label>Name <input id="contact-name"></label><button id="add" onclick="document.getElementById('out').textContent='Added '+document.getElementById('contact-name').value">Add</button><div id="out"></div></body></html>`)
	}))
	defer srv.Close()
	d := t.TempDir()
	f := Folder{Root: d}
	os.MkdirAll(filepath.Join(d, "cases", "crm"), 0o755)
	os.MkdirAll(filepath.Join(d, "data"), 0o755)
	os.WriteFile(filepath.Join(d, "data", "people.csv"), []byte("who\nAnn\nBob, Jr\n"), 0o644)
	os.WriteFile(filepath.Join(d, "cases", "crm", "add.yaml"), []byte(`name: Add a contact
data: data/people.csv
vars: {who: Zed}
steps:
  - run: {session: Lab/sw1, command: show vlan}
  - web:
      login: Apps/CRM
      url: /contacts
      actions:
        - {do: fill, target: 'label=Full name', alt: ['#contact-name'], value: '{{who}}'}
        - {do: click, target: '#add'}
        - {do: expect, target: '#out', value: 'Added {{who}}'}
        - {do: read, target: 'h1', value: title}
        - {do: expect, value: Contacts}
`), 0o644)
	os.WriteFile(filepath.Join(d, "cases", "crm", "fail.yaml"), []byte("name: Missing text\nsteps:\n  - web:\n      login: Apps/CRM\n      url: /contacts\n      actions:\n        - {do: expect, value: Not on the page}\n"), 0o644)
	os.WriteFile(filepath.Join(d, "cases", "crm", "cli.yaml"), []byte("name: CLI only\nsteps:\n  - run: {session: Lab/sw1, command: show vlan}\n"), 0o644)
	login := []webrun.Action{{Do: "fill", Target: "label=User", Value: "{{username}}"}, {Do: "fill", Target: "label=Password", Value: "{{password}}"}, {Do: "click", Target: "text=Sign in"}}
	dev := func(s string) (InventoryDevice, bool) {
		if s == "Apps/CRM" {
			return standalone.Device{Session: s, Protocol: "web", WebURL: srv.URL, Username: "qa", WebLogin: login}, true
		}
		return standalone.Device{}, false
	}
	zb, name, err := f.Export(ExportOptions{Lang: "playwright", OS: "linux", Name: "crm", Device: dev})
	if err != nil || name != "crm-playwright.zip" {
		t.Fatal(err, name)
	}
	zr, _ := zip.NewReader(bytes.NewReader(zb), int64(len(zb)))
	out := t.TempDir()
	files := map[string]string{}
	for _, zf := range zr.File {
		rc, _ := zf.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		rel := strings.TrimPrefix(zf.Name, "crm/")
		files[rel] = string(b)
		os.MkdirAll(filepath.Join(out, filepath.Dir(rel)), 0o755)
		os.WriteFile(filepath.Join(out, rel), b, 0o644)
	}
	spec := files["tests/crm/add.spec.ts"]
	for _, want := range []string{"import { tw } from '../../tw';", `const rows = tw.rows("data/people.csv")`, `await p.goto(tw.url("Apps/CRM", "/contacts", v));`,
		`(await tw.find(p, "label=Full name", ["#contact-name"])).fill(tw.v("{{who}}", v))`, `not in Playwright: "show vlan" on Lab/sw1`} {
		if !strings.Contains(spec, want) {
			t.Errorf("spec lacks %s:\n%s", want, spec)
		}
	}
	if files["tests/crm/cli.spec.ts"] != "" || !strings.Contains(files["README.md"], "cases/crm/cli.yaml") || strings.Contains(files["apps.json"], "pw1") ||
		!strings.Contains(files["apps.json"], `"passwordEnv": "TW_PASSWORD_CRM"`) || files["data/people.csv"] == "" || files["tw.ts"] == "" {
		t.Fatalf("files: %v\n%s", keys(files), files["apps.json"])
	}
	nm := os.Getenv("TW_PW_NODE_MODULES")
	if nm == "" {
		t.Skip("TW_PW_NODE_MODULES not set: the spec is not run")
	}
	os.Symlink(nm, filepath.Join(out, "node_modules"))
	if tc, err := exec.Command("npx", "--prefix", out, "tsc", "--noEmit", "-p", out).CombinedOutput(); err != nil { // the code type-checks
		t.Fatalf("tsc: %v\n%s", err, tc)
	}
	cmd := exec.Command("npx", "playwright", "test", "--reporter=line", "tests/crm/add.spec.ts")
	cmd.Dir = out
	cmd.Env = append(os.Environ(), "TW_PASSWORD_CRM=pw1")
	b, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(b), "2 passed") || !strings.Contains(string(b), "healed: label=Full name -> #contact-name") {
		t.Fatalf("%v\n%s", err, b)
	}
	// a check that fails: its step has the page as an attachment (FAIL_SHOTS on by default)
	cmd = exec.Command("npx", "playwright", "test", "--reporter=json", "tests/crm/fail.spec.ts")
	cmd.Dir = out
	cmd.Env = append(os.Environ(), "TW_PASSWORD_CRM=pw1")
	b, _ = cmd.Output()
	if !strings.Contains(string(b), `"name": "step 1 - web on Apps/CRM - failed"`) || !strings.Contains(string(b), `"contentType": "image/png"`) || !strings.Contains(string(b), `"status": "failed"`) {
		t.Fatalf("no screenshot of the failed step:\n%.3000s", b)
	}
	cmd = exec.Command("npx", "playwright", "test", "--reporter=json", "tests/crm/fail.spec.ts")
	cmd.Dir = out
	cmd.Env = append(os.Environ(), "TW_PASSWORD_CRM=pw1", "TW_FAIL_SHOTS=0")
	if b, _ = cmd.Output(); strings.Contains(string(b), "- failed\"") {
		t.Fatal("TW_FAIL_SHOTS=0 still attaches the failure screenshot")
	}
	t.Log(string(b))
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestProcKinds(t *testing.T) {
	d := t.TempDir()
	f := Folder{Root: d}
	os.MkdirAll(filepath.Join(d, "cases"), 0o755)
	if _, err := f.SaveWebBlock("open-cp-gvi", "", []WebAction{{Do: "click", Target: "text={{profile}}"}, {Do: "click", Target: "text=Global Value Inheritance"}}); err != nil {
		t.Fatal(err)
	}
	f.SaveProc("create-vlan", Proc{Params: []string{"vlan"}, Steps: []Step{{Run: &RunStep{Session: "{{sw}}", Command: "create vlan {{vlan}}"}}}})
	os.WriteFile(filepath.Join(d, "cases", "a.yaml"), []byte("name: A\nsteps:\n  - web: {login: Apps/X, actions: [{do: block, target: open-cp-gvi, with: {profile: P}}]}\n  - call: {proc: create-vlan, with: {vlan: '5'}}\n"), 0o644)
	os.WriteFile(filepath.Join(d, "cases", "b.yaml"), []byte("name: B\nsteps:\n  - web: {login: Apps/X, actions: [{do: block, target: open-cp-gvi}, {do: block, target: open-cp-gvi}]}\n"), 0o644)
	got := map[string]ProcInfo{}
	for _, p := range f.Procs() {
		got[p.Name] = p
	}
	if w := got["open-cp-gvi"]; w.Kind != "web" || w.Actions != 2 || w.UsedBy != 2 || len(w.Params) != 1 {
		t.Fatalf("%+v", w)
	}
	if c := got["create-vlan"]; c.Kind != "device" || c.Steps != 1 || c.UsedBy != 1 {
		t.Fatalf("%+v", c)
	}
}
