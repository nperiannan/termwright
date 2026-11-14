package main

import (
	"runtime"
	"testing"
)

// Without a dictation Termwright can start, the microphone gives a hint instead.
func TestStartDictationHint(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the hint is for systems without dictation")
	}
	if h := (&App{}).StartDictation(); h == "" {
		t.Fatal("no hint on Linux")
	}
}
