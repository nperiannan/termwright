package testrec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Booking reserves a testbed for a time: runs and schedules of other people are refused then.
// Bookings are in bookings.yaml in the tests folder, so a team shares them through Git.
type Booking struct {
	ID      string    `yaml:"id" json:"id"`
	Testbed string    `yaml:"testbed" json:"testbed"`
	From    time.Time `yaml:"from" json:"from"`
	To      time.Time `yaml:"to" json:"to"`
	// Who: the user name that booked it (runs by that user are allowed).
	Who string `yaml:"who" json:"who"`
	Why string `yaml:"why,omitempty" json:"why,omitempty"`
	// Mine: booked by this computer's user (not saved).
	Mine bool `yaml:"-" json:"mine,omitempty"`
}

// ErrBooked: the testbed is booked by someone else.
var ErrBooked = errors.New("testbed booked")

func (f Folder) bookingsFile() string { return filepath.Join(f.Root, "bookings.yaml") }

// Bookings lists the bookings that have not ended (all when all is set), by start time.
func (f Folder) Bookings(all bool) []Booking {
	out := []Booking{}
	b, err := os.ReadFile(f.bookingsFile())
	if err != nil {
		return out
	}
	var l []Booking
	if yaml.Unmarshal(b, &l) != nil {
		return out
	}
	u, _ := me()
	now := time.Now()
	for _, x := range l {
		if !all && !x.To.After(now) {
			continue
		}
		x.Mine = strings.EqualFold(x.Who, u)
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From.Before(out[j].From) })
	return out
}

func (f Folder) writeBookings(l []Booking) error {
	sort.Slice(l, func(i, j int) bool { return l[i].From.Before(l[j].From) })
	b, err := yaml.Marshal(l)
	if err != nil {
		return err
	}
	return os.WriteFile(f.bookingsFile(), append([]byte("# Testbed bookings - edit in Termwright (Tests > Testbeds > Calendar)\n"), b...), 0o644)
}

// SaveBooking adds a booking (an empty ID) or changes one; it is refused when it overlaps another
// booking of that testbed. Who defaults to this computer's user.
func (f Folder) SaveBooking(bk Booking) (Booking, error) {
	if bk.Testbed == "" {
		return bk, errors.New("choose a testbed")
	}
	if _, err := f.Testbed(bk.Testbed); err != nil {
		return bk, err
	}
	if !bk.To.After(bk.From) {
		return bk, errors.New("the booking must end after it starts")
	}
	if bk.To.Sub(bk.From) > 31*24*time.Hour {
		return bk, errors.New("a booking can be 31 days at most")
	}
	u, _ := me()
	if strings.TrimSpace(bk.Who) == "" {
		bk.Who = u
	}
	l := f.Bookings(true)
	var keep []Booking
	found := bk.ID == ""
	for _, x := range l {
		if x.ID == bk.ID && bk.ID != "" {
			found = true
			continue
		}
		if x.Testbed == bk.Testbed && x.From.Before(bk.To) && bk.From.Before(x.To) {
			return bk, fmt.Errorf("%s is already booked by %s from %s to %s%s", bk.Testbed, x.Who, x.From.Local().Format("Jan 2 15:04"), x.To.Local().Format("Jan 2 15:04"), why(x.Why))
		}
		// bookings that ended more than 90 days ago are dropped from the file
		if time.Since(x.To) < 90*24*time.Hour {
			keep = append(keep, x)
		}
	}
	if !found {
		return bk, errors.New("no booking " + bk.ID)
	}
	if bk.ID == "" {
		bk.ID = "bk-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	bk.Mine = strings.EqualFold(bk.Who, u)
	return bk, f.writeBookings(append(keep, bk))
}

// RemoveBooking cancels a booking (it is taken out of bookings.yaml).
func (f Folder) RemoveBooking(id string) error {
	l := f.Bookings(true)
	var keep []Booking
	for _, x := range l {
		if x.ID != id {
			keep = append(keep, x)
		}
	}
	if len(keep) == len(l) {
		return errors.New("no booking " + id)
	}
	return f.writeBookings(keep)
}

// BookedNow is the booking of a testbed by someone else at the time t, or nil.
func (f Folder) BookedNow(testbed string, t time.Time) *Booking {
	for _, x := range f.Bookings(false) {
		if x.Testbed == testbed && !x.Mine && !t.Before(x.From) && t.Before(x.To) {
			return &x
		}
	}
	return nil
}

// CheckBooking refuses a run on a testbed booked by someone else (unless force).
func (f Folder) CheckBooking(testbed string, force bool) error {
	if testbed == "" || force {
		return nil
	}
	if b := f.BookedNow(testbed, time.Now()); b != nil {
		return fmt.Errorf("%w: %s is booked by %s until %s%s", ErrBooked, testbed, b.Who, b.To.Local().Format("Jan 2 15:04"), why(b.Why))
	}
	return nil
}

func why(s string) string {
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}
