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

// A turned-off jump host or proxy is kept but not used, each on its own.
func TestRouteOff(t *testing.T) {
	s := Session{Name: "r", Protocol: ProtoSSH2, Hostname: "h", JumpSession: "Lab/jump", ProxyType: "socks5", ProxyHost: "p", ProxyPort: 1080, JumpOff: true}
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(s, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get("r")
	if got.JumpSession != "" || got.OffJump != "Lab/jump" || got.ProxyType != "socks5" || got.ProxyOff {
		t.Fatalf("jump off: %+v", got)
	}
	got.ProxyOff, got.OffProxy, got.ProxyType, got.ProxyHost, got.ProxyPort = true, &Proxy{Type: "socks5", Host: "p", Port: 1080}, "", "", 0
	got.Hostname = "h2"
	if _, err := st.Save(got, "r"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get("r")
	if got.ProxyType != "" || got.OffProxy == nil || got.OffProxy.Port != 1080 || got.OffJump != "Lab/jump" || got.JumpSession != "" {
		t.Fatalf("both off: %+v %+v", got, got.OffProxy)
	}
	got.JumpOff, got.JumpSession, got.OffJump = false, got.OffJump, ""
	if _, err := st.Save(got, "r"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get("r")
	if got.JumpSession != "Lab/jump" || got.JumpOff || !got.ProxyOff || got.OffProxy == nil {
		t.Fatalf("jump on again: %+v", got)
	}
}
