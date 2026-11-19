package snmp

import (
	"errors"
	"net"
	"strings"
	"time"
)

// Trap is a notification an agent sent: a v1 trap, a v2c/v3 trap or an inform.
type Trap struct {
	At        time.Time `json:"at"`
	From      string    `json:"from"`
	Version   string    `json:"version"` // 1, 2c, 3
	Community string    `json:"community,omitempty"`
	User      string    `json:"user,omitempty"`
	Kind      string    `json:"kind"`    // trap, inform
	TrapOID   string    `json:"trapOid"` // snmpTrapOID.0 (v1: made from enterprise / generic / specific, RFC 3584)
	Uptime    uint64    `json:"uptime"`  // sysUpTime.0 in hundredths of a second
	Varbinds  []Varbind `json:"varbinds"`
	Note      string    `json:"note,omitempty"`
}

// TrapUser is a v3 user traps are accepted from (engine ID of the sender, hex; "" = any engine, keys made per engine).
type TrapUser struct {
	User     string
	Auth     Auth
	AuthPass string
	Priv     Priv
	PrivPass string
}

// DecodeTrap reads a notification. users are the v3 users known. reply is the answer an inform needs (nil otherwise).
func DecodeTrap(b []byte, from string, users []TrapUser) (t Trap, reply []byte, err error) {
	t = Trap{At: time.Now(), From: from}
	keyed := map[string]*usm{}
	m, err := decode(b, func(user string, eid []byte) *usm {
		for _, u := range users {
			if u.User != user {
				continue
			}
			k := user + "|" + string(eid)
			if keyed[k] == nil {
				x := &usm{user: user, auth: u.Auth, priv: u.Priv, engineID: eid}
				if x.localize(u.AuthPass, u.PrivPass) != nil {
					return nil
				}
				keyed[k] = x
			}
			return keyed[k]
		}
		return nil
	})
	if err != nil {
		if err == errNeedKeys {
			t.Version, t.User, t.Kind = "3", m.User, "trap"
			t.Note = "a v3 notification from user " + m.User + " - add that user (SNMP Browser → Traps → v3 users) to read it"
			return t, nil, nil
		}
		return t, nil, err
	}
	switch m.Version {
	case V1:
		t.Version = "1"
	case V2c:
		t.Version = "2c"
	default:
		t.Version = "3"
	}
	t.Community, t.User = m.Community, m.User
	p := m.PDU
	switch p.Type {
	case PDUTrapV1:
		t.Kind = "trap"
		if p.TrapV1 != nil {
			t.Uptime = p.TrapV1.TimeStamp
			if p.TrapV1.Generic < 6 {
				t.TrapOID = "1.3.6.1.6.3.1.1.5." + itoaU(uint64(p.TrapV1.Generic+1))
			} else {
				t.TrapOID = p.TrapV1.Enterprise + ".0." + itoaU(uint64(p.TrapV1.Specific))
			}
			if p.TrapV1.AgentAddr != "" && p.TrapV1.AgentAddr != "0.0.0.0" {
				t.Note = "agent " + p.TrapV1.AgentAddr
			}
		}
		t.Varbinds = p.Varbinds
	case PDUTrapV2, PDUInform:
		t.Kind = "trap"
		if p.Type == PDUInform {
			t.Kind = "inform"
		}
		for _, v := range p.Varbinds {
			switch v.OID {
			case "1.3.6.1.2.1.1.3.0":
				t.Uptime = v.Uint
			case "1.3.6.1.6.3.1.1.4.1.0":
				t.TrapOID = v.Value
			default:
				t.Varbinds = append(t.Varbinds, v)
			}
		}
		if p.Type == PDUInform {
			r := PDU{Type: PDUResponse, RequestID: p.RequestID, Varbinds: p.Varbinds}
			if m.Version == V3 {
				var u *usm
				for _, x := range keyed {
					u = x
				}
				reply, _ = encodeV3(Message{Version: V3, MsgID: m.MsgID, EngineID: m.EngineID, Boots: m.Boots, Time: m.Time, User: m.User, ContextID: m.ContextID, Context: m.Context, PDU: r}, u)
			} else {
				reply, _ = encodeCommunity(m.Version, m.Community, r)
			}
		}
	default:
		return t, nil, errors.New("snmp: not a notification")
	}
	return t, reply, nil
}

func itoaU(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// SendTrap sends a v2c trap (or a v1 trap when cfg.Version is V1) to addr - for tests and for checking a receiver.
func SendTrap(cfg Config, trapOID string, uptime uint64, vbs []Varbind) error {
	addr := cfg.Address
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(strings.Trim(addr, "[]"), "162")
	}
	c, err := net.Dial("udp", addr)
	if err != nil {
		return err
	}
	defer c.Close()
	var b []byte
	if cfg.Version == V1 {
		ent := strings.TrimSuffix(parentOf(trapOID), ".0") // RFC 3584: enterprise.0.specific
		p := PDU{Type: PDUTrapV1, TrapV1: &TrapV1{Enterprise: ent, AgentAddr: "0.0.0.0", Generic: 6, Specific: int(atoiU(lastOf(trapOID))), TimeStamp: uptime}, Varbinds: vbs}
		b, err = encodeCommunity(V1, cfg.Community, p)
	} else {
		all := append([]Varbind{{OID: "1.3.6.1.2.1.1.3.0", Type: TimeTicks, Uint: uptime}, {OID: "1.3.6.1.6.3.1.1.4.1.0", Type: ObjectID, Value: trapOID}}, vbs...)
		b, err = encodeCommunity(V2c, cfg.Community, PDU{Type: PDUTrapV2, RequestID: randID(), Varbinds: all})
	}
	if err != nil {
		return err
	}
	_, err = c.Write(b)
	return err
}

func parentOf(o string) string {
	if i := strings.LastIndexByte(o, '.'); i > 0 {
		return o[:i]
	}
	return o
}

func lastOf(o string) string { return o[strings.LastIndexByte(o, '.')+1:] }
