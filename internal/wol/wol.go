// Package wol builds and sends Wake-on-LAN magic packets.
package wol

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strconv"
)

// DefaultPort is the usual Wake-on-LAN UDP port (7 is also common).
const DefaultPort = 9

// Packet returns the magic packet for a MAC address (aa:bb:cc:dd:ee:ff,
// aa-bb-…, aabb.ccdd.eeff or aabbccddeeff): six 0xFF bytes and the address
// sixteen times.
func Packet(mac string) ([]byte, error) {
	hw, err := ParseMAC(mac)
	if err != nil {
		return nil, err
	}
	return append(bytes.Repeat([]byte{0xff}, 6), bytes.Repeat(hw, 16)...), nil
}

// ParseMAC parses a 6-byte MAC address in the usual notations.
func ParseMAC(mac string) (net.HardwareAddr, error) {
	if len(mac) == 12 {
		if _, err := strconv.ParseUint(mac, 16, 64); err == nil {
			mac = mac[0:2] + ":" + mac[2:4] + ":" + mac[4:6] + ":" + mac[6:8] + ":" + mac[8:10] + ":" + mac[10:12]
		}
	}
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return nil, fmt.Errorf("%q is not a MAC address (e.g. 00:11:22:33:44:55)", mac)
	}
	return hw, nil
}

// CheckBroadcast accepts "" (every network of this computer) or an IPv4 address.
func CheckBroadcast(b string) error {
	if b == "" {
		return nil
	}
	if ip := net.ParseIP(b); ip == nil || ip.To4() == nil {
		return fmt.Errorf("%q is not an IPv4 broadcast address (e.g. 192.168.20.255)", b)
	}
	return nil
}

// Targets returns where to send: the given address, or - for "" -
// 255.255.255.255 and the broadcast address of each IPv4 network this
// computer is on (macOS and Windows send 255.255.255.255 out of one
// interface only).
func Targets(broadcast string) []string {
	if broadcast != "" {
		return []string{broadcast}
	}
	out := []string{"255.255.255.255"}
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagBroadcast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			ip, mask := n.IP.To4(), net.IP(n.Mask).To4()
			if mask == nil {
				continue
			}
			b := make(net.IP, 4)
			for i := range b {
				b[i] = ip[i] | ^mask[i]
			}
			if s := b.String(); s != "255.255.255.255" {
				out = append(out, s)
			}
		}
	}
	return out
}

// Send sends the packet from this computer to each target address and
// returns those it went to.
func Send(pkt []byte, broadcast string, port int) ([]string, error) {
	if port <= 0 {
		port = DefaultPort
	}
	if err := CheckBroadcast(broadcast); err != nil {
		return nil, err
	}
	var sent []string
	var lastErr error
	for _, t := range Targets(broadcast) {
		c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP(t), Port: port})
		if err != nil {
			lastErr = err
			continue
		}
		// three times, as many tools do: one packet is easily lost
		for i := 0; i < 3; i++ {
			_, err = c.Write(pkt)
		}
		_ = c.Close()
		if err != nil {
			lastErr = err
			continue
		}
		sent = append(sent, t)
	}
	if len(sent) == 0 {
		if lastErr == nil {
			lastErr = errors.New("no network to send on")
		}
		return nil, lastErr
	}
	return sent, nil
}

// RemoteCommand is a shell command that sends the packet from a Linux or
// macOS server (python3, or the wakeonlan tool), for a device on the
// server's network. Its arguments are validated, never taken as typed.
func RemoteCommand(pkt []byte, mac, broadcast string, port int) (string, error) {
	if port <= 0 {
		port = DefaultPort
	}
	if err := CheckBroadcast(broadcast); err != nil {
		return "", err
	}
	hw, err := ParseMAC(mac)
	if err != nil {
		return "", err
	}
	if broadcast == "" {
		broadcast = "255.255.255.255"
	}
	py := fmt.Sprintf(`import socket
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
s.setsockopt(socket.SOL_SOCKET,socket.SO_BROADCAST,1)
for i in range(3): s.sendto(bytes.fromhex("%x"),("%s",%d))`, pkt, broadcast, port)
	return fmt.Sprintf(`python3 -c '%s' 2>/dev/null || wakeonlan -i %s -p %d %s`, py, broadcast, port, hw.String()), nil
}
