package term

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestShellSFTP serves files from a shell (here this computer's sh, as a switch's "!sh" would answer) to an SFTP
// client: list, stat, read, write, mkdir, rename and remove.
func TestShellSFTP(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "agent.log"), []byte("line 1\nline 2 \xc4\x9b\n"), 0o644)
	os.Mkdir(filepath.Join(d, "certs"), 0o755)
	os.WriteFile(filepath.Join(d, ".hidden"), []byte("x"), 0o600)
	fs := &shellFS{exec: func(cmd string) (string, error) {
		b, _ := exec.Command("sh", "-c", cmd).CombinedOutput()
		return string(b), nil
	}}
	c, stop, err := fs.serve(d)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	l, err := c.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]os.FileInfo{}
	for _, f := range l {
		got[f.Name()] = f
	}
	if len(got) != 3 || !got["certs"].IsDir() || got["agent.log"].Size() != 17 || got[".hidden"].Mode().Perm() != 0o600 {
		t.Fatalf("%v", got)
	}
	f, err := c.Open(filepath.Join(d, "agent.log"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "line 1\nline 2 \xc4\x9b\n" {
		t.Fatalf("%q", b)
	}
	w, err := c.Create(filepath.Join(d, "new.conf"))
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("vlan 10\n", 2000)
	w.Write([]byte(big))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(d, "new.conf")); string(b) != big {
		t.Fatalf("written %d bytes", len(b))
	}
	if err := c.Mkdir(filepath.Join(d, "tmp")); err != nil {
		t.Fatal(err)
	}
	if err := c.Rename(filepath.Join(d, "new.conf"), filepath.Join(d, "tmp", "n.conf")); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(filepath.Join(d, "tmp", "n.conf")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "tmp", "n.conf")); !os.IsNotExist(err) {
		t.Fatal("not removed")
	}
	if _, err := c.ReadDir(filepath.Join(d, "nope")); err == nil {
		t.Fatal("a missing folder")
	}
	// ls -la, when there is no stat -c
	if l := parseLs("drwxr-xr-x    2 root     root          4096 Nov 25 10:00 certs\n-rw-r--r--    1 root     root         48210 Jan  3  2025 agent.log\n"); len(l) != 2 || !l[0].IsDir() || l[1].Size() != 48210 || l[1].Mode().Perm() != 0o644 {
		t.Fatalf("%+v", l)
	}
}
