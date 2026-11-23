package webrun

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun drives a real browser when TW_PLAYWRIGHT points at an installed playwright package.
func TestRun(t *testing.T) {
	pw := os.Getenv("TW_PLAYWRIGHT")
	if pw == "" {
		t.Skip("TW_PLAYWRIGHT not set")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	os.Symlink(pw, filepath.Join(dir, "node_modules", "playwright"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><title>Login</title><body><label>User <input id="u"></label><input type="password" id="password"><button onclick="document.body.innerHTML='<h1>Welcome '+document.getElementById('u').value+'</h1>'">Sign in</button></body></html>`)
	}))
	defer srv.Close()
	r := Runner{Dir: dir}
	w := Step{URL: srv.URL, Actions: []Action{
		{Do: "fill", Target: "#u", Value: "admin"}, {Do: "fill", Target: "#password", Value: "x"},
		{Do: "click", Target: "text=Sign in"}, {Do: "expect", Value: "Welcome admin"}, {Do: "expect_not", Target: "#u"},
	}}
	out, err := r.Run(context.Background(), w, filepath.Join(dir, "shots"), 10)
	if err != nil || !strings.Contains(out, "Welcome admin") || strings.Contains(out, `"x"`) {
		t.Fatalf("%v\n%s", err, out)
	}
	w.Actions = append(w.Actions, Action{Do: "expect", Value: "Not there"})
	out, err = r.Run(context.Background(), w, filepath.Join(dir, "shots"), 2)
	if err == nil || !strings.Contains(out, "screenshot: ") {
		t.Fatalf("no failure: %s", out)
	}
	t.Log(out)
}

const formPage = `<html><title>Settings</title><body>
<label>Name <input id="name"></label>
<label for="pw">Password</label><input type="password" id="pw">
<select id="mode"><option value="a">Access</option><option value="t">Trunk</option></select>
<label><input type="checkbox" id="on"> Enabled</label>
<input type="file" id="file">
<button id="save" onclick="if(confirm('Save?')){document.getElementById('msg').textContent='Saved '+document.getElementById('name').value+' '+document.getElementById('mode').value}">Save</button>
<div id="msg"></div>
<ul><li>one</li><li>two</li><li>three</li></ul>
<iframe srcdoc="<button onclick=&quot;this.textContent='Done'&quot;>Go</button>"></iframe>
<a href="/two" target="_blank">Open</a>
</body></html>`

func pwRunner(t *testing.T) (Runner, *httptest.Server, string) {
	pw := os.Getenv("TW_PLAYWRIGHT")
	if pw == "" {
		t.Skip("TW_PLAYWRIGHT not set")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	os.Symlink(pw, filepath.Join(dir, "node_modules", "playwright"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/two" {
			fmt.Fprint(w, `<html><title>Two</title><body><h1>Second tab</h1></body></html>`)
			return
		}
		fmt.Fprint(w, formPage)
	}))
	t.Cleanup(srv.Close)
	return Runner{Dir: dir}, srv, dir
}

// TestActions runs the newer actions: labels, select by label, check, upload, dialogs, frames, tabs, read,
// counts, the URL and title, and a password kept secret.
func TestActions(t *testing.T) {
	r, srv, dir := pwRunner(t)
	os.WriteFile(filepath.Join(dir, "cfg.txt"), []byte("x"), 0o644)
	w := WithLogin(Step{URL: srv.URL, State: "lab", Actions: []Action{
		{Do: "fill", Target: "label=Name", Value: "{{username}}"},
		{Do: "fill", Target: "label=Password", Value: "{{password}}"},
		{Do: "select", Target: "#mode", Value: "Trunk"},
		{Do: "check", Target: "label=Enabled"},
		{Do: "expect_checked", Target: "#on"},
		{Do: "upload", Target: "#file", Value: "cfg.txt"},
		{Do: "dialog", Value: "accept"},
		{Do: "click", Target: "#save"},
		{Do: "expect", Target: "#msg", Value: "Saved admin t"},
		{Do: "expect_value", Target: "#pw", Value: "{{password}}"},
		{Do: "expect_count", Target: "li", Value: "3"},
		{Do: "read", Target: "li >> nth=1", Value: "second"},
		{Do: "click", Target: "frame=iframe >> text=Go"},
		{Do: "expect", Target: "frame=iframe >> button", Value: "Done"},
		{Do: "expect_title", Value: "Settings"},
		{Do: "click", Target: "text=Open"},
		{Do: "tab", Value: "last"},
		{Do: "expect", Value: "Second tab"},
		{Do: "expect_url", Value: "/two"},
		{Do: "close_tab"},
		{Do: "expect_title", Value: "Settings"},
		{Do: "screenshot", Value: "end"},
	}}, "admin", "s3cret")
	out, err := r.Run(context.Background(), w, filepath.Join(dir, "shots"), 15, Extra{Root: dir, StateDir: filepath.Join(dir, "state")})
	if err != nil || !strings.Contains(out, "read second = two") || strings.Contains(out, "s3cret") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "state", "lab.json")); err != nil {
		t.Errorf("state not kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "shots", "end.png")); err != nil {
		t.Errorf("no screenshot: %v", err)
	}
}

// TestRecord drives the page as a person would and checks the actions recorded.
func TestRecord(t *testing.T) {
	r, srv, dir := pwRunner(t)
	var live int
	acts, err := r.Record(context.Background(), Step{URL: srv.URL}, func(Action) { live++ }, Extra{Root: dir}, []Drive{
		{Do: "fill", Target: "#name", Value: "core1"},
		{Do: "fill", Target: "#pw", Value: "x"},
		{Do: "select", Target: "#mode", Value: "Trunk"},
		{Do: "click", Target: "#on"},
		{Do: "altclick", Target: "li >> nth=0"},
	}...)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range acts {
		got = append(got, a.Do+" "+a.Target+" "+a.Value)
	}
	s := strings.Join(got, "\n")
	for _, want := range []string{"fill label=Name core1", "fill label=Password {{password}}", "select #mode Trunk", "check label=Enabled"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q in\n%s", want, s)
		}
	}
	if live != len(acts) {
		t.Errorf("live %d, actions %d", live, len(acts))
	}
	t.Log(s)
}

// TestSession steps through a page: login by the app's recorded login, run, heal, pick and record.
func TestSession(t *testing.T) {
	pw := os.Getenv("TW_PLAYWRIGHT")
	if pw == "" {
		t.Skip("TW_PLAYWRIGHT not set")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	os.Symlink(pw, filepath.Join(dir, "node_modules", "playwright"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sid"); err != nil || c.Value != "ok" {
			fmt.Fprint(w, `<html><title>Login</title><body><label>User <input id="u"></label><label>Password <input type="password" id="p"></label>
<button onclick="if(document.getElementById('p').value==='pw1'){document.cookie='sid=ok;path=/';location.reload()}">Sign in</button></body></html>`)
			return
		}
		fmt.Fprint(w, `<html><title>App</title><body><h1>Contacts</h1><label>Name <input id="contact-name"></label><button id="add" onclick="document.getElementById('out').textContent='Added '+document.getElementById('contact-name').value">Add</button><div id="out"></div></body></html>`)
	}))
	defer srv.Close()
	r := Runner{Dir: dir}
	login := []Action{{Do: "fill", Target: "label=User", Value: "qa"}, {Do: "fill", Target: "label=Password", Value: "pw1", Secret: true}, {Do: "click", Target: "text=Sign in"}}
	x := Extra{Root: dir, StateDir: filepath.Join(dir, "state"), Login: login}
	s, err := r.Open(context.Background(), Step{URL: srv.URL + "/contacts", State: "app"}, filepath.Join(dir, "shots"), x, Drive{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// label=Full name is gone: its fallback #contact-name is used
	res, err := s.Run([]Action{{Do: "fill", Target: "label=Full name", Alt: []string{"#contact-name"}, Value: "Ann"}, {Do: "click", Target: "#add"}, {Do: "expect", Value: "Added Ann"}}, 0)
	if err != nil || len(res) != 3 || !res[2].OK || res[0].Healed != "#contact-name" {
		t.Fatalf("%v %+v", err, res)
	}
	res, _ = s.Run([]Action{{Do: "expect", Value: "Not here"}}, 3)
	if len(res) != 1 || res[0].OK || res[0].I != 3 {
		t.Fatalf("%+v", res)
	}
	var rec []Action
	s.OnRec = func(a Action) { rec = append(rec, a) }
	if err := s.Record(true); err != nil {
		t.Fatal(err)
	}
	s.Drive([]Drive{{Do: "click", Target: "#add"}})
	s.Record(false)
	s.Drive([]Drive{{Do: "click", Target: "#add"}})
	if len(rec) != 1 || rec[0].Do != "click" || rec[0].Target != "#add" {
		t.Fatalf("recorded %+v", rec)
	}
	// the login was kept: a normal run starts logged in
	out, err := r.Run(context.Background(), Step{URL: srv.URL, State: "app", Pictures: "all", Actions: []Action{{Do: "expect", Value: "Contacts"}}}, filepath.Join(dir, "shots"), 10, Extra{StateDir: x.StateDir})
	if err != nil || !strings.Contains(out, "\npicture: ") {
		t.Fatalf("%v\n%s", err, out)
	}
	// pictures: by default only of a failure - the page at the end is kept for the step's checks
	shots := filepath.Join(dir, "shots2")
	out, err = r.Run(context.Background(), Step{URL: srv.URL, State: "app", Actions: []Action{{Do: "expect", Value: "Contacts"}}}, shots, 10, Extra{StateDir: x.StateDir})
	end := ""
	for _, ln := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(ln, EndPicture); ok {
			end = p
		}
	}
	if err != nil || strings.Contains(out, "\npicture: ") || end == "" {
		t.Fatalf("%v\n%s", err, out)
	}
	if f := FailPicture(out, true); !strings.Contains(f, "screenshot: "+end) || strings.Contains(f, EndPicture) {
		t.Fatal(f)
	}
	if f := FailPicture(out, false); strings.Contains(f, end) {
		t.Fatal(f)
	} else if _, err := os.Stat(end); err == nil {
		t.Fatal("the picture of a passed step was kept")
	}
	// none: no picture, not even of a failure
	out, err = r.Run(context.Background(), Step{URL: srv.URL, State: "app", Pictures: "none", Actions: []Action{{Do: "expect", Value: "Not there"}}}, shots, 3, Extra{StateDir: x.StateDir})
	if err == nil || strings.Contains(out, "screenshot: ") || strings.Contains(out, EndPicture) {
		t.Fatalf("%v\n%s", err, out)
	}
	out, err = r.Run(context.Background(), Step{URL: srv.URL, State: "app", Actions: []Action{{Do: "expect", Value: "Not there"}}}, shots, 3, Extra{StateDir: x.StateDir})
	if err == nil || !strings.Contains(out, "screenshot: ") {
		t.Fatalf("a failed action has no screenshot: %v\n%s", err, out)
	}
	// without the state, the app's login runs by itself
	out, err = r.Run(context.Background(), Step{URL: srv.URL, Actions: []Action{{Do: "expect", Value: "Contacts"}}}, filepath.Join(dir, "shots"), 10, Extra{Login: login})
	if err != nil || !strings.Contains(out, "logged in") || strings.Contains(out, "pw1") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestForApp(t *testing.T) {
	w, x := ForApp(Step{URL: "/contacts", Actions: []Action{{Do: "goto", Value: "/x"}, {Do: "fill", Target: "label=P", Value: "{{password}}"}}},
		App{Name: "Apps/CRM", URL: "https://crm.example.com/", Insecure: true, User: "qa", Password: "pw", Login: []Action{{Do: "fill", Value: "{{username}}"}}}, Extra{})
	if w.URL != "https://crm.example.com/contacts" || w.Actions[0].Value != "https://crm.example.com/x" || !w.Actions[1].Secret || w.Actions[1].Value != "pw" ||
		!w.Insecure || w.State != "app-Apps/CRM" || x.Login[0].Value != "qa" {
		t.Fatalf("%+v %+v", w, x)
	}
	if Join("https://a", "https://b/c") != "https://b/c" || Join("https://a", "") != "https://a" {
		t.Fatal("join")
	}
}
