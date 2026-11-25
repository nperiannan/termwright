package logging

import (
	"os"
	"path/filepath"
	"strings"
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

func TestStripUnendedString(t *testing.T) {
	var s Stripper
	// a string sequence the device never ends: the commands after it are kept
	got := string(s.Strip([]byte("\x1bPgarbage\r\n# show vlan\r\nDefault 1\r\n\x1b]0;title\x1b[0m# show ports\r\nport 1:1 \xc4\x9b ok\r\n")))
	for _, want := range []string{"# show vlan", "Default 1", "# show ports", "port 1:1 \xc4\x9b ok"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %q", want, got)
		}
	}
	if strings.Contains(got, "garbage") || strings.Contains(got, "title") {
		t.Errorf("sequence text kept: %q", got)
	}
}
