package team

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func start(t *testing.T) (*Server, string, *httptest.Server) {
	t.Helper()
	s, code, err := Open(t.TempDir(), "Switching Lab B", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(s.Handler())
	ts.TLS = s.TLSConfig()
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return s, code, ts
}

func addr(ts *httptest.Server) string { return strings.TrimPrefix(ts.URL, "https://") }

func TestTeamJoinRolesInvite(t *testing.T) {
	s, code, ts := start(t)
	ctx := context.Background()
	if code == "" {
		t.Fatal("no owner code at the first start")
	}
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	if _, err := NewClient(addr(ts), strings.Repeat("0", 64), "").Info(ctx); err != ErrFingerprint {
		t.Fatalf("wrong fingerprint accepted: %v", err)
	}
	if i, err := anon.Info(ctx); err != nil || i.Name != "Switching Lab B" || i.Owner {
		t.Fatalf("info %+v %v", i, err)
	}
	if _, err := anon.Join(ctx, JoinRequest{Invite: "nope", Name: "x"}); err == nil {
		t.Fatal("wrong invite accepted")
	}
	s.fails = map[string]time.Time{}
	own, err := anon.Join(ctx, JoinRequest{Invite: code, Name: "Natarajan", User: "np", Machine: "NP-Mac"}) // a hand-made invite with the setup code
	if err != nil || own.Role != RoleOwner {
		t.Fatalf("owner join %+v %v", own, err)
	}
	if _, err := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "again"}); err == nil {
		t.Fatal("owner code used twice")
	}
	s.fails = map[string]time.Time{}
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	tm, err := oc.Team(ctx)
	if err != nil || tm.Invite == "" || len(tm.Members) != 1 {
		t.Fatalf("team %+v %v", tm, err)
	}
	iv := Invitation{Address: addr(ts), Key: tm.Invite, Fingerprint: s.Fingerprint(), Name: tm.Name}
	p, err := ParseInvite(" " + iv.String()[:20] + "\n" + iv.String()[20:])
	if err != nil || p != iv {
		t.Fatalf("invite round trip %+v %v", p, err)
	}
	rv, err := anon.Join(ctx, JoinRequest{Invite: p.Key, Name: "Ravi", User: "ravi", Machine: "RK"})
	if err != nil || rv.Role != RoleMember {
		t.Fatalf("member join %+v %v", rv, err)
	}
	rc := NewClient(addr(ts), s.Fingerprint(), rv.Token)
	if mt, _ := rc.Team(ctx); mt.Invite != "" || len(mt.Members) != 2 {
		t.Fatalf("a member sees the invite or misses members: %+v", mt)
	}
	if _, err := rc.RotateInvite(ctx); err == nil {
		t.Fatal("a member rotated the invite")
	}
	if err := oc.SetRole(ctx, rv.MemberID, RoleCoOwner); err != nil {
		t.Fatal(err)
	}
	newKey, err := rc.RotateInvite(ctx)
	if err != nil || newKey == p.Key {
		t.Fatalf("co-owner rotate %v", err)
	}
	if _, err := anon.Join(ctx, JoinRequest{Invite: p.Key, Name: "late"}); err == nil {
		t.Fatal("old invite still works")
	}
	if err := rc.Remove(ctx, own.MemberID); err == nil {
		t.Fatal("the owner was removed")
	}
	if err := oc.Leave(ctx); err == nil {
		t.Fatal("the owner left a team with members")
	}
	if err := oc.Remove(ctx, rv.MemberID); err != nil {
		t.Fatal(err)
	}
	if _, err := rc.Team(ctx); !Unauthorized(err) {
		t.Fatalf("a removed member still gets in: %v", err)
	}
	// a restart keeps the team, the certificate and gives no new owner code
	s2, code2, err := Open(s.Dir, "other", "test", nil)
	if err != nil || code2 != "" || s2.Fingerprint() != s.Fingerprint() || s2.st.Name != "Switching Lab B" {
		t.Fatalf("restart: code %q fp %v name %q %v", code2, s2.Fingerprint() == s.Fingerprint(), s2.st.Name, err)
	}
}
