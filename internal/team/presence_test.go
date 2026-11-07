package team

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPresenceReserveAndNotices(t *testing.T) {
	s, code, ts := start(t)
	ctx := context.Background()
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	own, _ := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "Natarajan"})
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	tm, _ := oc.Team(ctx)
	ra, _ := anon.Join(ctx, JoinRequest{Invite: tm.Invite, Name: "Ravi"})
	rc := NewClient(addr(ts), s.Fingerprint(), ra.Token)
	_, _ = oc.Push(ctx, []Device{{Name: "core-sw1", Host: "10.127.35.13", Protocol: "SSH2", DeviceType: "EXOS", MAC: "0c:9b:78:ea:9c:00"}})

	st, _ := rc.Status(ctx, 0, 0)
	// Ravi waits for a change; Natarajan opens the device (under another name, found by MAC)
	got := make(chan Status, 1)
	go func() { x, _ := rc.Status(ctx, st.Rev, 20); got <- x }()
	time.Sleep(100 * time.Millisecond)
	if err := oc.Presence(ctx, []DeviceRef{{Host: "10.127.35.13", Port: 22, Protocol: "SSH2", Name: "10.127.35.13", Since: "2026-11-07T00:30:00Z"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case x := <-got:
		if len(x.InUse) != 1 || x.InUse[0].Name != "Natarajan" {
			t.Fatalf("in use %+v", x.InUse)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the status long poll did not wake up")
	}
	// Natarajan reserves it; Ravi cannot, and connecting anyway sends a notice
	if _, err := oc.Reserve(ctx, DeviceRef{Host: "10.127.35.13", Protocol: "SSH2"}, time.Now().Add(2*time.Hour), "upgrade test"); err != nil {
		t.Fatal(err)
	}
	if _, err := rc.Reserve(ctx, DeviceRef{MAC: "0C-9B-78-EA-9C-00", Host: "10.9.9.9"}, time.Now().Add(time.Hour), ""); err == nil {
		t.Fatal("a reserved device was reserved again")
	}
	if _, err := rc.Reserve(ctx, DeviceRef{Host: "10.1.1.1"}, time.Now().Add(time.Hour), ""); err == nil {
		t.Fatal("a device outside the inventory was reserved")
	}
	_ = rc.Connected(ctx, DeviceRef{Host: "10.127.35.13", Protocol: "SSH2"})
	x, _ := oc.Status(ctx, 0, 0)
	if len(x.Reserved) != 1 || len(x.Notes) != 1 {
		t.Fatalf("status %+v", x)
	}
	if y, _ := oc.Status(ctx, 0, 0); len(y.Notes) != 0 {
		t.Fatal("a notice came twice")
	}
	// Ravi cannot release it; the owner of the reservation can
	if err := rc.Release(ctx, x.Reserved[0].ID); err == nil {
		t.Fatal("someone else released a reservation")
	}
	if err := oc.Release(ctx, x.Reserved[0].ID); err != nil {
		t.Fatal(err)
	}
	// a heartbeat that stops drops out
	s.mu.Lock()
	s.now = func() time.Time { return time.Now().Add(2 * presenceFor) }
	s.mu.Unlock()
	if z, _ := rc.Status(ctx, 0, 0); len(z.InUse) != 0 || len(z.Reserved) != 0 {
		t.Fatalf("after the heartbeat stopped %+v", z)
	}
}

func TestUsageHistory(t *testing.T) {
	s, code, ts := start(t)
	ctx := context.Background()
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	own, _ := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "Natarajan"})
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	_, _ = oc.Push(ctx, []Device{{Name: "core-sw1", Host: "10.0.0.1", Protocol: "SSH2", DeviceType: "VOSS"}, {Name: "build-server", Host: "10.0.0.9", Protocol: "SSH2", DeviceType: "Linux"}})
	base := time.Now()
	s.mu.Lock()
	s.now = func() time.Time { return base }
	s.mu.Unlock()
	_ = oc.Presence(ctx, []DeviceRef{{Host: "10.0.0.1", Protocol: "SSH2"}, {Host: "10.0.0.9", Protocol: "SSH2"}}) // the Linux server is not counted
	_, _ = oc.Reserve(ctx, DeviceRef{Host: "10.0.0.1"}, base.Add(2*time.Hour), "test")
	s.mu.Lock()
	s.now = func() time.Time { return base.Add(time.Hour) }
	s.mu.Unlock()
	_ = oc.Presence(ctx, nil) // closed the tab after an hour
	var rep UsageReport
	if err := oc.call(ctx, "GET", "/api/admin/usage?days=7", nil, &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Use) != 1 || rep.Use[0].E-rep.Use[0].S != 3600 || rep.Use[0].DevName != "core-sw1" {
		t.Fatalf("use %+v", rep.Use)
	}
	if len(rep.Res) != 1 || !rep.Res[0].Active {
		t.Fatalf("res %+v", rep.Res)
	}
	// the history survives a restart
	s2, _, _ := Open(s.Dir, "", "test", nil)
	if len(s2.usage.Use) != 1 {
		t.Fatalf("after restart %+v", s2.usage)
	}
}

// The same person on a second computer (a member with the same name) is not
// warned about, notified of or counted against their own reservation.
func TestSamePersonOnTwoComputers(t *testing.T) {
	s, code, ts := start(t)
	ctx := context.Background()
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	own, _ := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "Natarajan Periannan"})
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	tm, _ := oc.Team(ctx)
	vnc, _ := anon.Join(ctx, JoinRequest{Invite: tm.Invite, Name: "natarajan  periannan"})
	vc := NewClient(addr(ts), s.Fingerprint(), vnc.Token)
	_, _ = oc.Push(ctx, []Device{{Name: "core-sw1", Host: "10.127.35.13", Protocol: "SSH2"}})
	res, err := oc.Reserve(ctx, DeviceRef{Host: "10.127.35.13", Protocol: "SSH2"}, time.Now().Add(2*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	_ = vc.Connected(ctx, DeviceRef{Host: "10.127.35.13", Protocol: "SSH2"})
	x, _ := oc.Status(ctx, 0, 0)
	if len(x.Notes) != 0 || len(s.usage.Con) != 0 {
		t.Fatalf("a notice or conflict for my own reservation: %+v %+v", x.Notes, s.usage.Con)
	}
	if len(x.Also) != 1 || x.Also[0] != vnc.MemberID {
		t.Fatalf("also %+v", x.Also)
	}
	if _, err := vc.Reserve(ctx, DeviceRef{Host: "10.127.35.13", Protocol: "SSH2"}, time.Now().Add(3*time.Hour), "longer"); err != nil {
		t.Fatalf("my other computer could not change my reservation: %v", err)
	}
	if err := vc.Release(ctx, res.ID); err != nil {
		t.Fatalf("my other computer could not release my reservation: %v", err)
	}
	for _, a := range s.st.Activity {
		if strings.Contains(a.What, "UTC") {
			t.Fatalf("activity with a UTC time: %q", a.What)
		}
	}
}

func TestTracked(t *testing.T) {
	for typ, want := range map[string]bool{"": false, "EXOS": true, "VOSS": true, "FortiGate": true, "Linux": false, "Ubuntu server": false, "Windows": false,
		"IoT": false, "Printer": false, "Smart TV": false, "IP camera": false, "PDU": true, "Cisco IOS": true, "Synology NAS": false} {
		if Tracked(typ) != want {
			t.Errorf("Tracked(%q) = %v", typ, !want)
		}
	}
}
