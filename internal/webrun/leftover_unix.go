//go:build !windows

package webrun

import (
	"os/exec"
	"syscall"
)

func hide(*exec.Cmd) {}

func stop(pid int, force bool) {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	_ = syscall.Kill(pid, sig)
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }
