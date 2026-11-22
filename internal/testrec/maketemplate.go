package testrec

import (
	"fmt"
	"regexp"
	"strings"
)

// TemplateColumn is a column of a table made from output: its name (capitals) and whether its values
// may contain spaces (the columns are then split at two spaces or more).
type TemplateColumn struct {
	Name   string `json:"name"`
	Spaces bool   `json:"spaces,omitempty"`
	// Match: a regular expression for its values (default \S+, or .+? with spaces).
	Match string `json:"match,omitempty"`
}

// TemplateField is a "label: value" line of output kept as a value.
type TemplateField struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// TemplateSpec says how to read a command's output without writing TextFSM: a table under a header line
// (Kind "table") or labelled fields (Kind "fields").
type TemplateSpec struct {
	Kind    string           `json:"kind"`
	Header  string           `json:"header,omitempty"` // the header line (rows start after it)
	Columns []TemplateColumn `json:"columns,omitempty"`
	Fields  []TemplateField  `json:"fields,omitempty"`
}

var nonName = regexp.MustCompile(`[^A-Z0-9_]+`)

// ColumnName makes a header word a template value name ("Protocol Addr" → PROTOCOL_ADDR).
func ColumnName(s string) string {
	n := strings.Trim(nonName.ReplaceAllString(strings.ToUpper(strings.TrimSpace(s)), "_"), "_")
	if n == "" || (n[0] >= '0' && n[0] <= '9') {
		n = "COL_" + n
	}
	return n
}

// MakeTemplate writes the TextFSM template for a spec - what the Tests tab's table maker saves, so the
// table works in Termwright's runs, the generated code and anywhere TextFSM runs.
func MakeTemplate(sp TemplateSpec) (string, error) {
	var b strings.Builder
	seen := map[string]bool{}
	name := func(n string) (string, error) {
		n = ColumnName(n)
		if seen[n] {
			return "", fmt.Errorf("two columns are called %s", n)
		}
		seen[n] = true
		return n, nil
	}
	switch sp.Kind {
	case "fields":
		if len(sp.Fields) == 0 {
			return "", fmt.Errorf("choose at least one field")
		}
		var rules []string
		for _, f := range sp.Fields {
			n, err := name(f.Name)
			if err != nil {
				return "", err
			}
			// a value ends at two spaces (the next "label: value" on the same line) or the line's end
			fmt.Fprintf(&b, "Value %s (\\S+(?: \\S+)*)\n", n)
			lab := regexp.QuoteMeta(strings.TrimRight(strings.TrimSpace(f.Label), ":"))
			lab = strings.ReplaceAll(lab, " ", `\s+`)
			rules = append(rules, fmt.Sprintf("  ^.*?\\b%s\\s*[:=]?\\s+${%s}(?:\\s{2,}.*)?$$ -> Continue", lab, n))
		}
		b.WriteString("\nStart\n" + strings.Join(rules, "\n") + "\n")
		return b.String(), nil
	case "table", "":
		if len(sp.Columns) == 0 {
			return "", fmt.Errorf("choose at least one column")
		}
		var cols []string
		for i, c := range sp.Columns {
			n, err := name(c.Name)
			if err != nil {
				return "", err
			}
			m := c.Match
			if m == "" {
				m = `\S+`
				if c.Spaces {
					m = `\S.*?`
					if i == len(sp.Columns)-1 {
						m = `\S.*`
					}
				}
			}
			if _, err := regexp.Compile(m); err != nil {
				return "", fmt.Errorf("column %s: %v", n, err)
			}
			fmt.Fprintf(&b, "Value %s (%s)\n", n, m)
			if i > 0 {
				// after a column with spaces in its values the next starts after two spaces or more
				if sp.Columns[i-1].Spaces {
					cols = append(cols, `\s{2,}`)
				} else {
					cols = append(cols, `\s+`)
				}
			}
			cols = append(cols, "${"+n+"}")
		}
		row := `  ^\s*` + strings.Join(cols, "") + `\s*$$ -> Record`
		b.WriteString("\nStart\n")
		if h := strings.TrimSpace(sp.Header); h != "" {
			words := strings.Fields(h)
			for i, w := range words {
				words[i] = regexp.QuoteMeta(w)
			}
			b.WriteString(`  ^\s*` + strings.Join(words, `\s+`) + "\\s*$$ -> Table\n\nTable\n")
		}
		b.WriteString("  ^\\s*[-=_+|\\s]+$$ -> Next\n" + row + "\n")
		return b.String(), nil
	}
	return "", fmt.Errorf("kind %q: table or fields", sp.Kind)
}
