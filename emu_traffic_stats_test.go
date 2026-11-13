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
