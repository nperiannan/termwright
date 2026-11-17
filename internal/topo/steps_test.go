package topo

import "testing"

func TestStepsDone(t *testing.T) {
	n := len(Plan("exos"))
	if n == 0 {
		t.Fatal("no plan")
	}
	if d, now := StepsDone("exos", 0); d != 1 || now == "" {
		t.Errorf("before any command: done=%d now=%q, want 1 and a command", d, now)
	}
	prev := 0
	for k := 0; k <= n; k++ {
		d, _ := StepsDone("exos", k)
		if d < prev {
			t.Errorf("steps went back at command %d: %d after %d", k, d, prev)
		}
		prev = d
	}
	if d, now := StepsDone("exos", n); d != 4 || now != "" {
		t.Errorf("after all commands: done=%d now=%q, want 4 and nothing", d, now)
	}
	if d, _ := StepsDone("nope", 3); d != 4 {
		t.Errorf("unknown OS has no plan, got %d", d)
	}
}
