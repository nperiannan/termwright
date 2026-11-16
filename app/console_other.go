//go:build !windows

package app

// attachConsole: elsewhere the program has its terminal already.
func attachConsole() {}
