package app

import (
	"context"
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

func TestTestSchedules(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	f, _ := a.testsFolder()
	os.WriteFile(filepath.Join(f.Root, "cases", "a.yaml"), []byte("steps:\n  - http: {url: \""+srv.URL+"\"}\n    expect: [{contains: nope}]\n"), 0o644)
	if _, err := a.TestSaveSchedule(testrec.Schedule{Repeat: "daily", At: "25:00"}); err == nil {
		t.Fatal("bad time saved")
	}
	s, err := a.TestSaveSchedule(testrec.Schedule{Name: "nightly", Repeat: "daily", At: "02:15"})
	if err != nil || s.ID == "" {
		t.Fatal(s, err)
	}
	if !a.runSchedule(context.Background(), s) {
		t.Fatal("did not run")
	}
	l := a.TestSchedules()
	if len(l) != 1 || l[0].State.Failed != 1 || l[0].When != "daily 02:15" || l[0].Next == "" {
		t.Fatalf("%+v", l)
	}
	yes := func(string, string, bool) error { return nil }
	if out, _ := a.testsTool(context.Background(), map[string]any{"action": "schedules"}, yes); !strings.Contains(out, `"nightly"  daily 02:15`) {
		t.Fatal(out)
	}
	if out, err := a.testsTool(context.Background(), map[string]any{"action": "schedule", "name": "4h", "repeat": "hours", "hours": 4.0}, yes); err != nil || !strings.Contains(out, "every 4 hours") {
		t.Fatal(out, err)
	}
	if err := a.TestRemoveSchedule(s.ID); err != nil || len(a.TestSchedules()) != 1 {
		t.Fatal(err)
	}
}
