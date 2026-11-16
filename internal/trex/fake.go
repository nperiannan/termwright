package trex

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"sync"
)

// Fake is a TRex server for tests: two 10G ports cabled to each other, whose bursts arrive whole.
type Fake struct {
	L       net.Listener
	mu      sync.Mutex
	Calls   []string                 // the methods called, in order
	Streams map[int][]map[string]any // per port: the streams added
	state   map[int]string           // per port
	sent    map[int]bool             // per port: started since its streams were added
}

// NewFake starts a fake server on 127.0.0.1.
func NewFake() (*Fake, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	f := &Fake{L: l, Streams: map[int][]map[string]any{}, state: map[int]string{0: "IDLE", 1: "IDLE"}, sent: map[int]bool{}}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f, nil
}

// Addr is where it listens.
func (f *Fake) Addr() string { return f.L.Addr().String() }

// Close stops it.
func (f *Fake) Close() { f.L.Close() }

// Called tells how often a method was called.
func (f *Fake) Called(m string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if c == m {
			n++
		}
	}
	return n
}

func (f *Fake) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	g := make([]byte, 64)
	if _, err := c.Write(Greeting(true)); err != nil {
		return
	}
	if _, err := io.ReadFull(r, g); err != nil {
		return
	}
	if _, _, err := ReadFrame(r); err != nil { // READY
		return
	}
	_ = WriteFrame(c, 0x04, Ready("REP"))
	for {
		var body []byte
		for {
			flags, b, err := ReadFrame(r)
			if err != nil {
				return
			}
			body = append(body, b...)
			if flags&0x01 == 0 {
				break
			}
		}
		var req struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		res, e := f.answer(req.Method, req.Params)
		rep := map[string]any{"id": req.ID, "jsonrpc": "2.0"}
		if e != "" {
			rep["error"] = map[string]any{"code": -32000, "message": e}
		} else {
			rep["result"] = res
		}
		b, _ := json.Marshal(rep)
		if len(b) > 200 { // as TRex does with large replies
			var z bytes.Buffer
			w := zlib.NewWriter(&z)
			_, _ = w.Write(b)
			_ = w.Close()
			b = append(binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, zipMagic), uint32(len(b))), z.Bytes()...)
		}
		_ = WriteFrame(c, 0x01, nil)
		_ = WriteFrame(c, 0x00, b)
	}
}

func (f *Fake) answer(m string, p map[string]any) (any, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, m)
	port := 0
	if x, ok := p["port_id"].(float64); ok {
		port = int(x)
	}
	if m != "api_sync_v2" && p["api_h"] != "fake-api" {
		return nil, "API handle is missing"
	}
	switch m {
	case "api_sync_v2":
		if p["name"] != "STL" {
			return nil, "unknown API"
		}
		return map[string]any{"api_h": "fake-api"}, ""
	case "get_version":
		return map[string]any{"version": "v3.04"}, ""
	case "get_system_info":
		return map[string]any{"ports": []map[string]any{
			{"index": 0, "speed": 10, "hw_macaddr": "0c:42:a1:00:00:10", "driver": "net_ixgbe"},
			{"index": 1, "speed": 10, "hw_macaddr": "0c:42:a1:00:00:11", "driver": "net_ixgbe"}}}, ""
	case "get_port_status":
		st := f.state[port]
		if st == "TX" && f.sent[port] {
			st = "STREAMS" // its bursts are over
		}
		return map[string]any{"state": st, "owner": "", "attr": map[string]any{"link": map[string]any{"up": true}}}, ""
	case "acquire":
		return "h" + string(rune('0'+port)), ""
	case "remove_all_streams":
		f.Streams[port], f.sent[port] = nil, false
		return map[string]any{}, ""
	case "add_stream":
		s, _ := p["stream"].(map[string]any)
		f.Streams[port] = append(f.Streams[port], s)
		f.state[port] = "STREAMS"
		return map[string]any{}, ""
	case "start_traffic":
		f.state[port], f.sent[port] = "TX", true
		return map[string]any{}, ""
	case "stop_traffic":
		if f.state[port] == "TX" {
			f.state[port] = "STREAMS"
		}
		return map[string]any{}, ""
	case "get_pgid_stats":
		fs, lat := map[string]any{}, map[string]any{}
		for port, ss := range f.Streams {
			if !f.sent[port] {
				continue
			}
			for _, s := range ss {
				x, _ := s["flow_stats"].(map[string]any)
				mode, _ := s["mode"].(map[string]any)
				n, _ := mode["total_pkts"].(float64)
				id, _ := x["stream_id"].(float64)
				k := string(rune('0'+port)) + ""
				o := "1"
				if port == 1 {
					o = "0"
				}
				fs[jsonInt(id)] = map[string]any{"tx_pkts": map[string]any{k: n, "total": n}, "rx_pkts": map[string]any{o: n, "total": n}}
				if x["rule_type"] == "latency" {
					lat[jsonInt(id)] = map[string]any{"latency": map[string]any{"average": 12.5, "total_max": 40.0, "total_min": 6.0, "jitter": 2.0},
						"err_cntrs": map[string]any{"dropped": 0, "dup": 0, "out_of_order": 0}}
				}
			}
		}
		return map[string]any{"flow_stats": fs, "latency": lat}, ""
	case "get_port_stats":
		var tx float64
		for _, s := range f.Streams[port] {
			mode, _ := s["mode"].(map[string]any)
			n, _ := mode["total_pkts"].(float64)
			if f.sent[port] {
				tx += n
			}
		}
		return map[string]any{"opackets": tx, "ipackets": tx, "obytes": tx * 128, "ibytes": tx * 128}, ""
	}
	return map[string]any{}, ""
}

func jsonInt(f float64) string { b, _ := json.Marshal(int(f)); return string(b) }
