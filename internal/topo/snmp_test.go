package topo

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// SYNTHETIC: tables written to the layout of IF-MIB, LLDP-MIB, BRIDGE-MIB and IEEE8023-LAG-MIB (the lab's
// switches have SNMP off, so this was not run against a device).
func snmpTable() map[string][]VB {
	s := func(oid, v string) VB { return VB{OID: oid, Bytes: []byte(v)} }
	i := func(oid string, v int64) VB { return VB{OID: oid, Int: v, IsInt: true} }
	return map[string][]VB{
		oidIfName:         {s(oidIfName+".1", "1"), s(oidIfName+".2", "2"), s(oidIfName+".9", "lag1")},
		oidIfAdmin:        {i(oidIfAdmin+".1", 1), i(oidIfAdmin+".2", 1), i(oidIfAdmin+".9", 1)},
		oidIfOper:         {i(oidIfOper+".1", 1), i(oidIfOper+".2", 2), i(oidIfOper+".9", 1)},
		oidLldpLoc + ".2": {i(oidLldpLoc+".2.1", 5), i(oidLldpLoc+".2.2", 5)},
		oidLldpLoc + ".3": {s(oidLldpLoc+".3.1", "1"), s(oidLldpLoc+".3.2", "2")},
		oidLldpRem: {
			i(oidLldpRem+".4.0.1.1", 4), i(oidLldpRem+".4.0.2.1", 4),
			{OID: oidLldpRem + ".5.0.1.1", Bytes: []byte{0xac, 0xed, 0x32, 0x94, 0x2c, 0x00}}, {OID: oidLldpRem + ".5.0.2.1", Bytes: []byte{0xac, 0xed, 0x32, 0x94, 0x2c, 0x01}},
			i(oidLldpRem+".6.0.1.1", 5), i(oidLldpRem+".6.0.2.1", 5),
			s(oidLldpRem+".7.0.1.1", "7"), s(oidLldpRem+".7.0.2.1", "8"),
			s(oidLldpRem+".9.0.1.1", "core-2"), s(oidLldpRem+".9.0.2.1", "core-3"),
		},
		oidStpState:  {i(oidStpState+".1", 5), i(oidStpState+".2", 2)},
		oidBasePort:  {i(oidBasePort+".1", 1), i(oidBasePort+".2", 2)},
		oidLagAttach: {i(oidLagAttach+".1", 9), i(oidLagAttach+".2", 0)},
	}
}

func walkOf(tab map[string][]VB) Walker {
	return func(_ context.Context, root string) ([]VB, error) {
		if v, ok := tab[root]; ok {
			return v, nil
		}
		return nil, nil
	}
}

func TestCollectSNMP(t *testing.T) {
	d := CollectSNMP(context.Background(), Target{Session: "s", Name: "sw", OS: "exos"}, walkOf(snmpTable()), t0)
	if d.Err != "" || len(d.Ports) != 3 || len(d.Neighbors) != 2 {
		t.Fatalf("%+v", d)
	}
	n := d.Neighbors[0]
	if n.LocalPort != "1" || n.Chassis != "ac:ed:32:94:2c:00" || n.RemotePort != "7" || n.Name != "core-2" || n.Age != -1 {
		t.Fatalf("neighbour: %+v", n)
	}
	if p := portOf(&d, "1"); !p.Oper || p.STP != "forwarding" {
		t.Fatalf("port 1: %+v", p)
	}
	if p := portOf(&d, "2"); p.Oper || p.STP != "blocked" {
		t.Fatalf("port 2: %+v", p)
	}
	if len(d.LAGs) != 1 || d.LAGs[0].Name != "lag1" || len(d.LAGs[0].Members) != 1 || d.LAGs[0].Members[0] != "1" {
		t.Fatalf("lags: %+v", d.LAGs)
	}
	// port 2 has no link: its neighbour is old, and the map ignores it
	m := Build([]Device{d}, nil, t0)
	if len(m.Links) != 1 || m.Links[0].B != "n:core-2" {
		t.Fatalf("links: %+v", m.Links)
	}
}

func TestCollectSNMPFailures(t *testing.T) {
	d := CollectSNMP(context.Background(), Target{Name: "x"}, func(context.Context, string) ([]VB, error) { return nil, errors.New("i/o timeout") }, t0)
	if !strings.Contains(d.Err, "i/o timeout") || !strings.HasPrefix(d.Err, "SNMP: ") {
		t.Fatalf("got %q", d.Err)
	}
	d = CollectSNMP(context.Background(), Target{Name: "x"}, walkOf(nil), t0)
	if !strings.Contains(d.Err, "no interfaces") {
		t.Fatalf("empty answer: %q", d.Err)
	}
}
