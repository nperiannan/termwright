package testrec

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/termwright/termwright/internal/webrun"
)

// ImportedCase is a test made from a manual test case table.
type ImportedCase struct {
	File string `json:"file"`
	Case Case   `json:"case"`
}

var quoted = regexp.MustCompile(`"([^"]{2,})"|“([^”]{2,})”`)

// ImportManual reads manual test cases from CSV (Excel: Save As CSV) with a header row. Columns,
// by name (case-insensitive, the first that fits): Suite, Test (or Test Case, Name), Step (or
// Action, Command), Device (or Session), Expected (or Expected Result), Type (command, api, manual).
// Each row is a step; rows with the same Test belong to one test. Type web: the step is a web action
// (ParseWebAction) on the web app in Device; web rows that follow each other make one web step. A step without a device becomes a
// manual step; quoted text in Expected ("v200 created") becomes a contains check. device: the
// session for rows without one ("" = manual).
func ImportManual(r io.Reader, device string) ([]ImportedCase, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	return ImportRows(rows, device)
}

// ImportRows is ImportManual for rows already read (an Excel sheet).
func ImportRows(rows [][]string, device string) ([]ImportedCase, error) {
	if len(rows) < 2 {
		return nil, fmt.Errorf("the table needs a header row and at least one step")
	}
	col := func(names ...string) int {
		for _, n := range names {
			for i, h := range rows[0] {
				if strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")), n) {
					return i
				}
			}
		}
		return -1
	}
	cSuite, cTest, cStep := col("suite", "folder", "module"), col("test", "test case", "testcase", "name", "title"), col("step", "action", "command", "step description", "steps")
	cDev, cExp, cType := col("device", "session", "dut"), col("expected", "expected result", "expected results"), col("type")
	if cStep < 0 {
		return nil, fmt.Errorf("no Step (or Action, Command) column in %v", rows[0])
	}
	get := func(row []string, i int) string {
		if i < 0 || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	var out []ImportedCase
	index := map[string]int{}
	lastTest := ""
	for _, row := range rows[1:] {
		step := get(row, cStep)
		if step == "" {
			continue
		}
		test := get(row, cTest)
		if test == "" {
			test = lastTest
		}
		if test == "" {
			test = "Imported test"
		}
		lastTest = test
		suite := get(row, cSuite)
		key := suite + "\x00" + test
		i, ok := index[key]
		if !ok {
			file := "cases/"
			if suite != "" {
				file += slug(suite) + "/"
			}
			out = append(out, ImportedCase{File: file + slug(test) + ".yaml", Case: Case{Name: test}})
			i = len(out) - 1
			index[key] = i
		}
		exp, dev, typ := get(row, cExp), get(row, cDev), strings.ToLower(get(row, cType))
		if dev == "" {
			dev = device
		}
		st := Step{Expected: exp}
		if typ == "web" { // a web action: rows in a row on the same web app make one web step
			act, ok := ParseWebAction(step)
			if !ok {
				st.Manual = &ManualStep{Instruction: step + " (not a web action: " + webVerbs + ")"}
				out[i].Case.Steps = append(out[i].Case.Steps, st)
				continue
			}
			acts := []WebAction{act}
			for _, m := range quoted.FindAllStringSubmatch(exp, 5) {
				acts = append(acts, WebAction{Do: "expect", Value: m[1] + m[2]})
			}
			steps := out[i].Case.Steps
			if n := len(steps); n > 0 && steps[n-1].Web != nil && steps[n-1].Web.Login == dev {
				steps[n-1].Web.Actions = append(steps[n-1].Web.Actions, acts...)
				if exp != "" && steps[n-1].Expected != "" {
					steps[n-1].Expected += "; " + exp
				} else if exp != "" {
					steps[n-1].Expected = exp
				}
				continue
			}
			st.Web = &WebStep{Login: dev, Actions: acts}
			out[i].Case.Steps = append(out[i].Case.Steps, st)
			continue
		}
		switch {
		case typ == "manual" || dev == "" && typ != "api":
			st.Manual = &ManualStep{Instruction: step}
		case typ == "api" || regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE)\s+\S+`).MatchString(step):
			m, u, _ := strings.Cut(step, " ")
			st.HTTP = &HTTPStep{Session: dev, Method: strings.ToUpper(m), URL: strings.TrimSpace(u)}
		default:
			st.Run = &RunStep{Session: dev, Command: step}
		}
		if st.Manual == nil {
			for _, m := range quoted.FindAllStringSubmatch(exp, 5) {
				t := m[1] + m[2]
				st.Expect = append(st.Expect, Expect{Contains: t})
			}
		}
		out[i].Case.Steps = append(out[i].Case.Steps, st)
	}
	return out, nil
}

// XLSXRows reads the first sheet of an Excel workbook as text rows (shared and inline strings, numbers).
func XLSXRows(b []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("not an Excel workbook (.xlsx): %v", err)
	}
	read := func(name string) []byte {
		for _, f := range zr.File {
			if f.Name == name {
				rc, err := f.Open()
				if err != nil {
					return nil
				}
				defer rc.Close()
				d, _ := io.ReadAll(io.LimitReader(rc, 64<<20))
				return d
			}
		}
		return nil
	}
	var shared []string
	if d := read("xl/sharedStrings.xml"); d != nil {
		var sst struct {
			SI []struct {
				T string `xml:"t"`
				R []struct {
					T string `xml:"t"`
				} `xml:"r"`
			} `xml:"si"`
		}
		xml.Unmarshal(d, &sst)
		for _, si := range sst.SI {
			t := si.T
			for _, r := range si.R {
				t += r.T
			}
			shared = append(shared, t)
		}
	}
	d := read("xl/worksheets/sheet1.xml")
	if d == nil {
		return nil, fmt.Errorf("the workbook has no first sheet")
	}
	var ws struct {
		Rows []struct {
			C []struct {
				R  string `xml:"r,attr"`
				T  string `xml:"t,attr"`
				V  string `xml:"v"`
				IS struct {
					T string `xml:"t"`
				} `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.Unmarshal(d, &ws); err != nil {
		return nil, err
	}
	var out [][]string
	for _, r := range ws.Rows {
		var row []string
		for _, c := range r.C {
			col := 0
			for _, ch := range c.R {
				if ch >= 'A' && ch <= 'Z' {
					col = col*26 + int(ch-'A'+1)
				}
			}
			for len(row) < col-1 {
				row = append(row, "")
			}
			v := c.V
			switch c.T {
			case "s":
				if i, err := strconv.Atoi(c.V); err == nil && i < len(shared) {
					v = shared[i]
				}
			case "inlineStr":
				v = c.IS.T
			}
			row = append(row, v)
		}
		out = append(out, row)
	}
	return out, nil
}

const webVerbs = "goto, click, fill <target> = <text>, select, check, press, wait, expect <text>, expect <target> = <text>…"

// webValueOnly: actions whose one word after the verb is their value, not a target.
var webValueOnly = map[string]bool{"goto": true, "press": true, "wait": true, "expect": true, "expect_not": true,
	"expect_url": true, "expect_title": true, "dialog": true, "tab": true, "screenshot": true}

var webAlias = map[string]string{"open": "goto", "navigate": "goto", "visit": "goto", "go": "goto", "enter": "fill",
	"choose": "select", "tick": "check", "untick": "uncheck", "verify": "expect", "see": "expect", "assert": "expect",
	"double-click": "dblclick", "right-click": "rightclick", "key": "press"}

// ParseWebAction reads a web action as a manual test table writes it: "click text=Log In",
// "fill label=Email = {{email}}", "expect Networks", "expect #out = Added", "goto /networks", "press Enter".
// The target is as Termwright records it (label=, text=, placeholder=, testid=, #id, role=…); quotes around
// the parts are taken off.
func ParseWebAction(s string) (WebAction, bool) {
	s = strings.TrimSpace(s)
	verb, rest, _ := strings.Cut(s, " ")
	verb = strings.ToLower(strings.TrimSuffix(verb, ":"))
	if a, ok := webAlias[verb]; ok {
		verb = a
	}
	if verb == "goto" && strings.HasPrefix(strings.ToLower(rest), "to ") {
		rest = rest[3:]
	}
	known := false
	for _, a := range WebActions {
		if a == verb {
			known = true
		}
	}
	if !known || verb == "block" {
		return WebAction{}, false
	}
	unq := func(x string) string {
		x = strings.TrimSpace(x)
		if len(x) >= 2 && (x[0] == '"' && x[len(x)-1] == '"' || x[0] == '\'' && x[len(x)-1] == '\'') {
			return x[1 : len(x)-1]
		}
		if strings.HasPrefix(x, "“") && strings.HasSuffix(x, "”") {
			return strings.TrimSuffix(strings.TrimPrefix(x, "“"), "”")
		}
		return x
	}
	a := WebAction{Do: verb}
	if t, v, ok := strings.Cut(rest, " = "); ok {
		a.Target, a.Value = unq(t), unq(v)
	} else if webValueOnly[verb] {
		a.Value = unq(rest)
	} else {
		a.Target = unq(rest)
	}
	if verb == "goto" && (strings.ContainsAny(a.Value, " \t") || !strings.ContainsAny(a.Value, "/.{")) {
		return WebAction{}, false // a sentence ("Open the CRM"), not an address
	}
	for _, x := range webrun.Actions {
		if x.Do == verb && strings.Contains(x.Needs, "target") && a.Target == "" {
			return WebAction{}, false
		}
		if x.Do == verb && strings.Contains(x.Needs, "value") && a.Value == "" {
			return WebAction{}, false
		}
	}
	return a, true
}
