package team

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOwnerHandsOverAndActivity(t *testing.T) {
	s, code, ts := start(t)
	ctx := context.Background()
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	own, _ := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "Natarajan"})
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	tm, _ := oc.Team(ctx)
	an, _ := anon.Join(ctx, JoinRequest{Invite: tm.Invite, Name: "Anisha"})
	ac := NewClient(addr(ts), s.Fingerprint(), an.Token)
	if err := ac.SetRole(ctx, own.MemberID, RoleOwner); err == nil {
		t.Fatal("a member took the team over")
	}
	if err := oc.SetRole(ctx, an.MemberID, RoleOwner); err != nil {
		t.Fatal(err)
	}
	x, _ := ac.Team(ctx)
	y, _ := oc.Team(ctx)
	if x.Role != RoleOwner || y.Role != RoleCoOwner {
		t.Fatalf("roles after hand-over: %s %s", x.Role, y.Role)
	}
	if st, _ := ac.Status(ctx, 0, 0); len(st.Notes) != 1 {
		t.Fatalf("no notice for the new owner: %+v", st.Notes)
	}
	// the activity page: newest first, filtered
	for i := 0; i < 3; i++ {
		_, _ = oc.AdminLink(ctx)
	}
	var page struct {
		Total int        `json:"total"`
		Items []Activity `json:"items"`
	}
	if err := ac.call(ctx, "GET", "/api/admin/activity?q=owner&limit=10", nil, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || !strings.Contains(page.Items[0].What, "the owner") {
		t.Fatalf("activity %+v", page)
	}
}

func TestSignInNotedOnce(t *testing.T) {
	s, _, _ := start(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	m := &memberRec{Member: Member{ID: "m1", Name: "N", Role: RoleOwner}}
	s.st.Members = append(s.st.Members, m)
	n0 := len(s.st.Activity)
	for i := 0; i < 3; i++ {
		now := s.now()
		if last, ok := s.signIns[m.ID]; !ok || now.Sub(last) > sessionFor {
			s.signIns[m.ID] = now
			s.log(m.Name, "signed in to the admin page")
		}
	}
	if len(s.st.Activity)-n0 != 1 {
		t.Fatalf("%d sign-ins noted", len(s.st.Activity)-n0)
	}
}

func TestWatchUntilFree(t *testing.T) {
	s, code, ts := start(t)
	ctx := context.Background()
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	own, _ := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "Natarajan"})
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	tm, _ := oc.Team(ctx)
	ra, _ := anon.Join(ctx, JoinRequest{Invite: tm.Invite, Name: "Ravi"})
	rc := NewClient(addr(ts), s.Fingerprint(), ra.Token)
	_, _ = oc.Push(ctx, []Device{{Name: "core-sw1", Host: "10.0.0.1", Protocol: "SSH2", DeviceType: "VOSS"}})
	dev := DeviceRef{Host: "10.0.0.1", Protocol: "SSH2"}
	if free, err := rc.Watch(ctx, dev, true); err != nil || !free {
		t.Fatalf("a free device was not reported free: %v %v", free, err)
	}
	_ = oc.Presence(ctx, []DeviceRef{dev})
	if free, err := rc.Watch(ctx, dev, true); err != nil || free {
		t.Fatalf("watch: %v %v", free, err)
	}
	if st, _ := rc.Status(ctx, 0, 0); len(st.Watching) != 1 || len(st.Notes) != 0 {
		t.Fatalf("watching %+v notes %+v", st.Watching, st.Notes)
	}
	_ = oc.Presence(ctx, nil) // Natarajan closes it
	st, _ := rc.Status(ctx, 0, 0)
	if len(st.Notes) != 1 || !strings.Contains(st.Notes[0].Text, "is free now") || len(st.Watching) != 0 {
		t.Fatalf("after it got free: notes %+v watching %+v", st.Notes, st.Watching)
	}
}

func TestBackupAndRestore(t *testing.T) {
	s, code, ts := start(t)
	ctx := context.Background()
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	own, _ := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "Natarajan"})
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	_, _ = oc.Push(ctx, []Device{{Name: "core-sw1", Host: "10.0.0.1", Protocol: "SSH2", DeviceType: "VOSS"}})
	b, err := oc.Backup(ctx)
	if err != nil || len(b) < 100 {
		t.Fatalf("backup %d bytes, %v", len(b), err)
	}
	files, err := readBackup(strings.NewReader(string(b)))
	if err != nil || files["key.pem"] == nil || files["cert.pem"] == nil {
		t.Fatalf("backup files %v %v", len(files), err)
	}
	// a change after the backup goes away with the restore
	_, _ = oc.Push(ctx, []Device{{Name: "core-sw2", Host: "10.0.0.2", Protocol: "SSH2", DeviceType: "VOSS"}})
	tm, _ := oc.Team(ctx)
	ra, _ := anon.Join(ctx, JoinRequest{Invite: tm.Invite, Name: "Ravi"})
	rc := NewClient(addr(ts), s.Fingerprint(), ra.Token)
	if _, err := rc.Backup(ctx); err == nil {
		t.Fatal("a member downloaded a backup")
	}
	if err := rc.Restore(ctx, b); err == nil {
		t.Fatal("a member restored a backup")
	}
	if err := oc.Restore(ctx, []byte("not a backup")); err == nil {
		t.Fatal("garbage was restored")
	}
	if err := oc.Restore(ctx, b); err != nil {
		t.Fatal(err)
	}
	inv, _ := oc.Inventory(ctx, 0, 0)
	if len(inv.Devices) != 1 {
		t.Fatalf("inventory after restore: %d devices", len(inv.Devices))
	}
	if _, err := rc.Team(ctx); err == nil {
		t.Fatal("a member who joined after the backup is still in")
	}
	_ = time.Second
}
