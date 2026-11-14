package emu

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Checkpoints on a real QEMU (no OS in it: a VM sitting in its firmware is enough): taken while it runs,
// listed, restored, and the oldest dropped, all through its monitor.
func TestCheckpointScripts(t *testing.T) {
	if _, err := exec.LookPath("qemu-system-x86_64"); err != nil {
		t.Skip("no qemu")
	}
	dir := t.TempDir()
	l := &Lab{ID: "ck", Dir: dir, Base: 23000 + os.Getpid()%2000*4}
	v := VM{ID: "v1", Name: "sw", Index: 1}
	vd := l.VMDir()
	_ = os.MkdirAll(vd, 0o755)
	disk := filepath.Join(vd, "v1.qcow2")
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", disk, "64M").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	q := exec.Command("qemu-system-x86_64", "-display", "none", "-m", "64", "-drive", "file="+disk+",format=qcow2,if=virtio",
		"-monitor", "tcp:127.0.0.1:"+strconv.Itoa(l.MonitorPort(v))+",server,nowait", "-pidfile", filepath.Join(vd, "v1.pid"), "-daemonize")
	if out, err := q.CombinedOutput(); err != nil {
		t.Skipf("qemu did not start: %v %s", err, out)
	}
	defer func() {
		if b, err := os.ReadFile(filepath.Join(vd, "v1.pid")); err == nil {
			_ = exec.Command("kill", strings.TrimSpace(string(b))).Run()
		}
	}()
	time.Sleep(500 * time.Millisecond)
	run := func(script string) string {
		out, _ := exec.Command("bash", "-c", script).CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	if r := run(l.CheckpointScript(v, "tw-ck-a", nil)); r != "ok" {
		t.Fatalf("take: %q", r)
	}
	if r := run(l.CheckpointScript(v, "tw-ck-b", []string{"tw-ck-a"})); r != "ok" {
		t.Fatalf("take and drop: %q", r)
	}
	if r := run(l.RestoreScript(v, "tw-ck-b")); r != "ok" {
		t.Fatalf("restore: %q", r)
	}
	if r := run(l.RestoreScript(v, "tw-ck-a")); !strings.HasPrefix(r, "err") {
		t.Fatalf("restoring a dropped one: %q", r)
	}
	if r := run(l.DropCheckpointScript(v, []string{"tw-ck-b"}, true)); r != "ok" {
		t.Fatalf("drop: %q", r)
	}
	if r := run(l.CheckpointScript(VM{ID: "v9", Index: 9}, "x", nil)); r != "err it is not running" {
		t.Fatalf("not running: %q", r)
	}
}

// The patch panel joins two unconnected switch ports on the host: what one sends reaches the other, from
// the port the other expects; an empty list ends it.
func TestPatchPy(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "tw-patch.py"), []byte(PatchPy), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "patch.json"), []byte("[[42101,42111,42102,42112]]"), 0o644)
	cmd := exec.Command("python3", "-I", filepath.Join(dir, "tw-patch.py"), dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	ep := func(se int) *net.UDPConn {
		c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: se})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	a, b := ep(42111), ep(42112) // the QEMUs: they listen on "send" and send to "listen"
	defer a.Close()
	defer b.Close()
	time.Sleep(400 * time.Millisecond)
	recv := func(c *net.UDPConn) (string, int) {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 2000)
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return "", 0
		}
		return string(buf[:n]), from.Port
	}
	_, _ = a.WriteToUDP([]byte("from A"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 42101})
	if got, from := recv(b); got != "from A" || from != 42102 {
		t.Fatalf("A to B: %q from %d", got, from)
	}
	_, _ = b.WriteToUDP([]byte("from B"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 42102})
	if got, from := recv(a); got != "from B" || from != 42101 {
		t.Fatalf("B to A: %q from %d", got, from)
	}
	time.Sleep(50 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(dir, "patch.json"), []byte("[]"), 0o644)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("it did not end without cables")
	}
	if s := (&Lab{ID: "x", Dir: "d"}).PatchScript(nil); !strings.Contains(s, "echo '[]' > patch.json.new") || strings.Contains(s, "nohup") {
		t.Fatalf("no pairs: %s", s)
	}
}

// A hub repeats a frame to every other leg; an L2 switch floods an unknown destination, then sends to where
// it learned the MAC.
func TestHubPy(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "tw-hub.py"), []byte(HubPy), 0o644)
	write := func(kind string) {
		_ = os.WriteFile(filepath.Join(dir, "hubs.json"), []byte(`{"hubs":[{"id":"h1","kind":"`+kind+`","legs":[[43101,43111],[43102,43112],[43103,43113]]}]}`), 0o644)
	}
	write("hub")
	cmd := exec.Command("python3", "-I", filepath.Join(dir, "tw-hub.py"), dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	var eps []*net.UDPConn
	for _, p := range []int{43111, 43112, 43113} {
		c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		eps = append(eps, c)
	}
	time.Sleep(400 * time.Millisecond)
	mac := func(n byte) []byte { return []byte{2, 0, 0, 0, 0, n} }
	frame := func(dst, src []byte, tag string) []byte {
		return append(append(append(append([]byte{}, dst...), src...), 0x88, 0xb5), []byte(tag+"................................................")...)
	}
	send := func(from int, f []byte) {
		_, _ = eps[from].WriteToUDP(f, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 43101 + from})
	}
	got := func(i int) string {
		_ = eps[i].SetReadDeadline(time.Now().Add(700 * time.Millisecond))
		buf := make([]byte, 2000)
		n, _, err := eps[i].ReadFromUDP(buf)
		if err != nil || n < 17 {
			return ""
		}
		return string(buf[14:17])
	}
	send(0, frame(mac(2), mac(1), "aaa"))
	if g1, g2 := got(1), got(2); g1 != "aaa" || g2 != "aaa" {
		t.Fatalf("hub: %q %q", g1, g2)
	}
	time.Sleep(50 * time.Millisecond)
	write("switch")
	time.Sleep(700 * time.Millisecond)
	send(0, frame(mac(2), mac(1), "bbb")) // unknown: flooded; it learns mac 1 on leg 0
	if g1, g2 := got(1), got(2); g1 != "bbb" || g2 != "bbb" {
		t.Fatalf("flood: %q %q", g1, g2)
	}
	send(1, frame(mac(1), mac(2), "ccc")) // known: only leg 0
	if g0, g2 := got(0), got(2); g0 != "ccc" || g2 != "" {
		t.Fatalf("learned: %q %q", g0, g2)
	}
}
