package testrec

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HistoryEntry is one test's result in one run (reports/history.jsonl, one JSON line each).
type HistoryEntry struct {
	At       time.Time         `json:"at"`
	Dir      string            `json:"dir"`
	File     string            `json:"file"`
	Case     string            `json:"case"`
	Passed   bool              `json:"passed"`
	Skipped  bool              `json:"skipped,omitempty"`
	Ms       int64             `json:"ms"`
	Testbed  string            `json:"testbed,omitempty"`
	Versions map[string]string `json:"versions,omitempty"`
	Problem  string            `json:"problem,omitempty"`
	RunBy    string            `json:"runBy,omitempty"`
	Flaky    bool              `json:"flaky,omitempty"`
}

func (f Folder) historyFile() string { return filepath.Join(f.Root, "reports", "history.jsonl") }

func (f Folder) appendHistory(rep RunReport) {
	fh, err := os.OpenFile(f.historyFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer fh.Close()
	for _, r := range rep.Results {
		e := HistoryEntry{At: r.Start, Dir: rep.Dir, File: r.File, Case: r.Case, Passed: r.Passed, Skipped: r.Skipped, Ms: r.Ms, Testbed: rep.Testbed, Versions: r.Versions, RunBy: rep.RunBy, Flaky: r.Flaky}
		if !r.Passed {
			e.Problem = FirstProblem(r)
		}
		b, _ := json.Marshal(e)
		fh.Write(append(b, '\n'))
	}
}

// FirstProblem: why a result failed, in one line.
func FirstProblem(r Result) string {
	if r.Error != "" {
		return r.Error
	}
	for i, s := range r.Steps {
		if s.Passed || s.Skipped {
			continue
		}
		if s.Error != "" {
			return s.Name + ": " + s.Error
		}
		for _, c := range s.Checks {
			if !c.Passed {
				return s.Name + ": " + c.What + ", got \"" + c.Got + "\""
			}
		}
		return s.Name + " failed (step " + itoa(i+1) + ")"
	}
	return ""
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

// TestHistory is a test's history with its numbers.
type TestHistory struct {
	File    string         `json:"file"`
	Runs    []HistoryEntry `json:"runs"` // newest first
	Passed  int            `json:"passed"`
	Failed  int            `json:"failed"`
	Skipped int            `json:"skipped"`
	// Flips: how often the result changed between passed and failed in these runs (flaky tests flip).
	Flips int   `json:"flips"`
	AvgMs int64 `json:"avgMs"`
	// ByVersion: passed / failed per software version of the test's devices.
	ByVersion []VersionStats `json:"byVersion"`
}

// VersionStats: results with one software version.
type VersionStats struct {
	Version string `json:"version"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
}

// History reads the last runs (at most limit) of one test, or of every test (file "").
func (f Folder) History(file string, limit int) []TestHistory {
	if limit <= 0 {
		limit = 50
	}
	byFile := map[string][]HistoryEntry{}
	fh, err := os.Open(f.historyFile())
	if err == nil {
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var e HistoryEntry
			if json.Unmarshal(sc.Bytes(), &e) != nil || file != "" && e.File != file {
				continue
			}
			byFile[e.File] = append(byFile[e.File], e)
		}
		fh.Close()
	}
	var out []TestHistory
	for fl, es := range byFile {
		if len(es) > limit {
			es = es[len(es)-limit:]
		}
		h := TestHistory{File: fl}
		var ms int64
		last := ""
		vs := map[string]*VersionStats{}
		for i := len(es) - 1; i >= 0; i-- {
			e := es[i]
			h.Runs = append(h.Runs, e)
			ms += e.Ms
			st := "passed"
			switch {
			case e.Skipped:
				h.Skipped++
				st = ""
			case e.Passed:
				h.Passed++
			default:
				h.Failed++
				st = "failed"
			}
			if st != "" {
				if last != "" && last != st {
					h.Flips++
				}
				last = st
				var ver []string
				for _, x := range e.Versions {
					ver = append(ver, x)
				}
				sort.Strings(ver)
				k := strings.Join(ver, ", ")
				if k == "" {
					k = "unknown"
				}
				if vs[k] == nil {
					vs[k] = &VersionStats{Version: k}
				}
				if e.Passed {
					vs[k].Passed++
				} else {
					vs[k].Failed++
				}
			}
		}
		if len(es) > 0 {
			h.AvgMs = ms / int64(len(es))
		}
		for _, v := range vs {
			h.ByVersion = append(h.ByVersion, *v)
		}
		sort.Slice(h.ByVersion, func(i, j int) bool { return h.ByVersion[i].Version < h.ByVersion[j].Version })
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

// LoadReport reads a run's result.json.
func (f Folder) LoadReport(dir string) (RunReport, error) {
	var r RunReport
	b, err := f.ReadText(strings.TrimSuffix(dir, "/") + "/result.json")
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal([]byte(b), &r)
}

// SaveReport writes a run's result.json again (after qTest runs or defects were added).
func (f Folder) SaveReport(r RunReport) error {
	p, err := f.path(r.Dir + "/result.json")
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return os.WriteFile(p, b, 0o644)
}

// CaseQTest: the qTest test case of a test file ("" = none).
func (f Folder) CaseQTest(file string) string {
	if c, err := f.Load(file); err == nil && c.QTest != nil {
		return strings.TrimSpace(c.QTest.TestCase)
	}
	return ""
}

// Audit adds a line to reports/audit.log: when, who, what (run, save, delete, send to qTest …).
func (f Folder) Audit(action, detail string) {
	u, h := me()
	fh, err := os.OpenFile(filepath.Join(f.Root, "reports", "audit.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer fh.Close()
	fmt := time.Now().Format("2006-01-02 15:04:05")
	fh.WriteString(fmt + "\t" + u + "@" + h + "\t" + action + "\t" + strings.ReplaceAll(detail, "\n", " ") + "\n")
}

// RunTrend is one run in a trend: its counts.
type RunTrend struct {
	Dir     string    `json:"dir"`
	At      time.Time `json:"at"`
	Passed  int       `json:"passed"`
	Failed  int       `json:"failed"`
	Skipped int       `json:"skipped"`
	Flaky   int       `json:"flaky"`
	Ms      int64     `json:"ms"`
	Testbed string    `json:"testbed,omitempty"`
	Version string    `json:"version,omitempty"`
}

// TestTrend is a test's record over the runs of a trend.
type TestTrend struct {
	File     string `json:"file"`
	Case     string `json:"case"`
	Runs     int    `json:"runs"`
	Failures int    `json:"failures"`
	Flips    int    `json:"flips"`
	Flaky    int    `json:"flaky"`
	Last     string `json:"last,omitempty"` // the last problem
}

// VersionTrend is the pass rate on one software version.
type VersionTrend struct {
	Version string `json:"version"`
	Passed  int    `json:"passed"`
	Total   int    `json:"total"`
}

// Trends is the whole suite over its runs.
type Trends struct {
	Runs     []RunTrend     `json:"runs"`
	Tests    []TestTrend    `json:"tests"` // the most failing first
	Versions []VersionTrend `json:"versions"`
}

// SuiteTrends reads the history: the last runs (only those tests when files is set, only that testbed when
// testbed is set), the tests that fail most, and pass rates per software version.
func (f Folder) SuiteTrends(files []string, testbed string, limit int) Trends {
	want := map[string]bool{}
	for _, x := range files {
		want[x] = true
	}
	fh, err := os.Open(f.historyFile())
	out := Trends{Runs: []RunTrend{}, Tests: []TestTrend{}, Versions: []VersionTrend{}}
	if err != nil {
		return out
	}
	defer fh.Close()
	runs := map[string]*RunTrend{}
	tests := map[string]*TestTrend{}
	lastPass := map[string]*bool{}
	vers := map[string]*VersionTrend{}
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e HistoryEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil || (len(want) > 0 && !want[e.File]) || (testbed != "" && e.Testbed != testbed) {
			continue
		}
		rt := runs[e.Dir]
		if rt == nil {
			rt = &RunTrend{Dir: e.Dir, At: e.At, Testbed: e.Testbed}
			runs[e.Dir] = rt
		}
		ver := versionText(e.Versions)
		if rt.Version == "" {
			rt.Version = ver
		}
		rt.Ms += e.Ms
		switch {
		case e.Skipped:
			rt.Skipped++
		case e.Passed:
			rt.Passed++
			if e.Flaky {
				rt.Flaky++
			}
		default:
			rt.Failed++
		}
		if e.Skipped {
			continue
		}
		tt := tests[e.File]
		if tt == nil {
			tt = &TestTrend{File: e.File, Case: e.Case}
			tests[e.File] = tt
		}
		tt.Runs++
		if !e.Passed {
			tt.Failures++
			tt.Last = e.Problem
		}
		if e.Flaky {
			tt.Flaky++
		}
		if lp := lastPass[e.File]; lp != nil && *lp != e.Passed {
			tt.Flips++
		}
		p := e.Passed
		lastPass[e.File] = &p
		if ver != "" {
			vt := vers[ver]
			if vt == nil {
				vt = &VersionTrend{Version: ver}
				vers[ver] = vt
			}
			vt.Total++
			if e.Passed {
				vt.Passed++
			}
		}
	}
	for _, r := range runs {
		out.Runs = append(out.Runs, *r)
	}
	sort.Slice(out.Runs, func(i, j int) bool { return out.Runs[i].At.Before(out.Runs[j].At) })
	if limit > 0 && len(out.Runs) > limit {
		out.Runs = out.Runs[len(out.Runs)-limit:]
	}
	for _, t := range tests {
		if t.Failures > 0 || t.Flaky > 0 {
			out.Tests = append(out.Tests, *t)
		}
	}
	sort.Slice(out.Tests, func(i, j int) bool {
		if out.Tests[i].Failures != out.Tests[j].Failures {
			return out.Tests[i].Failures > out.Tests[j].Failures
		}
		return out.Tests[i].Flips > out.Tests[j].Flips
	})
	if len(out.Tests) > 15 {
		out.Tests = out.Tests[:15]
	}
	for _, v := range vers {
		out.Versions = append(out.Versions, *v)
	}
	sort.Slice(out.Versions, func(i, j int) bool { return out.Versions[i].Version > out.Versions[j].Version })
	return out
}

// versionText is the software of a result in a few words ("33.7.1.6", or "sw1 33.7.1.6, sw2 9.1").
func versionText(v map[string]string) string {
	seen := map[string]bool{}
	var l []string
	for _, k := range sortedKeys(v) {
		if !seen[v[k]] {
			seen[v[k]] = true
			l = append(l, v[k])
		}
	}
	return strings.Join(l, ", ")
}
