package snmp

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Config is how to reach an agent. Passwords and communities are only held in memory.
type Config struct {
	Address   string // host or host:port (161)
	Version   Version
	Community string
	User      string
	Auth      Auth
	AuthPass  string
	Priv      Priv
	PrivPass  string
	Context   string
	Timeout   time.Duration // per try; 3 s
	Retries   int           // 1
}

// Client talks to one agent. It is safe for one request at a time.
type Client struct {
	cfg  Config
	conn net.Conn
	mu   sync.Mutex
	u    *usm
	// counts for the result footer
	Requests int
}

// Dial opens the UDP socket and, for v3, finds the agent's engine ID, boots and time and localizes the keys.
func Dial(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.Retries < 0 {
		cfg.Retries = 0
	} else if cfg.Retries == 0 {
		cfg.Retries = 1
	}
	addr := cfg.Address
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(strings.Trim(addr, "[]"), "161")
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return nil, err
	}
	cl := &Client{cfg: cfg, conn: c}
	if cfg.Version == V3 {
		if err := cl.discover(ctx); err != nil {
			c.Close()
			return nil, err
		}
	}
	return cl, nil
}

// Close closes the socket.
func (c *Client) Close() error { return c.conn.Close() }

// EngineID is the agent's SNMP engine ID (v3), as hex.
func (c *Client) EngineID() string {
	if c.u == nil {
		return ""
	}
	return fmt.Sprintf("%x", c.u.engineID)
}

func randID() int32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return int32(binary.BigEndian.Uint32(b[:]) & 0x7fffffff)
}

func (c *Client) discover(ctx context.Context) error {
	if c.cfg.Auth == AuthNone && c.cfg.Priv != PrivNone {
		return errors.New("snmp: privacy needs authentication")
	}
	c.u = &usm{user: c.cfg.User, auth: c.cfg.Auth, priv: c.cfg.Priv}
	var sb [8]byte
	_, _ = rand.Read(sb[:])
	c.u.salt = binary.BigEndian.Uint64(sb[:])
	m := Message{Version: V3, MsgID: randID(), Flags: flagReportable, PDU: PDU{Type: PDUGet, RequestID: randID()}}
	req, err := encodeV3(m, nil)
	if err != nil {
		return err
	}
	resp, err := c.exchange(ctx, req, m.MsgID, true)
	if err != nil {
		return fmt.Errorf("snmp: no answer to engine discovery (%w) - check the address, UDP port and that SNMPv3 is on", err)
	}
	if len(resp.EngineID) == 0 {
		return errors.New("snmp: the agent gave no engine ID")
	}
	c.u.engineID, c.u.boots, c.u.time = resp.EngineID, resp.Boots, resp.Time
	return c.u.localize(c.cfg.AuthPass, c.cfg.PrivPass)
}

func (c *Client) lookup(user string, engineID []byte) *usm {
	if c.u != nil && user == c.u.user {
		return c.u
	}
	return nil
}

// exchange sends req and waits for the answer with the same message ID (v3) or request ID (v1/v2c).
func (c *Client) exchange(ctx context.Context, req []byte, id int32, v3 bool) (Message, error) {
	buf := make([]byte, 65535)
	var last error = errors.New("timeout")
	for try := 0; try <= c.cfg.Retries; try++ {
		if err := ctx.Err(); err != nil {
			return Message{}, err
		}
		c.Requests++
		if _, err := c.conn.Write(req); err != nil {
			return Message{}, err
		}
		dl := time.Now().Add(c.cfg.Timeout)
		if d, ok := ctx.Deadline(); ok && d.Before(dl) {
			dl = d
		}
		_ = c.conn.SetReadDeadline(dl)
		for {
			n, err := c.conn.Read(buf)
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					last = errors.New("timeout")
					break
				}
				if strings.Contains(err.Error(), "refused") {
					return Message{}, errors.New("port unreachable (no SNMP agent there)")
				}
				return Message{}, err
			}
			m, err := decode(buf[:n], c.lookup)
			if err != nil {
				return Message{}, err
			}
			if v3 && m.MsgID == id || !v3 && m.PDU.RequestID == id {
				return m, nil
			}
		}
	}
	return Message{}, last
}

// usmStats report OIDs (RFC 3414).
var reportText = map[string]string{
	"1.3.6.1.6.3.15.1.1.1.0": "unsupported security level",
	"1.3.6.1.6.3.15.1.1.2.0": "not in time window",
	"1.3.6.1.6.3.15.1.1.3.0": "unknown user name",
	"1.3.6.1.6.3.15.1.1.4.0": "unknown engine ID",
	"1.3.6.1.6.3.15.1.1.5.0": "wrong authentication digest (wrong password or protocol)",
	"1.3.6.1.6.3.15.1.1.6.0": "decryption error (wrong privacy password or protocol)",
}

// Do sends one PDU and returns the answer's PDU. It resends once when a v3 agent reports a stale time.
func (c *Client) Do(ctx context.Context, p PDU) (PDU, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		p.RequestID = randID()
		var req []byte
		var err error
		var mid int32
		if c.cfg.Version == V3 {
			mid = randID()
			m := Message{Version: V3, MsgID: mid, Flags: flagReportable, EngineID: c.u.engineID, Boots: c.u.boots, Time: c.u.time,
				User: c.u.user, ContextID: c.u.engineID, Context: c.cfg.Context, PDU: p}
			req, err = encodeV3(m, c.u)
		} else {
			req, err = encodeCommunity(c.cfg.Version, c.cfg.Community, p)
		}
		if err != nil {
			return PDU{}, err
		}
		id := p.RequestID
		if c.cfg.Version == V3 {
			id = mid
		}
		m, err := c.exchange(ctx, req, id, c.cfg.Version == V3)
		if err != nil {
			if c.cfg.Version != V3 && err.Error() == "timeout" {
				return PDU{}, errors.New("no answer (timeout) - wrong community, address or port, or SNMP is off")
			}
			return PDU{}, err
		}
		if m.PDU.Type == PDUReport {
			for _, v := range m.PDU.Varbinds {
				if v.OID == "1.3.6.1.6.3.15.1.1.2.0" && attempt == 0 { // not in time window: take the agent's time and resend
					c.u.boots, c.u.time = m.Boots, m.Time
					goto again
				}
				if t, ok := reportText[v.OID]; ok {
					return PDU{}, errors.New("snmp: " + t)
				}
			}
			return PDU{}, errors.New("snmp: the agent sent a report")
		}
		if m.PDU.ErrStatus != 0 && p.Type != PDUGetBulk {
			where := ""
			if i := m.PDU.ErrIndex; i > 0 && i <= len(p.Varbinds) {
				where = " (" + p.Varbinds[i-1].OID + ")"
			}
			return m.PDU, fmt.Errorf("snmp: %s%s", ErrorText(m.PDU.ErrStatus), where)
		}
		return m.PDU, nil
	again:
	}
	return PDU{}, errors.New("snmp: not in time window")
}

func nulls(oids []string) []Varbind {
	out := make([]Varbind, len(oids))
	for i, o := range oids {
		out[i] = Varbind{OID: strings.Trim(o, "."), Type: Null}
	}
	return out
}

// Get reads the given instances.
func (c *Client) Get(ctx context.Context, oids ...string) ([]Varbind, error) {
	p, err := c.Do(ctx, PDU{Type: PDUGet, Varbinds: nulls(oids)})
	return p.Varbinds, err
}

// GetNext reads the instance after each OID.
func (c *Client) GetNext(ctx context.Context, oids ...string) ([]Varbind, error) {
	p, err := c.Do(ctx, PDU{Type: PDUGetNext, Varbinds: nulls(oids)})
	return p.Varbinds, err
}

// GetBulk (v2c/v3) reads maxRep instances after each OID.
func (c *Client) GetBulk(ctx context.Context, nonRepeaters, maxRep int, oids ...string) ([]Varbind, error) {
	if c.cfg.Version == V1 {
		return nil, errors.New("snmp: GetBulk needs v2c or v3")
	}
	p, err := c.Do(ctx, PDU{Type: PDUGetBulk, ErrStatus: nonRepeaters, ErrIndex: maxRep, Varbinds: nulls(oids)})
	return p.Varbinds, err
}

// Set writes the values. Callers decide whether a write is allowed (production sessions ask).
func (c *Client) Set(ctx context.Context, vbs ...Varbind) ([]Varbind, error) {
	p, err := c.Do(ctx, PDU{Type: PDUSet, Varbinds: vbs})
	return p.Varbinds, err
}

// Walk reads every instance below root (GetBulk with maxRep, or GetNext on v1), up to max values (0 = 100000).
// fn, when not nil, gets each page as it arrives.
func (c *Client) Walk(ctx context.Context, root string, maxRep, max int, fn func([]Varbind)) ([]Varbind, error) {
	root = strings.Trim(root, ".")
	if max <= 0 {
		max = 100000
	}
	if maxRep <= 0 {
		maxRep = 25
	}
	var out []Varbind
	cur := root
	for len(out) < max {
		var page []Varbind
		var err error
		if c.cfg.Version == V1 {
			page, err = c.GetNext(ctx, cur)
			if err != nil && strings.Contains(err.Error(), "noSuchName") {
				break
			}
		} else {
			page, err = c.GetBulk(ctx, 0, maxRep, cur)
		}
		if err != nil {
			return out, err
		}
		var keep []Varbind
		done := len(page) == 0
		for _, v := range page {
			if v.Exception() || !(v.OID == root || strings.HasPrefix(v.OID, root+".")) || CompareOID(v.OID, cur) <= 0 {
				done = true
				break
			}
			keep = append(keep, v)
			cur = v.OID
		}
		if len(out)+len(keep) > max {
			keep = keep[:max-len(out)]
		}
		out = append(out, keep...)
		if fn != nil && len(keep) > 0 {
			fn(keep)
		}
		if done {
			break
		}
	}
	return out, nil
}

// CompareOID orders two dotted OIDs numerically.
func CompareOID(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, y := atoiU(pa[i]), atoiU(pb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return len(pa) - len(pb)
}

func atoiU(s string) uint64 {
	var n uint64
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}
