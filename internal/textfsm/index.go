package textfsm

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Index finds the template for a platform and command, from an ntc-templates style "index" file:
// CSV with the columns Template, Hostname, Platform, Command; commands use [[...]] for the part
// that may be left out ("sh[[ow]] vl[[an]]").
type Index struct {
	Dir     string
	entries []entry
}

type entry struct {
	file     string
	platform *regexp.Regexp
	command  *regexp.Regexp
}

// expand turns "sh[[ow]]" into "sh(o(w)?)?".
func expand(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "[[")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.Index(s[i:], "]]")
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		opt := s[i+2 : i+j]
		for _, c := range opt {
			b.WriteString("(" + regexp.QuoteMeta(string(c)))
		}
		b.WriteString(strings.Repeat(")?", len([]rune(opt))))
		s = s[i+j+2:]
	}
}

// LoadIndex reads dir/index (missing: an empty index).
func LoadIndex(dir string) (*Index, error) {
	ix := &Index{Dir: dir}
	b, err := os.ReadFile(filepath.Join(dir, "index"))
	if os.IsNotExist(err) {
		return ix, nil
	}
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			lines = append(lines, l)
		}
	}
	r := csv.NewReader(strings.NewReader(strings.Join(lines, "\n")))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("templates/index: %v", err)
	}
	col := map[string]int{}
	for i, row := range rows {
		if i == 0 {
			for j, h := range row {
				col[strings.TrimSpace(h)] = j
			}
			continue
		}
		get := func(k string) string {
			if j, ok := col[k]; ok && j < len(row) {
				return strings.TrimSpace(row[j])
			}
			return ""
		}
		p, err1 := regexp.Compile("^(?i:" + get("Platform") + ")$")
		c, err2 := regexp.Compile("^(?i:" + expand(get("Command")) + ")\\s*$")
		if err1 != nil || err2 != nil || get("Template") == "" {
			continue
		}
		ix.entries = append(ix.entries, entry{file: get("Template"), platform: p, command: c})
	}
	return ix, nil
}

// Find returns the template files (several are joined with ':' in an index) for a command.
func (ix *Index) Find(platform, command string) []string {
	command = strings.Join(strings.Fields(command), " ")
	for _, e := range ix.entries {
		if e.platform.MatchString(platform) && e.command.MatchString(command) {
			var out []string
			for _, f := range strings.Split(e.file, ":") {
				out = append(out, filepath.Join(ix.Dir, strings.TrimSpace(f)))
			}
			return out
		}
	}
	return nil
}

// Platform is the ntc-templates platform name of a Termwright device type.
func Platform(deviceType string) string {
	switch strings.ToLower(strings.TrimSpace(deviceType)) {
	case "exos":
		return "extreme_exos"
	case "voss":
		return "extreme_vsp"
	case "cisco ios", "cisco ios-xe":
		return "cisco_ios"
	case "cisco nx-os":
		return "cisco_nxos"
	case "huawei vrp":
		return "huawei_vrp"
	case "ruckus icx":
		return "ruckus_fastiron"
	case "eltex mes":
		return "eltex_mes"
	case "juniper junos":
		return "juniper_junos"
	case "arista eos":
		return "arista_eos"
	}
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(deviceType)), " ", "_")
}
