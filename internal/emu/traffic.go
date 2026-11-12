package emu

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// The traffic generator: test ports on switch ports, and streams of frames
// between them, sent and counted on the hosts by traffic.py.

//go:embed traffic.py
var TrafficPy string

// Tester is a test port: a sender and counter on a switch's data port (no VM).
type Tester struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	VM   string  `json:"vm"`
	Port int     `json:"port"`
	MAC  string  `json:"mac"`
	IP   string  `json:"ip,omitempty"` // with its prefix (10.10.0.1/24): it answers ARP and ping there
	GW   string  `json:"gw,omitempty"` // for streams to another network
	VLAN int     `json:"vlan,omitempty"`
	X    float64 `json:"x,omitempty"` // on the canvas
	Y    float64 `json:"y,omitempty"`
}

// Stream is frames from one test port to another (or to all).
type Stream struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	From       string  `json:"from"`
	To         string  `json:"to,omitempty"`   // a test port; "" = every other one (broadcast)
	Kind       string  `json:"kind"`           // udp, icmp, tcp (SYN), arp, raw
	VLAN       int     `json:"vlan,omitempty"` // 802.1Q tag; 0 = untagged
	Prio       int     `json:"prio,omitempty"`
	DstMAC     string  `json:"dstMac,omitempty"` // "" = the destination test port's (or the gateway's, or broadcast)
	SrcIP      string  `json:"srcIp,omitempty"`  // "" = the test ports'
	DstIP      string  `json:"dstIp,omitempty"`
	DPort      int     `json:"dport,omitempty"`
	Size       int     `json:"size"`
	PPS        int     `json:"pps"`
	Seconds    int     `json:"seconds,omitempty"`
	Count      int     `json:"count,omitempty"`
	MaxLoss    float64 `json:"maxLoss"`              // % for a pass
	MaxLatency float64 `json:"maxLatency,omitempty"` // ms; 0 = not checked
	Off        bool    `json:"off,omitempty"`        // left out of Start All
}

// TesterPorts: where a switch port's QEMU socket sends (the test port listens) and listens (the test port sends).
func (l *Lab) TesterPorts(v VM, n int) (listen, send int) {
	return l.Base + 12000 + v.Index*64 + n, l.udpPort(v, n)
}

// TesterByID finds a test port.
func (l *Lab) TesterByID(id string) *Tester {
	for i := range l.Testers {
		if l.Testers[i].ID == id {
			return &l.Testers[i]
		}
	}
	return nil
}

// TesterOn is the test port on a switch port (nil = none).
func (l *Lab) TesterOn(vm string, port int) *Tester {
	for i := range l.Testers {
		if l.Testers[i].VM == vm && l.Testers[i].Port == port {
			return &l.Testers[i]
		}
	}
	return nil
}

// TrafficConfig is traffic.json for one host: its test ports, every stream
// (frames carry the stream's index) and the run (0 = stopped).
func (l *Lab) TrafficConfig(here func(VM) bool, run uint32, on map[string]bool) ([]byte, int) {
	type tc struct {
		ID     string `json:"id"`
		MAC    string `json:"mac"`
		IP     string `json:"ip,omitempty"`
		GW     string `json:"gw,omitempty"`
		VLAN   int    `json:"vlan,omitempty"`
		Listen int    `json:"listen"`
		Send   int    `json:"send"`
	}
	type sc struct {
		Stream
		On     bool   `json:"on"`
		Routed bool   `json:"routed,omitempty"`
		GwIP   string `json:"gwIp,omitempty"`
		ToMAC  string `json:"toMac,omitempty"`
	}
	var ts []tc
	for _, t := range l.Testers {
		v := l.VMByID(t.VM)
		if v == nil || !here(*v) {
			continue
		}
		li, se := l.TesterPorts(*v, t.Port)
		ts = append(ts, tc{t.ID, t.MAC, t.IP, t.GW, t.VLAN, li, se})
	}
	var ss []sc
	for _, s := range l.Streams {
		x := sc{Stream: s, On: on[s.ID]}
		if f, to := l.TesterByID(s.From), l.TesterByID(s.To); f != nil {
			if x.SrcIP == "" {
				x.SrcIP = strings.Split(f.IP, "/")[0]
			}
			if to != nil && x.DstIP == "" {
				x.DstIP = strings.Split(to.IP, "/")[0]
			}
			if x.DstMAC == "" && x.Kind != "arp" {
				if SameNet(f.IP, x.DstIP) || f.GW == "" {
					if to != nil {
						x.ToMAC = to.MAC
					}
				} else {
					x.Routed, x.GwIP = true, f.GW
				}
			}
		}
		ss = append(ss, x)
	}
	b, _ := json.Marshal(map[string]any{"run": run, "testers": ts, "streams": ss})
	return b, len(ts)
}

// SameNet: ip is in the network of cidr (10.10.0.1/24); true when either is missing.
func SameNet(cidr, ip string) bool {
	_, n, err := net.ParseCIDR(cidr)
	a := net.ParseIP(ip)
	if err != nil || a == nil {
		return true
	}
	return n.Contains(a)
}

// TrafficScript writes a host's traffic.json and starts traffic.py there when it is not running
// (it ends by itself when no test port is left on the host).
func (l *Lab) TrafficScript(cfg []byte) string {
	dir := Q(l.VMDir())
	var b strings.Builder
	fmt.Fprintf(&b, "mkdir -p %s && cd %s || exit 1\n", dir, dir)
	fmt.Fprintf(&b, "cat > tw-traffic.py.new <<'TWPYEOF'\n%sTWPYEOF\n", TrafficPy)
	b.WriteString("cmp -s tw-traffic.py.new tw-traffic.py || { mv -f tw-traffic.py.new tw-traffic.py; p=$(cat traffic.pid 2>/dev/null); [ -n \"$p\" ] && kill $p 2>/dev/null; }; rm -f tw-traffic.py.new\n")
	fmt.Fprintf(&b, "cat > traffic.json.new <<'TWJSEOF'\n%s\nTWJSEOF\nmv -f traffic.json.new traffic.json\n", cfg)
	b.WriteString("p=$(cat traffic.pid 2>/dev/null); if [ -z \"$p\" ] || ! grep -q tw-traffic /proc/$p/cmdline 2>/dev/null; then command -v python3 >/dev/null || { echo 'traffic: python3 is missing'; exit 1; }; nohup python3 tw-traffic.py \"$PWD\" >traffic.log 2>&1 & echo $! > traffic.pid; fi\necho traffic-ok\n")
	return b.String()
}
