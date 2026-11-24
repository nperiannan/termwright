package webrun

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func hide(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func stop(pid int, force bool) {
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	c := exec.Command("taskkill", args...)
	hide(c)
	_ = c.Run()
}

func alive(pid int) bool {
	c := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH")
	hide(c)
	b, _ := c.Output()
	return strings.Contains(string(b), " "+strconv.Itoa(pid)+" ")
}
