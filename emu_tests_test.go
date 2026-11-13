package main

import "testing"

// The rate search ends within its resolution below the highest passing rate, in few trials.
func TestEmuSearch(t *testing.T) {
	for _, c := range []struct{ top, limit int }{{10000, 3700}, {10000, 10000}, {10000, 1}, {4096, 1000}} {
		got, trials, err := emuSearch(c.top, 0.02, func(n int) (bool, error) { return n <= c.limit, nil })
		if err != nil || got > c.limit || float64(c.limit-got) > float64(c.limit)*0.02+1 || trials > 20 {
			t.Fatalf("limit %d: got %d in %d trials", c.limit, got, trials)
		}
	}
	if got, _, _ := emuSearch(1000, 0.02, func(int) (bool, error) { return false, nil }); got != 0 {
		t.Fatalf("nothing passes: %d", got)
	}
}
