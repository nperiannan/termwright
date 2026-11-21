package testrec

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// rangeValues: "1-8" → 1..8; "1:1-1:4" → 1:1..1:4 (the last number counts).
func rangeValues(r string) ([]string, error) {
	a, b, ok := strings.Cut(strings.TrimSpace(r), "-")
	if !ok {
		return nil, fmt.Errorf("range %q: write it as 1-8", r)
	}
	re := regexp.MustCompile(`^(.*?)(\d+)$`)
	ma, mb := re.FindStringSubmatch(strings.TrimSpace(a)), re.FindStringSubmatch(strings.TrimSpace(b))
	if ma == nil || mb == nil {
		return nil, fmt.Errorf("range %q: write it as 1-8 or 1:1-1:8", r)
	}
	pre := ma[1]
	if mb[1] != "" && mb[1] != pre {
		return nil, fmt.Errorf("range %q: both ends need the same prefix", r)
	}
	x, _ := strconv.Atoi(ma[2])
	y, _ := strconv.Atoi(mb[2])
	if y < x || y-x > 4096 {
		return nil, fmt.Errorf("range %q: from a smaller to a larger number, at most 4096 values", r)
	}
	var out []string
	for i := x; i <= y; i++ {
		out = append(out, pre+strconv.Itoa(i))
	}
	return out, nil
}

// LoopValues are a loop's values with variables filled in.
func LoopValues(l Loop, v map[string]string) ([]string, error) {
	switch {
	case len(l.Values) > 0:
		out := make([]string, len(l.Values))
		for i, x := range l.Values {
			out[i] = Fill(x, v)
		}
		return out, nil
	case l.Range != "":
		return rangeValues(Fill(l.Range, v))
	}
	src := Fill(l.From, v)
	if strings.Contains(src, "{{") {
		return nil, fmt.Errorf("loop from %s: the variable has no value", l.From)
	}
	sep := func(r rune) bool { return r == ',' || r == '\n' || r == ';' }
	if !strings.ContainsAny(src, ",\n;") {
		sep = func(r rune) bool { return r == ' ' || r == '\t' }
	}
	var out []string
	for _, x := range strings.FieldsFunc(src, sep) {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out, nil
}

var condOps = []string{" !contains ", " contains ", " != ", " == ", " >= ", " <= ", " ~ ", " > ", " < "}

// Cond evaluates an if condition (variables already filled in).
func Cond(s string) (bool, error) {
	s = strings.TrimSpace(s)
	for _, op := range condOps {
		a, b, ok := strings.Cut(s, op)
		if !ok {
			continue
		}
		a, b = strings.Trim(strings.TrimSpace(a), `"'`), strings.Trim(strings.TrimSpace(b), `"'`)
		fa, ea := strconv.ParseFloat(a, 64)
		fb, eb := strconv.ParseFloat(b, 64)
		num := ea == nil && eb == nil
		switch strings.TrimSpace(op) {
		case "==":
			return a == b || num && fa == fb, nil
		case "!=":
			return !(a == b || num && fa == fb), nil
		case "contains":
			return strings.Contains(a, b), nil
		case "!contains":
			return !strings.Contains(a, b), nil
		case "~":
			re, err := regexp.Compile(b)
			if err != nil {
				return false, fmt.Errorf("if %s: %v", s, err)
			}
			return re.MatchString(a), nil
		}
		if !num {
			return false, fmt.Errorf("if %s: %s compares numbers", s, strings.TrimSpace(op))
		}
		switch strings.TrimSpace(op) {
		case ">":
			return fa > fb, nil
		case "<":
			return fa < fb, nil
		case ">=":
			return fa >= fb, nil
		}
		return fa <= fb, nil
	}
	l := strings.ToLower(strings.Trim(s, `"'`))
	return l != "" && l != "0" && l != "false" && l != "no" && !strings.Contains(l, "{{"), nil
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// execStep runs a step with its logic - if, wait, loop, retry - and returns its results (a loop gives
// one per inner step and value).
func execStep(ctx context.Context, st Step, i int, d Driver, v map[string]string) []StepResult {
	name := Fill(stepName(st, i), v)
	if st.If != "" {
		ok, err := Cond(Fill(st.If, v))
		if err != nil {
			return []StepResult{{Name: name, Kind: stepKind(st), Error: err.Error()}}
		}
		if !ok {
			return []StepResult{{Name: name, Kind: stepKind(st), Skipped: true, Passed: true, Error: "skipped: if " + Fill(st.If, v) + " is false"}}
		}
	}
	if st.Wait > 0 {
		if !sleepCtx(ctx, time.Duration(st.Wait*float64(time.Second))) {
			return []StepResult{{Name: name, Kind: "wait", Error: "stopped"}}
		}
		if actions(st) == 0 && st.Loop == nil {
			return []StepResult{{Name: fmt.Sprintf("wait %g s", st.Wait), Kind: "wait", Passed: true, Ms: int64(st.Wait * 1000)}}
		}
	}
	if st.Loop != nil {
		vals, err := LoopValues(*st.Loop, v)
		if err != nil {
			return []StepResult{{Name: name, Kind: "loop", Error: err.Error()}}
		}
		old, had := v[st.Loop.Var]
		defer func() {
			if had {
				v[st.Loop.Var] = old
			} else {
				delete(v, st.Loop.Var)
			}
		}()
		var out []StepResult
		for _, x := range vals {
			v[st.Loop.Var] = x
			for j, in := range st.Loop.Steps {
				rs := execStep(ctx, in, j, d, v)
				for k := range rs {
					rs[k].Name += fmt.Sprintf(" [%s=%s]", st.Loop.Var, x)
				}
				out = append(out, rs...)
				failed := false
				for _, r := range rs {
					failed = failed || !r.Passed && !r.Skipped && !in.ContinueOnFail
				}
				if failed || ctx.Err() != nil {
					return out
				}
			}
		}
		if len(out) == 0 {
			out = append(out, StepResult{Name: name, Kind: "loop", Passed: true, Skipped: true, Error: "the loop has no values"})
		}
		return out
	}
	if st.Retry == nil {
		return []StepResult{runStep(ctx, st, i, d, v)}
	}
	return []StepResult{retryStep(ctx, st, i, d, v)}
}

// retryStep runs a step until its checks pass or its retry time is up.
func retryStep(ctx context.Context, st Step, i int, d Driver, v map[string]string) StepResult {
	to, iv := st.Retry.Timeout, st.Retry.Interval
	if to <= 0 {
		to = 60
	}
	if iv <= 0 {
		iv = 5
	}
	end := time.Now().Add(time.Duration(to * float64(time.Second)))
	start := time.Now()
	for n := 1; ; n++ {
		// saved values only count once the checks pass
		tv := make(map[string]string, len(v))
		for k, x := range v {
			tv[k] = x
		}
		sr := runStep(ctx, st, i, d, tv)
		if sr.Passed || time.Now().Add(time.Duration(iv*float64(time.Second))).After(end) || ctx.Err() != nil {
			for k, x := range tv {
				v[k] = x
			}
			if n > 1 {
				sr.Name += fmt.Sprintf(" (%d tries)", n)
				sr.Ms = time.Since(start).Milliseconds()
			}
			if !sr.Passed && sr.Error == "" {
				sr.Error = fmt.Sprintf("the checks did not pass within %g s (%d tries)", to, n)
			}
			return sr
		}
		if !sleepCtx(ctx, time.Duration(iv*float64(time.Second))) {
			return sr
		}
	}
}
