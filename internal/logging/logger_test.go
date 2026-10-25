package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExpand(t *testing.T) {
	tm := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	got := Expand("%H_%S_%Y%M%D-%h%m%s%%", Vars{Host: "h1", Session: "Lab/sw"}, tm)
	if got != "h1_Lab_sw_20260304-050607%" {
		t.Fatal(got)
	}
}

func TestStripSplitSequences(t *testing.T) {
	var s Stripper
	out := string(s.Strip([]byte("a\x1b[3"))) + string(s.Strip([]byte("1mb\x1b]0;title\x07c\r\n")))
	if out != "abc\r\n" {
		t.Fatalf("%q", out)
	}
}

func TestLoggerPrefix(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	l, err := Open(Options{Path: p, LinePrefix: "> "})
	if err != nil {
		t.Fatal(err)
	}
	l.Write([]byte("one\r\ntw"))
	l.Write([]byte("o\r\n"))
	l.Close()
	b, _ := os.ReadFile(p)
	if string(b) != "> one\n> two\n" {
		t.Fatalf("%q", b)
	}
}
