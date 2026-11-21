package testrec

import (
	"testing"
	"time"
)

func TestScheduleDue(t *testing.T) {
	loc := time.UTC
	mon0700 := time.Date(2026, 11, 23, 7, 0, 0, 0, loc) // a Monday
	d := Schedule{Repeat: "daily", At: "06:30"}
	if !d.Due(time.Time{}, mon0700) || d.Due(mon0700.Add(-10*time.Minute), mon0700) || d.Due(time.Time{}, mon0700.Add(-time.Hour)) {
		t.Fatal("daily")
	}
	if n := d.Next(mon0700.Add(-10*time.Minute), mon0700); !n.Equal(time.Date(2026, 11, 24, 6, 30, 0, 0, loc)) {
		t.Fatal("daily next", n)
	}
	w := Schedule{Repeat: "weekly", At: "06:00", Day: 2}
	if w.Due(time.Time{}, mon0700) || w.Next(time.Time{}, mon0700).Weekday() != time.Tuesday || w.Text() != "Tuesdays 06:00" {
		t.Fatal("weekly", w.Next(time.Time{}, mon0700))
	}
	sat := time.Date(2026, 11, 28, 9, 0, 0, 0, loc)
	if (Schedule{Repeat: "weekdays", At: "06:00"}).Due(time.Time{}, sat) {
		t.Fatal("weekdays on Saturday")
	}
	h := Schedule{Repeat: "hours", Hours: 4}
	if !h.Due(time.Time{}, mon0700) || h.Due(mon0700.Add(-3*time.Hour), mon0700) || !h.Due(mon0700.Add(-4*time.Hour), mon0700) {
		t.Fatal("hours")
	}
	if (Schedule{Repeat: "daily", At: "6"}).Check() == "" || (Schedule{Repeat: "daily", At: "06:00", Off: true}).Due(time.Time{}, mon0700) {
		t.Fatal("check/off")
	}
}
