package emu

import (
	"bufio"
	"context"
	"net"
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
	if sv := s.SaveScript("alpine-3.22.qcow2"); !strings.Contains(sv, "qemu-img convert -c -O qcow2 termwright-lab/install/ab12.qcow2 termwright-lab/images/alpine-3.22.qcow2.part") || !strings.Contains(sv, ".sha256") {
		t.Errorf("save script:\n%s", sv)
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
	for _, want := range []string{`HOSTNAMEOPTS="-n lab1"`, `DISKOPTS="-m sys /dev/vda"`, `INTERFACESOPTS="auto lo\niface lo inet loopback`, "> /tmp/ans"} {
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
	if err := InstallAlpine(ctx, c, AlpineOpts{Hostname: "lab1", Password: "pw"}, func(s string) { notes = append(notes, s) }); err != nil {
		t.Fatalf("%v (console: %s)", err, c.Tail(300))
	}
	if l := <-got; !strings.Contains(l, `HOSTNAMEOPTS="-n lab1"`) {
		t.Fatalf("answer file line: %s", l)
	}
	if l := <-got; l != "poweroff" {
		t.Fatalf("last command %q", l)
	}
	if len(notes) < 3 {
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
	err := InstallAlpine(ctx, c, AlpineOpts{Password: "pw"}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "setup-alpine failed") {
		t.Fatalf("err %v", err)
	}
}

func TestInstallAlpineNeedsPassword(t *testing.T) {
	if err := InstallAlpine(context.Background(), nil, AlpineOpts{}, func(string) {}); err == nil {
		t.Fatal("empty password accepted")
	}
}
