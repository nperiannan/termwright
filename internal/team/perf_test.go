package team

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// TestPerfTeam: a big team - TW_PERF_TEAM=1 - 5000 inventory devices, 200
// members sending heartbeats and long-polling the status, and a year of usage.
func TestPerfTeam(t *testing.T) {
	if os.Getenv("TW_PERF_TEAM") == "" {
		t.Skip("set TW_PERF_TEAM=1")
	}
	s, code, ts := start(t)
	ctx := context.Background()
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	own, _ := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "Owner"})
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	tm, _ := oc.Team(ctx)
	log := func(what string, d time.Duration, extra string) {
		t.Logf("%-44s %8.0f ms %s", what, float64(d.Microseconds())/1000, extra)
	}

	devs := make([]Device, 5000)
	for i := range devs {
		devs[i] = Device{Name: fmt.Sprintf("sw-%04d", i), Folder: fmt.Sprintf("Site %02d/Rack %d", i%50, i/50%10), Host: fmt.Sprintf("10.%d.%d.%d", i/65536, i/256%256, i%256), Protocol: "SSH2", DeviceType: "EXOS"}
	}
	t0 := time.Now()
	if _, err := oc.Push(ctx, devs); err != nil {
		t.Fatal(err)
	}
	log("push 5000 devices", time.Since(t0), "")
	t0 = time.Now()
	inv, _ := oc.Inventory(ctx, 0, 0)
	log("inventory download (every member)", time.Since(t0), fmt.Sprintf("%d devices", len(inv.Devices)))

	// a year of usage: 200 members, ~8 sessions a working day each
	s.mu.Lock()
	now := time.Now()
	for d := 0; d < 365; d++ {
		for m := 0; m < 200; m++ {
			for k := 0; k < 8; k++ {
				st := now.Add(-time.Duration(d)*24*time.Hour + time.Duration(k)*time.Hour).Unix()
				s.usage.Use = append(s.usage.Use, UseRec{Dev: s.st.Inventory[(d*7+m*13+k)%5000].ID, DevName: "sw", M: fmt.Sprintf("m%d", m), Name: fmt.Sprintf("Member %d", m), S: st, E: st + 2400})
			}
		}
	}
	n := len(s.usage.Use)
	s.saveUsage()
	s.lastUse = nil
	s.mu.Unlock()
	t0 = time.Now()
	s.FlushUsage()
	log("write usage.json (background, every 15 s)", time.Since(t0), fmt.Sprintf("%d sessions recorded", n))
	fi, _ := os.Stat(s.usageFile())
	t.Logf("%-44s %8.1f MB", "usage.json size", float64(fi.Size())/1e6)

	for _, days := range []int{30, 365} {
		t0 = time.Now()
		b, err := oc.raw(ctx, "GET", fmt.Sprintf("/api/admin/usage?days=%d", days), nil)
		if err != nil {
			t.Fatal(err)
		}
		log(fmt.Sprintf("usage report, %d days (admin page)", days), time.Since(t0), fmt.Sprintf("%.1f MB", float64(len(b))/1e6))
	}
	var rep UsageReport
	_ = rep
	var st AdminState
	t0 = time.Now()
	_ = oc.call(ctx, "GET", "/api/admin/state", nil, &st)
	log("admin page state", time.Since(t0), "")

	// 200 members: heartbeats with 3 devices each, and the status long poll
	clients := make([]*Client, 200)
	for i := range clients {
		j, err := anon.Join(ctx, JoinRequest{Invite: tm.Invite, Name: fmt.Sprintf("Member %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		clients[i] = NewClient(addr(ts), s.Fingerprint(), j.Token)
	}
	var wg sync.WaitGroup
	var worst time.Duration
	var mu sync.Mutex
	t0 = time.Now()
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *Client) {
			defer wg.Done()
			refs := []DeviceRef{}
			for k := 0; k < 3; k++ {
				d := devs[(i*3+k)%5000]
				refs = append(refs, DeviceRef{Host: d.Host, Protocol: "SSH2", Port: 22})
			}
			s0 := time.Now()
			if err := c.Presence(ctx, refs); err != nil {
				t.Error(err)
			}
			if _, err := c.Status(ctx, 0, 0); err != nil {
				t.Error(err)
			}
			mu.Lock()
			worst = max(worst, time.Since(s0))
			mu.Unlock()
		}(i, c)
	}
	wg.Wait()
	log("200 members: heartbeat + status at once", time.Since(t0), fmt.Sprintf("slowest member %.0f ms", float64(worst.Microseconds())/1000))
	// steady state: every member has Last already and sends the next heartbeat
	lastRev := int64(-1)
	if st0, err := clients[0].Status(ctx, 0, 0); err == nil {
		lastRev = st0.LastRev
	}
	worst = 0
	t0 = time.Now()
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *Client) {
			defer wg.Done()
			d := devs[(i*3+7)%5000]
			s0 := time.Now()
			_ = c.Presence(ctx, []DeviceRef{{Host: d.Host, Protocol: "SSH2", Port: 22}})
			st, err := c.StatusSince(ctx, 0, lastRev, 0)
			if err != nil || st.Last != nil && st.LastRev == lastRev {
				t.Errorf("status %v (last sent again: %v)", err, st.Last != nil)
			}
			mu.Lock()
			worst = max(worst, time.Since(s0))
			mu.Unlock()
		}(i, c)
	}
	wg.Wait()
	log("200 members: next heartbeat + status", time.Since(t0), fmt.Sprintf("slowest member %.0f ms", float64(worst.Microseconds())/1000))
	t0 = time.Now()
	if _, err := oc.Reserve(ctx, DeviceRef{Host: devs[42].Host, Protocol: "SSH2"}, time.Now().Add(time.Hour), "x"); err != nil {
		t.Fatal(err)
	}
	log("a reservation (saves team.json)", time.Since(t0), "")
	fi, _ = os.Stat(s.Dir + "/team.json")
	t.Logf("%-44s %8.1f MB", "team.json size", float64(fi.Size())/1e6)
}
