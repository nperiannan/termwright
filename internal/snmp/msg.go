package snmp

import (
	"bytes"
	"errors"
	"fmt"
)

// Version of the protocol.
type Version int

const (
	V1  Version = 0
	V2c Version = 1
	V3  Version = 3
)

// Message is a decoded SNMP message.
type Message struct {
	Version   Version
	Community string
	// v3
	MsgID     int32
	Flags     byte // 1 auth, 2 priv, 4 reportable
	EngineID  []byte
	Boots     int64
	Time      int64
	User      string
	ContextID []byte
	Context   string
	PDU       PDU
}

const (
	flagAuth       = 1
	flagPriv       = 2
	flagReportable = 4
)

// encodeCommunity builds a v1 or v2c message.
func encodeCommunity(v Version, community string, p PDU) ([]byte, error) {
	pdu, err := encPDU(p)
	if err != nil {
		return nil, err
	}
	body := encInt(tagInteger, int64(v))
	body = append(body, tlv(tagOctets, []byte(community))...)
	return tlv(tagSequence, append(body, pdu...)), nil
}

// encodeV3 builds a v3 message for user u (nil: noAuthNoPriv with no user, as for discovery).
func encodeV3(m Message, u *usm) ([]byte, error) {
	pdu, err := encPDU(m.PDU)
	if err != nil {
		return nil, err
	}
	scoped := tlv(tagSequence, append(append(tlv(tagOctets, m.ContextID), tlv(tagOctets, []byte(m.Context))...), pdu...))
	flags := m.Flags
	var privParams []byte
	data := scoped
	authN := 0
	if u != nil && u.auth != AuthNone {
		flags |= flagAuth
		authN = u.authLen()
		if u.priv != PrivNone {
			flags |= flagPriv
			ct, salt, err := u.encrypt(scoped)
			if err != nil {
				return nil, err
			}
			privParams, data = salt, tlv(tagOctets, ct)
		}
	}
	user := m.User
	header := tlv(tagSequence, append(append(append(encInt(tagInteger, int64(m.MsgID)), encInt(tagInteger, 65507)...), tlv(tagOctets, []byte{flags})...), encInt(tagInteger, 3)...))
	authPlace := make([]byte, authN)
	sec := tlv(tagSequence, concat(tlv(tagOctets, m.EngineID), encInt(tagInteger, m.Boots), encInt(tagInteger, m.Time),
		tlv(tagOctets, []byte(user)), tlv(tagOctets, authPlace), tlv(tagOctets, privParams)))
	msg := tlv(tagSequence, concat(encInt(tagInteger, 3), header, tlv(tagOctets, sec), data))
	if authN > 0 {
		mac := u.mac(msg)
		// the auth parameters are the only authN zero bytes right after the user name
		marker := concat(tlv(tagOctets, []byte(user)), tlv(tagOctets, authPlace))
		i := bytes.Index(msg, marker)
		if i < 0 {
			return nil, errors.New("snmp: internal: auth field not found")
		}
		copy(msg[i+len(tlv(tagOctets, []byte(user)))+len(tlv(tagOctets, authPlace))-authN:], mac)
	}
	return msg, nil
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// errNeedKeys: decode found a v3 message for a user whose keys are not known (lookup returns nil).
var errNeedKeys = errors.New("snmp: unknown user")

// decode reads a message. lookup gives the USM state for a v3 user and engine (it may set keys);
// it is not called for v1/v2c.
func decode(b []byte, lookup func(user string, engineID []byte) *usm) (Message, error) {
	var m Message
	_, body, _, err := decTLV(b)
	if err != nil {
		return m, err
	}
	_, x, body, err := decTLV(body)
	if err != nil {
		return m, err
	}
	m.Version = Version(decInt(x))
	if m.Version != V3 {
		if _, x, body, err = decTLV(body); err != nil {
			return m, err
		}
		m.Community = string(x)
		m.PDU, err = decPDU(body)
		return m, err
	}
	var header, secB []byte
	if _, header, body, err = decTLV(body); err != nil {
		return m, err
	}
	if _, x, header, err = decTLV(header); err != nil {
		return m, err
	}
	m.MsgID = int32(decInt(x))
	if _, _, header, err = decTLV(header); err != nil { // max size
		return m, err
	}
	if _, x, _, err = decTLV(header); err != nil {
		return m, err
	}
	if len(x) == 1 {
		m.Flags = x[0]
	}
	if _, secB, body, err = decTLV(body); err != nil {
		return m, err
	}
	_, sec, _, err := decTLV(secB)
	if err != nil {
		return m, err
	}
	var authParams, privParams []byte
	if _, x, sec, err = decTLV(sec); err != nil {
		return m, err
	}
	m.EngineID = append([]byte(nil), x...)
	if _, x, sec, err = decTLV(sec); err != nil {
		return m, err
	}
	m.Boots = decInt(x)
	if _, x, sec, err = decTLV(sec); err != nil {
		return m, err
	}
	m.Time = decInt(x)
	if _, x, sec, err = decTLV(sec); err != nil {
		return m, err
	}
	m.User = string(x)
	if _, authParams, sec, err = decTLV(sec); err != nil {
		return m, err
	}
	if _, privParams, _, err = decTLV(sec); err != nil {
		return m, err
	}
	var u *usm
	if m.Flags&flagAuth != 0 {
		if u = lookup(m.User, m.EngineID); u == nil || len(u.authKey) == 0 {
			return m, errNeedKeys
		}
		if len(authParams) != u.authLen() {
			return m, errors.New("snmp: authentication failed (wrong protocol)")
		}
		cp := append([]byte(nil), b...)
		mk := concat(tlv(tagOctets, []byte(m.User)), tlv(tagOctets, authParams))
		i := bytes.Index(cp, mk)
		if i < 0 {
			return m, errors.New("snmp: authentication failed")
		}
		for k := range authParams {
			cp[i+len(mk)-len(authParams)+k] = 0
		}
		if !bytes.Equal(u.mac(cp), authParams) {
			return m, errors.New("snmp: authentication failed (wrong password or protocol)")
		}
	}
	var scoped []byte
	if m.Flags&flagPriv != 0 {
		if u == nil || len(u.privKey) == 0 {
			return m, errors.New("snmp: the answer is encrypted but no privacy key is set")
		}
		if _, x, _, err = decTLV(body); err != nil {
			return m, err
		}
		plain, err := u.decrypt(x, privParams, m.Boots, m.Time)
		if err != nil {
			return m, err
		}
		if _, scoped, _, err = decTLV(plain); err != nil {
			return m, errors.New("snmp: decryption failed (wrong privacy password or protocol)")
		}
	} else if _, scoped, _, err = decTLV(body); err != nil {
		return m, err
	}
	if _, x, scoped, err = decTLV(scoped); err != nil {
		return m, err
	}
	m.ContextID = append([]byte(nil), x...)
	if _, x, scoped, err = decTLV(scoped); err != nil {
		return m, err
	}
	m.Context = string(x)
	m.PDU, err = decPDU(scoped)
	if err != nil {
		return m, fmt.Errorf("snmp: %w", err)
	}
	return m, nil
}
