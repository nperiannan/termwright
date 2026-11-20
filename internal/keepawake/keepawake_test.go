package keepawake

import "testing"

func TestSetIdempotent(t *testing.T) {
	if err := Set(false); err != nil || Active() {
		t.Fatalf("off: %v %v", err, Active())
	}
	if err := Set(true); err != nil {
		t.Skip("no sleep control here:", err) // CI containers have no desktop or logind
	}
	if !Active() || Set(true) != nil {
		t.Fatal("second Set(true)")
	}
	if Set(false); Active() {
		t.Fatal("still active")
	}
}
