package main

import (
	"math"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/emu"
)

// Percentiles and the histogram from quarter-octave buckets, per-flow loss, marks, and rates from Mbit/s or % of line.
func TestTrafficStatsMath(t *testing.T) {
	// 90 frames near 0.1 ms (bucket 26: 90.5-107.6 µs), 9 near 1 ms (bucket 39: 861-1024 µs), 1 near 10 ms (bucket 53)
	h := map[string]int{"26": 90, "39": 9, "53": 1}
	if p := emuLatPct(h, 0.5); p < 0.09 || p > 0.11 {
		t.Fatalf("p50 %v", p)
	}
	if p := emuLatPct(h, 0.95); p < 0.85 || p > 1.05 {
		t.Fatalf("p95 %v", p)
	}
	if p := emuLatPct(h, 0.999); p < 8 || p > 11 {
		t.Fatalf("p99.9 %v", p)
	}
	if b := emuLatBins(h); len(b) != 8 || b[0].N != 90 || b[3].N != 9 || b[7].N != 1 || math.Abs(b[0].Lo-0.064) > 1e-9 {
		t.Fatalf("bins %+v", b)
	}
	fl, bad := emuFlowResults(map[string]int{"0": 50, "1": 50, "2": 50}, map[string][]float64{"0": {50, 5, 0.2, 50}, "1": {40, 4, 0.3, 40}, "2": {50, 0, 0, 0}}, map[string]string{"1": "dport 5002"}, 1)
	if bad != 1 || fl[0].Name != "dport 5002" || fl[0].Loss != 20 || fl[1].Flow != 0 || fl[1].LatAvg != 0.1 || fl[2].LatAvg != -1 {
		t.Fatalf("flows %+v %d", fl, bad)
	}
	if m := emuMarks(map[string]int{"10/5/46": 3, "10/0/0": 7}); len(m) != 2 || m[0].N != 7 || m[1].DSCP != 46 {
		t.Fatalf("marks %+v", m)
	}
	s := emu.Stream{Size: 1518, RateUnit: "pct", Rate: 10}
	if p := s.PPSFor(1000); math.Round(p) != 8127 {
		t.Fatalf("10 %% of 1G at 1518 B: %v", p)
	}
	s = emu.Stream{SizeMode: "imix", RateUnit: "mbps", Rate: 100}
	if p := s.PPSFor(1000); math.Round(p) != 32737 {
		t.Fatalf("100 Mbit/s IMIX: %v", p)
	}
	if g := emuSVGHist("x", emuLatBins(h)); !strings.Contains(g, "<rect") {
		t.Fatal("no bars")
	}
	if g := emuSVGLines("x", "pps", []emuSeries{{"a", "#000", [][2]float64{{0, 1}, {1, 3}}}}); !strings.Contains(g, "<polyline") {
		t.Fatal("no line")
	}
}

// The lab's name servers: checked, filled into day 0 ({dns} {dns2} {dnsList}, a computer's own first), and what a restart changes.
func TestLabDNSAndRestartNeed(t *testing.T) {
	v := emu.VM{Name: "pc", LabDNS: []string{"10.0.0.53", "9.9.9.9"}}
	if got := emu.Fill("{dns} {dns2} [{dnsList}]", v, 1); got != "10.0.0.53 9.9.9.9 [10.0.0.53 9.9.9.9]" {
		t.Fatalf("fill %q", got)
	}
	v.DNS = "9.9.9.9"
	if got := emu.Fill("{dnsList}", v, 1); got != "9.9.9.9 10.0.0.53" {
		t.Fatalf("own first %q", got)
	}
	if got := emu.Fill("{dns}", emu.VM{}, 1); got != "8.8.8.8" {
		t.Fatalf("default %q", got)
	}
	im := emu.Image{Day0: []string{"hostname x", "grep -q '^nameserver' /etc/resolv.conf 2>/dev/null || printf 'nameserver 8.8.8.8\\nnameserver 1.1.1.1\\n' > /etc/resolv.conf"}}
	if !emu.AddLinuxDNS(&im) || im.Day0[1] != emu.LinuxDNS {
		t.Fatalf("old day 0 not moved to the lab's servers: %q", im.Day0)
	}
	if s := emuRestartNeed([4]int{2, 2048, 0, 0}, emu.VM{CPUs: 4, RAM: 4096}); s != "vCPU 2 → 4 · RAM 2 GB → 4 GB" {
		t.Fatalf("restart %q", s)
	}
	if s := emuRestartNeed([4]int{}, emu.VM{CPUs: 4}); s != "" {
		t.Fatalf("unknown start %q", s)
	}
	a := &App{}
	if _, err := a.EmuSetDNS([]string{"10.0.0.1, nope"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("bad address: %v", err)
	}
	if _, err := a.EmuSetDNS([]string{"1.1.1.1 2.2.2.2 3.3.3.3 4.4.4.4"}); err == nil {
		t.Fatal("four servers taken")
	}
}
