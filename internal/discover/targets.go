// Package discover finds devices in an address range: which management ports
// answer (SSH, Telnet, web, printing, …), the SSH greeting, the DNS name and,
// with an SNMP community, the system name and description. It never tries to
// log in.
package discover

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// MaxTargets limits one scan (a /22).
const MaxTargets = 1024

// ParseTargets expands "10.0.0.0/24", "10.0.0.10-10.0.0.80", "10.0.0.10-80"
// and single addresses, separated by commas, spaces or new lines. A network
// address without a mask ("192.168.1.0" or "192.168.1") is its /24.
func ParseTargets(spec string) ([]netip.Addr, error) {
	var out []netip.Addr
	seen := map[netip.Addr]bool{}
	add := func(a netip.Addr) error {
		if seen[a] {
			return nil
		}
		if len(out) >= MaxTargets {
			return fmt.Errorf("more than %d addresses - scan a /22 or smaller at a time", MaxTargets)
		}
		seen[a] = true
		out = append(out, a)
		return nil
	}
	for _, f := range strings.FieldsFunc(spec, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\r' || r == '\t' }) {
		f = bareNetwork(f)
		switch {
		case strings.Contains(f, "/"):
			p, err := netip.ParsePrefix(f)
			if err != nil || !p.Addr().Is4() {
				return nil, fmt.Errorf("%q is not an IPv4 network (e.g. 10.0.0.0/24)", f)
			}
			p = p.Masked()
			if p.Bits() < 22 {
				return nil, fmt.Errorf("%s is too large - scan a /22 or smaller at a time", f)
			}
			first, last := p.Addr(), lastOf(p)
			for a := first; ; a = a.Next() {
				// skip the network and broadcast address of /30 and larger networks
				if p.Bits() > 30 || (a != first && a != last) {
					if err := add(a); err != nil {
						return nil, err
					}
				}
				if a == last {
					break
				}
			}
		case strings.Contains(f, "-"):
			lo, hi, _ := strings.Cut(f, "-")
			a, err := netip.ParseAddr(strings.TrimSpace(lo))
			if err != nil || !a.Is4() {
				return nil, fmt.Errorf("%q is not a range (e.g. 10.0.0.10-10.0.0.80 or 10.0.0.10-80)", f)
			}
			b, err := netip.ParseAddr(strings.TrimSpace(hi))
			if err != nil {
				n, nerr := strconv.Atoi(strings.TrimSpace(hi))
				if nerr != nil || n < 0 || n > 255 {
					return nil, fmt.Errorf("%q is not a range (e.g. 10.0.0.10-10.0.0.80 or 10.0.0.10-80)", f)
				}
				v := a.As4()
				v[3] = byte(n)
				b = netip.AddrFrom4(v)
			}
			if !b.Is4() || b.Less(a) {
				return nil, fmt.Errorf("%q: the end of the range is before its start", f)
			}
			for x := a; ; x = x.Next() {
				if err := add(x); err != nil {
					return nil, err
				}
				if x == b {
					break
				}
			}
		default:
			a, err := netip.ParseAddr(f)
			if err != nil || !a.Is4() {
				return nil, fmt.Errorf("%q is not an IPv4 address", f)
			}
			if err := add(a); err != nil {
				return nil, err
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("enter a network (10.0.0.0/24), a range (10.0.0.10-80) or addresses")
	}
	return out, nil
}

// bareNetwork turns "192.168.1.0" and "192.168.1" into "192.168.1.0/24";
// anything else is returned as it is.
func bareNetwork(f string) string {
	if strings.ContainsAny(f, "/-") {
		return f
	}
	parts := strings.Split(strings.TrimSuffix(f, "."), ".")
	if len(parts) == 3 {
		parts = append(parts, "0")
	}
	if len(parts) != 4 || parts[3] != "0" {
		return f
	}
	if a, err := netip.ParseAddr(strings.Join(parts, ".")); err == nil && a.Is4() {
		return a.String() + "/24"
	}
	return f
}

func lastOf(p netip.Prefix) netip.Addr {
	v := p.Addr().As4()
	n := uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3])
	n |= (1 << (32 - p.Bits())) - 1
	return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)})
}
