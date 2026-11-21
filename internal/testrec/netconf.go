package testrec

import (
	"context"
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// NetconfDriver is a Driver that can send NETCONF operations.
type NetconfDriver interface {
	Netconf(ctx context.Context, n NetconfStep) (string, error)
}

func fillNetconf(n NetconfStep, v map[string]string) NetconfStep {
	n.Session, n.Filter, n.Config, n.RPC = Fill(n.Session, v), Fill(n.Filter, v), Fill(n.Config, v), Fill(n.RPC, v)
	return n
}

// NetconfText is a step's NETCONF operation in a few words.
func NetconfText(n NetconfStep) string {
	op := strings.ToLower(n.Op)
	if op == "" {
		op = "get"
	}
	switch op {
	case "get-config":
		return "netconf get-config " + orDefault(n.Source, "running")
	case "edit-config":
		return "netconf edit-config " + orDefault(n.Target, "running")
	case "rpc":
		r := strings.TrimSpace(n.RPC)
		if i := strings.IndexAny(r, " />"); strings.HasPrefix(r, "<") && i > 1 {
			return "netconf rpc " + r[1:i]
		}
		return "netconf rpc"
	}
	return "netconf " + op
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// NetconfRPC is the operation's XML inside <rpc> (and a second one, <commit/>, for a candidate edit).
func NetconfRPC(n NetconfStep) ([]string, error) {
	filter := ""
	if f := strings.TrimSpace(n.Filter); f != "" {
		filter = `<filter type="subtree">` + f + `</filter>`
	}
	ds := func(s string) (string, error) {
		s = strings.ToLower(orDefault(s, "running"))
		if s != "running" && s != "candidate" && s != "startup" {
			return "", fmt.Errorf("datastore %q: running, candidate or startup", s)
		}
		return "<" + s + "/>", nil
	}
	switch strings.ToLower(orDefault(n.Op, "get")) {
	case "get":
		return []string{"<get>" + filter + "</get>"}, nil
	case "get-config":
		s, err := ds(n.Source)
		if err != nil {
			return nil, err
		}
		return []string{"<get-config><source>" + s + "</source>" + filter + "</get-config>"}, nil
	case "edit-config":
		t, err := ds(n.Target)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(n.Config) == "" {
			return nil, fmt.Errorf("edit-config needs config")
		}
		out := []string{"<edit-config><target>" + t + "</target><config>" + n.Config + "</config></edit-config>"}
		if t == "<candidate/>" {
			out = append(out, "<commit/>")
		}
		return out, nil
	case "rpc":
		if strings.TrimSpace(n.RPC) == "" {
			return nil, fmt.Errorf("op rpc needs rpc (the operation's XML)")
		}
		return []string{n.RPC}, nil
	}
	return nil, fmt.Errorf("netconf op %q: get, get-config, edit-config or rpc", n.Op)
}

// XMLDoc turns XML into nested maps for json-style paths ("data.vlans.vlan[0].name"): an element is a map
// of its children (repeated ones become a list), a leaf is its text; namespaces and attributes are left out.
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
		switch e := t.(type) {
		case xml.StartElement:
			stack = append(stack, &frame{name: e.Name.Local, kids: map[string]any{}})
		case xml.CharData:
			stack[len(stack)-1].text.Write(e)
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
	// skip the <rpc-reply> wrapper so paths start at data
	if r, ok := root.kids["rpc-reply"].(map[string]any); ok && len(root.kids) == 1 {
		return r
	}
	return root.kids
}

// sampleStep runs a step Sample.Count times and checks agg expectations over all the values.
func sampleStep(ctx context.Context, st Step, i int, d Driver, v map[string]string) StepResult {
	n := st.Sample.Count
	if n > 1000 {
		n = 1000
	}
	iv := time.Duration(st.Sample.Interval * float64(time.Second))
	plain := st
	plain.Sample, plain.Expect = nil, nil
	var aggs []Expect
	for _, e := range st.Expect {
		if e.Agg != "" {
			aggs = append(aggs, e)
		} else {
			plain.Expect = append(plain.Expect, e)
		}
	}
	type sample struct {
		at   time.Time
		vals []string
		ok   []bool
	}
	var samples []sample
	var last StepResult
	var totalMs int64
	for k := 0; k < n; k++ {
		if k > 0 && !sleepCtx(ctx, iv) {
			last.Error = "stopped"
			last.Passed = false
			break
		}
		at := time.Now()
		last = runStep(ctx, plain, i, d, v)
		totalMs += last.Ms
		if last.Error != "" {
			break
		}
		sm := sample{at: at}
		for _, e := range aggs {
			var got string
			var ok bool
			if e.OID != "" {
				r, f := FindOID(last.Rows, Fill(e.OID, v))
				got, ok = r.Value, f
			} else {
				got, ok = extract(Fill(e.Field, v), Fill(e.JSON, v), last.Output)
			}
			sm.vals, sm.ok = append(sm.vals, got), append(sm.ok, ok)
		}
		samples = append(samples, sm)
	}
	last.Name = fmt.Sprintf("%s (%d samples)", Fill(stepName(st, i), v), len(samples))
	last.Ms = totalMs
	for j, e := range aggs {
		var xs []float64
		var ts []time.Time
		for _, sm := range samples {
			if f, err := strconv.ParseFloat(strings.TrimSpace(sm.vals[j]), 64); err == nil && sm.ok[j] {
				xs, ts = append(xs, f), append(ts, sm.at)
			}
		}
		subj := e.JSON
		switch {
		case e.Field != "":
			subj = "field " + e.Field
		case e.OID != "":
			subj = "oid " + e.OID
		default:
			subj = "json " + subj
		}
		what := strings.ToLower(e.Agg) + " of " + subj
		val, ok := aggregate(strings.ToLower(e.Agg), xs, ts)
		got := ""
		if ok {
			got = strconv.FormatFloat(val, 'f', -1, 64)
		}
		c := compare(what, got, ok, Expect{Equals: e.Equals, NotEquals: e.NotEquals, Greater: e.Greater, Less: e.Less, Exists: e.Exists}, v)
		if !ok {
			c.Got = fmt.Sprintf("%d of %d samples had a number", len(xs), len(samples))
		}
		last.Checks = append(last.Checks, c)
		last.Passed = last.Passed && c.Passed
		last.Unchecked = false
	}
	return last
}

func aggregate(agg string, xs []float64, ts []time.Time) (float64, bool) {
	if len(xs) == 0 {
		return 0, false
	}
	switch agg {
	case "min":
		m := math.Inf(1)
		for _, x := range xs {
			m = math.Min(m, x)
		}
		return m, true
	case "max":
		m := math.Inf(-1)
		for _, x := range xs {
			m = math.Max(m, x)
		}
		return m, true
	case "avg":
		s := 0.0
		for _, x := range xs {
			s += x
		}
		return s / float64(len(xs)), true
	case "delta":
		return xs[len(xs)-1] - xs[0], len(xs) > 1
	case "rate":
		if len(xs) < 2 {
			return 0, false
		}
		dt := ts[len(ts)-1].Sub(ts[0]).Seconds()
		if dt <= 0 {
			return 0, false
		}
		return (xs[len(xs)-1] - xs[0]) / dt, true
	}
	return 0, false
}
