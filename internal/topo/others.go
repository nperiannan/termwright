package topo

// Parsers for Cisco IOS / NX-OS, Arista EOS and Juniper Junos. Unlike EXOS and VOSS these were written from
// the vendors' documented output, not from a device in the lab, and their tests use samples written in that
// documented layout: check them against a real device before relying on the less common variations.

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

var (
	cdpDevice  = regexp.MustCompile(`^Device ID:\s*(.+?)\s*$`)
	cdpIP      = regexp.MustCompile(`^\s*(?:IP address|IPv4 Address):\s*(\S+)`)
	cdpPlat    = regexp.MustCompile(`^Platform:\s*(.*?),\s*Capabilities:\s*(.*?)\s*$`)
	cdpIface   = regexp.MustCompile(`^Interface:\s*([^,]+),\s*Port ID \(outgoing port\):\s*(\S+)`)
	cdpHold    = regexp.MustCompile(`^Holdtime\s*:\s*(\d+)`)
	lldpLocal  = regexp.MustCompile(`^Local Intf:\s*(\S+)`)
	lldpChas   = regexp.MustCompile(`^Chassis id:\s*(\S+)`)
	lldpPortID = regexp.MustCompile(`^Port id:\s*(\S+)`)
	lldpName   = regexp.MustCompile(`^System Name:\s*(.*?)\s*$`)
	lldpRemain = regexp.MustCompile(`^Time remaining:\s*(\d+)`)
	lldpIPLine = regexp.MustCompile(`^\s+IP:\s*(\S+)`)
)

// ParseCiscoCDP reads "show cdp neighbors detail" (IOS, NX-OS). CDP prints the time left, not the age, so
// the age is taken as the 180 s default holdtime minus it.
func ParseCiscoCDP(text string) []Neighbor {
	var out []Neighbor
	var cur *Neighbor
	hold := -1
	flush := func() {
		if cur != nil && cur.LocalPort != "" {
			if hold >= 0 {
				cur.Age = max(0, 180-hold)
			}
			out = append(out, *cur)
		}
		cur, hold = nil, -1
	}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		if m := cdpDevice.FindStringSubmatch(l); m != nil {
			flush()
			cur = &Neighbor{Proto: CDP, Name: m[1], Age: -1}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case cdpIP.MatchString(l) && cur.IP == "":
			cur.IP = cdpIP.FindStringSubmatch(l)[1]
		case cdpPlat.MatchString(l):
			cur.Caps = cdpPlat.FindStringSubmatch(l)[2]
		case cdpIface.MatchString(l):
			m := cdpIface.FindStringSubmatch(l)
			cur.LocalPort, cur.RemotePort = m[1], m[2]
		case cdpHold.MatchString(l):
			hold, _ = strconv.Atoi(cdpHold.FindStringSubmatch(l)[1])
		}
	}
	flush()
	return out
}

// ParseCiscoLLDP reads "show lldp neighbors detail" (IOS, NX-OS); the age is the 120 s default minus the
// time remaining.
func ParseCiscoLLDP(text string) []Neighbor {
	var out []Neighbor
	var cur *Neighbor
	flush := func() {
		if cur != nil && cur.LocalPort != "" {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		if m := lldpLocal.FindStringSubmatch(l); m != nil {
			flush()
			cur = &Neighbor{Proto: LLDP, LocalPort: m[1], Age: -1}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case lldpChas.MatchString(l):
			c := lldpChas.FindStringSubmatch(l)[1]
			if mac := NormMAC(c); mac != "" {
				cur.Chassis = mac
			}
		case lldpPortID.MatchString(l):
			cur.RemotePort = lldpPortID.FindStringSubmatch(l)[1]
		case lldpName.MatchString(l):
			cur.Name = lldpName.FindStringSubmatch(l)[1]
		case lldpRemain.MatchString(l):
			n, _ := strconv.Atoi(lldpRemain.FindStringSubmatch(l)[1])
			cur.Age = max(0, 120-n)
		case lldpIPLine.MatchString(l) && cur.IP == "":
			cur.IP = lldpIPLine.FindStringSubmatch(l)[1]
		}
	}
	flush()
	return out
}

var (
	ecRow    = regexp.MustCompile(`^\s*(?:\d+\s+)?(\S+?)\(([A-Za-z]+)\)\s+(\S+)\s+(.*)$`)
	ecMember = regexp.MustCompile(`(\S+?)\(([A-Za-z+]+)\)`)
)

// ParseEtherchannel reads "show etherchannel summary" (Cisco) and "show port-channel summary" (Arista, NX-OS):
// "Po1(SU)  LACP  Gi1/0/1(P) Gi1/0/2(P)". A member with P in its flags is bundled (active).
func ParseEtherchannel(text string) []LAG {
	var out []LAG
	for _, l := range strings.Split(text, "\n") {
		m := ecRow.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil || !strings.HasPrefix(strings.ToLower(m[1]), "po") {
			continue
		}
		g := LAG{Name: m[1], Mode: "static"}
		if strings.HasPrefix(strings.ToUpper(m[3]), "LACP") {
			g.Mode = "lacp"
		}
		for _, mm := range ecMember.FindAllStringSubmatch(m[4], -1) {
			g.Members = append(g.Members, mm[1])
			if strings.Contains(mm[2], "P") {
				g.Active = append(g.Active, mm[1])
			}
		}
		out = append(out, g)
	}
	return out
}

var ifStatusRow = regexp.MustCompile(`^(\S+)\s+.*?\b(connected|notconnect|disabled|err-?disabled|monitoring|inactive|suspended|sfpAbsent)\b`)

// ParseIfStatus reads "show interfaces status" (Cisco, Arista): connected = link up, disabled = shut down.
func ParseIfStatus(text string) []Port {
	var out []Port
	for _, l := range strings.Split(text, "\n") {
		m := ifStatusRow.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil || strings.EqualFold(m[1], "Port") {
			continue
		}
		out = append(out, Port{Name: m[1], Admin: m[2] != "disabled" && !strings.HasPrefix(m[2], "err"), Oper: m[2] == "connected"})
	}
	return out
}

var stpIOSRow = regexp.MustCompile(`^(\S+)\s+(Root|Desg|Altn|Back|Mstr|Bound)\s+(FWD|BLK|LRN|LIS|DIS)\b`)

// ParseCiscoSTP reads "show spanning-tree" (Cisco, Arista): a port is blocked when it blocks in every
// instance it appears in, forwarding when it forwards in any.
func ParseCiscoSTP(text string) map[string]string {
	fwd, blk := map[string]bool{}, map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		m := stpIOSRow.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil {
			continue
		}
		switch m[3] {
		case "FWD":
			fwd[m[1]] = true
		case "BLK":
			blk[m[1]] = true
		default:
			fwd[m[1]] = fwd[m[1]] || false
			if _, ok := blk[m[1]]; !ok && !fwd[m[1]] {
				blk[m[1]] = false
			}
		}
	}
	out := map[string]string{}
	for p := range fwd {
		out[p] = "forwarding"
	}
	for p, b := range blk {
		if _, ok := out[p]; !ok {
			if b {
				out[p] = "blocked"
			} else {
				out[p] = "learning"
			}
		}
	}
	return out
}

// ParseEOSLLDP reads "show lldp neighbors detail | json" (Arista EOS).
func ParseEOSLLDP(text string) []Neighbor {
	i := strings.IndexByte(text, '{')
	if i < 0 {
		return nil
	}
	var doc struct {
		Neighbors map[string]struct {
			Info []struct {
				ChassisID string `json:"chassisId"`
				SysName   string `json:"systemName"`
				TTL       int    `json:"ttl"`
				Iface     struct {
					ID  string `json:"interfaceId"`
					ID2 string `json:"interfaceId_v2"`
				} `json:"neighborInterfaceInfo"`
				Mgmt []struct {
					Address string `json:"address"`
				} `json:"managementAddresses"`
			} `json:"lldpNeighborInfo"`
		} `json:"lldpNeighbors"`
	}
	dec := json.NewDecoder(strings.NewReader(text[i:]))
	if dec.Decode(&doc) != nil {
		return nil
	}
	var out []Neighbor
	for port, v := range doc.Neighbors {
		for _, n := range v.Info {
			rp := n.Iface.ID2
			if rp == "" {
				rp = strings.Trim(n.Iface.ID, `"`)
			}
			nb := Neighbor{Proto: LLDP, LocalPort: port, Chassis: NormMAC(n.ChassisID), RemotePort: rp, Name: n.SysName, Age: -1}
			if len(n.Mgmt) > 0 {
				nb.IP = n.Mgmt[0].Address
			}
			out = append(out, nb)
		}
	}
	return out
}

var junosLLDPRow = regexp.MustCompile(`^(\S+)\s+(\S+)\s+((?:[0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2})\s+(\S+)\s*(.*?)\s*$`)

// ParseJunosLLDP reads "show lldp neighbors" (Junos): local interface, parent interface, chassis ID, port
// info, system name.
func ParseJunosLLDP(text string) []Neighbor {
	var out []Neighbor
	for _, l := range strings.Split(text, "\n") {
		m := junosLLDPRow.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil {
			continue
		}
		out = append(out, Neighbor{Proto: LLDP, LocalPort: m[1], Chassis: NormMAC(m[3]), RemotePort: m[4], Name: m[5], Age: -1})
	}
	return out
}

var junosTerse = regexp.MustCompile(`^(\S+?)(?:\.\d+)?\s+(up|down)\s+(up|down)\b`)

// ParseJunosTerse reads "show interfaces terse": admin and link state per physical interface (the unit
// lines such as "ge-0/0/0.0" are folded into it).
func ParseJunosTerse(text string) []Port {
	var out []Port
	seen := map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, " ") {
			continue
		}
		m := junosTerse.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil || seen[m[1]] || strings.Contains(strings.Fields(l)[0], ".") {
			continue
		}
		seen[m[1]] = true
		out = append(out, Port{Name: m[1], Admin: m[2] == "up", Oper: m[3] == "up"})
	}
	return out
}

var junosAE = regexp.MustCompile(`^Aggregated interface:\s*(\S+)`)

// ParseJunosLACP reads "show lacp interfaces": a member is active when it distributes and collects.
func ParseJunosLACP(text string) []LAG {
	var out []LAG
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		if m := junosAE.FindStringSubmatch(l); m != nil {
			out = append(out, LAG{Name: m[1], Mode: "lacp"})
			continue
		}
		f := strings.Fields(l)
		if len(out) == 0 || len(f) < 6 || f[1] != "Actor" {
			continue
		}
		g := &out[len(out)-1]
		g.Members = append(g.Members, f[0])
		if f[4] == "Yes" && f[5] == "Yes" {
			g.Active = append(g.Active, f[0])
		}
	}
	return out
}

var junosSTP = regexp.MustCompile(`^(\S+?)(?:\.\d+)?\s+\d+:\d+\s+.*\s(FWD|BLK|LRN|LIS|DIS)\s+\w+\s*$`)

// ParseJunosSTP reads "show spanning-tree interface".
func ParseJunosSTP(text string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(text, "\n") {
		if m := junosSTP.FindStringSubmatch(strings.TrimRight(l, "\r")); m != nil {
			out[m[1]] = map[string]string{"FWD": "forwarding", "BLK": "blocked", "LRN": "learning", "LIS": "blocked", "DIS": "disabled"}[m[2]]
		}
	}
	return out
}
