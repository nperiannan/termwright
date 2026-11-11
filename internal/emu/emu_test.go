package emu

import (
	"context"
	"encoding/base64"
	"fmt"
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
		"telnet:127.0.0.1:20002,server,nowait", "-daemonize", "e1000,id=nic3,mac=52:54:a1:00:00:03"} {
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
	if !strings.Contains(s, `b="$HOME/termwright-lab/images/demo.qcow2"`) || !strings.Contains(s, `qemu-img create -q -f qcow2 -F "${f:-qcow2}" -b "$b" termwright-lab/lab-a1/v1.qcow2`) || !strings.Contains(s, "qemu-system-x86_64 -name core-1") {
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
	 "images":[{"filename":"demo-1.0.qcow2","version":"v1.0","md5sum":"ABC","filesize":10,"direct_download_url":"https://example.com/demo-1.0.qcow2"}],
	 "versions":[{"name":"v1.0","images":{"hda_disk_image":"demo-1.0.qcow2"}}]}`))
	if err != nil || len(a.Profiles) != 1 {
		t.Fatal(err, a)
	}
	p := a.Profiles[0]
	if p.Name != "Demo VM" || p.Version != "1.0" || p.URL != "https://example.com/demo-1.0.qcow2" || p.MD5 != "abc" || p.Size != 10 || p.FileName != "demo-1.0.qcow2" || p.NICs != 8 || p.NIC != "virtio-net-pci" || p.Extra != "-cpu Nehalem" || p.PortNameOf(3) != "Ethernet3" {
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

func TestMoreDisks(t *testing.T) {
	l, imgs := testLab()
	im := imgs["i1"]
	im.Disk = "sata"
	im.Disks = []Disk{{Slot: "hdb", FileName: "two.qcow2", Bus: "sata"}, {Slot: "cdrom", FileName: "seed.iso"}, {Slot: "bios", FileName: "OVMF.fd"}}
	a := strings.Join(l.Args(l.VMs[0], im), " ")
	for _, want := range []string{"-device ahci,id=ahci -drive file=termwright-lab/lab-a1/v1.qcow2,if=none,id=d0", "ide-hd,drive=d1,bus=ahci.1", "file=termwright-lab/lab-a1/v1-hdb.qcow2",
		"file=termwright-lab/images/seed.iso,media=cdrom,readonly=on", "-bios termwright-lab/images/OVMF.fd"} {
		if !strings.Contains(a, want) {
			t.Errorf("args lack %q:\n%s", want, a)
		}
	}
	if strings.Count(a, "ahci,id=ahci") != 1 {
		t.Error("two AHCI controllers")
	}
	if s := l.PrepareScript(l.VMs[:1], imgs); !strings.Contains(s, "v1-hdb.qcow2") || !strings.Contains(s, "images/two.qcow2") {
		t.Error(s)
	}
	if !strings.Contains(l.ResetScript([]string{"v1"}), "v1-hd?.qcow2") {
		t.Error("reset keeps the second disk")
	}
	if im.HasFiles() {
		t.Error("files missing")
	}
}

func TestDownLinks(t *testing.T) {
	l, imgs := testLab()
	l.Cables = []Cable{{A: "v1", APort: 1, B: "v2", BPort: 1}, {A: "v1", APort: 2, B: "v3", BPort: 1, Down: true}}
	got := strings.Join(l.DownLinks(l.VMs[0], imgs["i1"]), " ")
	if got != "n2 nic3" {
		t.Fatal(got)
	}
	if a := strings.Join(l.Args(l.VMs[0], imgs["i1"]), " "); !strings.Contains(a, "e1000,id=nic3,mac=") {
		t.Fatal(a)
	}
}

func TestPortFormula(t *testing.T) {
	im := &Image{PortName: "Ethernet{n*4-4}"}
	if im.PortNameOf(1) != "Ethernet0" || im.PortNameOf(3) != "Ethernet8" {
		t.Fatal(im.PortNameOf(1), im.PortNameOf(3))
	}
}

// Day-0 on a console that asks to confirm a command, prints a log line after
// the prompt and pages, with an old questionnaire in a command's output.
func TestConsoleQuestions(t *testing.T) {
	old := rePrompt
	rePrompt = 300 * time.Millisecond
	defer func() { rePrompt = old }()
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
		one := make([]byte, 1)
		read := func(stop byte) string {
			var s strings.Builder
			for {
				if _, err := b.Read(one); err != nil {
					return s.String()
				}
				if one[0] == stop || one[0] == '\r' {
					if one[0] == ' ' {
						return "<space>"
					}
					return s.String()
				}
				s.WriteByte(one[0])
			}
		}
		read('\r')
		w("sw login: ")
		lines = append(lines, read('\r'))
		w("password: ")
		lines = append(lines, read('\r'))
		w("sw.1 # ")
		lines = append(lines, read('\r'))
		w("\r\nThis will take a while. Continue? (y/N) ")
		lines = append(lines, read('\r'))
		w("\r\n* sw.2 # \r\n<Info:AAA> ssh2 started\r\n")
		lines = append(lines, read('\r')) // Enter for the prompt again
		w("* sw.3 # ")
		lines = append(lines, read('\r'))
		w("\r\nWould you like to disable Telnet? [y/N/q]: q\r\nline\r\nPress <SPACE> to continue or <Q> to quit:")
		lines = append(lines, read(' '))
		w("\r\nmore\r\n* sw.4 # ")
		got <- lines
		time.Sleep(200 * time.Millisecond)
		b.Close()
	}()
	c := NewConsole(a)
	var asked []string
	c.OnAnswer = func(q, s string) { asked = append(asked, q+" => "+s) }
	im := &Image{Login: "admin", Answers: []Answer{{Match: "[y/N/q]:", Send: "q"}}, Day0: []string{"enable ssh2", "show log"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := Day0(ctx, c, im, VM{Name: "sw"}, 1, nil); err != nil {
		t.Fatal(err, c.Text())
	}
	l := <-got
	if strings.Join(l, "|") != "admin||enable ssh2|y||show log|<space>" {
		t.Fatalf("%q", l)
	}
	if len(asked) != 1 || !strings.Contains(asked[0], "Continue? (y/N) => y") {
		t.Fatalf("answers %q", asked)
	}
}

// The host's network, routed: a bridge, proxy ARP both ways, a /32 route per switch.
func TestCloudMgmt(t *testing.T) {
	l := &Lab{ID: "c1", Dir: "x", Mgmt: "cloud", MgmtNet: "10.50.0.146/24", CloudGw: "10.50.0.254", CloudIf: "ens160", HostIP: "10.50.0.20", VMs: []VM{{ID: "v1", Index: 0}, {ID: "v2", Index: 1}}}
	if ip, gw := l.Address(1); ip != "10.50.0.147/24" || gw != "10.50.0.254" {
		t.Fatal(ip, gw)
	}
	if !l.Fixed() || !strings.Contains(strings.Join(l.Args(l.VMs[0], &Image{NICs: 2}), " "), "tap,id=n0,ifname=twc10m") {
		t.Fatal("not tapped")
	}
	s := l.StartScript(l.VMs, nil)
	for _, want := range []string{"ip link add twc1br type bridge", "sudo -n ip addr add 10.50.0.20/32 dev twc1br", "net.ipv4.conf.twc1br.proxy_arp=1 net.ipv4.conf.ens160.proxy_arp=1", "net.ipv4.ip_forward=1", "ip route replace 10.50.0.146/32 dev twc1br", "ip route replace 10.50.0.147/32 dev twc1br"} {
		if !strings.Contains(s, want) {
			t.Errorf("start lacks %q:\n%s", want, s)
		}
	}
	if !strings.Contains(l.StopScript([]string{"v2"}), "ip route del 10.50.0.147/32 dev twc1br") {
		t.Error("route kept")
	}
}

// A yes/no question at the login (EXOS: save to primary.cfg? after its
// questionnaire) takes its default, so the check after it gets its prompt.
func TestLoginDefaultAnswer(t *testing.T) {
	a, b := net.Pipe()
	got := make(chan string, 1)
	go func() {
		w := func(s string) { _, _ = b.Write([]byte(s)) }
		buf := make([]byte, 64)
		read := func() string {
			var s strings.Builder
			for {
				n, err := b.Read(buf)
				if err != nil {
					return s.String()
				}
				for _, ch := range buf[:n] {
					if ch == '\r' {
						return s.String()
					}
					s.WriteByte(ch)
				}
			}
		}
		read()
		w("login: ")
		read()
		w("password: ")
		read()
		w("Would you like to disable Telnet? [y/N/q]: ")
		q := read()
		w("\r\nDo you wish to save your configuration changes to primary.cfg? (y/N) ")
		ans := read()
		w("\r\n* sw.1 # ")
		cmd := read()
		got <- q + "|" + ans + "|" + cmd
		w("\r\nSwitch : 5520-24T\r\n* sw.2 # ")
		time.Sleep(200 * time.Millisecond)
		b.Close()
	}()
	c := NewConsole(a)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := Ask(ctx, c, &Image{Login: "admin", Answers: []Answer{{Match: "[y/N/q]:", Send: "q"}}}, "show switch")
	if err != nil || !strings.Contains(out, "5520-24T") {
		t.Fatal(err, out)
	}
	if g := <-got; g != "q|n|show switch" {
		t.Fatalf("%q", g)
	}
}

func TestParseLags(t *testing.T) {
	exos := `Load Sharing Monitor
Config    Current Agg     Min     Ld Share  Dist  Ld Share  Agg Link  Link Up
Master    Master  Control Active  Algorithm Flags Group     Mbr State Transitions
==============================================================================
     1       1    LACP       1    L3_L4     A     1         Y     A         1
                                  L3_L4     A     2         Y     A         1
     5       -    Static     1    L2        A     5         N     R         0
                                  L2        A     6         N     R         0
==============================================================================
Link State: A-Active, D-Disabled, R-Ready, NP-Port not present, L-Loopback`
	got := ParseLags(exos)
	if len(got) != 2 || got[0].Mode != "lacp" || strings.Join(got[0].Ports, ",") != "1,2" || got[1].Mode != "static" || strings.Join(got[1].Ports, ",") != "5,6" {
		t.Fatalf("exos %+v", got)
	}
	voss := `================================================================================
                                    Mlt Info
================================================================================
                        PORT     SVLAN   MLT        MLT          PORT         VLAN
MLTID IFINDEX NAME      TYPE     TYPE    ADMIN      CURRENT      MEMBERS      IDS
--------------------------------------------------------------------------------
1     6144    MLT-1     access   normal  norm       norm         1/1-1/2,1/5  1`
	got = ParseLags(voss)
	if len(got) != 1 || strings.Join(got[0].Ports, ",") != "1/1,1/2,1/5" {
		t.Fatalf("voss %+v", got)
	}
	im := &Image{NICs: 9, PortName: "{n}"}
	if im.DataPortOf("1:3") != 3 || im.DataPortOf("7") != 7 || im.DataPortOf("99") != 0 {
		t.Fatal("port names")
	}
}

// Two hosts: each runs its switches with its own management; a cable between
// them is a UDP tunnel to the other host's address.
func TestTwoHosts(t *testing.T) {
	im := &Image{ID: "i1", NICs: 4, NIC: "e1000", FileName: "x.qcow2"}
	l := &Lab{ID: "a1", Dir: "d", Base: 20000, Mgmt: "cloud", MgmtNet: "10.1.0.146/24", CloudGw: "10.1.0.254", CloudIf: "ens160", HostIP: "10.1.0.20",
		VMs:    []VM{{ID: "v1", Image: "i1", Index: 0}, {ID: "v2", Image: "i1", Index: 1, Host: "kvm-2"}},
		Cables: []Cable{{A: "v1", APort: 1, B: "v2", BPort: 2}},
		Nets:   map[string]HostNet{"kvm-2": {Mgmt: "nat"}},
		Addrs:  map[string]string{"kvm-1": "10.1.0.20", "kvm-2": "10.1.0.60"}}
	if h := l.Hosts("kvm-1"); strings.Join(h, ",") != "kvm-1,kvm-2" {
		t.Fatal(h)
	}
	a1 := strings.Join(l.For("kvm-1", "kvm-1").Args(l.VMs[0], im), " ")
	a2 := strings.Join(l.For("kvm-2", "kvm-1").Args(l.VMs[1], im), " ")
	if !strings.Contains(a1, fmt.Sprintf("udp=10.1.0.60:%d,localaddr=0.0.0.0:%d", l.udpPort(l.VMs[1], 2), l.udpPort(l.VMs[0], 1))) {
		t.Errorf("kvm-1 side: %s", a1)
	}
	if !strings.Contains(a2, fmt.Sprintf("udp=10.1.0.20:%d,localaddr=0.0.0.0:%d", l.udpPort(l.VMs[0], 1), l.udpPort(l.VMs[1], 2))) {
		t.Errorf("kvm-2 side: %s", a2)
	}
	if v := l.For("kvm-2", "kvm-1"); v.Mgmt != "nat" || l.For("kvm-1", "kvm-1").Mgmt != "cloud" {
		t.Error("management per host")
	}
	if ip, _ := l.For("kvm-1", "kvm-1").Address(0); ip != "10.1.0.146/24" {
		t.Error("cloud address on its host", ip)
	}
}

// A log message right after the login hides the prompt: Enter shows it again.
func TestLoginPromptHidden(t *testing.T) {
	old := rePrompt
	rePrompt = 300 * time.Millisecond
	defer func() { rePrompt = old }()
	a, b := net.Pipe()
	go func() {
		w := func(s string) { _, _ = b.Write([]byte(s)) }
		buf := make([]byte, 64)
		read := func() string {
			var s strings.Builder
			for {
				n, err := b.Read(buf)
				if err != nil {
					return s.String()
				}
				for _, ch := range buf[:n] {
					if ch == '\r' {
						return s.String()
					}
					s.WriteByte(ch)
				}
			}
		}
		read()
		w("Login: ")
		read()
		w("Password: ")
		read()
		w("\r\nVSP:1>\r\nCP1 Mgmt SW INFO ZTP+ discovery failed - No DNS server configured\r\n")
		read() // the Enter that shows the prompt again
		w("\r\nVSP:1>")
		read()
		w("\r\nversion 9.4\r\nVSP:1>")
		time.Sleep(200 * time.Millisecond)
		b.Close()
	}()
	c := NewConsole(a)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := Ask(ctx, c, &Image{Login: "rwa", Password: "rwa"}, "show version")
	if err != nil || !strings.Contains(out, "version 9.4") {
		t.Fatalf("got %q, %v", out, err)
	}
}

// A first login that asks for a new password gets the image's password twice.
func TestLoginNewPassword(t *testing.T) {
	a, b := net.Pipe()
	got := make(chan string, 1)
	go func() {
		w := func(s string) { _, _ = b.Write([]byte(s)) }
		buf := make([]byte, 64)
		read := func() string {
			var s strings.Builder
			for {
				n, err := b.Read(buf)
				if err != nil {
					return s.String()
				}
				for _, ch := range buf[:n] {
					if ch == '\r' {
						return s.String()
					}
					s.WriteByte(ch)
				}
			}
		}
		read()
		w("Login: ")
		read()
		w("Password: ")
		read()
		w("\r\n\tPlease change the password to continue.\r\n\r\nEnter the New password : ")
		n1 := read()
		w("\r\nRe-enter the New password : ")
		n2 := read()
		got <- n1 + "|" + n2
		w("\r\nVSP:1>")
		read()
		w("\r\nversion 9.4\r\nVSP:1>")
		time.Sleep(200 * time.Millisecond)
		b.Close()
	}()
	c := NewConsole(a)
	var shown string
	c.OnAnswer = func(q, sent string) { shown += sent }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	im := &Image{Login: "rwa", Password: "pw1", Answers: []Answer{{Match: "New password", Send: "{password}"}}}
	out, err := Ask(ctx, c, im, "show version")
	if err != nil || !strings.Contains(out, "version 9.4") {
		t.Fatalf("got %q, %v", out, err)
	}
	if g := <-got; g != "pw1|pw1" || strings.Contains(shown, "pw1") {
		t.Fatalf("%q, shown %q", g, shown)
	}
}

// A switch is ready when its whole console log shows the ready text, not
// only its end (a login and show commands push it out).
func TestStatusReady(t *testing.T) {
	l := &Lab{Dir: "lab"}
	s := l.StatusScript([]string{"v1", "v2"}, map[string]string{"v1": "login:"})
	if !strings.Contains(s, "grep -qaF -- login: v1.log") || strings.Contains(s, "v2.log 2>/dev/null && echo") {
		t.Fatal(s)
	}
	if st := ParseStatus("up v1 5\nready v1\n", nil); !st.Ready["v1"] || st.Ready["v2"] {
		t.Fatal(st.Ready)
	}
}

func TestPagerQuit(t *testing.T) {
	for _, s := range []string{"x\r\n--More-- (q = quit) ", "--More--", "Press <SPACE> to continue or <Q> to quit:"} {
		if !pager.MatchString(s) {
			t.Fatal(s)
		}
	}
}

func TestLagCommands(t *testing.T) {
	im := &Image{Name: "NOS", PortName: "1/{n}", LagSet: map[string][]string{"lacp": {"lag {id} add {ports} master {master} lacp"}}, LagDel: []string{"no lag {id}"}}
	got, err := im.LagCommands(Lag{Name: "LAG 3", Mode: "lacp", Ports: []int{2, 4}}, false)
	if err != nil || len(got) != 1 || got[0] != "lag 3 add 1/2,1/4 master 1/2 lacp" {
		t.Fatal(got, err)
	}
	if got, _ := im.LagCommands(Lag{Name: "LAG 3", Mode: "lacp", Ports: []int{2}}, true); got[0] != "no lag 3" {
		t.Fatal(got)
	}
	if _, err := im.LagCommands(Lag{Name: "x", Mode: "static", Ports: []int{1}}, false); err == nil {
		t.Fatal("static has no commands")
	}
}

// A data port on the host's network: a tap on its bridge, or macvtap with its own fd.
func TestUplinks(t *testing.T) {
	im := &Image{FileName: "x.qcow2", NICs: 4, NIC: "e1000"}
	for _, kind := range []string{"bridge", "macvtap"} {
		l := &Lab{ID: "u1", Dir: "lab", Mgmt: "nat", LanIf: "br0", LanKind: kind, VMs: []VM{{ID: "v1", Image: "i", Uplinks: []int{2}}}}
		args := strings.Join(l.Args(l.VMs[0], im), " ")
		sc := l.StartScript(l.VMs, map[string]*Image{"i": im})
		if kind == "bridge" && (!strings.Contains(args, "tap,id=n2,ifname=twu10u2") || !strings.Contains(sc, "master br0")) {
			t.Fatalf("bridge:\n%s\n%s", args, sc)
		}
		if kind == "macvtap" && (!strings.Contains(args, "tap,id=n2,fd=12") || !strings.Contains(sc, "type macvtap") || !strings.Contains(sc, "12<>/dev/tap$i2")) {
			t.Fatalf("macvtap:\n%s\n%s", args, sc)
		}
		if st := l.StopScript([]string{"v1"}); !strings.Contains(st, "ip link del twu10u2") {
			t.Fatal(st)
		}
	}
}
