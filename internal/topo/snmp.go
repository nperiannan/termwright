package topo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// VB is one SNMP value (strings as the raw bytes they are).
type VB struct {
	OID   string
	Bytes []byte
	Int   int64
	IsInt bool
}

// Walker reads every value below an OID (read-only SNMP).
type Walker func(ctx context.Context, root string) ([]VB, error)

const (
	oidIfName    = "1.3.6.1.2.1.31.1.1.1.1"
	oidIfDescr   = "1.3.6.1.2.1.2.2.1.2"
	oidIfAdmin   = "1.3.6.1.2.1.2.2.1.7"
	oidIfOper    = "1.3.6.1.2.1.2.2.1.8"
	oidLldpLoc   = "1.0.8802.1.1.2.1.3.7.1"
	oidLldpRem   = "1.0.8802.1.1.2.1.4.1.1"
	oidStpState  = "1.3.6.1.2.1.17.2.15.1.3"
	oidBasePort  = "1.3.6.1.2.1.17.1.4.1.2"
	oidLagAttach = "1.2.840.10006.300.43.1.2.1.1.13"
)

func suffix(oid, root string) []string {
	if !strings.HasPrefix(oid, root+".") {
		return nil
	}
	return strings.Split(oid[len(root)+1:], ".")
}

func text(b []byte) string {
	s := strings.TrimRight(string(b), "\x00")
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return ""
		}
	}
	return strings.TrimSpace(s)
}

func macOf(b []byte) string {
	if len(b) != 6 {
		return ""
	}
	return NormMAC(fmt.Sprintf("%02x%02x%02x%02x%02x%02x", b[0], b[1], b[2], b[3], b[4], b[5]))
}

// CollectSNMP reads the same facts as Collect from SNMP: interface names and link state (IF-MIB), LLDP
// neighbours (LLDP-MIB), spanning-tree port state (BRIDGE-MIB) and LACP bundles (IEEE8023-LAG-MIB). SNMP
// tells nothing about static LAGs or EDP/CDP, and no age, so those stay the CLI's.
func CollectSNMP(ctx context.Context, t Target, walk Walker, now time.Time) Device {
	d := Device{Session: t.Session, Name: t.Name, OS: t.OS, IP: t.IP, Read: now, Neighbors: []Neighbor{}, Ports: []Port{}, LAGs: []LAG{}}
	get := func(root string) ([]VB, error) { return walk(ctx, root) }
	names, err := get(oidIfName)
	if err != nil {
		d.Err = "SNMP: " + err.Error()
		return d
	}
	ifname := map[string]string{}
	for _, v := range names {
		if s := suffix(v.OID, oidIfName); len(s) == 1 {
			ifname[s[0]] = text(v.Bytes)
		}
	}
	if len(ifname) == 0 { // no ifXTable: the older descriptions
		descr, _ := get(oidIfDescr)
		for _, v := range descr {
			if s := suffix(v.OID, oidIfDescr); len(s) == 1 {
				ifname[s[0]] = text(v.Bytes)
			}
		}
	}
	if len(ifname) == 0 {
		d.Err = "SNMP: the device answered but lists no interfaces (is the community right?)"
		return d
	}
	admin, oper := map[string]bool{}, map[string]bool{}
	if vs, err := get(oidIfAdmin); err == nil {
		for _, v := range vs {
			if s := suffix(v.OID, oidIfAdmin); len(s) == 1 {
				admin[s[0]] = v.Int == 1
			}
		}
	}
	if vs, err := get(oidIfOper); err == nil {
		for _, v := range vs {
			if s := suffix(v.OID, oidIfOper); len(s) == 1 {
				oper[s[0]] = v.Int == 1
			}
		}
	}
	idx := make([]string, 0, len(ifname))
	for i := range ifname {
		idx = append(idx, i)
	}
	sort.Slice(idx, func(a, b int) bool { x, _ := strconv.Atoi(idx[a]); y, _ := strconv.Atoi(idx[b]); return x < y })
	pos := map[string]int{}
	for _, i := range idx {
		if ifname[i] == "" {
			continue
		}
		pos[i] = len(d.Ports)
		d.Ports = append(d.Ports, Port{Name: ifname[i], Admin: admin[i], Oper: oper[i]})
	}

	// LLDP: which port is lldpLocPortNum n, and what each remote entry says
	locName := map[string]string{}
	if vs, err := get(oidLldpLoc + ".3"); err == nil {
		sub := map[string]int64{}
		if ss, err := get(oidLldpLoc + ".2"); err == nil {
			for _, v := range ss {
				if s := suffix(v.OID, oidLldpLoc+".2"); len(s) == 1 {
					sub[s[0]] = v.Int
				}
			}
		}
		for _, v := range vs {
			s := suffix(v.OID, oidLldpLoc+".3")
			if len(s) != 1 {
				continue
			}
			id := text(v.Bytes)
			switch {
			case sub[s[0]] == 5 && id != "":
				locName[s[0]] = id
			case id != "" && ifname[id] != "":
				locName[s[0]] = ifname[id]
			case ifname[s[0]] != "":
				locName[s[0]] = ifname[s[0]]
			case id != "":
				locName[s[0]] = id
			}
		}
	}
	type rem struct {
		port string
		n    Neighbor
	}
	ents := map[string]*rem{}
	rows, err := get(oidLldpRem)
	if err == nil {
		sub := map[string]int64{}
		psub := map[string]int64{}
		for _, v := range rows {
			s := suffix(v.OID, oidLldpRem)
			if len(s) != 4 {
				continue
			}
			key := s[1] + "." + s[2] + "." + s[3]
			e := ents[key]
			if e == nil {
				port := locName[s[2]]
				if port == "" {
					port = ifname[s[2]]
				}
				if port == "" {
					port = s[2]
				}
				e = &rem{port, Neighbor{Proto: LLDP, LocalPort: port, Age: -1}}
				ents[key] = e
			}
			switch s[0] {
			case "4":
				sub[key] = v.Int
			case "5":
				if sub[key] == 4 || len(v.Bytes) == 6 && sub[key] == 0 {
					e.n.Chassis = macOf(v.Bytes)
				}
			case "6":
				psub[key] = v.Int
			case "7":
				if psub[key] == 3 {
					e.n.RemotePort = macOf(v.Bytes)
				} else {
					e.n.RemotePort = text(v.Bytes)
				}
			case "9":
				e.n.Name = text(v.Bytes)
			}
		}
	}
	keys := make([]string, 0, len(ents))
	for k := range ents {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		n := ents[k].n
		if n.Chassis != "" || n.Name != "" || n.RemotePort != "" {
			d.Neighbors = append(d.Neighbors, n)
		}
	}
	if len(d.Neighbors) == 0 && err != nil {
		d.Notes = append(d.Notes, "SNMP: LLDP-MIB not answered")
	}

	// spanning tree: dot1dBasePort -> ifIndex -> port
	if st, err := get(oidStpState); err == nil && len(st) > 0 {
		base := map[string]string{}
		if bs, err := get(oidBasePort); err == nil {
			for _, v := range bs {
				if s := suffix(v.OID, oidBasePort); len(s) == 1 {
					base[s[0]] = strconv.FormatInt(v.Int, 10)
				}
			}
		}
		for _, v := range st {
			s := suffix(v.OID, oidStpState)
			if len(s) != 1 {
				continue
			}
			ifi := base[s[0]]
			if ifi == "" {
				ifi = s[0]
			}
			if p, ok := pos[ifi]; ok {
				d.Ports[p].STP = map[int64]string{1: "disabled", 2: "blocked", 3: "blocked", 4: "learning", 5: "forwarding"}[v.Int]
			}
		}
	}
	// LACP bundles: a port whose attached aggregator is not 0
	if ag, err := get(oidLagAttach); err == nil {
		by := map[string]*LAG{}
		var order []string
		for _, v := range ag {
			s := suffix(v.OID, oidLagAttach)
			if len(s) != 1 || v.Int == 0 {
				continue
			}
			a := strconv.FormatInt(v.Int, 10)
			g := by[a]
			if g == nil {
				name := ifname[a]
				if name == "" {
					name = a
				}
				g = &LAG{Name: name, Mode: "lacp"}
				by[a] = g
				order = append(order, a)
			}
			if n := ifname[s[0]]; n != "" {
				g.Members = append(g.Members, n)
			}
		}
		for _, a := range order {
			d.LAGs = append(d.LAGs, *by[a])
		}
	}
	if len(d.Neighbors) == 0 && len(d.Ports) == 0 {
		d.Err = errors.New("SNMP: nothing could be read").Error()
	}
	return d
}
