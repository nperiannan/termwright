//go:build !darwin || !cgo

// Package macwin reads where macOS draws the window buttons; elsewhere there
// are none to line up with.
package macwin

// WindowButtons reports no window buttons outside macOS.
func WindowButtons() (centerY, right float64, ok bool) { return 0, 0, false }
