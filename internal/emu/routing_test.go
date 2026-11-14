package emu

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRoutingPlanAndScript(t *testing.T) {
	r := Routing{Proto: "ospf", Port: 1, Routes: 1000, Prefix: "100.64.0.0/24"}
	p, step, super, err := r.Plan()
	if err != nil || p.String() != "100.64.0.0/24" || step != 256 || super.String() != "100.64.0.0/14" {
		t.Fatalf("%v %v %d %v", err, p, step, super)
	}
	for _, bad := range []Routing{{Proto: "rip", Port: 1, Routes: 1, Prefix: "10.0.0.0/24"}, {Proto: "ospf", Port: 1, Routes: 10001, Prefix: "10.0.0.0/24"}, {Proto: "ospf", Port: 1, Routes: 2, Prefix: "255.255.255.0/24"}, {Proto: "bgp", Port: 1, Routes: 2, Prefix: "10.0.0.0/24"}} {
		if _, _, _, err := bad.Plan(); err == nil {
			t.Errorf("%+v passed", bad)
		}
	}
	s, err := RoutingScript(r, "pc-1", "eth1", "10.20.0.11/24")
	if err != nil || !strings.Contains(s, "ip ospf area 0.0.0.0") || !strings.Contains(s, "ip route replace local 100.64.0.0/14 dev lo") || !strings.Contains(s, "redistribute kernel") {
		t.Fatalf("%v\n%s", err, s)
	}
	// the prefixes it makes: run its awk
	if _, err := exec.LookPath("awk"); err == nil {
		i := strings.Index(s, "awk '")
		j := strings.Index(s[i:], " > /tmp/tw-routes")
		out, err := exec.Command("sh", "-c", s[i:i+j]).Output()
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if err != nil || len(lines) != 1000 || lines[0] != "route replace blackhole 100.64.0.0/24 proto 99" || lines[999] != "route replace blackhole 100.67.231.0/24 proto 99" {
			t.Errorf("awk: %v %q … %q", err, lines[0], lines[len(lines)-1])
		}
	}
	b := Routing{Proto: "bgp", Port: 1, Routes: 3, Prefix: "198.18.0.0/16", Peer: "10.20.0.1", PeerAS: 65100}
	s, err = RoutingScript(b, "pc-2", "eth1", "10.20.0.12/24")
	if err != nil || !strings.Contains(s, "router bgp 65001") || !strings.Contains(s, "neighbor 10.20.0.1 remote-as 65100") || !strings.Contains(s, "local 198.16.0.0/13") {
		t.Fatalf("%v\n%s", err, s)
	}
	st, up, n := RoutingState(r, "Neighbor ID     Pri State           Up Time         Dead Time Address         Interface                        RXmtL RqstL DBsmL\n10.0.0.1          1 Full/DR         1m02s             36.123s 10.20.0.1       eth1:10.20.0.11                      0     0     0\ntw-prefixes 1000")
	if st != "Full/DR" || !up || n != 1000 {
		t.Errorf("ospf state %q %v %d", st, up, n)
	}
	st, up, _ = RoutingState(b, "Neighbor        V         AS   MsgRcvd   MsgSent   TblVer  InQ OutQ  Up/Down State/PfxRcd   PfxSnt Desc\n10.20.0.1       4      65100        12        15        0    0    0 00:05:01            4        3 N/A\ntw-prefixes 3")
	if st != "Established" || !up {
		t.Errorf("bgp state %q %v", st, up)
	}
	st, up, _ = RoutingState(b, "10.20.0.1       4      65100         0         0        0    0    0    never       Active        0 N/A")
	if st != "Active" || up {
		t.Errorf("bgp down %q %v", st, up)
	}
}
