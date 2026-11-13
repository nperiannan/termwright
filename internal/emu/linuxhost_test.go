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
	c := FillPort(im.PortAddr[0], "0C:01:03:00:00:02", "10.0.0.5/24")
	if !strings.Contains(c, "/0c:01:03:00:00:02/") || !strings.Contains(c, "ip addr add 10.0.0.5/24 dev") {
		t.Fatal(c)
	}
	a, err := ParseAppliance([]byte(`{"name":"Alpine Linux","category":"guest","default_username":"root","qemu":{"adapters":2,"ram":128},"images":[{"filename":"a.qcow2","version":"3"}],"versions":[{"name":"3","images":{"hda_disk_image":"a.qcow2"}}]}`))
	if err != nil || len(a.Profiles[0].PortAddr) != 1 || a.Profiles[0].Login != "root" {
		t.Fatal(err, a.Profiles)
	}
}
