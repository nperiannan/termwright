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

	"github.com/termwright/termwright/internal/testrec"
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
	w := testrec.WebStep{URL: srv.URL, Actions: []testrec.WebAction{
		{Do: "fill", Target: "#u", Value: "admin"}, {Do: "fill", Target: "#password", Value: "x"},
		{Do: "click", Target: "text=Sign in"}, {Do: "expect", Value: "Welcome admin"}, {Do: "expect_not", Target: "#u"},
	}}
	out, err := r.Run(context.Background(), w, filepath.Join(dir, "shots"), 10)
	if err != nil || !strings.Contains(out, "Welcome admin") || strings.Contains(out, `"x"`) {
		t.Fatalf("%v\n%s", err, out)
	}
	w.Actions = append(w.Actions, testrec.WebAction{Do: "expect", Value: "Not there"})
	out, err = r.Run(context.Background(), w, filepath.Join(dir, "shots"), 2)
	if err == nil || !strings.Contains(out, "screenshot: ") {
		t.Fatalf("no failure: %s", out)
	}
	t.Log(out)
}
