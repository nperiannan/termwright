package app

import (
	"context"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

// restore: config needs the device type's save and restore commands; lab needs an emulated lab switch;
// the generated tests' save_state / restore_state go through a token.
func TestTestsSaveState(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	s, _ := st.Save(session.Session{Name: "sw", Folder: "Lab", Protocol: session.ProtoSSH2, Hostname: "10.0.0.9", DeviceType: "EXOS"}, "")
	res, back := a.testsSaveState(context.Background(), []string{"config", "lab"}, []string{s.Path})
	if len(res) != 2 || res[0].Passed || !strings.Contains(res[0].Error, "no save and restore commands") || res[1].Passed || !strings.Contains(res[1].Error, "emulated lab") {
		t.Fatalf("%+v", res)
	}
	if r := back(context.Background()); len(r) != 0 {
		t.Fatalf("nothing to restore: %+v", r)
	}
	out, err := a.testsStateTool(context.Background(), "save_state", map[string]any{"kinds": []any{"lab"}, "sessions": []any{s.Path}})
	if err != nil || !strings.Contains(out, `"token"`) {
		t.Fatal(out, err)
	}
	if _, err := a.testsStateTool(context.Background(), "restore_state", map[string]any{"token": "nope"}); err == nil {
		t.Fatal("unknown token")
	}
}
