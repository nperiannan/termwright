package testrec

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestChangeTestbed(t *testing.T) {
	d := t.TempDir()
	f := Folder{Root: d}
	rel, err := f.WriteTestbed("lab", []string{"Lab/EXOS/10.0.0.1", "Lab/EXOS/core"}, "devices of Lab/EXOS")
	if err != nil {
		t.Fatal(err)
	}
	f.SaveText(rel, mustRead(t, filepath.Join(d, rel))+"vlan: \"200\"\n")
	if err := f.ChangeTestbed(rel, []string{"Lab/VOSS/edge", "Lab/EXOS/core"}, []string{"dev_10_0_0_1"}); err != nil {
		t.Fatal(err)
	}
	m, _ := f.Testbed(rel)
	if m["edge"] != "Lab/VOSS/edge" || m["core"] != "Lab/EXOS/core" || m["dev_10_0_0_1"] != "" || m["vlan"] != "200" || len(m) != 3 {
		t.Fatal(m)
	}
	if txt := mustRead(t, filepath.Join(d, rel)); !strings.HasPrefix(txt, "# devices of Lab/EXOS") {
		t.Fatal(txt)
	}
}

func mustRead(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
