//go:build !darwin && !windows && !linux

package keepawake

import "errors"

func start() error { return errors.New("not supported on this system") }
func stop()        {}
