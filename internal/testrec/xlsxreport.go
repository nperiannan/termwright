package testrec

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/termwright/termwright/internal/xlsx"
)

// XLSX is a run as an Excel workbook: a summary, one row per test and one per step (with expected vs
// got) - for people who keep test results in spreadsheets.
func XLSX(rep RunReport) []byte {
	res := func(passed, skipped bool) xlsx.Cell {
		switch {
		case skipped:
			return xlsx.Cell{V: "skipped", S: xlsx.Bold}
		case passed:
			return xlsx.Cell{V: "passed", S: xlsx.Pass}
		}
		return xlsx.Cell{V: "failed", S: xlsx.Fail}
	}
	h := func(l ...string) []any {
		out := make([]any, len(l))
		for i, x := range l {
			out[i] = xlsx.Cell{V: x, S: xlsx.Header}
		}
		return out
	}
	var vers []string
	for _, k := range sortedKeys(rep.Versions) {
		vers = append(vers, k+" "+rep.Versions[k])
	}
	sum := [][]any{
		{xlsx.Cell{V: "Test run " + rep.Dir, S: xlsx.Title}},
		{"Passed", rep.Passed}, {"Failed", rep.Failed}, {"Skipped", rep.Skipped},
		{"Time (s)", float64(rep.Ms) / 1000}, {"Testbed", rep.Testbed}, {"Cleanup", map[string]string{"": "cleanup steps", "off": "no cleanup", "auto": "cleanup steps, else undo"}[rep.Cleanup]},
		{"Software", strings.Join(vers, ", ")},
	}
	tests := [][]any{h("Test", "File", "Result", "First problem", "Time (s)", "Versions", "qTest run", "Defect")}
	steps := [][]any{h("Test", "#", "Phase", "Step", "Device", "Sent", "Expected", "Checks (expected → got)", "Result", "Time (ms)")}
	for _, r := range rep.Results {
		var vs []string
		for k, v := range r.Versions {
			vs = append(vs, k+" "+v)
		}
		sort.Strings(vs)
		prob := ""
		if !r.Passed {
			prob = FirstProblem(r)
		}
		tests = append(tests, []any{r.Case, r.File, res(r.Passed, r.Skipped), prob, float64(r.Ms) / 1000, strings.Join(vs, ", "), r.QTestRun, r.Defect})
		for i, s := range r.Steps {
			var cs []string
			for _, c := range s.Checks {
				m := "✓"
				if !c.Passed {
					m = "✗"
				}
				cs = append(cs, fmt.Sprintf("%s %s → %s", m, c.What, c.Got))
			}
			if s.Error != "" {
				cs = append(cs, s.Error)
			}
			ph := s.Phase
			if ph == "" {
				ph = "steps"
			}
			steps = append(steps, []any{r.Case, i + 1, ph, s.Name, s.Target, s.Sent, s.Expected, strings.Join(cs, "\n"), res(s.Passed, s.Skipped), s.Ms})
		}
	}
	var b bytes.Buffer
	xlsx.Write(&b, []xlsx.Sheet{
		{Name: "Summary", Rows: sum, Widths: []float64{14, 60}},
		{Name: "Tests", Rows: tests, Widths: []float64{30, 32, 10, 60, 9, 30, 12, 12}, Freeze: true},
		{Name: "Steps", Rows: steps, Widths: []float64{26, 5, 9, 36, 22, 36, 36, 60, 10, 10}, Freeze: true},
	})
	return b.Bytes()
}
