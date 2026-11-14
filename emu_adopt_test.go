package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/termwright/termwright/internal/emu"
)

// A switch found running (Termwright restarted) still needs its console typed when its day 0 or its fixed address never was,
// or its DHCP address is not known.
func TestEmuNeedsConsole(t *testing.T) {
	fixed := &emu.Lab{Mgmt: "lan", LanFixed: true}
	dhcp := &emu.Lab{Mgmt: "lan"}
	voss := &emu.Image{Day0: []string{"enable"}, AddrSet: []string{"enable", "configure terminal"}}
	plain := &emu.Image{}
	for _, c := range []struct {
		name string
		l    *emu.Lab
		v    emu.VM
		im   *emu.Image
		want bool
	}{
		{"nothing typed yet", fixed, emu.VM{MgmtIP: "10.0.0.5/24"}, voss, true},
		{"day 0 done, address never typed", fixed, emu.VM{Day0Done: true, MgmtIP: "10.0.0.5/24"}, voss, true},
		{"address typed", fixed, emu.VM{Day0Done: true, MgmtIP: "10.0.0.5/24", AddrOn: "10.0.0.5/24"}, voss, false},
		{"address changed since", fixed, emu.VM{Day0Done: true, MgmtIP: "10.0.0.6/24", AddrOn: "10.0.0.5/24"}, voss, true},
		{"DHCP, day 0 done, address known", dhcp, emu.VM{Day0Done: true, MgmtIP: "10.127.25.40/24"}, voss, false},
		{"DHCP, day 0 done, no address yet", dhcp, emu.VM{Day0Done: true}, &emu.Image{Day0: []string{"x"}, AddrCmd: "show ip"}, true},
		{"DHCP, no address command", dhcp, emu.VM{Day0Done: true}, voss, false},
		{"image without commands", fixed, emu.VM{MgmtIP: "10.0.0.5/24"}, plain, false},
		{"no lab", nil, emu.VM{}, voss, false},
	} {
		if got := emuNeedsConsole(c.l, c.v, c.im); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEmuNameList(t *testing.T) {
	for in, want := range map[string]int{"tc-1,tc-2": 2, " a , b ,": 2, "pc-{n}": 1, "": 0} {
		if got := emuNameList(in); len(got) != want {
			t.Errorf("%q: %v", in, got)
		}
	}
}

// Start All powers on what is off: running and booting switches (their QEMU up) are left alone.
func TestEmuNotRunning(t *testing.T) {
	emuLab.mu.Lock()
	old := emuLab.status
	emuLab.status.Up = map[string]float64{"v1": 12, "v3": 1}
	got := emuNotRunning([]string{"v1", "v2", "v3", "v4"})
	emuLab.status = old
	emuLab.mu.Unlock()
	if len(got) != 2 || got[0] != "v2" || got[1] != "v4" {
		t.Fatal(got)
	}
}

// Rack front is the default drawing style; the old Classic 3-D ("blue") draws as Rack front.
func TestEmuLook(t *testing.T) {
	for in, want := range map[string]string{"": "rack", "blue": "rack", "box": "box", "slate": "slate", "rack": "rack"} {
		if got := emuLook(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// Stopping a lab in the background switches off its QEMU processes only - not another lab's, not other programs.
func TestEmuStopLabScript(t *testing.T) {
	if _, err := os.Stat("/proc/self/comm"); err != nil {
		t.Skip("needs /proc")
	}
	home := t.TempDir()
	fake := filepath.Join(home, "qemu-system-x86_64")
	sl, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep")
	}
	b, _ := os.ReadFile(sl)
	if err := os.WriteFile(fake, b, 0o755); err != nil {
		t.Fatal(err)
	}
	start := func(lab, id string, prog string) *exec.Cmd {
		c := exec.Command(prog, "60")
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		d := filepath.Join(home, "termwright-lab", "lab-"+lab)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, id+".pid"), []byte(strconv.Itoa(c.Process.Pid)), 0o644)
		return c
	}
	a1, a2 := start("ab12", "v1", fake), start("ab12", "v2", fake)
	other, notQemu := start("cd34", "v1", fake), start("ab12", "v3", sl)
	defer func() { _ = other.Process.Kill(); _ = notQemu.Process.Kill() }()
	cmd := exec.Command("bash", "-c", emuStopLabScript("ab12"))
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "@@stopped 2") {
		t.Fatalf("%v %s", err, out)
	}
	for _, c := range []*exec.Cmd{a1, a2} {
		if err := c.Wait(); err == nil {
			t.Error("a switch of the lab still ran")
		}
	}
	if other.Process.Signal(syscall.Signal(0)) != nil || notQemu.Process.Signal(syscall.Signal(0)) != nil {
		t.Error("another lab's switch, or a program that is not QEMU, was stopped")
	}
	if emuLabIDRe.MatchString("ab12; rm -rf ~") || !emuLabIDRe.MatchString("c8") {
		t.Error("lab ID check")
	}
}
