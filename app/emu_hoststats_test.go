package app

import (
	"os/exec"
	"testing"
)

func TestEmuHostStatScript(t *testing.T) {
	out, err := exec.Command("bash", "-c", emuStatScript("no-such-dir", "")).CombinedOutput()
	if err != nil {
		t.Skip("no bash / proc here:", err)
	}
	var s EmuHostStat
	emuParseStat(string(out), &s)
	if s.CPU < 0 || s.CPU > 100 || s.Mem <= 0 || s.MemTotal <= 0 || s.Disk <= 0 || s.DiskTotal <= 0 || s.Cores <= 0 {
		t.Fatalf("%+v\n%s", s, out)
	}
}

func TestEmuParseStat(t *testing.T) {
	out := "a cpu  100 0 100 800 0 0 0 0 0 0 | 1000 2000\nb cpu  150 0 150 900 0 0 0 0 0 0 | 126000 2000\n" +
		"MemTotal:       131072000 kB\nMemAvailable:   13107200 kB\ncores 32\ndf /dev/sda1 1000000 900000 100000 90% /\nif eno1 1000\n"
	var s EmuHostStat
	emuParseStat(out, &s)
	if int(s.CPU) != 50 || int(s.Mem) != 90 || int(s.Disk) != 90 || s.If != "eno1" || s.Speed != 1000 || s.Rx != 1_000_000 || s.Tx != 0 || int(s.Net*10) != 1 {
		t.Fatalf("%+v", s)
	}
}
