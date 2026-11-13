package emu

import (
	"strings"
	"testing"
)

func TestLinuxHost(t *testing.T) {
	if !IsLinuxAppliance("Alpine Linux", "guest") || !IsLinuxAppliance("Ubuntu Cloud Guest", "") || IsLinuxAppliance("Ubuntu Cloud Guest", "router") || IsLinuxAppliance("VyOS", "guest") {
		t.Fatal("IsLinuxAppliance")
	}
	im := Image{Login: "alpine"}
	LinuxHost(&im)
	if im.Login != "alpine" || im.DeviceType != "Linux" || len(im.Day0) != 2 || len(im.PortAddr) != 1 || len(im.PortDHCP) != 1 || len(im.AddrSet) != 1 {
		t.Fatalf("%+v", im)
	}
	if im.NICs != 3 || im.PortName != "eth{n}" || !strings.Contains(im.Elevate, "sudo -s") || im.DataPorts() != 2 {
		t.Fatalf("a computer needs data ports and root: %+v", im)
	}
	c := FillPort(im.PortAddr[0], "0C:01:03:00:00:02", "10.0.0.5/24")
	if !strings.Contains(c, "/0c:01:03:00:00:02/") || !strings.Contains(c, "ip addr add 10.0.0.5/24 dev") {
		t.Fatal(c)
	}
	a, err := ParseAppliance([]byte(`{"name":"Alpine Linux","category":"guest","default_username":"root","qemu":{"adapters":2,"ram":128},"images":[{"filename":"a.qcow2","version":"3"}],"versions":[{"name":"3","images":{"hda_disk_image":"a.qcow2"}}]}`))
	if err != nil || len(a.Profiles[0].PortAddr) != 1 || a.Profiles[0].Login != "root" {
		t.Fatal(err, a.Profiles)
	}
	got := ParsePortShow("for d in ...; echo \"PA $m $a\"\nPA 52:54:01:00:03:00 192.168.122.31/24\nPA 0c:01:03:00:00:01 10.10.0.11/24\nPA 0c:01:03:00:00:01 2001:db8::11/64\nPA 0c:01:03:00:00:02 10.20.0.57/24\nLL 0c:01:03:00:00:01 fe80::e01:3ff:fe00:1/64\nGW 10.10.0.1\nDNS 10.10.0.1\n", map[int]string{0: "52:54:01:00:03:00", 1: "0C:01:03:00:00:01", 2: "0c:01:03:00:00:02"})
	if len(got.Addrs) != 2 || got.Addrs[1] != "10.10.0.11/24, 2001:db8::11/64" || got.Addrs[2] != "10.20.0.57/24" || got.LinkLocal[1] != "fe80::e01:3ff:fe00:1/64" || got.GW != "10.10.0.1" || got.DNS != "10.10.0.1" {
		t.Fatal(got)
	}
}

func TestArgsEscapeCommaInName(t *testing.T) {
	l := &Lab{}
	a := l.Args(VM{ID: "v1", Name: "tc-1,tc-2-1", Index: 1}, &Image{ID: "i", Name: "x"})
	if len(a) < 2 || a[0] != "-name" || a[1] != "tc-1,,tc-2-1" {
		t.Fatalf("%v", a[:2])
	}
}

func TestTinyCoreIsNotAComputer(t *testing.T) {
	if !NoSerialConsole("Tiny Core Linux") || NoSerialConsole("Alpine Linux Virt") {
		t.Fatal("serial console detection")
	}
	if IsLinuxAppliance("Tiny Core Linux", "guest") || !IsLinuxAppliance("Alpine Linux Virt", "guest") {
		t.Fatal("IsLinuxAppliance")
	}
}
