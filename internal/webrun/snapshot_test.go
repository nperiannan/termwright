package webrun

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// An AI (or anything) reads the page's elements and acts with their targets.
func TestSnapshotThenAct(t *testing.T) {
	pw := os.Getenv("TW_PLAYWRIGHT")
	if pw == "" {
		t.Skip("TW_PLAYWRIGHT not set")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	os.Symlink(pw, filepath.Join(dir, "node_modules", "playwright"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/devices" {
			fmt.Fprint(w, `<html><title>Devices</title><body><h1>Devices</h1>
<label for="sn">Serial number</label><input id="sn">
<select aria-label="Site"><option>Lab A</option><option>Lab B</option></select>
<label><input type="checkbox" name="mgmt"> Managed</label>
<button data-testid="onboard" onclick="document.getElementById('out').textContent='Onboarded '+document.getElementById('sn').value">Onboard</button>
<p id="out"></p><button disabled>Delete</button><div style="display:none"><button>Hidden</button></div></body></html>`)
			return
		}
		if c, err := r.Cookie("sid"); err == nil && c.Value == "ok" {
			fmt.Fprint(w, `<html><title>App</title><body><h1>Dashboard</h1><a href="/devices">Devices</a></body></html>`)
			return
		}
		fmt.Fprint(w, shadowLogin)
	}))
	defer srv.Close()
	r := Runner{Dir: dir}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s, err := r.Open(ctx, Step{URL: srv.URL}, dir, Extra{Root: dir, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sn, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	find := func(sn Snapshot, f func(SnapshotElement) bool) *SnapshotElement {
		for i := range sn.Elements {
			if f(sn.Elements[i]) {
				return &sn.Elements[i]
			}
		}
		return nil
	}
	email := find(sn, func(e SnapshotElement) bool { return e.Name == "Email" })
	pass := find(sn, func(e SnapshotElement) bool { return e.Secret })
	login := find(sn, func(e SnapshotElement) bool { return e.Role == "button" && e.Name == "Log In" })
	if email == nil || pass == nil || login == nil {
		t.Fatalf("login page: %+v", sn.Elements)
	}
	res, err := s.Run([]Action{{Do: "fill", Target: email.Target, Value: "qa@x.com"}, {Do: "fill", Target: pass.Target, Value: "pw1"}, {Do: "click", Target: login.Target}}, 0)
	if err != nil || len(res) != 3 {
		t.Fatalf("%v %+v", err, res)
	}
	time.Sleep(500 * time.Millisecond)
	sn, _ = s.Snapshot()
	link := find(sn, func(e SnapshotElement) bool { return e.Role == "link" && e.Name == "Devices" })
	if link == nil || len(sn.Headings) == 0 || sn.Headings[0] != "Dashboard" {
		t.Fatalf("dashboard: %+v", sn)
	}
	if _, err := s.Run([]Action{{Do: "click", Target: link.Target}}, 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	sn, _ = s.Snapshot()
	serial := find(sn, func(e SnapshotElement) bool { return e.Role == "textbox" })
	site := find(sn, func(e SnapshotElement) bool { return e.Role == "combobox" })
	box := find(sn, func(e SnapshotElement) bool { return e.Role == "checkbox" })
	onb := find(sn, func(e SnapshotElement) bool { return e.Name == "Onboard" })
	del := find(sn, func(e SnapshotElement) bool { return e.Name == "Delete" })
	if serial == nil || serial.Target != "label=Serial number" || site == nil || len(site.Options) != 2 || box == nil || onb == nil || onb.Target != "testid=onboard" ||
		del == nil || !del.Disabled || find(sn, func(e SnapshotElement) bool { return e.Name == "Hidden" }) != nil {
		t.Fatalf("devices page: %+v", sn.Elements)
	}
	res, err = s.Run([]Action{{Do: "fill", Target: serial.Target, Value: "SN123"}, {Do: "select", Target: site.Target, Value: "Lab B"}, {Do: "check", Target: box.Target},
		{Do: "click", Target: onb.Target}, {Do: "expect", Target: "#out", Value: "Onboarded SN123"}}, 0)
	if err != nil || len(res) != 5 || !res[4].OK {
		t.Fatalf("%v %+v", err, res)
	}
}
