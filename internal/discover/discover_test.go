package discover

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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
		Community: "lab", SNMPPort: snmp, NoDNS: true, NoARP: true, NoWeb: true, SIPPort: -1}
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
	Scan(context.Background(), addrs, Options{Probes: []Probe{{closedPort(t), "ssh"}}, Timeout: 300 * time.Millisecond, NoDNS: true, NoARP: true, SIPPort: -1},
		func(r Result) { got = append(got, r) }, nil)
	if len(got) != 0 {
		t.Fatalf("closed host reported: %+v", got)
	}
}

// A Telnet device that only shows its prompt after the option negotiation is answered.
func TestTelnetNegotiation(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write([]byte{0xff, 0xfd, 0x18, 0xff, 0xfd, 0x1f, 0xff, 0xfb, 0x01, 0xff, 0xfb, 0x03})
		buf := make([]byte, 64)
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _ := c.Read(buf)
		if n < 6 || buf[0] != 0xff {
			return // no answer: a real device would stay silent
		}
		_, _ = c.Write([]byte("\r\n\r\nExtremeXOS (X440G2-48p-10G4) version 31.7.1.4\r\nCopyright (C) 1996-2023 Extreme Networks.\r\n\r\nlogin: "))
		time.Sleep(300 * time.Millisecond)
	}()
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	g := telnetGreeting(c, 2*time.Second)
	if !strings.Contains(g, "ExtremeXOS") || !strings.HasSuffix(g, "login:") {
		t.Fatalf("greeting %q", g)
	}
}

func TestWebAndSIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/tos/index.php", http.StatusFound)
			return
		}
		w.Header().Set("Server", "nginx")
		fmt.Fprint(w, `<html><head><title>TOS &amp; Apps</title></head><body>TerraMaster F4-423</body></html>`)
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	w := webInfo(context.Background(), netip.MustParseAddr("127.0.0.1"), port, false, 2*time.Second)
	if w.Title != "TOS & Apps" || w.Server != "nginx" || len(w.Hints) == 0 || w.Hints[0] != "TerraMaster" {
		t.Fatalf("%+v", w)
	}
	// SIP over UDP
	pc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer pc.Close()
	go func() {
		buf := make([]byte, 2048)
		n, from, err := pc.ReadFrom(buf)
		if err != nil || !strings.HasPrefix(string(buf[:n]), "OPTIONS sip:") {
			return
		}
		_, _ = pc.WriteTo([]byte("SIP/2.0 200 OK\r\nUser-Agent: Yealink SIP-T46S 66.86.0.15\r\nContent-Length: 0\r\n\r\n"), from)
	}()
	ua := sipOptions(context.Background(), netip.MustParseAddr("127.0.0.1"), pc.LocalAddr().(*net.UDPAddr).Port, time.Second)
	if ua != "Yealink SIP-T46S 66.86.0.15" {
		t.Fatalf("sip %q", ua)
	}
}

func TestNeighbourTables(t *testing.T) {
	out := map[netip.Addr]string{}
	parseARP("? (10.0.0.1) at 0:4:96:a1:b2:c3 on en0 ifscope [ethernet]\n? (10.0.0.9) at (incomplete) on en0\n"+
		"  10.0.0.20          00-15-65-12-34-56     dynamic\n", out)
	parseProcARP(strings.NewReader("IP address HW type Flags HW address Mask Device\n10.0.0.30 0x1 0x2 00:11:32:aa:bb:cc * eth0\n10.0.0.31 0x1 0x0 00:00:00:00:00:00 * eth0\n"), out)
	if len(out) != 3 || out[netip.MustParseAddr("10.0.0.1")] != "00:04:96:a1:b2:c3" || out[netip.MustParseAddr("10.0.0.20")] != "00:15:65:12:34:56" {
		t.Fatalf("%v", out)
	}
	o, err := LoadOUI(strings.NewReader("Registry,Assignment,Organization Name,Organization Address\nMA-L,000496,\"Extreme Networks, Inc.\",x\nMA-L,001565,\"XIAMEN YEALINK NETWORK TECHNOLOGY CO.,LTD.\",y\n"))
	if err != nil || o.Vendor("00:04:96:a1:b2:c3") != "Extreme Networks, Inc." || o.Vendor("02:11:22:33:44:55") != "private (randomised) address" {
		t.Fatalf("%v %v", o, err)
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
		{Result{Banner: "SSH-2.0-OpenSSH_8.1", Services: []string{"ssh", "telnet", "http", "https"}}, KindSwitch},
		{Result{Banner: "SSH-2.0-Mocana SSH 6.3", Services: []string{"ssh"}}, KindSwitch},
		{Result{Banner: "SSH-2.0-OpenSSH_8.1", Telnet: "ExtremeXOS (X440G2-48p) version 31.7 / login:", Services: []string{"ssh", "telnet"}}, KindSwitch},
		{Result{Banner: "SSH-2.0-OpenSSH_8.4", Services: []string{"ssh", "smb", "http"}}, KindServer},
		{Result{SysDescr: "Linux TNAS-F4 4.4.302+", Web: WebInfo{Title: "TOS", Hints: []string{"TerraMaster"}}, Services: []string{"ssh", "smb", "snmp"}}, KindNAS},
		{Result{SIP: "Yealink SIP-T46S 66.86.0.15", Services: []string{"http", "sip-udp"}}, KindPhone},
		{Result{SIP: "SIP", Services: []string{"sip-udp"}}, KindPhone},
		{Result{MAC: "00:15:65:12:34:56", MACVendor: "XIAMEN YEALINK NETWORK TECHNOLOGY CO.,LTD."}, KindPhone},
		{Result{Web: WebInfo{Title: "HP LaserJet MFP M428fdw"}, Services: []string{"http", "https", "printer"}}, KindPrinter},
		{Result{Self: true, Services: []string{"ssh"}}, KindComputer},
		{Result{MAC: "6a:1f:22:33:44:55", MACVendor: "private (randomised) address"}, KindMobile},
		{Result{DNS: "Galaxy-S22.lan"}, KindMobile},
		{Result{MDNS: "Natarajans-iPhone", Services: []string{"mdns"}}, KindMobile},
		{Result{MDNS: "Natarajans-MacBook-Pro", Services: []string{"mdns"}}, KindComputer},
		{Result{MAC: "8c:79:f5:00:00:01", MACVendor: "Samsung Electronics Co.,Ltd"}, KindMobile},
		{Result{MAC: "8c:79:f5:00:00:01", MACVendor: "Samsung Electronics Co.,Ltd", Services: []string{"http"}}, KindUnknown},
	} {
		r := c.r
		classify(&r)
		if r.Kind != c.kind {
			t.Errorf("%q %v -> %s, want %s", c.r.SysDescr+c.r.Banner, c.r.Services, r.Kind, c.kind)
		}
	}
}

// This computer is listed even when none of its ports is open.
func TestSelfListed(t *testing.T) {
	var me netip.Addr
	for a := range localAddrs() {
		me = a
		break
	}
	if !me.IsValid() {
		t.Skip("no IPv4 interface")
	}
	var got []Result
	Scan(context.Background(), []netip.Addr{me}, Options{Probes: []Probe{{closedPort(t), "ssh"}}, Timeout: 300 * time.Millisecond,
		NoDNS: true, NoARP: true, SIPPort: -1}, func(r Result) { got = append(got, r) }, nil)
	if len(got) != 1 || !got[0].Self || got[0].Kind != KindComputer || got[0].Name == "" {
		t.Fatalf("%+v", got)
	}
}

// A device answering the unicast mDNS reverse lookup with its own name.
func TestMDNSName(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 1500)
		n, from, err := pc.ReadFrom(buf)
		if err != nil || n < 12 {
			return
		}
		q := buf[12:n] // the question, echoed back
		ans := []byte{0xc0, 12, 0, 12, 0, 1, 0, 0, 0, 120}
		rd := []byte{}
		for _, l := range []string{"Natarajans-iPhone", "local"} {
			rd = append(append(rd, byte(len(l))), l...)
		}
		rd = append(rd, 0)
		ans = append(ans, byte(len(rd)>>8), byte(len(rd)))
		ans = append(ans, rd...)
		resp := append([]byte{buf[0], buf[1], 0x84, 0, 0, 1, 0, 1, 0, 0, 0, 0}, q...)
		_, _ = pc.WriteTo(append(resp, ans...), from)
	}()
	name := mdnsName(context.Background(), netip.MustParseAddr("127.0.0.1"), pc.LocalAddr().(*net.UDPAddr).Port, time.Second)
	if name != "Natarajans-iPhone" {
		t.Fatalf("mdns name %q", name)
	}
}

func TestTemplateTitleIgnored(t *testing.T) {
	for _, s := range []string{"{{pageTitle}}", "${title}", "<%= title %>", "[[title]]"} {
		if !reTemplate.MatchString(s) {
			t.Errorf("%q not recognised", s)
		}
	}
	if reTemplate.MatchString("ExtremeXOS ScreenPlay") {
		t.Error("real title ignored")
	}
}
