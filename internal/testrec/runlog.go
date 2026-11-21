package testrec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StepLog is a test's run log: for every step what was sent to which device, what came back in full,
// each check with what was expected and what was there, and the result - the record of the run.
func StepLog(rep RunReport, r Result) string {
	var b strings.Builder
	res := func(passed, skipped bool) string {
		switch {
		case skipped:
			return "SKIPPED"
		case passed:
			return "PASSED"
		}
		return "FAILED"
	}
	fmt.Fprintf(&b, "Test:     %s\nFile:     %s\nStarted:  %s\nResult:   %s  (%.1f s)\n", r.Case, r.File, r.Start.Local().Format("2006-01-02 15:04:05"), res(r.Passed, r.Skipped), float64(r.Ms)/1000)
	if rep.Testbed != "" {
		fmt.Fprintf(&b, "Testbed:  %s\n", rep.Testbed)
	}
	for _, k := range sortedKeys(r.Versions) {
		fmt.Fprintf(&b, "Software: %s %s\n", k, r.Versions[k])
	}
	if r.Error != "" {
		fmt.Fprintf(&b, "Error:    %s\n", r.Error)
	}
	for i, s := range r.Steps {
		ph := s.Phase
		if ph == "" {
			ph = "step"
		}
		fmt.Fprintf(&b, "\n=== %d. %s: %s  [%s, %d ms] ===\n", i+1, ph, s.Name, res(s.Passed, s.Skipped), s.Ms)
		if s.Target != "" {
			fmt.Fprintf(&b, "Device:   %s\n", s.Target)
		}
		if s.Sent != "" {
			fmt.Fprintf(&b, "Sent:     %s\n", s.Sent)
		}
		if s.Status != 0 {
			fmt.Fprintf(&b, "Status:   %d\n", s.Status)
		}
		if s.Expected != "" {
			fmt.Fprintf(&b, "Expected: %s\n", s.Expected)
		}
		if s.Output != "" {
			fmt.Fprintf(&b, "--- output ---\n%s\n--------------\n", strings.TrimRight(s.Output, "\n"))
		}
		if len(s.Checks) > 0 {
			w := len("Expected")
			for _, c := range s.Checks {
				w = max(w, min(len([]rune(c.What)), 60))
			}
			fmt.Fprintf(&b, "Checks:\n  %-8s  %-*s  %s\n", "Result", w, "Expected", "Found")
			for _, c := range s.Checks {
				m := "✓ pass"
				if !c.Passed {
					m = "✗ FAIL"
				}
				fmt.Fprintf(&b, "  %-8s  %-*s  %s\n", m, w, c.What, CheckFound(c))
			}
		}
		if s.Error != "" {
			fmt.Fprintf(&b, "Error:    %s\n", s.Error)
		}
	}
	return b.String()
}

// writeLogs writes each test's log to <run>/logs/NN-<test>.log and notes the file in its result.
func (f Folder) writeLogs(rep *RunReport, abs string) {
	if err := os.MkdirAll(filepath.Join(abs, "logs"), 0o755); err != nil {
		return
	}
	for i := range rep.Results {
		name := fmt.Sprintf("%02d-%s.log", i+1, slug(rep.Results[i].Case))
		if os.WriteFile(filepath.Join(abs, "logs", name), []byte(StepLog(*rep, rep.Results[i])), 0o644) == nil {
			rep.Results[i].Log = rep.Dir + "/logs/" + name
		}
	}
}

// CheckFound is what a check found, in words: the value, or found / not found for text checks.
func CheckFound(c CheckResult) string {
	if c.Got != "" {
		return c.Got
	}
	if c.Passed {
		return "found"
	}
	return "not in the output"
}
