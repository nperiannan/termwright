package discover

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// A minimal SNMP v2c GET client (RFC 3416), enough to read the system group.

const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysObjectID = "1.3.6.1.2.1.1.2.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
)

func encLen(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte(n)}, b...)
		n >>= 8
	}
	return append([]byte{0x80 | byte(len(b))}, b...)
}

func tlv(tag byte, v []byte) []byte { return append(append([]byte{tag}, encLen(len(v))...), v...) }

func encInt(i int64) []byte {
	b := []byte{byte(i)}
	for i >>= 8; !((i == 0 && b[0]&0x80 == 0) || (i == -1 && b[0]&0x80 != 0)); i >>= 8 {
		b = append([]byte{byte(i)}, b...)
	}
	return tlv(0x02, b)
}

func encOID(oid string) ([]byte, error) {
	parts := strings.Split(oid, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("bad OID %q", oid)
	}
	n := make([]uint64, len(parts))
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("bad OID %q", oid)
		}
		n[i] = v
	}
	out := []byte{byte(n[0]*40 + n[1])}
	for _, v := range n[2:] {
		var b []byte
		b = append(b, byte(v&0x7f))
		for v >>= 7; v > 0; v >>= 7 {
			b = append([]byte{byte(v&0x7f) | 0x80}, b...)
		}
		out = append(out, b...)
	}
	return tlv(0x06, out), nil
}

func decOID(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	parts := []string{strconv.Itoa(int(v[0]) / 40), strconv.Itoa(int(v[0]) % 40)}
	var n uint64
	for _, c := range v[1:] {
		n = n<<7 | uint64(c&0x7f)
		if c&0x80 == 0 {
			parts = append(parts, strconv.FormatUint(n, 10))
			n = 0
		}
	}
	return strings.Join(parts, ".")
}

// decTLV splits one BER element off b.
func decTLV(b []byte) (tag byte, val, rest []byte, err error) {
	if len(b) < 2 {
		return 0, nil, nil, errors.New("short SNMP message")
	}
	tag, l, i := b[0], int(b[1]), 2
	if l&0x80 != 0 {
		nb := l & 0x7f
		if nb == 0 || nb > 3 || len(b) < 2+nb {
			return 0, nil, nil, errors.New("bad SNMP length")
		}
		l = 0
		for _, c := range b[2 : 2+nb] {
			l = l<<8 | int(c)
		}
		i = 2 + nb
	}
	if len(b) < i+l {
		return 0, nil, nil, errors.New("truncated SNMP message")
	}
	return tag, b[i : i+l], b[i+l:], nil
}

func decInt(v []byte) int64 {
	var n int64
	for i, c := range v {
		if i == 0 && c&0x80 != 0 {
			n = -1
		}
		n = n<<8 | int64(c)
	}
	return n
}

// getRequest builds a v2c GetRequest.
func getRequest(community string, reqID int32, oids []string) ([]byte, error) {
	var vbs []byte
	for _, o := range oids {
		e, err := encOID(o)
		if err != nil {
			return nil, err
		}
		vbs = append(vbs, tlv(0x30, append(e, 0x05, 0x00))...)
	}
	pdu := append(append(append(encInt(int64(reqID)), encInt(0)...), encInt(0)...), tlv(0x30, vbs)...)
	msg := append(append(encInt(1), tlv(0x04, []byte(community))...), tlv(0xA0, pdu)...)
	return tlv(0x30, msg), nil
}

// parseResponse returns the request id and the values by OID (only present values).
func parseResponse(b []byte) (int32, map[string]string, error) {
	_, msg, _, err := decTLV(b)
	if err != nil {
		return 0, nil, err
	}
	_, _, msg, err = decTLV(msg) // version
	if err != nil {
		return 0, nil, err
	}
	_, _, msg, err = decTLV(msg) // community
	if err != nil {
		return 0, nil, err
	}
	tag, pdu, _, err := decTLV(msg)
	if err != nil || tag != 0xA2 {
		return 0, nil, errors.New("not an SNMP response")
	}
	_, id, pdu, err := decTLV(pdu)
	if err != nil {
		return 0, nil, err
	}
	_, es, pdu, err := decTLV(pdu)
	if err != nil {
		return 0, nil, err
	}
	if decInt(es) != 0 {
		return int32(decInt(id)), nil, fmt.Errorf("SNMP error %d", decInt(es))
	}
	_, _, pdu, err = decTLV(pdu) // error index
	if err != nil {
		return 0, nil, err
	}
	_, vbs, _, err := decTLV(pdu)
	if err != nil {
		return 0, nil, err
	}
	out := map[string]string{}
	for len(vbs) > 0 {
		var vb []byte
		if _, vb, vbs, err = decTLV(vbs); err != nil {
			return 0, nil, err
		}
		_, o, rest, err := decTLV(vb)
		if err != nil {
			return 0, nil, err
		}
		t, v, _, err := decTLV(rest)
		if err != nil {
			return 0, nil, err
		}
		switch t {
		case 0x04:
			out[decOID(o)] = printable(string(v))
		case 0x06:
			out[decOID(o)] = decOID(v)
		case 0x02, 0x41, 0x42, 0x43:
			out[decOID(o)] = strconv.FormatInt(decInt(v), 10)
		}
	}
	return int32(decInt(id)), out, nil
}

// printable keeps text readable (device descriptions sometimes carry control characters).
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f || r == 0xfffd {
			return -1
		}
		return r
	}, s)
}

// snmpGet reads oids from addr ("host:port") with a v2c community.
func snmpGet(ctx context.Context, addr, community string, oids []string, timeout time.Duration) (map[string]string, error) {
	var idb [4]byte
	_, _ = rand.Read(idb[:])
	reqID := int32(binary.BigEndian.Uint32(idb[:]) & 0x7fffffff)
	req, err := getRequest(community, reqID, oids)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	c, err := dialBusy(ctx, &d, "udp", addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	deadline := time.Now().Add(timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = c.SetDeadline(deadline)
	if _, err := c.Write(req); err != nil {
		return nil, err
	}
	buf := make([]byte, 8192)
	for {
		n, err := c.Read(buf)
		if err != nil {
			return nil, err
		}
		id, vals, err := parseResponse(buf[:n])
		if id != reqID {
			continue // a late answer to something else
		}
		return vals, err
	}
}
