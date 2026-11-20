package snmp

import (
	"strings"
	"testing"
)

func TestStandardMIBs(t *testing.T) {
	m := Standard()
	mods := strings.Join(m.Modules(), " ")
	for _, want := range []string{"SNMPv2-MIB", "IF-MIB", "IP-MIB", "BRIDGE-MIB", "Q-BRIDGE-MIB", "ENTITY-MIB", "HOST-RESOURCES-MIB", "RSTP-MIB"} {
		if !strings.Contains(mods, want) {
			t.Fatalf("%s not loaded: %s", want, mods)
		}
	}
	for name, oid := range map[string]string{"sysDescr": "1.3.6.1.2.1.1.1", "ifDescr": "1.3.6.1.2.1.2.2.1.2", "ifHCInOctets": "1.3.6.1.2.1.31.1.1.1.6",
		"dot1qVlanStaticName": "1.3.6.1.2.1.17.7.1.4.3.1.1", "entPhysicalDescr": "1.3.6.1.2.1.47.1.1.1.1.2", "dot1dStpPortState": "1.3.6.1.2.1.17.2.15.1.3",
		"IF-MIB::ifOperStatus": "1.3.6.1.2.1.2.2.1.8", "hrSystemUptime": "1.3.6.1.2.1.25.1.1"} {
		got, err := m.Resolve(name)
		if err != nil || got != oid {
			t.Fatalf("%s: %s %v", name, got, err)
		}
	}
	n, inst := m.Lookup("1.3.6.1.2.1.2.2.1.8.12")
	if n == nil || n.Name != "ifOperStatus" || inst != "12" || n.Kind != "column" || n.Enums[1] != "up" || n.Base != "Integer" || n.Module != "IF-MIB" {
		t.Fatalf("%+v %s", n, inst)
	}
	if got := m.Describe(Varbind{OID: "1.3.6.1.2.1.2.2.1.8.12", Type: Integer, Int: 2}); got != "down (2)" {
		t.Fatal(got)
	}
	if got := m.Describe(Varbind{OID: "1.3.6.1.2.1.2.2.1.6.1", Type: OctetString, Bytes: []byte{0x40, 0xe3, 0x17, 0xcb, 0xb4, 1}}); got != "40:e3:17:cb:b4:01" {
		t.Fatal(got)
	}
	if got := m.Describe(Varbind{OID: "1.3.6.1.2.1.1.3.0", Type: TimeTicks, Uint: 27557500}); got != "3 d 04:32:55" {
		t.Fatal(got)
	}
	tbl := m.Node("1.3.6.1.2.1.2.2")
	row := m.Node("1.3.6.1.2.1.2.2.1")
	if tbl.Kind != "table" || row.Kind != "row" || len(row.Index) != 1 || row.Index[0] != "ifIndex" || !strings.Contains(tbl.Description, "list of interface entries") {
		t.Fatalf("%+v %+v", tbl, row)
	}
	if n := m.Node("1.3.6.1.2.1.31.1.1.1.6"); n == nil || n.Base != "Counter64" {
		t.Fatalf("ifHCInOctets: %+v", n)
	}
	if n := m.Node("1.3.6.1.6.3.1.1.5.4"); n == nil || n.Name != "linkUp" || n.Kind != "notification" || len(n.Objects) != 3 {
		t.Fatalf("linkUp: %+v", n)
	}
	kids := m.Children("1.3.6.1.2.1")
	if len(kids) < 10 || kids[0].Name != "system" {
		t.Fatalf("children of mib-2: %d %v", len(kids), kids[0].Name)
	}
	if s := m.Search("ifoper", 5); len(s) == 0 || s[0].Name != "ifOperStatus" {
		t.Fatalf("search: %+v", s)
	}
	unresolved := len(m.pending)
	if unresolved > 0 {
		var names []string
		for _, p := range m.pending {
			names = append(names, p.Module+"::"+p.Name)
		}
		t.Logf("%d objects without a parent: %v", unresolved, names[:min(10, len(names))])
	}
	t.Logf("%d objects in %d modules", len(m.byOID), len(m.Modules()))
}

func TestLoadPrivateMIB(t *testing.T) {
	m := Standard().Clone()
	names, err := m.Load(`EXTREME-TEST-MIB DEFINITIONS ::= BEGIN
IMPORTS MODULE-IDENTITY, OBJECT-TYPE, Integer32 FROM SNMPv2-SMI DisplayString FROM SNMPv2-TC;
extremeTest MODULE-IDENTITY LAST-UPDATED "202611200000Z" ORGANIZATION "x" CONTACT-INFO "x" DESCRIPTION "Test." ::= { extremenetworks 99 }
-- a comment -- testCpu OBJECT-TYPE SYNTAX Integer32 (0..100) UNITS "percent" MAX-ACCESS read-only STATUS current
  DESCRIPTION "CPU use." ::= { extremeTest 1 }
testMode OBJECT-TYPE SYNTAX INTEGER { fast(1), slow(2) } MAX-ACCESS read-write STATUS current DESCRIPTION "Mode." ::= { extremeTest 2 }
END`)
	if err != nil || len(names) != 1 {
		t.Fatal(names, err)
	}
	if o, _ := m.Resolve("testCpu.0"); o != "1.3.6.1.4.1.1916.99.1.0" {
		t.Fatal(o)
	}
	if got := m.Describe(Varbind{OID: "1.3.6.1.4.1.1916.99.1.0", Type: Integer, Int: 42}); got != "42 percent" {
		t.Fatal(got)
	}
	if got := m.Describe(Varbind{OID: "1.3.6.1.4.1.1916.99.2.0", Type: Integer, Int: 2}); got != "slow (2)" {
		t.Fatal(got)
	}
	if _, err := Standard().Resolve("testCpu"); err == nil {
		t.Fatal("the shared MIB must not change")
	}
}

// Imports, dependents and a missing module are known; the root of a module is found.
func TestMIBImportsAndRoot(t *testing.T) {
	m := Standard().Clone()
	if _, err := m.Load(`LM-SENSORS-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, MODULE-IDENTITY FROM SNMPv2-SMI ucdExperimental FROM UCD-SNMP-MIB;
lmSensorsMIB MODULE-IDENTITY LAST-UPDATED "200011050000Z" ORGANIZATION "x" CONTACT-INFO "x" DESCRIPTION "x" ::= { lmSensors 1 }
lmSensors OBJECT IDENTIFIER ::= { ucdExperimental 16 }
END`); err != nil {
		t.Fatal(err)
	}
	if im := m.Imports("LM-SENSORS-MIB"); len(im) != 2 || im[1] != "UCD-SNMP-MIB" || m.Loaded("UCD-SNMP-MIB") {
		t.Fatal(im)
	}
	if u := m.Unplaced()["LM-SENSORS-MIB"]; len(u) == 0 {
		t.Fatal("unplaced expected")
	}
	if d := m.Dependents("IF-MIB"); len(d) == 0 {
		t.Fatal("IF-MIB has dependents")
	}
	if r := m.Root("IF-MIB"); r != "1.3.6.1.2.1.31" {
		t.Fatal(r)
	}
}
