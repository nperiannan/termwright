package emu

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// traffic.py end to end: two test ports joined by a "switch" (a UDP relay):
// a UDP stream and a broadcast ARP stream are counted on the other side, and
// a test port answers ARP for its address.
func TestTrafficPy(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tw-traffic.py"), []byte(TrafficPy), 0o644); err != nil {
		t.Fatal(err)
	}
	// ports: test port 1 listens 41001, sends to 41011 (the switch); test port 2 listens 41002, sends to 41012
	sw := func(in, out int) *net.UDPConn {
		c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: in})
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			buf := make([]byte, 65536)
			for {
				n, _, err := c.ReadFromUDP(buf)
				if err != nil {
					return
				}
				_, _ = c.WriteToUDP(buf[:n], &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: out})
			}
		}()
		return c
	}
	a, b := sw(41011, 41002), sw(41012, 41001)
	defer a.Close()
	defer b.Close()
	cfg := map[string]any{"run": 5, "testers": []map[string]any{
		{"id": "t1", "mac": "02:74:77:00:00:01", "ip": "10.10.0.1/24", "listen": 41001, "send": 41011},
		{"id": "t2", "mac": "02:74:77:00:00:02", "ip": "10.10.0.2/24", "listen": 41002, "send": 41012}},
		"streams": []map[string]any{
			{"id": "s1", "from": "t1", "to": "t2", "kind": "udp", "vlan": 10, "size": 256, "pps": 500, "count": 300, "on": true, "toMac": "02:74:77:00:00:02", "srcIp": "10.10.0.1", "dstIp": "10.10.0.2"},
			{"id": "s2", "from": "t2", "kind": "arp", "size": 64, "pps": 200, "count": 100, "on": true, "srcIp": "10.10.0.2", "dstIp": "10.10.0.1"},
			{"id": "s3", "from": "t1", "to": "t2", "kind": "icmp", "size": 100, "pps": 100, "count": 50, "on": true, "toMac": "02:74:77:00:00:02", "srcIp": "10.10.0.1", "dstIp": "10.10.0.2"},
			{"id": "s4", "from": "t2", "to": "t1", "kind": "tcp", "size": 80, "pps": 100, "count": 50, "on": true, "toMac": "02:74:77:00:00:01", "srcIp": "10.10.0.2", "dstIp": "10.10.0.1"}}}
	j, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "traffic.json"), j, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", "-I", filepath.Join(dir, "tw-traffic.py"), dir)
	out, _ := os.Create(filepath.Join(dir, "log"))
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	var st struct {
		TX, RX map[string]int
		Lat    map[string][]float64
		Busy   map[string]bool
	}
	for end := time.Now().Add(8 * time.Second); time.Now().Before(end); time.Sleep(300 * time.Millisecond) {
		b, err := os.ReadFile(filepath.Join(dir, "traffic-stats.json"))
		if err == nil && json.Unmarshal(b, &st) == nil && st.TX["s1"] == 300 && st.RX["s1"] == 300 && st.RX["s2"] == 100 && st.RX["s3"] == 50 && st.RX["s4"] == 50 && !st.Busy["s1"] {
			break
		}
	}
	if st.TX["s1"] != 300 || st.RX["s1"] != 300 || st.TX["s2"] != 100 || st.RX["s2"] != 100 || st.RX["s3"] != 50 || st.RX["s4"] != 50 {
		lg, _ := os.ReadFile(filepath.Join(dir, "log"))
		t.Fatalf("tx %v rx %v\n%s", st.TX, st.RX, lg)
	}
	if l := st.Lat["s1"]; len(l) != 3 || l[2] != 300 || l[0]/l[2] > 50 {
		t.Fatalf("latency %v", l)
	}
	// stop: an empty configuration ends it
	_ = os.WriteFile(filepath.Join(dir, "traffic.json"), []byte(`{"run":0,"testers":[]}`), 0o644)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("it did not end without test ports")
	}
}
