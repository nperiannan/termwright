package main

import (
	"testing"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

func TestEmuCPUStatsOf(t *testing.T) {
	t0 := time.Now()
	var h []emuCPUSample
	use := []float64{100, 300, 900, 1000, 200, 950, 100, 100, 100, 1000}
	for i, m := range use {
		h = append(h, emuCPUSample{at: t0.Add(time.Duration(i) * time.Minute), m: m, thr: float64(i * 2), thU: float64(i) * 100000})
	}
	h[6].thr, h[6].thU = 0, 0 // restarted: its counters started again
	s := emuCPUStatsOf(h, 500)
	if s == nil || s.Mins != 9 || s.Avg != 475 || s.Peak != 1000 || s.P95 != 1000 || s.Over != 40 || s.OverN != 3 {
		t.Fatalf("%+v", s)
	}
	if s.Thr != 28 || s.ThrS != 1.4 { // 5×2 before the restart, then 14, 2 and 2
		t.Fatalf("throttling %+v", s)
	}
	if emuCPUStatsOf(h[:1], 0) != nil {
		t.Fatal("one sample is not enough")
	}
	if s := emuCPUStatsOf(h, 0); s.Over != 0 || s.OverN != 0 {
		t.Fatalf("no request: nothing is over it %+v", s)
	}
}

func TestVMSetCPUReq(t *testing.T) {
	v := emu.VM{CPUWeight: 50, CPUMax: 0.5}
	if v.CPUReqM() != 500 || v.CPULimM() != 500 {
		t.Fatalf("older settings: %d %d", v.CPUReqM(), v.CPULimM())
	}
	v.SetCPUReq(250, 1500)
	if v.CPUWeight != 25 || v.CPUMax != 1.5 || v.CPUReqM() != 250 || v.CPULimM() != 1500 {
		t.Fatalf("%+v", v)
	}
	v.SetCPUReq(0, 0)
	if v.CPUWeight != 0 || v.CPUMax != 0 || v.CPUReqM() != 0 || v.CPULimM() != 0 {
		t.Fatalf("none: %+v", v)
	}
	v.SetCPUReq(5, 0)
	if v.CPUWeight != 1 {
		t.Fatalf("a tiny request keeps weight 1: %d", v.CPUWeight)
	}
	st := emu.ParseStatus("clk 100\nup v1 1234 5000\nthr v1 37 4200000\n", nil)
	if st.Thr["v1"] != [2]float64{37, 4200000} {
		t.Fatalf("throttling read: %v", st.Thr)
	}
}

func TestEmuSetCPUReqChecks(t *testing.T) {
	a := &App{}
	for _, c := range [][2]int{{-1, 0}, {0, 300000}, {0, 5}, {800, 500}} {
		if _, err := a.EmuSetCPUReq("x", c[0], c[1]); err == nil {
			t.Errorf("%v accepted", c)
		}
	}
}
