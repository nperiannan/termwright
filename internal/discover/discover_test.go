package discover

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestParseTargets(t *testing.T) {
	for _, c := range []struct {
		spec        string
		n           int
		first, last string
	}{
		{"192.0.2.0/24", 254, "192.0.2.1", "192.0.2.254"},
		{"192.0.2.10-20", 11, "192.0.2.10", "192.0.2.20"},
		{"192.0.2.250-192.0.3.5", 12, "192.0.2.250", "192.0.3.5"},
		{"192.0.2.1, 192.0.2.2\n192.0.2.1", 2, "192.0.2.1", "192.0.2.2"},
		{"192.0.2.8/31", 2, "192.0.2.8", "192.0.2.9"},
		{"10.0.0.0/22", 1022, "10.0.0.1", "10.0.3.254"},
	} {
		a, err := ParseTargets(c.spec)
		if err != nil || len(a) != c.n || a[0].String() != c.first || a[len(a)-1].String() != c.last {
			t.Errorf("%q: %d %v (%v)", c.spec, len(a), err, a)
		}
	}
	for _, bad := range []string{"", "10.0.0.0/16", "10.0.0.20-10", "host.example", "2001:db8::/120", "10.0.0.0/22 10.0.4.0/24"} {
		if _, err := ParseTargets(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSNMPEncoding(t *testing.T) {
	for _, o := range []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.4.1.1916.2.1", "1.3.6.1.4.1.2272.30"} {
		e, _ := encOID(o)
		if _, v, _, _ := decTLV(e); decOID(v) != o {
			t.Errorf("OID %s -> %s", o, decOID(v))
		}
	}
	for _, i := range []int64{0, 1, 127, 128, 200, 65535, -1, 1 << 30} {
		if _, v, _, _ := decTLV(encInt(i)); decInt(v) != i {
			t.Errorf("int %d -> %d", i, decInt(v))
		}
	}
	long := strings.Repeat("x", 300)
	if _, v, _, err := decTLV(tlv(0x04, []byte(long))); err != nil || string(v) != long {
		t.Error("long length")
	}
}

// fakeAgent answers SNMP v2c GETs for the system group when the community matches.
func fakeAgent(t *testing.T, community string, values map[string]string) int {
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := c.ReadFrom(buf)
			if err != nil {
				return
			}
			_, msg, _, _ := decTLV(buf[:n])
			_, _, msg, _ = decTLV(msg)
			_, comm, msg, _ := decTLV(msg)
			if string(comm) != community {
				continue // wrong community: no answer, like a real agent
			}
			_, pdu, _, _ := decTLV(msg)
			_, id, pdu, _ := decTLV(pdu)
			_, _, pdu, _ = decTLV(pdu)
			_, _, pdu, _ = decTLV(pdu)
			_, vbs, _, _ := decTLV(pdu)
			var out []byte
			for len(vbs) > 0 {
				var vb []byte
				_, vb, vbs, _ = decTLV(vbs)
				_, o, _, _ := decTLV(vb)
				oid := decOID(o)
				e, _ := encOID(oid)
				val := []byte{0x80, 0x00} // noSuchObject
				if v, ok := values[oid]; ok {
					if strings.HasPrefix(v, "1.3.6.") {
						val, _ = encOID(v)
					} else {
						val = tlv(0x04, []byte(v))
					}
				}
				out = append(out, tlv(0x30, append(e, val...))...)
			}
			resp := tlv(0x30, append(append(encInt(1), tlv(0x04, comm)...), tlv(0xA2,
				append(append(append(tlv(0x02, id), encInt(0)...), encInt(0)...), tlv(0x30, out)...))...))
			_, _ = c.WriteTo(resp, from)
		}
	}()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// listen serves a greeting on a TCP port.
func listen(t *testing.T, greeting string) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte(greeting))
			time.Sleep(50 * time.Millisecond)
			c.Close()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

func closedPort(t *testing.T) int {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}

func TestScan(t *testing.T) {
	ssh := listen(t, "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13\r\n")
	telnet := listen(t, "\xff\xfb\x01\xff\xfb\x03\r\nUser Access Verification\r\n\r\nUsername: ")
	snmp := fakeAgent(t, "lab", map[string]string{oidSysDescr: "ExtremeXOS (X440G2-48p-10G4) version 31.7.1.4",
		oidSysObjectID: "1.3.6.1.4.1.1916.2.258", oidSysName: "edge-sw1"})
	addrs := []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	opt := Options{Probes: []Probe{{ssh, "ssh"}, {telnet, "telnet"}, {closedPort(t), "http"}}, Timeout: 500 * time.Millisecond,
		Community: "lab", SNMPPort: snmp, NoDNS: true}
	var got []Result
	done := 0
	Scan(context.Background(), addrs, opt, func(r Result) { got = append(got, r) }, func(d, _ int) { done = d })
	if len(got) != 1 || done != 1 {
		t.Fatalf("results %v done %d", got, done)
	}
	r := got[0]
	if r.Name != "edge-sw1" || r.Kind != KindSwitch || r.Vendor != "Extreme Networks" || r.Protocol != "SSH2" || r.Port != ssh ||
		!strings.HasPrefix(r.OS, "ExtremeXOS") || strings.Join(r.Services, ",") != "ssh,telnet,snmp" {
		t.Fatalf("%+v", r)
	}
	// wrong community: no SNMP, the SSH greeting decides
	opt.Community = "public"
	got = nil
	Scan(context.Background(), addrs, opt, func(r Result) { got = append(got, r) }, nil)
	if len(got) != 1 || got[0].Kind != KindServer || got[0].Vendor != "Ubuntu" || got[0].Name != "127.0.0.1" || got[0].SysName != "" {
		t.Fatalf("%+v", got)
	}
	// nothing open: no result
	got = nil
	Scan(context.Background(), addrs, Options{Probes: []Probe{{closedPort(t), "ssh"}}, Timeout: 300 * time.Millisecond, NoDNS: true},
		func(r Result) { got = append(got, r) }, nil)
	if len(got) != 0 {
		t.Fatalf("closed host reported: %+v", got)
	}
	// telnet greeting without IAC noise
	if g := greeting([]byte("\xff\xfd\x18\xff\xfa\x18\x01\xff\xf0\r\nlogin: ")); g != "login:" {
		t.Fatalf("greeting %q", g)
	}
}

func TestClassify(t *testing.T) {
	for _, c := range []struct {
		r    Result
		kind string
	}{
		{Result{SysDescr: "VSP-8404 (8.10.0.0)", SysObjectID: "1.3.6.1.4.1.2272.202"}, KindSwitch},
		{Result{SysDescr: "5520-24T-FabricEngine (9.0.1.0)"}, KindSwitch},
		{Result{SysDescr: "HP ETHERNET MULTI-ENVIRONMENT,ROM none,JETDIRECT,JD153"}, KindPrinter},
		{Result{Services: []string{"http", "printer"}}, KindPrinter},
		{Result{SysDescr: "Polycom VVX 450"}, KindPhone},
		{Result{Services: []string{"http", "sip"}}, KindPhone},
		{Result{Services: []string{"smb", "rdp"}}, KindComputer},
		{Result{SysDescr: "Hardware: Intel64 Family 6 - Software: Windows Version 6.3"}, KindComputer},
		{Result{Banner: "SSH-2.0-OpenSSH_8.7"}, KindServer},
		{Result{SysDescr: "Linux web01 5.15.0-91-generic #101-Ubuntu SMP x86_64"}, KindServer},
		{Result{SysDescr: "Cisco IOS Software, ISR4300 Software (X86_64_LINUX_IOSD-UNIVERSALK9-M)"}, KindRouter},
		{Result{SysDescr: "Cisco Adaptive Security Appliance Version 9.16"}, KindFirewall},
		{Result{SysDescr: "FortiGate-60F v7.2.5"}, KindFirewall},
		{Result{SysDescr: "Cisco Aironet 2800 Series Access Point"}, KindWireless},
		{Result{SysDescr: "Cisco NX-OS(tm) n9000"}, KindSwitch},
		{Result{Services: []string{"telnet"}}, KindSwitch},
		{Result{Services: []string{"https"}}, KindUnknown},
	} {
		r := c.r
		classify(&r)
		if r.Kind != c.kind {
			t.Errorf("%q %v -> %s, want %s", c.r.SysDescr+c.r.Banner, c.r.Services, r.Kind, c.kind)
		}
	}
}
