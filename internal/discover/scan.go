package discover

import (
	"context"
	"net"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Probe is a TCP port checked on every address.
type Probe struct {
	Port    int
	Service string // ssh, telnet, http, https, smb, rdp, printer, ipp, sip
}

// DefaultProbes are the TCP ports checked by default: logins, web interfaces,
// and the ports that tell computers, printers, phones and IoT boards apart
// (ESPHome API, MQTT, Modbus). SIP and CoAP are also asked over UDP.
var DefaultProbes = []Probe{{22, "ssh"}, {23, "telnet"}, {80, "http"}, {443, "https"}, {445, "smb"},
	{3389, "rdp"}, {9100, "printer"}, {631, "ipp"}, {5060, "sip"}, {6053, "esphome"}, {1883, "mqtt"}, {502, "modbus"}}

// Options control a scan.
type Options struct {
	Probes    []Probe
	Timeout   time.Duration // per connection (default 800 ms)
	Parallel  int           // addresses scanned at the same time (default 32)
	Community string        // SNMP v2c community; "" = no SNMP
	SNMPPort  int           // default 161
	SIPPort   int           // UDP SIP OPTIONS; default 5060, -1 = off
	MDNSPort  int           // unicast mDNS name query; default 5353, -1 = off
	CoAPPort  int           // CoAP GET /.well-known/core over UDP; default 5683, -1 = off
	NoDNS     bool
	NoWeb     bool // skip reading web pages
	NoARP     bool // skip the neighbour (ARP) table
	OUI       OUI  // MAC vendor table (nil = none)
}

// Result describes one device that answered.
type Result struct {
	IP           string   `json:"ip"`
	Name         string   `json:"name"` // best name: SNMP sysName, DNS name, or the address
	DNS          string   `json:"dns"`
	SysName      string   `json:"sysName"`
	SysDescr     string   `json:"sysDescr"`
	SysObjectID  string   `json:"sysObjectId"`
	Services     []string `json:"services"` // open probes, "snmp", "sip-udp"
	Banner       string   `json:"banner"`   // SSH greeting
	Telnet       string   `json:"telnet"`   // Telnet banner / login prompt
	Web          WebInfo  `json:"web"`
	SIP          string   `json:"sip"`          // SIP User-Agent / Server
	MDNS         string   `json:"mdns"`         // the name the device gives itself (mDNS / Bonjour)
	MDNSServices []string `json:"mdnsServices"` // DNS-SD service types the device offers (esphomelib, hap, …)
	CoAP         string   `json:"coap"`         // CoAP /.well-known/core answer
	MAC          string   `json:"mac"`
	MACVendor    string   `json:"macVendor"`
	Self         bool     `json:"self"` // this computer
	Kind         string   `json:"kind"` // computer, server, nas, phone, printer, switch, router, firewall, wireless, unknown
	Vendor       string   `json:"vendor"`
	OS           string   `json:"os"`
	Platform     string   `json:"platform"` // network OS named by any evidence: EXOS, VOSS, NX-OS, …
	Evidence     []string `json:"evidence"` // what the kind and vendor are based on
	Protocol     string   `json:"protocol"` // SSH2, Telnet or "" when no login port is open
	Port         int      `json:"port"`
}

// Scan checks addrs and calls found for every device that answers and
// progress after every address. Devices seen only in the neighbour table, and
// MAC addresses of devices already reported, are reported (again) at the end.
func Scan(ctx context.Context, addrs []netip.Addr, opt Options, found func(Result), progress func(done, total int)) {
	if opt.Probes == nil {
		opt.Probes = DefaultProbes
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 800 * time.Millisecond
	}
	if opt.Parallel <= 0 {
		opt.Parallel = 32
	}
	if opt.SNMPPort == 0 {
		opt.SNMPPort = 161
	}
	if opt.SIPPort == 0 {
		opt.SIPPort = 5060
	}
	if opt.MDNSPort == 0 {
		opt.MDNSPort = 5353
	}
	if opt.CoAPPort == 0 {
		opt.CoAPPort = 5683
	}
	self := localAddrs()
	jobs := make(chan netip.Addr)
	var mu sync.Mutex
	done := 0
	results := map[netip.Addr]*Result{}
	var wg sync.WaitGroup
	for i := 0; i < opt.Parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range jobs {
				r, ok := scanHost(ctx, a, opt)
				if self[a] {
					r.Self, ok = true, true
				}
				mu.Lock()
				if ok && ctx.Err() == nil {
					finish(&r, opt)
					results[a] = &r
					found(r)
				}
				done++
				if progress != nil {
					progress(done, len(addrs))
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, a := range addrs {
		select {
		case jobs <- a:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if opt.NoARP || ctx.Err() != nil {
		return
	}
	// The scan has just talked to every address, so the neighbour table now
	// lists the live devices of the local subnets - also those with no open port.
	inRange := map[netip.Addr]bool{}
	for _, a := range addrs {
		inRange[a] = true
	}
	for a, mac := range neighbours() {
		if !inRange[a] {
			continue
		}
		r := results[a]
		if r == nil {
			r = &Result{IP: a.String(), Name: a.String()}
			if !opt.NoDNS {
				r.DNS = reverse(ctx, r.IP)
			}
		} else if r.MAC != "" {
			continue
		}
		r.MAC = mac
		finish(r, opt)
		found(*r)
	}
}

// finish names and classifies a result.
func finish(r *Result, opt Options) {
	if r.MAC != "" {
		// private (random) addresses are told by the address itself, without the vendor list
		r.MACVendor = OUI(nil).Vendor(r.MAC)
		if opt.OUI != nil {
			r.MACVendor = opt.OUI.Vendor(r.MAC)
		}
	}
	if r.Self {
		if h, err := os.Hostname(); err == nil && r.SysName == "" {
			r.SysName = strings.TrimSuffix(h, ".local")
		}
	}
	switch {
	case r.SysName != "":
		r.Name = r.SysName
	case r.MDNS != "":
		r.Name = r.MDNS
	case r.DNS != "":
		r.Name = r.DNS
	default:
		r.Name = r.IP
	}
	classify(r)
}

func reverse(ctx context.Context, ip string) string {
	dctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if names, err := net.DefaultResolver.LookupAddr(dctx, ip); err == nil && len(names) > 0 {
		return strings.TrimSuffix(names[0], ".")
	}
	return ""
}

// localAddrs returns the IPv4 addresses of this computer.
func localAddrs() map[netip.Addr]bool {
	out := map[netip.Addr]bool{}
	as, _ := net.InterfaceAddrs()
	for _, a := range as {
		if n, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(n.IP.To4()); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
				out[ip] = true
			}
		}
	}
	return out
}

func scanHost(ctx context.Context, a netip.Addr, opt Options) (Result, bool) {
	r := Result{IP: a.String()}
	var mu sync.Mutex
	var wg sync.WaitGroup
	open := map[string]bool{}
	webPorts := map[int]bool{}
	for _, p := range opt.Probes {
		wg.Add(1)
		go func(p Probe) {
			defer wg.Done()
			banner, ok := probe(ctx, a, p, opt.Timeout)
			if !ok {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			open[p.Service] = true
			switch p.Service {
			case "ssh":
				if banner != "" {
					r.Banner = banner
				}
				if r.Protocol != "SSH2" || p.Port == 22 {
					r.Protocol, r.Port = "SSH2", p.Port
				}
			case "telnet":
				if banner != "" {
					r.Telnet = banner
				}
				if r.Protocol == "" {
					r.Protocol, r.Port = "Telnet", p.Port
				}
			case "http":
				webPorts[p.Port] = false
			case "https":
				webPorts[p.Port] = true
			}
		}(p)
	}
	if opt.Community != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vals, err := snmpGet(ctx, netip.AddrPortFrom(a, uint16(opt.SNMPPort)).String(), opt.Community,
				[]string{oidSysDescr, oidSysObjectID, oidSysName}, opt.Timeout+400*time.Millisecond)
			if err != nil || len(vals) == 0 {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			open["snmp"] = true
			r.SysDescr, r.SysObjectID, r.SysName = vals[oidSysDescr], vals[oidSysObjectID], strings.TrimSpace(vals[oidSysName])
		}()
	}
	if opt.SIPPort > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ua := sipOptions(ctx, a, opt.SIPPort, opt.Timeout+400*time.Millisecond); ua != "" {
				mu.Lock()
				open["sip-udp"] = true
				r.SIP = ua
				mu.Unlock()
			}
		}()
	}
	if opt.MDNSPort > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if name := mdnsName(ctx, a, opt.MDNSPort, opt.Timeout+400*time.Millisecond); name != "" {
				mu.Lock()
				open["mdns"] = true
				r.MDNS = name
				mu.Unlock()
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if svcs := mdnsServices(ctx, a, opt.MDNSPort, opt.Timeout+400*time.Millisecond); len(svcs) > 0 {
				mu.Lock()
				open["mdns"] = true
				r.MDNSServices = svcs
				mu.Unlock()
			}
		}()
	}
	if opt.CoAPPort > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v := coapCore(ctx, a, opt.CoAPPort, opt.Timeout+400*time.Millisecond); v != "" {
				mu.Lock()
				open["coap"] = true
				r.CoAP = v
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if !opt.NoWeb && len(webPorts) > 0 && ctx.Err() == nil {
		// one page is enough: HTTPS first (it also has a certificate), then HTTP
		ports := make([]int, 0, len(webPorts))
		for p, tls := range webPorts {
			if tls {
				ports = append([]int{p}, ports...)
			} else {
				ports = append(ports, p)
			}
		}
		for _, p := range ports {
			w := webInfo(ctx, a, p, webPorts[p], 3*time.Second)
			if w.Server != "" || w.Title != "" || w.Realm != "" || w.Cert != "" || len(w.Hints) > 0 {
				if r.Web.Cert == "" && w.Cert != "" && webPorts[p] {
					r.Web = w
				} else if r.Web.Title == "" && r.Web.Server == "" {
					r.Web = w
				}
				if r.Web.Title != "" && (r.Web.Server != "" || len(r.Web.Hints) > 0) {
					break
				}
			}
		}
	}
	if len(open) == 0 {
		return r, false
	}
	for _, s := range append(serviceOrder(opt.Probes), "snmp", "sip-udp", "mdns", "coap") {
		if open[s] {
			r.Services = append(r.Services, s)
			delete(open, s)
		}
	}
	if !opt.NoDNS {
		r.DNS = reverse(ctx, r.IP)
	}
	return r, true
}

func serviceOrder(ps []Probe) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Service)
	}
	return out
}

// probe connects to a port; for SSH and Telnet it also reads the greeting.
func probe(ctx context.Context, a netip.Addr, p Probe, timeout time.Duration) (string, bool) {
	d := net.Dialer{Timeout: timeout}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(a.String(), strconv.Itoa(p.Port)))
	if err != nil {
		return "", false
	}
	defer c.Close()
	switch p.Service {
	case "ssh":
		_ = c.SetReadDeadline(time.Now().Add(timeout + 1200*time.Millisecond))
		buf := make([]byte, 512)
		n, _ := c.Read(buf)
		return firstLine(string(buf[:n])), true
	case "telnet":
		return telnetGreeting(c, timeout+2*time.Second), true
	}
	return "", true
}

// telnetGreeting answers the option negotiation (refusing every option, which
// devices accept) and returns the banner and login prompt.
func telnetGreeting(c net.Conn, wait time.Duration) string {
	end := time.Now().Add(wait)
	var text []byte
	buf := make([]byte, 1024)
	quiet := 600 * time.Millisecond
	for time.Now().Before(end) && len(text) < 2048 {
		dl := time.Now().Add(quiet)
		if dl.After(end) {
			dl = end
		}
		_ = c.SetReadDeadline(dl)
		n, err := c.Read(buf)
		if n > 0 {
			var reply []byte
			b := buf[:n]
			for i := 0; i < len(b); i++ {
				if b[i] != 0xff || i+1 >= len(b) {
					text = append(text, b[i])
					continue
				}
				switch cmd := b[i+1]; cmd {
				case 0xfb, 0xfc, 0xfd, 0xfe: // WILL, WONT, DO, DONT + option
					if i+2 < len(b) {
						opt := b[i+2]
						switch cmd {
						case 0xfb: // WILL x -> DO for echo/suppress-go-ahead (gets the prompt), DONT otherwise
							if opt == 1 || opt == 3 {
								reply = append(reply, 0xff, 0xfd, opt)
							} else {
								reply = append(reply, 0xff, 0xfe, opt)
							}
						case 0xfd: // DO x -> WONT x
							reply = append(reply, 0xff, 0xfc, opt)
						}
					}
					i += 2
				case 0xfa: // sub-negotiation until IAC SE
					for i < len(b)-1 && !(b[i] == 0xff && b[i+1] == 0xf0) {
						i++
					}
					i++
				case 0xff: // escaped 0xff
					text = append(text, 0xff)
					i++
				default:
					i++
				}
			}
			if len(reply) > 0 {
				_, _ = c.Write(reply)
			}
			if t := strings.TrimSpace(string(text)); t != "" && promptEnd(t) {
				break
			}
			continue
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() && strings.TrimSpace(string(text)) == "" && time.Now().Before(end) {
				// nothing yet: some devices only talk after a key press
				_, _ = c.Write([]byte("\r\n"))
				quiet = wait / 2
				continue
			}
			break
		}
	}
	return summary(string(text))
}

// promptEnd reports whether text ends like a login prompt.
func promptEnd(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	for _, s := range []string{"login:", "username:", "user name:", "user:", "password:", "#", ">", "$"} {
		if strings.HasSuffix(t, s) {
			return true
		}
	}
	return false
}

// summary keeps up to three readable lines of a banner, joined with " / ".
func summary(s string) string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", "\n"), "\n") {
		l = strings.TrimSpace(printable(l))
		if l == "" || strings.Trim(l, "*=-_#~ ") == "" {
			continue
		}
		if len(l) > 100 {
			l = l[:100]
		}
		out = append(out, l)
	}
	if len(out) > 3 {
		out = append(out[:2], out[len(out)-1]) // first two lines and the prompt
	}
	return strings.Join(out, " / ")
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(printable(line)); line != "" {
			if len(line) > 120 {
				line = line[:120]
			}
			return line
		}
	}
	return ""
}

// sipOptions sends a SIP OPTIONS request over UDP and returns the
// User-Agent or Server of the answer ("" when nothing answers).
func sipOptions(ctx context.Context, a netip.Addr, port int, timeout time.Duration) string {
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp", netip.AddrPortFrom(a, uint16(port)).String())
	if err != nil {
		return ""
	}
	defer c.Close()
	local := c.LocalAddr().(*net.UDPAddr)
	tag := strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	req := "OPTIONS sip:" + a.String() + " SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP " + local.String() + ";branch=z9hG4bK" + tag + ";rport\r\n" +
		"Max-Forwards: 70\r\nFrom: <sip:termwright@" + local.IP.String() + ">;tag=" + tag + "\r\n" +
		"To: <sip:" + a.String() + ">\r\nCall-ID: " + tag + "@" + local.IP.String() + "\r\nCSeq: 1 OPTIONS\r\n" +
		"Accept: application/sdp\r\nUser-Agent: Termwright-discovery\r\nContent-Length: 0\r\n\r\n"
	_ = c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write([]byte(req)); err != nil {
		return ""
	}
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil || !strings.HasPrefix(string(buf[:n]), "SIP/2.0") {
		return ""
	}
	agent := "SIP"
	for _, l := range strings.Split(string(buf[:n]), "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "user-agent", "server":
			if v = strings.TrimSpace(printable(v)); v != "" {
				return v
			}
		}
	}
	return agent
}

// hostOS names the operating system of this computer.
func hostOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return runtime.GOOS
}
