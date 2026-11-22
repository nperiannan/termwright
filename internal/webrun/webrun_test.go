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
