package testrec

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// CallStep runs a procedure: a shared set of steps in procs/<name>.yaml with parameters.
type CallStep struct {
	// Proc: the procedure's name (procs/<name>.yaml).
	Proc string `yaml:"proc" json:"proc"`
	// With: its parameters ({{name}} in its steps); values may use the test's variables.
	With map[string]string `yaml:"with,omitempty" json:"with,omitempty"`
}

// Proc is a reusable procedure: steps with parameters, called from tests with call: {proc, with}.
type Proc struct {
	Name        string `yaml:"name,omitempty" json:"name,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Params: the parameter names, with an optional default ("vlan", "port=1:1").
	Params []string `yaml:"params,omitempty" json:"params,omitempty"`
	Steps  []Step   `yaml:"steps" json:"steps"`
}

// ProcInfo is a procedure in a list.
type ProcInfo struct {
	File        string   `json:"file"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Params      []string `json:"params,omitempty"`
	Steps       int      `json:"steps"`
	Error       string   `json:"error,omitempty"`
}

// LoadProc reads procs/<name>.yaml.
func (f Folder) LoadProc(name string) (Proc, error) {
	var p Proc
	n := strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(name), "procs/"), ".yaml")
	if n == "" || strings.Contains(n, "..") {
		return p, fmt.Errorf("procedure %q", name)
	}
	b, err := os.ReadFile(filepath.Join(f.Root, "procs", filepath.FromSlash(n)+".yaml"))
	if err != nil {
		return p, fmt.Errorf("procedure %s: %v", n, err)
	}
	if err := yaml.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("procedure %s: %v", n, err)
	}
	if p.Name == "" {
		p.Name = n
	}
	return p, nil
}

// Procs lists procs/*.yaml.
func (f Folder) Procs() []ProcInfo {
	out := []ProcInfo{}
	_ = filepath.WalkDir(filepath.Join(f.Root, "procs"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		rel, _ := filepath.Rel(filepath.Join(f.Root, "procs"), path)
		n := strings.TrimSuffix(filepath.ToSlash(rel), ".yaml")
		i := ProcInfo{File: "procs/" + n + ".yaml", Name: n}
		if p, err := f.LoadProc(n); err != nil {
			i.Error = err.Error()
		} else {
			i.Name, i.Description, i.Params, i.Steps = p.Name, p.Description, p.Params, len(p.Steps)
		}
		out = append(out, i)
		return nil
	})
	sort.Slice(out, func(a, b int) bool { return out[a].File < out[b].File })
	return out
}

// SaveProc writes procs/<name>.yaml.
func (f Folder) SaveProc(name string, p Proc) (string, error) {
	n := slug(name)
	if p.Name == "" {
		p.Name = name
	}
	b, err := yaml.Marshal(p)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(f.Root, "procs"), 0o755); err != nil {
		return "", err
	}
	rel := "procs/" + n + ".yaml"
	return rel, os.WriteFile(filepath.Join(f.Root, rel), b, 0o644)
}

// subst replaces {{param}} in a step (all its text) with the call's values.
func subst(st Step, vals map[string]string) Step {
	b, err := json.Marshal(st)
	if err != nil {
		return st
	}
	s := varRef.ReplaceAllStringFunc(string(b), func(m string) string {
		k := varRef.FindStringSubmatch(m)[1]
		if v, ok := vals[k]; ok {
			j, _ := json.Marshal(v)
			return strings.Trim(string(j), `"`)
		}
		return m
	})
	var out Step
	if json.Unmarshal([]byte(s), &out) != nil {
		return st
	}
	return out
}

// ExpandCalls replaces call steps by the procedures' steps (their parameters filled in; names prefixed
// with the procedure's), in setup, steps, cleanup and loops; procedures may call others (depth 8).
func (f Folder) ExpandCalls(c Case) (Case, error) {
	var err error
	if c.Setup, err = f.expandList(c.Setup, 0); err != nil {
		return c, err
	}
	if c.Steps, err = f.expandList(c.Steps, 0); err != nil {
		return c, err
	}
	if c.Cleanup, err = f.expandList(c.Cleanup, 0); err != nil {
		return c, err
	}
	for _, l := range [][]Step{c.Setup, c.Steps, c.Cleanup} {
		if err := f.expandStepBlocks(l); err != nil {
			return c, err
		}
		applyDefaults(l, c.Defaults)
	}
	return c, nil
}

func (f Folder) expandList(l []Step, depth int) ([]Step, error) {
	if depth > 8 {
		return nil, fmt.Errorf("procedures call each other more than 8 deep")
	}
	var out []Step
	for i, st := range l {
		if st.Loop != nil {
			inner, err := f.expandList(st.Loop.Steps, depth)
			if err != nil {
				return nil, err
			}
			st.Loop.Steps = inner
		}
		if st.Call == nil {
			out = append(out, st)
			continue
		}
		p, err := f.LoadProc(st.Call.Proc)
		if err != nil {
			return nil, fmt.Errorf("step %d: %v", i+1, err)
		}
		vals := map[string]string{}
		for _, prm := range p.Params {
			if k, d, ok := strings.Cut(prm, "="); ok {
				vals[strings.TrimSpace(k)] = strings.TrimSpace(d)
			}
		}
		for k, v := range st.Call.With {
			vals[k] = v
		}
		for _, prm := range p.Params {
			k, _, _ := strings.Cut(prm, "=")
			if _, ok := vals[strings.TrimSpace(k)]; !ok {
				return nil, fmt.Errorf("step %d: procedure %s needs %s", i+1, p.Name, strings.TrimSpace(k))
			}
		}
		steps := make([]Step, len(p.Steps))
		for j, ps := range p.Steps {
			steps[j] = subst(ps, vals)
			if steps[j].Name == "" {
				steps[j].Name = stepName(steps[j], j)
			}
			steps[j].Name = p.Name + " › " + steps[j].Name
			if st.ContinueOnFail {
				steps[j].ContinueOnFail = true
			}
			if st.If != "" && steps[j].If == "" {
				steps[j].If = st.If
			}
		}
		inner, err := f.expandList(steps, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, inner...)
	}
	return out, nil
}

// DataRows reads a test's data file (CSV with a header line, or an Excel sheet) as one params entry per row.
func (f Folder) DataRows(rel string) ([]map[string]string, error) {
	p, err := f.path(rel)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("data %s: %v", rel, err)
	}
	var rows [][]string
	if strings.EqualFold(filepath.Ext(rel), ".xlsx") {
		if rows, err = XLSXRows(b); err != nil {
			return nil, err
		}
	} else {
		r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(b), "\ufeff")))
		r.FieldsPerRecord, r.TrimLeadingSpace = -1, true
		if rows, err = r.ReadAll(); err != nil {
			return nil, fmt.Errorf("data %s: %v", rel, err)
		}
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("data %s needs a header line and at least one row", rel)
	}
	var out []map[string]string
	for _, row := range rows[1:] {
		m := map[string]string{}
		empty := true
		for i, h := range rows[0] {
			if h = strings.TrimSpace(h); h == "" {
				continue
			}
			if i < len(row) {
				m[h] = strings.TrimSpace(row[i])
				empty = empty && m[h] == ""
			}
		}
		if !empty {
			out = append(out, m)
		}
	}
	return out, nil
}

// Prepare makes a loaded test ready to run or to write as code: calls expanded, data rows as params.
func (f Folder) Prepare(c Case) (Case, error) {
	c = f.withSuite(c)
	c, err := f.ExpandCalls(c)
	if err != nil {
		return c, err
	}
	if c.Data != "" {
		rows, err := f.DataRows(c.Data)
		if err != nil {
			return c, err
		}
		c.Params = append(c.Params, rows...)
	}
	return c, nil
}

// ownOptions says whether a step sets step options itself.
func ownOptions(st Step) bool {
	return st.Timeout != 0 || st.Wait != 0 || st.Retry != nil || st.ContinueOnFail || st.Run != nil && st.Run.IgnoreErrors
}

// applyDefaults gives the steps that use the test's defaults (options "" without options of their own,
// or "default") the test's step options.
func applyDefaults(l []Step, d *StepOptions) {
	if d == nil {
		return
	}
	for i := range l {
		st := &l[i]
		if st.Loop != nil {
			applyDefaults(st.Loop.Steps, d)
		}
		if st.Options == "none" || st.Options == "custom" || st.Options == "" && ownOptions(*st) || st.Manual != nil || st.Loop != nil {
			continue
		}
		st.Timeout, st.Wait, st.ContinueOnFail = d.Timeout, d.Wait, d.ContinueOnFail
		if d.Retry != nil {
			r := *d.Retry
			st.Retry = &r
		}
		if st.Run != nil && d.IgnoreErrors {
			r := *st.Run
			r.IgnoreErrors = true
			st.Run = &r
		}
	}
}
