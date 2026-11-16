package trex

import (
	"testing"
	"time"
)

func TestClientAgainstFake(t *testing.T) {
	f, err := NewFake()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := Dial(f.Addr(), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Version != "v3.04" {
		t.Fatalf("version %q", c.Version)
	}
	ps, err := c.Ports()
	if err != nil || len(ps) != 2 || ps[1].Speed != 10000 || ps[0].MAC != "0c:42:a1:00:00:10" || ps[0].Link == nil || !*ps[0].Link || ps[0].State != "IDLE" {
		t.Fatalf("ports %+v %v", ps, err)
	}
	h, err := c.Acquire(0, "termwright")
	if err != nil || h != "h0" {
		t.Fatalf("acquire %q %v", h, err)
	}
	c.Reset(0, h)
	for i, rule := range []string{"stats", "latency"} {
		s := map[string]any{"enabled": true, "packet": map[string]any{"binary": "AAAA"}, "mode": map[string]any{"type": "single_burst", "total_pkts": 1000 / (1 + 9*i),
			"rate": map[string]any{"type": "pps", "value": 100}}, "flow_stats": map[string]any{"enabled": true, "stream_id": 10 + i, "rule_type": rule}}
		if err := c.AddStream(0, h, i+1, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Start(0, h); err != nil {
		t.Fatal(err)
	}
	st, err := c.PGIDStats([]int{10, 11}) // a long reply: compressed
	if err != nil || st[10].TX != 1000 || st[10].RX != 1000 || st[11].RX != 100 || st[11].LatAvg != 12.5 || st[11].HasLatency != 1 {
		t.Fatalf("pgid stats %+v %v", st, err)
	}
	if p, err := c.PortStats(0); err != nil || p.OPackets != 1100 {
		t.Fatalf("port stats %+v %v", p, err)
	}
	if s := c.PortState(0); s != "STREAMS" {
		t.Fatalf("state %q", s)
	}
	if err := c.Release(0, h); err != nil {
		t.Fatal(err)
	}
	if f.Called("api_sync_v2") != 1 || f.Called("add_stream") != 2 {
		t.Fatalf("calls %v", f.Calls)
	}
}
