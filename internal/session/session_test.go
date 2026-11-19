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

// SNMP and REST API settings are kept in the session file; secret variables keep their name only.
func TestSNMPAndAPIRoundTrip(t *testing.T) {
	s := Session{Name: "sw", Protocol: ProtoSSH2, Hostname: "10.0.0.1", SNMPVersion: "3", SNMPPort: 1161, SNMPHost: "10.0.0.2", SNMPUser: "netops",
		SNMPAuth: "SHA-256", SNMPPriv: "AES-128", SNMPContext: "c1", SNMPWrite: true}
	got := roundTripForTest(t, s)
	if got.SNMPVersion != "3" || got.SNMPPort != 1161 || got.SNMPHost != "10.0.0.2" || got.SNMPUser != "netops" || got.SNMPAuth != "SHA-256" ||
		got.SNMPPriv != "AES-128" || got.SNMPContext != "c1" || !got.SNMPWrite {
		t.Fatalf("snmp lost: %+v", got)
	}
	a := Session{Name: "rest", Protocol: ProtoAPI, APIBaseURL: "https://10.0.0.1", APIAuth: "login", APILogin: "POST /auth/token", APITokenPath: "data.token",
		APIKeyName: "X-Key", APIKeyIn: "query", APITokenURL: "https://idp/token", APIClientID: "cid", APIScope: "read", APIInsecure: true, APICAFile: "/ca.pem",
		APIVars: []APIVar{{Name: "site", Value: "lab\tA", Prod: "dc"}, {Name: "key", Value: "s3cret", Secret: true}}}
	g := roundTripForTest(t, a)
	if g.Protocol != ProtoAPI || g.APIBaseURL != a.APIBaseURL || g.APIAuth != "login" || g.APILogin != a.APILogin || g.APITokenPath != "data.token" ||
		g.APIKeyName != "X-Key" || g.APIKeyIn != "query" || g.APITokenURL != a.APITokenURL || g.APIClientID != "cid" || g.APIScope != "read" || !g.APIInsecure || g.APICAFile != "/ca.pem" {
		t.Fatalf("api lost: %+v", g)
	}
	if len(g.APIVars) != 2 || g.APIVars[0] != a.APIVars[0] || g.APIVars[1] != (APIVar{Name: "key", Secret: true}) {
		t.Fatalf("vars: %+v", g.APIVars)
	}
}
