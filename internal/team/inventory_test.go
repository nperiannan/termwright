package team

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

func TestInventoryPushApproveAndAdmin(t *testing.T) {
	s, code, ts := start(t)
	ctx := context.Background()
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	own, _ := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "Owner"})
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	tm, _ := oc.Team(ctx)
	rv, _ := anon.Join(ctx, JoinRequest{Invite: tm.Invite, Name: "Ravi"})
	rc := NewClient(addr(ts), s.Fingerprint(), rv.Token)

	// the owner pushes: added at once
	res, err := oc.Push(ctx, []Device{{Name: "core-sw1", Folder: "Core", Host: "10.0.0.1", Protocol: "SSH2", DeviceType: "VOSS"}})
	if err != nil || res[0].Status != "added" {
		t.Fatalf("owner push %+v %v", res, err)
	}
	inv, err := rc.Inventory(ctx, 0, 0)
	if err != nil || inv.Rev != 1 || len(inv.Devices) != 1 {
		t.Fatalf("inventory %+v %v", inv, err)
	}
	// a member pushes the same device under another name, and a new one
	res, err = rc.Push(ctx, []Device{{Name: "my-core", Host: "10.0.0.1", Port: 22}, {Name: "edge-1", Host: "10.0.0.9", Protocol: "Telnet"}})
	if err != nil || res[0].Status != "exists" || res[0].Name != "core-sw1" || res[1].Status != "pending" {
		t.Fatalf("member push %+v %v", res, err)
	}
	if chk, _ := rc.Check(ctx, []Device{{Host: "10.0.0.9", Protocol: "Telnet"}}); chk[0].Status != "pending" {
		t.Fatalf("check %+v", chk)
	}
	// a long poll wakes up on the approval
	got := make(chan Inventory, 1)
	go func() { i, _ := rc.Inventory(ctx, inv.Rev, 20); got <- i }()
	time.Sleep(150 * time.Millisecond)
	s.mu.Lock()
	pid := s.st.Pending[0].ID
	s.mu.Unlock()

	// the admin page: a link signs in once, then the cookie works
	link, err := oc.AdminLink(ctx)
	if err != nil || !strings.HasPrefix(link, "/admin#c=") {
		t.Fatalf("link %q %v", link, err)
	}
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	post := func(path, body string, hdr bool) *http.Response {
		req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if hdr {
			req.Header.Set("X-Termwright", "1")
		}
		r, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r
	}
	c := strings.TrimPrefix(link, "/admin#c=")
	if r := post("/api/admin/session", `{"code":"`+c+`"}`, true); r.StatusCode != 200 {
		t.Fatalf("sign in %d", r.StatusCode)
	}
	if r := post("/api/admin/session", `{"code":"`+c+`"}`, true); r.StatusCode != 401 {
		t.Fatalf("a link worked twice: %d", r.StatusCode)
	}
	if r := post("/api/admin/pending/approve", `{"ids":["`+pid+`"]}`, false); r.StatusCode != 403 {
		t.Fatalf("a post without the header was taken: %d", r.StatusCode)
	}
	if r := post("/api/admin/pending/approve", `{"ids":["`+pid+`"],"folder":"Edge"}`, true); r.StatusCode != 200 {
		t.Fatalf("approve %d", r.StatusCode)
	}
	select {
	case i := <-got:
		if len(i.Devices) != 2 || i.Devices[1].Folder != "Edge" || i.Devices[1].AddedBy != "Ravi" {
			t.Fatalf("after approval %+v", i.Devices)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the long poll did not wake up")
	}
	r, _ := hc.Get(ts.URL + "/api/admin/state")
	var as AdminState
	_ = json.NewDecoder(r.Body).Decode(&as)
	r.Body.Close()
	if len(as.Activity) == 0 || !strings.Contains(as.Invite, "termwright-team://") || len(as.Pending) != 0 {
		t.Fatalf("admin state %+v", as)
	}
	// a member cannot sign in to the admin page
	if _, err := rc.AdminLink(ctx); err == nil {
		t.Fatal("a member got an admin link")
	}
	if pg, _ := hc.Get(ts.URL + "/admin"); pg.StatusCode != 200 {
		t.Fatalf("admin page %d", pg.StatusCode)
	}
}
