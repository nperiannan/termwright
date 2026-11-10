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
