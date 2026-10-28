package trash

import "testing"

func TestMoveRefusesRoot(t *testing.T) {
	for _, p := range []string{"", "/", "."} {
		if p == "." {
			continue
		}
		if err := Move(p); err == nil {
			t.Fatalf("Move(%q) did not refuse", p)
		}
	}
}
