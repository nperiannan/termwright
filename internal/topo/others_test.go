package topo

import (
	"reflect"
	"testing"
)

// These samples are written in the vendors' documented layout (no such device in the lab).

func TestCiscoCDP(t *testing.T) {
	n := ParseCiscoCDP(`-------------------------
Device ID: sw2.example.com
Entry address(es): 
  IP address: 10.1.1.2
Platform: cisco WS-C3750X-48,  Capabilities: Switch IGMP 
Interface: GigabitEthernet1/0/48,  Port ID (outgoing port): GigabitEthernet1/0/1
Holdtime : 143 sec
-------------------------
Device ID: ap1
Entry address(es): 
Platform: cisco AIR-AP,  Capabilities: Trans-Bridge
Interface: GigabitEthernet1/0/3,  Port ID (outgoing port): GigabitEthernet0
Holdtime : 170 sec
`)
	if len(n) != 2 || n[0].Name != "sw2.example.com" || n[0].LocalPort != "GigabitEthernet1/0/48" || n[0].RemotePort != "GigabitEthernet1/0/1" || n[0].IP != "10.1.1.2" || n[0].Age != 37 || n[0].Proto != CDP {
		t.Fatalf("got %+v", n)
	}
	if n[1].IP != "" || n[1].Age != 10 {
		t.Fatalf("ap: %+v", n[1])
	}
}

func TestCiscoLLDP(t *testing.T) {
	n := ParseCiscoLLDP(`------------------------------------------------
Local Intf: Gi1/0/1
Chassis id: 0012.3456.789a
Port id: Gi1/0/2
Port Description: uplink
System Name: sw2
Time remaining: 110 seconds
Management Addresses:
    IP: 10.1.1.2
`)
	if len(n) != 1 || n[0].Chassis != "00:12:34:56:78:9a" || n[0].RemotePort != "Gi1/0/2" || n[0].Name != "sw2" || n[0].Age != 10 || n[0].IP != "10.1.1.2" {
		t.Fatalf("got %+v", n)
	}
}

func TestEtherchannel(t *testing.T) {
	l := ParseEtherchannel(`Group  Port-channel  Protocol    Ports
------+-------------+-----------+-----------------------------------------------
1      Po1(SU)         LACP      Gi1/0/1(P)   Gi1/0/2(P)
2      Po2(SD)         -         Gi1/0/3(D)
`)
	want := []LAG{{Name: "Po1", Mode: "lacp", Members: []string{"Gi1/0/1", "Gi1/0/2"}, Active: []string{"Gi1/0/1", "Gi1/0/2"}}, {Name: "Po2", Mode: "static", Members: []string{"Gi1/0/3"}}}
	if !reflect.DeepEqual(l, want) {
		t.Fatalf("got %+v", l)
	}
	a := ParseEtherchannel("   Po1(U)             LACP(a)     Et1(PG+) Et2(PG+)\n")
	if len(a) != 1 || a[0].Mode != "lacp" || len(a[0].Active) != 2 {
		t.Fatalf("arista: %+v", a)
	}
}

func TestIfStatusAndSTP(t *testing.T) {
	p := ParseIfStatus(`Port      Name               Status       Vlan       Duplex  Speed Type
Gi1/0/1   uplink to core     connected    trunk      a-full  a-1000 10/100/1000BaseTX
Gi1/0/2                      notconnect   1          auto    auto   10/100/1000BaseTX
Gi1/0/3                      disabled     1          auto    auto   10/100/1000BaseTX
`)
	if len(p) != 3 || !p[0].Oper || p[1].Oper || !p[1].Admin || p[2].Admin {
		t.Fatalf("got %+v", p)
	}
	m := ParseCiscoSTP(`VLAN0001
Interface           Role Sts Cost      Prio.Nbr Type
------------------- ---- --- --------- -------- --------------------------------
Gi1/0/1             Desg FWD 4         128.1    P2p
Gi1/0/2             Altn BLK 4         128.2    P2p
VLAN0010
Gi1/0/1             Desg FWD 4         128.1    P2p
Gi1/0/2             Altn BLK 4         128.2    P2p
Gi1/0/3             Desg FWD 4         128.3    P2p
VLAN0020
Gi1/0/3             Altn BLK 4         128.3    P2p
`)
	if m["Gi1/0/1"] != "forwarding" || m["Gi1/0/2"] != "blocked" || m["Gi1/0/3"] != "forwarding" {
		t.Fatalf("got %v", m)
	}
}

func TestEOSLLDP(t *testing.T) {
	n := ParseEOSLLDP(`{"lldpNeighbors": {"Ethernet1": {"lldpNeighborInfo": [{"chassisIdType": "macAddress", "chassisId": "001c.7300.0001", "systemName": "leaf2", "ttl": 120, "neighborInterfaceInfo": {"interfaceId": "\"Ethernet49\"", "interfaceId_v2": "Ethernet49"}, "managementAddresses": [{"address": "10.0.0.2"}]}]}}}`)
	want := Neighbor{Proto: LLDP, LocalPort: "Ethernet1", Chassis: "00:1c:73:00:00:01", RemotePort: "Ethernet49", Name: "leaf2", IP: "10.0.0.2", Age: -1}
	if len(n) != 1 || n[0] != want {
		t.Fatalf("got %+v", n)
	}
	if ParseEOSLLDP("not json") != nil {
		t.Fatal("garbage")
	}
}

func TestJunos(t *testing.T) {
	n := ParseJunosLLDP(`Local Interface    Parent Interface    Chassis Id          Port info          System Name
ge-0/0/0           -                   00:11:22:33:44:55   ge-0/0/1           sw2
ge-0/0/1           ae0                 00:11:22:33:44:56   Gi1/0/5            core-1
`)
	if len(n) != 2 || n[0].Chassis != "00:11:22:33:44:55" || n[0].Name != "sw2" || n[1].RemotePort != "Gi1/0/5" {
		t.Fatalf("got %+v", n)
	}
	p := ParseJunosTerse(`Interface               Admin Link Proto    Local                 Remote
ge-0/0/0                up    up
ge-0/0/0.0              up    up   eth-switch
ge-0/0/1                up    down
ge-0/0/2                down  down
ae0                     up    up
`)
	if len(p) != 4 || !p[0].Oper || p[1].Oper || p[2].Admin || p[3].Name != "ae0" {
		t.Fatalf("terse: %+v", p)
	}
	l := ParseJunosLACP(`Aggregated interface: ae0
    LACP state:       Role   Exp   Def  Dist  Col  Syn  Aggr  Timeout  Activity
      ge-0/0/0       Actor    No    No   Yes  Yes  Yes   Yes     Fast    Active
      ge-0/0/0     Partner    No    No   Yes  Yes  Yes   Yes     Fast    Active
      ge-0/0/1       Actor    No    No    No   No   No   Yes     Fast    Active
`)
	if len(l) != 1 || !reflect.DeepEqual(l[0].Members, []string{"ge-0/0/0", "ge-0/0/1"}) || !reflect.DeepEqual(l[0].Active, []string{"ge-0/0/0"}) {
		t.Fatalf("lacp: %+v", l)
	}
	s := ParseJunosSTP(`Interface    Port ID    Designated     Designated         Port    State  Role
ge-0/0/0.0   128:513    128:513   8192.0019e2503ac0       20000  FWD    DESG
ge-0/0/1.0   128:514    128:514   8192.0019e2503ac0       20000  BLK    ALT
`)
	if s["ge-0/0/0"] != "forwarding" || s["ge-0/0/1"] != "blocked" {
		t.Fatalf("stp: %v", s)
	}
}
