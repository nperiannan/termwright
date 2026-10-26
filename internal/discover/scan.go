package discover

import (
	"bytes"
	"context"
	"net"
	"net/netip"
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

// DefaultProbes are the ports checked by default: logins, and the ones that
// tell computers, printers and phones apart.
var DefaultProbes = []Probe{{22, "ssh"}, {23, "telnet"}, {80, "http"}, {443, "https"}, {445, "smb"},
	{3389, "rdp"}, {9100, "printer"}, {631, "ipp"}, {5060, "sip"}}

// Options control a scan.
type Options struct {
	Probes    []Probe
	Timeout   time.Duration // per connection (default 800 ms)
	Parallel  int           // addresses scanned at the same time (default 32)
	Community string        // SNMP v2c community; "" = no SNMP
	SNMPPort  int           // default 161
	NoDNS     bool
}

// Result describes one device that answered.
type Result struct {
	IP          string   `json:"ip"`
	Name        string   `json:"name"` // best name: SNMP sysName, DNS name, or the address
	DNS         string   `json:"dns"`
	SysName     string   `json:"sysName"`
	SysDescr    string   `json:"sysDescr"`
	SysObjectID string   `json:"sysObjectId"`
	Services    []string `json:"services"` // open probes and "snmp"
	Banner      string   `json:"banner"`   // SSH greeting (or the first Telnet text)
	Kind        string   `json:"kind"`     // computer, server, phone, printer, switch, router, firewall, wireless, unknown
	Vendor      string   `json:"vendor"`
	OS          string   `json:"os"`
	Protocol    string   `json:"protocol"` // SSH2, Telnet or "" when no login port is open
	Port        int      `json:"port"`
}

// Scan checks addrs and calls found for every device that answers and
// progress after every address. It returns when done or when ctx is cancelled.
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
	jobs := make(chan netip.Addr)
	var mu sync.Mutex
	done := 0
	var wg sync.WaitGroup
	for i := 0; i < opt.Parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range jobs {
				if r, ok := scanHost(ctx, a, opt); ok && ctx.Err() == nil {
					mu.Lock()
					found(r)
					mu.Unlock()
				}
				mu.Lock()
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
}

func scanHost(ctx context.Context, a netip.Addr, opt Options) (Result, bool) {
	r := Result{IP: a.String()}
	var mu sync.Mutex
	var wg sync.WaitGroup
	open := map[string]bool{}
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
			if p.Service == "ssh" && banner != "" {
				r.Banner = banner
			} else if p.Service == "telnet" && banner != "" && r.Banner == "" {
				r.Banner = banner
			}
			switch {
			case p.Service == "ssh" && (r.Protocol != "SSH2" || p.Port == 22):
				r.Protocol, r.Port = "SSH2", p.Port
			case p.Service == "telnet" && r.Protocol == "":
				r.Protocol, r.Port = "Telnet", p.Port
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
	wg.Wait()
	if len(open) == 0 {
		return r, false
	}
	for _, p := range append(opt.Probes, Probe{Service: "snmp"}) {
		if open[p.Service] {
			r.Services = append(r.Services, p.Service)
			delete(open, p.Service) // a service listed twice (two ports) is shown once
		}
	}
	if !opt.NoDNS {
		dctx, cancel := context.WithTimeout(ctx, time.Second)
		if names, err := net.DefaultResolver.LookupAddr(dctx, r.IP); err == nil && len(names) > 0 {
			r.DNS = strings.TrimSuffix(names[0], ".")
		}
		cancel()
	}
	switch {
	case r.SysName != "":
		r.Name = r.SysName
	case r.DNS != "":
		r.Name = r.DNS
	default:
		r.Name = r.IP
	}
	classify(&r)
	return r, true
}

// probe connects to a port; for SSH and Telnet it also reads the greeting.
func probe(ctx context.Context, a netip.Addr, p Probe, timeout time.Duration) (string, bool) {
	d := net.Dialer{Timeout: timeout}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(a.String(), strconv.Itoa(p.Port)))
	if err != nil {
		return "", false
	}
	defer c.Close()
	if p.Service != "ssh" && p.Service != "telnet" {
		return "", true
	}
	_ = c.SetReadDeadline(time.Now().Add(timeout + 700*time.Millisecond))
	buf := make([]byte, 512)
	n, _ := c.Read(buf)
	return greeting(buf[:n]), true
}

// greeting returns the first readable line (Telnet option negotiation removed).
func greeting(b []byte) string {
	var out bytes.Buffer
	for i := 0; i < len(b); i++ {
		if b[i] == 0xff && i+1 < len(b) { // IAC
			switch b[i+1] {
			case 0xfb, 0xfc, 0xfd, 0xfe: // WILL, WONT, DO, DONT + option
				i += 2
			case 0xfa: // sub-negotiation until IAC SE
				for i < len(b)-1 && !(b[i] == 0xff && b[i+1] == 0xf0) {
					i++
				}
				i++
			default:
				i++
			}
			continue
		}
		out.WriteByte(b[i])
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if line = strings.TrimSpace(printable(line)); line != "" {
			if len(line) > 120 {
				line = line[:120]
			}
			return line
		}
	}
	return ""
}
