package term

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/session"
)

func waitXfer(t *testing.T, ch chan Transfer) Transfer {
	t.Helper()
	select {
	case tr := <-ch:
		return tr
	case <-time.After(20 * time.Second):
		t.Fatal("transfer timeout")
	}
	return Transfer{}
}

func xferChan(em *recEmitter) chan Transfer {
	ch := make(chan Transfer, 8)
	em.mu.Lock()
	em.onXfer = func(tr Transfer) {
		if tr.State != "running" {
			ch <- tr
		}
	}
	em.mu.Unlock()
	return ch
}

func TestSCPUploadDownload(t *testing.T) {
	if _, err := exec.LookPath("scp"); err != nil {
		t.Skip("scp not installed")
	}
	m, em, tab := connectedTab(t)
	defer m.CloseAll()
	ch := xferChan(em)
	local, remote, back := t.TempDir(), t.TempDir(), t.TempDir()
	data := bytes.Repeat([]byte("termwright"), 50000)
	os.WriteFile(filepath.Join(local, "img.bin"), data, 0o644)
	m.SCPTransfer(tab, "up", filepath.Join(local, "img.bin"), remote)
	if tr := waitXfer(t, ch); tr.State != "done" {
		t.Fatalf("scp up %+v", tr)
	}
	m.SCPTransfer(tab, "down", filepath.Join(remote, "img.bin"), back)
	if tr := waitXfer(t, ch); tr.State != "done" {
		t.Fatalf("scp down %+v", tr)
	}
	if got, _ := os.ReadFile(filepath.Join(back, "img.bin")); !bytes.Equal(got, data) {
		t.Fatal("scp content differs")
	}
}

func TestSFTPSync(t *testing.T) {
	m, em, tab := connectedTab(t)
	defer m.CloseAll()
	ch := xferChan(em)
	id, _, err := m.SFTPOpen(tab, "")
	if err != nil {
		t.Fatal(err)
	}
	local, remote := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(local, "d"), 0o755)
	os.WriteFile(filepath.Join(local, "a"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(local, "d", "b"), []byte("22"), 0o644)
	m.StartSync(id, "up", local, remote)
	if tr := waitXfer(t, ch); tr.State != "done" || tr.FilesDone != 2 {
		t.Fatalf("first sync %+v", tr)
	}
	m.StartSync(id, "up", local, remote)
	if tr := waitXfer(t, ch); tr.State != "done" || tr.FilesDone != 0 {
		t.Fatalf("second sync should copy nothing: %+v", tr)
	}
	time.Sleep(1100 * time.Millisecond)
	os.WriteFile(filepath.Join(local, "a"), []byte("333"), 0o644)
	m.StartSync(id, "up", local, remote)
	if tr := waitXfer(t, ch); tr.FilesDone != 1 {
		t.Fatalf("changed file %+v", tr)
	}
	if b, _ := os.ReadFile(filepath.Join(remote, "a")); string(b) != "333" {
		t.Fatalf("remote a=%q", b)
	}
}

// A local shell runs "sz"; Termwright must detect it and receive automatically.
func TestZmodemAutoReceiveInTerminal(t *testing.T) {
	if _, err := exec.LookPath("sz"); err != nil {
		t.Skip("lrzsz not installed")
	}
	src, dl := t.TempDir(), t.TempDir()
	data := randomFile(t, src, "cfg.txt", 40000)
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	m.DownloadDir = func() string { return dl }
	ch := xferChan(em)
	s := session.Defaults()
	s.Protocol = session.ProtoLocal
	s.LocalDir = src
	s.LocalCommand = "sleep 0.5; sz -q cfg.txt; echo SZ-DONE; sleep 2"
	ti := m.Create(s)
	m.Start(ti.ID, 80, 24)
	if tr := waitXfer(t, ch); tr.State != "done" || tr.Direction != "zmodem-down" {
		t.Fatalf("%+v", tr)
	}
	if got, _ := os.ReadFile(filepath.Join(dl, "cfg.txt")); !bytes.Equal(got, data) {
		t.Fatal("received file differs")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(em.output(), "SZ-DONE") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(em.output(), "SZ-DONE") {
		t.Fatalf("output %q", em.output())
	}
	m.CloseAll()
}
