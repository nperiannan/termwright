package keepawake

import (
	"os"
	"os/exec"
	"strconv"
)

var cmd *exec.Cmd

// macOS: caffeinate -i holds an idle-sleep assertion; -w ends it when Termwright exits, even on a crash.
func start() error {
	c := exec.Command("/usr/bin/caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
	if err := c.Start(); err != nil {
		return err
	}
	cmd = c
	go c.Wait()
	return nil
}

func stop() {
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
	cmd = nil
}
