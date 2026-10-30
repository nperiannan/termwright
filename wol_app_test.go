package main

import (
	"net"
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

func TestWakeFromHere(t *testing.T) {
	ifs, _ := net.InterfaceAddrs()
	var local string // an address on one of this machine's own networks
	for _, a := range ifs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
			local = n.IP.String()
			break
		}
	}
	far := "203.0.113.9" // documentation range: never an interface here
	cases := []struct {
		name string
		s    session.Session
		want bool
	}{
		{"no jump host", session.Session{Protocol: session.ProtoWOL, Hostname: far}, true},
		{"jump host, device on the far side", session.Session{Protocol: session.ProtoSSH2, Hostname: far, JumpSession: "lab/jump"}, false},
		{"jump host, forced local", session.Session{Protocol: session.ProtoSSH2, Hostname: far, JumpSession: "lab/jump", WOLFrom: "local"}, true},
		{"jump host, forced jump", session.Session{Protocol: session.ProtoSSH2, Hostname: local, JumpSession: "lab/jump", WOLFrom: "jump"}, false},
		{"jump host, unknown address", session.Session{Protocol: session.ProtoSSH2, Hostname: "vm.example", JumpSession: "lab/jump"}, false},
	}
	if local != "" {
		cases = append(cases, struct {
			name string
			s    session.Session
			want bool
		}{"jump host, device on this computer's network", session.Session{Protocol: session.ProtoSSH2, Hostname: local, JumpSession: "lab/jump"}, true})
	}
	for _, c := range cases {
		if got := wakeFromHere(c.s); got != c.want {
			t.Errorf("%s: wakeFromHere = %v, want %v", c.name, got, c.want)
		}
	}
}
