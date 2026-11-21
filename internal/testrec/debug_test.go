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
	r = RunWith(context.Background(), c, f, nil, RunOptions{Pause: func(context.Context, string, string, StepResult) string {
		n++
		if n == 1 {
			return "retry"
		}
		return "continue"
	}})
	if n != 2 || strings.Count(strings.Join(f.sent, ","), "s: show vlan,") != 2 || r.Passed || len(f.sent) != 5 {
		t.Fatalf("pause: %d %v %+v", n, f.sent, r.Passed)
	}
}
