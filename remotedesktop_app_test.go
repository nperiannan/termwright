package main

import (
	"bytes"
	"os/exec"
	"testing"
)

// The script that is copied to the Linux machine must be embedded and valid bash.
func TestRemoteDesktopScript(t *testing.T) {
	if len(remoteDesktopScript) < 1000 || !bytes.HasPrefix(remoteDesktopScript, []byte("#!/usr/bin/env bash")) {
		t.Fatal("setup script is not embedded")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	cmd := exec.Command(bash, "-n")
	cmd.Stdin = bytes.NewReader(remoteDesktopScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}
}
