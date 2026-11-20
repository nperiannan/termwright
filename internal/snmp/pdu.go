package snmp

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PDU types.
const (
	PDUGet      = 0xA0
	PDUGetNext  = 0xA1
	PDUResponse = 0xA2
	PDUSet      = 0xA3
	PDUTrapV1   = 0xA4
	PDUGetBulk  = 0xA5
	PDUInform   = 0xA6
	PDUTrapV2   = 0xA7
	PDUReport   = 0xA8
)

// Type is the SMI type of a value.
type Type string

const (
	Integer     Type = "Integer"
	OctetString Type = "OctetString"
	Null        Type = "Null"
	ObjectID    Type = "ObjectIdentifier"
	IPAddress   Type = "IpAddress"
	Counter32   Type = "Counter32"
	Gauge32     Type = "Gauge32"
	TimeTicks   Type = "TimeTicks"
	Opaque      Type = "Opaque"
	Counter64   Type = "Counter64"
	NoSuchObj   Type = "noSuchObject"
	NoSuchInst  Type = "noSuchInstance"
	EndOfMib    Type = "endOfMibView"
)

// Varbind is one OID and its value. Int carries Integer; Uint carries the counters, gauges and
// TimeTicks; Bytes carries OctetString, IpAddress and Opaque; OID carries an ObjectIdentifier value.
type Varbind struct {
	OID   string `json:"oid"`
	Type  Type   `json:"type"`
	Int   int64  `json:"int,omitempty"`
	Uint  uint64 `json:"uint,omitempty"`
	Bytes []byte `json:"bytes,omitempty"`
	Value string `json:"value"` // the value as text (strings as text when printable, else hex)
}

// Exception: the agent has no such object or instance, or the walk passed the end of the MIB.
func (v Varbind) Exception() bool {
	return v.Type == NoSuchObj || v.Type == NoSuchInst || v.Type == EndOfMib
}

// PDU is a request, response, report or trap.
type PDU struct {
	Type      byte
	RequestID int32
	ErrStatus int // or non-repeaters for GetBulk
	ErrIndex  int // or max-repetitions for GetBulk
	Varbinds  []Varbind
	TrapV1    *TrapV1 // only for a v1 trap
}

// TrapV1 is the header of an SNMPv1 trap.
type TrapV1 struct {
	Enterprise string
	AgentAddr  string
	Generic    int
	Specific   int
	TimeStamp  uint64
}

func encValue(v Varbind) ([]byte, error) {
	switch v.Type {
	case Integer:
		return encInt(tagInteger, v.Int), nil
	case OctetString, "":
		return tlv(tagOctets, v.Bytes), nil
	case Null:
		return []byte{tagNull, 0}, nil
	case ObjectID:
		return encOID(v.Value)
	case IPAddress:
		ip := net.ParseIP(v.Value).To4()
		if ip == nil && len(v.Bytes) == 4 {
			ip = v.Bytes
		}
		if ip == nil {
			return nil, fmt.Errorf("snmp: %q is not an IPv4 address", v.Value)
		}
		return tlv(tagIPAddress, ip), nil
	case Counter32:
		return encUint(tagCounter32, v.Uint), nil
	case Gauge32:
		return encUint(tagGauge32, v.Uint), nil
	case TimeTicks:
		return encUint(tagTimeTicks, v.Uint), nil
	case Opaque:
		return tlv(tagOpaque, v.Bytes), nil
	case Counter64:
		return encUint(tagCounter64, v.Uint), nil
	case NoSuchObj:
		return []byte{tagNoSuchObj, 0}, nil
	case NoSuchInst:
		return []byte{tagNoSuchIns, 0}, nil
	case EndOfMib:
		return []byte{tagEndOfMib, 0}, nil
	}
	return nil, fmt.Errorf("snmp: unknown type %q", v.Type)
}

func decValue(tag byte, b []byte) Varbind {
	v := Varbind{}
	switch tag {
	case tagInteger:
		v.Type, v.Int = Integer, decInt(b)
		v.Value = strconv.FormatInt(v.Int, 10)
	case tagOctets:
		v.Type, v.Bytes = OctetString, append([]byte(nil), b...)
		v.Value = Text(b)
	case tagNull:
		v.Type = Null
	case tagOID:
		v.Type, v.Value = ObjectID, decOID(b)
	case tagIPAddress:
		v.Type, v.Bytes = IPAddress, append([]byte(nil), b...)
		if len(b) == 4 {
			v.Value = net.IP(b).String()
		}
	case tagCounter32, tagGauge32, tagTimeTicks, tagCounter64:
		v.Uint = decUint(b)
		v.Type = map[byte]Type{tagCounter32: Counter32, tagGauge32: Gauge32, tagTimeTicks: TimeTicks, tagCounter64: Counter64}[tag]
		v.Value = strconv.FormatUint(v.Uint, 10)
	case tagOpaque:
		v.Type, v.Bytes, v.Value = Opaque, append([]byte(nil), b...), hex.EncodeToString(b)
	case tagNoSuchObj:
		v.Type = NoSuchObj
	case tagNoSuchIns:
		v.Type = NoSuchInst
	case tagEndOfMib:
		v.Type = EndOfMib
	default:
		v.Type, v.Bytes, v.Value = Type(fmt.Sprintf("tag 0x%02x", tag)), append([]byte(nil), b...), hex.EncodeToString(b)
	}
	return v
}

// Text shows an OCTET STRING as text when it is printable, else as hex bytes (a MAC address is "40:e3:…").
func Text(b []byte) string {
	if utf8.Valid(b) {
		ok := true
		for _, r := range string(b) {
			if r < 0x20 && r != '\n' && r != '\r' && r != '\t' || r == 0x7f {
				ok = false
				break
			}
		}
		if ok {
			return string(b)
		}
	}
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02x", c)
	}
	return strings.Join(parts, ":")
}

func encPDU(p PDU) ([]byte, error) {
	var list []byte
	for _, v := range p.Varbinds {
		o, err := encOID(v.OID)
		if err != nil {
			return nil, err
		}
		val, err := encValue(v)
		if err != nil {
			return nil, err
		}
		list = append(list, tlv(tagSequence, append(o, val...))...)
	}
	if p.Type == PDUTrapV1 && p.TrapV1 != nil {
		t := p.TrapV1
		e, err := encOID(t.Enterprise)
		if err != nil {
			return nil, err
		}
		ip := net.ParseIP(t.AgentAddr).To4()
		if ip == nil {
			ip = net.IPv4zero.To4()
		}
		body := append(e, tlv(tagIPAddress, ip)...)
		body = append(body, encInt(tagInteger, int64(t.Generic))...)
		body = append(body, encInt(tagInteger, int64(t.Specific))...)
		body = append(body, encUint(tagTimeTicks, t.TimeStamp)...)
		body = append(body, tlv(tagSequence, list)...)
		return tlv(PDUTrapV1, body), nil
	}
	body := encInt(tagInteger, int64(p.RequestID))
	body = append(body, encInt(tagInteger, int64(p.ErrStatus))...)
	body = append(body, encInt(tagInteger, int64(p.ErrIndex))...)
	body = append(body, tlv(tagSequence, list)...)
	return tlv(p.Type, body), nil
}

func decVarbinds(list []byte) ([]Varbind, error) {
	var out []Varbind
	for len(list) > 0 {
		_, vb, rest, err := decTLV(list)
		if err != nil {
			return nil, err
		}
		list = rest
		_, o, r2, err := decTLV(vb)
		if err != nil {
			return nil, err
		}
		tag, val, _, err := decTLV(r2)
		if err != nil {
			return nil, err
		}
		v := decValue(tag, val)
		v.OID = decOID(o)
		out = append(out, v)
	}
	return out, nil
}

func decPDU(b []byte) (PDU, error) {
	tag, body, _, err := decTLV(b)
	if err != nil {
		return PDU{}, err
	}
	p := PDU{Type: tag}
	if tag == PDUTrapV1 {
		t := &TrapV1{}
		var x []byte
		if _, x, body, err = decTLV(body); err != nil {
			return p, err
		}
		t.Enterprise = decOID(x)
		if _, x, body, err = decTLV(body); err != nil {
			return p, err
		}
		if len(x) == 4 {
			t.AgentAddr = net.IP(x).String()
		}
		if _, x, body, err = decTLV(body); err != nil {
			return p, err
		}
		t.Generic = int(decInt(x))
		if _, x, body, err = decTLV(body); err != nil {
			return p, err
		}
		t.Specific = int(decInt(x))
		if _, x, body, err = decTLV(body); err != nil {
			return p, err
		}
		t.TimeStamp = decUint(x)
		if _, x, _, err = decTLV(body); err != nil {
			return p, err
		}
		p.TrapV1 = t
		p.Varbinds, err = decVarbinds(x)
		return p, err
	}
	var x []byte
	if _, x, body, err = decTLV(body); err != nil {
		return p, err
	}
	p.RequestID = int32(decInt(x))
	if _, x, body, err = decTLV(body); err != nil {
		return p, err
	}
	p.ErrStatus = int(decInt(x))
	if _, x, body, err = decTLV(body); err != nil {
		return p, err
	}
	p.ErrIndex = int(decInt(x))
	if _, x, _, err = decTLV(body); err != nil {
		return p, err
	}
	p.Varbinds, err = decVarbinds(x)
	return p, err
}

// ErrorText is the name of an SNMP error status (RFC 3416).
func ErrorText(status int) string {
	names := []string{"noError", "tooBig", "noSuchName", "badValue", "readOnly", "genErr", "noAccess", "wrongType", "wrongLength",
		"wrongEncoding", "wrongValue", "noCreation", "inconsistentValue", "resourceUnavailable", "commitFailed", "undoFailed",
		"authorizationError", "notWritable", "inconsistentName"}
	if status >= 0 && status < len(names) {
		return names[status]
	}
	return "error " + strconv.Itoa(status)
}
