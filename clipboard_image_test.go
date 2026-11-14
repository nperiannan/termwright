package main

import "testing"

// osascript prints the clipboard's PNG as hex; anything else is no picture.
func TestMacClipData(t *testing.T) {
	b := macClipData("«data PNGf89504E470D0A1A0A0000000D»\n")
	if !isPNG(b) || len(b) != 12 {
		t.Fatalf("%x", b)
	}
	for _, s := range []string{"", "«data TIFF4D4D»", "«data PNGfZZ»", "some text"} {
		if isPNG(macClipData(s)) {
			t.Errorf("%q taken for a picture", s)
		}
	}
}
