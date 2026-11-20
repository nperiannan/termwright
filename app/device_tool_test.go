package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

// The device tool lists types with their Detect patterns, gives a session's stored facts and sets its
// device type only when the ask is approved.
func TestDeviceTool(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	s, err := st.Save(session.Session{Name: "edge-sw3", Folder: "Lab", Protocol: session.ProtoSSH2, Hostname: "10.0.0.3"}, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetFacts(s.Path, map[string]string{"Model": "S5735-L24P4S-A", "Version": "5.170"})
	ctx := context.Background()
	yes := func(string, string, bool) error { return nil }
	no := func(string, string, bool) error { return errors.New("refused") }
	if out, err := a.deviceTool(ctx, map[string]any{"action": "types"}, yes); err != nil || !strings.Contains(out, "Huawei VRP  detect (?i)huawei") {
		t.Fatal(err, out)
	}
	if out, _ := a.deviceTool(ctx, map[string]any{"action": "facts", "session": s.Path}, yes); !strings.Contains(out, "Model: S5735-L24P4S-A") {
		t.Fatal(out)
	}
	if _, err := a.deviceTool(ctx, map[string]any{"action": "set_type", "session": s.Path, "type": "huawei vrp"}, no); err == nil {
		t.Fatal("set without asking")
	}
	if out, err := a.deviceTool(ctx, map[string]any{"action": "set_type", "session": s.Path, "type": "huawei vrp"}, yes); err != nil || !strings.Contains(out, "now Huawei VRP") {
		t.Fatal(err, out)
	}
	if got, _ := st.Get(s.Path); got.DeviceType != "Huawei VRP" {
		t.Fatal(got.DeviceType)
	}
	if _, err := a.deviceTool(ctx, map[string]any{"action": "set_type", "session": s.Path, "type": "Nope"}, yes); err == nil {
		t.Fatal("unknown type")
	}
	if !netWrites("device", map[string]any{"action": "set_type"}) || netWrites("device", map[string]any{"action": "identify"}) {
		t.Fatal("netWrites")
	}
}
