package main

import (
	"context"
	"strings"
	"testing"
)

func TestParsePing(t *testing.T) {
	mac := "PING 10.0.0.1: 56 data bytes\n4 packets transmitted, 4 packets received, 0.0% packet loss\nround-trip min/avg/max/stddev = 1.1/3.4/9.0/1.2 ms"
	if ok, ms, sum, _ := parsePing(mac, "10.0.0.1"); !ok || ms != 3.4 || sum != "3.4 ms" {
		t.Fatalf("mac %v %v %q", ok, ms, sum)
	}
	lin := "4 packets transmitted, 3 received, 25% packet loss, time 905ms\nrtt min/avg/max/mdev = 0.4/12.6/30/1 ms"
	if ok, _, sum, _ := parsePing(lin, "x"); !ok || sum != "13 ms · 3 of 4" {
		t.Fatalf("linux %q", sum)
	}
	win := "Packets: Sent = 4, Received = 0, Lost = 4 (100% loss),"
	if ok, _, sum, _ := parsePing(win, "x"); ok || sum != "0 of 4 answered" {
		t.Fatalf("windows %q", sum)
	}
	winOK := "Packets: Sent = 4, Received = 4, Lost = 0 (0% loss),\nMinimum = 1ms, Maximum = 3ms, Average = 2ms"
	if ok, ms, _, _ := parsePing(winOK, "x"); !ok || ms != 2 {
		t.Fatalf("windows ok %v", ms)
	}
}

func TestNetCheckGateway(t *testing.T) {
	old := ncRun
	t.Cleanup(func() { ncRun = old })
	ncRun = func(_ context.Context, name string, args ...string) (string, error) {
		switch name {
		case "ip":
			return "default via 10.234.179.1 dev en7 proto dhcp", nil
		case "route":
			if args[0] == "-n" {
				return "   route to: default\ndestination: default\n    gateway: 192.168.1.1\n", nil
			}
			return "IPv4 Route Table\n          0.0.0.0          0.0.0.0     10.1.1.254    10.1.1.20     25\n", nil
		case "ping":
			return "4 packets transmitted, 4 received, 0% packet loss\nrtt min/avg/max/mdev = 1.0/1.2/1.5/0.1 ms", nil
		}
		return "", nil
	}
	for goos, gw := range map[string]string{"linux": "10.234.179.1", "darwin": "192.168.1.1", "windows": "10.1.1.254"} {
		r := ncCheck(context.Background(), goos, "gateway")
		if r.Address != gw || r.State != "ok" || r.Summary != "1.2 ms" {
			t.Fatalf("%s: %+v", goos, r)
		}
	}
	r := ncCheck(context.Background(), "linux", "127.0.0.1:1")
	if r.State != "bad" || !strings.Contains(r.Summary, "TCP 1 closed") {
		t.Fatalf("closed port: %+v", r)
	}
}

func TestIperfJSON(t *testing.T) {
	two := `{"start":{"connected":[{"remote_host":"10.10.0.50"}],"timestamp":{"time":"Mon, 10 Nov 2026 10:00:00 GMT"}},"end":{"sum_sent":{"seconds":10,"bits_per_second":942000000,"retransmits":12},"sum_received":{"bits_per_second":938000000}}}
{"start":{},"end":{},"error":"the client has terminated"}
{"start":{"connected":[{"remote_host":"10.10.0.51"}]},"end":{"sum_sent":{"seconds":5,"bits_per_second":100},"sum_received":{"bits_per_second":90}}}`
	var got []IperfTest
	readIperfTests(strings.NewReader(two), func(t IperfTest) { got = append(got, t) })
	if len(got) != 2 || got[0].Peer != "10.10.0.50" || got[0].SentBps != 942e6 || got[0].Retransmits != 12 || got[1].Peer != "10.10.0.51" {
		t.Fatalf("%+v", got)
	}
}
