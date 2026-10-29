package discover

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// FindMAC looks for the device with this MAC address on a network of this
// computer (a /22 or smaller, e.g. 192.168.1.0/24 or 192.168.1.0): it makes
// the computer resolve every address of the network (ARP) and returns the
// IPv4 address that has the MAC address, "" when no device has it. Only
// networks this computer is directly on can be searched this way.
func FindMAC(ctx context.Context, network, mac string) (string, error) {
	want := NormalizeMAC(mac)
	if want == "" {
		return "", errors.New("not a MAC address: " + mac)
	}
	p, err := NetworkOf(network)
	if err != nil {
		return "", err
	}
	addrs, err := ParseTargets(p.String())
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", errors.New("no addresses in " + network)
	}
	in := make(map[netip.Addr]bool, len(addrs))
	for _, a := range addrs {
		in[a] = true
	}
	find := func() string {
		for ip, m := range neighbours() {
			if in[ip] && NormalizeMAC(m) == want {
				return ip.String()
			}
		}
		return ""
	}
	if ip := find(); ip != "" {
		return ip, nil
	}
	wake(ctx, addrs, 2*time.Second)
	return find(), ctx.Err()
}

// NetworkOf reads a network as people write it: "192.168.1.0/24",
// "192.168.1.0", "192.168.1" or an address in it ("192.168.1.41") - the
// last three mean its /24.
func NetworkOf(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil || !p.Addr().Is4() {
			return netip.Prefix{}, fmt.Errorf("%q is not an IPv4 network (e.g. 192.168.1.0/24)", s)
		}
		return p.Masked(), nil
	}
	parts := strings.Split(strings.TrimSuffix(s, "."), ".")
	if len(parts) == 3 {
		parts = append(parts, "0")
	}
	a, err := netip.ParseAddr(strings.Join(parts, "."))
	if err != nil || !a.Is4() {
		return netip.Prefix{}, fmt.Errorf("%q is not an IPv4 network (e.g. 192.168.1.0)", s)
	}
	return netip.PrefixFrom(a, 24).Masked(), nil
}
