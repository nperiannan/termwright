package emu

import (
	"bufio"
	"context"
	"encoding/base64"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallScripts(t *testing.T) {
	s := InstallSpec{ID: "ab12", Dir: "termwright-lab", ISO: "alpine virt.iso", GB: 8, RAM: 1024, CPUs: 2}
	st := s.StartScript()
	for _, want := range []string{"qemu-img create -q -f qcow2 termwright-lab/install/ab12.qcow2 8G", "-m 1024 -smp 2", "media=cdrom,readonly=on", "'alpine virt.iso'", "-boot order=cd", "telnet=on", "-daemonize", "-enable-kvm -cpu host", "-accel tcg"} {
		if !strings.Contains(st, want) {
			t.Errorf("start script lacks %q:\n%s", want, st)
		}
	}
	if sv := s.SaveScript("alpine-3.22.qcow2"); !strings.Contains(sv, "qemu-img convert -p -c -O qcow2 termwright-lab/install/ab12.qcow2 termwright-lab/images/alpine-3.22.qcow2.part > termwright-lab/install/ab12.prog 2>&1") || !strings.Contains(sv, ".sha256") {
		t.Errorf("save script:\n%s", sv)
	}
	if got := (InstallSpec{ID: "m1", Dir: "d", ISO: "a.iso", MB: 768, GB: 1}).StartScript(); !strings.Contains(got, "d/install/m1.qcow2 768M") {
		t.Errorf("a disk in MB:\n%s", got)
	}
	up, port, fail := ParseInstall("port 24902\nstarted\n")
	if !up || port != 24902 || fail != "" {
		t.Fatalf("parsed %v %d %q", up, port, fail)
	}
	if _, _, fail := ParseInstall("fail the ISO is not on the host\n"); fail != "the ISO is not on the host" {
		t.Fatalf("fail %q", fail)
	}
	if up, _, _ := ParseInstall("down\nport 24900\n"); up {
		t.Fatal("down read as up")
	}
}

func TestAlpineAnswers(t *testing.T) {
	cmd := AlpineAnswers(AlpineOpts{Hostname: "lab1"})
	for _, want := range []string{`HOSTNAMEOPTS="-n lab1"`, `DISKOPTS="-m sys /dev/vda"`, `USEROPTS="none"`, `INTERFACESOPTS="auto lo\niface lo inet loopback`, "> /tmp/ans"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("answers lack %q: %s", want, cmd)
		}
	}
	if strings.Contains(cmd, "\n") {
		t.Fatal("the answer file must be one line to type")
	}
}

// fakeAlpine plays the live ISO on a serial console: login, a shell, setup-alpine's questions.
func fakeAlpine(t *testing.T, conn net.Conn, setupExit int, got chan<- string) {
	r := bufio.NewReader(conn)
	w := func(s string) { conn.Write([]byte(s)) }
	w("Welcome to Alpine Linux 3.22\r\nKernel 6.12 on x86_64 (/dev/ttyS0)\r\n\r\nlocalhost login: ")
	read := func() string {
		l, err := r.ReadString('\r')
		if err != nil {
			return ""
		}
		return strings.TrimSpace(l)
	}
	for {
		l := read()
		if l == "" {
			if _, err := r.Peek(1); err != nil {
				return
			}
			continue
		}
		switch {
		case l == "root":
			w("\r\nWelcome to Alpine!\r\nlocalhost:~# ")
		case strings.HasPrefix(l, "printf "):
			got <- l
			w("\r\nlocalhost:~# ")
		case strings.HasPrefix(l, "setup-alpine"):
			w("\r\nWARNING: Erase the above disk(s) and continue? [y/N]: ")
			if read() != "y" {
				w("Aborted\r\n")
				continue
			}
			w("Installing system on /dev/vda:\r\nNew password: ")
			a := read()
			w("\r\nRetype password: ")
			b := read()
			if a != "pw" || b != "pw" {
				w("\r\nPasswords do not match\r\n")
			}
			w("\r\nSetup a user? (enter a lower-case loginname, or 'no') [no] ")
			if u := read(); u != "no" {
				w("\r\nuser " + u + " made\r\n")
				setupExit = 9
			}
			w("\r\nInstallation is complete. Please reboot.\r\n")
			w("TWEND-" + string(rune('0'+setupExit)) + "\r\nlocalhost:~# ")
		case l == "poweroff":
			got <- "poweroff"
			return
		}
	}
}

func TestInstallAlpineFlow(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewConsole(a)
	defer c.Close()
	got := make(chan string, 4)
	go fakeAlpine(t, b, 0, got)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var notes []string
	if err := InstallAlpine(ctx, c, AlpineOpts{Hostname: "lab1", Password: "pw"}, func(step, msg string) { notes = append(notes, step+": "+msg) }); err != nil {
		t.Fatalf("%v (console: %s)", err, c.Tail(300))
	}
	if l := <-got; !strings.Contains(l, `HOSTNAMEOPTS="-n lab1"`) {
		t.Fatalf("answer file line: %s", l)
	}
	if l := <-got; l != "poweroff" {
		t.Fatalf("last command %q", l)
	}
	if len(notes) != 4 || !strings.HasPrefix(notes[0], "boot: ") || !strings.HasPrefix(notes[1], "login: ") || !strings.HasPrefix(notes[2], "install: ") || !strings.HasPrefix(notes[3], "poweroff: ") {
		t.Fatalf("notes %v", notes)
	}
}

func TestInstallAlpineReportsFailure(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewConsole(a)
	defer c.Close()
	got := make(chan string, 4)
	go fakeAlpine(t, b, 3, got)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := InstallAlpine(ctx, c, AlpineOpts{Password: "pw"}, func(string, string) {})
	if err == nil || !strings.Contains(err.Error(), "setup-alpine failed") {
		t.Fatalf("err %v", err)
	}
}

func TestInstallAlpineNeedsPassword(t *testing.T) {
	if err := InstallAlpine(context.Background(), nil, AlpineOpts{}, func(string, string) {}); err == nil {
		t.Fatal("empty password accepted")
	}
}

func TestProgressScriptAndParse(t *testing.T) {
	s := InstallSpec{ID: "ab12", Dir: "termwright-lab", ISO: "a.iso", GB: 8, RAM: 1024, CPUs: 2}
	sc := s.ProgressScript("alpine.qcow2")
	for _, want := range []string{"termwright-lab/install/ab12.qcow2", "termwright-lab/images/alpine.qcow2.part", "termwright-lab/install/ab12.prog", "ab12.log", "base64"} {
		if !strings.Contains(sc, want) {
			t.Errorf("progress script lacks %q:\n%s", want, sc)
		}
	}
	if strings.Contains(s.ProgressScript(""), ".part") {
		t.Error("no image file yet: no part line")
	}
	log := "Welcome\r\n\x1b[97mRoot Password\x1b[0m\r\n---\r\nNew password: "
	out := "disk 142606336\npart 0\npct 45.50\nidle 7\nlog " + base64.StdEncoding.EncodeToString([]byte(log)) + "\n"
	p := ParseProgress(out)
	if p.Disk != 142606336 || p.Pct != 45.5 || p.Idle != 7 || p.Log != "Welcome\nRoot Password\n---\nNew password: " {
		t.Fatalf("parsed %+v", p)
	}
	if p := ParseProgress("disk 0\npct \nidle x\nlog\n"); p.Disk != 0 || p.Pct != 0 || p.Log != "" {
		t.Fatalf("empty %+v", p)
	}
	if got := LastLines("a\n\n  \nb\nc  \nddddddddddd\n", 3, 5); got != "b\nc\nddddd…" {
		t.Fatalf("lines %q", got)
	}
}

// The console's reads can cut an escape sequence in two: no "[97m" is left in the text.
func TestConsoleKeepsSplitEscapes(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewConsole(a)
	defer c.Close()
	for _, part := range []string{"lease obtained\r\n\x1b", "[97m Root Password \x1b[", "0m\r\n----\r\nChanging password"} {
		if _, err := b.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := c.Lines(5, 80); got != "lease obtained\n Root Password\n----\nChanging password" {
		t.Fatalf("console shows %q", got)
	}
	if c.Idle() > 5*time.Second || c.Idle() == 0 {
		t.Fatalf("idle %v", c.Idle())
	}
}

// The save and progress scripts run in a real shell: the conversion's percent and the sizes are read back.
func TestSaveAndProgressScriptsRun(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("no qemu-img")
	}
	home := t.TempDir()
	s := InstallSpec{ID: "t1", Dir: "lab", ISO: "a.iso", GB: 1, RAM: 256, CPUs: 1}
	run := func(script string) string {
		cmd := exec.Command("sh", "-c", script)
		cmd.Dir = home
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return string(out)
	}
	if err := os.MkdirAll(filepath.Join(home, "lab/install"), 0o755); err != nil {
		t.Fatal(err)
	}
	run("qemu-img create -q -f qcow2 lab/install/t1.qcow2 1G && qemu-io -c 'write -P 7 0 20M' lab/install/t1.qcow2 >/dev/null")
	p := ParseProgress(run(s.ProgressScript("my image.qcow2")))
	if p.Disk < 100000 || p.Part != 0 || p.Pct != 0 {
		t.Fatalf("before: %+v", p)
	}
	if out := run(s.SaveScript("my image.qcow2")); !strings.HasPrefix(out, "saved ") {
		t.Fatalf("save: %s", out)
	}
	p = ParseProgress(run(s.ProgressScript("my image.qcow2")))
	if p.Pct != 100 {
		t.Fatalf("after: %+v", p)
	}
	bad := InstallSpec{ID: "t2", Dir: "lab"}
	if out := run(bad.SaveScript("y.qcow2")); !strings.HasPrefix(out, "fail ") || !strings.Contains(out, "nosuch") && !strings.Contains(out, "t2.qcow2") {
		t.Fatalf("a failing conversion: %s", out)
	}
	if _, err := os.Stat(filepath.Join(home, "lab/images/y.qcow2.part")); err == nil {
		t.Error("the part file was left")
	}
}

// An installed Alpine gives root a shell on the serial console without a login prompt: that is booted too.
func TestReadyInSeesAnAutologinShell(t *testing.T) {
	im := Image{Ready: "login:", DeviceType: "Linux", Prompt: `[#$]\s*$`}
	boot := " * Starting sshd ... [ ok ]\r\n\r\nWelcome to Alpine!\r\n\r\nalpine:~# "
	if !im.ReadyIn(boot) {
		t.Error("a shell prompt at the end is not seen")
	}
	if im.ReadyIn(" * Starting networking ... [ ok ]\r\n * Starting sshd ...") {
		t.Error("a boot in progress is seen as booted")
	}
	if !im.ReadyIn("alpine login: ") {
		t.Error("the login prompt is not seen")
	}
	if up := (Image{Ready: "login:", DeviceType: "LINUX", Prompt: `[#$]\s*$`}); !up.ReadyIn(boot) {
		t.Error("type LINUX (from a device pack) is a Linux computer too")
	}
	sw := Image{Ready: "login:", DeviceType: "EXOS", Prompt: `[#>]\s*$`}
	if sw.ReadyIn("boot # ") {
		t.Error("a switch is ready only by its ready text")
	}
	if emuReadyText := "login:|Welcome to Alpine"; !(&Image{Ready: emuReadyText}).ReadyIn("Welcome to Alpine!\r\n") {
		t.Error("the Alpine ready text does not match its welcome")
	}
}

// A Linux computer gets name servers in its day 0 (before iperf3 is installed), and older images get them too.
func TestLinuxDNS(t *testing.T) {
	im := Image{}
	LinuxHost(&im)
	if len(im.Day0) != 3 || im.Day0[1] != LinuxDNS || !strings.Contains(im.Day0[2], "iperf3") {
		t.Fatalf("day 0: %q", im.Day0)
	}
	old := Image{Day0: []string{"hostname {hostname}", "(command -v iperf3 || apk add -q iperf3) &"}}
	if !AddLinuxDNS(&old) || old.Day0[1] != LinuxDNS || AddLinuxDNS(&old) || len(old.Day0) != 3 {
		t.Fatalf("older image: %q", old.Day0)
	}
	if !strings.Contains(AlpineAnswers(AlpineOpts{}), `DNSOPTS="-d lab -n 8.8.8.8"`) {
		t.Fatal("setup-dns needs -n before the server")
	}
	home := t.TempDir()
	run := func(script string) string {
		cmd := exec.Command("sh", "-c", script)
		cmd.Env = append(os.Environ(), "HOME="+home)
		out, _ := cmd.CombinedOutput()
		return string(out)
	}
	f := filepath.Join(home, "resolv.conf")
	_ = os.WriteFile(f, nil, 0o644)
	run(strings.ReplaceAll(Fill(LinuxDNS, VM{}, 1), "/etc/resolv.conf", f))
	if b, _ := os.ReadFile(f); string(b) != "nameserver 8.8.8.8\nnameserver 1.1.1.1\n" {
		t.Fatalf("an empty resolv.conf became %q", b)
	}
	_ = os.WriteFile(f, []byte("nameserver 10.1.1.1\n"), 0o644)
	run(strings.ReplaceAll(Fill(LinuxDNS, VM{}, 1), "/etc/resolv.conf", f))
	if b, _ := os.ReadFile(f); string(b) != "nameserver 10.1.1.1\n" {
		t.Fatalf("a resolv.conf with a server was changed: %q", b)
	}
}
