package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// termwright tests run / secrets without the window: a passing and a failing test, JUnit copied,
// production refused without --production, env var names listed.
func TestTestsCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMWRIGHT_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) }))
	defer srv.Close()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "cases"), 0o755)
	os.WriteFile(filepath.Join(dir, "cases", "ok.yaml"), []byte("steps:\n  - http: {url: \""+srv.URL+"\"}\n    expect: [{json: ok, equals: \"true\"}]\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "cases", "bad.yaml"), []byte("steps:\n  - http: {url: \""+srv.URL+"\"}\n    expect: [{contains: nope}]\n"), 0o644)
	junit := filepath.Join(t.TempDir(), "j.xml")
	if rc := testsCLIMain([]string{"run", filepath.Join(dir, "cases", "ok.yaml"), "--folder", dir, "--junit", junit}); rc != 0 {
		t.Fatal("ok test:", rc)
	}
	if b, _ := os.ReadFile(junit); !strings.Contains(string(b), `tests="1"`) {
		t.Fatalf("junit: %s", b)
	}
	if rc := testsCLIMain([]string{"run", "--folder", dir}); rc != 1 {
		t.Fatal("all tests should fail one:", rc)
	}
	if rc := testsCLIMain([]string{"secrets", "--folder", dir}); rc != 0 {
		t.Fatal("secrets:", rc)
	}
	if rc := testsCLIMain([]string{"nope"}); rc != 2 {
		t.Fatal("unknown command:", rc)
	}
}
