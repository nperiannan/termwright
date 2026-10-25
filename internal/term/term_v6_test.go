package term

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSCPFolder(t *testing.T) {
	if _, err := exec.LookPath("scp"); err != nil {
		t.Skip("scp not installed")
	}
	m, em, tab := connectedTab(t)
	defer m.CloseAll()
	ch := xferChan(em)
	local, remote, back := t.TempDir(), t.TempDir(), t.TempDir()
	src := filepath.Join(local, "tree")
	os.MkdirAll(filepath.Join(src, "sub", "deep"), 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("alpha"), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "deep", "b.bin"), bytes.Repeat([]byte{7}, 70000), 0o644)
	m.SCPTransfer(tab, "up", src, remote)
	if tr := waitXfer(t, ch); tr.State != "done" || tr.FilesDone != 2 {
		t.Fatalf("scp folder up %+v", tr)
	}
	m.SCPTransfer(tab, "down", filepath.Join(remote, "tree"), back)
	if tr := waitXfer(t, ch); tr.State != "done" || tr.FilesDone != 2 {
		t.Fatalf("scp folder down %+v", tr)
	}
	if got, _ := os.ReadFile(filepath.Join(back, "tree", "sub", "deep", "b.bin")); len(got) != 70000 {
		t.Fatalf("deep file size %d", len(got))
	}
	if got, _ := os.ReadFile(filepath.Join(back, "tree", "a.txt")); string(got) != "alpha" {
		t.Fatalf("a.txt = %q", got)
	}
}

func TestSFTPResume(t *testing.T) {
	m, em, tab := connectedTab(t)
	defer m.CloseAll()
	ch := xferChan(em)
	id, _, err := m.SFTPOpen(tab, "")
	if err != nil {
		t.Fatal(err)
	}
	local, remote := t.TempDir(), t.TempDir()
	data := bytes.Repeat([]byte("0123456789"), 30000)
	os.WriteFile(filepath.Join(local, "big.bin"), data, 0o644)
	os.WriteFile(filepath.Join(local, "done.txt"), []byte("same"), 0o644)
	// Simulate an interrupted copy: a partial file and a complete one.
	os.WriteFile(filepath.Join(remote, "big.bin"), data[:123457], 0o644)
	os.WriteFile(filepath.Join(remote, "done.txt"), []byte("same"), 0o644)
	m.mu.Lock()
	m.jobs["old"] = xferJob{sftpID: id, direction: "up", dest: remote,
		sources: []string{filepath.Join(local, "big.bin"), filepath.Join(local, "done.txt")}}
	m.mu.Unlock()
	if _, err := m.ResumeTransfer("old"); err != nil {
		t.Fatal(err)
	}
	if tr := waitXfer(t, ch); tr.State != "done" || tr.FilesDone != 2 || tr.Bytes != int64(len(data))+4 {
		t.Fatalf("resume %+v", tr)
	}
	if got, _ := os.ReadFile(filepath.Join(remote, "big.bin")); !bytes.Equal(got, data) {
		t.Fatalf("resumed file differs (len %d)", len(got))
	}
	// Download resume.
	back := t.TempDir()
	os.WriteFile(filepath.Join(back, "big.bin"), data[:1000], 0o644)
	m.mu.Lock()
	m.jobs["old2"] = xferJob{sftpID: id, direction: "down", dest: back, sources: []string{filepath.Join(remote, "big.bin")}}
	m.mu.Unlock()
	m.ResumeTransfer("old2")
	if tr := waitXfer(t, ch); tr.State != "done" {
		t.Fatalf("resume down %+v", tr)
	}
	if got, _ := os.ReadFile(filepath.Join(back, "big.bin")); !bytes.Equal(got, data) {
		t.Fatalf("resumed download differs (len %d)", len(got))
	}
}

func TestSFTPConnectStandalone(t *testing.T) {
	addr := startSSHServer(t, "pw")
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	autoAnswer(m, em, "pw")
	defer m.CloseAll()
	id, home, info, err := m.SFTPConnect(sshSession(addr), "")
	if err != nil || home == "" || !strings.HasPrefix(info, "SFTP ") {
		t.Fatal(err, home, info)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o644)
	ents, err := m.SFTPList(id, dir)
	if err != nil || len(ents) != 1 || ents[0].Name != "x.txt" {
		t.Fatal(err, ents)
	}
	m.SFTPClose(id)
	if _, err := m.SFTPList(id, dir); err == nil {
		t.Fatal("session still open after close")
	}
}

func TestSiteToSiteCopy(t *testing.T) {
	m, em, tab := connectedTab(t)
	defer m.CloseAll()
	ch := xferChan(em)
	a, _, err := m.SFTPOpen(tab, "")
	if err != nil {
		t.Fatal(err)
	}
	b, _, _, err := m.SFTPConnect(sshSession(startSSHServer(t, "pw")), "")
	if err != nil {
		t.Fatal(err)
	}
	srcRoot, dst := t.TempDir(), t.TempDir()
	src := filepath.Join(srcRoot, "cfg")
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "sub", "r.txt"), bytes.Repeat([]byte("z"), 99999), 0o644)
	os.WriteFile(filepath.Join(src, "top.txt"), []byte("t"), 0o644)
	if _, err := m.StartRemoteCopy(a, b, []string{src}, dst); err != nil {
		t.Fatal(err)
	}
	if tr := waitXfer(t, ch); tr.State != "done" || tr.FilesDone != 2 || tr.Bytes != 100000 {
		t.Fatalf("%+v", tr)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "cfg", "sub", "r.txt")); len(got) != 99999 {
		t.Fatalf("copied size %d", len(got))
	}
	if _, err := m.StartRemoteCopy(a, a, []string{src}, dst); err == nil {
		t.Fatal("same-session copy should be refused")
	}
}

func TestSCPOverSFTPSession(t *testing.T) {
	if _, err := exec.LookPath("scp"); err != nil {
		t.Skip("scp not installed")
	}
	addr := startSSHServer(t, "pw")
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	autoAnswer(m, em, "pw")
	defer m.CloseAll()
	ch := xferChan(em)
	id, _, _, err := m.SFTPConnect(sshSession(addr), "")
	if err != nil {
		t.Fatal(err)
	}
	src, dst := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, "f.txt"), []byte("scp via sftp tab"), 0o644)
	if _, err := m.SCPTransferSFTP(id, "up", filepath.Join(src, "f.txt"), dst); err != nil {
		t.Fatal(err)
	}
	if tr := waitXfer(t, ch); tr.State != "done" {
		t.Fatalf("%+v", tr)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "f.txt")); string(b) != "scp via sftp tab" {
		t.Fatalf("got %q", b)
	}
}
