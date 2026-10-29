package term

import (
	"testing"

	"github.com/termwright/termwright/internal/session"
)

func TestAccountPrecedence(t *testing.T) {
	m := NewManager(&recEmitter{}, t.TempDir(), func() string { return t.TempDir() })
	defer m.CloseAll()
	m.DeviceType = func(name string) (session.DeviceType, bool) {
		return session.DeviceType{Name: name, Username: "dtuser", Credential: map[string]string{"EXOS": "radius"}[name]}, true
	}
	m.Credential = func(name string) (string, string, bool) {
		switch name {
		case "lab":
			return "admin", "labpw", true
		case "radius":
			return "nperiannan", "", true
		}
		return "", "", false
	}
	m.SavedPassword = func(string) (string, bool) { return "own", true }
	for _, c := range []struct {
		s       session.Session
		account string
		pw      string
		saved   bool
	}{
		{session.Session{Path: "a", Account: "lab"}, "lab", "labpw", true},
		{session.Session{Path: "b", Account: "lab", DeviceType: "EXOS"}, "lab", "labpw", true}, // own choice wins over the device type
		{session.Session{Path: "c", DeviceType: "EXOS"}, "radius", "", false},                  // device type's account, no saved password: asked
		{session.Session{Path: "d", Account: "-", DeviceType: "EXOS"}, "", "own", true},        // own login
		{session.Session{Path: "e", OwnLogin: true, DeviceType: "EXOS"}, "", "own", true},      // older "own login" sessions
		{session.Session{Path: "f"}, "", "own", true},
	} {
		if got := m.AccountOf(c.s); got != c.account {
			t.Errorf("%s: account %q, want %q", c.s.Path, got, c.account)
		}
		if pw, ok := m.loginPassword(c.s); pw != c.pw || ok != c.saved {
			t.Errorf("%s: password %q/%v, want %q/%v", c.s.Path, pw, ok, c.pw, c.saved)
		}
	}
}
