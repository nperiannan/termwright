//go:build !darwin || !cgo

// Package macwin reads where macOS draws the window buttons; elsewhere there
// are none to line up with.
package macwin

import "errors"

// WindowButtons reports no window buttons outside macOS.
func WindowButtons() (centerY, right float64, ok bool) { return 0, 0, false }

// StartDictation: macOS only (Windows has its own, see dictation_windows.go in the app).
func StartDictation() error { return errNoDictation }

var errNoDictation = errors.New("no dictation here")
