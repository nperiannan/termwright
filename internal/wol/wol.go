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

// Limited is the broadcast address every host of the local network receives.
const Limited = "255.255.255.255"

// Dest is one place to send the packet to: a broadcast address, and the
// address of the network interface to send it out of (nil = the system picks).
type Dest struct {
	Addr string
	From net.IP
}

type localNet struct {
	ip    net.IP
	net   *net.IPNet
	bcast string
}

// localNets are the IPv4 networks this computer is on (up, with broadcast, not loopback).
func localNets() []localNet {
	var out []localNet
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
			out = append(out, localNet{ip: ip, net: &net.IPNet{IP: ip.Mask(net.IPMask(mask)), Mask: n.Mask}, bcast: b.String()})
		}
	}
	return out
}

// Plan says where to send, so that the packet is advertised on the whole
// network the way WoL tools do: out of every network interface (macOS and
// Windows send 255.255.255.255 out of one interface only, so each interface
// is addressed by its own address), to the broadcast address of that network
// and to 255.255.255.255. With a broadcast address given (the session's, or
// the one worked out for the device's network) it is added, and the
// interface it belongs to is used; when this computer is on no such network
// every interface is used. Several routers drop directed broadcasts, so the
// limited broadcast is always sent as well.
func Plan(broadcast string) []Dest {
	nets := localNets()
	var out []Dest
	seen := map[string]bool{}
	add := func(addr string, from net.IP) {
		k := addr + "|" + from.String()
		if !seen[k] {
			seen[k] = true
			out = append(out, Dest{Addr: addr, From: from})
		}
	}
	var match []localNet
	if broadcast != "" {
		if ip := net.ParseIP(broadcast).To4(); ip != nil {
			for _, n := range nets {
				if n.bcast == broadcast || n.net.Contains(ip) {
					match = append(match, n)
				}
			}
		}
		if len(match) == 0 {
			add(broadcast, nil)
		}
		for _, n := range match {
			add(broadcast, n.ip)
		}
	}
	use := match
	if len(use) == 0 {
		use = nets
	}
	for _, n := range use {
		if broadcast == "" {
			add(n.bcast, n.ip)
		}
		add(Limited, n.ip)
	}
	add(Limited, nil)
	return out
}

// Targets returns the addresses the packet goes to (see Plan).
func Targets(broadcast string) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range Plan(broadcast) {
		if !seen[d.Addr] {
			seen[d.Addr] = true
			out = append(out, d.Addr)
		}
	}
	return out
}

// Send sends the packet from this computer to every place of Plan and
// returns the addresses it went to.
func Send(pkt []byte, broadcast string, port int) ([]string, error) {
	if port <= 0 {
		port = DefaultPort
	}
	if err := CheckBroadcast(broadcast); err != nil {
		return nil, err
	}
	var sent []string
	seen := map[string]bool{}
	var lastErr error
	for _, d := range Plan(broadcast) {
		var local *net.UDPAddr
		if d.From != nil {
			local = &net.UDPAddr{IP: d.From}
		}
		c, err := net.DialUDP("udp4", local, &net.UDPAddr{IP: net.ParseIP(d.Addr), Port: port})
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
		if !seen[d.Addr] {
			seen[d.Addr] = true
			sent = append(sent, d.Addr)
		}
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
		broadcast = Limited
	}
	// the network's broadcast address and the limited broadcast: several routers drop directed broadcasts
	targets := fmt.Sprintf("%q", broadcast)
	wake := fmt.Sprintf("wakeonlan -i %s -p %d %s", broadcast, port, hw.String())
	if broadcast != Limited {
		targets += fmt.Sprintf(",%q", Limited)
		wake += fmt.Sprintf("; wakeonlan -i %s -p %d %s", Limited, port, hw.String())
	}
	py := fmt.Sprintf(`import socket
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
s.setsockopt(socket.SOL_SOCKET,socket.SO_BROADCAST,1)
for t in (%s):
  for i in range(3): s.sendto(bytes.fromhex("%x"),(t,%d))`, targets, pkt, port)
	return fmt.Sprintf(`python3 -c '%s' 2>/dev/null || { %s; }`, py, wake), nil
}
