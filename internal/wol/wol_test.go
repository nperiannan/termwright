package wol

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPacket(t *testing.T) {
	for _, m := range []string{"00:11:22:33:44:55", "00-11-22-33-44-55", "0011.2233.4455", "001122334455"} {
		p, err := Packet(m)
		if err != nil || len(p) != 102 || p[5] != 0xff || p[6] != 0 || p[11] != 0x55 || p[101] != 0x55 {
			t.Fatalf("%s: %x %v", m, p, err)
		}
	}
	if _, err := Packet("00:11:22"); err == nil {
		t.Fatal("short MAC accepted")
	}
}

func TestSendToAddress(t *testing.T) {
	ln, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	p, _ := Packet("aa:bb:cc:dd:ee:ff")
	sent, err := Send(p, "127.0.0.1", ln.LocalAddr().(*net.UDPAddr).Port)
	if err != nil || len(sent) == 0 || sent[0] != "127.0.0.1" {
		t.Fatalf("%v %v", sent, err)
	}
	_ = ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	b := make([]byte, 200)
	n, _, err := ln.ReadFromUDP(b)
	if err != nil || n != 102 || b[6] != 0xaa {
		t.Fatalf("got %d bytes %v", n, err)
	}
}

func TestRemoteCommandIsValidated(t *testing.T) {
	p, _ := Packet("aa:bb:cc:dd:ee:ff")
	if _, err := RemoteCommand(p, "aa:bb:cc:dd:ee:ff", "1.2.3.4'; rm -rf /", 9); err == nil {
		t.Fatal("bad broadcast accepted")
	}
	c, err := RemoteCommand(p, "aa:bb:cc:dd:ee:ff", "", 9)
	if err != nil || !strings.Contains(c, `"255.255.255.255"`) || !strings.Contains(c, "wakeonlan -i 255.255.255.255 -p 9 aa:bb:cc:dd:ee:ff") {
		t.Fatalf("%s %v", c, err)
	}
	// a network's broadcast address is sent to, and the limited broadcast as well
	c, err = RemoteCommand(p, "aa:bb:cc:dd:ee:ff", "10.234.179.255", 9)
	if err != nil || !strings.Contains(c, `"10.234.179.255","255.255.255.255"`) || !strings.Contains(c, "wakeonlan -i 10.234.179.255") || !strings.Contains(c, "wakeonlan -i 255.255.255.255") {
		t.Fatalf("%s %v", c, err)
	}
}

func TestParseNBSTAT(t *testing.T) {
	b := make([]byte, 57)
	b[7] = 1 // one answer
	b[56] = 2
	n1 := append([]byte("WORKGROUP      "), 0x00, 0x84, 0x00) // group name
	n2 := append([]byte("NAS01          "), 0x00, 0x04, 0x00)
	b = append(b, n1...)
	b = append(b, n2...)
	if got := parseNBSTAT(b); got != "NAS01" {
		t.Fatalf("got %q", got)
	}
}

func TestPingRejectsOptions(t *testing.T) {
	if Ping(t.Context(), "-f", time.Second) || Ping(t.Context(), "a;b", time.Second) {
		t.Fatal("unsafe host accepted")
	}
}

func TestPlanAdvertisesOnTheWholeNetwork(t *testing.T) {
	// whatever networks this computer is on: the limited broadcast is always planned, with the
	// system's choice of interface as the last resort, and no destination is planned twice
	for _, bc := range []string{"", "10.234.179.255"} {
		plan := Plan(bc)
		seen := map[string]bool{}
		var limited, wanted bool
		for _, d := range plan {
			k := d.Addr + "|" + d.From.String()
			if seen[k] {
				t.Errorf("%q: %s planned twice", bc, k)
			}
			seen[k] = true
			limited = limited || d.Addr == Limited
			wanted = wanted || d.Addr == bc
		}
		if !limited || (bc != "" && !wanted) {
			t.Errorf("%q: limited %v, given address %v: %+v", bc, limited, wanted, plan)
		}
		if last := plan[len(plan)-1]; last.Addr != Limited || last.From != nil {
			t.Errorf("%q: the last destination is %+v", bc, last)
		}
	}
	// a wrong broadcast address is still refused
	if _, err := Send([]byte{1}, "not-an-ip", 9); err == nil {
		t.Fatal("bad address accepted")
	}
}

func TestMACsAndKnock(t *testing.T) {
	m, err := MACs("AA-BB-CC-DD-EE-FF, 11:22:33:44:55:66")
	if err != nil || len(m) != 2 || m[0] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("%v %v", m, err)
	}
	if _, err := MACs("aa:bb, x"); err == nil {
		t.Fatal("bad MAC accepted")
	}
	if p := KnockPorts("22, 5900;445 x 70000"); len(p) != 3 || p[2] != 445 {
		t.Fatalf("%v", p)
	}
	if _, err := KnockCommand("mac.local; rm -rf /", []int{22}); err == nil {
		t.Fatal("a host name was accepted")
	}
	// the knock reaches a listening port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	got := make(chan bool, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
			got <- true
		}
	}()
	cmd, err := KnockCommand("127.0.0.1", []int{ln.Addr().(*net.TCPAddr).Port})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("no connection")
	}
}

// The perl fallback is valid perl (python3 is missing on some NAS systems).
func TestRemoteCommandPerl(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("no perl")
	}
	p, _ := Packet("aa:bb:cc:dd:ee:ff")
	c, _ := RemoteCommand(p, "aa:bb:cc:dd:ee:ff", "127.255.255.255", 9)
	i := strings.Index(c, "perl -e '")
	j := strings.Index(c[i+9:], "'")
	if out, err := exec.Command("perl", "-c", "-e", c[i+9:i+9+j]).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
}

// The perl path of the remote command, run by a plain POSIX sh (dash, like
// busybox ash on a NAS) with python3 failing: the packet arrives intact.
func TestRemoteCommandPerlUnderPosixSh(t *testing.T) {
	if _, err := exec.LookPath("dash"); err != nil {
		t.Skip("no dash")
	}
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("no perl")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "python3"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	p, _ := Packet("aa:bb:cc:dd:ee:ff")
	c, _ := RemoteCommand(p, "aa:bb:cc:dd:ee:ff", "127.0.0.1", ln.LocalAddr().(*net.UDPAddr).Port)
	cmd := exec.Command("dash", "-c", c)
	cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	_ = ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 200)
	n, _, err := ln.ReadFromUDP(buf)
	if err != nil || string(buf[:n]) != string(p) {
		t.Fatalf("got %d bytes, %v", n, err)
	}
}
