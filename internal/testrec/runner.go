package testrec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Driver runs the steps: the app runs commands on saved sessions and sends HTTP requests; tests
// use a fake one.
type Driver interface {
	// Command sends cmd on the saved session and returns its output (no echo, no prompt).
	Command(ctx context.Context, session, cmd, prompt string) (string, error)
	// HTTP sends a request.
	HTTP(ctx context.Context, r HTTPStep) (HTTPAnswer, error)
	// SNMP runs an SNMP operation.
	SNMP(ctx context.Context, r SNMPStep) ([]SNMPValue, error)
}

// HTTPAnswer is what an HTTP step got.
type HTTPAnswer struct {
	Status int    // the status code (0: nothing came back)
	Body   string // the body as text
}

// Result is one run of a case.
type Result struct {
	// Case: its name.
	Case string `json:"case"`
	// File: the case file, relative to the tests folder.
	File string `json:"file,omitempty"`
	// Start of the run.
	Start time.Time `json:"start"`
	// Ms: how long the whole case took.
	Ms int64 `json:"ms"`
	// Passed: every step passed.
	Passed bool `json:"passed"`
	// Error: why the case could not run at all (a bad file).
	Error string `json:"error,omitempty"`
	// Steps: one per step that ran (steps after a failure are Skipped).
	Steps []StepResult `json:"steps"`
}

// StepResult is one step of a run.
type StepResult struct {
	// Name of the step.
	Name string `json:"name"`
	// Kind: run or http.
	Kind string `json:"kind"`
	// Target: the session or the URL.
	Target string `json:"target,omitempty"`
	// Sent: the command or "METHOD url" sent (variables filled in).
	Sent string `json:"sent,omitempty"`
	// Status: the HTTP status.
	Status int `json:"status,omitempty"`
	// Output: what came back (cut at 64 KB).
	Output string `json:"output,omitempty"`
	// Ms: how long the step took.
	Ms int64 `json:"ms"`
	// Passed: it ran and every check passed.
	Passed bool `json:"passed"`
	// Skipped: an earlier step failed.
	Skipped bool `json:"skipped,omitempty"`
	// Error: why it could not run.
	Error string `json:"error,omitempty"`
	// Rows: the values an SNMP step got.
	Rows []SNMPValue `json:"rows,omitempty"`
	// Checks: one per expect.
	Checks []CheckResult `json:"checks,omitempty"`
}

// CheckResult is one expect of a step.
type CheckResult struct {
	// What was checked, e.g. `status = 200`.
	What string `json:"what"`
	// Passed: it held.
	Passed bool `json:"passed"`
	// Got: the value found ("" when not found).
	Got string `json:"got,omitempty"`
}

const maxOut = 64 << 10

// CaseEnder is a Driver that keeps logins open while a case runs (one per session instead of one
// per step); EndCase is called when the case is done so it logs out.
type CaseEnder interface{ EndCase() }

// Run runs a case. vars win over the case's own.
func Run(ctx context.Context, c Case, d Driver, vars map[string]string) Result {
	if e, ok := d.(CaseEnder); ok {
		defer e.EndCase()
	}
	r := Result{Case: c.Name, Start: time.Now(), Passed: true}
	v := map[string]string{}
	for k, x := range c.Vars {
		v[k] = x
	}
	for k, x := range vars {
		v[k] = x
	}
	failed := false
	for i, st := range c.Steps {
		if failed || ctx.Err() != nil {
			r.Steps = append(r.Steps, StepResult{Name: Fill(stepName(st, i), v), Kind: stepKind(st), Skipped: true})
			continue
		}
		sr := runStep(ctx, st, i, d, v)
		r.Steps = append(r.Steps, sr)
		if !sr.Passed {
			r.Passed = false
			failed = !st.ContinueOnFail
		}
	}
	if len(c.Steps) == 0 {
		r.Passed, r.Error = false, "the test has no steps"
	}
	r.Ms = time.Since(r.Start).Milliseconds()
	return r
}

// RunParams runs a case once per Params entry (once when it has none); each result is named
// "case [name=value …]". vars win over the params, which win over the case's own vars.
func RunParams(ctx context.Context, c Case, d Driver, vars map[string]string) []Result {
	if len(c.Params) == 0 {
		return []Result{Run(ctx, c, d, vars)}
	}
	var out []Result
	for _, p := range c.Params {
		v := map[string]string{}
		for k, x := range p {
			v[k] = x
		}
		for k, x := range vars {
			v[k] = x
		}
		r := Run(ctx, c, d, v)
		r.Case = c.Name + " " + ParamName(p)
		out = append(out, r)
		if ctx.Err() != nil {
			break
		}
	}
	return out
}

// ParamName is "[k=v, …]" with the keys sorted.
func ParamName(p map[string]string) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		keys[i] = k + "=" + p[k]
	}
	return "[" + strings.Join(keys, ", ") + "]"
}

// RoleName makes a variable name for a session path: Lab/core-sw1 → core_sw1.
func RoleName(path string) string {
	base := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		base = path[i+1:]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "_"):
			b.WriteByte('_')
		}
	}
	n := strings.Trim(b.String(), "_")
	if n == "" || n[0] >= '0' && n[0] <= '9' {
		n = "dev_" + n
	}
	return n
}

func stepKind(s Step) string {
	if s.HTTP != nil {
		return "http"
	}
	if s.SNMP != nil {
		return "snmp"
	}
	return "run"
}

func stepName(s Step, i int) string {
	switch {
	case s.Name != "":
		return s.Name
	case s.Run != nil:
		return s.Run.Command
	case s.SNMP != nil:
		return snmpText(*s.SNMP)
	case s.HTTP != nil:
		m := strings.ToUpper(strings.TrimSpace(s.HTTP.Method))
		if m == "" {
			m = "GET"
		}
		return m + " " + s.HTTP.URL
	}
	return fmt.Sprintf("step %d", i+1)
}

func runStep(ctx context.Context, st Step, i int, d Driver, v map[string]string) StepResult {
	sr := StepResult{Name: Fill(stepName(st, i), v), Kind: stepKind(st)}
	to := time.Duration(st.Timeout) * time.Second
	if to <= 0 {
		to = 60 * time.Second
	}
	sctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	start := time.Now()
	var out string
	var err error
	var rows []SNMPValue
	switch {
	case actions(st) > 1:
		err = errors.New("a step has one of run, http and snmp")
	case st.Run != nil:
		sr.Target, sr.Sent = Fill(st.Run.Session, v), Fill(st.Run.Command, v)
		out, err = d.Command(sctx, sr.Target, sr.Sent, st.Run.Prompt)
	case st.HTTP != nil:
		h := fillHTTP(*st.HTTP, v)
		if h.Method == "" {
			h.Method = "GET"
		}
		sr.Target, sr.Sent = h.Session, h.Method+" "+h.URL
		var a HTTPAnswer
		a, err = d.HTTP(sctx, h)
		sr.Status, out = a.Status, a.Body
	case st.SNMP != nil:
		sn := fillSNMP(*st.SNMP, v)
		sr.Target, sr.Sent = sn.Session, snmpText(sn)
		rows, err = d.SNMP(sctx, sn)
		sr.Rows, out = rows, SNMPOutput(rows)
	default:
		err = errors.New("the step has nothing to do (run, http or snmp)")
	}
	sr.Ms = time.Since(start).Milliseconds()
	if len(out) > maxOut {
		sr.Output = out[:maxOut] + "\n… (cut)"
	} else {
		sr.Output = out
	}
	if err != nil {
		if sctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("no answer in %s", to)
		}
		sr.Error = err.Error()
		return sr
	}
	sr.Passed = true
	for _, e := range st.Expect {
		c := check(e, out, sr.Status, sr.Ms, v, rows)
		sr.Checks = append(sr.Checks, c)
		sr.Passed = sr.Passed && c.Passed
	}
	for _, s := range st.Save {
		val, ok := extract(s.Field, s.JSON, out)
		if s.OID != "" {
			var r SNMPValue
			r, ok = FindOID(rows, Fill(s.OID, v))
			val = r.Display
		}
		if ok && s.Var != "" {
			v[s.Var] = val
		}
	}
	return sr
}

var varRe = regexp.MustCompile(`\{\{\s*([\w.\-]+)\s*\}\}`)

// Fill replaces {{name}} with its value; unknown names stay as they are.
func Fill(s string, v map[string]string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	return varRe.ReplaceAllStringFunc(s, func(m string) string {
		if x, ok := v[varRe.FindStringSubmatch(m)[1]]; ok {
			return x
		}
		return m
	})
}

func fillHTTP(h HTTPStep, v map[string]string) HTTPStep {
	h.Session, h.Environment, h.Collection, h.URL, h.Body = Fill(h.Session, v), Fill(h.Environment, v), Fill(h.Collection, v), Fill(h.URL, v), Fill(h.Body, v)
	h.Method = strings.ToUpper(strings.TrimSpace(h.Method))
	if len(h.Headers) > 0 {
		m := make(map[string]string, len(h.Headers))
		for k, x := range h.Headers {
			m[k] = Fill(x, v)
		}
		h.Headers = m
	}
	return h
}

func check(e Expect, out string, status int, ms int64, v map[string]string, rows []SNMPValue) CheckResult {
	switch {
	case e.Contains != "":
		t := Fill(e.Contains, v)
		return CheckResult{What: fmt.Sprintf("contains %q", t), Passed: strings.Contains(out, t)}
	case e.NotContains != "":
		t := Fill(e.NotContains, v)
		return CheckResult{What: fmt.Sprintf("does not contain %q", t), Passed: !strings.Contains(out, t)}
	case e.Matches != "":
		re, err := regexp.Compile(Fill(e.Matches, v))
		if err != nil {
			return CheckResult{What: "matches " + e.Matches, Got: "bad pattern: " + err.Error()}
		}
		return CheckResult{What: "matches " + re.String(), Passed: re.MatchString(out), Got: re.FindString(out)}
	case e.Status != 0:
		return CheckResult{What: fmt.Sprintf("status = %d", e.Status), Passed: status == e.Status, Got: strconv.Itoa(status)}
	case e.OID != "":
		r, ok := FindOID(rows, Fill(e.OID, v))
		c := compare("oid "+e.OID, r.Display, ok, e, v)
		if !c.Passed && ok && (e.Equals != nil || e.NotEquals != nil || e.Greater != nil || e.Less != nil) {
			if c2 := compare("oid "+e.OID, r.Value, ok, e, v); c2.Passed {
				c2.Got = r.Display
				return c2
			}
		}
		return c
	case e.MaxMs != 0 && e.Field == "" && e.JSON == "":
		return CheckResult{What: fmt.Sprintf("took ≤ %d ms", e.MaxMs), Passed: ms <= e.MaxMs, Got: fmt.Sprintf("%d ms", ms)}
	case e.Field != "" || e.JSON != "":
		subj := "json " + e.JSON
		if e.Field != "" {
			subj = "field " + e.Field
		}
		got, ok := extract(Fill(e.Field, v), Fill(e.JSON, v), out)
		return compare(subj, got, ok, e, v)
	}
	return CheckResult{What: "empty check", Got: "set contains, matches, field, json, status or maxMs"}
}

func compare(subj, got string, found bool, e Expect, v map[string]string) CheckResult {
	c := CheckResult{What: subj, Got: got, Passed: found}
	var parts []string
	if e.Exists != nil {
		c.Passed = found == *e.Exists
		parts = append(parts, fmt.Sprintf("exists = %v", *e.Exists))
	}
	if e.Equals != nil {
		w := Fill(*e.Equals, v)
		c.Passed = c.Passed && found && got == w
		parts = append(parts, fmt.Sprintf("= %q", w))
	}
	if e.NotEquals != nil {
		w := Fill(*e.NotEquals, v)
		c.Passed = c.Passed && got != w
		parts = append(parts, fmt.Sprintf("≠ %q", w))
	}
	if e.Greater != nil || e.Less != nil {
		n, err := strconv.ParseFloat(strings.TrimSpace(got), 64)
		if err != nil {
			c.Passed = false
		}
		if e.Greater != nil {
			c.Passed = c.Passed && n > *e.Greater
			parts = append(parts, "> "+strconv.FormatFloat(*e.Greater, 'g', -1, 64))
		}
		if e.Less != nil {
			c.Passed = c.Passed && n < *e.Less
			parts = append(parts, "< "+strconv.FormatFloat(*e.Less, 'g', -1, 64))
		}
	}
	if len(parts) > 0 {
		c.What += " " + strings.Join(parts, " and ")
	} else {
		c.What += " found"
	}
	return c
}

// extract finds a value: field is a regular expression whose first group (else the match) is the
// value; path is a JSON path into out.
func extract(field, path, out string) (string, bool) {
	if field != "" {
		re, err := regexp.Compile(field)
		if err != nil {
			return "", false
		}
		m := re.FindStringSubmatch(out)
		if m == nil {
			return "", false
		}
		if len(m) > 1 {
			return m[1], true
		}
		return m[0], true
	}
	if path == "" {
		return "", false
	}
	var doc any
	if json.Unmarshal([]byte(out), &doc) != nil {
		return "", false
	}
	x, ok := JSONPath(doc, path)
	if !ok {
		return "", false
	}
	switch t := x.(type) {
	case string:
		return t, true
	case nil:
		return "null", true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(t), true
	}
	b, _ := json.Marshal(x)
	return string(b), true
}

var pathPart = regexp.MustCompile(`([^.\[\]]+)|\[(\d+|-1)\]|\["([^"]*)"\]`)

// JSONPath walks a decoded JSON document: a.b[0].c, $.a, ["key with dots"], [-1] for the last item
// and length (the number of items or keys).
func JSONPath(doc any, path string) (any, bool) {
	path = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(path), "$"), ".")
	cur := doc
	for _, m := range pathPart.FindAllStringSubmatch(path, -1) {
		switch {
		case m[2] != "":
			a, ok := cur.([]any)
			if !ok {
				return nil, false
			}
			i, _ := strconv.Atoi(m[2])
			if i < 0 {
				i = len(a) + i
			}
			if i < 0 || i >= len(a) {
				return nil, false
			}
			cur = a[i]
		default:
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
	}
	return cur, true
}

// StepAnswer is what RunOne got: the generated tests check it themselves.
type StepAnswer struct {
	// Output: the command's output or the HTTP body.
	Output string `json:"output"`
	// Status: the HTTP status.
	Status int `json:"status,omitempty"`
	// Ms: how long it took.
	Ms int64 `json:"ms"`
	// Error: why it could not run.
	Error string `json:"error,omitempty"`
	// Rows: the values an SNMP step got.
	Rows []SNMPValue `json:"rows,omitempty"`
}

// RunOne runs one step's action (variables already filled in) without its checks - for the
// generated tests, which send their steps to Termwright one by one.
func RunOne(ctx context.Context, st Step, d Driver) StepAnswer {
	sr := runStep(ctx, Step{Run: st.Run, HTTP: st.HTTP, SNMP: st.SNMP, Timeout: st.Timeout}, 0, d, map[string]string{})
	return StepAnswer{Output: sr.Output, Status: sr.Status, Ms: sr.Ms, Error: sr.Error, Rows: sr.Rows}
}

func actions(s Step) int {
	n := 0
	for _, b := range []bool{s.Run != nil, s.HTTP != nil, s.SNMP != nil} {
		if b {
			n++
		}
	}
	return n
}

func snmpText(s SNMPStep) string {
	op := strings.ToLower(s.Op)
	if op == "" {
		op = "get"
	}
	if op == "set" {
		var p []string
		for _, x := range s.Values {
			p = append(p, x.OID+" = "+x.Value)
		}
		return "set " + strings.Join(p, ", ")
	}
	return op + " " + strings.Join(s.OIDs, " ")
}

func fillSNMP(s SNMPStep, v map[string]string) SNMPStep {
	s.Session = Fill(s.Session, v)
	s.Op = strings.ToLower(strings.TrimSpace(s.Op))
	if s.Op == "" {
		s.Op = "get"
	}
	oids := make([]string, len(s.OIDs))
	for i, o := range s.OIDs {
		oids[i] = Fill(o, v)
	}
	s.OIDs = oids
	vals := make([]SNMPSetValue, len(s.Values))
	for i, x := range s.Values {
		vals[i] = SNMPSetValue{OID: Fill(x.OID, v), Type: x.Type, Value: Fill(x.Value, v)}
	}
	s.Values = vals
	return s
}

// SNMPOutput is an SNMP step's values as text, one "name = value" line each, for contains, matches
// and field checks.
func SNMPOutput(rows []SNMPValue) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.Name + " = " + r.Display + "\n")
	}
	return b.String()
}

// FindOID finds a value by name (ifOperStatus.5, IF-MIB::ifOperStatus.5, ifOperStatus = the first
// instance) or numeric OID.
func FindOID(rows []SNMPValue, oid string) (SNMPValue, bool) {
	oid = strings.TrimPrefix(strings.TrimSpace(oid), ".")
	for _, r := range rows {
		if r.OID == oid || r.Name == oid {
			return r, true
		}
	}
	for _, r := range rows {
		n := r.Name
		if _, after, ok := strings.Cut(n, "::"); ok && !strings.Contains(oid, "::") {
			n = after
		}
		if n == oid || strings.HasPrefix(n, oid+".") || strings.HasPrefix(r.OID, oid+".") {
			return r, true
		}
	}
	return SNMPValue{}, false
}
