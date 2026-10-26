package discover

import "testing"

func TestNormalizeMAC(t *testing.T) {
	for in, want := range map[string]string{
		"AA-BB-CC-DD-EE-01": "aa:bb:cc:dd:ee:01", "aabb.ccdd.ee01": "aa:bb:cc:dd:ee:01", "aabbccddee01": "aa:bb:cc:dd:ee:01",
		"0:1b:2:3:4:5": "00:1b:02:03:04:05", "": "", "nope": "", "ff:ff:ff:ff:ff:ff": "",
	} {
		if got := NormalizeMAC(in); got != want {
			t.Errorf("%q: %q want %q", in, got, want)
		}
	}
}
