package snmp

import "testing"

func TestWalkFile(t *testing.T) {
	src := `.1.3.6.1.2.1.1.5.0 = STRING: "sw1"
.1.3.6.1.2.1.1.3.0 = Timeticks: (12345) 0:02:03.45
.1.3.6.1.2.1.2.2.1.8.1 = INTEGER: up(1)
.1.3.6.1.2.1.2.2.1.10.1 = Counter32: 1000
.1.3.6.1.2.1.2.2.1.6.1 = Hex-STRING: 00 11 22 AA BB CC
.1.3.6.1.2.1.4.20.1.1.10.0.0.1 = IpAddress: 10.0.0.1
.1.3.6.1.2.1.1.2.0 = OID: .1.3.6.1.4.1.1916.2.1
.1.3.6.1.2.1.1.1.0 = STRING: "line one
line two"
junk line
`
	vs := ParseWalk(src)
	if len(vs) != 8 || vs[0].Value != "sw1" || vs[1].Uint != 12345 || vs[2].Int != 1 || vs[4].Bytes[5] != 0xcc || vs[7].Value != "line one\nline two" {
		t.Fatalf("%+v", vs)
	}
	back := ParseWalk(FormatWalk(vs))
	if len(back) != 8 || back[4].Bytes[0] != 0 || back[6].Value != "1.3.6.1.4.1.1916.2.1" {
		t.Fatalf("%+v", back)
	}
	a := NewAgent("public", []byte("x"))
	for _, v := range vs {
		a.Put(v)
	}
	if v, ok := a.next("1.3.6.1.2.1.1.5.0"); !ok || v.OID != "1.3.6.1.2.1.2.2.1.10.1" && v.OID == "" {
		t.Fatal(v)
	}
}
