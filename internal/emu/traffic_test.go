package emu

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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
	if l := st.Lat["s1"]; len(l) < 3 || l[2] != 300 || l[0]/l[2] > 50 {
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

// The richer streams: changing fields, IMIX sizes, DSCP, IPv6, multicast to the ports that joined,
// and the receiver's out-of-order, duplicate and jitter counts (the "switch" here reorders and repeats frames).
func TestTrafficPyMore(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tw-traffic.py"), []byte(TrafficPy), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	seen := map[string]int{} // what the switch saw
	sw := func(in, out int, mess bool) *net.UDPConn {
		c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: in})
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			buf := make([]byte, 65536)
			var held []byte
			n := 0
			for {
				k, _, err := c.ReadFromUDP(buf)
				if err != nil {
					return
				}
				f := append([]byte(nil), buf[:k]...)
				mu.Lock()
				if len(f) > 14 && f[12] == 0x86 && f[13] == 0xdd {
					seen["v6"]++
				}
				if len(f) > 16 && f[12] == 0x08 && f[13] == 0 {
					seen[fmt.Sprintf("tos%d", f[15])]++
					seen[fmt.Sprintf("src%d", f[29])]++
				}
				seen[fmt.Sprintf("len%d", k)]++
				mu.Unlock()
				to := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: out}
				n++
				switch {
				case mess && n%50 == 10: // hold one back: it arrives after the next
					held = f
					continue
				case mess && n%50 == 20: // and one twice
					_, _ = c.WriteToUDP(f, to)
				}
				_, _ = c.WriteToUDP(f, to)
				if held != nil {
					_, _ = c.WriteToUDP(held, to)
					held = nil
				}
			}
		}()
		return c
	}
	a, b := sw(42011, 42002, true), sw(42012, 42001, false)
	defer a.Close()
	defer b.Close()
	cfg := map[string]any{"run": 9, "testers": []map[string]any{
		{"id": "t1", "mac": "02:74:77:00:00:01", "ip": "10.10.0.1/24", "ip6": "fd00::1/64", "listen": 42001, "send": 42011},
		{"id": "t2", "mac": "02:74:77:00:00:02", "ip": "10.10.0.2/24", "ip6": "fd00::2/64", "groups": []string{"239.1.1.1"}, "listen": 42002, "send": 42012}},
		"streams": []map[string]any{
			{"id": "s1", "from": "t1", "to": "t2", "kind": "udp", "size": 64, "sizeMode": "imix", "dscp": 46, "pps": 2000, "count": 600, "on": true, "toMac": "02:74:77:00:00:02", "srcIp": "10.10.0.1", "dstIp": "10.10.0.2",
				"mods": []map[string]any{{"field": "srcIp", "mode": "inc", "count": 4}}},
			{"id": "s2", "from": "t1", "to": "t2", "kind": "udp", "size": 128, "pps": 500, "count": 100, "on": true, "toMac": "02:74:77:00:00:02", "srcIp": "fd00::1", "dstIp": "fd00::2"},
			{"id": "s3", "from": "t1", "kind": "udp", "size": 128, "pps": 500, "count": 100, "burst": 10, "on": true, "srcIp": "10.10.0.1", "dstIp": "239.1.1.1"}}}
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
		TX, RX, Ooo, Dup map[string]int
		Jit              map[string][]float64
		Busy             map[string]bool
	}
	for end := time.Now().Add(8 * time.Second); time.Now().Before(end); time.Sleep(300 * time.Millisecond) {
		b, err := os.ReadFile(filepath.Join(dir, "traffic-stats.json"))
		if err == nil && json.Unmarshal(b, &st) == nil && st.TX["s1"] == 600 && st.TX["s3"] == 100 && st.TX["s2"] == 100 && !st.Busy["s1"] && !st.Busy["s3"] {
			time.Sleep(500 * time.Millisecond)
			b, _ = os.ReadFile(filepath.Join(dir, "traffic-stats.json"))
			_ = json.Unmarshal(b, &st)
			break
		}
	}
	lg, _ := os.ReadFile(filepath.Join(dir, "log"))
	mu.Lock()
	defer mu.Unlock()
	if st.RX["s2"] != 100 || seen["v6"] < 100 {
		t.Fatalf("IPv6: rx %v, frames %d\n%s", st.RX, seen["v6"], lg)
	}
	if st.RX["s3"] != 100 {
		t.Fatalf("multicast to the port that joined: rx %v\n%s", st.RX, lg)
	}
	if seen["tos184"] < 600 || seen["src1"] < 150 || seen["src4"] < 150 {
		t.Fatalf("DSCP 46 and the source address stepping: %v", seen)
	}
	if seen["len1518"] < 40 || seen["len594"] < 150 {
		t.Fatalf("IMIX sizes: %v", seen)
	}
	if st.Ooo["s1"]+st.Ooo["s2"]+st.Ooo["s3"] == 0 || st.Dup["s1"]+st.Dup["s2"]+st.Dup["s3"] == 0 || len(st.Jit["s1"]) != 2 {
		t.Fatalf("out of order %v, duplicates %v, jitter %v", st.Ooo, st.Dup, st.Jit)
	}
	if st.RX["s1"] != 600 {
		t.Fatalf("a duplicate is not counted as received: %v", st.RX)
	}
}
