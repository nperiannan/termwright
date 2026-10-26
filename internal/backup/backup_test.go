package backup

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestSaveOnlyChangedAndKeep(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	t0 := time.Date(2026, 10, 26, 2, 0, 0, 0, time.Local)
	cfg := "#\n# Mon Oct 26 02:00:01 2026 UTC\n#\nvlan create 10\n"
	f1, ch, err := s.Save("Lab/sw1", cfg, true, 3, t0)
	if err != nil || !ch || !strings.HasSuffix(f1, "2026-10-26_020000.txt") {
		t.Fatalf("first: %s %v %v", f1, ch, err)
	}
	// only the timestamp differs: not changed, nothing written
	f2, ch, _ := s.Save("Lab/sw1", strings.Replace(cfg, "02:00:01", "03:00:07", 1), true, 3, t0.Add(time.Hour))
	if ch || f2 != f1 {
		t.Fatalf("unchanged: %s %v", f2, ch)
	}
	for i := 1; i <= 4; i++ {
		if _, ch, _ := s.Save("Lab/sw1", cfg+"vlan create "+strings.Repeat("1", i)+"\n", true, 3, t0.Add(time.Duration(i)*24*time.Hour)); !ch {
			t.Fatal("change not seen")
		}
	}
	files, _ := s.Files("Lab/sw1")
	if len(files) != 3 || files[0].Name != "2026-10-30_020000.txt" {
		t.Fatalf("%+v", files)
	}
	if _, _, err := s.Save("../x", "a", true, 3, t0); err == nil {
		t.Fatal("path outside the folder accepted")
	}
}

func TestStatusAndJobs(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	_ = s.SetStatus("Lab/sw1", Status{OK: true, Changed: true, Copies: 2})
	_ = s.SetJobRun("nightly", time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC))
	if st := s.Status()["Lab/sw1"]; !st.OK || st.Copies != 2 {
		t.Fatalf("%+v", st)
	}
	if s.JobRun("nightly").Hour() != 2 {
		t.Fatal("job time")
	}
	if _, err := os.Stat(s.Dir + "/backup-status.json"); err != nil {
		t.Fatal(err)
	}
}

func TestDue(t *testing.T) {
	loc := time.Local
	mon := time.Date(2026, 10, 26, 2, 30, 0, 0, loc) // Monday
	never := time.Time{}
	if !Due("daily", "02:00", never, mon) || Due("daily", "03:00", never, mon) {
		t.Fatal("daily")
	}
	if Due("daily", "02:00", mon.Add(-10*time.Minute), mon) {
		t.Fatal("ran already today")
	}
	if Due("weekdays", "02:00", never, mon.AddDate(0, 0, -2)) || !Due("weekdays", "02:00", never, mon) {
		t.Fatal("weekdays")
	}
	if Due("", "02:00", never, mon) {
		t.Fatal("manual job")
	}
}
