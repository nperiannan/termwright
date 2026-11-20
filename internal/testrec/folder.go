package testrec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Folder is a tests folder: cases/ (the .yaml tests), lib/ (shared code of the generated tests),
// testbeds/ (which devices a run uses) and reports/ (one folder per run).
type Folder struct{ Root string }

// Dirs of a tests folder.
var Dirs = []string{"cases", "lib", "testbeds", "reports"}

// MaxCaseSize is the largest case file read (1 MB).
const MaxCaseSize = 1 << 20

// Init makes the folders and a README when they are missing (nothing is overwritten).
func (f Folder) Init() error {
	for _, d := range Dirs {
		if err := os.MkdirAll(filepath.Join(f.Root, d), 0o755); err != nil {
			return err
		}
	}
	readme := filepath.Join(f.Root, "README.md")
	if _, err := os.Stat(readme); errors.Is(err, fs.ErrNotExist) {
		return os.WriteFile(readme, []byte(readmeText), 0o644)
	}
	return nil
}

const readmeText = `# Tests

Termwright tests. Each test in cases/ is a YAML file of steps (a command on a saved session, an HTTP
request) and the checks on what came back. Run them in Termwright (Tests), from the local API
(POST /api/v1/tools/tests) or the Python client. testbeds/ says which devices a run uses; reports/
holds a JUnit XML file and an HTML report per run. Passwords are never written here: steps name
saved sessions, whose logins are in the computer's credential store.
`

// CaseInfo is a case in the folder's list.
type CaseInfo struct {
	// File: the path relative to the tests folder, e.g. cases/vlan/add-vlan.yaml.
	File string `json:"file"`
	// Name of the test.
	Name string `json:"name"`
	// Description of the test.
	Description string `json:"description,omitempty"`
	// Tags of the test.
	Tags []string `json:"tags,omitempty"`
	// Steps: how many.
	Steps int `json:"steps"`
	// Error: why the file could not be read.
	Error string `json:"error,omitempty"`
}

// List returns the cases under cases/ (sub-folders too), sorted by file.
func (f Folder) List() []CaseInfo {
	var out []CaseInfo
	root := filepath.Join(f.Root, "cases")
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !isCaseFile(p) {
			return nil
		}
		rel, _ := filepath.Rel(f.Root, p)
		ci := CaseInfo{File: filepath.ToSlash(rel)}
		c, err := f.Load(ci.File)
		if err != nil {
			ci.Name, ci.Error = strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())), err.Error()
		} else {
			ci.Name, ci.Description, ci.Tags, ci.Steps = c.Name, c.Description, c.Tags, len(c.Steps)
		}
		out = append(out, ci)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

func isCaseFile(p string) bool {
	e := strings.ToLower(filepath.Ext(p))
	return e == ".yaml" || e == ".yml"
}

// path makes rel a path inside the folder; a path leaving it is refused.
func (f Folder) path(rel string) (string, error) {
	rel = filepath.FromSlash(strings.TrimSpace(rel))
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%q: give a path inside the tests folder, e.g. cases/smoke.yaml", rel)
	}
	p := filepath.Join(f.Root, rel)
	if r, err := filepath.Rel(f.Root, p); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q is outside the tests folder", rel)
	}
	return p, nil
}

// Load reads a case (rel to the folder; "cases/" may be left out).
func (f Folder) Load(rel string) (Case, error) {
	p, err := f.find(rel)
	if err != nil {
		return Case{}, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return Case{}, err
	}
	if st.Size() > MaxCaseSize {
		return Case{}, fmt.Errorf("%s is larger than 1 MB", rel)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Case{}, err
	}
	c, err := Parse(b)
	if err != nil {
		return Case{}, fmt.Errorf("%s: %w", rel, err)
	}
	if c.Name == "" {
		c.Name = strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	}
	return c, nil
}

func (f Folder) find(rel string) (string, error) {
	p, err := f.path(rel)
	if err != nil {
		return "", err
	}
	if _, e := os.Stat(p); e == nil || strings.HasPrefix(filepath.ToSlash(rel), "cases/") {
		return p, nil
	}
	if q, e := f.path("cases/" + rel); e == nil {
		for _, x := range []string{q, q + ".yaml", q + ".yml"} {
			if _, e := os.Stat(x); e == nil {
				return x, nil
			}
		}
	}
	return p, nil
}

// Parse reads a case from YAML (JSON is YAML too) and checks its steps.
func Parse(b []byte) (Case, error) {
	var c Case
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, err
	}
	for i, s := range c.Steps {
		switch {
		case actions(s) == 0:
			return c, fmt.Errorf("step %d has none of run, http and snmp", i+1)
		case actions(s) > 1:
			return c, fmt.Errorf("step %d has more than one of run, http and snmp", i+1)
		case s.SNMP != nil && s.SNMP.Session == "":
			return c, fmt.Errorf("step %d: snmp needs a session", i+1)
		case s.SNMP != nil && strings.EqualFold(s.SNMP.Op, "set") && len(s.SNMP.Values) == 0:
			return c, fmt.Errorf("step %d: an snmp set needs values", i+1)
		case s.SNMP != nil && !strings.EqualFold(s.SNMP.Op, "set") && len(s.SNMP.OIDs) == 0:
			return c, fmt.Errorf("step %d: snmp needs oids", i+1)
		case s.Run != nil && (s.Run.Session == "" || s.Run.Command == ""):
			return c, fmt.Errorf("step %d: run needs a session and a command", i+1)
		case s.HTTP != nil && s.HTTP.URL == "":
			return c, fmt.Errorf("step %d: http needs a url", i+1)
		}
	}
	return c, nil
}

// Marshal writes a case as YAML.
func Marshal(c Case) ([]byte, error) { return yaml.Marshal(c) }

// Save writes a case (rel to the folder, under cases/); it returns the path written, relative.
func (f Folder) Save(rel string, c Case) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if !strings.HasPrefix(rel, "cases/") {
		rel = "cases/" + rel
	}
	if !isCaseFile(rel) {
		rel += ".yaml"
	}
	p, err := f.path(rel)
	if err != nil {
		return "", err
	}
	b, err := Marshal(c)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return rel, os.WriteFile(p, b, 0o644)
}

// RunReport is a run of one or more cases with its reports.
type RunReport struct {
	// Dir: the run's folder under reports/, relative to the tests folder.
	Dir string `json:"dir"`
	// Passed and Failed: cases.
	Passed int `json:"passed"`
	// Failed cases (errors included).
	Failed int `json:"failed"`
	// Ms: the whole run.
	Ms int64 `json:"ms"`
	// Results: one per case.
	Results []Result `json:"results"`
}

// RunFiles runs the cases (rel paths; "" or "all" runs every case) one after another and writes
// reports/<time>/result.json, junit.xml and report.html. progress, when set, is called after each case.
func (f Folder) RunFiles(ctx context.Context, files []string, d Driver, vars map[string]string, progress func(Result)) (RunReport, error) {
	if len(files) == 0 || len(files) == 1 && (files[0] == "" || strings.EqualFold(files[0], "all")) {
		files = nil
		for _, c := range f.List() {
			files = append(files, c.File)
		}
		if len(files) == 0 {
			return RunReport{}, errors.New("no tests in " + filepath.Join(f.Root, "cases"))
		}
	}
	start := time.Now()
	rep := RunReport{}
	for _, file := range files {
		var r Result
		if c, err := f.Load(file); err != nil {
			r = Result{Case: file, File: file, Start: time.Now(), Error: err.Error()}
		} else {
			r = Run(ctx, c, d, vars)
			r.File = file
		}
		if r.Passed {
			rep.Passed++
		} else {
			rep.Failed++
		}
		rep.Results = append(rep.Results, r)
		if progress != nil {
			progress(r)
		}
		if ctx.Err() != nil {
			break
		}
	}
	rep.Ms = time.Since(start).Milliseconds()
	dir := filepath.Join("reports", start.Format("2006-01-02_150405"))
	if len(files) == 1 {
		dir += "_" + slug(rep.Results[0].Case)
	}
	rep.Dir = filepath.ToSlash(dir)
	abs := filepath.Join(f.Root, dir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return rep, err
	}
	j, _ := json.MarshalIndent(rep, "", "  ")
	for name, b := range map[string][]byte{"result.json": j, "junit.xml": JUnit(rep), "report.html": HTML(rep)} {
		if err := os.WriteFile(filepath.Join(abs, name), b, 0o644); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
		if b.Len() >= 40 {
			return strings.Trim(b.String(), "-")
		}
	}
	return strings.Trim(b.String(), "-")
}

// Tree is what the Tests view lists: tests, testbeds and past runs.
type Tree struct {
	// Root: the tests folder.
	Root string `json:"root"`
	// Cases: the tests.
	Cases []CaseInfo `json:"cases"`
	// Testbeds: files in testbeds/ (relative to the folder).
	Testbeds []string `json:"testbeds"`
	// Reports: past runs, newest first (at most 30).
	Reports []ReportInfo `json:"reports"`
}

// ReportInfo is a past run.
type ReportInfo struct {
	// Dir: its folder, relative, e.g. reports/2026-11-20_164000.
	Dir string `json:"dir"`
	// Passed cases.
	Passed int `json:"passed"`
	// Failed cases.
	Failed int `json:"failed"`
	// Ms: how long the run took.
	Ms int64 `json:"ms"`
}

// Tree lists the folder.
func (f Folder) Tree() Tree {
	t := Tree{Root: f.Root, Cases: f.List(), Testbeds: []string{}, Reports: []ReportInfo{}}
	if t.Cases == nil {
		t.Cases = []CaseInfo{}
	}
	if es, err := os.ReadDir(filepath.Join(f.Root, "testbeds")); err == nil {
		for _, e := range es {
			if !e.IsDir() && isCaseFile(e.Name()) {
				t.Testbeds = append(t.Testbeds, "testbeds/"+e.Name())
			}
		}
	}
	if es, err := os.ReadDir(filepath.Join(f.Root, "reports")); err == nil {
		for i := len(es) - 1; i >= 0 && len(t.Reports) < 30; i-- {
			if !es[i].IsDir() {
				continue
			}
			ri := ReportInfo{Dir: "reports/" + es[i].Name()}
			if b, err := os.ReadFile(filepath.Join(f.Root, "reports", es[i].Name(), "result.json")); err == nil {
				var r RunReport
				if json.Unmarshal(b, &r) == nil {
					ri.Passed, ri.Failed, ri.Ms = r.Passed, r.Failed, r.Ms
				}
			}
			t.Reports = append(t.Reports, ri)
		}
	}
	return t
}

// Signature changes when a file under cases/, testbeds/ or reports/ is added, removed or changed:
// the Tests view polls it to see edits made outside (an editor, git pull).
func (f Folder) Signature() string {
	h := fnv.New64a()
	for _, d := range []string{"cases", "testbeds", "reports"} {
		filepath.WalkDir(filepath.Join(f.Root, d), func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if fi, err := e.Info(); err == nil {
				fmt.Fprintf(h, "%s|%d|%d\n", p, fi.Size(), fi.ModTime().UnixNano())
			}
			return nil
		})
	}
	return fmt.Sprintf("%x", h.Sum64())
}

// ReadText reads a text file of the folder (a test's YAML or Go, a testbed, a report), at most 2 MB.
func (f Folder) ReadText(rel string) (string, error) {
	p, err := f.path(rel)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if st.Size() > 2<<20 {
		return "", fmt.Errorf("%s is larger than 2 MB", rel)
	}
	b, err := os.ReadFile(p)
	return string(b), err
}

// Abs is rel as a path on disk (refused outside the folder).
func (f Folder) Abs(rel string) (string, error) { return f.path(rel) }

// Testbed reads a testbed file's values as variables: "name: value" lines, nested maps as
// role.field (lab: {sw1: Lab/core-sw1} → {{sw1}}… and {{lab.sw1}}).
func (f Folder) Testbed(rel string) (map[string]string, error) {
	if rel == "" {
		return nil, nil
	}
	if !strings.Contains(rel, "/") {
		rel = "testbeds/" + rel
	}
	txt, err := f.ReadText(rel)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(txt), &m); err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	out := map[string]string{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				walk(strings.TrimPrefix(prefix+"."+k, "."), x)
			}
		case nil:
			out[prefix] = ""
		default:
			out[prefix] = fmt.Sprint(t)
		}
	}
	walk("", m)
	return out, nil
}
