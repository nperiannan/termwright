package discover

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"time"
)

// Varbind is one value of an SNMP answer, with strings kept as the raw bytes they are (a chassis ID is
// binary).
type Varbind struct {
	OID   string
	Bytes []byte // OCTET STRING
	Int   int64  // INTEGER, Counter, Gauge, TimeTicks
	IsInt bool
}

// bulkRequest is an SNMP v2c GetBulkRequest (RFC 3416) for one OID.
func bulkRequest(community string, reqID int32, oid string, maxRep int) ([]byte, error) {
	e, err := encOID(oid)
	if err != nil {
		return nil, err
	}
	vb := tlv(0x30, append(e, 0x05, 0x00))
	pdu := append(append(append(encInt(int64(reqID)), encInt(0)...), encInt(int64(maxRep))...), tlv(0x30, vb)...)
	msg := append(append(encInt(1), tlv(0x04, []byte(community))...), tlv(0xA5, pdu)...)
	return tlv(0x30, msg), nil
}

// parseVarbinds reads a response's id and all its varbinds; end is true when the agent says the MIB ends
// (endOfMibView) or an OID does not exist.
func parseVarbinds(b []byte) (id int32, vbs []Varbind, end bool, err error) {
	_, msg, _, err := decTLV(b)
	if err != nil {
		return
	}
	if _, _, msg, err = decTLV(msg); err != nil { // version
		return
	}
	if _, _, msg, err = decTLV(msg); err != nil { // community
		return
	}
	tag, pdu, _, err := decTLV(msg)
	if err != nil {
		return
	}
	if tag != 0xA2 {
		err = errors.New("not an SNMP response")
		return
	}
	var idb, es []byte
	if _, idb, pdu, err = decTLV(pdu); err != nil {
		return
	}
	id = int32(decInt(idb))
	if _, es, pdu, err = decTLV(pdu); err != nil {
		return
	}
	if decInt(es) != 0 {
		err = errors.New("SNMP error " + itoa(int(decInt(es))))
		return
	}
	if _, _, pdu, err = decTLV(pdu); err != nil { // error index
		return
	}
	_, list, _, err := decTLV(pdu)
	if err != nil {
		return
	}
	for len(list) > 0 {
		var vb []byte
		if _, vb, list, err = decTLV(list); err != nil {
			return
		}
		_, o, rest, e2 := decTLV(vb)
		if e2 != nil {
			err = e2
			return
		}
		t, v, _, e2 := decTLV(rest)
		if e2 != nil {
			err = e2
			return
		}
		x := Varbind{OID: decOID(o)}
		switch t {
		case 0x04:
			x.Bytes = append([]byte(nil), v...)
		case 0x02, 0x41, 0x42, 0x43:
			x.Int, x.IsInt = decInt(v), true
		case 0x06:
			x.Bytes = []byte(decOID(v))
		case 0x80, 0x81, 0x82:
			end = true
			continue
		default:
			continue
		}
		vbs = append(vbs, x)
	}
	return
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// SNMPWalk reads every value below root with v2c GetBulk. addr is "host" or "host:port" (161). It is
// read-only (no SNMP write is ever sent) and gives up after maxVars values or the timeout.
func SNMPWalk(ctx context.Context, addr, community, root string, timeout time.Duration, maxVars int) ([]Varbind, error) {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "161")
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
	var out []Varbind
	cur := root
	buf := make([]byte, 65535)
	for len(out) < maxVars {
		var idb [4]byte
		_, _ = rand.Read(idb[:])
		reqID := int32(binary.BigEndian.Uint32(idb[:]) & 0x7fffffff)
		req, err := bulkRequest(community, reqID, cur, 20)
		if err != nil {
			return out, err
		}
		_ = c.SetDeadline(deadline)
		if _, err := c.Write(req); err != nil {
			return out, err
		}
		var vbs []Varbind
		var end bool
		for {
			n, err := c.Read(buf)
			if err != nil {
				return out, err
			}
			id, v, e, perr := parseVarbinds(buf[:n])
			if id != reqID && perr == nil {
				continue
			}
			vbs, end, err = v, e, perr
			break
		}
		if err != nil {
			return out, err
		}
		inTree := false
		for _, v := range vbs {
			if !strings.HasPrefix(v.OID, root+".") {
				end = true
				break
			}
			out = append(out, v)
			cur, inTree = v.OID, true
		}
		if end || !inTree {
			break
		}
	}
	return out, nil
}
