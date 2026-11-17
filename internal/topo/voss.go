package topo

import (
	"regexp"
	"strings"
)

var (
	vossPort    = regexp.MustCompile(`^Port:\s+(\S+)\s+Index`)
	vossChassis = regexp.MustCompile(`^\s+ChassisId\s*:\s*.*?(\S+)\s*$`)
	vossPortID  = regexp.MustCompile(`^\s+PortId\s*:\s*\S+\s+(.*?)\s*$`)
	vossSysName = regexp.MustCompile(`^\s+SysName\s*:\s*(.*?)\s*$`)
	vossAddr    = regexp.MustCompile(`^\s+Address\s*:\s*(\S+)`)
	vossCap     = regexp.MustCompile(`^\s+SysCap\s*:\s*(.*?)\s*$`)
)

// ParseVOSSLLDP reads "show lldp neighbor". It prints no age.
func ParseVOSSLLDP(text string) []Neighbor {
	var out []Neighbor
	var cur *Neighbor
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		if m := vossPort.FindStringSubmatch(l); m != nil {
			flush()
			cur = &Neighbor{Proto: LLDP, LocalPort: m[1], Age: -1}
			continue
		}
		if strings.HasPrefix(l, "----") {
			flush()
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case vossChassis.MatchString(l):
			cur.Chassis = NormMAC(vossChassis.FindStringSubmatch(l)[1])
		case vossPortID.MatchString(l):
			cur.RemotePort = vossPortID.FindStringSubmatch(l)[1]
		case vossSysName.MatchString(l):
			cur.Name = vossSysName.FindStringSubmatch(l)[1]
		case vossAddr.MatchString(l):
			if ip := vossAddr.FindStringSubmatch(l)[1]; !isUnsetIP(ip) {
				cur.IP = ip
			}
		case vossCap.MatchString(l):
			if c := strings.Join(strings.Fields(vossCap.FindStringSubmatch(l)[1]), " "); c != "/" {
				cur.Caps = c
			}
		}
	}
	flush()
	return out
}

var vossIfRow = regexp.MustCompile(`^(\d+/\d+(?:/\d+)?)\s+\d+\s+.*\s(up|down)\s+(up|down)\s*$`)

// ParseVOSSInterfaces reads "show interfaces gigabitEthernet" (ADMIN and OPERATE columns).
func ParseVOSSInterfaces(text string) []Port {
	var out []Port
	for _, l := range strings.Split(text, "\n") {
		if m := vossIfRow.FindStringSubmatch(strings.TrimRight(l, "\r")); m != nil {
			out = append(out, Port{Name: m[1], Admin: m[2] == "up", Oper: m[3] == "up"})
		}
	}
	return out
}

var vossSTPRow = regexp.MustCompile(`^(\d+/\d+(?:/\d+)?)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s*$`)

// ParseVOSSSTP reads "show spanning-tree mstp port role" (role, state, STP status, oper status).
func ParseVOSSSTP(text string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(text, "\n") {
		m := vossSTPRow.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil {
			continue
		}
		switch {
		case strings.EqualFold(m[4], "disabled"):
			out[m[1]] = "disabled"
		default:
			out[m[1]] = stpWord(m[3])
		}
	}
	return out
}

var vossMLTRow = regexp.MustCompile(`^(\d+)\s+\d+\s+(\S+)\s+(?:access|trunk)\s+\S+\s+\S+\s*(.*?)\s+(CONFIG|LACP|SMLT|\S+)\s*$`)

// ParseVOSSMLT reads the first table of "show mlt" (MLTID, IFINDEX, NAME, PORT TYPE, ADMIN, CURRENT, PORT
// MEMBERS, ORIGIN, VLAN IDS). lacp lists the MLT ids whose LACP is enabled (from the second table), so the
// mode is right. The members column is read as comma lists and ranges ("1/3,1/4", "1/3-1/4"); a LACP MLT
// has none here until its ports are bundled.
func ParseVOSSMLT(text string) []LAG {
	var out []LAG
	lacp := map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		f := strings.Fields(l)
		// MLTID IFINDEX DESIGNATED-PORTS LACP-ADMIN LACP-OPER
		if n := len(f); n >= 5 && isDigits(f[0]) && isDigits(f[1]) && (f[n-1] == "up" || f[n-1] == "down") && (f[n-2] == "enable" || f[n-2] == "disable") {
			lacp[f[0]] = f[n-2] == "enable"
		}
	}
	seen := map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		m := vossMLTRow.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		mode := "static"
		if lacp[m[1]] {
			mode = "lacp"
		}
		out = append(out, LAG{Name: m[1], Mode: mode, Members: expandPorts(strings.Fields(m[3]))})
	}
	return out
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// expandPorts turns "1/3-1/4" and "1/3,1/4" into single ports (ranges of the last number in a slot/port name).
func expandPorts(tok []string) []string {
	var out []string
	for _, t := range tok {
		for _, p := range strings.Split(t, ",") {
			if p == "" {
				continue
			}
			if a, b, ok := strings.Cut(p, "-"); ok {
				ia, ib := strings.LastIndex(a, "/"), strings.LastIndex(b, "/")
				if ia >= 0 && ib >= 0 && a[:ia] == b[:ib] {
					lo, hi := atoi(a[ia+1:]), atoi(b[ib+1:])
					if lo > 0 && hi >= lo && hi-lo < 64 {
						for n := lo; n <= hi; n++ {
							out = append(out, a[:ia+1]+itoa(n))
						}
						continue
					}
				}
			}
			out = append(out, p)
		}
	}
	return out
}
