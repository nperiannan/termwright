package wol

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestPacket(t *testing.T) {
	for _, m := range []string{"00:11:22:33:44:55", "00-11-22-33-44-55", "0011.2233.4455", "001122334455"} {
		p, err := Packet(m)
		if err != nil || len(p) != 102 || p[5] != 0xff || p[6] != 0 || p[11] != 0x55 || p[101] != 0x55 {
			t.Fatalf("%s: %x %v", m, p, err)
		}
	}
	if _, err := Packet("00:11:22"); err == nil {
		t.Fatal("short MAC accepted")
	}
}

func TestSendToAddress(t *testing.T) {
	ln, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	p, _ := Packet("aa:bb:cc:dd:ee:ff")
	sent, err := Send(p, "127.0.0.1", ln.LocalAddr().(*net.UDPAddr).Port)
	if err != nil || len(sent) != 1 {
		t.Fatalf("%v %v", sent, err)
	}
	_ = ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	b := make([]byte, 200)
	n, _, err := ln.ReadFromUDP(b)
	if err != nil || n != 102 || b[6] != 0xaa {
		t.Fatalf("got %d bytes %v", n, err)
	}
}

func TestRemoteCommandIsValidated(t *testing.T) {
	p, _ := Packet("aa:bb:cc:dd:ee:ff")
	if _, err := RemoteCommand(p, "aa:bb:cc:dd:ee:ff", "1.2.3.4'; rm -rf /", 9); err == nil {
		t.Fatal("bad broadcast accepted")
	}
	c, err := RemoteCommand(p, "aa:bb:cc:dd:ee:ff", "", 9)
	if err != nil || !strings.Contains(c, `("255.255.255.255",9)`) || !strings.Contains(c, "wakeonlan -i 255.255.255.255 -p 9 aa:bb:cc:dd:ee:ff") {
		t.Fatalf("%s %v", c, err)
	}
}

func TestParseNBSTAT(t *testing.T) {
	b := make([]byte, 57)
	b[7] = 1 // one answer
	b[56] = 2
	n1 := append([]byte("WORKGROUP      "), 0x00, 0x84, 0x00) // group name
	n2 := append([]byte("NAS01          "), 0x00, 0x04, 0x00)
	b = append(b, n1...)
	b = append(b, n2...)
	if got := parseNBSTAT(b); got != "NAS01" {
		t.Fatalf("got %q", got)
	}
}

func TestPingRejectsOptions(t *testing.T) {
	if Ping(t.Context(), "-f", time.Second) || Ping(t.Context(), "a;b", time.Second) {
		t.Fatal("unsafe host accepted")
	}
}
