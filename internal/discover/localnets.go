package discover

import (
	"net"
	"net/netip"
	"sort"
	"strings"
)

// LocalNet is a network this computer has an address on.
type LocalNet struct {
	Interface string `json:"interface"` // en0, utun3, Ethernet, …
	Address   string `json:"address"`   // this computer's address on it
	Network   string `json:"network"`   // 192.168.1.0/24
	Scan      string `json:"scan"`      // what to scan: the network, or the /24 around the address when it is too big or not a real subnet
	Hosts     int    `json:"hosts"`     // addresses in Scan
	VPN       bool   `json:"vpn"`       // a tunnel (VPN client): its remote networks are not known here
	Note      string `json:"note,omitempty"`
}

// LocalNetworks lists the IPv4 networks of this computer's interfaces that are up
// (wired, Wi-Fi, VPN), one row per address, so a scan can start from any of them.
func LocalNetworks() []LocalNet {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []LocalNet
	for _, in := range ifs {
		if in.Flags&net.FlagUp == 0 || in.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := in.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			p, err := netip.ParsePrefix(n.String())
			if err != nil || !p.Addr().Is4() || p.Addr().IsLinkLocalUnicast() {
				continue
			}
			out = append(out, describeNet(in.Name, in.Flags&net.FlagPointToPoint != 0, p))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return !out[i].VPN && out[j].VPN })
	return out
}

func describeNet(name string, p2p bool, p netip.Prefix) LocalNet {
	n := LocalNet{Interface: name, Address: p.Addr().String(), Network: p.Masked().String()}
	n.VPN = p2p || isTunnel(name)
	scan := p.Masked()
	switch {
	case p.Bits() < 22:
		scan = netip.PrefixFrom(p.Addr(), 24).Masked()
		n.Note = "a large network - the /24 around this computer"
	case p.Bits() > 30:
		scan = netip.PrefixFrom(p.Addr(), 24).Masked()
		n.Note = "a point-to-point address - the /24 around it"
	}
	if n.VPN {
		if n.Note != "" {
			n.Note += "; "
		}
		n.Note += "VPN: type the office networks behind it yourself"
	}
	n.Scan = scan.String()
	n.Hosts = 1 << (32 - scan.Bits())
	return n
}

// isTunnel recognises the usual names of VPN and tunnel interfaces.
func isTunnel(name string) bool {
	l := strings.ToLower(name)
	for _, p := range []string{"utun", "tun", "tap", "ppp", "wg", "ipsec", "gpd", "zt", "tailscale", "vpn", "anyconnect", "cisco", "forti", "nordlynx", "proton"} {
		if strings.HasPrefix(l, p) || strings.Contains(l, "vpn") {
			return true
		}
	}
	return false
}
