package main

import (
	"testing"
	"time"
)

func TestWakeDue(t *testing.T) {
	loc := time.Local
	day := func(h, m int) time.Time { return time.Date(2026, 11, 2, h, m, 0, 0, loc) } // a Monday
	never := time.Time{}
	if wakeDue("daily", "08:00", never, day(7, 59)) {
		t.Fatal("before the time")
	}
	if !wakeDue("daily", "08:00", never, day(8, 0)) || !wakeDue("weekdays", "08:00", never, day(8, 10)) {
		t.Fatal("at the time")
	}
	if wakeDue("daily", "08:00", never, day(8, 30)) {
		t.Fatal("too late: the slot is skipped")
	}
	if wakeDue("daily", "08:00", day(8, 0), day(8, 1)) {
		t.Fatal("twice in one slot")
	}
	sat := time.Date(2026, 11, 7, 8, 0, 0, 0, loc)
	if wakeDue("weekdays", "08:00", never, sat) || !wakeDue("daily", "08:00", never, sat) {
		t.Fatal("weekend")
	}
}
