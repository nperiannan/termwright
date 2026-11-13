package main

import (
	"fmt"
	"html"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// EmuLatBin is a bar of a stream's latency histogram: frames whose latency was from Lo to Hi ms.
type EmuLatBin struct {
	Lo float64 `json:"lo"` // ms
	Hi float64 `json:"hi"` // ms
	N  int     `json:"n"`  // frames
}

// EmuFlowResult is one flow of a stream with changing fields: one combination of their values.
type EmuFlowResult struct {
	Flow   int     `json:"flow"`   // its number (0 = every field at its first value)
	Name   string  `json:"name"`   // its field values (dport 5002, srcIp 10.0.0.3)
	TX     int     `json:"tx"`     // frames sent
	RX     int     `json:"rx"`     // frames received
	Loss   float64 `json:"loss"`   // %
	LatAvg float64 `json:"latAvg"` // ms (-1 = not measured)
	LatMax float64 `json:"latMax"` // ms
}

// EmuMark is how a stream's frames arrived: their VLAN, 802.1p priority and DSCP (-1: not IP), and how many.
type EmuMark struct {
	VLAN int `json:"vlan"`
	Prio int `json:"prio"`
	DSCP int `json:"dscp"`
	N    int `json:"n"`
}

// emuFlowsShown: the flows a result lists (the ones with the most loss first); the API's flows report has them all.
const emuFlowsShown = 32

// emuBucketMs is the latency (ms) at a histogram bucket's middle: bucket b holds 2^(b/4) to 2^((b+1)/4) µs.
func emuBucketMs(b, at float64) float64 { return math.Pow(2, (b+at)/4) / 1000 }

// emuLatPct is a latency percentile (p of 0-1) from a histogram (bucket → frames), ms; -1 without frames.
func emuLatPct(h map[string]int, p float64) float64 {
	type bc struct{ b, n int }
	var bs []bc
	total := 0
	for k, n := range h {
		if b, err := strconv.Atoi(k); err == nil && n > 0 {
			bs = append(bs, bc{b, n})
			total += n
		}
	}
	if total == 0 {
		return -1
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].b < bs[j].b })
	need, sum := p*float64(total), 0
	for _, x := range bs {
		sum += x.n
		if float64(sum) >= need {
			return emuBucketMs(float64(x.b), 0.5)
		}
	}
	return emuBucketMs(float64(bs[len(bs)-1].b), 0.5)
}

// emuLatBins is the histogram for the screens: an octave a bar (64-128 µs, 128-256 µs …), from the fastest frame's to the slowest's.
func emuLatBins(h map[string]int) []EmuLatBin {
	oct := map[int]int{}
	lo, hi := math.MaxInt, -1
	for k, n := range h {
		if b, err := strconv.Atoi(k); err == nil && n > 0 {
			o := b / 4
			oct[o] += n
			lo, hi = min(lo, o), max(hi, o)
		}
	}
	var out []EmuLatBin
	for o := lo; o <= hi; o++ {
		out = append(out, EmuLatBin{Lo: math.Pow(2, float64(o)) / 1000, Hi: math.Pow(2, float64(o+1)) / 1000, N: oct[o]})
	}
	return out
}

// emuFlowResults: a stream's flows (sent, received and latency per flow), the most lossy first; recv is how many test ports each frame is for.
func emuFlowResults(ftx map[string]int, frx map[string][]float64, lab map[string]string, recv int) (out []EmuFlowResult, lossy int) {
	for k, tx := range ftx {
		f, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		r := EmuFlowResult{Flow: f, Name: lab[k], TX: tx, LatAvg: -1}
		if x := frx[k]; len(x) == 4 {
			r.RX = int(x[0])
			if x[3] > 0 {
				r.LatAvg, r.LatMax = x[1]/x[3], x[2]
			}
		}
		if exp := tx * recv; exp > 0 {
			r.Loss = math.Max(0, float64(exp-r.RX)*100/float64(exp))
		}
		if r.Loss > 0 {
			lossy++
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Loss != out[j].Loss {
			return out[i].Loss > out[j].Loss
		}
		return out[i].Flow < out[j].Flow
	})
	return out, lossy
}

// emuMarks: how a stream's frames arrived ("vlan/prio/dscp" → frames), the most first.
func emuMarks(q map[string]int) []EmuMark {
	var out []EmuMark
	for k, n := range q {
		p := strings.Split(k, "/")
		if len(p) != 3 {
			continue
		}
		v, _ := strconv.Atoi(p[0])
		pr, _ := strconv.Atoi(p[1])
		d, _ := strconv.Atoi(p[2])
		out = append(out, EmuMark{v, pr, d, n})
	}
	slices.SortFunc(out, func(a, b EmuMark) int { return b.N - a.N })
	return out
}

// emuAddCounts adds one host's per-key counters to the sum.
func emuAddCounts(sum map[string]map[string]int, add map[string]map[string]int) {
	for sid, m := range add {
		if sum[sid] == nil {
			sum[sid] = map[string]int{}
		}
		for k, n := range m {
			sum[sid][k] += n
		}
	}
}

// emuSeries is a line of a report graph.
type emuSeries struct {
	Name, Color string
	Pts         [][2]float64 // [seconds, value]
}

// emuReportPalette: the report's line colours (the Traffic page's, light).
var emuReportPalette = []string{"#2a78d6", "#eb6834", "#1baf7a", "#eda100", "#e87ba4", "#008300", "#6250d6", "#e34948"}

// emuSVGLines is a report graph over time as inline SVG: a recessive grid, 2 px lines and a legend.
func emuSVGLines(title, unit string, ss []emuSeries) string {
	const w, h, l, r, t, bt = 560.0, 170.0, 52.0, 12.0, 26.0, 22.0
	t0, t1, vmax := math.Inf(1), math.Inf(-1), 0.0
	for _, s := range ss {
		for _, p := range s.Pts {
			t0, t1, vmax = math.Min(t0, p[0]), math.Max(t1, p[0]), math.Max(vmax, p[1])
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<figure><figcaption><b>%s</b> <span>%s</span>`, html.EscapeString(title), html.EscapeString(unit))
	if len(ss) > 1 {
		for _, s := range ss {
			fmt.Fprintf(&b, ` <i style="background:%s"></i>%s`, s.Color, html.EscapeString(s.Name))
		}
	}
	b.WriteString("</figcaption>")
	if math.IsInf(t0, 1) || t1 <= t0 {
		return b.String() + "<p>No data.</p></figure>"
	}
	step := emuNiceStep(math.Max(vmax, 1e-9) / 3)
	ymax := step * 3
	x := func(v float64) float64 { return l + (v-t0)/(t1-t0)*(w-l-r) }
	y := func(v float64) float64 { return h - bt - v/ymax*(h-bt-t) }
	fmt.Fprintf(&b, `<svg viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" role="img" aria-label="%s">`, w, h, w, h, html.EscapeString(title))
	for k := 0; k <= 3; k++ {
		v := float64(k) * step
		fmt.Fprintf(&b, `<line x1="%.0f" x2="%.0f" y1="%.1f" y2="%.1f" stroke="#e3e3e3"/><text x="%.0f" y="%.1f" text-anchor="end" font-size="10" fill="#777">%s</text>`, l, w-r, y(v), y(v), l-6, y(v)+3, emuAxis(v))
	}
	for _, f := range []float64{0, 0.5, 1} {
		tv := t0 + f*(t1-t0)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.0f" text-anchor="middle" font-size="10" fill="#777">%.0f s</text>`, x(tv), h-6, tv)
	}
	for _, s := range ss {
		var pts []string
		for _, p := range s.Pts {
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", x(p[0]), y(p[1])))
		}
		fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="2" stroke-linejoin="round" points="%s"/>`, s.Color, strings.Join(pts, " "))
	}
	return b.String() + "</svg></figure>"
}

// emuSVGHist is a latency histogram as inline SVG bars (an octave a bar).
func emuSVGHist(title string, bins []EmuLatBin) string {
	const w, h, l, t, bt = 560.0, 150.0, 52.0, 10.0, 22.0
	top := 0
	for _, x := range bins {
		top = max(top, x.N)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<figure><figcaption><b>%s</b> <span>frames per latency band, ms</span></figcaption>`, html.EscapeString(title))
	if top == 0 {
		return b.String() + "<p>No latency measured.</p></figure>"
	}
	fmt.Fprintf(&b, `<svg viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" role="img" aria-label="%s">`, w, h, w, h, html.EscapeString(title))
	bw := (w - l - 8) / float64(len(bins))
	for i, x := range bins {
		bh := float64(x.N) / float64(top) * (h - bt - t)
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="2" fill="#2a78d6"><title>%s-%s ms: %d</title></rect>`, l+float64(i)*bw+1, h-bt-bh, math.Max(1, bw-2), bh, emuMs(x.Lo), emuMs(x.Hi), x.N)
		if len(bins) <= 12 || i%2 == 0 {
			fmt.Fprintf(&b, `<text x="%.1f" y="%.0f" text-anchor="middle" font-size="10" fill="#777">%s</text>`, l+float64(i)*bw+bw/2, h-6, emuMs(x.Lo))
		}
	}
	fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" text-anchor="end" font-size="10" fill="#777">%d</text>`, l-6, t+4, top)
	return b.String() + "</svg></figure>"
}

func emuNiceStep(v float64) float64 {
	p := math.Pow(10, math.Floor(math.Log10(v)))
	switch m := v / p; {
	case m <= 1:
		return p
	case m <= 2:
		return 2 * p
	case m <= 5:
		return 5 * p
	}
	return 10 * p
}

// emuAxis writes an axis value: 12k, 1.5k, 0.25.
func emuAxis(v float64) string {
	switch {
	case v >= 10000:
		return fmt.Sprintf("%.0fk", v/1000)
	case v >= 1000:
		return fmt.Sprintf("%.1fk", v/1000)
	case v >= 10 || v == 0:
		return fmt.Sprintf("%.0f", v)
	}
	return strconv.FormatFloat(v, 'g', 2, 64)
}

// emuMs writes a latency band's edge: 0.064, 0.13, 1, 16.
func emuMs(v float64) string { return strconv.FormatFloat(v, 'g', 2, 64) }

// emuReportGraphs: the report's graphs: throughput, loss and latency over the run, and each stream's latency histogram and flows.
func emuReportGraphs(v EmuTrafficView) string {
	var b strings.Builder
	tx, rx := map[float64]float64{}, map[float64]float64{}
	var loss, lat []emuSeries
	for i, s := range v.Streams {
		h := v.History[s.ID]
		if len(h) == 0 {
			continue
		}
		c := emuReportPalette[i%len(emuReportPalette)]
		ls, la := emuSeries{Name: s.Name, Color: c}, emuSeries{Name: s.Name, Color: c}
		for _, p := range h {
			tx[p[0]] += p[1]
			rx[p[0]] += p[2]
			ls.Pts = append(ls.Pts, [2]float64{p[0], p[3]})
			if p[4] >= 0 {
				la.Pts = append(la.Pts, [2]float64{p[0], p[4]})
			}
		}
		loss = append(loss, ls)
		if len(la.Pts) > 0 {
			lat = append(lat, la)
		}
	}
	if len(loss) == 0 {
		return ""
	}
	pts := func(m map[float64]float64) [][2]float64 {
		var out [][2]float64
		for t, n := range m {
			out = append(out, [2]float64{t, n})
		}
		sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
		return out
	}
	b.WriteString(`<h3>Over the run</h3><div class="g">`)
	b.WriteString(emuSVGLines("Throughput", "frames per second", []emuSeries{{"Sent", emuReportPalette[0], pts(tx)}, {"Received", emuReportPalette[1], pts(rx)}}))
	b.WriteString(emuSVGLines("Loss so far", "%", loss))
	if len(lat) > 0 {
		b.WriteString(emuSVGLines("Latency", "ms", lat))
	}
	b.WriteString("</div>")
	res := map[string]EmuStreamResult{}
	for _, r := range v.Results {
		res[r.ID] = r
	}
	for _, s := range v.Streams {
		r := res[s.ID]
		if len(r.Hist) == 0 && len(r.Flows) == 0 && len(r.Marks) == 0 {
			continue
		}
		fmt.Fprintf(&b, `<h3>%s</h3><div class="g">`, html.EscapeString(s.Name))
		if len(r.Hist) > 0 {
			b.WriteString(emuSVGHist(fmt.Sprintf("Latency: p50 %.3f · p95 %.3f · p99 %.3f ms", r.P50, r.P95, r.P99), r.Hist))
		}
		b.WriteString("</div>")
		if len(r.Marks) > 0 {
			b.WriteString("<table><tr><th>Arrived with VLAN</th><th>Priority</th><th>DSCP</th><th>Frames</th></tr>")
			for _, m := range r.Marks {
				d := "-"
				if m.DSCP >= 0 {
					d = fmt.Sprint(m.DSCP)
				}
				fmt.Fprintf(&b, "<tr><td>%s</td><td>%d</td><td>%s</td><td>%d</td></tr>", map[bool]string{true: "untagged", false: fmt.Sprint(m.VLAN)}[m.VLAN == 0], m.Prio, d, m.N)
			}
			b.WriteString("</table>")
		}
		if len(r.Flows) > 0 {
			fmt.Fprintf(&b, "<p>%d flows, %d with loss", r.FlowsN, r.FlowsBad)
			if r.FlowsN > len(r.Flows) {
				fmt.Fprintf(&b, " (the %d with the most loss below)", len(r.Flows))
			}
			b.WriteString("</p><table><tr><th>Flow</th><th>Sent</th><th>Received</th><th>Loss %</th><th>Latency avg ms</th><th>Latency max ms</th></tr>")
			for _, f := range r.Flows {
				la, lm := "-", "-"
				if f.LatAvg >= 0 {
					la, lm = fmt.Sprintf("%.3f", f.LatAvg), fmt.Sprintf("%.3f", f.LatMax)
				}
				fmt.Fprintf(&b, "<tr><td>%s</td><td>%d</td><td>%d</td><td>%.2f</td><td>%s</td><td>%s</td></tr>", html.EscapeString(f.Name), f.TX, f.RX, f.Loss, la, lm)
			}
			b.WriteString("</table>")
		}
	}
	return b.String()
}
