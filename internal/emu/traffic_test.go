package emu

import (
	"bytes"
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

// A test port's own protocols: it takes an address by DHCP and one by SLAAC, and announces
// itself with gratuitous ARP and LLDP (the "switch" here is a DHCP server and an IPv6 router).
func TestTrafficPyProtocols(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "tw-traffic.py"), []byte(TrafficPy), 0o644)
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 43011})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	to := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 43001}
	var mu sync.Mutex
	seen := map[string]bool{}
	csum := func(b []byte) uint16 {
		var s uint32
		for i := 0; i+1 < len(b); i += 2 {
			s += uint32(b[i])<<8 | uint32(b[i+1])
		}
		if len(b)%2 == 1 {
			s += uint32(b[len(b)-1]) << 8
		}
		for s>>16 != 0 {
			s = s&0xffff + s>>16
		}
		return ^uint16(s)
	}
	srvMAC := []byte{2, 0, 0, 0, 0, 0xfe}
	dhcpReply := func(req []byte, kind byte) []byte {
		// Ethernet + IPv4 + UDP + BOOTP from the server 10.20.0.1 giving 10.20.0.50/24, router 10.20.0.1
		bp := make([]byte, 240)
		bp[0], bp[1], bp[2] = 2, 1, 6
		copy(bp[4:8], req[14+20+8+4:14+20+8+8]) // xid
		copy(bp[16:20], []byte{10, 20, 0, 50})
		copy(bp[28:34], req[6:12])
		copy(bp[236:240], []byte{0x63, 0x82, 0x53, 0x63})
		bp = append(bp, 53, 1, kind, 54, 4, 10, 20, 0, 1, 1, 4, 255, 255, 255, 0, 3, 4, 10, 20, 0, 1, 51, 4, 0, 0, 0x0e, 0x10, 255)
		udp := append([]byte{0, 67, 0, 68, byte((8 + len(bp)) >> 8), byte(8 + len(bp)), 0, 0}, bp...)
		ip := []byte{0x45, 0, byte((20 + len(udp)) >> 8), byte(20 + len(udp)), 0, 0, 0, 0, 64, 17, 0, 0, 10, 20, 0, 1, 255, 255, 255, 255}
		cs := csum(ip)
		ip[10], ip[11] = byte(cs>>8), byte(cs)
		return append(append(append(append([]byte{}, req[6:12]...), srvMAC...), 0x08, 0x00), append(ip, udp...)...)
	}
	ra := func(to6 []byte, dstMAC []byte) []byte {
		src := []byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xfe, 0, 0, 1}
		p := []byte{134, 0, 0, 0, 64, 0, 0x07, 0x08, 0, 0, 0, 0, 0, 0, 0, 0}
		pio := []byte{3, 4, 64, 0xc0, 0, 0, 0x0e, 0x10, 0, 0, 0x0e, 0x10, 0, 0, 0, 0, 0xfd, 0, 0, 0x99, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
		p = append(p, pio...)
		ph := append(append(append([]byte{}, src...), to6...), 0, 0, 0, byte(len(p)), 0, 0, 0, 58)
		cs := csum(append(ph, p...))
		p[2], p[3] = byte(cs>>8), byte(cs)
		ip := append([]byte{0x60, 0, 0, 0, 0, byte(len(p)), 58, 255}, append(src, to6...)...)
		return append(append(append(append([]byte{}, dstMAC...), srvMAC...), 0x86, 0xdd), append(ip, p...)...)
	}
	go func() {
		buf := make([]byte, 65536)
		for {
			n, _, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			f := append([]byte(nil), buf[:n]...)
			mu.Lock()
			switch {
			case n > 14 && f[12] == 0x88 && f[13] == 0xcc:
				seen["lldp"] = bytes.Contains(f, []byte("Termwright Test port 1"))
			case n > 14 && f[12] == 0x08 && f[13] == 0x06 && bytes.Equal(f[14+14:14+18], f[14+24:14+28]):
				seen["garp "+net.IP(f[14+14:14+18]).String()] = true
			case n > 14+20+8+240 && f[12] == 0x08 && f[13] == 0 && f[23] == 17 && f[14+20+3] == 67:
				for i := 14 + 20 + 8 + 240; i+2 < n; i += 2 + int(f[i+1]) {
					if f[i] == 53 {
						if f[i+2] == 1 {
							seen["discover"] = true
							_, _ = c.WriteToUDP(dhcpReply(f, 2), to)
						} else if f[i+2] == 3 {
							seen["request"] = true
							_, _ = c.WriteToUDP(dhcpReply(f, 5), to)
						}
						break
					}
				}
			case n > 54 && f[12] == 0x86 && f[13] == 0xdd && f[20] == 58 && f[54] == 133:
				seen["rs"] = true
				_, _ = c.WriteToUDP(ra(f[22:38], f[6:12]), to)
			}
			mu.Unlock()
		}
	}()
	cfg := map[string]any{"run": 0, "testers": []map[string]any{{"id": "t1", "name": "Test port 1", "mac": "02:74:77:00:00:01", "dhcp": true, "slaac": true, "lldp": true, "listen": 43001, "send": 43011}}}
	j, _ := json.Marshal(cfg)
	_ = os.WriteFile(filepath.Join(dir, "traffic.json"), j, 0o644)
	cmd := exec.Command("python3", "-I", filepath.Join(dir, "tw-traffic.py"), dir)
	out, _ := os.Create(filepath.Join(dir, "log"))
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	var st struct {
		Lease  map[string]map[string]string
		Lease6 map[string]map[string]string
	}
	for end := time.Now().Add(8 * time.Second); time.Now().Before(end); time.Sleep(300 * time.Millisecond) {
		b, err := os.ReadFile(filepath.Join(dir, "traffic-stats.json"))
		mu.Lock()
		done := seen["garp 10.20.0.50"] && seen["lldp"]
		mu.Unlock()
		if err == nil && json.Unmarshal(b, &st) == nil && st.Lease["t1"]["ip"] != "" && st.Lease6["t1"]["ip"] != "" && done {
			break
		}
	}
	lg, _ := os.ReadFile(filepath.Join(dir, "log"))
	mu.Lock()
	defer mu.Unlock()
	if st.Lease["t1"]["ip"] != "10.20.0.50/24" || st.Lease["t1"]["gw"] != "10.20.0.1" || !seen["discover"] || !seen["request"] {
		t.Fatalf("DHCP: %v %v\n%s", st.Lease, seen, lg)
	}
	if st.Lease6["t1"]["ip"] != "fd00:99::74:77ff:fe00:1/64" || !seen["rs"] {
		t.Fatalf("SLAAC: %v %v", st.Lease6, seen)
	}
	if !seen["garp 10.20.0.50"] || !seen["lldp"] {
		t.Fatalf("gratuitous ARP after the lease, LLDP: %v", seen)
	}
}

// Per-flow counters, the latency histogram, the marks frames arrive with, and a round-trip stream
// (the far test port sends its frames back; the sender measures the latency).
func TestTrafficPyFlows(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tw-traffic.py"), []byte(TrafficPy), 0o644); err != nil {
		t.Fatal(err)
	}
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
	a, b := sw(41111, 41102), sw(41112, 41101)
	defer a.Close()
	defer b.Close()
	cfg := map[string]any{"run": 7, "testers": []map[string]any{
		{"id": "t1", "mac": "02:74:77:00:00:01", "ip": "10.10.0.1/24", "listen": 41101, "send": 41111},
		{"id": "t2", "mac": "02:74:77:00:00:02", "ip": "10.10.0.2/24", "listen": 41102, "send": 41112}},
		"streams": []map[string]any{
			{"id": "s1", "from": "t1", "to": "t2", "kind": "udp", "vlan": 10, "prio": 5, "dscp": 46, "size": 128, "pps": 400, "count": 200, "on": true, "toMac": "02:74:77:00:00:02", "srcIp": "10.10.0.1", "dstIp": "10.10.0.2",
				"mods": []map[string]any{{"field": "dport", "mode": "inc", "count": 4}}},
			{"id": "s2", "from": "t1", "to": "t2", "kind": "udp", "rtt": true, "size": 100, "pps": 200, "count": 100, "on": true, "toMac": "02:74:77:00:00:02", "srcIp": "10.10.0.1", "dstIp": "10.10.0.2"}}}
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
		RX   map[string]int
		Lat  map[string][]float64
		Hist map[string]map[string]int
		FRX  map[string]map[string][]float64 `json:"frx"`
		FTX  map[string]map[string]int       `json:"ftx"`
		FLab map[string]map[string]string    `json:"flab"`
		QoS  map[string]map[string]int       `json:"qos"`
	}
	for end := time.Now().Add(8 * time.Second); time.Now().Before(end); time.Sleep(300 * time.Millisecond) {
		b, err := os.ReadFile(filepath.Join(dir, "traffic-stats.json"))
		if err == nil && json.Unmarshal(b, &st) == nil && st.RX["s1"] == 200 && st.RX["s2"] == 100 && len(st.Lat["s2"]) > 2 && st.Lat["s2"][2] == 100 {
			break
		}
	}
	if st.RX["s1"] != 200 || len(st.FRX["s1"]) != 4 || st.FRX["s1"]["3"][0] != 50 || st.FTX["s1"]["0"] != 50 || st.FLab["s1"]["1"] != "dport 5002" {
		lg, _ := os.ReadFile(filepath.Join(dir, "log"))
		t.Fatalf("flows: rx %v frx %v ftx %v lab %v\n%s", st.RX, st.FRX, st.FTX, st.FLab, lg)
	}
	if st.QoS["s1"]["10/5/46"] != 200 || st.QoS["s2"]["0/0/0"] != 100 {
		t.Fatalf("marks %v", st.QoS)
	}
	n := 0
	for _, c := range st.Hist["s1"] {
		n += c
	}
	if n != 200 {
		t.Fatalf("histogram %v", st.Hist)
	}
	if l := st.Lat["s2"]; len(l) < 3 || l[2] != 100 || st.RX["s2"] != 100 {
		t.Fatalf("round trip: lat %v rx %v", l, st.RX)
	}
}
