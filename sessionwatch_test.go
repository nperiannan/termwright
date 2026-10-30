package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFolderSignature(t *testing.T) {
	dir := t.TempDir()
	a := folderSignature(dir)
	if a == 0 || a != folderSignature(dir) {
		t.Fatal("the signature of an unchanged folder must be stable and not 0")
	}
	f := filepath.Join(dir, "Lab", "sw1.json")
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(`{"name":"sw1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b := folderSignature(dir)
	if b == a {
		t.Fatal("a new session did not change the signature")
	}
	// a file rewritten in place with the same size still changes its time
	if err := os.WriteFile(f, []byte(`{"name":"sw2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(f, later, later)
	if folderSignature(dir) == b {
		t.Fatal("an edited session did not change the signature")
	}
}
