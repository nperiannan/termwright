package team

import (
	"context"
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
	_, _ = oc.Push(ctx, []Device{{Name: "core-sw1", Host: "10.127.35.13", Protocol: "SSH2", MAC: "0c:9b:78:ea:9c:00"}})

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
