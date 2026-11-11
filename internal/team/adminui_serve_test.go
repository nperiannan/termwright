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
	s.scan = &scanState{ID: "x", Targets: "10.127.25.0/24", By: "Owner", Done: 254, Total: 254, Started: time.Now().Format(time.RFC3339),
		Results: []discover.Result{{IP: "10.127.25.254", Name: "10.127.25.254", Kind: "switch", Vendor: "Extreme Networks", OS: "EXOS", Services: []string{"telnet", "http"}},
			{IP: "10.127.25.253", Name: "sw-2", Kind: "switch", Vendor: "Extreme Networks", OS: "VOSS", Services: []string{"ssh"}}}}
	s.mu.Unlock()
	fmt.Printf("URL %s/admin COOKIE %s\n", ts.URL, cookieName)
	var n int
	fmt.Sscan(secs, &n)
	time.Sleep(time.Duration(n) * time.Second)
}
