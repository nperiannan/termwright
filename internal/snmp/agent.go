package snmp

import (
	"net"
	"sort"
	"strings"
	"sync"
)

// Agent is a small SNMP agent answering from a table of values (for tests, demos and the simulated lab).
// It speaks v1/v2c with one community and v3 with the users added.
type Agent struct {
	Community string
	EngineID  []byte
	Boots     int64
	mu        sync.Mutex
	data      map[string]Varbind
	order     []string
	users     map[string]*usm
	writable  bool
	conn      net.PacketConn
}

// NewAgent makes an agent with an engine ID.
func NewAgent(community string, engineID []byte) *Agent {
	return &Agent{Community: community, EngineID: engineID, Boots: 1, data: map[string]Varbind{}, users: map[string]*usm{}}
}

// Put stores a value.
func (a *Agent) Put(v Varbind) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v.Value == "" {
		v = withText(v)
	}
	if _, ok := a.data[v.OID]; !ok {
		a.order = append(a.order, v.OID)
		sort.Slice(a.order, func(i, j int) bool { return CompareOID(a.order[i], a.order[j]) < 0 })
	}
	a.data[v.OID] = v
}

func withText(v Varbind) Varbind {
	b, err := encValue(v)
	if err != nil {
		return v
	}
	tag, val, _, _ := decTLV(b)
	w := decValue(tag, val)
	w.OID = v.OID
	return w
}

// SetWritable lets Set change the values (off: every Set answers notWritable).
func (a *Agent) SetWritable(on bool) { a.mu.Lock(); a.writable = on; a.mu.Unlock() }

// AddUser adds a v3 user.
func (a *Agent) AddUser(user string, auth Auth, authPass string, priv Priv, privPass string) error {
	u := &usm{user: user, auth: auth, priv: priv, engineID: a.EngineID}
	if err := u.localize(authPass, privPass); err != nil {
		return err
	}
	a.mu.Lock()
	a.users[user] = u
	a.mu.Unlock()
	return nil
}

// Listen serves on addr ("127.0.0.1:0") until Close; it returns the address used.
func (a *Agent) Listen(addr string) (string, error) {
	c, err := net.ListenPacket("udp", addr)
	if err != nil {
		return "", err
	}
	a.conn = c
	go a.serve()
	return c.LocalAddr().String(), nil
}

// Close stops the agent.
func (a *Agent) Close() error {
	if a.conn == nil {
		return nil
	}
	return a.conn.Close()
}

func (a *Agent) serve() {
	buf := make([]byte, 65535)
	for {
		n, from, err := a.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if out := a.Answer(buf[:n]); out != nil {
			_, _ = a.conn.WriteTo(out, from)
		}
	}
}

// Answer handles one request and returns the response (nil: dropped).
func (a *Agent) Answer(req []byte) []byte {
	var who *usm
	a.mu.Lock()
	users := make(map[string]*usm, len(a.users))
	for k, v := range a.users {
		users[k] = v
	}
	a.mu.Unlock()
	m, err := decode(req, func(user string, eid []byte) *usm {
		if u := users[user]; u != nil && string(eid) == string(a.EngineID) {
			who = u
			return u
		}
		return nil
	})
	if m.Version == V3 {
		report := func(oid string) []byte {
			r := Message{Version: V3, MsgID: m.MsgID, EngineID: a.EngineID, Boots: a.Boots, Time: 100, User: m.User, ContextID: a.EngineID,
				PDU: PDU{Type: PDUReport, RequestID: m.PDU.RequestID, Varbinds: []Varbind{{OID: oid, Type: Counter32, Uint: 1}}}}
			b, _ := encodeV3(r, nil)
			return b
		}
		if len(m.EngineID) == 0 {
			return report("1.3.6.1.6.3.15.1.1.4.0")
		}
		if err == errNeedKeys {
			return report("1.3.6.1.6.3.15.1.1.3.0")
		}
		if err != nil {
			if strings.Contains(err.Error(), "authentication") {
				return report("1.3.6.1.6.3.15.1.1.5.0")
			}
			return report("1.3.6.1.6.3.15.1.1.6.0")
		}
		if who == nil {
			who = users[m.User]
		}
		if who == nil || (who.auth != AuthNone) != (m.Flags&flagAuth != 0) || (who.priv != PrivNone) != (m.Flags&flagPriv != 0) {
			return report("1.3.6.1.6.3.15.1.1.1.0")
		}
		resp := a.handle(m.PDU)
		r := Message{Version: V3, MsgID: m.MsgID, EngineID: a.EngineID, Boots: a.Boots, Time: 100, User: m.User, ContextID: a.EngineID, Context: m.Context, PDU: resp}
		sv := *who
		sv.boots, sv.time = a.Boots, 100
		b, _ := encodeV3(r, &sv)
		who.salt = sv.salt
		return b
	}
	if err != nil || m.Community != a.Community {
		return nil
	}
	b, _ := encodeCommunity(m.Version, m.Community, a.handle(m.PDU))
	return b
}

func (a *Agent) next(oid string) (Varbind, bool) {
	i := sort.Search(len(a.order), func(i int) bool { return CompareOID(a.order[i], oid) > 0 })
	if i >= len(a.order) {
		return Varbind{}, false
	}
	return a.data[a.order[i]], true
}

func (a *Agent) handle(p PDU) PDU {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := PDU{Type: PDUResponse, RequestID: p.RequestID}
	switch p.Type {
	case PDUGet:
		for _, v := range p.Varbinds {
			if d, ok := a.data[v.OID]; ok {
				r.Varbinds = append(r.Varbinds, d)
			} else {
				r.Varbinds = append(r.Varbinds, Varbind{OID: v.OID, Type: NoSuchInst})
			}
		}
	case PDUGetNext:
		for _, v := range p.Varbinds {
			if d, ok := a.next(v.OID); ok {
				r.Varbinds = append(r.Varbinds, d)
			} else {
				r.Varbinds = append(r.Varbinds, Varbind{OID: v.OID, Type: EndOfMib})
			}
		}
	case PDUGetBulk:
		for _, v := range p.Varbinds {
			cur := v.OID
			for k := 0; k < p.ErrIndex; k++ {
				d, ok := a.next(cur)
				if !ok {
					r.Varbinds = append(r.Varbinds, Varbind{OID: cur, Type: EndOfMib})
					break
				}
				r.Varbinds = append(r.Varbinds, d)
				cur = d.OID
			}
		}
	case PDUSet:
		if !a.writable {
			r.ErrStatus, r.ErrIndex, r.Varbinds = 17, 1, p.Varbinds // notWritable
			return r
		}
		for _, v := range p.Varbinds {
			v = withText(v)
			if _, ok := a.data[v.OID]; !ok {
				a.order = append(a.order, v.OID)
				sort.Slice(a.order, func(i, j int) bool { return CompareOID(a.order[i], a.order[j]) < 0 })
			}
			a.data[v.OID] = v
			r.Varbinds = append(r.Varbinds, v)
		}
	}
	return r
}
