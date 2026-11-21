package testrec

import (
	"fmt"
	"strings"

	"github.com/termwright/termwright/internal/pdf"
)

var (
	pdfGreen = pdf.RGB{R: 0.13, G: 0.6, B: 0.33}
	pdfRed   = pdf.RGB{R: 0.82, G: 0.2, B: 0.2}
	pdfAmber = pdf.RGB{R: 0.85, G: 0.6, B: 0.1}
	pdfGrey  = pdf.RGB{R: 0.45, G: 0.47, B: 0.5}
	pdfLight = pdf.RGB{R: 0.95, G: 0.96, B: 0.97}
)

// pdfFlow writes top to bottom and starts new pages.
type pdfFlow struct {
	d    *pdf.Doc
	y    float64
	page int
	foot string
}

const pdfLeft, pdfRight = 42.0, pdf.PageW - 42

func (f *pdfFlow) need(h float64) {
	if f.y-h < 50 {
		f.footer()
		f.d.AddPage()
		f.page++
		f.y = pdf.PageH - 50
	}
}

func (f *pdfFlow) footer() {
	f.d.Text(pdfLeft, 28, pdf.Helvetica, 8, pdfGrey, f.foot)
	f.d.Text(pdfRight-40, 28, pdf.Helvetica, 8, pdfGrey, fmt.Sprintf("page %d", f.page))
}

func (f *pdfFlow) line(font string, size float64, c pdf.RGB, indent float64, s string) {
	for _, l := range pdf.Wrap(font, size, pdfRight-pdfLeft-indent, s) {
		f.need(size + 4)
		f.d.Text(pdfLeft+indent, f.y-size, font, size, c, l)
		f.y -= size + 3
	}
}

func resultWord(passed, skipped bool) (string, pdf.RGB) {
	switch {
	case skipped:
		return "SKIPPED", pdfGrey
	case passed:
		return "PASSED", pdfGreen
	}
	return "FAILED", pdfRed
}

// PDFReport is a run's report as a PDF: the summary, a table of the tests, and every step with what was
// sent, each check with expected and found, errors and the start of the output.
func PDFReport(rep RunReport) []byte {
	d := pdf.New("Termwright test report " + rep.Dir)
	f := &pdfFlow{d: d, y: pdf.PageH - 50, page: 1, foot: "Termwright test report · " + rep.Dir}
	d.Text(pdfLeft, f.y-20, pdf.Bold, 20, pdf.Black, "Test report")
	f.y -= 30
	meta := []string{fmt.Sprintf("%.1f s", float64(rep.Ms)/1000)}
	if len(rep.Results) > 0 {
		meta = append([]string{rep.Results[0].Start.Local().Format("2006-01-02 15:04")}, meta...)
	}
	if rep.Testbed != "" {
		meta = append(meta, "testbed "+rep.Testbed)
	}
	if rep.RunBy != "" {
		meta = append(meta, "run by "+rep.RunBy)
	}
	f.line(pdf.Helvetica, 10, pdfGrey, 0, strings.Join(meta, "  ·  "))
	for _, k := range sortedKeys(rep.Versions) {
		f.line(pdf.Helvetica, 9, pdfGrey, 0, "Software: "+k+" "+rep.Versions[k])
	}
	f.y -= 8
	// summary boxes
	boxes := []struct {
		n    int
		what string
		c    pdf.RGB
	}{{rep.Passed, "passed", pdfGreen}, {rep.Failed, "failed", pdfRed}, {rep.Skipped, "skipped", pdfGrey}, {rep.Flaky, "flaky (passed on a rerun)", pdfAmber}}
	x := pdfLeft
	for _, b := range boxes {
		d.Rect(x, f.y-46, 118, 46, pdfLight)
		d.Rect(x, f.y-46, 4, 46, b.c)
		d.Text(x+12, f.y-26, pdf.Bold, 18, b.c, fmt.Sprint(b.n))
		d.Text(x+12, f.y-40, pdf.Helvetica, 8.5, pdfGrey, b.what)
		x += 128
	}
	f.y -= 62
	// tests table
	f.need(20)
	d.Text(pdfLeft, f.y-11, pdf.Bold, 11, pdf.Black, "Tests")
	f.y -= 18
	for _, r := range rep.Results {
		f.need(16)
		w, c := resultWord(r.Passed, r.Skipped)
		if r.Flaky {
			w, c = "FLAKY", pdfAmber
		}
		if r.CleanupFailed {
			w = "FAILED (cleanup)"
		}
		d.Text(pdfLeft, f.y-10, pdf.Bold, 8.5, c, w)
		d.Text(pdfLeft+82, f.y-10, pdf.Helvetica, 9.5, pdf.Black, clipText(r.Case, 62))
		d.Text(pdfRight-60, f.y-10, pdf.Helvetica, 9, pdfGrey, fmt.Sprintf("%.1f s", float64(r.Ms)/1000))
		d.Line(pdfLeft, f.y-14, pdfRight, f.y-14, 0.3, pdfLight)
		f.y -= 16
	}
	// each test
	for _, r := range rep.Results {
		f.y -= 14
		f.need(40)
		w, c := resultWord(r.Passed, r.Skipped)
		d.Rect(pdfLeft, f.y-22, pdfRight-pdfLeft, 22, pdfLight)
		d.Text(pdfLeft+8, f.y-15, pdf.Bold, 12, pdf.Black, clipText(r.Case, 60))
		d.Text(pdfRight-70, f.y-15, pdf.Bold, 10, c, w)
		f.y -= 28
		if r.File != "" {
			f.line(pdf.Helvetica, 8.5, pdfGrey, 0, r.File)
		}
		if r.Error != "" {
			f.line(pdf.Helvetica, 9, pdfRed, 0, r.Error)
		}
		for i, s := range r.Steps {
			f.y -= 4
			f.need(30)
			sw, sc := resultWord(s.Passed, s.Skipped)
			ph := ""
			if s.Phase != "" {
				ph = s.Phase + ": "
			}
			d.Text(pdfLeft, f.y-10, pdf.Bold, 9.5, pdf.Black, clipText(fmt.Sprintf("%d. %s%s", i+1, ph, s.Name), 80))
			d.Text(pdfRight-90, f.y-10, pdf.Bold, 8.5, sc, sw)
			d.Text(pdfRight-40, f.y-10, pdf.Helvetica, 8.5, pdfGrey, fmt.Sprintf("%d ms", s.Ms))
			f.y -= 14
			if s.Target != "" || s.Sent != "" {
				f.line(pdf.Helvetica, 8.5, pdfGrey, 12, strings.TrimSpace(s.Target+"  "+s.Sent))
			}
			for _, ck := range s.Checks {
				cw, cc := "PASS", pdfGreen
				if !ck.Passed {
					cw, cc = "FAIL", pdfRed
				}
				f.need(12)
				d.Text(pdfLeft+12, f.y-9, pdf.Bold, 8, cc, cw)
				f.line(pdf.Helvetica, 8.5, pdf.Black, 42, ck.What+"   →  "+CheckFound(ck))
			}
			if s.Error != "" {
				f.line(pdf.Helvetica, 8.5, pdfRed, 12, s.Error)
			}
			if out := strings.TrimRight(s.Output, "\n"); out != "" && !s.Skipped {
				ls := strings.Split(out, "\n")
				if len(ls) > 14 {
					ls = append(ls[:14], fmt.Sprintf("… %d more lines in the run log", len(ls)-14))
				}
				for _, l := range ls {
					f.line(pdf.Courier, 7.5, pdfGrey, 12, l)
				}
			}
		}
	}
	f.footer()
	return d.Bytes()
}

func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
