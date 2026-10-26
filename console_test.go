package main

import (
	"testing"

	"github.com/termwright/termwright/internal/session"
)

func TestConsoleOf(t *testing.T) {
	cs := session.Session{Path: "Lab/cs1", Name: "cs1", Protocol: session.ProtoSSH2, Hostname: "10.0.0.5", Port: 22, Username: "admin", JumpSession: "Cloud/jump"}
	dev := session.Session{Path: "Lab/core-sw1", Name: "core-sw1", Protocol: session.ProtoConsole, ConsoleServer: "Lab/cs1", ConsolePort: 5, DeviceType: "VOSS"}
	for _, c := range []struct {
		method, proto, user string
		port                int
	}{
		{"opengear-ssh", session.ProtoSSH2, "admin:port05", 22},
		{"avocent-ssh", session.ProtoSSH2, "admin:ttyS5", 22},
		{"ssh-3000", session.ProtoSSH2, "admin", 3005},
		{"telnet-2000", session.ProtoTelnet, "", 2005},
		{"telnet-7000", session.ProtoTelnet, "", 7005},
	} {
		d := dev
		d.ConsoleMethod = c.method
		got, err := consoleOf(d, cs)
		if err != nil || got.Protocol != c.proto || got.Port != c.port || got.TransportUser != c.user || got.Hostname != "10.0.0.5" ||
			got.JumpSession != "Cloud/jump" || got.DeviceType != "VOSS" || !got.InitialCR || got.ConsoleServer != "Lab/cs1" {
			t.Errorf("%s: %+v %v", c.method, got, err)
		}
		if c.proto == session.ProtoSSH2 && got.TransportPath != "Lab/cs1" {
			t.Errorf("%s: password path %q", c.method, got.TransportPath)
		}
	}
	d := dev
	d.ConsoleMethod, d.ConsoleProtocol, d.ConsoleBase, d.ConsoleUser = "custom", session.ProtoSSH2, 4000, "{user}:{port}"
	if got, _ := consoleOf(d, cs); got.Port != 4005 || got.TransportUser != "admin:5" {
		t.Errorf("custom: %+v", got)
	}
}
