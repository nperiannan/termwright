package main

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/session"
)

// A route generator only on a Linux computer, with a fixed address on its port and good settings.
func TestEmuSetRoutingChecks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(filepath.Join(dir, "Sessions"))
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = true, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	emuLab.f.Host = "Lab/kvm-1"
	emuLab.f.Images = []emu.Image{{ID: "i1", Name: "EXOS-VM", NICs: 9, PortName: "{n}"}, {ID: "i2", Name: "Alpine", NICs: 3, PortName: "eth{n}", PortAddr: []string{"ip addr add {addr} dev {port}"}}}
	emuLab.f.Lab = &emu.Lab{ID: "a1", VMs: []emu.VM{{ID: "v1", Name: "sw1", Image: "i1"}, {ID: "v2", Name: "pc-1", Image: "i2", Index: 1, Addrs: map[int]string{2: "dhcp"}}}}
	ok := emu.Routing{Proto: "ospf", Port: 1, Routes: 10, Prefix: "100.64.0.0/24"}
	if _, err := a.EmuSetRouting("v1", ok); err == nil || !strings.Contains(err.Error(), "Linux computer") {
		t.Errorf("a switch: %v", err)
	}
	if _, err := a.EmuSetRouting("v2", ok); err == nil || !strings.Contains(err.Error(), "fixed IPv4 address") {
		t.Errorf("no address: %v", err)
	}
	if _, err := a.EmuSetRouting("v2", emu.Routing{Proto: "bgp", Port: 1, Routes: 10, Prefix: "100.64.0.0/24"}); err == nil || !strings.Contains(err.Error(), "peer") {
		t.Errorf("BGP without a peer: %v", err)
	}
	emuLab.f.Lab.VMs[1].Addrs[1] = "10.20.0.11/24"
	if _, err := a.EmuSetRouting("v2", ok); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("a stopped computer: %v", err)
	}
	if emuLab.f.Lab.VMs[1].Routing == nil || emuLab.f.Lab.VMs[1].Routing.Routes != 10 {
		t.Error("the settings are kept for its next start")
	}
}

// A click on a running VM's screen goes to its USB tablet (real QEMU when the sandbox has one).
func TestEmuScreenClickHMP(t *testing.T) {
	if _, err := exec.LookPath("qemu-system-x86_64"); err != nil {
		t.Skip("no qemu")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	q := exec.Command("qemu-system-x86_64", "-m", "64", "-usb", "-device", "usb-tablet", "-display", "none", "-S", "-monitor", fmt.Sprintf("telnet:127.0.0.1:%d,server,nowait", port))
	if err := q.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Process.Kill(); _, _ = q.Process.Wait() }()
	time.Sleep(time.Second)
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skip("no monitor: " + err.Error())
	}
	h := &emuHMP{c: c, r: bufio.NewReader(c)}
	defer h.close()
	if _, err := h.until(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := emuClick(h, 300, 200, 1, true); err != nil {
		t.Fatal(err)
	}
	out, _ := h.do("info mice", 3*time.Second)
	if !strings.Contains(out, "* Mouse #3: QEMU HID Tablet") && !strings.Contains(out, "*Mouse #3") {
		t.Fatalf("info mice: %q", out)
	}
}
