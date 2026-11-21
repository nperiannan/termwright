package testrec

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Folder is a tests folder: cases/ (the .yaml tests), lib/ (shared code of the generated tests),
// testbeds/ (which devices a run uses) and reports/ (one folder per run).
type Folder struct{ Root string }

// builtinTemplates: TextFSM templates and their index, written to templates/ when missing.
//
//go:embed templates
var builtinTemplates embed.FS

// TemplatesDir is where the TextFSM templates are.
func (f Folder) TemplatesDir() string { return filepath.Join(f.Root, "templates") }

// Dirs of a tests folder.
var Dirs = []string{"cases", "lib", "testbeds", "reports", "templates", "suites"}

// MaxCaseSize is the largest case file read (1 MB).
const MaxCaseSize = 1 << 20

// Init makes the folders and a README when they are missing (nothing is overwritten).
func (f Folder) Init() error {
	for _, d := range Dirs {
		if err := os.MkdirAll(filepath.Join(f.Root, d), 0o755); err != nil {
			return err
		}
	}
	if gi := filepath.Join(f.Root, ".gitignore"); !exists(gi) {
		if err := os.WriteFile(gi, []byte("# run reports and testbed locks are kept out of Git\nreports/\nlocks/\n"), 0o644); err != nil {
			return err
		}
	}
	ents, _ := builtinTemplates.ReadDir("templates")
	for _, e := range ents {
		dst := filepath.Join(f.Root, "templates", e.Name())
		if !exists(dst) {
			b, _ := builtinTemplates.ReadFile("templates/" + e.Name())
			if err := os.WriteFile(dst, b, 0o644); err != nil {
				return err
			}
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
	for _, r := range c.Restore {
		if r != "config" && r != "lab" {
			return c, fmt.Errorf("restore %q: use config or lab", r)
		}
	}
	all := append(append(append([]Step{}, c.Setup...), c.Steps...), c.Cleanup...)
	return c, checkSteps(all, "")
}

func checkSteps(all []Step, in string) error {
	for i, s := range all {
		if s.Loop != nil {
			if s.Run != nil || s.HTTP != nil || s.SNMP != nil {
				return fmt.Errorf("%sstep %d: a loop step has only its loop", in, i+1)
			}
			if s.Loop.Var == "" || len(s.Loop.Steps) == 0 || len(s.Loop.Values) == 0 && s.Loop.Range == "" && s.Loop.From == "" {
				return fmt.Errorf("%sstep %d: loop needs var, values (or range or from) and steps", in, i+1)
			}
			if s.Loop.Range != "" {
				if _, err := rangeValues(s.Loop.Range); err != nil {
					return fmt.Errorf("%sstep %d: %v", in, i+1, err)
				}
			}
			if err := checkSteps(s.Loop.Steps, fmt.Sprintf("%sloop of step %d: ", in, i+1)); err != nil {
				return err
			}
			continue
		}
		if actions(s) == 0 && s.Wait > 0 {
			continue
		}
		switch {
		case actions(s) == 0:
			return fmt.Errorf("%sstep %d has none of run, http, snmp, traffic, loop and wait", in, i+1)
		case actions(s) > 1:
			return fmt.Errorf("%sstep %d has more than one of run, http, snmp and traffic", in, i+1)
		case s.SNMP != nil && s.SNMP.Session == "":
			return fmt.Errorf("%sstep %d: snmp needs a session", in, i+1)
		case s.SNMP != nil && strings.EqualFold(s.SNMP.Op, "set") && len(s.SNMP.Values) == 0:
			return fmt.Errorf("%sstep %d: an snmp set needs values", in, i+1)
		case s.SNMP != nil && !strings.EqualFold(s.SNMP.Op, "set") && len(s.SNMP.OIDs) == 0:
			return fmt.Errorf("%sstep %d: snmp needs oids", in, i+1)
		case s.Upgrade != nil && (s.Upgrade.Session == "" || s.Upgrade.Image == ""):
			return fmt.Errorf("%sstep %d: upgrade needs a session and an image", in, i+1)
		case s.Traffic != nil && s.Traffic.Test == "":
			return fmt.Errorf("%sstep %d: traffic needs a test (a saved lab traffic test)", in, i+1)
		case s.Run != nil && (s.Run.Session == "" || s.Run.Command == ""):
			return fmt.Errorf("%sstep %d: run needs a session and a command", in, i+1)
		case s.HTTP != nil && s.HTTP.URL == "":
			return fmt.Errorf("%sstep %d: http needs a url", in, i+1)
		}
	}
	return nil
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
	// RunBy: who ran it (user@computer).
	RunBy string `json:"runBy,omitempty"`
	// Versions: the software each device ran at the start of the run (session → version).
	Versions map[string]string `json:"versions,omitempty"`
	// Testbed the run used ("" = the tests' own sessions) and its cleanup mode.
	Testbed string `json:"testbed,omitempty"`
	Cleanup string `json:"cleanup,omitempty"`
	// Dir: the run's folder under reports/, relative to the tests folder.
	Dir string `json:"dir"`
	// Passed and Failed: cases.
	Passed int `json:"passed"`
	// Failed cases (errors included).
	Failed int `json:"failed"`
	// Skipped cases (the run stopped after a failed cleanup).
	Skipped int `json:"skipped,omitempty"`
	// Ms: the whole run.
	Ms int64 `json:"ms"`
	// OnCleanupFail: what the run did when a cleanup failed: stop, restore or factory.
	OnCleanupFail string `json:"onCleanupFail,omitempty"`
	// Recovery: saving the devices' configuration before the run and the recoveries after failed
	// cleanups, in order.
	Recovery []StepResult `json:"recovery,omitempty"`
	// Results: one per case.
	Results []Result `json:"results"`
}

// RunOptions are how a run of several cases goes.
type RunOptions struct {
	// OnCleanupFail: when a case's cleanup fails - "stop" (default: the remaining cases are skipped),
	// "restore" (the configuration saved before the run is put back, then the run goes on) or
	// "factory" (the device type's factory-default commands, then the run goes on).
	OnCleanupFail string `json:"onCleanupFail,omitempty"`
	// Cleanup: "" (each test's cleanup steps and restore), "off" (none: tests build on what earlier
	// ones configured) or "auto" (the cleanup steps; without them the undo of the commands that ran).
	Cleanup string `json:"cleanup,omitempty"`
	// Tags: only tests with one of these tags; a tag starting with ! leaves its tests out.
	Tags []string `json:"tags,omitempty"`
	// OnStepStart is called before each step with its name (live progress).
	OnStepStart func(file, caseName, step string) `json:"-"`
	// OnStep is called after each step (live progress).
	OnStep func(file, caseName string, sr StepResult) `json:"-"`
	// From: start at this step of the steps (1-based; the setup still runs, earlier steps are skipped).
	From int `json:"from,omitempty"`
	// Only: run only this step of the steps (no setup).
	Only int `json:"only,omitempty"`
	// Pause is called when a setup or test step fails (debugging): "retry" runs the step again,
	// "continue" goes on as if it passed, anything else stops (the cleanup still runs). The devices'
	// connections stay open while it waits.
	Pause func(ctx context.Context, file, caseName string, sr StepResult) string `json:"-"`
	file   string
	// Testbed: shown in the report.
	Testbed string `json:"testbed,omitempty"`
}

// Recoverer is a Driver that can bring devices back after a failed cleanup: Baseline saves their
// configuration before the run (mode restore); Recover puts it back or resets them to factory default
// (mode factory), waiting for a restart.
type Recoverer interface {
	Baseline(ctx context.Context, sessions []string) []StepResult
	Recover(ctx context.Context, mode string, sessions []string) []StepResult
}

// RunFiles runs the cases (rel paths; "" or "all" runs every case) one after another and writes
// reports/<time>/result.json, junit.xml and report.html. progress, when set, is called after each case.
func (f Folder) RunFiles(ctx context.Context, files []string, d Driver, vars map[string]string, progress func(Result)) (RunReport, error) {
	return f.RunFilesWith(ctx, files, d, vars, RunOptions{}, progress)
}

// RunFilesWith is RunFiles with options (what to do when a cleanup fails).
func (f Folder) RunFilesWith(ctx context.Context, files []string, d Driver, vars map[string]string, opt RunOptions, progress func(Result)) (RunReport, error) {
	schemaRoot.Store(f.Root)
	files, err := f.ExpandFiles(files, opt.Tags)
	if err != nil {
		return RunReport{}, err
	}
	if len(files) == 0 {
		return RunReport{}, errors.New("no tests to run in " + filepath.Join(f.Root, "cases"))
	}
	start := time.Now()
	mode := opt.OnCleanupFail
	if mode != "restore" && mode != "factory" {
		mode = "stop"
	}
	u, h := me()
	rep := RunReport{OnCleanupFail: mode, Testbed: opt.Testbed, Cleanup: opt.Cleanup, RunBy: u + "@" + h}
	rc, canRecover := d.(Recoverer)
	cases := map[string]Case{}
	var all []string // every device the run uses, for the configuration saved before it
	for _, file := range files {
		if c, err := f.Load(file); err == nil {
			cases[file] = c
			v := map[string]string{}
			for k, x := range c.Vars {
				v[k] = x
			}
			for k, x := range vars {
				v[k] = x
			}
			for _, s := range Sessions(c, v) {
				if !slices.Contains(all, s) {
					all = append(all, s)
				}
			}
		}
	}
	if vr, ok := d.(Versioner); ok && len(all) > 0 {
		rep.Versions = vr.Versions(ctx, all)
	}
	if mode != "stop" && !canRecover {
		return rep, errors.New("this runner cannot restore or reset devices after a failed cleanup")
	}
	if mode == "restore" && len(all) > 0 {
		for _, sr := range rc.Baseline(ctx, all) {
			sr.Phase = "recovery"
			rep.Recovery = append(rep.Recovery, sr)
			if !sr.Passed {
				return rep, fmt.Errorf("the configuration of %s could not be saved before the run: %s", sr.Target, sr.Error)
			}
		}
	}
	stopped := ""                 // why the remaining cases are skipped
	status := map[string]string{} // file → passed, failed or skipped (for dependsOn)
	for _, file := range files {
		var rs []Result
		c, loaded := cases[file]
		switch {
		case stopped != "":
			name := file
			if loaded && c.Name != "" {
				name = c.Name
			}
			rs = []Result{{Case: name, File: file, Start: time.Now(), Skipped: true, Error: stopped}}
		case !loaded:
			_, err := f.Load(file)
			rs = []Result{{Case: file, File: file, Start: time.Now(), Error: err.Error()}}
		case f.blockedBy(c, status) != "":
			name := c.Name
			if name == "" {
				name = file
			}
			rs = []Result{{Case: name, File: file, Start: time.Now(), Skipped: true, Error: f.blockedBy(c, status)}}
		default:
			o := opt
			o.file = file
			rs = RunParamsWith(ctx, c, d, vars, o)
		}
		for _, r := range rs {
			r.File = file
			if len(rep.Versions) > 0 && loaded {
				v := map[string]string{}
				for k, x := range c.Vars {
					v[k] = x
				}
				for k, x := range vars {
					v[k] = x
				}
				for _, s := range Sessions(c, v) {
					if x := rep.Versions[s]; x != "" {
						if r.Versions == nil {
							r.Versions = map[string]string{}
						}
						r.Versions[s] = x
					}
				}
			}
			switch {
			case r.Skipped && status[file] == "":
				status[file] = "skipped"
			case !r.Passed && !r.Skipped:
				status[file] = "failed"
			case status[file] == "":
				status[file] = "passed"
			}
			switch {
			case r.Skipped:
				rep.Skipped++
			case r.Passed:
				rep.Passed++
			default:
				rep.Failed++
			}
			rep.Results = append(rep.Results, r)
			if progress != nil {
				progress(r)
			}
			if !r.CleanupFailed || stopped != "" {
				continue
			}
			// a failed cleanup: the devices may be dirty for the next test
			if mode == "stop" {
				stopped = fmt.Sprintf("skipped: the cleanup of %q failed, so the devices may not be clean (on a failed cleanup: stop the run)", r.Case)
				continue
			}
			v := map[string]string{}
			for k, x := range c.Vars {
				v[k] = x
			}
			for k, x := range vars {
				v[k] = x
			}
			ok := true
			for _, sr := range rc.Recover(ctx, mode, Sessions(c, v)) {
				sr.Phase = "recovery"
				rep.Recovery = append(rep.Recovery, sr)
				ok = ok && sr.Passed
			}
			if !ok {
				stopped = fmt.Sprintf("skipped: the cleanup of %q failed and bringing the devices back (%s) failed too", r.Case, map[string]string{"restore": "restoring the saved configuration", "factory": "factory default"}[mode])
			}
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
	if opt.Testbed != "" {
		dir += "_" + slug(strings.TrimSuffix(filepath.Base(opt.Testbed), filepath.Ext(opt.Testbed)))
	}
	for n, base := 2, dir; ; n++ { // parallel runs may start in the same second
		if _, err := os.Stat(filepath.Join(f.Root, dir)); err != nil {
			break
		}
		dir = fmt.Sprintf("%s-%d", base, n)
	}
	rep.Dir = filepath.ToSlash(dir)
	abs := filepath.Join(f.Root, dir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return rep, err
	}
	f.writeLogs(&rep, abs)
	j, _ := json.MarshalIndent(rep, "", "  ")
	for name, b := range map[string][]byte{"result.json": j, "junit.xml": JUnit(rep), "report.html": HTML(rep), "results.xlsx": XLSX(rep)} {
		if err := os.WriteFile(filepath.Join(abs, name), b, 0o644); err != nil {
			return rep, err
		}
	}
	f.appendHistory(rep)
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
	// Lib: the files in lib/ (the helpers of the generated tests and your own).
	Lib []string `json:"lib"`
	// Templates: the TextFSM templates and their index in templates/.
	Templates []string `json:"templates"`
	// Suites: named sets of tests in suites/.
	Suites []SuiteInfo `json:"suites"`
	// Locks: testbeds in use.
	Locks []Lock `json:"locks"`
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
	// Skipped cases and the testbed.
	Skipped int    `json:"skipped,omitempty"`
	Testbed string `json:"testbed,omitempty"`
}

// Tree lists the folder.
func (f Folder) Tree() Tree {
	t := Tree{Root: f.Root, Cases: f.List(), Testbeds: []string{}, Reports: []ReportInfo{}, Lib: f.LibFiles(), Templates: f.dirFiles("templates"), Suites: f.Suites(), Locks: f.Locks()}
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
					ri.Passed, ri.Failed, ri.Ms, ri.Skipped, ri.Testbed = r.Passed, r.Failed, r.Ms, r.Skipped, r.Testbed
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

// WriteTestbed writes testbeds/<name>.yaml with a role per session (RoleName → path), sorted, with
// a comment line; an existing file is not replaced. It returns the file, relative.
func (f Folder) WriteTestbed(name string, sessions []string, comment string) (string, error) {
	name = slug(strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml"))
	if name == "" {
		return "", errors.New("give the testbed a name")
	}
	rel := "testbeds/" + name + ".yaml"
	p, err := f.path(rel)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(p); err == nil {
		return "", fmt.Errorf("%s is already there", rel)
	}
	m := map[string]string{}
	for _, s := range sessions {
		r := RoleName(s)
		for i := 2; m[r] != "" && m[r] != s; i++ {
			r = RoleName(s) + "_" + strconv.Itoa(i)
		}
		m[r] = s
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	if comment != "" {
		b.WriteString("# " + comment + "\n")
	}
	b.WriteString("# role: saved session path - tests use {{role}}\n")
	for _, k := range keys {
		y, _ := yaml.Marshal(map[string]string{k: m[k]})
		b.Write(y)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return rel, os.WriteFile(p, []byte(b.String()), 0o644)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// Remove moves a test and its generated code (Go, Python, TCL) to .removed/<time>/ in the tests folder:
// nothing is deleted. It returns where they went.
func (f Folder) Remove(rel string) (string, error) {
	p, err := f.find(rel)
	if err != nil {
		return "", err
	}
	r, _ := filepath.Rel(f.Root, p)
	r = filepath.ToSlash(r)
	files := []string{r, GoFile(r), PyFile(r), TclFile(r)}
	switch {
	case (strings.HasPrefix(r, "testbeds/") || strings.HasPrefix(r, "suites/")) && isCaseFile(r):
		files = []string{r} // a testbed or a plan
	case !strings.HasPrefix(r, "cases/") || !isCaseFile(r):
		return "", fmt.Errorf("%s is not a test, testbed or plan", rel)
	}
	dest := filepath.Join(".removed", time.Now().Format("2006-01-02_150405"))
	for _, x := range files {
		src := filepath.Join(f.Root, filepath.FromSlash(x))
		if _, err := os.Stat(src); err != nil {
			continue
		}
		to := filepath.Join(f.Root, dest, filepath.FromSlash(x))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return "", err
		}
		if err := os.Rename(src, to); err != nil {
			return "", err
		}
	}
	return filepath.ToSlash(dest), nil
}

// Rename moves a test to another file in cases/ (its generated code is written again there, the old
// code goes to .removed/); copy keeps the original (Duplicate). It returns the new file.
func (f Folder) Rename(rel, to string, copy bool) (string, error) {
	c, err := f.Load(rel)
	if err != nil {
		return "", err
	}
	src, _ := f.find(rel)
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	to = filepath.ToSlash(strings.TrimSpace(to))
	if !strings.HasPrefix(to, "cases/") {
		to = "cases/" + to
	}
	if !isCaseFile(to) {
		to += ".yaml"
	}
	dst, err := f.path(to)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dst); err == nil {
		return "", fmt.Errorf("%s is already there", to)
	}
	if copy && c.Name != "" {
		var m map[string]any
		if yaml.Unmarshal(b, &m) == nil {
			c.Name += " (copy)"
			if nb, err := Marshal(c); err == nil {
				b = nb
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return "", err
	}
	if !copy {
		if _, err := f.Remove(rel); err != nil {
			return to, err
		}
	}
	_, err = f.WriteGo(to)
	return to, err
}

// LibFiles lists the files under lib/ (relative to the folder), at most 300.
func (f Folder) LibFiles() []string {
	out := []string{}
	root := filepath.Join(f.Root, "lib")
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || len(out) >= 300 {
			return nil
		}
		if d.IsDir() && p != root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			r, _ := filepath.Rel(f.Root, p)
			out = append(out, filepath.ToSlash(r))
		}
		return nil
	})
	return out
}

// dirFiles lists the files directly in a folder of the tests folder (relative paths), at most 300.
func (f Folder) dirFiles(dir string) []string {
	out := []string{}
	es, _ := os.ReadDir(filepath.Join(f.Root, dir))
	for _, e := range es {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") && len(out) < 300 {
			out = append(out, dir+"/"+e.Name())
		}
	}
	return out
}

// SaveText writes a testbed, template, suite or library file (not tests: those are checked first).
func (f Folder) SaveText(rel, text string) error {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	ok := false
	for _, d := range []string{"testbeds/", "templates/", "suites/", "lib/"} {
		ok = ok || strings.HasPrefix(rel, d)
	}
	if !ok {
		return fmt.Errorf("%s: only testbeds, templates, suites and lib files are saved this way", rel)
	}
	p, err := f.path(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(text), 0o644)
}
