package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/clirun"

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
	if err := rc.Facts(ctx, DeviceRef{Host: "10.0.0.1", Port: 22, Protocol: "SSH2"}, "lab-x465-1", "X465-48T", "1234N-0001", "00-04-96-AA-BB-CC"); err != nil {
		t.Fatal(err)
	}
	_ = rc.Facts(ctx, DeviceRef{Host: "10.0.0.2", Port: 22, Protocol: "SSH2"}, "10.0.0.2", "", "", "")
	_ = rc.Facts(ctx, DeviceRef{Host: "10.9.9.9", Port: 22, Protocol: "SSH2"}, "x", "y", "", "")
	s.mu.Lock()
	d1, d2 := *s.st.Inventory[0], *s.st.Inventory[1]
	// a scan names the one still without a hostname, not the other
	n := s.namesFromScan([]discover.Result{{IP: "10.0.0.1", SysName: "other"}, {IP: "10.0.0.2", SysName: "core-vsp-1"}, {IP: "10.0.0.3", SysName: "x"}})
	d2b := *s.st.Inventory[1]
	s.mu.Unlock()
	if d1.Hostname != "lab-x465-1" || d1.Model != "X465-48T" || d1.Serial != "1234N-0001" || d1.BaseMAC != "00:04:96:aa:bb:cc" || d1.FactsBy != "Ravi" || d2.Hostname != "" {
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
	if d1c.Name != "lab-x465-1" || d1c.Hostname != "lab-x465-1" || d1c.Model != "X465-48T" || d1c.Serial != "1234N-0001" || d1c.BaseMAC == "" {
		t.Fatalf("edit lost the facts %+v", d1c)
	}
}

// The server reads device facts with the login for each kind of device: the factory default until one is
// set; a login kept when its password is not typed again; devices added get read; others are skipped with
// the reason; a refused login is shown on the device. Members never see the logins.
func TestServerReadsFacts(t *testing.T) {
	s, code, ts := start(t)
	ctx := context.Background()
	anon := NewClient(addr(ts), s.Fingerprint(), "")
	own, _ := anon.Join(ctx, JoinRequest{OwnerCode: code, Name: "Owner"})
	oc := NewClient(addr(ts), s.Fingerprint(), own.Token)
	var mu sync.Mutex
	seen := map[string]clirun.Target{}
	s.mu.Lock()
	s.runCLI = func(_ context.Context, t clirun.Target, paging string, cmds []string) ([]string, string, error) {
		mu.Lock()
		seen[t.Host] = t
		mu.Unlock()
		if t.Password == "wrong" {
			return nil, "SHA256:k", clirun.ErrLogin
		}
		if paging != "disable clipaging" || len(cmds) != 2 {
			return nil, "", fmt.Errorf("commands %q %q", paging, cmds)
		}
		return []string{"SysName: lab-5520-1\nSystem MAC: 00:04:96:9F:12:30\nSystem Type: 5520-24T\n", "Switch      : 800744-00-04 2133N-41234 Rev 4.0\n"}, "SHA256:k", nil
	}
	s.mu.Unlock()
	wait := func() {
		for i := 0; i < 200; i++ {
			s.mu.Lock()
			r := s.facts.Running
			s.mu.Unlock()
			if !r {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("still reading")
	}
	// added devices are read with the factory default login (EXOS: admin, no password)
	if err := oc.SaveDevices(ctx, []Device{{Name: "a", Host: "10.0.0.1", Protocol: "SSH2", DeviceType: "EXOS"}, {Name: "l", Host: "10.0.0.9", Protocol: "SSH2", DeviceType: "Linux"}}); err != nil {
		t.Fatal(err)
	}
	wait()
	s.mu.Lock()
	d := *s.st.Inventory[0]
	s.mu.Unlock()
	if tg := seen["10.0.0.1"]; tg.User != "admin" || tg.Password != "" || seen["10.0.0.9"].Host != "" {
		t.Fatalf("logins used %+v", seen)
	}
	if d.Model != "5520-24T" || d.Serial != "2133N-41234" || d.BaseMAC != "00:04:96:9f:12:30" || d.Hostname != "lab-5520-1" || d.FactsBy != "server" || d.FactsErr != "" || s.st.HostKeys["10.0.0.1:22"] != "SHA256:k" {
		t.Fatalf("facts %+v %v", d, s.st.HostKeys)
	}
	// a login set: kept without typing its password again; never in what members fetch
	var lv struct{ Logins []LoginView }
	if err := oc.call(ctx, "POST", "/api/admin/logins", map[string]any{"logins": []map[string]string{{"family": "exos", "user": "lab", "password": "wrong"}}, "factsOnAdd": true}, &lv); err != nil {
		t.Fatal(err)
	}
	if err := oc.call(ctx, "POST", "/api/admin/logins", map[string]any{"logins": []map[string]string{{"family": "exos", "user": "lab", "password": ""}, {"family": "voss", "user": "rwa"}}, "factsOnAdd": true}, &lv); err != nil {
		t.Fatal(err)
	}
	if l := s.st.Logins["exos"]; l.User != "lab" || l.Password != "wrong" || lv.Logins[0].IsDefault || !lv.Logins[1].IsDefault || !lv.Logins[1].HasPassword {
		t.Fatalf("logins %+v %+v", s.st.Logins, lv.Logins)
	}
	inv, _ := oc.Inventory(ctx, 0, 0)
	if b, _ := json.Marshal(inv); strings.Contains(string(b), "wrong") {
		t.Fatal("a login in the inventory")
	}
	// read again: the login is refused; the Linux device is skipped with the reason
	var rr struct {
		Started, Skipped int
		Why              string
	}
	if err := oc.call(ctx, "POST", "/api/admin/facts", map[string]any{"ids": []string{d.ID, s.st.Inventory[1].ID}}, &rr); err != nil || rr.Started != 1 || rr.Skipped != 1 || !strings.Contains(rr.Why, "not EXOS") {
		t.Fatalf("read %v %+v", err, rr)
	}
	wait()
	s.mu.Lock()
	d = *s.st.Inventory[0]
	s.mu.Unlock()
	if !strings.Contains(d.FactsErr, "the login lab was refused") || d.Serial != "2133N-41234" {
		t.Fatalf("refused %+v", d)
	}
	// back to the factory default; off when added
	if err := oc.call(ctx, "POST", "/api/admin/logins", map[string]any{"reset": "exos", "factsOnAdd": false}, &lv); err != nil || !lv.Logins[0].IsDefault || lv.Logins[0].User != "admin" || !s.st.NoFactsOnAdd {
		t.Fatalf("reset %v %+v", err, lv.Logins)
	}
}
