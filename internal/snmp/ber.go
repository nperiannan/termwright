// Package snmp is an SNMP v1, v2c and v3 (USM: MD5, SHA-1, SHA-2 authentication; DES and AES privacy)
// manager written on the Go standard library only: Get, GetNext, GetBulk, Walk and Set, and the message
// codec an agent or a trap receiver needs too.
package snmp

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// BER tags of SNMP.
const (
	tagInteger   = 0x02
	tagOctets    = 0x04
	tagNull      = 0x05
	tagOID       = 0x06
	tagSequence  = 0x30
	tagIPAddress = 0x40
	tagCounter32 = 0x41
	tagGauge32   = 0x42
	tagTimeTicks = 0x43
	tagOpaque    = 0x44
	tagCounter64 = 0x46
	tagNoSuchObj = 0x80
	tagNoSuchIns = 0x81
	tagEndOfMib  = 0x82
)

var errShort = errors.New("snmp: message cut short")

func encLen(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	var b []byte
	for v := n; v > 0; v >>= 8 {
		b = append([]byte{byte(v)}, b...)
	}
	return append([]byte{0x80 | byte(len(b))}, b...)
}

func tlv(tag byte, v []byte) []byte {
	out := append([]byte{tag}, encLen(len(v))...)
	return append(out, v...)
}

func encInt(tag byte, n int64) []byte {
	b := big.NewInt(n).Bytes()
	if n == 0 {
		b = []byte{0}
	} else if n < 0 {
		// two's complement, shortest form
		var tmp []byte
		for v := n; ; v >>= 8 {
			tmp = append([]byte{byte(v)}, tmp...)
			if v >= -128 && v < 128 {
				break
			}
		}
		b = tmp
	} else if b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return tlv(tag, b)
}

func encUint(tag byte, n uint64) []byte {
	b := new(big.Int).SetUint64(n).Bytes()
	if len(b) == 0 {
		b = []byte{0}
	}
	if b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return tlv(tag, b)
}

func encOID(oid string) ([]byte, error) {
	parts := strings.Split(strings.Trim(oid, "."), ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("snmp: %q is not an OID", oid)
	}
	ns := make([]uint64, len(parts))
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("snmp: %q is not an OID", oid)
		}
		ns[i] = v
	}
	if ns[0] > 2 || (ns[0] < 2 && ns[1] > 39) {
		return nil, fmt.Errorf("snmp: %q is not an OID", oid)
	}
	out := base128(ns[0]*40 + ns[1])
	for _, v := range ns[2:] {
		out = append(out, base128(v)...)
	}
	return tlv(tagOID, out), nil
}

func base128(v uint64) []byte {
	b := []byte{byte(v & 0x7f)}
	for v >>= 7; v > 0; v >>= 7 {
		b = append([]byte{byte(v&0x7f) | 0x80}, b...)
	}
	return b
}

// decTLV splits the first element off b: its tag, its contents, and what follows it.
func decTLV(b []byte) (tag byte, val, rest []byte, err error) {
	if len(b) < 2 {
		return 0, nil, nil, errShort
	}
	tag = b[0]
	n, i := int(b[1]), 2
	if n&0x80 != 0 {
		k := n & 0x7f
		if k == 0 || k > 4 || len(b) < 2+k {
			return 0, nil, nil, errors.New("snmp: bad length")
		}
		n = 0
		for _, c := range b[2 : 2+k] {
			n = n<<8 | int(c)
		}
		i = 2 + k
	}
	if n < 0 || len(b) < i+n {
		return 0, nil, nil, errShort
	}
	return tag, b[i : i+n], b[i+n:], nil
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

func decUint(v []byte) uint64 {
	var n uint64
	for _, c := range v {
		n = n<<8 | uint64(c)
	}
	return n
}

func decOID(v []byte) string {
	var parts []string
	var cur uint64
	first := true
	for _, c := range v {
		cur = cur<<7 | uint64(c&0x7f)
		if c&0x80 != 0 {
			continue
		}
		if first {
			a, b := cur/40, cur%40
			if a > 2 {
				a, b = 2, cur-80
			}
			parts = append(parts, strconv.FormatUint(a, 10), strconv.FormatUint(b, 10))
			first = false
		} else {
			parts = append(parts, strconv.FormatUint(cur, 10))
		}
		cur = 0
	}
	return strings.Join(parts, ".")
}
