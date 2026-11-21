package topo

import (
	"context"
	"testing"
	"time"
)

func runCustom(t *testing.T, os string, outs map[string]string) Device {
	t.Helper()
	c, ok := BuiltinCustom(os)
	if !ok {
		t.Fatal("no plan for", os)
	}
	run := func(_ context.Context, cmds []string) ([]string, error) {
		r := make([]string, len(cmds))
		for i, c := range cmds {
			r[i] = outs[c]
		}
		return r, nil
	}
	return CollectCustom(context.Background(), Target{Session: "s", Name: "d", OS: os}, c, run, time.Now())
}

func TestCustomHuawei(t *testing.T) {
	d := runCustom(t, "huawei", map[string]string{
		"display lldp local": "System information\nChassis type          :macAddress\nChassis ID            :00e0-fc12-3456\nSystem name           :SwitchA\n",
		"display lldp neighbor": `GigabitEthernet0/0/1 has 1 neighbor(s):

Neighbor index :1
Chassis type   :MAC address
Chassis ID     :00e0-fc12-0001
Port ID type   :Interface name
Port ID        :GigabitEthernet0/0/2
System name    :SwitchB
Management address value :10.1.1.2

GigabitEthernet0/0/3 has 2 neighbor(s):

Neighbor index :1
Chassis ID     :00e0-fc12-0002
Port ID        :GE0/0/9
System name    :SwitchC

Neighbor index :2
Chassis ID     :00e0-fc12-0003
Port ID        :GE0/0/10
System name    :SwitchD
`})
	if d.Err != "" || d.SysName != "SwitchA" || d.Chassis != "00:e0:fc:12:34:56" {
		t.Fatalf("%+v", d)
	}
	if len(d.Neighbors) != 3 || d.Neighbors[0].LocalPort != "GigabitEthernet0/0/1" || d.Neighbors[0].IP != "10.1.1.2" || d.Neighbors[2].LocalPort != "GigabitEthernet0/0/3" || d.Neighbors[2].Name != "SwitchD" {
		t.Fatalf("%+v", d.Neighbors)
	}
}

func TestCustomICX(t *testing.T) {
	d := runCustom(t, "icx", map[string]string{
		"show lldp local-info": "  Chassis ID (MAC address): 609c.9f1d.2a40\n  System name         : \"ICX-A\"\n",
		"show lldp neighbors detail": `Local port: 1/1/1
  Neighbor: 609c.9f1d.2b40, TTL 113 seconds
    + Chassis ID (MAC address): 609c.9f1d.2b40
    + Port ID (interface name): 1/1/2
    + System name         : "ICX-B"
    + Management address (IPv4): 10.0.0.2

Local port: 1/1/5
  Neighbor: 609c.9f1d.2c40, TTL 100 seconds
    + Chassis ID (MAC address): 609c.9f1d.2c40
    + Port ID (MAC address): 609c.9f1d.2c41
    + System name         : "ICX-C"
`})
	if d.SysName != "ICX-A" || len(d.Neighbors) != 2 || d.Neighbors[0].Name != "ICX-B" || d.Neighbors[0].RemotePort != "1/1/2" || d.Neighbors[1].LocalPort != "1/1/5" || d.Neighbors[1].Chassis != "60:9c:9f:1d:2c:40" {
		t.Fatalf("%+v %+v", d, d.Neighbors)
	}
}

func TestCustomEltex(t *testing.T) {
	d := runCustom(t, "eltex", map[string]string{
		"show system": "System Name:                    mes-1\nSystem MAC Address:             a8:f9:4b:00:00:01\n",
		"show lldp neighbors": `  Port        Device ID          Port ID          System Name    Capabilities  TTL
--------- ----------------- ----------------- ----------------- ------------ -----
gi1/0/1   a8:f9:4b:aa:bb:cc   gi1/0/24          mes-2          B, R         104
gi1/0/2   a8:f9:4b:aa:bb:dd   gi1/0/23          mes-3          B            98
`})
	if d.SysName != "mes-1" || len(d.Neighbors) != 2 || d.Neighbors[1].Name != "mes-3" || d.Neighbors[0].RemotePort != "gi1/0/24" {
		t.Fatalf("%+v %+v", d, d.Neighbors)
	}
}

func TestCustomAllowed(t *testing.T) {
	for c, ok := range map[string]bool{"display lldp neighbor": true, "show lldp neighbors | no-more": true, "screen-length 0 temporary": true,
		"configure terminal": false, "show run | include x": false, "display x; reboot": false, "reload": false} {
		if CustomAllowed(c) != ok {
			t.Error(c)
		}
	}
	if OSFor("Huawei VRP") != "huawei" || OSFor("Ruckus ICX") != "icx" || OSFor("Eltex MES") != "eltex" || OSFor("Extreme EXOS") != "exos" {
		t.Error("OSFor")
	}
}
