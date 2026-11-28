package emu

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWFSFast(t *testing.T) {
	d := t.TempDir()
	bin := filepath.Join(d, "bin")
	os.MkdirAll(bin, 0o755)
	// a fake guestfish: "-a disk -m fs upload src dst : cat dst" copies src to $d/store and prints it
	os.WriteFile(filepath.Join(bin, "guestfish"), []byte("#!/bin/sh\necho \"$@\" >> "+d+"/calls\nif [ \"$5\" = upload ]; then cp \"$6\" "+d+"/store; cat "+d+"/store; exit 0; fi\nexit 1\n"), 0o755)
	l := &Lab{ID: "a", Dir: "x"}
	s := l.WriteFileScriptAt(VM{ID: "v1"}, "/x/__vm_config.yaml", "hello\n", "/dev/sda8 / fstab: /dev/hda8")
	cmd := exec.Command("bash", "-c", s)
	cmd.Dir = d
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "HOME="+d)
	os.MkdirAll(filepath.Join(d, "x", "lab-a"), 0o755)
	out, _ := cmd.CombinedOutput()
	calls, _ := os.ReadFile(filepath.Join(d, "calls"))
	if !strings.Contains(string(out), "ok /dev/sda8 /__vm_config.yaml (fstab: /dev/hda8, as for") || strings.Count(string(calls), "\n") != 1 {
		t.Fatalf("%s\ncalls:\n%s", out, calls)
	}
}
