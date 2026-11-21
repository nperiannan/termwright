package testrec

import (
	"fmt"
	"time"
)

// Schedule runs tests by themselves while Termwright is open.
type Schedule struct {
	// ID: unique, set when saved.
	ID string `json:"id"`
	// Name shown in the list.
	Name string `json:"name"`
	// Files: the tests (none: all of them).
	Files []string `json:"files,omitempty"`
	// Testbed: a file in testbeds/ whose values fill the {{variables}}.
	Testbed string `json:"testbed,omitempty"`
	// Repeat: daily, weekdays, weekly or hours.
	Repeat string `json:"repeat"`
	// At: the time of day (15:04) for daily, weekdays and weekly.
	At string `json:"at,omitempty"`
	// Day: the weekday for weekly (0 Sunday … 6 Saturday).
	Day int `json:"day,omitempty"`
	// Hours: the interval for hours.
	Hours int `json:"hours,omitempty"`
	// Production: the tests may use production sessions (otherwise such a run is refused).
	Production bool `json:"production,omitempty"`
	// Off: kept but not run.
	Off bool `json:"off,omitempty"`
}

// Check says what is wrong with a schedule ("" when it can be saved).
func (s Schedule) Check() string {
	switch s.Repeat {
	case "daily", "weekdays", "weekly":
		if _, err := time.Parse("15:04", s.At); err != nil {
			return "give the time as HH:MM, e.g. 06:30"
		}
		if s.Repeat == "weekly" && (s.Day < 0 || s.Day > 6) {
			return "the weekday is 0 (Sunday) to 6 (Saturday)"
		}
	case "hours":
		if s.Hours < 1 || s.Hours > 24*7 {
			return "every 1 to 168 hours"
		}
	default:
		return "repeat is daily, weekdays, weekly or hours"
	}
	return ""
}

// slot is the last time the schedule should have run at or before now (zero: none yet today / this week).
func (s Schedule) slot(now time.Time) time.Time {
	hm, err := time.ParseInLocation("15:04", s.At, now.Location())
	if err != nil {
		return time.Time{}
	}
	t := time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, now.Location())
	switch s.Repeat {
	case "weekdays":
		if wd := now.Weekday(); wd == time.Saturday || wd == time.Sunday {
			return time.Time{}
		}
	case "weekly":
		if int(now.Weekday()) != s.Day {
			return time.Time{}
		}
	}
	if now.Before(t) {
		return time.Time{}
	}
	return t
}

// Due says whether the schedule should run now, given when it last ran.
func (s Schedule) Due(last, now time.Time) bool {
	if s.Off || s.Check() != "" {
		return false
	}
	if s.Repeat == "hours" {
		return last.IsZero() || now.Sub(last) >= time.Duration(s.Hours)*time.Hour
	}
	sl := s.slot(now)
	return !sl.IsZero() && last.Before(sl)
}

// Next is when the schedule runs next (zero when off).
func (s Schedule) Next(last, now time.Time) time.Time {
	if s.Off || s.Check() != "" {
		return time.Time{}
	}
	if s.Repeat == "hours" {
		if last.IsZero() {
			return now
		}
		return last.Add(time.Duration(s.Hours) * time.Hour)
	}
	for d := 0; d < 8; d++ {
		day := now.AddDate(0, 0, d)
		hm, _ := time.ParseInLocation("15:04", s.At, now.Location())
		t := time.Date(day.Year(), day.Month(), day.Day(), hm.Hour(), hm.Minute(), 0, 0, now.Location())
		if !t.After(now) && !(d == 0 && last.Before(t)) {
			continue
		}
		wd := t.Weekday()
		if s.Repeat == "weekdays" && (wd == time.Saturday || wd == time.Sunday) || s.Repeat == "weekly" && int(wd) != s.Day {
			continue
		}
		return t
	}
	return time.Time{}
}

// Text describes when it runs: "daily 06:30", "Mondays 07:00", "every 4 hours".
func (s Schedule) Text() string {
	switch s.Repeat {
	case "daily":
		return "daily " + s.At
	case "weekdays":
		return "weekdays " + s.At
	case "weekly":
		return time.Weekday(s.Day).String() + "s " + s.At
	case "hours":
		if s.Hours == 1 {
			return "every hour"
		}
		return fmt.Sprintf("every %d hours", s.Hours)
	}
	return s.Repeat
}
