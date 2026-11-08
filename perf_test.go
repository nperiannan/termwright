package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

// TestPerfSessions measures the session store with a large tree (TW_PERF=N
// sessions, e.g. 20000). It only runs when asked:
//
//	TW_PERF=20000 go test -run TestPerfSessions -v .
func TestPerfSessions(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("TW_PERF"))
	if n == 0 {
		t.Skip("set TW_PERF to the number of sessions")
	}
	dir := t.TempDir()
	st, err := session.NewStore(filepath.Join(dir, "Sessions"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(dir)
	a := &App{store: st, cfg: cfg}
	types := []string{"VOSS", "EXOS", "Cisco IOS", "FortiGate", "Linux", ""}
	t0 := time.Now()
	for i := 0; i < n; i++ {
		s := session.Session{
			Name: fmt.Sprintf("sw-%05d", i), Folder: fmt.Sprintf("Site %02d/Building %d/Floor %d", i%40, i/40%5, i/200%4),
			Protocol: session.ProtoSSH2, Hostname: fmt.Sprintf("10.%d.%d.%d", i/65536%256, i/256%256, i%256), Port: 22,
			Username: "admin", DeviceType: types[i%len(types)], Description: []string{"rack " + strconv.Itoa(i%42)},
		}
		if _, err := st.Save(s, ""); err != nil {
			t.Fatal(err)
		}
	}
	report := func(what string, d time.Duration, extra string) {
		t.Logf("%-34s %8.0f ms %s", what, float64(d.Microseconds())/1000, extra)
	}
	report(fmt.Sprintf("create %d sessions", n), time.Since(t0), "")

	t0 = time.Now()
	tree, err := st.Tree()
	if err != nil {
		t.Fatal(err)
	}
	report("Tree() (Session Manager load)", time.Since(t0), "")
	t0 = time.Now()
	b, _ := json.Marshal(tree)
	report("tree JSON for the UI", time.Since(t0), fmt.Sprintf("%.1f MB", float64(len(b))/1e6))
	t0 = time.Now()
	_, _ = st.Tree()
	report("Tree() again (warm cache)", time.Since(t0), "")

	t0 = time.Now()
	paths, _ := st.AllSessions(nil)
	report("AllSessions", time.Since(t0), fmt.Sprintf("%d", len(paths)))
	t0 = time.Now()
	if _, err := st.Get(paths[n/2]); err != nil {
		t.Fatal(err)
	}
	report("Get one session", time.Since(t0), "")
	s, _ := st.Get(paths[n/2])
	s.Hostname = "10.99.99.99"
	t0 = time.Now()
	_, _ = st.Save(s, s.Path)
	report("Save one session", time.Since(t0), "")

	s2 := cfg.Get()
	s2.SyncFolder = t.TempDir()
	_ = cfg.Save(s2)
	t0 = time.Now()
	a.syncOnce()
	report("sync: first run (export)", time.Since(t0), a.SyncState().Error)
	t0 = time.Now()
	a.syncOnce()
	report("sync: idle run (every 30 s)", time.Since(t0), a.SyncState().Error)
	t0 = time.Now()
	_, _ = a.ExportSessions(nil, filepath.Join(t.TempDir(), "all.zip"), "zip", true)
	report("export all to zip", time.Since(t0), "")
}
