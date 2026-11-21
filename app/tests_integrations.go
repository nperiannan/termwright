package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/integrations"
	"github.com/termwright/termwright/internal/testrec"
)

// TestSendQTest sends a run's results to qTest: a test run per test that names its qTest test case
// (qtest: {testCase: …}), with its status and the HTML report attached. files: only these tests
// (none: all). It returns what was sent.
func (a *App) TestSendQTest(dir string, files []string) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	rep, err := f.LoadReport(dir)
	if err != nil {
		return "", err
	}
	q := a.qtestClient()
	html, _ := os.ReadFile(filepath.Join(f.Root, rep.Dir, "report.html"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var sent, missing, failed []string
	for i, r := range rep.Results {
		if r.File == "" || len(files) > 0 && !contains(files, r.File) || r.Skipped {
			continue
		}
		tc := f.CaseQTest(r.File)
		if tc == "" {
			missing = append(missing, r.Case)
			continue
		}
		res := integrations.QTestResult{TestCase: tc, Name: r.Case, Passed: r.Passed, Start: r.Start, End: r.Start.Add(time.Duration(r.Ms) * time.Millisecond),
			Note: qtestNote(rep, r), Defect: r.Defect}
		if len(html) > 0 {
			res.Attach = []integrations.Attachment{{Name: "termwright-report.html", ContentType: "text/html", Data: html}}
		}
		pid, err := q.Submit(ctx, res)
		if err != nil {
			failed = append(failed, r.Case+": "+err.Error())
			continue
		}
		rep.Results[i].QTestRun = pid
		sent = append(sent, r.Case+" → "+pid)
	}
	if len(sent) > 0 {
		f.SaveReport(rep)
		f.Audit("send to qTest", strings.Join(sent, ", "))
	}
	var b strings.Builder
	if len(sent) > 0 {
		fmt.Fprintf(&b, "Sent to qTest: %s.", strings.Join(sent, ", "))
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, " No qTest test case (⋯ → qTest Test Case…): %s.", strings.Join(missing, ", "))
	}
	if len(failed) > 0 {
		return strings.TrimSpace(b.String()), errors.New(strings.Join(failed, "; "))
	}
	if b.Len() == 0 {
		return "", errors.New("nothing to send: the run has no tests that ran")
	}
	return strings.TrimSpace(b.String()), nil
}

func contains(l []string, x string) bool {
	for _, y := range l {
		if y == x {
			return true
		}
	}
	return false
}

func qtestNote(rep testrec.RunReport, r testrec.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Termwright run %s", rep.Dir)
	if rep.Testbed != "" {
		fmt.Fprintf(&b, " on %s", rep.Testbed)
	}
	for k, v := range r.Versions {
		fmt.Fprintf(&b, "\n%s: %s", k, v)
	}
	if !r.Passed {
		fmt.Fprintf(&b, "\nFailed: %s", testrec.FirstProblem(r))
	}
	return b.String()
}

// TestCreateDefect opens a Jira defect for a failed test of a run, with its steps, the failed
// output and the HTML report; the key is saved with the result. It returns the key and its link.
func (a *App) TestCreateDefect(dir, file string) ([]string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return nil, err
	}
	rep, err := f.LoadReport(dir)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, r := range rep.Results {
		if (r.File == file || r.Case == file) && !r.Passed && !r.Skipped {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, errors.New("no failed result of " + file + " in this run")
	}
	r := rep.Results[idx]
	if r.Defect != "" {
		return []string{r.Defect, r.DefectURL}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	key, link, err := a.jiraClient().Create(ctx, defectFor(f, rep, r))
	if key != "" {
		rep.Results[idx].Defect, rep.Results[idx].DefectURL = key, link
		f.SaveReport(rep)
		f.Audit("Jira defect", key+" for "+r.Case)
	}
	if err != nil {
		return []string{key, link}, err
	}
	return []string{key, link}, nil
}

func defectFor(f testrec.Folder, rep testrec.RunReport, r testrec.Result) integrations.Defect {
	var d strings.Builder
	fmt.Fprintf(&d, "Termwright test *%s* failed.\n\n*Problem:* %s\n*Test:* %s\n*Run:* %s\n", r.Case, testrec.FirstProblem(r), r.File, rep.Dir)
	if rep.Testbed != "" {
		fmt.Fprintf(&d, "*Testbed:* %s\n", rep.Testbed)
	}
	for k, v := range r.Versions {
		fmt.Fprintf(&d, "*%s:* %s\n", k, v)
	}
	d.WriteString("\n*Steps:*\n")
	var out strings.Builder
	for i, s := range r.Steps {
		st := map[bool]string{true: "passed", false: "FAILED"}[s.Passed]
		if s.Skipped {
			st = "skipped"
		}
		fmt.Fprintf(&d, "# %s - %s %s\n", st, s.Name, s.Target)
		for _, c := range s.Checks {
			if !c.Passed {
				fmt.Fprintf(&d, "** expected %s, got %q\n", c.What, c.Got)
			}
		}
		if !s.Passed && !s.Skipped && s.Output != "" {
			fmt.Fprintf(&out, "=== step %d: %s (%s)\n%s\n\n", i+1, s.Name, s.Target, s.Output)
		}
	}
	at := []integrations.Attachment{}
	if out.Len() > 0 {
		at = append(at, integrations.Attachment{Name: "failed-output.txt", ContentType: "text/plain", Data: []byte(out.String())})
	}
	if b, err := os.ReadFile(filepath.Join(f.Root, rep.Dir, "report.html")); err == nil {
		at = append(at, integrations.Attachment{Name: "termwright-report.html", ContentType: "text/html", Data: b})
	}
	sum := "Test failed: " + r.Case
	if p := testrec.FirstProblem(r); p != "" {
		sum += " - " + clip(p, 150)
	}
	return integrations.Defect{Summary: sum, Description: d.String(), Labels: []string{"termwright"}, Attach: at}
}

// afterRun sends a finished run to qTest and opens Jira defects when the Integrations say so
// (defects only for scheduled runs). Errors go to the log and a notification.
func (a *App) afterRun(rep testrec.RunReport, scheduled bool) {
	if rep.Dir == "" {
		return
	}
	cfg := a.integrationsCfg()
	if cfg.Jira.Auto && scheduled {
		for _, r := range rep.Results {
			if !r.Passed && !r.Skipped && r.File != "" {
				if _, err := a.TestCreateDefect(rep.Dir, r.File); err != nil {
					_ = a.Notify("Jira defect not opened", r.Case+": "+err.Error())
				}
			}
		}
	}
	if cfg.QTest.Auto {
		if _, err := a.TestSendQTest(rep.Dir, nil); err != nil {
			_ = a.Notify("qTest", err.Error())
		}
	}
}
