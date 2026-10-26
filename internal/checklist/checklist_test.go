package checklist

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBeforeAfter(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	t0 := time.Date(2026, 10, 27, 22, 0, 0, 0, time.Local)
	r, err := s.NewRun("Core / upgrade", "CHG-1234", t0)
	if err != nil {
		t.Fatal(err)
	}
	b := Format([]string{"show isis adjacency"}, []string{"1/1 UP 10:22:01\n"})
	if _, err := s.Record("Core / upgrade", r.ID, "Lab/sw1", Before, b, nil, "", t0); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Record("Core / upgrade", r.ID, "Lab/sw2", Before, b, nil, "", t0)
	// same except the time of day: not changed
	d, _ := s.Record("Core / upgrade", r.ID, "Lab/sw1", After, strings.Replace(b, "10:22:01", "23:40:12", 1), nil, "", t0.Add(time.Hour))
	if d.Changed == nil || *d.Changed {
		t.Fatalf("sw1 %+v", d)
	}
	d, _ = s.Record("Core / upgrade", r.ID, "Lab/sw2", After, strings.Replace(b, "UP", "DOWN", 1), nil, "", t0.Add(time.Hour))
	if d.Changed == nil || !*d.Changed {
		t.Fatalf("sw2 %+v", d)
	}
	d, _ = s.Record("Core / upgrade", r.ID, "Lab/sw3", After, "", errors.New("no saved password"), "Login:", t0)
	if d.After.OK || d.After.Log == "" || d.Changed != nil {
		t.Fatalf("sw3 %+v", d.After)
	}
	runs, _ := s.Runs("Core / upgrade")
	if len(runs) != 1 || len(runs[0].Devices) != 3 || runs[0].Note != "CHG-1234" {
		t.Fatalf("%+v", runs)
	}
	if _, err := s.Record("Core / upgrade", r.ID, "../x", Before, "x", nil, "", t0); err == nil {
		t.Fatal("bad path accepted")
	}
}

func TestFormat(t *testing.T) {
	got := Format([]string{"a", "b"}, []string{"1\n", "2"})
	if got != "### a\n1\n\n### b\n2\n\n" {
		t.Fatalf("%q", got)
	}
}
