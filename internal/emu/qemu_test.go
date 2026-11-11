package emu

import (
	"context"
	"encoding/base64"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestQEMU boots a tiny Linux on this machine with the real qemu (software
// emulation), the way a KVM host would: TW_EMU_KERNEL and TW_EMU_INITRD name a
// kernel and an initramfs whose /init shows "fake login:" on the serial console.
func TestQEMU(t *testing.T) {
	kernel, initrd := os.Getenv("TW_EMU_KERNEL"), os.Getenv("TW_EMU_INITRD")
	if kernel == "" || initrd == "" {
		t.Skip("TW_EMU_KERNEL / TW_EMU_INITRD not set")
	}
	home := t.TempDir()
	run := func(script string) string {
		c := exec.Command("bash", "-c", script)
		c.Dir, c.Env = home, append(os.Environ(), "HOME="+home)
		out, _ := c.CombinedOutput()
		return string(out)
	}
	_ = os.MkdirAll(filepath.Join(home, "termwright-lab/images"), 0o755)
	if out, err := exec.Command("qemu-img", "create", "-q", "-f", "qcow2", filepath.Join(home, "termwright-lab/images/fake.qcow2"), "16M").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	im := &Image{ID: "f", Name: "Fake NOS", FileName: "fake.qcow2", RAM: 256, CPUs: 1, NICs: 3, NIC: "e1000", Ready: "fake login:", Login: "admin", Password: "x",
		Answers: []Answer{{Match: "[y/N]", Send: "n"}}, Day0: []string{"hostname {hostname}", "ip {mgmtIp} gw {gateway}"},
		Extra: "-kernel " + kernel + " -initrd " + initrd + " -append 'console=ttyS0 quiet panic=-1'"}
	base := 31000 + os.Getpid()%2000*8
	l := &Lab{ID: "e1", Dir: "termwright-lab", Mgmt: "forward", Base: base, VMs: []VM{{ID: "v1", Name: "sw-1", Image: "f", Index: 0}, {ID: "v2", Name: "sw-2", Image: "f", Index: 1}}}
	imgs := map[string]*Image{"f": im}
	l.Wire(l.VMs, "chain", imgs)
	for i := range l.VMs {
		l.VMs[i].MgmtIP, l.VMs[i].Gateway = l.Address(i)
	}
	out := run(l.StartScript(l.VMs, imgs))
	defer run(l.StopScript([]string{"v1", "v2"}))
	if strings.Count(out, "started") != 2 {
		t.Fatalf("start: %s", out)
	}
	// boot: the console log shows the login prompt
	deadline := time.Now().Add(90 * time.Second)
	var st Status
	for time.Now().Before(deadline) {
		st = ParseStatus(run(l.StatusScript([]string{"v1"})), base64.StdEncoding.DecodeString)
		if im.ReadyIn(st.Log["v1"]) {
			break
		}
		time.Sleep(time.Second)
	}
	if len(st.Up) != 2 || !im.ReadyIn(st.Log["v1"]) {
		t.Fatalf("status %+v", st)
	}
	conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(l.ConsolePort(l.VMs[0])))
	if err != nil {
		t.Fatal(err)
	}
	c := NewConsole(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := Day0(ctx, c, im, l.VMs[0], 1, nil); err != nil {
		t.Fatalf("day0: %v\n%s", err, c.Text())
	}
	im.AddrCmd = "show ip"
	if a, err := AskAddress(ctx, c, im); err != nil || a != "10.20.30.40" {
		t.Fatalf("address %q %v\n%s", a, err, c.Text())
	}
	c.Close()
	txt := c.Text()
	if !strings.Contains(txt, "answer=n") || !strings.Contains(txt, "ran: hostname sw-1") || !strings.Contains(txt, "ran: ip 10.0.2.15/24 gw 10.0.2.2") {
		t.Fatalf("console:\n%s", txt)
	}
	// stop
	run(l.StopScript([]string{"v1", "v2"}))
	time.Sleep(500 * time.Millisecond)
	if st := ParseStatus(run(l.StatusScript(nil)), base64.StdEncoding.DecodeString); len(st.Up) != 0 {
		t.Fatalf("still up %+v", st)
	}
}

// TestPersonaWrite writes a persona file into a VM disk with libguestfs
// (TW_EMU_GUESTFS = a qcow2 image with a filesystem that has /scratch).
func TestPersonaWrite(t *testing.T) {
	img := os.Getenv("TW_EMU_GUESTFS")
	if img == "" {
		t.Skip("TW_EMU_GUESTFS not set")
	}
	home := t.TempDir()
	run := func(script string) string {
		c := exec.Command("bash", "-c", script)
		c.Dir, c.Env = home, append(os.Environ(), "HOME="+home, "LIBGUESTFS_BACKEND=direct")
		out, _ := c.CombinedOutput()
		return string(out)
	}
	_ = os.MkdirAll(filepath.Join(home, "termwright-lab/images"), 0o755)
	if out, err := exec.Command("cp", img, filepath.Join(home, "termwright-lab/images/gf.qcow2")).CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	im := &Image{ID: "g", FileName: "gf.qcow2"}
	l := &Lab{ID: "e2", Dir: "termwright-lab", VMs: []VM{{ID: "v1", Name: "sw-1", Image: "g", DiskGB: 1}}}
	run(l.PrepareScript(l.VMs, map[string]*Image{"g": im}))
	out := run(l.WriteFileScript(l.VMs[0], "/scratch/persona.yaml", "system:\n  slots: []\n"))
	if !strings.Contains(out, "ok /dev/sda1 /scratch/persona.yaml (folder)") {
		t.Fatalf("write: %s", out)
	}
	got := run("guestfish --ro -a termwright-lab/lab-e2/v1.qcow2 -m /dev/sda1 cat /scratch/persona.yaml")
	if strings.TrimRight(got, "\n") != "system:\n  slots: []" {
		t.Fatalf("read back %q", got)
	}
	if size := run("qemu-img info termwright-lab/lab-e2/v1.qcow2 | grep 'virtual size'"); !strings.Contains(size, "1 GiB") {
		t.Fatalf("not grown: %s", size)
	}
	// a /scratch partition that the image's fstab mounts (by UUID) over an empty /scratch folder
	if two := os.Getenv("TW_EMU_GUESTFS2"); two != "" {
		if o, err := exec.Command("cp", two, filepath.Join(home, "termwright-lab/lab-e2/v1.qcow2")).CombinedOutput(); err != nil {
			t.Fatal(string(o))
		}
		if out := run(l.WriteFileScript(l.VMs[0], "/scratch/persona.yaml", "x: 1\n")); !strings.Contains(out, "ok /dev/sda2 /persona.yaml (fstab: UUID=") {
			t.Fatalf("fstab write: %s", out)
		}
	}
}

// TestQEMUFast saves a running VM's state into its disk and starts it from
// there: no boot (the console stays quiet), the same machine as it was.
func TestQEMUFast(t *testing.T) {
	kernel, initrd := os.Getenv("TW_EMU_KERNEL"), os.Getenv("TW_EMU_INITRD")
	if kernel == "" || initrd == "" {
		t.Skip("TW_EMU_KERNEL / TW_EMU_INITRD not set")
	}
	home := t.TempDir()
	run := func(script string) string {
		c := exec.Command("bash", "-c", script)
		c.Dir, c.Env = home, append(os.Environ(), "HOME="+home)
		out, _ := c.CombinedOutput()
		return string(out)
	}
	_ = os.MkdirAll(filepath.Join(home, "termwright-lab/images"), 0o755)
	if out, err := exec.Command("qemu-img", "create", "-q", "-f", "qcow2", filepath.Join(home, "termwright-lab/images/fake.qcow2"), "16M").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	im := &Image{ID: "f", Name: "Fake NOS", FileName: "fake.qcow2", RAM: 256, CPUs: 1, NICs: 2, NIC: "e1000", Ready: "fake login:",
		Extra: "-kernel " + kernel + " -initrd " + initrd + " -append 'console=ttyS0 quiet panic=-1'"}
	base := 33000 + os.Getpid()%2000*8
	l := &Lab{ID: "e2", Dir: "termwright-lab", Mgmt: "forward", Base: base, VMs: []VM{{ID: "v1", Name: "sw-1", Image: "f"}}}
	imgs := map[string]*Image{"f": im}
	defer run(l.StopScript([]string{"v1"}))
	wait := func(ok func(Status) bool) Status {
		var st Status
		for end := time.Now().Add(90 * time.Second); time.Now().Before(end); time.Sleep(time.Second) {
			if st = ParseStatus(run(l.StatusScript([]string{"v1"}, map[string]string{"v1": im.Ready})), base64.StdEncoding.DecodeString); ok(st) {
				break
			}
		}
		return st
	}
	if out := run(l.StartScript(l.VMs, imgs)); !strings.Contains(out, "started v1") {
		t.Fatal(out)
	}
	if st := wait(func(s Status) bool { return s.Ready["v1"] }); !st.Ready["v1"] {
		t.Fatalf("no boot: %q", st.Log["v1"])
	}
	out := run(l.SaveScript([]string{"v1"}))
	if !strings.Contains(out, "saved v1") {
		t.Fatalf("save: %s", out)
	}
	l.VMs[0].FastKey = l.FastKey(l.VMs[0], im)
	t0 := time.Now()
	if out := run(l.StartScript(l.VMs, imgs)); !strings.Contains(out, "resumed v1") {
		t.Fatalf("resume: %s", out)
	}
	if st := wait(func(s Status) bool { _, up := s.Up["v1"]; return up }); len(st.Up) != 1 || st.Ready["v1"] {
		t.Fatalf("after resume: up %v, it booted again: %v", st.Up, st.Ready["v1"])
	}
	t.Logf("resumed in %v", time.Since(t0))
	// another setup (more memory): it boots, and the old state goes
	run(l.StopScript([]string{"v1"}))
	time.Sleep(time.Second)
	im.RAM = 320
	if out := run(l.StartScript(l.VMs, imgs)); !strings.Contains(out, "started v1") {
		t.Fatalf("changed: %s", out)
	}
	if out := run("qemu-img snapshot -l termwright-lab/lab-e2/v1.qcow2 2>&1; ls termwright-lab"); strings.Contains(out, FastSnap) {
		t.Fatalf("the old state stayed: %s", out)
	}
}
