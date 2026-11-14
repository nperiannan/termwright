//go:build windows

package main

import (
	"os"
	"syscall"
)

// attachConsole gives "Termwright lab …" (a GUI program on Windows: no console of its own) the console of
// the Command Prompt or PowerShell it was typed in, so its output shows there. Output already redirected to
// a file or a pipe stays where it goes.
func attachConsole() {
	k := syscall.NewLazyDLL("kernel32.dll")
	if r, _, _ := k.NewProc("AttachConsole").Call(uintptr(^uint32(0))); r == 0 { // ATTACH_PARENT_PROCESS
		return
	}
	redirect := func(f **os.File, name string, flag int) {
		if *f != nil {
			if st, err := (*f).Stat(); err == nil && (st.Mode().IsRegular() || st.Mode()&os.ModeNamedPipe != 0) {
				return // redirected to a file or a pipe
			}
		}
		if h, err := os.OpenFile(name, flag, 0); err == nil {
			*f = h
		}
	}
	redirect(&os.Stdout, "CONOUT$", os.O_WRONLY)
	redirect(&os.Stderr, "CONOUT$", os.O_WRONLY)
	redirect(&os.Stdin, "CONIN$", os.O_RDONLY)
}
