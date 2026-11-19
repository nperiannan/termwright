package topo

import (
	"os"
	"reflect"
	"testing"
)

func fx(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEXOSLLDP(t *testing.T) {
	n := ParseEXOSLLDP(fx(t, "exos/lldp_neighbors_detail.txt"))
	if len(n) != 4 {
		t.Fatalf("got %d neighbours: %+v", len(n), n)
	}
	want := Neighbor{Proto: LLDP, LocalPort: "1", Chassis: "ac:ed:32:94:2c:00", RemotePort: "1", Name: "5520-24t-2", Age: 29}
	if n[0] != want {
		t.Fatalf("first = %+v, want %+v", n[0], want)
	}
	if n[1].LocalPort != "2" || n[1].RemotePort != "1/1" || n[1].IP != "10.234.179.150" || n[1].Caps != "Bridge, Router" || n[1].Name != "5520-24T-FabricEngine" {
		t.Fatalf("voss neighbour = %+v", n[1])
	}
	if n[3].LocalPort != "4" || n[3].RemotePort != "7" || n[3].Age != 22 {
		t.Fatalf("port 4 = %+v", n[3])
	}
}

func TestEXOSEDP(t *testing.T) {
	n := ParseEXOSEDP(fx(t, "exos/edp_ports.txt"))
	if len(n) != 3 {
		t.Fatalf("got %+v", n)
	}
	want := Neighbor{Proto: EDP, LocalPort: "3", Name: "5520-24t-2", Chassis: "ac:ed:32:94:2c:00", RemotePort: "1:6", Age: 26}
	if n[1] != want {
		t.Fatalf("got %+v want %+v", n[1], want)
	}
	// 1:6 on a device that is not stacked is its port 6
	if NormPort(n[1].RemotePort) != "6" {
		t.Fatal("slot 1 not dropped")
	}
}

func TestEXOSPorts(t *testing.T) {
	p := ParseEXOSPorts(fx(t, "exos/ports_info.txt"))
	if len(p) != 3 || p[0].Name != "1" || !p[0].Oper || !p[0].Admin || p[2].Name != "4" {
		t.Fatalf("got %+v", p)
	}
	d := ParseEXOSPorts("5   D-------e--fMB---x- ready  - / -/ -  1 1 1 1 9216 none\n6   Em------e--fMB---x- ready - / -/ - 1 1 1 1 9216 none\n")
	if len(d) != 2 || d[0].Admin || d[0].Oper || !d[1].Admin || d[1].Oper {
		t.Fatalf("disabled/ready: %+v", d)
	}
}

func TestEXOSSharing(t *testing.T) {
	l := ParseEXOSSharing(fx(t, "exos/sharing.txt"))
	want := []LAG{{Name: "4", Mode: "static", Members: []string{"3", "4"}, Active: []string{"3", "4"}}}
	if !reflect.DeepEqual(l, want) {
		t.Fatalf("got %+v", l)
	}
	if got := ParseEXOSSharing(fx(t, "exos/sharing_none.txt")); len(got) != 0 {
		t.Fatalf("none: %+v", got)
	}
	// LACP, one member not active
	got := ParseEXOSSharing("====\n     2      2     LACP       1    L3        A     2         Y     A       1\n                                  L3              5         Y     R       1\n====\n")
	if len(got) != 1 || got[0].Mode != "lacp" || !reflect.DeepEqual(got[0].Members, []string{"2", "5"}) || !reflect.DeepEqual(got[0].Active, []string{"2"}) {
		t.Fatalf("lacp: %+v", got)
	}
}

func TestEXOSSTP(t *testing.T) {
	m := ParseEXOSSTP(fx(t, "exos/stpd_ports.txt"))
	if m["1"] != "blocked" || m["2"] != "forwarding" || m["4"] != "forwarding" || m["5"] != "disabled" || len(m) != 4 {
		t.Fatalf("got %v", m)
	}
}

func TestVOSSLLDP(t *testing.T) {
	n := ParseVOSSLLDP(fx(t, "voss/lldp_neighbor.txt"))
	if len(n) != 2 {
		t.Fatalf("got %+v", n)
	}
	want := Neighbor{Proto: LLDP, LocalPort: "1/1", Chassis: "ac:ed:32:f5:af:00", RemotePort: "2", Name: "5520-24t-3", Age: -1}
	if n[0] != want {
		t.Fatalf("got %+v", n[0])
	}
	if n[1].IP != "10.234.179.147" || n[1].LocalPort != "1/3" || n[1].Caps != "Bridge / Bridge" {
		t.Fatalf("second: %+v", n[1])
	}
}

func TestVOSSInterfacesAndSTP(t *testing.T) {
	p := ParseVOSSInterfaces(fx(t, "voss/interfaces.txt"))
	if len(p) != 2 || !p[0].Oper || p[1].Oper || p[1].Admin || p[1].Name != "1/3" {
		t.Fatalf("got %+v", p)
	}
	m := ParseVOSSSTP(fx(t, "voss/stp_port_role.txt"))
	if m["1/1"] != "disabled" || m["1/3"] != "forwarding" || m["1/5"] != "blocked" || len(m) != 4 {
		t.Fatalf("got %v", m)
	}
}

func TestVOSSMLT(t *testing.T) {
	l := ParseVOSSMLT(fx(t, "voss/mlt_empty.txt") + "\n1      6144    null         enable    down\n")
	if len(l) != 1 || l[0].Name != "1" || l[0].Mode != "lacp" || len(l[0].Members) != 0 {
		t.Fatalf("empty LACP MLT: %+v", l)
	}
	// SYNTHETIC: the members column of a populated MLT could not be captured in the lab (its ports are
	// auto-sense); this is the layout the column is documented to have.
	l = ParseVOSSMLT("2     6145    MLT-2                 access  norm   norm   1/3-1/4,1/6                              CONFIG       \n")
	if len(l) != 1 || l[0].Mode != "static" || !reflect.DeepEqual(l[0].Members, []string{"1/3", "1/4", "1/6"}) {
		t.Fatalf("static MLT: %+v", l)
	}
}

func TestNormalisers(t *testing.T) {
	for in, want := range map[string]string{"AC:ED:32:94:2C:00": "ac:ed:32:94:2c:00", "acde.4800.0001": "ac:de:48:00:00:01", "AC-DE-48-00-00-01": "ac:de:48:00:00:01", "ac:de": "", "hello": ""} {
		if got := NormMAC(in); got != want {
			t.Errorf("NormMAC(%q) = %q want %q", in, got, want)
		}
	}
	same := [][2]string{{"1:5", "5"}, {"GigabitEthernet1/0/48", "Gi1/0/48"}, {"Ethernet1", "Et1"}, {`"1/1"`, "1/1"}, {"Eth1/1", "Et1/1"}, {"TenGigabitEthernet1/1/1", "Te1/1/1"}, {"Port-channel3", "Po3"}}
	for _, s := range same {
		if NormPort(s[0]) != NormPort(s[1]) {
			t.Errorf("%q != %q (%q vs %q)", s[0], s[1], NormPort(s[0]), NormPort(s[1]))
		}
	}
	if NormPort("2:5") == NormPort("5") {
		t.Error("slot 2 port 5 must differ from port 5")
	}
	if NormName("Core-1.example.net") != "core-1" || NormName("10.1.2.3") != "10.1.2.3" {
		t.Error("NormName")
	}
}
