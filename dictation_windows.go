//go:build windows

package main

import "syscall"

// winVoiceTyping presses Windows+H: Windows voice typing in the focused text field.
func winVoiceTyping() error {
	p := syscall.NewLazyDLL("user32.dll").NewProc("keybd_event")
	if err := p.Find(); err != nil {
		return err
	}
	const win, h, up = 0x5B, 0x48, 0x2
	_, _, _ = p.Call(win, 0, 0, 0)
	_, _, _ = p.Call(h, 0, 0, 0)
	_, _, _ = p.Call(h, 0, up, 0)
	_, _, _ = p.Call(win, 0, up, 0)
	return nil
}
