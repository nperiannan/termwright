package testrec

import (
	"encoding/xml"
	"fmt"
	"html"
	"strings"
)

// JUnit writes a run as JUnit XML (one testsuite, a testcase per case; failures list the failed
// steps), the file CI systems read.
func JUnit(rep RunReport) []byte {
	type failure struct {
		Message string `xml:"message,attr"`
		Text    string `xml:",chardata"`
	}
	type tcase struct {
		Name      string   `xml:"name,attr"`
		Classname string   `xml:"classname,attr"`
		Time      string   `xml:"time,attr"`
		Failure   *failure `xml:"failure,omitempty"`
		Error     *failure `xml:"error,omitempty"`
		Out       string   `xml:"system-out,omitempty"`
	}
	type suite struct {
		XMLName  xml.Name `xml:"testsuite"`
		Name     string   `xml:"name,attr"`
		Tests    int      `xml:"tests,attr"`
		Failures int      `xml:"failures,attr"`
		Errors   int      `xml:"errors,attr"`
		Time     string   `xml:"time,attr"`
		Cases    []tcase  `xml:"testcase"`
	}
	s := suite{Name: "termwright", Tests: len(rep.Results), Time: secs(rep.Ms)}
	for _, r := range rep.Results {
		tc := tcase{Name: r.Case, Classname: strings.TrimSuffix(strings.TrimPrefix(r.File, "cases/"), ".yaml"), Time: secs(r.Ms)}
		var out strings.Builder
		for _, st := range r.Steps {
			fmt.Fprintf(&out, "%s %s\n", mark(st), st.Name)
		}
		tc.Out = out.String()
		switch {
		case r.Error != "":
			s.Errors++
			tc.Error = &failure{Message: r.Error}
		case !r.Passed:
			s.Failures++
			tc.Failure = &failure{Message: firstFailure(r), Text: failText(r)}
		}
		s.Cases = append(s.Cases, tc)
	}
	b, _ := xml.MarshalIndent(s, "", "  ")
	return append([]byte(xml.Header), b...)
}

func secs(ms int64) string { return fmt.Sprintf("%.3f", float64(ms)/1000) }

func mark(st StepResult) string {
	switch {
	case st.Skipped:
		return "-"
	case st.Passed:
		return "✓"
	}
	return "✗"
}

func firstFailure(r Result) string {
	for _, st := range r.Steps {
		if !st.Passed && !st.Skipped {
			if st.Error != "" {
				return st.Name + ": " + st.Error
			}
			for _, c := range st.Checks {
				if !c.Passed {
					return fmt.Sprintf("%s: expected %s, got %q", st.Name, c.What, c.Got)
				}
			}
		}
	}
	return "failed"
}

func failText(r Result) string {
	var b strings.Builder
	for _, st := range r.Steps {
		if st.Passed || st.Skipped {
			continue
		}
		fmt.Fprintf(&b, "Step: %s\nSent: %s\n", st.Name, st.Sent)
		if st.Error != "" {
			fmt.Fprintf(&b, "Error: %s\n", st.Error)
		}
		for _, c := range st.Checks {
			fmt.Fprintf(&b, "%s %s (got %q)\n", map[bool]string{true: "✓", false: "✗"}[c.Passed], c.What, c.Got)
		}
		fmt.Fprintf(&b, "Output:\n%s\n\n", st.Output)
	}
	return b.String()
}

// HTML writes a run as a self-contained report page: a summary, then each case with its steps; a
// failed step shows its checks and output.
func HTML(rep RunReport) []byte {
	e := html.EscapeString
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Termwright test report</title><style>
body{font:14px system-ui,sans-serif;margin:24px;background:#0f1319;color:#dfe5ee}h1{font-size:20px}
.sum{display:flex;gap:16px;margin:8px 0 20px}.sum b{font-size:22px;display:block}.ok{color:#4ec07a}.bad{color:#ef6161}.mut{color:#8a95a5}
details{border:1px solid #2a3240;border-radius:8px;margin:8px 0;padding:6px 12px;background:#151b23}summary{cursor:pointer;font-weight:600}
table{border-collapse:collapse;width:100%;margin:8px 0}td{padding:4px 8px;border-top:1px solid #2a3240;vertical-align:top}
pre{white-space:pre-wrap;background:#0b0f14;padding:8px;border-radius:6px;max-height:320px;overflow:auto;font-size:12px}
@media (prefers-color-scheme: light){body{background:#f6f7f9;color:#1d2330}details{background:#fff;border-color:#dde2ea}td{border-color:#e5e8ee}pre{background:#f0f2f5}}
</style></head><body>`)
	fmt.Fprintf(&b, `<h1>Termwright test report</h1><div class="sum"><div><b class="ok">%d</b>passed</div><div><b class="%s">%d</b>failed</div><div><b>%s s</b>total</div></div>`,
		rep.Passed, map[bool]string{true: "bad", false: "mut"}[rep.Failed > 0], rep.Failed, secs(rep.Ms))
	for _, r := range rep.Results {
		cls, word := "ok", "passed"
		if !r.Passed {
			cls, word = "bad", "failed"
		}
		fmt.Fprintf(&b, `<details%s><summary><span class="%s">%s</span> %s <span class="mut">%s · %s s · %s</span></summary>`,
			map[bool]string{true: "", false: " open"}[r.Passed], cls, word, e(r.Case), e(r.File), secs(r.Ms), r.Start.Format("2006-01-02 15:04:05"))
		if r.Error != "" {
			fmt.Fprintf(&b, `<p class="bad">%s</p>`, e(r.Error))
		}
		b.WriteString("<table>")
		for _, st := range r.Steps {
			c := map[bool]string{true: "ok", false: "bad"}[st.Passed]
			if st.Skipped {
				c = "mut"
			}
			fmt.Fprintf(&b, `<tr><td class="%s">%s</td><td>%s<div class="mut">%s %s</div>`, c, mark(st), e(st.Name), e(st.Target), e(st.Sent))
			if st.Error != "" {
				fmt.Fprintf(&b, `<div class="bad">%s</div>`, e(st.Error))
			}
			for _, ch := range st.Checks {
				fmt.Fprintf(&b, `<div class="%s">%s %s <span class="mut">got %s</span></div>`, map[bool]string{true: "ok", false: "bad"}[ch.Passed], map[bool]string{true: "✓", false: "✗"}[ch.Passed], e(ch.What), e(fmt.Sprintf("%q", ch.Got)))
			}
			if !st.Passed && !st.Skipped && st.Output != "" {
				fmt.Fprintf(&b, `<pre>%s</pre>`, e(st.Output))
			}
			fmt.Fprintf(&b, `</td><td class="mut">%d ms</td></tr>`, st.Ms)
		}
		b.WriteString("</table></details>")
	}
	b.WriteString("</body></html>")
	return []byte(b.String())
}
