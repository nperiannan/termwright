package standalone

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/webrun"
)

// Step is a test step as the test file and the generated code write it (the parts run here).
type Step struct {
	Run *struct {
		Session string `json:"session"`
		Command string `json:"command"`
		Prompt  string `json:"prompt,omitempty"`
		Parse   string `json:"parse,omitempty"`
	} `json:"run,omitempty"`
	HTTP *HTTPRequest `json:"http,omitempty"`
	SNMP *struct {
		Session string      `json:"session"`
		Op      string      `json:"op,omitempty"`
		OIDs    []string    `json:"oids,omitempty"`
		Values  []SNMPValue `json:"values,omitempty"`
	} `json:"snmp,omitempty"`
	Netconf *Netconf     `json:"netconf,omitempty"`
	Web     *webrun.Step `json:"web,omitempty"`
	Upgrade any          `json:"upgrade,omitempty"`
	Traffic any          `json:"traffic,omitempty"`
	Manual  any          `json:"manual,omitempty"`
	Timeout int          `json:"timeout,omitempty"`
	Expect  []Expect     `json:"expect,omitempty"`
	Retry   *struct {
		Timeout  float64 `json:"timeout"`
		Interval float64 `json:"interval"`
	} `json:"retry,omitempty"`
	Sample *struct {
		Count    int     `json:"count"`
		Interval float64 `json:"interval"`
	} `json:"sample,omitempty"`
}

// Expect is one check of the test file.
type Expect struct {
	Contains    string   `json:"contains,omitempty"`
	NotContains string   `json:"notContains,omitempty"`
	Matches     string   `json:"matches,omitempty"`
	Field       string   `json:"field,omitempty"`
	OID         string   `json:"oid,omitempty"`
	JSON        string   `json:"json,omitempty"`
	Status      int      `json:"status,omitempty"`
	MaxMs       int64    `json:"maxMs,omitempty"`
	Equals      *string  `json:"equals,omitempty"`
	NotEquals   *string  `json:"notEquals,omitempty"`
	Greater     *float64 `json:"greater,omitempty"`
	Less        *float64 `json:"less,omitempty"`
	Exists      *bool    `json:"exists,omitempty"`
	Agg         string   `json:"agg,omitempty"`
	Schema      string   `json:"schema,omitempty"`
	Table       any      `json:"table,omitempty"`
}

// Check is a check's result.
type Check struct {
	What   string `json:"what"`
	Passed bool   `json:"passed"`
	Got    string `json:"got,omitempty"`
}

// Table is parsed output.
type Table struct {
	Header []string   `json:"header"`
	Rows   [][]string `json:"rows"`
}

// Answer is what a step got, as Termwright's step tool answers it.
type Answer struct {
	Target string    `json:"target,omitempty"`
	Sent   string    `json:"sent,omitempty"`
	Output string    `json:"output"`
	Status int       `json:"status,omitempty"`
	Ms     int64     `json:"ms"`
	Rows   []SNMPRow `json:"rows,omitempty"`
	Table  *Table    `json:"table,omitempty"`
	Checks []Check   `json:"checks,omitempty"`
	Error  string    `json:"error,omitempty"`
}

// ErrNeedsTermwright: a step only Termwright runs.
var ErrNeedsTermwright = errors.New("this step needs Termwright (software upgrades, lab traffic tests and manual steps are not run standalone)")

// One runs a step's action, without its checks.
func (e *Engine) One(ctx context.Context, st Step) (Answer, error) {
	t0 := time.Now()
	var a Answer
	var err error
	to := time.Duration(st.Timeout) * time.Second
	switch {
	case st.Run != nil:
		a.Target, a.Sent = st.Run.Session, st.Run.Command
		a.Output, err = e.Command(ctx, st.Run.Session, st.Run.Command, st.Run.Prompt, to)
		if err == nil && st.Run.Parse != "" {
			h, rows, perr := e.Parse(st.Run.Session, st.Run.Command, a.Output, st.Run.Parse)
			if perr != nil {
				err = perr
			} else {
				a.Table = &Table{Header: h, Rows: rows}
			}
		}
	case st.HTTP != nil:
		a.Target, a.Sent = orStr(st.HTTP.Session, st.HTTP.URL), strings.ToUpper(orStr(st.HTTP.Method, "GET"))+" "+st.HTTP.URL
		a.Status, a.Output, err = e.HTTP(ctx, *st.HTTP)
	case st.SNMP != nil:
		a.Target, a.Sent = st.SNMP.Session, "snmp "+orStr(st.SNMP.Op, "get")+" "+strings.Join(st.SNMP.OIDs, " ")
		a.Rows, err = e.SNMP(ctx, st.SNMP.Session, st.SNMP.Op, st.SNMP.OIDs, st.SNMP.Values)
		var lines []string
		for _, r := range a.Rows {
			lines = append(lines, r.Name+" = "+r.Display)
		}
		a.Output = strings.Join(lines, "\n")
	case st.Netconf != nil:
		a.Target, a.Sent = st.Netconf.Session, "netconf "+orStr(st.Netconf.Op, "get")
		a.Output, err = e.Netconf(ctx, *st.Netconf)
	case st.Web != nil:
		a.Target, a.Sent = "browser", "web "+st.Web.URL
		a.Output, err = e.Web(ctx, *st.Web, st.Timeout)
	case st.Upgrade != nil || st.Traffic != nil || st.Manual != nil:
		err = ErrNeedsTermwright
	default:
		err = errors.New("an empty step")
	}
	a.Ms = time.Since(t0).Milliseconds()
	return a, err
}

// Execute runs a step as the generated code sends it (JSON): its action, and with retry, sample, NETCONF
// or web also its checks. Errors are in Answer.Error.
func (e *Engine) Execute(ctx context.Context, stepJSON []byte) Answer {
	var st Step
	if err := json.Unmarshal(stepJSON, &st); err != nil {
		return Answer{Error: "step: " + err.Error()}
	}
	if st.Sample != nil && st.Sample.Count > 1 {
		var all []Answer
		t0 := time.Now()
		for i := 0; i < st.Sample.Count; i++ {
			if i > 0 {
				time.Sleep(time.Duration(st.Sample.Interval * float64(time.Second)))
			}
			a, err := e.One(ctx, st)
			if err != nil {
				a.Error = err.Error()
				return a
			}
			all = append(all, a)
		}
		secs := time.Since(t0).Seconds()
		a := all[len(all)-1]
		for _, x := range st.Expect {
			if x.Agg == "" {
				a.Checks = append(a.Checks, CheckOne(x, a))
				continue
			}
			var nums []float64
			for _, s := range all {
				c := CheckOne(Expect{Field: x.Field, JSON: x.JSON, OID: x.OID}, s)
				if n, err := strconv.ParseFloat(numOnly.ReplaceAllString(c.Got, ""), 64); c.Passed && err == nil {
					nums = append(nums, n)
				}
			}
			v, ok := agg(nums, x.Agg, secs)
			got := ""
			if ok {
				got = strconv.FormatFloat(v, 'g', 6, 64)
			}
			a.Checks = append(a.Checks, compare(x.Agg+" of "+orStr(x.Field, orStr(x.JSON, x.OID)), got, ok, x, ""))
		}
		return a
	}
	if st.Retry != nil {
		end := time.Now().Add(time.Duration(orF(st.Retry.Timeout, 60) * float64(time.Second)))
		for {
			a, err := e.One(ctx, st)
			if err != nil {
				a.Error = err.Error()
				return a
			}
			ok := true
			for _, x := range st.Expect {
				c := CheckOne(x, a)
				a.Checks = append(a.Checks, c)
				ok = ok && c.Passed
			}
			if ok || time.Now().After(end) {
				return a
			}
			time.Sleep(time.Duration(orF(st.Retry.Interval, 5) * float64(time.Second)))
		}
	}
	a, err := e.One(ctx, st)
	if err != nil {
		a.Error = err.Error()
		return a
	}
	if st.Netconf != nil || st.Web != nil {
		for _, x := range st.Expect {
			a.Checks = append(a.Checks, CheckOne(x, a))
		}
	}
	return a
}

var (
	numOnly  = regexp.MustCompile(`[^\d.\-]`)
	spaceRun = regexp.MustCompile(`\s+`)
	pathPart = regexp.MustCompile(`([^.\[\]]+)|\[(\d+|-1)\]|\["([^"]*)"\]`)
)

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func orF(f, d float64) float64 {
	if f == 0 {
		return d
	}
	return f
}

func agg(nums []float64, kind string, secs float64) (float64, bool) {
	if len(nums) == 0 {
		return 0, false
	}
	switch kind {
	case "min", "max":
		v := nums[0]
		for _, n := range nums {
			if kind == "min" {
				v = math.Min(v, n)
			} else {
				v = math.Max(v, n)
			}
		}
		return v, true
	case "avg":
		s := 0.0
		for _, n := range nums {
			s += n
		}
		return s / float64(len(nums)), true
	case "delta":
		return nums[len(nums)-1] - nums[0], true
	case "rate":
		if secs <= 0 {
			return 0, true
		}
		return (nums[len(nums)-1] - nums[0]) / secs, true
	}
	return 0, false
}

// HasText: out contains t, spacing and line breaks not counted.
func HasText(out, t string) bool {
	if strings.Contains(out, t) {
		return true
	}
	n := func(s string) string { return strings.TrimSpace(spaceRun.ReplaceAllString(s, " ")) }
	return strings.Contains(n(out), n(t))
}

// CheckOne runs one check on an answer (table and schema checks need Termwright's runner).
func CheckOne(x Expect, a Answer) Check {
	out := a.Output
	switch {
	case x.Contains != "":
		ok := HasText(out, x.Contains)
		return Check{What: fmt.Sprintf("contains %q", x.Contains), Passed: ok, Got: map[bool]string{false: "not in the output"}[ok]}
	case x.NotContains != "":
		ok := !HasText(out, x.NotContains)
		return Check{What: fmt.Sprintf("does not contain %q", x.NotContains), Passed: ok, Got: map[bool]string{false: "it is in the output"}[ok]}
	case x.Matches != "":
		re, err := regexp.Compile(x.Matches)
		if err != nil {
			return Check{What: "matches " + x.Matches, Got: "bad pattern: " + err.Error()}
		}
		return Check{What: "matches " + x.Matches, Passed: re.MatchString(out), Got: re.FindString(out)}
	case x.Status != 0:
		return Check{What: fmt.Sprintf("status = %d", x.Status), Passed: a.Status == x.Status, Got: strconv.Itoa(a.Status)}
	case x.OID != "":
		o := strings.TrimPrefix(x.OID, ".")
		for _, r := range a.Rows {
			n := r.Name
			if i := strings.Index(n, "::"); i >= 0 && !strings.Contains(o, "::") {
				n = n[i+2:]
			}
			if r.OID == o || r.Name == o || n == o || strings.HasPrefix(n, o+".") || strings.HasPrefix(r.OID, o+".") {
				return compare("oid "+x.OID, r.Display, true, x, r.Value)
			}
		}
		return compare("oid "+x.OID, "", false, x, "")
	case x.MaxMs != 0 && x.Field == "" && x.JSON == "":
		return Check{What: fmt.Sprintf("took ≤ %d ms", x.MaxMs), Passed: a.Ms <= x.MaxMs, Got: fmt.Sprintf("%d ms", a.Ms)}
	case x.Field != "":
		re, err := regexp.Compile(x.Field)
		if err != nil {
			return Check{What: "field " + x.Field, Got: "bad pattern: " + err.Error()}
		}
		m := re.FindStringSubmatch(out)
		if m == nil {
			return compare("field "+x.Field, "", false, x, "")
		}
		if len(m) > 1 {
			return compare("field "+x.Field, m[1], true, x, "")
		}
		return compare("field "+x.Field, m[0], true, x, "")
	case x.JSON != "":
		var doc any
		if t := strings.TrimSpace(out); strings.HasPrefix(t, "<") {
			doc = XMLDoc(t)
		} else if json.Unmarshal([]byte(out), &doc) != nil {
			return compare("json "+x.JSON, "", false, x, "")
		}
		v, ok := JSONPath(doc, x.JSON)
		return compare("json "+x.JSON, jsonText(v), ok, x, "")
	}
	return Check{What: "a check the standalone runner does not run (table or schema)", Got: "run it in Termwright"}
}

func jsonText(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func compare(subj, got string, found bool, x Expect, raw string) Check {
	c := Check{What: subj, Got: got, Passed: found}
	var parts []string
	if x.Exists != nil {
		c.Passed = found == *x.Exists
		parts = append(parts, fmt.Sprintf("exists = %v", *x.Exists))
	}
	if x.Equals != nil {
		c.Passed = c.Passed && found && (got == *x.Equals || (raw != "" && raw == *x.Equals))
		parts = append(parts, fmt.Sprintf("= %q", *x.Equals))
	}
	if x.NotEquals != nil {
		c.Passed = c.Passed && got != *x.NotEquals
		parts = append(parts, fmt.Sprintf("≠ %q", *x.NotEquals))
	}
	if x.Greater != nil || x.Less != nil {
		src := got
		if raw != "" {
			src = raw
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(src), 64)
		if err != nil {
			c.Passed = false
		}
		if x.Greater != nil {
			c.Passed = c.Passed && n > *x.Greater
			parts = append(parts, "> "+strconv.FormatFloat(*x.Greater, 'g', -1, 64))
		}
		if x.Less != nil {
			c.Passed = c.Passed && n < *x.Less
			parts = append(parts, "< "+strconv.FormatFloat(*x.Less, 'g', -1, 64))
		}
	}
	if len(parts) > 0 {
		c.What += " " + strings.Join(parts, " and ")
	} else {
		c.What += " found"
	}
	return c
}

// JSONPath finds a value: data.items[0].name, ["a.b"], [-1] (the last), .length.
func JSONPath(doc any, path string) (any, bool) {
	path = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(path), "$"), ".")
	cur := doc
	for _, m := range pathPart.FindAllStringSubmatch(path, -1) {
		if m[2] != "" {
			a, ok := cur.([]any)
			if !ok {
				return nil, false
			}
			i, _ := strconv.Atoi(m[2])
			if i < 0 {
				i += len(a)
			}
			if i < 0 || i >= len(a) {
				return nil, false
			}
			cur = a[i]
			continue
		}
		k := m[1]
		if m[3] != "" {
			k = m[3]
		}
		switch t := cur.(type) {
		case map[string]any:
			x, ok := t[k]
			if !ok {
				if k == "length" {
					return float64(len(t)), true
				}
				return nil, false
			}
			cur = x
		case []any:
			if k != "length" {
				return nil, false
			}
			return float64(len(t)), true
		default:
			return nil, false
		}
	}
	return cur, true
}

// XMLDoc reads XML (a NETCONF reply) as nested maps, so JSON paths work on it.
func XMLDoc(s string) any {
	d := xml.NewDecoder(strings.NewReader(s))
	d.Strict = false
	type frame struct {
		name string
		kids map[string]any
		text strings.Builder
	}
	root := &frame{kids: map[string]any{}}
	stack := []*frame{root}
	for {
		t, err := d.Token()
		if err != nil {
			break
		}
		switch x := t.(type) {
		case xml.StartElement:
			stack = append(stack, &frame{name: x.Name.Local, kids: map[string]any{}})
		case xml.CharData:
			stack[len(stack)-1].text.Write(x)
		case xml.EndElement:
			if len(stack) < 2 {
				continue
			}
			fr := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			var val any = fr.kids
			if len(fr.kids) == 0 {
				val = strings.TrimSpace(fr.text.String())
			}
			parent := stack[len(stack)-1].kids
			switch old := parent[fr.name].(type) {
			case nil:
				parent[fr.name] = val
			case []any:
				parent[fr.name] = append(old, val)
			default:
				parent[fr.name] = []any{old, val}
			}
		}
	}
	if r, ok := root.kids["rpc-reply"].(map[string]any); ok && len(root.kids) == 1 {
		return r
	}
	return root.kids
}
