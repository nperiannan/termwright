package team

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/discover"
)

// TestAdminUIServe serves the admin page with a finished scan for a browser
// test (TW_ADMIN_SERVE=seconds): it prints the URL and the admin cookie.
func TestAdminUIServe(t *testing.T) {
	secs := os.Getenv("TW_ADMIN_SERVE")
	if secs == "" {
		t.Skip("TW_ADMIN_SERVE not set")
	}
	s, code, ts := start(t)
	own, err := NewClient(addr(ts), s.Fingerprint(), "").Join(context.Background(), JoinRequest{Invite: code, Name: "Owner", User: "o", Machine: "M"})
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.sessions[hashToken("uitest")] = adminSession{member: own.MemberID, until: time.Now().Add(time.Hour)}
	s.scan = &scanState{ID: "x", Targets: "10.60.25.0/24", By: "Owner", Done: 254, Total: 254, Started: time.Now().Format(time.RFC3339),
		Results: []discover.Result{{IP: "10.60.25.254", Name: "10.60.25.254", Kind: "switch", Vendor: "Extreme Networks", OS: "EXOS", Services: []string{"telnet", "http"}},
			{IP: "10.60.25.253", Name: "sw-2", Kind: "switch", Vendor: "Extreme Networks", OS: "VOSS", Services: []string{"ssh"}}}}
	at := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	for i, d := range []Device{
		{Name: "5520-24t-1", Host: "10.60.34.1", Folder: "10.60.34.0", DeviceType: "EXOS", Protocol: "Telnet", Model: "5520-24T", Serial: "2133N-41234", BaseMAC: "00:04:96:9f:12:30", FactsBy: "server", FactsAt: at},
		{Name: "dist-5720-1", Host: "10.60.34.3", Folder: "10.60.34.0", DeviceType: "VOSS", Protocol: "SSH2", Model: "5720-24MXW", Serial: "2243Q-20456", BaseMAC: "dc:23:3b:4a:10:00", FactsBy: "Owner", FactsAt: at},
		{Name: "acc-5520-48t-2", Host: "10.60.34.9", Folder: "10.60.34.0", DeviceType: "EXOS", Protocol: "SSH2", Model: "5520-48T", Serial: "2201N-40877", BaseMAC: "00:04:96:a3:44:10", FactsBy: "server", FactsAt: at},
		{Name: "10.60.34.13", Host: "10.60.34.13", Folder: "10.60.34.0", DeviceType: "EXOS", Protocol: "SSH2", MAC: "52:54:00:1a:2b:3c", FactsBy: "server", FactsAt: at, FactsErr: "the login admin was refused (set it on Discover → Device logins)"},
		{Name: "10.60.3.34", Host: "10.60.3.34", Folder: "10.60.3.X/Linux", DeviceType: "Linux", Protocol: "SSH2", MAC: "52:54:00:77:01:09"},
	} {
		d.ID = fmt.Sprintf("d%d", i)
		s.st.Inventory = append(s.st.Inventory, &d)
	}
	s.mu.Unlock()
	fmt.Printf("URL %s/admin COOKIE %s\n", ts.URL, cookieName)
	var n int
	fmt.Sscan(secs, &n)
	time.Sleep(time.Duration(n) * time.Second)
}
