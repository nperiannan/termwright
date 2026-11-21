// Package textfsm parses command output into a table with TextFSM templates - the format of the
// widely shared ntc-templates - so tests can check "VLAN 200: STATUS = active" instead of text.
// Supported: Value options Filldown, Key, Required, List, Fillup; states with rules
// "^regex -> Action"; actions Next, Continue, Record, NoRecord, Clear, Clearall, Error and a new
// state; the EOF state. Go regular expressions (RE2) are used, so look-arounds are not available.
package textfsm

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

// Table is parsed output: the value names and one row per record.
type Table struct {
	Header []string   `json:"header"`
	Rows   [][]string `json:"rows"`
}

// Maps returns the rows as name → value.
func (t Table) Maps() []map[string]string {
	out := make([]map[string]string, 0, len(t.Rows))
	for _, r := range t.Rows {
		m := map[string]string{}
		for i, h := range t.Header {
			if i < len(r) {
				m[h] = r[i]
			}
		}
		out = append(out, m)
	}
	return out
}

type value struct {
	name                                  string
	re                                    string
	filldown, key, required, list, fillup bool
}

type rule struct {
	re         *regexp.Regexp
	line       string // Next, Continue, Error
	rec        string // Record, NoRecord, Clear, Clearall
	state, msg string
}

// Template is a compiled TextFSM template.
type Template struct {
	values []value
	states map[string][]rule
}

var (
	reValue = regexp.MustCompile(`^Value\s+(?:([\w,]+)\s+)?(\w+)\s+(\(.*\))\s*$`)
	reRule  = regexp.MustCompile(`^\s+\^(.*?)(?:\s+->\s+(.*))?$`)
	reVar   = regexp.MustCompile(`\$\{(\w+)\}|\$(\w+)`)
)

// Parse compiles a template.
func Parse(src string) (*Template, error) {
	t := &Template{states: map[string][]rule{}}
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n, state, inValues := 0, "", true
	byName := map[string]string{}
	for sc.Scan() {
		n++
		l := strings.TrimRight(sc.Text(), " \t\r")
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		if inValues {
			if l == "" {
				if len(t.values) > 0 {
					inValues = false
				}
				continue
			}
			m := reValue.FindStringSubmatch(l)
			if m == nil {
				return nil, fmt.Errorf("line %d: expected Value [options] NAME (regex)", n)
			}
			v := value{name: m[2], re: m[3]}
			for _, o := range strings.Split(m[1], ",") {
				switch o {
				case "":
				case "Filldown":
					v.filldown = true
				case "Key":
					v.key = true
				case "Required":
					v.required = true
				case "List":
					v.list = true
				case "Fillup":
					v.fillup = true
				default:
					return nil, fmt.Errorf("line %d: unknown option %s", n, o)
				}
			}
			if _, err := regexp.Compile(v.re); err != nil {
				return nil, fmt.Errorf("line %d: %v", n, err)
			}
			byName[v.name] = v.re
			t.values = append(t.values, v)
			continue
		}
		if l == "" {
			state = ""
			continue
		}
		if !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
			state = strings.TrimSpace(l)
			if _, ok := t.states[state]; ok {
				return nil, fmt.Errorf("line %d: state %s twice", n, state)
			}
			t.states[state] = []rule{}
			continue
		}
		if state == "" {
			return nil, fmt.Errorf("line %d: a rule outside a state", n)
		}
		m := reRule.FindStringSubmatch(l)
		if m == nil {
			return nil, fmt.Errorf("line %d: a rule starts with ^", n)
		}
		pat := strings.ReplaceAll(m[1], "$$", "\x00")
		bad := ""
		pat = reVar.ReplaceAllStringFunc(pat, func(s string) string {
			sm := reVar.FindStringSubmatch(s)
			name := sm[1] + sm[2]
			re, ok := byName[name]
			if !ok {
				bad = name
				return s
			}
			return "(?P<" + name + ">" + strings.TrimSuffix(strings.TrimPrefix(re, "("), ")") + ")"
		})
		if bad != "" {
			return nil, fmt.Errorf("line %d: no Value %s", n, bad)
		}
		pat = strings.ReplaceAll(pat, "\x00", "$")
		re, err := regexp.Compile("^" + pat)
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", n, err)
		}
		r := rule{re: re, line: "Next"}
		if err := r.action(strings.TrimSpace(m[2])); err != nil {
			return nil, fmt.Errorf("line %d: %v", n, err)
		}
		t.states[state] = append(t.states[state], r)
	}
	if _, ok := t.states["Start"]; !ok {
		return nil, fmt.Errorf("the template has no Start state")
	}
	for s, rs := range t.states {
		for _, r := range rs {
			if r.state != "" && r.state != "End" && r.state != "EOF" {
				if _, ok := t.states[r.state]; !ok {
					return nil, fmt.Errorf("state %s goes to unknown state %s", s, r.state)
				}
			}
		}
	}
	return t, nil
}

func (r *rule) action(a string) error {
	if a == "" {
		return nil
	}
	f := strings.Fields(a)
	first := f[0]
	setLR := func(w string) bool {
		switch w {
		case "Next", "Continue", "Error":
			r.line = w
		case "Record", "NoRecord", "Clear", "Clearall":
			r.rec = w
		default:
			return false
		}
		return true
	}
	if i := strings.Index(first, "."); i > 0 {
		if !setLR(first[:i]) || !setLR(first[i+1:]) {
			return fmt.Errorf("unknown action %s", first)
		}
	} else if !setLR(first) {
		r.state = first
		if len(f) > 1 {
			return fmt.Errorf("unknown action %s", a)
		}
		return nil
	}
	if len(f) > 1 {
		if r.line == "Error" {
			r.msg = strings.Trim(strings.Join(f[1:], " "), `"`)
		} else {
			r.state = f[1]
		}
	}
	if r.line == "Continue" && r.state != "" {
		return fmt.Errorf("Continue cannot change the state")
	}
	return nil
}

// Header is the template's value names.
func (t *Template) Header() []string {
	h := make([]string, len(t.values))
	for i, v := range t.values {
		h[i] = v.name
	}
	return h
}

// Run parses text.
func (t *Template) Run(text string) (Table, error) {
	tab := Table{Header: t.Header(), Rows: [][]string{}}
	cur := make([]string, len(t.values))
	lists := make([][]string, len(t.values))
	idx := map[string]int{}
	for i, v := range t.values {
		idx[v.name] = i
	}
	record := func() {
		row := make([]string, len(t.values))
		empty := true
		for i, v := range t.values {
			if v.list {
				row[i] = strings.Join(lists[i], ", ")
			} else {
				row[i] = cur[i]
			}
			if row[i] != "" && !v.filldown {
				empty = false
			}
			if v.required && row[i] == "" {
				return
			}
		}
		if empty {
			return
		}
		tab.Rows = append(tab.Rows, row)
		for i, v := range t.values {
			lists[i] = nil
			if !v.filldown {
				cur[i] = ""
			}
		}
	}
	clear := func(all bool) {
		for i, v := range t.values {
			if all || !v.filldown {
				cur[i] = ""
				lists[i] = nil
			}
		}
	}
	state := "Start"
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for _, line := range lines {
		if state == "End" {
			break
		}
		for _, r := range t.states[state] {
			at := r.re.FindStringSubmatchIndex(line)
			if at == nil {
				continue
			}
			for j, name := range r.re.SubexpNames() {
				i, ok := idx[name]
				if !ok || at[2*j] < 0 {
					continue
				}
				got := line[at[2*j]:at[2*j+1]]
				if t.values[i].list {
					lists[i] = append(lists[i], got)
				} else {
					cur[i] = got
				}
				if t.values[i].fillup {
					for k := len(tab.Rows) - 1; k >= 0 && tab.Rows[k][i] == ""; k-- {
						tab.Rows[k][i] = got
					}
				}
			}
			switch r.rec {
			case "Record":
				record()
			case "Clear":
				clear(false)
			case "Clearall":
				clear(true)
			}
			if r.line == "Error" {
				msg := r.msg
				if msg == "" {
					msg = "the template's Error rule matched"
				}
				return tab, fmt.Errorf("%s: %q", msg, line)
			}
			if r.line == "Next" {
				if r.state != "" {
					state = r.state
				}
				break
			}
		}
	}
	if eof, ok := t.states["EOF"]; !ok || len(eof) > 0 {
		record()
	}
	return tab, nil
}
