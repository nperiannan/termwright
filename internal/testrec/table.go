package testrec

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/termwright/termwright/internal/textfsm"
)

// Parser turns a command's output into a table with a TextFSM template ("auto": the templates/index
// entry for the session's device type and the command).
type Parser interface {
	ParseOutput(session, command, template, output string) (*textfsm.Table, error)
}

// TableExpect checks parsed output: the rows whose columns match Where (exact, case-insensitive;
// "~regex" for a pattern), then Rows (how many) or Column of the first such row against
// equals / notEquals / matches / greater / less / exists.
type TableExpect struct {
	Where     map[string]string `yaml:"where,omitempty" json:"where,omitempty"`
	Column    string            `yaml:"column,omitempty" json:"column,omitempty"`
	Rows      *int              `yaml:"rows,omitempty" json:"rows,omitempty"`
	Equals    *string           `yaml:"equals,omitempty" json:"equals,omitempty"`
	NotEquals *string           `yaml:"notEquals,omitempty" json:"notEquals,omitempty"`
	Matches   string            `yaml:"matches,omitempty" json:"matches,omitempty"`
	Greater   *float64          `yaml:"greater,omitempty" json:"greater,omitempty"`
	Less      *float64          `yaml:"less,omitempty" json:"less,omitempty"`
	Exists    *bool             `yaml:"exists,omitempty" json:"exists,omitempty"`
}

// Label is a short text for the check ("VLAN_ID=200 → STATUS = active").
func (t TableExpect) Label() string {
	var w []string
	for _, k := range sortedKeys(t.Where) {
		w = append(w, k+"="+t.Where[k])
	}
	s := "row " + strings.Join(w, " ")
	if len(w) == 0 {
		s = "rows"
	}
	switch {
	case t.Rows != nil:
		return fmt.Sprintf("%s: %d rows", s, *t.Rows)
	case t.Column != "":
		s += " → " + t.Column
	}
	return s
}

func sortedKeys(m map[string]string) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

// TableRows: the rows of tab whose columns match where (variables filled in).
func TableRows(tab *textfsm.Table, where map[string]string, v map[string]string) []map[string]string {
	var out []map[string]string
	if tab == nil {
		return nil
	}
	for _, r := range tab.Maps() {
		ok := true
		for k, want := range where {
			got, has := lookupCol(r, k)
			want = Fill(want, v)
			if strings.HasPrefix(want, "~") {
				re, err := regexp.Compile(want[1:])
				ok = ok && has && err == nil && re.MatchString(got)
			} else {
				ok = ok && has && strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want))
			}
		}
		if ok {
			out = append(out, r)
		}
	}
	return out
}

func lookupCol(r map[string]string, k string) (string, bool) {
	if x, ok := r[k]; ok {
		return x, true
	}
	for c, x := range r {
		if strings.EqualFold(c, k) {
			return x, true
		}
	}
	return "", false
}

func checkTable(t TableExpect, tab *textfsm.Table, v map[string]string) CheckResult {
	what := t.Label()
	if tab == nil {
		return CheckResult{What: what, Got: "the output was not parsed - add parse: auto (or a template) to the step"}
	}
	rows := TableRows(tab, t.Where, v)
	if t.Rows != nil {
		return CheckResult{What: what, Passed: len(rows) == *t.Rows, Got: strconv.Itoa(len(rows)) + " rows"}
	}
	if t.Column == "" {
		want := true
		if t.Exists != nil {
			want = *t.Exists
		}
		return CheckResult{What: what + map[bool]string{true: " is there", false: " is not there"}[want], Passed: (len(rows) > 0) == want, Got: strconv.Itoa(len(rows)) + " rows"}
	}
	got, found := "", false
	if len(rows) > 0 {
		got, found = lookupCol(rows[0], t.Column)
	}
	if t.Matches != "" {
		re, err := regexp.Compile(Fill(t.Matches, v))
		if err != nil {
			return CheckResult{What: what + " matches " + t.Matches, Got: "bad pattern"}
		}
		return CheckResult{What: what + " matches " + re.String(), Passed: found && re.MatchString(got), Got: got}
	}
	c := compare(what, got, found, Expect{Equals: t.Equals, NotEquals: t.NotEquals, Greater: t.Greater, Less: t.Less, Exists: t.Exists}, v)
	if !found && len(rows) == 0 {
		c.Got = "no such row"
	}
	return c
}
