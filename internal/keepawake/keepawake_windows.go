package keepawake

import (
	"errors"
	"runtime"
	"syscall"
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var (
	setState = syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")
	quit     chan struct{}
)

// Windows: the execution state belongs to a thread, so one locked thread holds it until stop;
// Windows drops it by itself when Termwright exits.
func start() error {
	ok := make(chan error, 1)
	q := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if r, _, err := setState.Call(esContinuous | esSystemRequired); r == 0 {
			ok <- errors.New("SetThreadExecutionState: " + err.Error())
			return
		}
		ok <- nil
		<-q
		setState.Call(esContinuous)
	}()
	if err := <-ok; err != nil {
		return err
	}
	quit = q
	return nil
}

func stop() {
	if quit != nil {
		close(quit)
		quit = nil
	}
}
