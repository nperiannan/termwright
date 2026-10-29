//go:build !windows

package wol

import "os/exec"

func hide(*exec.Cmd) {}
