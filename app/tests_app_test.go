package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/testrec"
)

// The tests tool saves, lists, shows and runs tests; HTTP steps go through the API client; a run on a
// production session is asked; a test with a password is refused.
func TestTestsTool(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"vlans":[{"id":42,"name":"v42"}]}`))
	}))
	defer srv.Close()
	ctx := context.Background()
	yes := func(string, string, bool) error { return nil }
	no := func(string, string, bool) error { return errors.New("refused") }
	y := "name: VLANs\nsteps:\n  - http: {url: \"" + srv.URL + "/vlans\"}\n    expect:\n      - status: 200\n      - {json: \"vlans[0].id\", equals: \"42\"}\n"
	if out, err := a.testsTool(ctx, map[string]any{"action": "save", "file": "api/vlans", "yaml": y}, yes); err != nil || out != "Saved cases/api/vlans.yaml." {
		t.Fatal(out, err)
	}
	if out, _ := a.testsTool(ctx, map[string]any{"action": "list"}, yes); !strings.Contains(out, `cases/api/vlans.yaml  "VLANs"  1 steps`) {
		t.Fatal(out)
	}
	out, err := a.testsTool(ctx, map[string]any{"action": "run"}, yes)
	if err != nil || !strings.HasPrefix(out, "1 passed, 0 failed") || !strings.Contains(out, "PASS VLANs") {
		t.Fatal(out, err)
	}
	f, _ := a.testsFolder()
	if _, err := os.Stat(filepath.Join(f.Root, a.TestLastRun().Dir, "junit.xml")); err != nil {
		t.Fatal(err)
	}
	// production: refused without asking, the binding needs Confirmed
	if _, err := st.Save(session.Session{Name: "core", Folder: "Prod", Protocol: session.ProtoSSH2, Hostname: "10.0.0.1", Environment: "production"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.TestSave("prod", testrec.Case{Steps: []testrec.Step{{Run: &testrec.RunStep{Session: "Prod/core", Command: "show vlan"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.testsTool(ctx, map[string]any{"action": "run", "file": "prod"}, no); err == nil {
		t.Fatal("ran on production without asking")
	}
	if _, err := a.RunTests(TestRunRequest{Files: []string{"prod"}}); !errors.Is(err, errTestsProduction) || !strings.Contains(err.Error(), "Prod/core") {
		t.Fatal(err)
	}
	if _, err := a.TestSave("bad", testrec.Case{Steps: []testrec.Step{{Run: &testrec.RunStep{Session: "Prod/core", Command: "configure snmp community private password Secr3t!"}}}}); err == nil {
		t.Fatal("saved a secret")
	}
}
