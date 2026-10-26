package discover

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// mdnsName asks a device for its own name with a unicast mDNS (Bonjour)
// reverse lookup on UDP 5353 ("4.3.2.1.in-addr.arpa" PTR). Apple devices,
// many printers, NAS boxes and smart devices answer with names such as
// "Natarajans-iPhone.local". It returns "" when nothing answers.
func mdnsName(ctx context.Context, a netip.Addr, port int, timeout time.Duration) string {
	v := a.As4()
	q := strconv.Itoa(int(v[3])) + "." + strconv.Itoa(int(v[2])) + "." + strconv.Itoa(int(v[1])) + "." + strconv.Itoa(int(v[0])) + ".in-addr.arpa"
	names := mdnsPTR(ctx, a, port, timeout, q)
	if len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSuffix(names[0], "."), ".local")
}

// mdnsServices asks a device which services it offers (DNS-SD service
// enumeration, "_services._dns-sd._udp.local" PTR, unicast). It returns
// service types without "_" and the protocol, e.g. "esphomelib", "arduino",
// "http", "hap", "apple-mobdev2".
func mdnsServices(ctx context.Context, a netip.Addr, port int, timeout time.Duration) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range mdnsPTR(ctx, a, port, timeout, "_services._dns-sd._udp.local") {
		t, _, _ := strings.Cut(strings.TrimPrefix(n, "_"), "._")
		if t = strings.ToLower(t); t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// mdnsPTR sends one unicast mDNS PTR question for name and returns the PTR
// targets of the answer.
func mdnsPTR(ctx context.Context, a netip.Addr, port int, timeout time.Duration, name string) []string {
	msg := []byte{0x12, 0x34, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0} // id, flags, 1 question
	for _, l := range strings.Split(name, ".") {
		msg = append(append(msg, byte(len(l))), l...)
	}
	msg = append(msg, 0, 0, 12, 0x80, 1) // PTR, class IN with the "unicast response" bit
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp", netip.AddrPortFrom(a, uint16(port)).String())
	if err != nil {
		return nil
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write(msg); err != nil {
		return nil
	}
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		return nil
	}
	names, _ := allPTR(buf[:n])
	return names
}

// firstPTR returns the target of the first PTR record in a DNS answer.
func firstPTR(m []byte) (string, error) {
	l, err := allPTR(m)
	if err != nil {
		return "", err
	}
	return l[0], nil
}

// allPTR returns the targets of the PTR records in a DNS answer.
func allPTR(m []byte) ([]string, error) {
	if len(m) < 12 || m[2]&0x80 == 0 {
		return nil, errors.New("not a DNS response")
	}
	qd := int(binary.BigEndian.Uint16(m[4:]))
	// answers, then authority and additional records (some responders put
	// the PTR records there)
	rr := int(binary.BigEndian.Uint16(m[6:])) + int(binary.BigEndian.Uint16(m[8:])) + int(binary.BigEndian.Uint16(m[10:]))
	off := 12
	for i := 0; i < qd; i++ {
		_, n, err := dnsName(m, off)
		if err != nil {
			return nil, err
		}
		off = n + 4
	}
	var out []string
	for i := 0; i < rr; i++ {
		_, n, err := dnsName(m, off)
		if err != nil || n+10 > len(m) {
			break
		}
		typ, rdlen := binary.BigEndian.Uint16(m[n:]), int(binary.BigEndian.Uint16(m[n+8:]))
		rd := n + 10
		if rd+rdlen > len(m) {
			break
		}
		if typ == 12 {
			if name, _, err := dnsName(m, rd); err == nil {
				out = append(out, name)
			}
		}
		off = rd + rdlen
	}
	if len(out) == 0 {
		return nil, errors.New("no PTR record")
	}
	return out, nil
}

// dnsName reads a (possibly compressed) name at off and returns it with the
// offset after it.
func dnsName(m []byte, off int) (string, int, error) {
	var labels []string
	end := -1
	for jumps := 0; jumps < 16; {
		if off >= len(m) {
			return "", 0, errors.New("short name")
		}
		l := int(m[off])
		switch {
		case l == 0:
			if end < 0 {
				end = off + 1
			}
			return strings.Join(labels, "."), end, nil
		case l&0xc0 == 0xc0:
			if off+1 >= len(m) {
				return "", 0, errors.New("short pointer")
			}
			if end < 0 {
				end = off + 2
			}
			off = int(binary.BigEndian.Uint16(m[off:]) & 0x3fff)
			jumps++
		default:
			if off+1+l > len(m) {
				return "", 0, errors.New("short label")
			}
			labels = append(labels, printable(string(m[off+1:off+1+l])))
			off += 1 + l
		}
	}
	return "", 0, errors.New("name loop")
}
