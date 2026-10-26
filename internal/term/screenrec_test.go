//go:build !windows

package term

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/recording"
	"github.com/termwright/termwright/internal/session"
)

func TestSessionRecording(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(nopEmitter{}, t.TempDir(), func() string { return t.TempDir() })
	m.RecordDir = func() string { return dir }
	s := session.Defaults()
	s.Protocol = session.ProtoLocal
	s.Path = "Lab/shell"
	s.RecordSession = true
	s.LocalCommand = "printf 'hello\\n'; sleep 0.4; printf 'bye\\n'; sleep 0.3"
	ti := m.Create(s)
	if err := m.Start(ti.ID, 100, 30); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for m.SessionRecording(ti.ID) == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.AddMarker(ti.ID, "mark one"); err != nil {
		t.Fatal(err)
	}
	_ = m.Resize(ti.ID, 120, 40)
	// the shell exits: the recording is closed with the connection
	for m.SessionRecording(ti.ID) != "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	m.CloseAll()
	var files []string
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, ".cast") {
			files = append(files, p)
		}
		return nil
	})
	if len(files) != 1 || !strings.Contains(filepath.ToSlash(files[0]), "/Lab/shell/") {
		t.Fatalf("files %v", files)
	}
	rec, err := recording.Load(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if rec.Header.Width != 100 || rec.Header.Height != 30 || rec.Header.Session != "Lab/shell" {
		t.Fatalf("header %+v", rec.Header)
	}
	var out strings.Builder
	codes := map[string]int{}
	for _, e := range rec.Events {
		codes[e.Code]++
		if e.Code == "o" {
			out.WriteString(e.Data)
		}
	}
	if !strings.Contains(out.String(), "hello") || !strings.Contains(out.String(), "bye") || codes["m"] != 1 || codes["r"] != 1 {
		t.Fatalf("events %+v", rec.Events)
	}
}

func TestCaptureIsNotRecorded(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(nopEmitter{}, t.TempDir(), func() string { return t.TempDir() })
	m.RecordDir = func() string { return dir }
	m.RecordAll = func(session.Session) bool { return true }
	s := session.Defaults()
	s.Protocol = session.ProtoLocal
	s.LocalCommand = "printf 'sw# '; read x; printf 'out\\nsw# '; sleep 1"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = m.Capture(ctx, s, []string{"show x"}, `# $`)
	n := 0
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, ".cast") {
			n++
		}
		return nil
	})
	if n != 0 {
		t.Fatalf("%d recordings of a background run", n)
	}
}
