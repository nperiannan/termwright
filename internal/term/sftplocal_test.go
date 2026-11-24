package term

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalSFTP(t *testing.T) {
	c, stop, err := localSFTP()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "a.json"), []byte(`{"a":1}`), 0o644)
	os.Mkdir(filepath.Join(d, "sub"), 0o755)
	root := LocalSFTPPath(d)
	l, err := c.ReadDir(root)
	if err != nil || len(l) != 2 {
		t.Fatal(err, l)
	}
	f, err := c.Create(root + "/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("hello"))
	f.Close()
	if b, _ := os.ReadFile(filepath.Join(d, "b.txt")); string(b) != "hello" {
		t.Fatalf("%q", b)
	}
	r, _ := c.Open(root + "/a.json")
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != `{"a":1}` {
		t.Fatal(string(b))
	}
	if err := c.Rename(root+"/b.txt", root+"/sub/c.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(root + "/sub/c.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "sub", "c.txt")); !os.IsNotExist(err) {
		t.Fatal("not removed")
	}
	if LocalPathOf(root) != d {
		t.Fatal(LocalPathOf(root))
	}
}
