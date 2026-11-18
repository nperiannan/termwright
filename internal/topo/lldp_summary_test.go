package topo

import (
	"os"
	"testing"
	"time"
)

func TestVOSSLLDPSummary(t *testing.T) {
	b, err := os.ReadFile("testdata/voss/lldp_summary.txt")
	if err != nil {
		t.Fatal(err)
	}
	ns := ParseVOSSLLDP(string(b))
	if len(ns) != 6 {
		t.Fatalf("want 6 neighbours, got %d: %+v", len(ns), ns)
	}
	n := ns[0]
	if n.LocalPort != "1/31" || n.RemotePort != "1/33" || n.IP != "10.127.35.169" || n.Chassis != NormMAC("40:e3:17:cb:b4:00") || n.Name != "5420F-16MW-32P-4XE-Fabr~" {
		t.Fatalf("row 1: %+v", n)
	}
	b2, _ := os.ReadFile("testdata/voss/lldp_summary_doc.txt")
	d2 := ParseVOSSLLDP(string(b2))
	if len(d2) != 4 || d2[0].Name != "VSP-4450GSX-~" || d2[0].IP != "" || d2[2].Name != "" || d2[2].IP != "192.0.2.94" || d2[3].Proto != CDP || d2[3].Chassis != "" || d2[3].RemotePort != "FastEthernet2/0/5" || d2[3].Name != "cisco3750.Tr~" {
		t.Fatalf("doc format: %+v", d2)
	}
	b3, _ := os.ReadFile("testdata/voss/lldp_summary_lab.txt")
	d3 := ParseVOSSLLDP(string(b3))
	if len(d3) != 5 || d3[0].IP != "" || d3[0].Chassis != NormMAC("ac:ed:32:f5:af:00") || d3[0].Name != "5520-24t-3" || d3[4].RemotePort != "4" {
		t.Fatalf("lab format: %+v", d3)
	}
	// two switches with the same (default) name: the name alone does not make a neighbour "itself"
	twin := Device{Session: "lab/c", Name: "core", SysName: "5420F-16MW-32P-4XE-FabricEngine", OS: "voss", Chassis: "11:22:33:44:55:66", IP: "10.0.0.9",
		Neighbors: []Neighbor{{Proto: LLDP, LocalPort: "1/1", RemotePort: "1/2", Name: "5420F-16MW-32P-4XE-Fabr~", Age: -1}}}
	mt := Build([]Device{twin}, nil, time.Now())
	if len(mt.Links) != 1 || len(mt.Nodes) != 2 {
		t.Fatalf("same-name neighbour should be its own (ghost) device: %+v", mt)
	}
	// cut-short name finds the session; the switch cabled to itself gets a note, no link
	dev := Device{Session: "lab/a", Name: "5420F-16MW-32P-4XE-FabricEngine", OS: "voss", Chassis: "40:e3:17:cb:b4:00", IP: "10.127.35.169", Neighbors: ns}
	other := Device{Session: "lab/b", Name: "x", OS: "voss", Neighbors: []Neighbor{{Proto: LLDP, LocalPort: "1/1", RemotePort: "1/2", Name: "5420F-16MW-32P-4XE-Fabr~", Age: -1}}}
	m := Build([]Device{dev, other}, nil, time.Now())
	if len(m.Links) != 1 {
		t.Fatalf("want 1 link (b to a), got %+v", m.Links)
	}
	notes := 0
	for _, nd := range m.Nodes {
		if nd.Session == "lab/a" {
			notes = len(nd.Notes)
		}
	}
	if notes != 3 {
		t.Fatalf("want 3 self-cable notes, got %d", notes)
	}
}
