package syslog

import (
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		raw                     string
		fac, sev                int
		host, app, text, format string
	}{
		{`<165>1 2026-11-19T10:00:00.003Z core-sw1 sshd 1234 ID47 [exampleSDID@32473 iut="3"] Login failed for admin`, 20, 5, "core-sw1", "sshd", "Login failed for admin", "5424"},
		{`<34>Oct 11 22:14:15 mymachine su: 'su root' failed for lonvick on /dev/pts/8`, 4, 2, "mymachine", "su", "'su root' failed for lonvick on /dev/pts/8", "3164"},
		{`<189>123: *Mar  1 00:01:02.345 UTC: %LINK-3-UPDOWN: Interface GigabitEthernet0/1, changed state to down`, 23, 5, "", "LINK-3-UPDOWN", "%LINK-3-UPDOWN: Interface GigabitEthernet0/1, changed state to down", "3164"},
		{`<190>Nov 19 10:01:02 10.127.34.33 vlan.msgs.portLinkStateDown: Port 1:2 link down`, 23, 6, "10.127.34.33", "vlan.msgs.portLinkStateDown", "Port 1:2 link down", "3164"},
		{`<13>2026-11-19T10:00:00+05:30 dist-03 CLI: user admin logged in`, 1, 5, "dist-03", "CLI", "user admin logged in", "3164"},
		{`no priority at all`, 1, 5, "", "", "no priority at all", "3164"},
	}
	for _, c := range cases {
		m := Parse(c.raw)
		if m.Facility != c.fac || m.Severity != c.sev || m.Host != c.host || m.App != c.app || m.Text != c.text || m.Format != c.format {
			t.Errorf("%s\n got %+v", c.raw, m)
		}
	}
	m := Parse(`<165>1 2026-11-19T10:00:00Z h a - - [a@1 x="1"][b@2 y="2"] text`)
	if m.SD != `[a@1 x="1"][b@2 y="2"]` || m.ProcID != "" || m.SeverityName != "notice" || m.FacilityName != "local4" {
		t.Fatalf("%+v", m)
	}
	if SeverityOf("error") != 3 || SeverityOf("warning") != 4 || FacilityOf("local7") != 23 || SeverityOf("x") != -1 {
		t.Fatal("names")
	}
}

func TestServerUDPTCPTLSAndQuery(t *testing.T) {
	var got int32
	s, err := Listen(Config{Bind: "127.0.0.1", UDP: freePort(t, "udp"), TCP: freePort(t, "tcp"), TLS: freePort(t, "tcp"), Keep: 100}, func(Message) { atomic.AddInt32(&got, 1) })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	addr := func(k string) string { return "127.0.0.1:" + strconv.Itoa(s.Port(k)) }
	if _, err := Send(Out{Target: addr("udp"), Facility: 23, Severity: 3, Host: "core-sw1", App: "PORT", Text: "1/1 down"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Send(Out{Target: addr("tcp"), Proto: "tcp", Format: "3164", Facility: 4, Severity: 6, Host: "dist-03", App: "aaa", Text: "user admin logged in"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Send(Out{Target: addr("tls"), Proto: "tls", Insecure: true, Facility: 1, Severity: 4, Text: "over tls"}); err != nil {
		t.Fatal(err)
	}
	// newline framing on TCP, several in one go
	c, err := net.Dial("tcp", addr("tcp"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(c, "<14>Nov 19 10:00:00 acc-05 app: one\n<11>Nov 19 10:00:01 acc-05 app: two\n")
	c.Close()
	// TLS refuses a client that checks the certificate (self-signed)
	if _, err := tls.Dial("tcp", addr("tls"), &tls.Config{}); err == nil {
		t.Fatal("self-signed accepted")
	}
	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&got) < 5 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	all, _ := s.Messages(Query{})
	if len(all) != 5 {
		t.Fatalf("%d: %+v", len(all), all)
	}
	three := 3
	if l, _ := s.Messages(Query{MaxSeverity: &three}); len(l) != 2 { // err (udp) and err "two"
		t.Fatalf("severity: %+v", l)
	}
	if l, _ := s.Messages(Query{Text: "admin logged"}); len(l) != 1 || l[0].Host != "dist-03" || l[0].Proto != "tcp" {
		t.Fatalf("text: %+v", l)
	}
	if l, _ := s.Messages(Query{Text: "/app one$|over tls$/"}); len(l) != 2 {
		t.Fatalf("regex: %+v", l)
	}
	if l, _ := s.Messages(Query{After: all[2].ID}); len(l) != 2 {
		t.Fatalf("after: %+v", l)
	}
	if _, err := s.Messages(Query{Text: "/(/"}); err == nil {
		t.Fatal("bad regex")
	}
	if s.Counts()[3] != 2 || s.Hosts()["127.0.0.1"] != 5 {
		t.Fatal(s.Counts(), s.Hosts())
	}
	for i := 0; i < 200; i++ { // keeps at most 100
		s.Inject("<14>x "+strconv.Itoa(i), "1.2.3.4", "udp")
	}
	if l, _ := s.Messages(Query{Limit: 1000}); len(l) > 100 || l[len(l)-1].Text != "x 199" {
		t.Fatalf("keep: %d", len(l))
	}
}

func freePort(t *testing.T, kind string) int {
	if kind == "udp" {
		c, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		return c.LocalAddr().(*net.UDPAddr).Port
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
