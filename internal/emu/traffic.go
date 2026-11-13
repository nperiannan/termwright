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
	ID       string   `json:"id"`                 // its id (made by Termwright)
	Name     string   `json:"name"`               // its name (Test port 1, …)
	VM       string   `json:"vm"`                 // the switch (its id) it is on
	Port     int      `json:"port"`               // the switch port number it is on
	MAC      string   `json:"mac"`                // its MAC address
	IP       string   `json:"ip,omitempty"`       // with its prefix (10.10.0.1/24): it answers ARP and ping there
	IP6      string   `json:"ip6,omitempty"`      // an IPv6 address with its prefix: it answers neighbour solicitations and ping there
	Groups   []string `json:"groups,omitempty"`   // multicast groups it joins (IGMP / MLD reports), to receive multicast streams
	DHCP     bool     `json:"dhcp,omitempty"`     // it takes its IPv4 address (and gateway) from a DHCP server
	SLAAC    bool     `json:"slaac,omitempty"`    // it takes its IPv6 address from router advertisements
	LLDP     bool     `json:"lldp,omitempty"`     // it announces itself by LLDP every 30 s (the switch sees "Termwright <name>")
	Lease    string   `json:"lease,omitempty"`    // the address it got by DHCP, and its gateway (filled in for the screens)
	Lease6   string   `json:"lease6,omitempty"`   // the IPv6 address it got by SLAAC (filled in for the screens)
	GW       string   `json:"gw,omitempty"`       // for streams to another network
	VLAN     int      `json:"vlan,omitempty"`     // it tags its own frames (ARP and ping answers) with this VLAN
	Speed    int      `json:"speed,omitempty"`    // its line rate, Mbit/s, for rates given as % of it (default 1000)
	PortName string   `json:"portName,omitempty"` // as the switch names it (filled in for the screens)
	X        float64  `json:"x,omitempty"`        // on the canvas
	Y        float64  `json:"y,omitempty"`        // on the canvas
}

// Stream is frames from one test port to another (or to all).
type Stream struct {
	ID         string  `json:"id"`                    // its id (made by Termwright)
	Name       string  `json:"name"`                  // its name (unique; the API and command line find it by it)
	From       string  `json:"from"`                  // the test port it is sent from (id; the API takes a name too)
	To         string  `json:"to,omitempty"`          // a test port; "" = every other one (broadcast)
	Kind       string  `json:"kind"`                  // udp, icmp, tcp (SYN), arp, raw, pcap (a capture's frames, replayed)
	VLAN       int     `json:"vlan,omitempty"`        // 802.1Q tag; 0 = untagged
	Prio       int     `json:"prio,omitempty"`        // 802.1p priority (0-7) in its VLAN tag
	DstMAC     string  `json:"dstMac,omitempty"`      // "" = the destination test port's (or the gateway's, or broadcast)
	SrcIP      string  `json:"srcIp,omitempty"`       // "" = the test ports'
	DstIP      string  `json:"dstIp,omitempty"`       // "" = the destination test port's; an address that is not a test port (icmp: its replies are counted); a multicast group; IPv6 makes it an IPv6 stream
	DPort      int     `json:"dport,omitempty"`       // UDP or TCP destination port (default 5001 for UDP, 80 for TCP)
	Size       int     `json:"size"`                  // frame bytes (60-9000; the smallest with sizeMode inc or random)
	PPS        int     `json:"pps"`                   // frames per second (1-100000)
	Seconds    int     `json:"seconds,omitempty"`     // how long it sends (with pps: seconds × pps frames)
	Count      int     `json:"count,omitempty"`       // how many frames (instead of seconds)
	MaxLoss    float64 `json:"maxLoss"`               // % for a pass
	MaxLatency float64 `json:"maxLatency,omitempty"`  // ms; 0 = not checked
	Off        bool    `json:"off,omitempty"`         // left out of Start All
	DSCP       int     `json:"dscp,omitempty"`        // IP DSCP (0-63)
	SizeMode   string  `json:"sizeMode,omitempty"`    // "" fixed, inc (Size..SizeMax, one more each frame), random, imix (64/594/1518 7:4:1)
	SizeMax    int     `json:"sizeMax,omitempty"`     // the largest frame with sizeMode inc or random
	Burst      int     `json:"burst,omitempty"`       // frames sent back to back (the rate is kept on average)
	Mods       []Mod   `json:"mods,omitempty"`        // fields that change from frame to frame
	MaxJitter  float64 `json:"maxJitter,omitempty"`   // ms for a pass (0 = not checked)
	Test       string  `json:"test,omitempty"`        // made by a test run for one of its trials (not one of the user's streams)
	RateUnit   string  `json:"rateUnit,omitempty"`    // how the rate was given: "" frames per second (pps), mbps (Mbit/s on the wire), pct (% of the sending test port's line rate)
	Rate       float64 `json:"rate,omitempty"`        // the rate in that unit (pps is worked out from it)
	RTT        bool    `json:"rtt,omitempty"`         // round trip: the test port it goes to sends each frame back, and the latency is the round trip (measured on one clock, so also between test ports on two hosts)
	Pcap       string  `json:"pcap,omitempty"`        // pcap: the capture file on this computer (classic pcap; Wireshark: Save As pcap)
	PcapID     string  `json:"pcapId,omitempty"`      // pcap: the capture as Termwright keeps it (its contents' hash; set when saved)
	PcapFrames int     `json:"pcapFrames,omitempty"`  // pcap: frames in the capture (set when saved)
	PcapSecs   float64 `json:"pcapSecs,omitempty"`    // pcap: how long the capture lasts, s (set when saved)
	PcapTiming bool    `json:"pcapTiming,omitempty"`  // pcap: send with the capture's own gaps between frames (else at the rate)
	Loops      int     `json:"loops,omitempty"`       // pcap: times through the capture (default 1; count or seconds instead)
	RewriteMAC bool    `json:"rewriteMacs,omitempty"` // pcap: frames go from this test port's MAC to the destination test port's (else as captured)
}

// IMIXMean is the mean size of an IMIX frame (64, 594 and 1518 bytes, 7:4:1).
const IMIXMean = (7*64 + 4*594 + 1518) / 12.0

// LineMbps is a test port's line rate (Mbit/s).
func (t Tester) LineMbps() int {
	if t.Speed > 0 {
		return t.Speed
	}
	return 1000
}

// MeanSize is a stream's mean frame size (bytes).
func (s Stream) MeanSize() float64 {
	switch s.SizeMode {
	case "imix":
		return IMIXMean
	case "inc", "random":
		return float64(s.Size+max(s.Size, s.SizeMax)) / 2
	}
	return float64(s.Size)
}

// WireBits is how many bits a frame of the stream takes on the wire, with its preamble and the gap after it (20 bytes), as a line rate counts them.
func (s Stream) WireBits() float64 { return (s.MeanSize() + 20) * 8 }

// PPSFor is the frames per second for a rate in Mbit/s or % of a line rate (Mbit/s); 0 for a rate in pps.
func (s Stream) PPSFor(lineMbps int) float64 {
	switch s.RateUnit {
	case "mbps":
		return s.Rate * 1e6 / s.WireBits()
	case "pct":
		return s.Rate / 100 * float64(lineMbps) * 1e6 / s.WireBits()
	}
	return 0
}

// TrafficTest is a saved test: an RFC 2544 benchmark, an address learning test or a convergence test,
// run as a series of trials by Termwright.
type TrafficTest struct {
	ID         string  `json:"id"`                   // its id (made by Termwright)
	Name       string  `json:"name"`                 // its name (unique)
	Kind       string  `json:"kind"`                 // throughput, latency, frame-loss, back-to-back (RFC 2544), learning (RFC 2889 address caching), convergence, iperf (iperf3 between two host VMs)
	From       string  `json:"from,omitempty"`       // the test port the frames go from (id; the API takes a name too)
	To         string  `json:"to,omitempty"`         // the test port they go to
	Both       bool    `json:"both,omitempty"`       // also the other way, at the same time (throughput, latency, frame-loss, convergence)
	Monitor    string  `json:"monitor,omitempty"`    // learning: a third test port that sees frames flooded to it
	VLAN       int     `json:"vlan,omitempty"`       // 802.1Q tag on the test frames
	Sizes      []int   `json:"sizes,omitempty"`      // frame sizes (default 64, 128, 256, 512, 1024, 1280, 1518)
	Trial      int     `json:"trial,omitempty"`      // seconds per trial (default 5)
	MaxPPS     int     `json:"maxPps,omitempty"`     // the highest rate tried, frames per second (default 10000)
	Rate       int     `json:"rate,omitempty"`       // latency, frame-loss, convergence: the rate (default: latency the throughput found at that size, else 1000)
	Loss       float64 `json:"loss,omitempty"`       // loss % that still counts as none (default 0)
	Resolution float64 `json:"resolution,omitempty"` // throughput, back-to-back, learning: the search stops when the step is below this % (default 2)
	Max        int     `json:"max,omitempty"`        // back-to-back: the longest burst tried (frames, default 10000); learning: the most addresses tried (default 4096)
	Action     string  `json:"action,omitempty"`     // convergence: pull (a cable or LAG member), stop (a switch), restart (a switch)
	Switch     string  `json:"switch,omitempty"`     // convergence: the switch (name or id)
	Port       string  `json:"port,omitempty"`       // convergence, pull: the port whose cable is pulled
	At         int     `json:"at,omitempty"`         // convergence: seconds after the start (default 5)
	Hold       int     `json:"hold,omitempty"`       // convergence: seconds until the cable is plugged in or the switch started again (0 = not)
	Seconds    int     `json:"seconds,omitempty"`    // convergence, iperf: how long the traffic runs (default 30, iperf 10)
	Server     string  `json:"server,omitempty"`     // iperf: the host VM (name or id) that runs iperf3 -s
	Client     string  `json:"client,omitempty"`     // iperf: the host VM that runs iperf3 -c
	ServerIP   string  `json:"serverIp,omitempty"`   // iperf: the server's address the client connects to (default: its first data port with a fixed address, else its management address)
	Proto      string  `json:"proto,omitempty"`      // iperf: tcp (default) or udp
	Bandwidth  string  `json:"bandwidth,omitempty"`  // iperf: target rate (iperf3 -b: 100M, 1G; udp default 1M)
	Parallel   int     `json:"parallel,omitempty"`   // iperf: parallel streams (iperf3 -P)
	Reverse    bool    `json:"reverse,omitempty"`    // iperf: the server sends (iperf3 -R)
}

// Mod changes a field of a stream's frames: the value plus 0, step, 2·step… (count values, then again), or one of them at random.
type Mod struct {
	Field string `json:"field"`          // srcMac, dstMac, srcIp, dstIp, vlan, sport, dport
	Mode  string `json:"mode"`           // inc, random
	Count int    `json:"count"`          // how many values (then again from the first)
	Step  int    `json:"step,omitempty"` // how far apart they are (default 1)
}

// IsMulticast: an IPv4 or IPv6 multicast address.
func IsMulticast(ip string) bool {
	a := net.ParseIP(strings.Split(ip, "/")[0])
	return a != nil && a.IsMulticast()
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
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		DHCP   bool     `json:"dhcp,omitempty"`
		SLAAC  bool     `json:"slaac,omitempty"`
		LLDP   bool     `json:"lldp,omitempty"`
		MAC    string   `json:"mac"`
		IP     string   `json:"ip,omitempty"`
		IP6    string   `json:"ip6,omitempty"`
		Groups []string `json:"groups,omitempty"`
		GW     string   `json:"gw,omitempty"`
		VLAN   int      `json:"vlan,omitempty"`
		Listen int      `json:"listen"`
		Send   int      `json:"send"`
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
		ts = append(ts, tc{t.ID, t.Name, t.DHCP, t.SLAAC, t.LLDP, t.MAC, t.IP, t.IP6, t.Groups, t.GW, t.VLAN, li, se})
	}
	var ss []sc
	for _, s := range l.Streams {
		x := sc{Stream: s, On: on[s.ID]}
		if f, to := l.TesterByID(s.From), l.TesterByID(s.To); f != nil {
			v6 := strings.Contains(x.DstIP, ":") || x.DstIP == "" && to != nil && to.IP == "" && to.IP6 != ""
			pick := func(t *Tester) string {
				if v6 {
					return strings.Split(t.IP6, "/")[0]
				}
				return strings.Split(t.IP, "/")[0]
			}
			if x.SrcIP == "" {
				x.SrcIP = pick(f)
			}
			if to != nil && x.DstIP == "" {
				x.DstIP = pick(to)
			}
			switch {
			case x.Kind == "pcap":
				if to != nil {
					x.ToMAC = to.MAC
				}
			case x.DstMAC != "" || x.Kind == "arp" || IsMulticast(x.DstIP):
			case v6 || SameNet(f.IP, x.DstIP) || f.GW == "": // IPv6: on the test ports' own network
				if to != nil {
					x.ToMAC = to.MAC
				}
			default:
				x.Routed, x.GwIP = true, f.GW
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
