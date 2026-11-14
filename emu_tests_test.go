package main

import (
	"strings"
	"testing"
)

// The rate search ends within its resolution below the highest passing rate, in few trials.
func TestEmuSearch(t *testing.T) {
	for _, c := range []struct{ top, limit int }{{10000, 3700}, {10000, 10000}, {10000, 1}, {4096, 1000}} {
		got, trials, err := emuSearch(c.top, 0.02, func(n int) (bool, error) { return n <= c.limit, nil })
		if err != nil || got > c.limit || float64(c.limit-got) > float64(c.limit)*0.02+1 || trials > 20 {
			t.Fatalf("limit %d: got %d in %d trials", c.limit, got, trials)
		}
	}
	if got, _, _ := emuSearch(1000, 0.02, func(int) (bool, error) { return false, nil }); got != 0 {
		t.Fatalf("nothing passes: %d", got)
	}
}

// iperf3's JSON as a serial console shows it (the command echoed, CRs) gives the rate, retransmits, jitter and loss.
func TestEmuIperfParse(t *testing.T) {
	tcp := "iperf3 -c 10.0.0.2 -t 10 -i 0 -J\r\n{\r\n\t\"start\": {},\r\n\t\"end\": {\r\n\t\t\"sum_sent\": {\"bits_per_second\": 941234567.8, \"retransmits\": 12},\r\n\t\t\"sum_received\": {\"bits_per_second\": 938765432.1}\r\n\t}\r\n}\r\nhost:~# "
	r, err := emuIperfParse(tcp)
	if err != nil || r.Mbps != 938.77 || r.Retrans != 12 {
		t.Fatalf("tcp %+v %v", r, err)
	}
	udp := "{\"end\": {\"sum\": {\"bits_per_second\": 1048576, \"jitter_ms\": 0.25, \"lost_percent\": 1.5, \"lost_packets\": 3, \"packets\": 200}}}"
	if r, err := emuIperfParse(udp); err != nil || r.Jitter != 0.25 || r.Loss != 1.5 || r.Lost != 3 || r.Mbps != 1.05 {
		t.Fatalf("udp %+v %v", r, err)
	}
	if _, err := emuIperfParse(`{"error": "unable to connect to server: Connection refused"}`); err == nil || !strings.Contains(err.Error(), "Connection refused") {
		t.Fatal("an iperf3 error")
	}
	if _, err := emuIperfParse("sh: iperf3: not found"); err == nil {
		t.Fatal("no output")
	}
}
