package emu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"net/netip"
	"strconv"
	"strings"
)

// Routing makes a Linux computer a route generator: FRR speaks OSPF or BGP with the switch on one of its data
// ports and advertises many prefixes. The computer answers ping on every address in them (a local route on its
// loopback), so an ICMP stream from a test port to the prefixes shows whether the switch forwards to the routes
// it learned.
type Routing struct {
	Proto  string `json:"proto"`            // ospf or bgp
	Port   int    `json:"port"`             // the data port it speaks on (1 = the first)
	Routes int    `json:"routes"`           // how many prefixes it advertises (1-10000)
	Prefix string `json:"prefix"`           // the first one with its length (100.64.0.0/24); the others follow it
	Area   string `json:"area,omitempty"`   // OSPF: the area (default 0.0.0.0)
	AS     int    `json:"as,omitempty"`     // BGP: its AS (default 65001)
	Peer   string `json:"peer,omitempty"`   // BGP: the switch's address on that port
	PeerAS int    `json:"peerAs,omitempty"` // BGP: the switch's AS (default 65000)
}

// RoutesMax is the most prefixes a computer advertises.
const RoutesMax = 10000

// Plan checks the settings and gives the first prefix, the addresses from one prefix to the next, and the
// prefix that covers them all (the computer answers on it).
func (r Routing) Plan() (netip.Prefix, uint32, netip.Prefix, error) {
	switch {
	case r.Proto != "ospf" && r.Proto != "bgp":
		return netip.Prefix{}, 0, netip.Prefix{}, errors.New("the protocol is ospf or bgp")
	case r.Routes < 1 || r.Routes > RoutesMax:
		return netip.Prefix{}, 0, netip.Prefix{}, fmt.Errorf("1 to %d routes", RoutesMax)
	case r.Port < 1:
		return netip.Prefix{}, 0, netip.Prefix{}, errors.New("choose its data port")
	}
	p, err := netip.ParsePrefix(strings.TrimSpace(r.Prefix))
	if err != nil || !p.Addr().Is4() || p.Bits() < 8 || p.Bits() > 32 {
		return netip.Prefix{}, 0, netip.Prefix{}, fmt.Errorf("%q is not an IPv4 prefix (100.64.0.0/24)", r.Prefix)
	}
	p = p.Masked()
	step := uint32(1) << (32 - p.Bits())
	first := binary.BigEndian.Uint32(p.Addr().AsSlice())
	last := uint64(first) + uint64(step)*uint64(r.Routes) - 1
	if last > 0xFFFFFFFF {
		return netip.Prefix{}, 0, netip.Prefix{}, errors.New("the prefixes run past 255.255.255.255")
	}
	n := bits.LeadingZeros32(first ^ uint32(last)) // the bits they share
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], first)
	super := netip.PrefixFrom(netip.AddrFrom4(b), n).Masked()
	if r.Proto == "bgp" {
		if a, err := netip.ParseAddr(strings.TrimSpace(r.Peer)); err != nil || !a.Is4() {
			return netip.Prefix{}, 0, netip.Prefix{}, errors.New("BGP needs the switch's address (peer)")
		}
	}
	return p, step, super, nil
}

// RoutingScript is the shell script that sets it up on the computer (Alpine or Debian/Ubuntu): FRR installed,
// the prefixes as blackhole routes FRR redistributes, a local route over them so it answers ping there, and
// ospfd or bgpd configured on the interface. net is the computer's address on that interface (address/bits).
func RoutingScript(r Routing, name, iface, net string) (string, error) {
	p, step, super, err := r.Plan()
	if err != nil {
		return "", err
	}
	me, err := netip.ParsePrefix(net)
	if err != nil || !me.Addr().Is4() {
		return "", fmt.Errorf("give its port %s an IPv4 address first", iface)
	}
	first := binary.BigEndian.Uint32(p.Addr().AsSlice())
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("if command -v apk >/dev/null; then apk add -q frr >/dev/null; elif command -v apt-get >/dev/null; then DEBIAN_FRONTEND=noninteractive apt-get install -y -q frr >/dev/null; else echo 'tw-routing: no apk or apt-get'; exit 1; fi\n")
	b.WriteString("sysctl -qw net.ipv4.conf.all.rp_filter=0 net.ipv4.conf.default.rp_filter=0 || true\n")
	fmt.Fprintf(&b, "ip route flush proto 99 2>/dev/null || true\nip route replace local %s dev lo proto 99\n", super)
	fmt.Fprintf(&b, "awk 'BEGIN{for(i=0;i<%d;i++){x=%d+i*%d; printf \"route replace blackhole %%d.%%d.%%d.%%d/%d proto 99\\n\", int(x/16777216)%%256, int(x/65536)%%256, int(x/256)%%256, x%%256}}' > /tmp/tw-routes && ip -batch /tmp/tw-routes\n",
		r.Routes, first, step, p.Bits())
	b.WriteString("d=/etc/frr/daemons; [ -f $d ] && sed -i 's/^ospfd=.*/ospfd=" + map[bool]string{true: "yes", false: "no"}[r.Proto == "ospf"] + "/; s/^bgpd=.*/bgpd=" + map[bool]string{true: "yes", false: "no"}[r.Proto == "bgp"] + "/' $d\n")
	b.WriteString("cat > /etc/frr/frr.conf <<'TWEOF'\nfrr defaults traditional\nhostname " + name + "\nlog syslog informational\n!\n")
	rid := me.Addr().String()
	if r.Proto == "ospf" {
		area := orDefault(r.Area, "0.0.0.0")
		fmt.Fprintf(&b, "interface %s\n ip ospf area %s\n!\nrouter ospf\n ospf router-id %s\n redistribute kernel\n!\n", iface, area, rid)
	} else {
		as, pas := r.AS, r.PeerAS
		if as == 0 {
			as = 65001
		}
		if pas == 0 {
			pas = 65000
		}
		fmt.Fprintf(&b, "router bgp %d\n bgp router-id %s\n no bgp ebgp-requires-policy\n no bgp network import-check\n neighbor %s remote-as %d\n !\n address-family ipv4 unicast\n  redistribute kernel\n exit-address-family\n!\n", as, rid, strings.TrimSpace(r.Peer), pas)
	}
	b.WriteString("TWEOF\n")
	b.WriteString("chown frr:frr /etc/frr/frr.conf 2>/dev/null || true\n")
	b.WriteString("if command -v rc-service >/dev/null; then rc-update add frr default >/dev/null 2>&1 || true; rc-service frr restart >/dev/null 2>&1; else systemctl enable frr >/dev/null 2>&1 || true; systemctl restart frr; fi\n")
	// at each start: the routes again (FRR keeps its configuration)
	b.WriteString("mkdir -p /etc/local.d && cp /tmp/tw-routes /etc/tw-routes && printf '#!/bin/sh\\nip route replace local " + super.String() + " dev lo proto 99\\nip -batch /etc/tw-routes\\n' > /etc/local.d/tw-routes.start && chmod +x /etc/local.d/tw-routes.start && (rc-update add local default >/dev/null 2>&1 || true)\n")
	b.WriteString("echo tw-routing-ok\n")
	return b.String(), nil
}

// RoutingOffScript stops it: the routes go, FRR stops.
func RoutingOffScript() string {
	return "ip route flush proto 99 2>/dev/null; rm -f /etc/local.d/tw-routes.start /etc/tw-routes; (rc-service frr stop || systemctl stop frr) >/dev/null 2>&1; echo tw-routing-ok"
}

// RoutingShowScript prints what FRR says: neighbours (OSPF) or the BGP summary, and how many prefixes it has.
func RoutingShowScript(r Routing) string {
	if r.Proto == "bgp" {
		return "vtysh -c 'show bgp ipv4 unicast summary' 2>&1 | tail -n 4; echo \"tw-prefixes $(ip route show proto 99 | grep -c blackhole)\""
	}
	return "vtysh -c 'show ip ospf neighbor' 2>&1 | tail -n +2; echo \"tw-prefixes $(ip route show proto 99 | grep -c blackhole)\""
}

// RoutingState reads RoutingShowScript's output: the neighbour's state and how many prefixes are there.
func RoutingState(r Routing, out string) (state string, up bool, prefixes int) {
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && f[0] == "tw-prefixes" {
			prefixes, _ = strconv.Atoi(f[1])
			continue
		}
		if r.Proto == "ospf" && len(f) >= 3 && strings.Count(f[0], ".") == 3 && strings.Contains(f[2], "/") {
			state = f[2] // Full/DR …
			up = strings.HasPrefix(f[2], "Full")
		}
		if r.Proto == "bgp" && len(f) >= 10 && f[0] == strings.TrimSpace(r.Peer) {
			st := f[9] // State/PfxRcd: a number once it is up
			if _, err := strconv.Atoi(st); err == nil {
				state, up = "Established", true
			} else {
				state = st
			}
		}
	}
	if state == "" {
		state = "no neighbour yet"
	}
	return state, up, prefixes
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return strings.TrimSpace(s)
}
