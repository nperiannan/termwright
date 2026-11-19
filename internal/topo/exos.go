package topo

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	exosLLDPPort = regexp.MustCompile(`^LLDP Port (\S+) detected \d+ neighbor`)
	exosLLDPNbr  = regexp.MustCompile(`^\s+Neighbor:\s+\S+, age (\d+) seconds`)
	exosChassis  = regexp.MustCompile(`^\s+Chassis ID\s*:\s*(\S+)`)
	exosPortID   = regexp.MustCompile(`^\s+Port ID\s*:\s*"?(.*?)"?\s*$`)
	exosSysName  = regexp.MustCompile(`^\s+(?:- )?System Name:\s*"(.*)"`)
	exosMgmt     = regexp.MustCompile(`^\s+Management Address\s*:\s*(\S+)`)
	exosCaps     = regexp.MustCompile(`^\s+Enabled Capabilities:\s*"(.*)"`)
)

// ParseEXOSLLDP reads "show lldp neighbors detail".
func ParseEXOSLLDP(text string) []Neighbor {
	var out []Neighbor
	var cur *Neighbor
	port := ""
	flush := func() {
		if cur != nil && (cur.Chassis != "" || cur.RemotePort != "" || cur.Name != "") {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		if m := exosLLDPPort.FindStringSubmatch(l); m != nil {
			flush()
			port = m[1]
			continue
		}
		if m := exosLLDPNbr.FindStringSubmatch(l); m != nil {
			flush()
			age, _ := strconv.Atoi(m[1])
			cur = &Neighbor{Proto: LLDP, LocalPort: port, Age: age}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case exosChassis.MatchString(l):
			cur.Chassis = NormMAC(exosChassis.FindStringSubmatch(l)[1])
		case exosPortID.MatchString(l):
			cur.RemotePort = exosPortID.FindStringSubmatch(l)[1]
		case exosSysName.MatchString(l):
			cur.Name = exosSysName.FindStringSubmatch(l)[1]
		case exosMgmt.MatchString(l):
			if ip := exosMgmt.FindStringSubmatch(l)[1]; !isUnsetIP(ip) && cur.IP == "" {
				cur.IP = ip
			}
		case exosCaps.MatchString(l):
			cur.Caps = exosCaps.FindStringSubmatch(l)[1]
		}
	}
	flush()
	return out
}

// Port, Neighbor, Neighbor-ID (00:00 and the MAC), Remote Port, Age, Num Vlans
var exosEDPRow = regexp.MustCompile(`^(\S+)\s+(.+?)\s+((?:[0-9a-fA-F]{2}:){7}[0-9a-fA-F]{2})\s+(\S+)\s+(\d+)\s+(\d+)\s*$`)

// ParseEXOSEDP reads "show edp ports all".
func ParseEXOSEDP(text string) []Neighbor {
	var out []Neighbor
	for _, l := range strings.Split(text, "\n") {
		m := exosEDPRow.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil {
			continue
		}
		age, _ := strconv.Atoi(m[5])
		out = append(out, Neighbor{Proto: EDP, LocalPort: m[1], Name: m[2], Chassis: NormMAC(m[3][6:]), RemotePort: m[4], Age: age})
	}
	return out
}

var exosPortRow = regexp.MustCompile(`^(\d+(?::\d+)?)\*?\s+(\S+)\s+(active|ready|disabled|removed|loopback|not\s+present|d)\b`)

// ParseEXOSPorts reads "show ports <list> information" (flags column: E enabled, D disabled; Link State
// column: active = link up).
func ParseEXOSPorts(text string) []Port {
	var out []Port
	for _, l := range strings.Split(text, "\n") {
		m := exosPortRow.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil {
			continue
		}
		flags := m[2]
		p := Port{Name: m[1], Admin: strings.Contains(flags, "E") && !strings.Contains(flags, "D"), Oper: m[3] == "active"}
		out = append(out, p)
	}
	return out
}

var exosShareStates = map[string]bool{"A": true, "D": true, "R": true, "NP": true, "L": true}

// ParseEXOSSharing reads "show sharing". The table's first row of a group has the master ports and the
// control (Static or LACP); the next rows are more members of the same group.
func ParseEXOSSharing(text string) []LAG {
	var out []LAG
	var cur *LAG
	in := false
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.HasPrefix(l, "====") {
			in = !in
			continue
		}
		if !in {
			continue
		}
		f := strings.Fields(l)
		// find "<group> Y|N <state>": the member's port, whether it is a member, its link state
		i := -1
		for k := 1; k+1 < len(f); k++ {
			if (f[k] == "Y" || f[k] == "N") && exosShareStates[f[k+1]] {
				i = k
				break
			}
		}
		if i < 1 {
			continue
		}
		port, state := f[i-1], f[i+1]
		if len(f) > 2 && (strings.EqualFold(f[2], "static") || strings.EqualFold(f[2], "lacp")) {
			out = append(out, LAG{Name: f[0], Mode: strings.ToLower(f[2])})
			cur = &out[len(out)-1]
		}
		if cur == nil {
			continue
		}
		cur.Members = append(cur.Members, port)
		if state == "A" {
			cur.Active = append(cur.Active, port)
		}
	}
	return out
}

var exosSTPRow = regexp.MustCompile(`^(\d+(?::\d+)?)(\*?)\s+\S+\s+(BLOCKING|FORWARDING|DISABLED|LEARNING|LISTENING|DISCARDING)\s+\d+\s+(\S+)`)

// ParseEXOSSTP reads "show stpd <name> ports".
func ParseEXOSSTP(text string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(text, "\n") {
		m := exosSTPRow.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil {
			continue
		}
		out[m[1]] = stpWord(m[3])
	}
	return out
}

func stpWord(s string) string {
	switch strings.ToLower(s) {
	case "forwarding":
		return "forwarding"
	case "blocking", "discarding", "listening":
		return "blocked"
	case "learning":
		return "learning"
	case "disabled":
		return "disabled"
	}
	return ""
}
