package discover

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps a console window from flashing up for helper commands.
func hideWindow(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
