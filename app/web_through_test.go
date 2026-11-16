package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/session"
)

func TestWebThroughError(t *testing.T) {
	for _, c := range []struct{ err, want string }{
		{"ssh: rejected: administratively prohibited (open failed)", "TCP forwarding off"},
		{"ssh: rejected: connect failed (unknown host)", "cannot find www.example.com"},
		{"no answer from www.example.com:443 within 15s", "can it reach the internet"},
		{"something else", "could not open a connection"},
	} {
		if err := webThroughError("5520-24t-1", "www.example.com", errors.New(c.err)); !strings.Contains(err.Error(), c.want) || !strings.HasPrefix(err.Error(), "5520-24t-1 ") {
			t.Errorf("%s: %v", c.err, err)
		}
	}
}

// Switches whose SSH server does not forward (EXOS, VOSS, Cisco IOS) get no page through them: said at once.
func TestWebThroughNoTunnel(t *testing.T) {
	for _, c := range []struct {
		dt   session.DeviceType
		want bool
	}{{session.DeviceType{Name: "EXOS"}, false}, {session.DeviceType{Name: "exos", Tunnel: "on"}, true}, {session.DeviceType{Name: "VOSS"}, false},
		{session.DeviceType{Name: "Cisco IOS"}, false}, {session.DeviceType{Name: "Linux"}, true}, {session.DeviceType{Name: "Mine", Tunnel: "off"}, false}} {
		if c.dt.Tunnels() != c.want {
			t.Errorf("%+v: %v", c.dt, !c.want)
		}
	}
	a := emuTestApp(t)
	_, err := a.OpenWebPage("", session.Session{Name: "5520-24t-1", Hostname: "10.0.0.1", Protocol: session.ProtoSSH2, DeviceType: "EXOS"}, "session", "http://www.google.com", "")
	if err == nil || !strings.Contains(err.Error(), "does not pass connections on") || !strings.Contains(err.Error(), "Open Web Interface") {
		t.Fatalf("EXOS: %v", err)
	}
}
