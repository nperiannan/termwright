package topo

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Step is one read-only command and what its output is used for.
type Step struct {
	Cmd  string
	Role string // paging, mac, lldp, cdp, edp, ports, lag, stp
}

var plans = map[string][]Step{
	"exos": {
		{"disable cli paging", "paging"}, {"show switch", "mac"}, {"show lldp neighbors detail", "lldp"}, {"show edp ports all", "edp"},
		{"show ports information", "ports"}, {"show sharing", "lag"}, {"show stpd s0 ports", "stp"}, {"show vlan detail", "vlan"},
	},
	"voss": {
		{"terminal more disable", "paging"}, {"show sys-info", "mac"}, {"show lldp neighbor summary", "lldp"}, {"show interfaces gigabitEthernet interface", "ports"},
		{"show mlt", "lag"}, {"show spanning-tree mstp port role", "stp"}, {"show vlan basic", "vlanname"}, {"show vlan members", "vlan"},
	},
	"ios": {
		{"terminal length 0", "paging"}, {"show version", "name"}, {"show cdp neighbors detail", "cdp"}, {"show lldp neighbors detail", "lldp"},
		{"show interfaces status", "ports"}, {"show etherchannel summary", "lag"}, {"show spanning-tree", "stp"},
	},
	"nxos": {
		{"terminal length 0", "paging"}, {"show hostname", "name"}, {"show cdp neighbors detail", "cdp"}, {"show lldp neighbors detail", "lldp"},
		{"show interface status", "ports"}, {"show port-channel summary", "lag"}, {"show spanning-tree", "stp"},
	},
	"eos": {
		{"terminal length 0", "paging"}, {"show hostname", "name"}, {"show lldp neighbors detail | json", "lldp"}, {"show interfaces status", "ports"},
		{"show port-channel summary", "lag"}, {"show spanning-tree", "stp"},
	},
	"junos": {
		{"set cli screen-length 0", "paging"}, {"show version", "name"}, {"show lldp neighbors", "lldp"}, {"show interfaces terse", "ports"},
		{"show lacp interfaces", "lag"}, {"show spanning-tree interface", "stp"},
	},
}

// StepsDone says how many of the four reading steps of the map are finished after k commands of the OS's plan
// have run (1 = connected, 2 = identified, 3 = neighbours read, 4 = ports, spanning tree and LAGs read), and the
// command that is running now ("" when all are done). The steps are: connect, identify, neighbours, ports.
func StepsDone(os string, k int) (done int, now string) {
	steps := plans[os]
	stage := func(role string) int {
		switch role {
		case "paging", "name", "mac":
			return 2
		case "lldp", "cdp", "edp":
			return 3
		}
		return 4
	}
	done = 1
	for st := 2; st <= 4; st++ {
		ok := true
		for i, s := range steps {
			if stage(s.Role) == st && i >= k {
				ok = false
			}
		}
		if !ok {
			break
		}
		done = st
	}
	if k < len(steps) {
		now = steps[k].Cmd
	}
	return done, now
}

// Plan is the commands read for a device of this OS ("" = not supported).
func Plan(os string) []Step { return plans[os] }

// OSFor guesses the OS family from a device type or session type name ("Extreme EXOS", "Fabric Engine",
// "Cisco IOS-XE", "Arista EOS", "Juniper Junos"); "" when it is none of them.
func OSFor(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "exos") || strings.Contains(n, "switch engine"):
		return "exos"
	case strings.Contains(n, "voss") || strings.Contains(n, "fabric engine") || strings.Contains(n, "fabricengine"):
		return "voss"
	case strings.Contains(n, "nx-os") || strings.Contains(n, "nxos") || strings.Contains(n, "nexus"):
		return "nxos"
	case strings.Contains(n, "arista") || strings.Contains(n, "eos"):
		return "eos"
	case strings.Contains(n, "junos") || strings.Contains(n, "juniper"):
		return "junos"
	case strings.Contains(n, "cisco") || strings.Contains(n, "ios"):
		return "ios"
	}
	return ""
}

var pagingCmds = map[string]bool{"disable cli paging": true, "terminal more disable": true, "terminal length 0": true, "set cli screen-length 0": true}

// Allowed reports whether a command may be run by the map: a "show" command (optionally ending in
// "| json"), or one of the commands that only turn paging off for the connection. Nothing else, ever.
func Allowed(cmd string) bool {
	c := strings.TrimSpace(cmd)
	if c == "" || strings.ContainsAny(c, ";&`$<>\n\r\\") {
		return false
	}
	if pagingCmds[c] {
		return true
	}
	if !strings.HasPrefix(strings.ToLower(c), "show ") {
		return false
	}
	if i := strings.Index(c, "|"); i >= 0 {
		return strings.TrimSpace(c[i:]) == "| json" && !strings.Contains(c[i+1:], "|")
	}
	return true
}

// Runner runs the commands on one device in the background and returns what each printed.
type Runner func(ctx context.Context, cmds []string) ([]string, error)

// Target is the device to read.
type Target struct {
	Session, Name, OS, IP string
}

var refused = regexp.MustCompile(`(?i)invalid input|unrecognized command|unknown command|% invalid|incomplete command|syntax error|command not found|not supported|ambiguous command`)
var lldpOff = regexp.MustCompile(`(?i)lldp (is )?(not enabled|disabled|not running)|% lldp is not enabled`)
var exosMAC = regexp.MustCompile(`(?m)^System MAC:\s*(\S+)`)
var devModel = regexp.MustCompile(`(?m)^\s*(?:System Type|ModelName)\s*:\s*(\S+)`)
var exosName = regexp.MustCompile(`(?m)^SysName:\s*(.+?)\s*$`)
var vossName = regexp.MustCompile(`(?m)^\s*SysName\s*:\s*(.+?)\s*$`)
var iosName = regexp.MustCompile(`(?m)^(\S+) uptime is `)
var hostNameLine = regexp.MustCompile(`(?mi)^\s*Hostname:\s*(\S+)`)
var vossMAC = regexp.MustCompile(`(?m)^\s*BaseMacAddr\s*:\s*(\S+)`)

// Collect runs the plan of the target's OS and parses it. A command the device refuses becomes a note, not
// a failure; when nothing could be read at all, Err says so (the map then keeps the device's links).
func Collect(ctx context.Context, t Target, run Runner, now time.Time) Device {
	d := Device{Session: t.Session, Name: t.Name, OS: t.OS, IP: t.IP, Read: now, Neighbors: []Neighbor{}, Ports: []Port{}, LAGs: []LAG{}}
	steps := Plan(t.OS)
	if len(steps) == 0 {
		d.Err = "this device type is not read by the network map"
		return d
	}
	cmds := make([]string, len(steps))
	for i, s := range steps {
		if !Allowed(s.Cmd) {
			d.Err = "internal: a command is not on the read-only list: " + s.Cmd
			return d
		}
		cmds[i] = s.Cmd
	}
	outs, err := run(ctx, cmds)
	if err != nil && len(outs) == 0 {
		d.Err = err.Error()
		return d
	}
	if len(outs) != len(cmds) {
		d.Err = "the device answered " + itoa(len(outs)) + " of " + itoa(len(cmds)) + " commands"
		if err != nil {
			d.Err = err.Error()
		}
		return d
	}
	stp := map[string]string{}
	var vlanPorts map[string][]int
	got := 0
	for i, s := range steps {
		out := outs[i]
		if s.Role == "paging" {
			continue
		}
		if refused.MatchString(out) {
			d.Notes = append(d.Notes, s.Cmd+": not supported on this device")
			continue
		}
		got++
		switch s.Role {
		case "name":
			for _, re := range []*regexp.Regexp{iosName, hostNameLine} {
				if m := re.FindStringSubmatch(out); m != nil {
					d.SysName = m[1]
					break
				}
			}
			if f := strings.Fields(out); d.SysName == "" && t.OS == "nxos" && len(f) == 1 {
				d.SysName = f[0] // NX-OS prints the bare name
			}
		case "mac":
			if m := exosName.FindStringSubmatch(out); m != nil {
				d.SysName = m[1]
			} else if m := vossName.FindStringSubmatch(out); m != nil {
				d.SysName = m[1]
			}
			if m := devModel.FindStringSubmatch(out); m != nil {
				d.Model = m[1]
			}
			if m := exosMAC.FindStringSubmatch(out); m != nil {
				d.Chassis = NormMAC(m[1])
			} else if m := vossMAC.FindStringSubmatch(out); m != nil {
				d.Chassis = NormMAC(m[1])
			}
		case "lldp", "cdp", "edp":
			var ns []Neighbor
			switch {
			case t.OS == "exos" && s.Role == "lldp":
				ns = ParseEXOSLLDP(out)
			case t.OS == "exos" && s.Role == "edp":
				ns = ParseEXOSEDP(out)
			case t.OS == "voss":
				ns = ParseVOSSLLDP(out)
			case (t.OS == "ios" || t.OS == "nxos") && s.Role == "cdp":
				ns = ParseCiscoCDP(out)
			case t.OS == "ios" || t.OS == "nxos":
				ns = ParseCiscoLLDP(out)
			case t.OS == "eos":
				ns = ParseEOSLLDP(out)
			case t.OS == "junos":
				ns = ParseJunosLLDP(out)
			}
			if len(ns) == 0 && lldpOff.MatchString(out) {
				d.Notes = append(d.Notes, strings.ToUpper(string(Proto(s.Role)))+" is off on this device")
			}
			d.Neighbors = append(d.Neighbors, ns...)
		case "ports":
			switch t.OS {
			case "exos":
				d.Ports = ParseEXOSPorts(out)
			case "voss":
				d.Ports = ParseVOSSInterfaces(out)
			case "junos":
				d.Ports = ParseJunosTerse(out)
			default:
				d.Ports = ParseIfStatus(out)
			}
		case "lag":
			switch t.OS {
			case "exos":
				d.LAGs = ParseEXOSSharing(out)
			case "voss":
				d.LAGs = ParseVOSSMLT(out)
			case "junos":
				d.LAGs = ParseJunosLACP(out)
			default:
				d.LAGs = ParseEtherchannel(out)
			}
		case "vlan":
			switch t.OS {
			case "exos":
				var names map[int]string
				vlanPorts, names = ParseEXOSVLANDetail(out)
				d.VLANNames = mergeNames(d.VLANNames, names)
			case "voss":
				vlanPorts = ParseVOSSVLANMembers(out)
			}
		case "vlanname":
			if t.OS == "voss" {
				d.VLANNames = mergeNames(d.VLANNames, ParseVOSSVLANBasic(out))
			}
		case "stp":
			switch t.OS {
			case "exos":
				stp = ParseEXOSSTP(out)
			case "voss":
				stp = ParseVOSSSTP(out)
			case "junos":
				stp = ParseJunosSTP(out)
			default:
				stp = ParseCiscoSTP(out)
			}
		}
	}
	if got == 0 {
		d.Err = errors.New("no command could be read").Error()
		return d
	}
	for k, v := range vlanPorts {
		for i := range d.Ports {
			if NormPort(k) == NormPort(d.Ports[i].Name) {
				d.Ports[i].VLANs = v
			}
		}
	}
	for i := range d.Ports {
		if s, ok := stp[d.Ports[i].Name]; ok {
			d.Ports[i].STP = s
			continue
		}
		for k, s := range stp {
			if NormPort(k) == NormPort(d.Ports[i].Name) {
				d.Ports[i].STP = s
			}
		}
	}
	return d
}

func mergeNames(a, b map[int]string) map[int]string {
	if a == nil {
		a = map[int]string{}
	}
	for k, v := range b {
		if v != "" {
			a[k] = v
		}
	}
	return a
}
