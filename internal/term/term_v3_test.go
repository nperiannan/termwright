package term

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func connectedTab(t *testing.T) (*Manager, *recEmitter, string) {
	t.Helper()
	addr := startSSHServer(t, "pw")
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	autoAnswer(m, em, "pw")
	ti := m.Create(sshSession(addr))
	m.Start(ti.ID, 80, 24)
	waitFor(t, func() bool { return strings.Contains(em.output(), "router>") })
	return m, em, ti.ID
}

func TestSFTPBrowseAndTransfer(t *testing.T) {
	m, _, tab := connectedTab(t)
	defer m.CloseAll()
	id, home, err := m.SFTPOpen(tab, "")
	if err != nil || home == "" {
		t.Fatal(err, home)
	}
	local := t.TempDir()
	os.MkdirAll(filepath.Join(local, "up", "sub"), 0o755)
	os.WriteFile(filepath.Join(local, "up", "a.txt"), []byte("alpha"), 0o644)
	os.WriteFile(filepath.Join(local, "up", "sub", "b.bin"), make([]byte, 300000), 0o644)
	remote := t.TempDir() // the test SFTP server serves the local filesystem
	done := make(chan Transfer, 4)
	em := m.emit.(*recEmitter)
	em.mu.Lock()
	em.onXfer = func(tr Transfer) {
		if tr.State != "running" {
			done <- tr
		}
	}
	em.mu.Unlock()
	if _, err := m.StartTransfer(id, "up", []string{filepath.Join(local, "up")}, remote); err != nil {
		t.Fatal(err)
	}
	tr := <-done
	if tr.State != "done" || tr.FilesDone != 2 || tr.Bytes != 300005 {
		t.Fatalf("upload %+v", tr)
	}
	list, err := m.SFTPList(id, filepath.Join(remote, "up"))
	if err != nil || len(list) != 2 || !list[0].IsDir || list[0].Name != "sub" {
		t.Fatalf("list %v %+v", err, list)
	}
	back := t.TempDir()
	m.StartTransfer(id, "down", []string{filepath.Join(remote, "up")}, back)
	if tr := <-done; tr.State != "done" {
		t.Fatalf("download %+v", tr)
	}
	if b, _ := os.ReadFile(filepath.Join(back, "up", "a.txt")); string(b) != "alpha" {
		t.Fatalf("downloaded %q", b)
	}
	if err := m.SFTPRename(id, filepath.Join(remote, "up", "a.txt"), filepath.Join(remote, "up", "c.txt")); err != nil {
		t.Fatal(err)
	}
	if err := m.SFTPRemove(id, filepath.Join(remote, "up")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(remote, "up")); !os.IsNotExist(err) {
		t.Fatal("remove failed")
	}
}

func TestScriptAndRecorder(t *testing.T) {
	m, em, tab := connectedTab(t)
	defer m.CloseAll()
	status := make(chan map[string]string, 8)
	em.mu.Lock()
	em.onScript = func(s map[string]string) {
		if s["state"] != "running" {
			status <- s
		}
	}
	em.mu.Unlock()
	code := `
term.sendLine("hello");
if (!term.waitFor(/echo:hel+o/, 5000)) throw new Error("no echo");
var i = term.waitForAny(["nope", "router"], 200);
term.sendLine("x" + i);
var before = term.readUntil("x-1", 5000);
log("ok", before !== null);
`
	if _, err := m.RunScript(tab, "t.js", code); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-status:
		if s["state"] != "done" {
			t.Fatalf("script %+v", s)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("script timeout")
	}
	// cancellation of a waiting script
	id, _ := m.RunScript(tab, "w.js", `term.waitFor("never")`)
	time.Sleep(100 * time.Millisecond)
	m.CancelScript(id)
	if s := <-status; s["state"] != "cancelled" {
		t.Fatalf("cancel %+v", s)
	}
	// recorder
	m.StartRecording(tab)
	m.Write(tab, []byte("show vlan\r"))
	waitFor(t, func() bool { return strings.Contains(em.output(), "echo:show vlan") })
	src, err := m.StopRecording(tab)
	if err != nil || !strings.Contains(src, `term.sendLine("show vlan");`) {
		t.Fatalf("recorded %v\n%s", err, src)
	}
}

func TestXmodemSend(t *testing.T) {
	data := make([]byte, 2500)
	for i := range data {
		data[i] = byte(i)
	}
	in := make(chan byte, 16)
	var got []byte
	var sent [][]byte
	write := func(p []byte) error {
		pkt := append([]byte(nil), p...)
		sent = append(sent, pkt)
		go func() {
			if pkt[0] == xEOT {
				in <- xACK
				return
			}
			size := 128
			if pkt[0] == xSTX {
				size = 1024
			}
			chunk := pkt[3 : 3+size]
			c := crc16(chunk)
			if pkt[3+size] != byte(c>>8) || pkt[4+size] != byte(c) || pkt[1] != ^pkt[2] {
				in <- xNAK
				return
			}
			got = append(got, chunk...)
			in <- xACK
		}()
		return nil
	}
	in <- xC
	if err := xmodemSend(context.Background(), write, in, data, 1024, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 4 || string(got[:len(data)]) != string(data) {
		t.Fatalf("blocks %d", len(sent))
	}
	for _, b := range got[len(data):] {
		if b != xSUB {
			t.Fatal("padding")
		}
	}
}
