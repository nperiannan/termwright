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
	"time"
)

// a sign-in page built of web components (shadow DOM) like many single sign-on pages: fields with labels
// inside the component, an id made anew on every load, and the component's class as the only stable name
const shadowLogin = `<html><body><script>
customElements.define('en-text-field', class extends HTMLElement { connectedCallback() { const r = this.attachShadow({ mode: 'open' }); const id = Math.random().toString(36).slice(2) + 'AbCdEfGh1';
  r.innerHTML = '<label for="' + id + '">' + this.getAttribute('label') + '</label><input id="' + id + '" type="' + (this.getAttribute('type') || 'text') + '">'; this.input = r.querySelector('input'); } });
customElements.define('en-button', class extends HTMLElement { connectedCallback() { this.attachShadow({ mode: 'open' }).innerHTML = '<button type="submit"><slot></slot></button>'; } });
</script>
<en-text-field class="ng-pristine cs-auto-login-email-txt" label="Email"></en-text-field>
<en-text-field class="ng-pristine cs-auto-login-password-txt" label="Password" type="password"></en-text-field>
<en-button class="cs-auto-login-login-btn" onclick="if(document.querySelector('.cs-auto-login-password-txt').input.value==='pw1'&&document.querySelector('.cs-auto-login-email-txt').input.value==='qa@x.com'){document.cookie='sid=ok;path=/';location.reload()}">Log In</en-button>
</body></html>`

func TestShadowLogin(t *testing.T) {
	pw := os.Getenv("TW_PLAYWRIGHT")
	if pw == "" {
		t.Skip("TW_PLAYWRIGHT not set")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	os.Symlink(pw, filepath.Join(dir, "node_modules", "playwright"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sid"); err == nil && c.Value == "ok" {
			fmt.Fprint(w, `<html><title>App</title><body><h1>Dashboard</h1></body></html>`)
			return
		}
		fmt.Fprint(w, shadowLogin)
	}))
	defer srv.Close()
	r := Runner{Dir: dir}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	acts, err := r.Record(ctx, Step{URL: srv.URL}, func(Action) {}, Extra{Root: dir}, []Drive{
		{Do: "fill", Target: "label=Email", Value: "qa@x.com"},
		{Do: "fill", Target: "label=Password", Value: "pw1"},
		{Do: "click", Target: "text=Log In"},
	}...)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range acts {
		got = append(got, a.Do+" "+a.Target+" "+a.Value+" "+strings.Join(a.Alt, ","))
	}
	s := strings.Join(got, "\n")
	if !strings.Contains(s, "fill label=Email qa@x.com") || !strings.Contains(s, "fill label=Password {{password}}") || !strings.Contains(s, "en-text-field.cs-auto-login-email-txt input") || strings.Contains(s, "AbCdEfGh1") {
		t.Fatalf("recorded:\n%s", s)
	}
	// and the recording logs in
	login := []Action{}
	for _, a := range acts {
		if a.Value == "qa@x.com" {
			a.Value = "{{username}}"
		}
		login = append(login, a)
	}
	w, x := ForApp(Step{}, App{Name: "SSO", URL: srv.URL, User: "qa@x.com", Password: "pw1", Login: login}, Extra{Root: dir, StateDir: filepath.Join(dir, "state")})
	out, err := r.Run(ctx, w, "", 10, x)
	if err != nil || !strings.Contains(out, "logged in") || !strings.Contains(out, "Dashboard") {
		t.Fatalf("%v\n%s", err, out)
	}
}
