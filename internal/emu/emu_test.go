package emu

import (
	"context"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"
)

func testLab() (*Lab, map[string]*Image) {
	im := &Image{ID: "i1", Name: "Demo NOS", FileName: "demo.qcow2", CPUs: 1, RAM: 512, NICs: 4, NIC: "e1000", Disk: "ide", PortName: "1/{n}", Ready: "login:"}
	l := &Lab{ID: "a1", Dir: "termwright-lab", Mgmt: "forward", Base: 20000, KVM: true,
		VMs: []VM{{ID: "v1", Name: "core-1", Image: "i1", Index: 0}, {ID: "v2", Name: "core-2", Image: "i1", Index: 1}, {ID: "v3", Name: "leaf-1", Image: "i1", Index: 2}}}
	return l, map[string]*Image{"i1": im}
}

func TestArgsAndWiring(t *testing.T) {
	l, imgs := testLab()
	l.Wire(l.VMs, "leaf-spine", imgs)
	if len(l.Cables) != 3 || l.Cables[0] != (Cable{A: "v1", APort: 1, B: "v2", BPort: 1}) {
		t.Fatalf("cables %+v", l.Cables)
	}
	a := strings.Join(l.Args(l.VMs[0], imgs["i1"]), " ")
	for _, want := range []string{"-enable-kvm", "-m 512", "file=termwright-lab/lab-a1/v1.qcow2,if=ide", "hostfwd=tcp:127.0.0.1:20000-:22",
		"socket,id=n1,udp=127.0.0.1:24065,localaddr=127.0.0.1:24001", "port=20001,server=on,wait=off,telnet=on,logfile=termwright-lab/lab-a1/v1.log",
		"telnet:127.0.0.1:20002,server,nowait", "-daemonize", "e1000,mac=52:54:a1:00:00:03"} {
		if !strings.Contains(a, want) {
			t.Errorf("args lack %q:\n%s", want, a)
		}
	}
	// the other end points back
	b := strings.Join(l.Args(l.VMs[1], imgs["i1"]), " ")
	if !strings.Contains(b, "udp=127.0.0.1:24001,localaddr=127.0.0.1:24065") {
		t.Errorf("peer args: %s", b)
	}
	s := l.StartScript(l.VMs[:1], imgs)
	if !strings.Contains(s, `qemu-img create -q -f qcow2 -F qcow2 -b "$HOME/termwright-lab/images/demo.qcow2" termwright-lab/lab-a1/v1.qcow2`) || !strings.Contains(s, "qemu-system-x86_64 -name core-1") {
		t.Errorf("start script:\n%s", s)
	}
	l.Mgmt, l.MgmtNet = "bridge", "10.99.0.0/24"
	if ip, gw := l.Address(2); ip != "10.99.0.13/24" || gw != "10.99.0.1" {
		t.Errorf("address %s %s", ip, gw)
	}
	if !strings.Contains(strings.Join(l.Args(l.VMs[0], imgs["i1"]), " "), "tap,id=n0,ifname=twa10m") {
		t.Error("tap")
	}
	v0 := l.VMs[0]
	v0.Serial, v0.BaseMAC = "AB12,3", "02:04:96:00:00:fe"
	if a := strings.Join(l.Args(v0, imgs["i1"]), " "); !strings.Contains(a, "type=1,serial=AB12,,3") || !strings.Contains(a, "mac=02:04:96:00:01:00") {
		t.Errorf("serial/mac args: %s", a)
	}
	if got := Fill("configure vlan Mgmt ipaddress {mgmtAddr} {mgmtMask} ; {hostname} {gateway}", VM{Name: "sw", MgmtIP: "10.0.2.15/24", Gateway: "10.0.2.2"}, 1); got != "configure vlan Mgmt ipaddress 10.0.2.15 255.255.255.0 ; sw 10.0.2.2" {
		t.Error(got)
	}
}

func TestLanAndAddress(t *testing.T) {
	l, imgs := testLab()
	l.Mgmt, l.LanIf, l.LanKind = "lan", "eno1", "macvtap"
	sc := l.StartScript(l.VMs[:1], imgs)
	if !strings.Contains(sc, "ip link add link eno1 name twa10m address 52:54:a1:00:00:00 type macvtap mode bridge") || !strings.Contains(sc, "3<>/dev/tap$idx") || !strings.Contains(sc, "tap,id=n0,fd=3") {
		t.Fatalf("macvtap:\n%s", sc)
	}
	l.LanIf, l.LanKind = "br0", "bridge"
	if sc := l.StartScript(l.VMs[:1], imgs); !strings.Contains(sc, "master br0 up") {
		t.Fatalf("bridge:\n%s", sc)
	}
	if ip, gw := l.Address(0); ip != "" || gw != "" {
		t.Error("lan address")
	}
	out := "show vlan Mgmt\r\nMgmt   4095  10.127.35.22  /24  ----  ANY  1 /1   VR-Mgmt\nmask 255.255.255.0\n"
	if a := FindAddress(CleanConsole(out)); a != "10.127.35.22" {
		t.Errorf("address %q", a)
	}
	if a := FindAddress("x\n inet 0.0.0.0 127.0.0.1 169.254.1.1 192.168.1.20/24"); a != "192.168.1.20/24" {
		t.Errorf("address %q", a)
	}
	h := ParseProbe("user=lab\narch=x86_64\nkvm=yes\nqemu=8\nqemuimg=yes\nsudo=yes\nuplink=br0\nbridge=br0:eno1,\nbridge=virbr0:\n", "d")
	if h.LanIf != "br0" || h.LanKind != "bridge" || len(h.Bridges) != 1 {
		t.Errorf("%+v", h)
	}
	h = ParseProbe("user=lab\narch=x86_64\nuplink=ens160\n", "d")
	if h.LanIf != "ens160" || h.LanKind != "macvtap" {
		t.Errorf("%+v", h)
	}
}

func TestStatusAndProbe(t *testing.T) {
	log := base64.StdEncoding.EncodeToString([]byte("SeaBIOS\r\n\x1b[2JBooting...\r\nsw1 login: "))
	st := ParseStatus("clk 100\nup v1 12.5\ndown v2\nlog v1 "+log+"\n", base64.StdEncoding.DecodeString)
	if st.Up["v1"] != 12.5 || st.Clk != 100 || len(st.Up) != 1 || !strings.HasSuffix(st.Log["v1"], "login: ") || strings.Contains(st.Log["v1"], "\x1b") {
		t.Fatalf("%+v", st)
	}
	h := ParseProbe("user=lab\nos=Ubuntu 24.04\narch=x86_64\nkvm=noaccess\nvt=yes\nqemu=QEMU emulator version 8.2.2 (Debian)\nqemuimg=yes\ncpus=32\nmemtotal=128000\nmemfree=96000\ndiskfree=500000\n", "termwright-lab")
	if h.OK() || len(h.Problems) != 1 || !strings.Contains(h.Problems[0], "kvm group") || h.QEMU != "8.2.2 (Debian)" || h.Fits(&Image{RAM: 2048, CPUs: 1}) != 46 {
		t.Fatalf("%+v %d", h, h.Fits(&Image{RAM: 2048, CPUs: 1}))
	}
}

func TestGNS3(t *testing.T) {
	a, err := ParseAppliance([]byte(`{"name":"Demo VM","qemu":{"adapter_type":"virtio-net-pci","adapters":8,"ram":2048,"hda_disk_interface":"virtio","arch":"x86_64","options":"-nographic -cpu Nehalem"},"port_name_format":"Ethernet{0}",
	 "versions":[{"name":"1.0","images":{"hda_disk_image":"demo-1.0.qcow2"}}]}`))
	if err != nil || len(a.Profiles) != 1 {
		t.Fatal(err, a)
	}
	p := a.Profiles[0]
	if p.Name != "Demo VM 1.0" || p.FileName != "demo-1.0.qcow2" || p.NICs != 8 || p.NIC != "virtio-net-pci" || p.Extra != "-cpu Nehalem" || p.PortNameOf(3) != "Ethernet3" {
		t.Fatalf("%+v", p)
	}
	if _, err := ParseAppliance([]byte(`{"name":"x","docker":{}}`)); err == nil {
		t.Error("docker appliance taken")
	}
}

// a fake switch console: login, a question, a prompt per command
func TestDay0(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan []string, 1)
	go func() {
		var lines []string
		w := func(s string) { _, _ = b.Write([]byte(s)) }
		buf := make([]byte, 256)
		read := func() string {
			var s strings.Builder
			for {
				n, err := b.Read(buf)
				if err != nil {
					return s.String()
				}
				for _, c := range buf[:n] {
					if c == '\r' {
						return s.String()
					}
					if c >= 0x20 && c < 0xf0 {
						s.WriteByte(c)
					}
				}
			}
		}
		w("\xff\xfb\x01\xff\xfb\x03Booting...\r\n")
		read()
		w("sw1 login: ")
		lines = append(lines, read())
		w("password: ")
		lines = append(lines, read())
		w("Disable Telnet? [y/N]: ")
		lines = append(lines, read())
		w("sw1.1 # ")
		for i := 0; i < 2; i++ {
			lines = append(lines, read())
			w("\r\nok\r\nsw1.2 # ")
		}
		got <- lines
		b.Close()
	}()
	c := NewConsole(a)
	im := &Image{Login: "admin", Password: "pw", Answers: []Answer{{Match: "[y/N]", Send: "n"}}, Day0: []string{"configure snmp sysName {hostname}", "save"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Day0(ctx, c, im, VM{Name: "core-1"}, 1, nil); err != nil {
		t.Fatal(err, c.Text())
	}
	l := <-got
	if strings.Join(l, "|") != "admin|pw|n|configure snmp sysName core-1|save" {
		t.Fatalf("%q", l)
	}
}

func TestNatAndChain(t *testing.T) {
	l, imgs := testLab()
	l.Mgmt = "nat"
	if a := strings.Join(l.Args(l.VMs[0], imgs["i1"]), " "); !strings.Contains(a, "tap,id=n0,ifname=twa10m") {
		t.Fatal(a)
	}
	sc := l.StartScript(l.VMs[:1], imgs)
	if !strings.Contains(sc, "net-start default") || !strings.Contains(sc, "master virbr0") {
		t.Fatal(sc)
	}
	if ip, gw := l.Address(0); ip != "" || gw != "" {
		t.Fatal(ip, gw)
	}
	if st := l.StatusScript(nil); !strings.Contains(st, "virbr0.status") || !strings.Contains(st, "ip neigh show dev virbr0") {
		t.Fatal(st)
	}
	st := ParseStatus("lease 52:54:00:AA:00:01 192.168.122.45\nlease 52:54:00:aa:00:01 192.168.122.99\nnatnet 192.168.122.1/24\n", nil)
	if st.Leases["52:54:00:aa:00:01"] != "192.168.122.45" || st.NatNet != "192.168.122.1/24" {
		t.Fatalf("%+v", st)
	}
	if !strings.Contains(l.StopScript([]string{"v1"}), "ip link del twa10m") {
		t.Fatal("tap not removed")
	}
	// switches added to a lab go on from its last switch
	l2, imgs2 := testLab()
	l2.Wire(l2.VMs[2:], "chain", imgs2)
	if len(l2.Cables) != 1 || l2.Cables[0].A != "v2" || l2.Cables[0].B != "v3" {
		t.Fatalf("%+v", l2.Cables)
	}
}
