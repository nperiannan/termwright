package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// udpFrame is an Ethernet/IPv4/UDP frame with Termwright's signature, for the report's tests.
func udpFrame(dst, src string, vlan int, sip, dip string, ttl, dscp byte, size int, stream uint16) []byte {
	d, _ := net.ParseMAC(dst)
	s, _ := net.ParseMAC(src)
	f := append(append([]byte{}, d...), s...)
	if vlan > 0 {
		f = binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(f, 0x8100), uint16(vlan))
	}
	f = binary.BigEndian.AppendUint16(f, 0x0800)
	n := size - 4 - len(f)
	ip := make([]byte, 20)
	ip[0], ip[1], ip[8], ip[9] = 0x45, dscp<<2, ttl, 17
	binary.BigEndian.PutUint16(ip[2:], uint16(n))
	copy(ip[12:], net.ParseIP(sip).To4())
	copy(ip[16:], net.ParseIP(dip).To4())
	binary.BigEndian.PutUint16(ip[10:], emuIPSum(ip))
	udp := make([]byte, n-20)
	binary.BigEndian.PutUint16(udp, 49152)
	binary.BigEndian.PutUint16(udp[2:], 5001)
	binary.BigEndian.PutUint16(udp[4:], uint16(n-20))
	sig := append([]byte("TWTG"), make([]byte, 20)...)
	binary.BigEndian.PutUint16(sig[4:], stream)
	copy(udp[len(udp)-len(sig):], sig)
	return append(append(f, ip...), udp...)
}

func sampleReport() emuRep {
	tps := []emu.Tester{
		{ID: "t1", Name: "Test port 1", VM: "v1", Port: 3, MAC: "02:63:00:00:00:01", IP: "10.1.0.10/24", GW: "10.1.0.1", X: 120, Y: 300},
		{ID: "t2", Name: "Test port 2", VM: "v2", Port: 5, MAC: "02:63:00:00:00:02", IP: "10.2.0.10/24", GW: "10.2.0.1", X: 620, Y: 300},
		{ID: "t3", Name: "Test port 3", VM: "v2", Port: 7, MAC: "02:63:00:00:00:03", VLAN: 20, X: 800, Y: 300},
	}
	streams := []emu.Stream{
		{ID: "s1", Name: "L3 Test port 1 to Test port 2", From: "t1", To: "t2", Kind: "udp", Size: 512, PPS: 2000, Seconds: 60},
		{ID: "s2", Name: "jitter (voice)", From: "t1", To: "t2", Kind: "udp", Size: 200, PPS: 50, Seconds: 60, DSCP: 46, MaxJitter: 1, MaxLatency: 10},
		{ID: "s3", Name: "Bad IP checksum", From: "t1", To: "t2", Kind: "udp", Size: 128, PPS: 100, Seconds: 30, Bad: "ipcsum", Expect: "drop"},
		{ID: "s4", Name: "fuzz IP header", From: "t1", To: "t2", Kind: "udp", Size: 256, PPS: 200, Seconds: 60, Expect: "any", Fuzz: &emu.Fuzz{From: 14, To: 34, Seed: 1}},
		{ID: "s5", Name: "VLAN 10 to VLAN 20", From: "t2", To: "t3", Kind: "raw", Size: 128, PPS: 100, Seconds: 20, Expect: "drop",
			UDFs: []emu.UDF{{Offset: 18, Width: 2, Mode: "inc", Count: 256, Step: 1}}},
	}
	sent := udpFrame("00:04:96:aa:00:01", "02:63:00:00:00:01", 0, "10.1.0.10", "10.2.0.10", 64, 0, 512, 0)
	got := udpFrame("02:63:00:00:00:02", "00:04:96:aa:00:02", 0, "10.1.0.10", "10.2.0.10", 63, 0, 512, 0)
	voice := udpFrame("00:04:96:aa:00:01", "02:63:00:00:00:01", 0, "10.1.0.10", "10.2.0.10", 64, 46, 200, 1)
	voiceGot := udpFrame("02:63:00:00:00:02", "00:04:96:aa:00:02", 0, "10.1.0.10", "10.2.0.10", 63, 0, 200, 1)
	bad := udpFrame("00:04:96:aa:00:01", "02:63:00:00:00:01", 0, "10.1.0.10", "10.2.0.10", 64, 0, 128, 2)
	bad[24] ^= 0xFF
	hx := func(b []byte) string { return fmt.Sprintf("%d:%s", len(b), hex.EncodeToString(b[:min(160, len(b))])) }
	hist := map[string][][5]float64{}
	for _, id := range []string{"s1", "s2", "s3", "s4", "s5"} {
		for t := 1.0; t <= 60; t++ {
			tx := map[string]float64{"s1": 2000, "s2": 50, "s3": 100, "s4": 200, "s5": 100}[id]
			if (id == "s3" && t > 30) || (id == "s5" && t > 20) {
				continue
			}
			rx := tx
			lat := 0.18 + 0.03*math.Sin(t/5)
			switch id {
			case "s3":
				rx, lat = 0, -1
			case "s5":
				rx = 12
			case "s4":
				rx = tx * 0.9
			}
			loss := 0.0
			if id == "s1" && t > 40 && t < 44 {
				rx, loss = tx*0.97, 0.05
			}
			if id == "s2" {
				lat += 0.4
			}
			hist[id] = append(hist[id], [5]float64{t, tx, rx, loss, lat})
		}
	}
	v := EmuTrafficView{Testers: tps, Streams: streams, History: hist,
		Results: []EmuStreamResult{
			{ID: "s1", TX: 120000, RX: 119820, Expected: 120000, Loss: 0.15, LatAvg: 0.182, LatMin: 0.091, LatMax: 1.84, Jitter: 0.021, Result: "pass", P50: 0.17, P95: 0.29, P99: 0.61,
				Hist: []EmuLatBin{{0.064, 0.13, 8200}, {0.13, 0.25, 101000}, {0.25, 0.5, 9800}, {0.5, 1, 700}, {1, 2, 120}}, Marks: []EmuMark{{0, 0, 0, 119820}}, Sent: hx(sent), Got: hx(got)},
			{ID: "s2", TX: 3000, RX: 3000, Expected: 3000, LatAvg: 0.58, LatMin: 0.41, LatMax: 0.92, Jitter: 0.034, Result: "pass", P50: 0.57, P95: 0.71, P99: 0.85,
				Hist: []EmuLatBin{{0.25, 0.5, 900}, {0.5, 1, 2100}}, Marks: []EmuMark{{0, 0, 0, 3000}}, Sent: hx(voice), Got: hx(voiceGot)},
			{ID: "s3", TX: 3000, RX: 0, Expected: 3000, LatAvg: -1, Result: "pass", Sent: hx(bad)},
			{ID: "s4", TX: 12000, RX: 10800, Expected: 12000, Loss: 10, LatAvg: 0.2, LatMin: 0.1, LatMax: 0.9, Jitter: 0.02, Result: "pass"},
			{ID: "s5", TX: 2000, RX: 240, Expected: 2000, Loss: 88, LatAvg: 0.15, LatMin: 0.1, LatMax: 0.4, Jitter: 0.01, Result: "fail", Why: "240 frames arrived: they should all have been dropped",
				FlowsN: 256, FlowsBad: 12, Flows: []EmuFlowResult{{Flow: 3, Name: "bytes 18-19 = 3", TX: 8, RX: 8, LatAvg: 0.15, LatMax: 0.3}, {Flow: 4, Name: "bytes 18-19 = 4", TX: 8, RX: 8, LatAvg: 0.14, LatMax: 0.2}}},
		},
		Ports: []EmuPortResult{{ID: "t1", TX: 138000}, {ID: "t2", TX: 2000, RX: 133620, Expected: 123000, Got: 122820, Loss: 0.15, LatAvg: 0.19, LatMax: 1.84}, {ID: "t3", RX: 240, Expected: 0}},
	}
	r := emuRep{Lab: "Campus core", At: time.Date(2026, 11, 16, 14, 5, 0, 0, time.UTC), Secs: 60, V: v, Tester: map[string]emu.Tester{}, Res: map[string]EmuStreamResult{}, Port: map[string]EmuPortResult{},
		VMs: []emu.VM{{ID: "v1", Name: "core-1", X: 240, Y: 120}, {ID: "v2", Name: "access-2", X: 680, Y: 120}}, Cables: []emu.Cable{{A: "v1", APort: 49, B: "v2", BPort: 49}},
		VMName: map[string]string{"v1": "core-1", "v2": "access-2"}, Color: map[string]string{}, Num: map[string]int{}, LatAvg: -1, Version: "0.39.0-dev"}
	for _, t := range tps {
		r.Tester[t.ID] = t
	}
	for _, p := range v.Ports {
		r.Port[p.ID] = p
	}
	for i, s := range streams {
		x := v.Results[i]
		r.Res[s.ID], r.Color[s.ID], r.Num[s.ID] = x, emuReportPalette[i], i+1
		r.Passed += map[bool]int{true: 1}[x.Result == "pass"]
		r.Failed += map[bool]int{true: 1}[x.Result == "fail"]
		r.Sent += x.TX
		r.Got += x.RX
		if s.Expect == "" {
			r.Expect += x.Expected
			r.Lost += max(0, x.Expected-x.RX)
		}
	}
	r.LatAvg, r.LatMax = 0.19, 1.84
	return r
}

func TestTrafficReport(t *testing.T) {
	r := sampleReport()
	page := emuReportHTML(r)
	for _, want := range []string{"Traffic report: Campus core", "1 FAILED", "Topology and streams", "Over the run", "core-1", "Test port 2",
		"First frame sent and received", "changed on the way", "10.2.0.10", "0x0800 IPv4", "Bad IP checksum", "(wrong)", "should be dropped", "bytes 18-19", "240 frames arrived"} {
		if !strings.Contains(page, want) {
			t.Errorf("the report has no %q", want)
		}
	}
	sb, _ := emuHexBytes(r.Res["s1"].Sent)
	gb, _ := emuHexBytes(r.Res["s1"].Got)
	rows := emuFrameRows(sb, gb)
	changed := map[string]bool{}
	for _, f := range rows {
		if f[4] != "" {
			changed[f[1]] = true
		}
	}
	if !changed["TTL"] || !changed["Destination MAC"] || !changed["Source MAC"] || changed["Destination"] || changed["Signature"] {
		t.Errorf("routed frame: changed fields %v", changed)
	}
	x, err := emuReportXLSX(r)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(x), int64(len(x)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range z.File {
		names[f.Name] = true
	}
	for _, n := range []string{"xl/workbook.xml", "xl/worksheets/sheet9.xml", "xl/charts/chart3.xml", "xl/drawings/drawing1.xml", "[Content_Types].xml"} {
		if !names[n] {
			t.Errorf("the workbook has no %s", n)
		}
	}
	if d := os.Getenv("TW_REPORT_OUT"); d != "" { // to look at them
		_ = os.WriteFile(filepath.Join(d, "traffic-report.html"), []byte(page), 0o644)
		_ = os.WriteFile(filepath.Join(d, "traffic-report.xlsx"), x, 0o644)
		if os.Getenv("TW_PDF_BROWSER") != "" {
			p, err := (&App{}).emuHTMLToPDF(page)
			if err != nil {
				t.Fatal(err)
			}
			_ = os.WriteFile(filepath.Join(d, "traffic-report.pdf"), p, 0o644)
		}
	}
}
