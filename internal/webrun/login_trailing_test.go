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

func TestLoginTrailingClick(t *testing.T) {
	if os.Getenv("TW_PLAYWRIGHT") == "" {
		t.Skip("TW_PLAYWRIGHT not set")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	os.Symlink(os.Getenv("TW_PLAYWRIGHT"), filepath.Join(dir, "node_modules", "playwright"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sid"); err != nil || c.Value != "ok" {
			fmt.Fprint(w, `<html><body><label>User <input id="u"></label><label>Password <input type="password" id="p"></label><button onclick="if(document.getElementById('p').value==='pw1'){document.cookie='sid=ok;path=/';location.reload()}">Sign in</button></body></html>`)
			return
		}
		fmt.Fprint(w, `<html><title>App</title><body><h1>Home</h1></body></html>`)
	}))
	defer srv.Close()
	app := App{Name: "A", URL: srv.URL, User: "qa", Password: "pw1", Login: []Action{{Do: "fill", Target: "label=User", Value: "{{username}}"}, {Do: "fill", Target: "label=Password", Value: "{{password}}"}, {Do: "click", Target: "text=Sign in"}, {Do: "click", Target: "text=Dashboard"}}}
	w, x := ForApp(Step{}, app, Extra{Root: dir, StateDir: filepath.Join(dir, "state")})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := Runner{Dir: dir}.Run(ctx, w, "", 5, x)
	if err != nil || !strings.Contains(out, "logged in") || !strings.Contains(out, "skipped: click text=Dashboard") {
		t.Fatal(err, out)
	}
	// a failure before the password is sent still fails
	app.Login = []Action{{Do: "fill", Target: "label=User", Value: "{{username}}"}, {Do: "click", Target: "text=Next"}, {Do: "fill", Target: "label=Password", Value: "{{password}}"}}
	w, x = ForApp(Step{}, app, Extra{Root: dir, StateDir: filepath.Join(dir, "state2")})
	if out, err := (Runner{Dir: dir}).Run(ctx, w, "", 5, x); err == nil && !strings.Contains(out, "click text=Next") {
		t.Fatal("no failure", out)
	}
}
