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
// Each row is a step; rows with the same Test belong to one test. A step without a device becomes a
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
