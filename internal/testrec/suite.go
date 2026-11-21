package testrec

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Suite is a named set of tests (suites/<name>.yaml): files, folders of cases/ or other suites,
// narrowed by tags.
type Suite struct {
	Name        string `yaml:"name,omitempty" json:"name,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Tests: cases/… files, folders (cases/vlan) or suites/… files, in the order to run them.
	Tests []string `yaml:"tests,omitempty" json:"tests,omitempty"`
	// Tags: only tests with one of these; a tag starting with ! leaves its tests out.
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// SuiteInfo is a suite in the folder's list.
type SuiteInfo struct {
	File  string `json:"file"`
	Name  string `json:"name"`
	Tests int    `json:"tests"`
	Error string `json:"error,omitempty"`
}

// LoadSuite reads a suite file.
func (f Folder) LoadSuite(rel string) (Suite, error) {
	var s Suite
	b, err := f.ReadText(rel)
	if err != nil {
		return s, err
	}
	if err := yaml.Unmarshal([]byte(b), &s); err != nil {
		return s, fmt.Errorf("%s: %v", rel, err)
	}
	return s, nil
}

// Suites lists suites/*.yaml with how many tests each runs.
func (f Folder) Suites() []SuiteInfo {
	out := []SuiteInfo{}
	for _, rel := range f.dirFiles("suites") {
		if !isCaseFile(rel) {
			continue
		}
		si := SuiteInfo{File: rel, Name: strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))}
		if s, err := f.LoadSuite(rel); err != nil {
			si.Error = err.Error()
		} else {
			if s.Name != "" {
				si.Name = s.Name
			}
			fs, err := f.ExpandFiles([]string{rel}, nil)
			if err != nil {
				si.Error = err.Error()
			}
			si.Tests = len(fs)
		}
		out = append(out, si)
	}
	return out
}

// SaveSuite writes a suite.
func (f Folder) SaveSuite(rel string, s Suite) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if !strings.HasPrefix(rel, "suites/") {
		rel = "suites/" + rel
	}
	if !isCaseFile(rel) {
		rel += ".yaml"
	}
	b, err := yaml.Marshal(s)
	if err != nil {
		return "", err
	}
	return rel, f.SaveText(rel, string(b))
}

// ExpandFiles turns what a run was given - nothing or "all" (every test), files, folders of cases/
// and suites - into test files, filtered by tags, with each test's dependencies moved before it.
func (f Folder) ExpandFiles(items []string, tags []string) ([]string, error) {
	all := f.List()
	byFile := map[string]CaseInfo{}
	for _, c := range all {
		byFile[c.File] = c
	}
	var out []string
	add := func(x string) {
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	var expand func(items, tags []string, depth int) error
	expand = func(items, tags []string, depth int) error {
		if depth > 8 {
			return fmt.Errorf("suites include each other too deep")
		}
		var picked []string
		if len(items) == 0 || len(items) == 1 && (items[0] == "" || strings.EqualFold(items[0], "all")) {
			for _, c := range all {
				picked = append(picked, c.File)
			}
		}
		for _, it := range items {
			it = filepath.ToSlash(strings.Trim(strings.TrimSpace(it), "/"))
			if it == "" || strings.EqualFold(it, "all") {
				continue
			}
			if strings.HasPrefix(it, "suites/") {
				s, err := f.LoadSuite(it)
				if err != nil {
					return err
				}
				var sub []string
				before := out
				out = nil
				if err := expand(s.Tests, s.Tags, depth+1); err != nil {
					return err
				}
				sub, out = out, before
				picked = append(picked, sub...)
				continue
			}
			if !strings.HasPrefix(it, "cases") {
				it = "cases/" + it
			}
			if p, err := f.path(it); err == nil {
				if st, err := os.Stat(p); err == nil && st.IsDir() {
					var in []string
					for _, c := range all {
						if strings.HasPrefix(c.File, strings.TrimSuffix(it, "/")+"/") {
							in = append(in, c.File)
						}
					}
					sort.Strings(in)
					picked = append(picked, in...)
					continue
				}
			}
			if !isCaseFile(it) {
				if _, ok := byFile[it+".yaml"]; ok {
					it += ".yaml"
				}
			}
			picked = append(picked, it)
		}
		for _, x := range picked {
			if c, ok := byFile[x]; ok && !tagsMatch(c.Tags, tags) {
				continue
			}
			add(x)
		}
		return nil
	}
	if err := expand(items, tags, 0); err != nil {
		return nil, err
	}
	return f.depOrder(out), nil
}

func tagsMatch(have, want []string) bool {
	inc := false
	for _, t := range want {
		if strings.HasPrefix(t, "!") {
			if slices.Contains(have, t[1:]) {
				return false
			}
			continue
		}
		inc = true
	}
	if !inc {
		return true
	}
	for _, t := range want {
		if !strings.HasPrefix(t, "!") && slices.Contains(have, t) {
			return true
		}
	}
	return false
}

// depFile finds a dependency: a file in cases/ (with or without .yaml) or a test's name.
func (f Folder) depFile(d string) string {
	d = filepath.ToSlash(strings.TrimSpace(d))
	for _, c := range f.List() {
		if c.File == d || c.File == "cases/"+d || c.File == d+".yaml" || c.File == "cases/"+d+".yaml" || strings.EqualFold(c.Name, d) {
			return c.File
		}
	}
	return ""
}

// depOrder puts each test's dependencies (those in the list) before it, keeping the order otherwise.
func (f Folder) depOrder(files []string) []string {
	deps := map[string][]string{}
	for _, x := range files {
		if c, err := f.Load(x); err == nil {
			for _, d := range c.DependsOn {
				if df := f.depFile(d); df != "" && slices.Contains(files, df) {
					deps[x] = append(deps[x], df)
				}
			}
		}
	}
	var out []string
	state := map[string]int{}
	var visit func(string)
	visit = func(x string) {
		if state[x] != 0 { // done, or a cycle: keep the order
			return
		}
		state[x] = 1
		for _, d := range deps[x] {
			visit(d)
		}
		state[x] = 2
		out = append(out, x)
	}
	for _, x := range files {
		visit(x)
	}
	return out
}

// blockedBy says why a test is skipped: a dependency that failed or was skipped in this run.
func (f Folder) blockedBy(c Case, status map[string]string) string {
	for _, d := range c.DependsOn {
		df := f.depFile(d)
		if df == "" {
			continue
		}
		switch status[df] {
		case "failed":
			return fmt.Sprintf("skipped: depends on %q, which failed", d)
		case "skipped":
			return fmt.Sprintf("skipped: depends on %q, which was skipped", d)
		}
	}
	return ""
}
