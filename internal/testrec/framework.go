package testrec

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// The order in which values win, weakest first: the tests folder's defaults.yaml, each folder's
// defaults.yaml down to the test's (a suite), the test's own values, the testbed, the run.

// SuiteDefaults merges the defaults.yaml files from the tests folder down to a test's folder.
func (f Folder) SuiteDefaults(rel string) Defaults {
	out := Defaults{Vars: map[string]string{}}
	dirs := []string{""}
	d := path.Dir(filepath.ToSlash(rel))
	parts := strings.Split(d, "/")
	for i := range parts {
		if parts[i] == "." || parts[i] == "" {
			continue
		}
		dirs = append(dirs, strings.Join(parts[:i+1], "/"))
	}
	for _, dir := range dirs {
		b, err := os.ReadFile(filepath.Join(f.Root, filepath.FromSlash(dir), "defaults.yaml"))
		if err != nil {
			continue
		}
		var x Defaults
		if yaml.Unmarshal(b, &x) != nil {
			continue
		}
		for k, v := range x.Vars {
			out.Vars[k] = v
		}
		out.StepOptions = mergeOptions(out.StepOptions, x.StepOptions)
	}
	return out
}

// mergeOptions: b's options over a's (what b sets wins).
func mergeOptions(a, b *StepOptions) *StepOptions {
	if b == nil {
		return a
	}
	if a == nil {
		c := *b
		return &c
	}
	c := *a
	if b.Timeout != 0 {
		c.Timeout = b.Timeout
	}
	if b.Wait != 0 {
		c.Wait = b.Wait
	}
	if b.Retry != nil {
		c.Retry = b.Retry
	}
	c.ContinueOnFail = c.ContinueOnFail || b.ContinueOnFail
	c.IgnoreErrors = c.IgnoreErrors || b.IgnoreErrors
	return &c
}

// SaveSuiteDefaults writes a folder's defaults.yaml ("" = the tests folder's).
func (f Folder) SaveSuiteDefaults(dir string, d Defaults) error {
	p, err := f.path(path.Join(dir, "defaults.yaml"))
	if err != nil {
		return err
	}
	b, err := yaml.Marshal(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, append([]byte("# Values and step options for every test in this folder and below (a test's own win).\n"), b...), 0o644)
}

// LoadSuiteDefaults reads one folder's defaults.yaml.
func (f Folder) LoadSuiteDefaults(dir string) Defaults {
	var d Defaults
	if p, err := f.path(path.Join(dir, "defaults.yaml")); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			_ = yaml.Unmarshal(b, &d)
		}
	}
	return d
}

// withSuite gives a test its suite's values and step options where it has none of its own.
func (f Folder) withSuite(c Case) Case {
	if c.file == "" {
		return c
	}
	d := f.SuiteDefaults(c.file)
	if len(d.Vars) > 0 {
		v := map[string]string{}
		for k, x := range d.Vars {
			v[k] = x
		}
		for k, x := range c.Vars {
			v[k] = x
		}
		c.Vars = v
	}
	c.Defaults = mergeOptions(d.StepOptions, c.Defaults)
	return c
}

// unmet says what a test needs that the devices of this run do not have ("" = all there). v holds the
// run's values with the devices' facts and ports.
func unmet(c Case, v map[string]string) string {
	roles := make([]string, 0, len(c.Requires))
	for r := range c.Requires {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	var miss []string
	for _, r := range roles {
		q := c.Requires[r]
		if v[r] == "" {
			miss = append(miss, "no device for {{"+r+"}}")
			continue
		}
		if q.DeviceType != "" && !strings.Contains(strings.ToLower(v[r+".device_type"]), strings.ToLower(q.DeviceType)) {
			miss = append(miss, fmt.Sprintf("{{%s}} is %s, not %s", r, orNone(v[r+".device_type"]), q.DeviceType))
		}
		if q.Stack != nil && *q.Stack != (v[r+".stack"] == "true" || v[r+".slot"] != "") {
			miss = append(miss, fmt.Sprintf("{{%s}} is %s", r, map[bool]string{true: "not a stack", false: "a stack"}[*q.Stack]))
		}
		if q.Model != "" && !strings.Contains(strings.ToLower(v[r+".model"]), strings.ToLower(q.Model)) {
			miss = append(miss, fmt.Sprintf("{{%s}} is model %s, not %s", r, orNone(v[r+".model"]), q.Model))
		}
		if q.Ports > 0 {
			n := 0
			for i := 1; v[r+".port"+strconv.Itoa(i)] != ""; i++ {
				n++
			}
			if n < q.Ports {
				miss = append(miss, fmt.Sprintf("{{%s}} has %d test ports, the test needs %d", r, n, q.Ports))
			}
		}
		for _, t := range q.Tags {
			if !strings.Contains(","+v["testbed.tags"]+",", ","+t+",") {
				miss = append(miss, "the testbed is not tagged "+t)
			}
		}
	}
	return strings.Join(miss, "; ")
}

func orNone(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// addLinks resolves the testbed's links into values: for a link "a.port1 - b.port3", {{a.to.b}} is a's port
// facing b and {{b.to.a}} b's (the first link between them), and {{a.links.b}} all of a's ports to b.
func addLinks(v map[string]string) {
	var keys []string
	for k := range v {
		if strings.HasPrefix(k, "links.") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		ends := strings.FieldsFunc(v[k], func(r rune) bool { return r == ' ' || r == ',' || r == '-' && false })
		var parts []string
		for _, e := range ends {
			if e != "-" && e != "<->" && e != "--" {
				parts = append(parts, e)
			}
		}
		if len(parts) != 2 {
			continue
		}
		a, ap, ok1 := strings.Cut(parts[0], ".")
		b, bp, ok2 := strings.Cut(parts[1], ".")
		if !ok1 || !ok2 {
			continue
		}
		for _, x := range [][4]string{{a, ap, b, bp}, {b, bp, a, ap}} {
			port := v[x[0]+"."+x[1]]
			if port == "" {
				port = x[1]
			}
			if key := x[0] + ".to." + x[2]; v[key] == "" {
				v[key] = port
			}
			lk := x[0] + ".links." + x[2]
			if v[lk] == "" {
				v[lk] = port
			} else if !strings.Contains(","+v[lk]+",", ","+port+",") {
				v[lk] += "," + port
			}
		}
	}
}
