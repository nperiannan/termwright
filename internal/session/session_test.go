package session

import "testing"

func TestNewConnectionOptionsRoundTrip(t *testing.T) {
	s := Session{Name: "x", Protocol: ProtoTelnet, Hostname: "h", LocalEcho: true, TelnetTLS: true, TLSAnyCert: true, Mosh: true}
	got := roundTripForTest(t, s)
	if !got.LocalEcho || !got.TelnetTLS || !got.TLSAnyCert || !got.Mosh {
		t.Fatalf("options lost: %+v", got)
	}
}

func roundTripForTest(t *testing.T, s Session) Session {
	t.Helper()
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(s, ""); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(s.Name)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A turned-off jump host and proxy are kept but not used.
func TestRouteOff(t *testing.T) {
	s := Session{Name: "r", Protocol: ProtoSSH2, Hostname: "h", JumpSession: "Lab/jump", ProxyType: "socks5", ProxyHost: "p", ProxyPort: 1080, RouteOff: true}
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(s, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get("r")
	if got.JumpSession != "" || got.ProxyType != "" || !got.RouteOff || got.OffRoute == nil || got.OffRoute.Jump != "Lab/jump" || got.OffRoute.ProxyPort != 1080 {
		t.Fatalf("off: %+v %+v", got, got.OffRoute)
	}
	// saved again while off (another field changed): still kept
	got.Hostname = "h2"
	if _, err := st.Save(got, "r"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get("r")
	if got.OffRoute == nil || got.OffRoute.Jump != "Lab/jump" || got.JumpSession != "" {
		t.Fatalf("lost while off: %+v", got.OffRoute)
	}
	// turned on again
	o := *got.OffRoute
	got.RouteOff, got.OffRoute, got.JumpSession, got.ProxyType, got.ProxyHost, got.ProxyPort = false, nil, o.Jump, o.ProxyType, o.ProxyHost, o.ProxyPort
	if _, err := st.Save(got, "r"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get("r")
	if got.JumpSession != "Lab/jump" || got.ProxyType != "socks5" || got.RouteOff {
		t.Fatalf("on: %+v", got)
	}
}
