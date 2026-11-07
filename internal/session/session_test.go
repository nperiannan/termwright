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
