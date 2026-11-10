package team

import (
	"context"
	"testing"

	"github.com/termwright/termwright/internal/discover"
)

func TestDeviceFactsAndScanNames(t *testing.T) {
	s, code, ts := start(t)
	ctx := context.Background()
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	own, _ := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "Owner"})
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	tm, _ := oc.Team(ctx)
	rv, _ := anon.Join(ctx, JoinRequest{Invite: tm.Invite, Name: "Ravi"})
	rc := NewClient(addr(ts), s.Fingerprint(), rv.Token)
	if _, err := oc.Push(ctx, []Device{{Name: "10.0.0.1", Host: "10.0.0.1", Protocol: "SSH2", DeviceType: "EXOS"}, {Name: "10.0.0.2", Host: "10.0.0.2", Protocol: "SSH2"}}); err != nil {
		t.Fatal(err)
	}
	// a member reports what it read; an IP is not a hostname; unknown devices are ignored
	if err := rc.Facts(ctx, DeviceRef{Host: "10.0.0.1", Port: 22, Protocol: "SSH2"}, "lab-x465-1", "X465-48T"); err != nil {
		t.Fatal(err)
	}
	_ = rc.Facts(ctx, DeviceRef{Host: "10.0.0.2", Port: 22, Protocol: "SSH2"}, "10.0.0.2", "")
	_ = rc.Facts(ctx, DeviceRef{Host: "10.9.9.9", Port: 22, Protocol: "SSH2"}, "x", "y")
	s.mu.Lock()
	d1, d2 := *s.st.Inventory[0], *s.st.Inventory[1]
	// a scan names the one still without a hostname, not the other
	n := s.namesFromScan([]discover.Result{{IP: "10.0.0.1", SysName: "other"}, {IP: "10.0.0.2", SysName: "core-vsp-1"}, {IP: "10.0.0.3", SysName: "x"}})
	d2b := *s.st.Inventory[1]
	s.mu.Unlock()
	if d1.Hostname != "lab-x465-1" || d1.Model != "X465-48T" || d2.Hostname != "" {
		t.Fatalf("facts %+v %+v", d1, d2)
	}
	if n != 1 || d2b.Hostname != "core-vsp-1" {
		t.Fatalf("scan names %d %+v", n, d2b)
	}
	// editing a device without the facts keeps them
	d1.Name = "lab-x465-1"
	d1.Hostname, d1.Model = "", ""
	if err := oc.SaveDevices(ctx, []Device{d1}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	d1c := *s.st.Inventory[0]
	s.mu.Unlock()
	if d1c.Name != "lab-x465-1" || d1c.Hostname != "lab-x465-1" || d1c.Model != "X465-48T" {
		t.Fatalf("edit lost the facts %+v", d1c)
	}
}
