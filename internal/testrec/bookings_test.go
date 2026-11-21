package testrec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBookings(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "testbeds"), 0o755)
	os.WriteFile(filepath.Join(d, "testbeds", "lab.yaml"), []byte("name: lab\ndevices: {}\n"), 0o644)
	f := Folder{Root: d}
	now := time.Now()
	b, err := f.SaveBooking(Booking{Testbed: "testbeds/lab.yaml", From: now.Add(-time.Hour), To: now.Add(time.Hour), Who: "someone-else", Why: "upgrade"})
	if err != nil || b.ID == "" {
		t.Fatal(b, err)
	}
	if _, err := f.SaveBooking(Booking{Testbed: "testbeds/lab.yaml", From: now, To: now.Add(2 * time.Hour)}); err == nil {
		t.Fatal("overlap allowed")
	}
	if err := f.CheckBooking("testbeds/lab.yaml", false); !errors.Is(err, ErrBooked) {
		t.Fatal(err)
	}
	if err := f.CheckBooking("testbeds/lab.yaml", true); err != nil {
		t.Fatal(err)
	}
	if err := f.RemoveBooking(b.ID); err != nil || len(f.Bookings(true)) != 0 {
		t.Fatal(err)
	}
	mine, _ := f.SaveBooking(Booking{Testbed: "testbeds/lab.yaml", From: now.Add(-time.Minute), To: now.Add(time.Hour)})
	if !mine.Mine || f.CheckBooking("testbeds/lab.yaml", false) != nil {
		t.Fatal("own booking refused", mine)
	}
}
