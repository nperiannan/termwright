package topo

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)

func linkBy(m Map, a, b string) *Link {
	for i := range m.Links {
		l := &m.Links[i]
		if l.A == a && l.B == b || l.A == b && l.B == a {
			return l
		}
	}
	return nil
}

func nodeBy(m Map, id string) *Node {
	for i := range m.Nodes {
		if m.Nodes[i].ID == id {
			return &m.Nodes[i]
		}
	}
	return nil
}

func up(ports ...string) []Port {
	var p []Port
	for _, n := range ports {
		p = append(p, Port{Name: n, Admin: true, Oper: true})
	}
	return p
}

// the lab: 5520-24t-3 (EXOS, from its real output) and 5520-24t-5 (VOSS, from its real output), cabled
// t-3 port 2 to t-5 1/1; t-3 ports 1, 3, 4 go to 5520-24t-2, which has no session here.
func labDevices(t *testing.T) []Device {
	exos := Device{Session: "lab/t-3", Name: "5520-24t-3", OS: "exos", IP: "10.234.179.148",
		Neighbors: ParseEXOSLLDP(fx(t, "exos/lldp_neighbors_detail.txt")), Ports: ParseEXOSPorts(fx(t, "exos/ports_info.txt")), LAGs: ParseEXOSSharing(fx(t, "exos/sharing.txt"))}
	for p, s := range ParseEXOSSTP(fx(t, "exos/stpd_ports.txt")) {
		if pp := portOf(&exos, p); pp != nil {
			pp.STP = s
		}
	}
	exos.Ports = append(exos.Ports, Port{Name: "2", Admin: true, Oper: true})
	voss := Device{Session: "lab/t-5", Name: "5520-24T-FabricEngine", OS: "voss", IP: "10.234.179.150",
		Neighbors: ParseVOSSLLDP(fx(t, "voss/lldp_neighbor.txt")), Ports: ParseVOSSInterfaces(fx(t, "voss/interfaces.txt"))}
	for i := range voss.Ports { // the fixture was read before the lab's 1/3 came up
		voss.Ports[i].Admin, voss.Ports[i].Oper = true, true
	}
	return []Device{exos, voss}
}

func TestBuildLab(t *testing.T) {
	m := Build(labDevices(t), nil, t0)
	if len(m.Nodes) != 3 || len(m.Links) != 5 {
		t.Fatalf("want 3 nodes and 5 links, got %d and %d: %+v", len(m.Nodes), len(m.Links), m.Links)
	}
	t3, t5, t2 := "n:5520-24t-3", "n:5520-24t-fabricengine", "n:5520-24t-2"
	if g := nodeBy(m, t2); g == nil || !g.Ghost {
		t.Fatalf("t-2 should be a ghost: %+v", g)
	}
	// t-3 port 2 and VOSS 1/1 see each other: one link, both ends
	l := linkBy(m, t3, t5)
	if l == nil || l.Ends != 2 || l.State != "up" {
		t.Fatalf("t-3 to VOSS: %+v", l)
	}
	// t-3 port 1 is blocked by spanning tree; t-3 ports 3 and 4 are one static LAG (group 4)
	var blocked, lag, vossToT2 int
	for _, k := range m.Links {
		switch {
		case k.A == t3 && k.B == t2:
			if k.State == "blocked" && k.APort == "1" {
				blocked++
			}
			if k.LagA != nil && k.LagA.Mode == "static" && k.LagA.Name == "4" && (k.APort == "3" || k.APort == "4") {
				lag++
			}
		case k.A == t5 && k.B == t2:
			vossToT2++
		}
	}
	if blocked != 1 || lag != 2 || vossToT2 != 1 {
		t.Fatalf("blocked=%d lag=%d: %+v", blocked, lag, m.Links)
	}
	// VOSS port 1/3 hears t-2 port 3, 1/1 hears t-3
	var v13 bool
	for _, k := range m.Links {
		if k.A == t2 && k.B == t5 && k.BPort == "1/3" && k.APort == "3" || k.A == t5 && k.B == t2 && k.APort == "1/3" && k.BPort == "3" {
			v13 = true
		}
	}
	if !v13 {
		t.Fatalf("VOSS 1/3 to t-2 port 3 missing: %+v", m.Links)
	}
}

func dev(name string, ports []Port, nb ...Neighbor) Device {
	return Device{Session: "s/" + name, Name: name, Ports: ports, Neighbors: nb}
}

func TestFreshestProtocolWins(t *testing.T) {
	// port 1 hears EDP "old-sw" 5 s ago and LLDP "new-sw" 100 s ago: the EDP is the newer
	d := dev("a", up("1"),
		Neighbor{Proto: LLDP, LocalPort: "1", Name: "new-sw", Chassis: "00:00:00:00:00:02", RemotePort: "9", Age: 100},
		Neighbor{Proto: EDP, LocalPort: "1", Name: "old-sw", Chassis: "00:00:00:00:00:01", RemotePort: "3", Age: 5})
	m := Build([]Device{d}, nil, t0)
	if len(m.Links) != 1 || m.Links[0].B != "n:old-sw" {
		t.Fatalf("got %+v", m.Links)
	}
	// the same neighbour by two protocols: one link that lists both
	d = dev("a", up("1"),
		Neighbor{Proto: LLDP, LocalPort: "1", Name: "sw", Chassis: "00:00:00:00:00:02", RemotePort: "3", Age: 20},
		Neighbor{Proto: EDP, LocalPort: "1", Name: "sw", Chassis: "00:00:00:00:00:02", RemotePort: "3", Age: 10})
	m = Build([]Device{d}, nil, t0)
	if len(m.Links) != 1 || len(m.Links[0].Seen) != 2 {
		t.Fatalf("got %+v", m.Links)
	}
}

func TestDownPortIgnoresOldNeighbour(t *testing.T) {
	d := dev("a", []Port{{Name: "1", Admin: true, Oper: false}}, Neighbor{Proto: LLDP, LocalPort: "1", Name: "b", Chassis: "00:00:00:00:00:02", RemotePort: "1", Age: 60})
	if m := Build([]Device{d}, nil, t0); len(m.Links) != 0 {
		t.Fatalf("a port with no link has no neighbour: %+v", m.Links)
	}
}

func TestStaleClaimOfTheOtherEnd(t *testing.T) {
	// a says "port 1 is cabled to r port 3" (heard 100 s ago); r says its port 3 is cabled to c (heard 10 s ago)
	a := dev("a", up("1"), Neighbor{Proto: LLDP, LocalPort: "1", Name: "r", RemotePort: "3", Age: 100})
	r := dev("r", up("3"), Neighbor{Proto: LLDP, LocalPort: "3", Name: "c", RemotePort: "7", Age: 10})
	m := Build([]Device{a, r}, nil, t0)
	if linkBy(m, "n:a", "n:r") != nil {
		t.Fatalf("the old claim should be dropped: %+v", m.Links)
	}
	if linkBy(m, "n:r", "n:c") == nil {
		t.Fatalf("the fresh link is missing: %+v", m.Links)
	}
	// and the other way round: a's entry is the fresher one, r still holds an old entry
	a.Neighbors[0].Age = 5
	r.Neighbors[0].Age = 120
	m = Build([]Device{a, r}, nil, t0)
	if linkBy(m, "n:a", "n:r") == nil || linkBy(m, "n:r", "n:c") != nil {
		t.Fatalf("a's fresh claim should win: %+v", m.Links)
	}
}

func TestMovedLinkIsNotDrawnDown(t *testing.T) {
	// before: acc port 1 to dist1 port 4. now: acc port 1 hears dist2 port 6, and dist1 port 4 is down
	acc0 := dev("acc", up("1"), Neighbor{Proto: LLDP, LocalPort: "1", Name: "dist1", RemotePort: "4", Age: 3})
	d1 := dev("dist1", up("4"), Neighbor{Proto: LLDP, LocalPort: "4", Name: "acc", RemotePort: "1", Age: 3})
	d2 := dev("dist2", up("6"))
	before := Build([]Device{acc0, d1, d2}, nil, t0)
	if linkBy(before, "n:acc", "n:dist1") == nil {
		t.Fatal("setup")
	}
	acc1 := dev("acc", up("1"), Neighbor{Proto: LLDP, LocalPort: "1", Name: "dist2", RemotePort: "6", Age: 2})
	d1b := dev("dist1", []Port{{Name: "4", Admin: true, Oper: false}})
	d2b := dev("dist2", up("6"), Neighbor{Proto: LLDP, LocalPort: "6", Name: "acc", RemotePort: "1", Age: 2})
	after := Build([]Device{acc1, d1b, d2b}, &before, t0.Add(time.Minute))
	if linkBy(after, "n:acc", "n:dist1") != nil {
		t.Fatalf("the old link must go, not be drawn down: %+v", after.Links)
	}
	if l := linkBy(after, "n:acc", "n:dist2"); l == nil || l.State != "up" || l.Ends != 2 {
		t.Fatalf("new link: %+v", after.Links)
	}
	ch := Diff(before, after, t0)
	var moved bool
	for _, c := range ch {
		moved = moved || c.Kind == "moved" && strings.Contains(c.Text, "acc 1") && strings.Contains(c.Text, "dist2 6")
		if c.Kind == "link-down" {
			t.Fatalf("a moved link is not a link-down: %+v", ch)
		}
	}
	if !moved {
		t.Fatalf("no moved change: %+v", ch)
	}
}

func TestDownLinkIsKeptAndUnreadableDeviceKeepsLinks(t *testing.T) {
	a := dev("a", up("1"), Neighbor{Proto: LLDP, LocalPort: "1", Name: "b", RemotePort: "2", Age: 1})
	b := dev("b", up("2"), Neighbor{Proto: LLDP, LocalPort: "2", Name: "a", RemotePort: "1", Age: 1})
	before := Build([]Device{a, b}, nil, t0)
	// the cable is pulled: both ports down, nothing replaces it
	a2 := dev("a", []Port{{Name: "1", Admin: true}})
	b2 := dev("b", []Port{{Name: "2", Admin: true}})
	after := Build([]Device{a2, b2}, &before, t0)
	if l := linkBy(after, "n:a", "n:b"); l == nil || l.State != "down" {
		t.Fatalf("should stay as a down link: %+v", after.Links)
	}
	if ch := Diff(before, after, t0); len(ch) != 1 || ch[0].Kind != "link-down" {
		t.Fatalf("changes: %+v", ch)
	}
	// b cannot be read: a's data still says the link exists; if a no longer lists it, it stays as it was
	b3 := Device{Session: "s/b", Name: "b", Err: "timed out"}
	a3 := dev("a", up("1"))
	after = Build([]Device{a3, b3}, &before, t0)
	if l := linkBy(after, "n:a", "n:b"); l == nil || l.State != "up" {
		t.Fatalf("an unreadable device must not drop its links: %+v", after.Links)
	}
	if nodeBy(after, "n:b") == nil || len(nodeBy(after, "n:b").Notes) == 0 {
		t.Fatal("the error should show on the node")
	}
}

func TestLAGWarnings(t *testing.T) {
	mk := func(ma, mb string) Map {
		a := dev("a", up("1", "2"), Neighbor{Proto: LLDP, LocalPort: "1", Name: "b", RemotePort: "1", Age: 1}, Neighbor{Proto: LLDP, LocalPort: "2", Name: "b", RemotePort: "2", Age: 1})
		b := dev("b", up("1", "2"), Neighbor{Proto: LLDP, LocalPort: "1", Name: "a", RemotePort: "1", Age: 1}, Neighbor{Proto: LLDP, LocalPort: "2", Name: "a", RemotePort: "2", Age: 1})
		if ma != "" {
			a.LAGs = []LAG{{Name: "1", Mode: ma, Members: []string{"1", "2"}}}
		}
		if mb != "" {
			b.LAGs = []LAG{{Name: "1", Mode: mb, Members: []string{"1", "2"}}}
		}
		return Build([]Device{a, b}, nil, t0)
	}
	if m := mk("lacp", "lacp"); len(m.Links) != 2 || m.Links[0].Warn != "" {
		t.Fatalf("both lacp: %+v", m.Links)
	}
	if m := mk("lacp", "static"); m.Links[0].Warn != "static on one side, LACP on the other" {
		t.Fatalf("mixed: %+v", m.Links[0])
	}
	if m := mk("lacp", ""); m.Links[0].Warn != "the LAG is set on one side only" {
		t.Fatalf("one side: %+v", m.Links[0])
	}
}

func TestMatchByMACOnly(t *testing.T) {
	// a hears "?" with only a MAC; b is a session device that learned that MAC from c's view of it
	a := dev("a", up("1"), Neighbor{Proto: LLDP, LocalPort: "1", Chassis: "aa:00:00:00:00:02", RemotePort: "5", Age: 1})
	b := Device{Session: "s/b", Name: "b", Chassis: "AA:00:00:00:00:02", Ports: up("5")}
	m := Build([]Device{a, b}, nil, t0)
	if l := linkBy(m, "n:a", "n:b"); l == nil || nodeBy(m, "n:b").Ghost {
		t.Fatalf("should join b by its MAC: %+v %+v", m.Links, m.Nodes)
	}
}

func TestCompare(t *testing.T) {
	a := dev("a", up("1"), Neighbor{Proto: LLDP, LocalPort: "1", Name: "b", RemotePort: "1", Age: 1})
	planned := Build([]Device{a}, nil, t0)
	c := dev("a", up("2"), Neighbor{Proto: LLDP, LocalPort: "2", Name: "b", RemotePort: "1", Age: 1})
	found := Build([]Device{c}, nil, t0)
	miss, extra := Compare(planned, found)
	if len(miss) != 1 || len(extra) != 1 {
		t.Fatalf("missing %+v unexpected %+v", miss, extra)
	}
}
