package discover

import (
	"context"
	"errors"
	"net/netip"
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
	addrs, err := ParseTargets(network)
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
