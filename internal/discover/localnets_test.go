package discover

import (
	"net/netip"
	"testing"
)

func TestDescribeNet(t *testing.T) {
	for _, c := range []struct {
		name, p, scan string
		p2p, vpn      bool
	}{
		{"en0", "192.168.1.23/24", "192.168.1.0/24", false, false},
		{"en7", "10.20.30.40/16", "10.20.30.0/24", false, false},
		{"utun3", "10.8.0.6/32", "10.8.0.0/24", true, true},
		{"Wi-Fi", "192.168.1.77/23", "192.168.0.0/23", false, false},
	} {
		n := describeNet(c.name, c.p2p, netip.MustParsePrefix(c.p))
		if n.Scan != c.scan || n.VPN != c.vpn {
			t.Errorf("%s %s: scan %s vpn %v, want %s %v", c.name, c.p, n.Scan, n.VPN, c.scan, c.vpn)
		}
		if _, err := ParseTargets(n.Scan); err != nil {
			t.Errorf("%s: scan %s is not scannable: %v", c.name, n.Scan, err)
		}
	}
}
