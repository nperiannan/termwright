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
}

func (f Folder) historyFile() string { return filepath.Join(f.Root, "reports", "history.jsonl") }

func (f Folder) appendHistory(rep RunReport) {
	fh, err := os.OpenFile(f.historyFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer fh.Close()
	for _, r := range rep.Results {
		e := HistoryEntry{At: r.Start, Dir: rep.Dir, File: r.File, Case: r.Case, Passed: r.Passed, Skipped: r.Skipped, Ms: r.Ms, Testbed: rep.Testbed, Versions: r.Versions}
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
