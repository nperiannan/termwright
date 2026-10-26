package recording

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteRead(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	cur := base
	now := func() time.Time { return cur }
	st := Store{Dir: dir}
	p := st.FileName("Datacenter/Core/core-sw1", base)
	if !strings.HasSuffix(filepath.ToSlash(p), "Datacenter/Core/core-sw1/2026-10-27_090000.cast") {
		t.Fatalf("path %s", p)
	}
	w, err := create(p, Header{Width: 120, Height: 40, Session: "Datacenter/Core/core-sw1", Title: "core-sw1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	cur = base.Add(500 * time.Millisecond)
	w.Output([]byte("hello \xe2\x82")) // € split over two writes
	cur = base.Add(1500 * time.Millisecond)
	w.Output([]byte("\xac done\r\n"))
	cur = base.Add(2 * time.Second)
	w.Resize(100, 30)
	w.Marker("change starts")
	cur = base.Add(3 * time.Second)
	w.Output([]byte{0xff, 'x'})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	rec, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Header.Width != 120 || rec.Header.Session != "Datacenter/Core/core-sw1" || rec.Header.Version != 2 {
		t.Fatalf("header %+v", rec.Header)
	}
	want := []Event{{0.5, "o", "hello "}, {1.5, "o", "€ done\r\n"}, {2, "r", "100x30"}, {2, "m", "change starts"}, {3, "o", "�x"}}
	if len(rec.Events) != len(want) {
		t.Fatalf("events %+v", rec.Events)
	}
	for i, e := range want {
		if rec.Events[i] != e {
			t.Fatalf("event %d: %+v want %+v", i, rec.Events[i], e)
		}
	}
	in, err := Stat(p)
	if err != nil || in.Duration != 3 || in.Markers != 1 || in.Title != "core-sw1" {
		t.Fatalf("stat %+v %v", in, err)
	}
	list, err := st.List()
	if err != nil || len(list) != 1 || list[0].Session != "Datacenter/Core/core-sw1" {
		t.Fatalf("list %+v %v", list, err)
	}
	old := time.Now().AddDate(0, 0, -40)
	_ = os.Chtimes(p, old, old)
	if n := st.Prune(30, time.Now()); n != 1 {
		t.Fatalf("pruned %d", n)
	}
	if list, _ := st.List(); len(list) != 0 {
		t.Fatalf("left %+v", list)
	}
}

func TestFileNameUnsafe(t *testing.T) {
	st := Store{Dir: "/r"}
	for _, s := range []string{"", "../../etc", "/"} {
		if p := st.FileName(s, time.Unix(0, 0)); !strings.Contains(p, "Ad hoc") {
			t.Fatalf("%q -> %s", s, p)
		}
	}
}

func TestReadOtherVersion(t *testing.T) {
	if _, err := Read(strings.NewReader(`{"version":1}` + "\n")); err == nil {
		t.Fatal("v1 accepted")
	}
}
