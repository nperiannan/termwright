package emu

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCPUScript(t *testing.T) {
	home := t.TempDir()
	l := Lab{ID: "ab", Dir: "tw"}
	_ = os.MkdirAll(filepath.Join(home, l.VMDir()), 0o755)
	sl := exec.Command("sleep", "30")
	if err := sl.Start(); err != nil {
		t.Skip(err)
	}
	defer sl.Process.Kill()
	_ = os.WriteFile(filepath.Join(home, l.VMDir(), "v1.pid"), []byte(strconv.Itoa(sl.Process.Pid)), 0o644)
	run := func(v VM) string {
		c := exec.Command("bash", "-c", l.CPUScript(v))
		c.Dir, c.Env = home, append(os.Environ(), "PATH=/usr/bin:/bin") // no sudo in a test's PATH is not certain: the cgroup line needs sudo -n to work
		out, _ := c.CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	if out := run(VM{ID: "v9"}); out != "cpu v9 none not running" {
		t.Fatal(out)
	}
	out := run(VM{ID: "v1", CPUWeight: 50})
	if out != "cpu v1 nice" && out != "cpu v1 cgroup" {
		t.Fatal(out)
	}
	if out == "cpu v1 nice" {
		b, _ := os.ReadFile("/proc/" + strconv.Itoa(sl.Process.Pid) + "/stat")
		if f := strings.Fields(string(b)); len(f) > 18 && f[18] != "10" {
			t.Fatal("nice", f[18])
		}
		if out := run(VM{ID: "v1", CPUMax: 0.5}); !strings.HasPrefix(out, "cpu v1 fail") {
			t.Fatal(out)
		}
	}
}
