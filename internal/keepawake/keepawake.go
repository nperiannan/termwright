// Package keepawake stops the computer from going to sleep on its own while Termwright runs, so
// connected sessions, transfers, captures and the emulated lab are not cut off. Only idle sleep is
// stopped: the display may still turn off, and the user can still put the computer to sleep.
package keepawake

import "sync"

var (
	mu sync.Mutex
	on bool
)

// Set turns keeping the computer awake on or off. Turning it on twice is harmless.
func Set(want bool) error {
	mu.Lock()
	defer mu.Unlock()
	if want == on {
		return nil
	}
	if want {
		if err := start(); err != nil {
			return err
		}
	} else {
		stop()
	}
	on = want
	return nil
}

// Active reports whether the computer is being kept awake.
func Active() bool {
	mu.Lock()
	defer mu.Unlock()
	return on
}
