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
	msg := []byte{0x12, 0x34, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0} // id, flags, 1 question
	for _, l := range strings.Split(q, ".") {
		msg = append(append(msg, byte(len(l))), l...)
	}
	msg = append(msg, 0, 0, 12, 0x80, 1) // PTR, class IN with the "unicast response" bit
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp", netip.AddrPortFrom(a, uint16(port)).String())
	if err != nil {
		return ""
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write(msg); err != nil {
		return ""
	}
	buf := make([]byte, 1500)
	n, err := c.Read(buf)
	if err != nil {
		return ""
	}
	name, err := firstPTR(buf[:n])
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSuffix(name, "."), ".local")
}

// firstPTR returns the target of the first PTR record in a DNS answer.
func firstPTR(m []byte) (string, error) {
	if len(m) < 12 || m[2]&0x80 == 0 {
		return "", errors.New("not a DNS response")
	}
	qd, an := int(binary.BigEndian.Uint16(m[4:])), int(binary.BigEndian.Uint16(m[6:]))
	off := 12
	for i := 0; i < qd; i++ {
		_, n, err := dnsName(m, off)
		if err != nil {
			return "", err
		}
		off = n + 4
	}
	for i := 0; i < an; i++ {
		_, n, err := dnsName(m, off)
		if err != nil || n+10 > len(m) {
			return "", errors.New("bad DNS answer")
		}
		typ, rdlen := binary.BigEndian.Uint16(m[n:]), int(binary.BigEndian.Uint16(m[n+8:]))
		rd := n + 10
		if rd+rdlen > len(m) {
			return "", errors.New("bad DNS answer")
		}
		if typ == 12 {
			name, _, err := dnsName(m, rd)
			return name, err
		}
		off = rd + rdlen
	}
	return "", errors.New("no PTR record")
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
