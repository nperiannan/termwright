// Package trex drives a TRex server (trex-core in stateless mode) through its JSON-RPC 2.0 API, which it serves
// over ZeroMQ (a REQ/REP socket, TCP port 4501 by default). It speaks ZMTP 3.0 itself, so nothing is needed
// besides the Go standard library, and nothing is installed on the TRex server.
//
// It was written from TRex's RPC specification and tested against a fake server: field names that differ between
// TRex versions are read defensively.
package trex

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client is one connection to a TRex server.
type Client struct {
	mu      sync.Mutex
	c       net.Conn
	r       *bufio.Reader
	id      int
	apiH    string
	Version string // the server's version (v3.04, …)
	Timeout time.Duration
}

// Dial connects to addr (host:port) and syncs the API version.
func Dial(addr string, timeout time.Duration) (*Client, error) {
	c, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	t := &Client{c: c, r: bufio.NewReader(c), Timeout: timeout}
	if err := t.handshake(); err != nil {
		c.Close()
		return nil, fmt.Errorf("not a TRex (ZeroMQ) server: %w", err)
	}
	if err := t.sync(); err != nil {
		c.Close()
		return nil, err
	}
	return t, nil
}

// Close closes the connection.
func (t *Client) Close() error { return t.c.Close() }

// handshake is ZMTP 3.0: the greeting (NULL security), then READY with our socket type.
func (t *Client) handshake() error {
	_ = t.c.SetDeadline(time.Now().Add(t.Timeout))
	defer t.c.SetDeadline(time.Time{})
	if _, err := t.c.Write(Greeting(false)); err != nil {
		return err
	}
	g := make([]byte, 64)
	if _, err := io.ReadFull(t.r, g); err != nil {
		return err
	}
	if g[0] != 0xFF || g[9] != 0x7F || g[10] < 3 {
		return errors.New("no ZMTP 3 greeting")
	}
	if err := WriteFrame(t.c, 0x04, Ready("REQ")); err != nil {
		return err
	}
	for {
		flags, body, err := ReadFrame(t.r)
		if err != nil {
			return err
		}
		if flags&0x04 != 0 && bytes.HasPrefix(body, []byte("\x05READY")) {
			return nil
		}
		if flags&0x04 != 0 && bytes.HasPrefix(body, []byte("\x05ERROR")) {
			return fmt.Errorf("refused: %s", body[7:])
		}
	}
}

// Greeting is ZMTP 3.0's greeting with the NULL mechanism.
func Greeting(server bool) []byte {
	g := make([]byte, 64)
	g[0], g[9], g[10], g[11] = 0xFF, 0x7F, 3, 0
	copy(g[12:], "NULL")
	if server {
		g[32] = 1
	}
	return g
}

// Ready is the READY command's body for a socket type.
func Ready(sock string) []byte {
	b := append([]byte{5}, "READY"...)
	b = append(b, 11)
	b = append(b, "Socket-Type"...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(sock)))
	return append(b, sock...)
}

// WriteFrame writes one ZMTP frame (flags: 1 more, 4 command; the long form is chosen here).
func WriteFrame(w io.Writer, flags byte, body []byte) error {
	var h []byte
	if len(body) > 255 {
		h = binary.BigEndian.AppendUint64([]byte{flags | 0x02}, uint64(len(body)))
	} else {
		h = []byte{flags, byte(len(body))}
	}
	_, err := w.Write(append(h, body...))
	return err
}

// ReadFrame reads one ZMTP frame.
func ReadFrame(r *bufio.Reader) (byte, []byte, error) {
	flags, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	var n uint64
	if flags&0x02 != 0 {
		var b [8]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, nil, err
		}
		n = binary.BigEndian.Uint64(b[:])
	} else {
		x, err := r.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		n = uint64(x)
	}
	if n > 256<<20 {
		return 0, nil, errors.New("frame too large")
	}
	b := make([]byte, n)
	_, err = io.ReadFull(r, b)
	return flags, b, err
}

// zipMagic starts a reply TRex compressed (zlib, after the uncompressed length).
const zipMagic = 0xABE85CEA

// Unzip undoes TRex's compression of a large message (other messages are returned as they are).
func Unzip(b []byte) ([]byte, error) {
	if len(b) < 8 || binary.BigEndian.Uint32(b) != zipMagic {
		return b, nil
	}
	z, err := zlib.NewReader(bytes.NewReader(b[8:]))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(z, 256<<20))
}

// Error is an error the server answered with.
type Error struct {
	Method, Message string
	Code            int
}

func (e *Error) Error() string { return fmt.Sprintf("TRex %s: %s", e.Method, e.Message) }

// Call sends one request and returns its result (the API handle is added to the parameters once there is one).
func (t *Client) Call(method string, params map[string]any) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if params == nil {
		params = map[string]any{}
	}
	if t.apiH != "" {
		params["api_h"] = t.apiH
	}
	t.id++
	req, _ := json.Marshal(map[string]any{"id": t.id, "jsonrpc": "2.0", "method": method, "params": params})
	_ = t.c.SetDeadline(time.Now().Add(t.Timeout))
	defer t.c.SetDeadline(time.Time{})
	if err := WriteFrame(t.c, 0x01, nil); err != nil { // REQ: an empty delimiter, then the body
		return nil, err
	}
	if err := WriteFrame(t.c, 0x00, req); err != nil {
		return nil, err
	}
	var body []byte
	for {
		flags, b, err := ReadFrame(t.r)
		if err != nil {
			return nil, err
		}
		if flags&0x04 != 0 {
			continue // a command (PING, …)
		}
		if len(b) > 0 {
			body = append(body, b...)
		}
		if flags&0x01 == 0 {
			break
		}
	}
	body, err := Unzip(body)
	if err != nil {
		return nil, err
	}
	var rep struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code     int    `json:"code"`
			Message  string `json:"message"`
			Specific string `json:"specific_err"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &rep); err != nil {
		return nil, fmt.Errorf("TRex %s: a reply that is not JSON-RPC", method)
	}
	if rep.Error != nil {
		return nil, &Error{method, strings.TrimSpace(rep.Error.Message + " " + rep.Error.Specific), rep.Error.Code}
	}
	return rep.Result, nil
}

var verRe = regexp.MustCompile(`server:\s*'?(\d+)\.(\d+)`)

// sync gets the API handle: api_sync_v2 (TRex 2.x and 3.x, stateless "STL"), retried with the version the server
// says it has, else the first API's api_sync.
func (t *Client) sync() error {
	major, minor := 5, 1
	for try := 0; try < 2; try++ {
		res, err := t.Call("api_sync_v2", map[string]any{"name": "STL", "major": major, "minor": minor})
		if err == nil {
			var r struct {
				APIH string `json:"api_h"`
			}
			if json.Unmarshal(res, &r) == nil && r.APIH != "" {
				t.apiH = r.APIH
				break
			}
		}
		var e *Error
		if !errors.As(err, &e) {
			return err
		}
		if m := verRe.FindStringSubmatch(e.Message); m != nil && try == 0 {
			major, _ = strconv.Atoi(m[1])
			minor, _ = strconv.Atoi(m[2])
			continue
		}
		if e.Code != -32601 && !strings.Contains(strings.ToLower(e.Message), "method not found") {
			return err
		}
		res, err = t.Call("api_sync", map[string]any{"api_vers": []map[string]any{{"type": "core", "major": 1, "minor": 0}}})
		if err != nil {
			return err
		}
		var r struct {
			Vers []struct {
				APIH string `json:"api_h"`
			} `json:"api_vers"`
		}
		if json.Unmarshal(res, &r) == nil && len(r.Vers) > 0 {
			t.apiH = r.Vers[0].APIH
		}
		break
	}
	if res, err := t.Call("get_version", nil); err == nil {
		var v struct {
			Version string `json:"version"`
		}
		_ = json.Unmarshal(res, &v)
		t.Version = v.Version
	}
	return nil
}

// Port is one of the server's ports.
type Port struct {
	Index  int    // its number
	Speed  int    // Mbit/s (0: not known)
	Link   *bool  // up (nil: not known)
	MAC    string // its MAC address
	Driver string // its DPDK driver
	State  string // DOWN, IDLE, STREAMS, TX, PAUSE
	Owner  string // who has it acquired
}

// Ports lists the server's ports with their link state.
func (t *Client) Ports() ([]Port, error) {
	res, err := t.Call("get_system_info", nil)
	if err != nil {
		return nil, err
	}
	var si struct {
		Ports []map[string]any `json:"ports"`
	}
	if err := json.Unmarshal(res, &si); err != nil {
		return nil, err
	}
	var ps []Port
	for i, p := range si.Ports {
		x := Port{Index: i, MAC: str(p, "src_macaddr", "hw_macaddr", "hw_mac", "src_mac"), Driver: str(p, "driver")}
		if n, ok := num(p, "index"); ok {
			x.Index = int(n)
		}
		if s, ok := num(p, "speed"); ok {
			x.Speed = mbps(s)
		}
		if st, err := t.Call("get_port_status", map[string]any{"port_id": x.Index}); err == nil {
			var ps map[string]any
			if json.Unmarshal(st, &ps) == nil {
				x.State, x.Owner = str(ps, "state"), str(ps, "owner")
				if a, ok := ps["attr"].(map[string]any); ok {
					if l, ok := a["link"].(map[string]any); ok {
						if up, ok := l["up"].(bool); ok {
							x.Link = &up
						}
					}
					if s, ok := num(a, "speed"); ok && x.Speed == 0 {
						x.Speed = mbps(s)
					}
				}
				if s, ok := num(ps, "speed"); ok && x.Speed == 0 {
					x.Speed = mbps(s)
				}
			}
		}
		ps = append(ps, x)
	}
	return ps, nil
}

// mbps reads a speed TRex gives in Gbit/s (newer) or Mbit/s (older).
func mbps(s float64) int {
	if s > 0 && s <= 800 {
		return int(s * 1000)
	}
	return int(s)
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func num(m map[string]any, k string) (float64, bool) {
	switch v := m[k].(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}

// Acquire takes a port (force: from whoever has it) and returns its handler.
func (t *Client) Acquire(port int, user string) (string, error) {
	res, err := t.Call("acquire", map[string]any{"port_id": port, "user": user, "session_id": rand.IntN(1 << 30), "force": true})
	if err != nil {
		return "", err
	}
	var h string
	if json.Unmarshal(res, &h) != nil {
		var m map[string]any
		_ = json.Unmarshal(res, &m)
		h = str(m, "handler")
	}
	if h == "" {
		return "", errors.New("TRex acquire: no handler")
	}
	return h, nil
}

// Release gives a port back.
func (t *Client) Release(port int, h string) error {
	_, err := t.Call("release", map[string]any{"port_id": port, "handler": h})
	return err
}

// Reset stops a port and removes its streams and receive filters (what failed is not an error: it was idle).
func (t *Client) Reset(port int, h string) {
	_, _ = t.Call("stop_traffic", map[string]any{"port_id": port, "handler": h})
	_, _ = t.Call("remove_all_streams", map[string]any{"port_id": port, "handler": h})
	_, _ = t.Call("remove_rx_filters", map[string]any{"port_id": port, "handler": h})
	_, _ = t.Call("set_port_attr", map[string]any{"port_id": port, "handler": h, "attr": map[string]any{"promiscuous": map[string]any{"enabled": true}}})
}

// AddStream adds a stream to a port.
func (t *Client) AddStream(port int, h string, id int, stream map[string]any) error {
	_, err := t.Call("add_stream", map[string]any{"port_id": port, "handler": h, "stream_id": id, "stream": stream})
	return err
}

// Start starts a port's streams (they stop by themselves: bursts).
func (t *Client) Start(port int, h string) error {
	_, err := t.Call("start_traffic", map[string]any{"port_id": port, "handler": h, "mul": map[string]any{"type": "raw", "op": "abs", "value": 1.0},
		"duration": -1, "force": true, "core_mask": uint64(1<<64 - 1), "start_at_ts": 0})
	return err
}

// Stop stops a port's streams.
func (t *Client) Stop(port int, h string) error {
	_, err := t.Call("stop_traffic", map[string]any{"port_id": port, "handler": h})
	return err
}

// PortStats are a port's counters since the server started.
type PortStats struct {
	OPackets, IPackets, OBytes, IBytes, IErrors float64
}

// PortStats reads a port's counters.
func (t *Client) PortStats(port int) (PortStats, error) {
	res, err := t.Call("get_port_stats", map[string]any{"port_id": port})
	if err != nil {
		return PortStats{}, err
	}
	var m map[string]any
	if err := json.Unmarshal(res, &m); err != nil {
		return PortStats{}, err
	}
	g := func(k string) float64 { f, _ := num(m, k); return f }
	return PortStats{g("opackets"), g("ipackets"), g("obytes"), g("ibytes"), g("ierrors")}, nil
}

// PortState is a port's state (TX while it sends).
func (t *Client) PortState(port int) string {
	res, err := t.Call("get_port_status", map[string]any{"port_id": port})
	if err != nil {
		return ""
	}
	var m map[string]any
	_ = json.Unmarshal(res, &m)
	return str(m, "state")
}

// PGID is what a flow-statistics id counted: frames sent and received (all ports), and for a latency id its
// latency (µs) and sequence errors.
type PGID struct {
	TX, RX                        float64
	LatAvg, LatMax, LatMin, Jit   float64 // µs
	Dropped, Dup, OOO, HasLatency float64
}

// PGIDStats reads flow statistics and latency for ids.
func (t *Client) PGIDStats(ids []int) (map[int]PGID, error) {
	res, err := t.Call("get_pgid_stats", map[string]any{"pgids": ids})
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(res, &m); err != nil {
		return nil, err
	}
	out := map[int]PGID{}
	total := func(v any) float64 { // a count per port ({"0": n, "1": n}, maybe with "total"), or a number
		switch x := v.(type) {
		case float64:
			return x
		case map[string]any:
			if f, ok := num(x, "total"); ok {
				return f
			}
			s := 0.0
			for _, y := range x {
				if f, ok := y.(float64); ok {
					s += f
				}
			}
			return s
		}
		return 0
	}
	if fs, ok := m["flow_stats"].(map[string]any); ok {
		for k, v := range fs {
			id, err := strconv.Atoi(k)
			x, ok := v.(map[string]any)
			if err != nil || !ok {
				continue
			}
			p := out[id]
			p.TX, p.RX = total(x["tx_pkts"]), total(x["rx_pkts"])
			out[id] = p
		}
	}
	if ls, ok := m["latency"].(map[string]any); ok {
		for k, v := range ls {
			id, err := strconv.Atoi(k)
			x, ok := v.(map[string]any)
			if err != nil || !ok {
				continue
			}
			p := out[id]
			p.HasLatency = 1
			if l, ok := x["latency"].(map[string]any); ok {
				p.LatAvg, _ = num(l, "average")
				p.LatMax, _ = num(l, "total_max")
				p.LatMin, _ = num(l, "total_min")
				p.Jit, _ = num(l, "jitter")
			}
			if e, ok := x["err_cntrs"].(map[string]any); ok {
				p.Dropped, _ = num(e, "dropped")
				p.Dup, _ = num(e, "dup")
				p.OOO, _ = num(e, "out_of_order")
			}
			out[id] = p
		}
	}
	return out, nil
}
