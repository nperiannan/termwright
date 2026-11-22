package testrec

import (
	"context"
	"strings"
	"testing"
)

func TestRunFromOnlyPauseVars(t *testing.T) {
	c := Case{Name: "dbg", Vars: map[string]string{"v": "42"}, Setup: []Step{{Run: &RunStep{Session: "s", Command: "show version"}}},
		Steps: []Step{
			{Run: &RunStep{Session: "s", Command: "show vlan {{v}}"}, Expect: []Expect{{Contains: "v42"}}},
			{Run: &RunStep{Session: "s", Command: "show vlan"}, Expect: []Expect{{Contains: "nothing like this"}}},
			{Run: &RunStep{Session: "s", Command: "show version"}},
		}}
	f := &fake{}
	r := RunWith(context.Background(), c, f, nil, RunOptions{Only: 1})
	if strings.Join(f.sent, ",") != "s: show vlan 42" || !r.Passed {
		t.Fatalf("only: %v %+v", f.sent, r)
	}
	if r.Steps[1].Vars["v"] != "42" {
		t.Fatalf("vars %+v", r.Steps[1])
	}
	f = &fake{}
	RunWith(context.Background(), c, f, nil, RunOptions{From: 3})
	if strings.Join(f.sent, ",") != "s: show version,s: show version" {
		t.Fatalf("from: %v", f.sent)
	}
	f = &fake{}
	n := 0
	r = RunWith(context.Background(), c, f, nil, RunOptions{Pause: func(context.Context, string, string, StepResult) (string, map[string]string) {
		n++
		if n == 1 {
			return "retry", nil
		}
		return "continue", nil
	}})
	if n != 2 || strings.Count(strings.Join(f.sent, ","), "s: show vlan,") != 2 || r.Passed || len(f.sent) != 5 {
		t.Fatalf("pause: %d %v %+v", n, f.sent, r.Passed)
	}
}

func TestBreakpoints(t *testing.T) {
	c := Case{Name: "bp", Vars: map[string]string{"v": "1"}, Steps: []Step{
		{Run: &RunStep{Session: "s", Command: "show vlan {{v}}"}},
		{Run: &RunStep{Session: "s", Command: "show version"}},
		{Run: &RunStep{Session: "s", Command: "show vlan"}},
	}}
	f := &fake{}
	var at []string
	acts := []string{"step", "skip", "continue"}
	RunWith(context.Background(), c, f, nil, RunOptions{Breaks: []int{1}, Pause: func(_ context.Context, _, _ string, sr StepResult) (string, map[string]string) {
		at = append(at, sr.Error)
		a := acts[0]
		acts = acts[1:]
		return a, map[string]string{"v": "42"}
	}})
	// step 1 (v set to 42), stop before 2 (skip), stop before 3 (continue)
	if len(at) != 3 || strings.Join(f.sent, ",") != "s: show vlan 42,s: show vlan" {
		t.Fatalf("%v %v", at, f.sent)
	}
}
