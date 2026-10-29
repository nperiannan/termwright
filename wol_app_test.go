package main

import (
	"testing"

	"github.com/termwright/termwright/internal/session"
)

func TestWOLBroadcastIsTheDevicesNetwork(t *testing.T) {
	for _, c := range []struct {
		s    session.Session
		want string
	}{
		{session.Session{Protocol: session.ProtoWOL, WOLBroadcast: "10.9.9.255", WOLNetwork: "192.168.1.0/24"}, "10.9.9.255"},
		{session.Session{Protocol: session.ProtoWOL, WOLNetwork: "192.168.20.0/24"}, "192.168.20.255"},
		{session.Session{Protocol: session.ProtoWOL, WOLNetwork: "10.1.0.0/22"}, "10.1.3.255"},
		{session.Session{Protocol: session.ProtoWOL, WOLNetwork: "172.16.5.0"}, "172.16.5.255"},
		{session.Session{Protocol: session.ProtoWOL, WOLNetwork: "172.16.6"}, "172.16.6.255"},
		{session.Session{Protocol: session.ProtoWOL, WOLNetwork: "192.168.1.41"}, "192.168.1.255"},
		{session.Session{Protocol: session.ProtoWOL, Hostname: "203.0.113.7"}, "203.0.113.255"},
		{session.Session{Protocol: session.ProtoSSH2, Hostname: "198.51.100.20", Port: 22}, "198.51.100.255"},
		{session.Session{Protocol: session.ProtoWOL}, ""},
	} {
		if got := wolBroadcast(c.s); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.s, got, c.want)
		}
	}
}
