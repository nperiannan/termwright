package term

import "testing"

func TestSameShape(t *testing.T) {
	if !sameShape("* Slot-1 Stack.6 #", "* Slot-1 Stack.5 #") || !sameShape("sw1#", "sw1#") || sameShape("sw1(config)#", "sw1#") || sameShape("", "") {
		t.Fatal("sameShape")
	}
}
