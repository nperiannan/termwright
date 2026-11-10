package sim

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Group asks for Count switches of one model, named by Names ("lab-{n}").
type Group struct {
	OS    string `json:"os,omitempty"` // the OS (for a model that runs more than one)
	Model string `json:"model"`
	Count int    `json:"count"`
	Names string `json:"names"`
}

// Cabling presets.
const (
	CableLeafSpine = "leaf-spine" // the first two switches as the core pair with a link between them; every other switch to each core
	CableChain     = "chain"
	CableRing      = "ring"
	CableNone      = "none"
)

// Plan makes the specs and cables of a new lab (or of switches added to one:
// have = the switches there already). Management addresses come from mgmtNet
// starting at .11, skipping addresses in use; SSH ports from firstPort up,
// skipping ports in use.
func Plan(pack *Pack, groups []Group, mgmtNet, cabling, password string, firstPort int, have []Spec, haveLinks []Link) ([]Spec, []Link, error) {
	if pack == nil {
		return nil, nil, errors.New("no device pack is loaded")
	}
	pfx, err := netip.ParsePrefix(strings.TrimSpace(mgmtNet))
	if err != nil || !pfx.Addr().Is4() || pfx.Bits() > 29 {
		return nil, nil, fmt.Errorf("management network %q: use an IPv4 network like 10.99.0.0/24", mgmtNet)
	}
	pfx = pfx.Masked()
	usedIP, usedPort, usedName := map[netip.Addr]bool{}, map[int]bool{}, map[string]bool{}
	for _, s := range have {
		if p, err := netip.ParsePrefix(s.Saved.MgmtIP); err == nil {
			usedIP[p.Addr()] = true
		}
		usedPort[s.Port] = true
		usedName[strings.ToLower(s.Saved.Hostname)] = true
	}
	ip := pfx.Addr()
	for i := 0; i < 11; i++ { // from .11
		ip = ip.Next()
	}
	port := firstPort
	total := 0
	var out []Spec
	for _, g := range groups {
		m, ok := pack.ModelOf(g.Model)
		if !ok {
			return nil, nil, fmt.Errorf("unknown model %q", g.Model)
		}
		if g.Count < 0 || g.Count > 200 {
			return nil, nil, fmt.Errorf("%s: 0 to 200 switches", g.Model)
		}
		total += g.Count
		if total+len(have) > 500 {
			return nil, nil, errors.New("a simulated lab holds up to 500 switches")
		}
		names := strings.TrimSpace(g.Names)
		if names == "" {
			names = strings.ToLower(strings.Split(m.SKU, "-")[0]) + "-{n}"
		}
		if !strings.Contains(names, "{n}") {
			names += "-{n}"
		}
		n := 1
		for i := 0; i < g.Count; i++ {
			var name string
			for {
				name = strings.ReplaceAll(names, "{n}", strconv.Itoa(n))
				n++
				if !usedName[strings.ToLower(name)] {
					break
				}
			}
			usedName[strings.ToLower(name)] = true
			for usedIP[ip] {
				ip = ip.Next()
			}
			if !pfx.Contains(ip.Next()) { // keep the broadcast address free
				return nil, nil, fmt.Errorf("management network %s is full: remove switches or use a bigger network (like 10.99.0.0/16)", pfx)
			}
			usedIP[ip] = true
			for usedPort[port] {
				port++
			}
			usedPort[port] = true
			os := ""
			for _, o := range m.OS {
				if os == "" || strings.EqualFold(o, g.OS) {
					os = o
				}
			}
			if os == "" {
				return nil, nil, fmt.Errorf("%s: the pack gives it no operating system", m.SKU)
			}
			sp := Spec{ID: randID(), OS: os, Model: m.SysTypeFor(os), SKU: m.SKU, Version: m.Version[os], Serial: Generate(m.Serial), MAC: Generate(m.MAC), Port: port, Password: password, Fields: map[string]string{}}
			for k, v := range m.Fields {
				if genToken.MatchString(v) {
					sp.Fields[k] = Generate(v)
				}
			}
			sp.Saved = DefaultConfig(pack.OS[os].PortStyle, name, netip.PrefixFrom(ip, pfx.Bits()).String(), pack.Layout(m))
			out = append(out, sp)
		}
	}
	return out, Cable(pack, out, have, haveLinks, cabling), nil
}

// Cable wires new switches with a preset (have = switches there already,
// whose free ports are used too; links = their cables).
func Cable(pack *Pack, add, have []Spec, links []Link, preset string) []Link {
	all := append(append([]Spec(nil), have...), add...)
	used := map[string]bool{}
	for _, l := range links {
		used[l.A+"/"+l.APort], used[l.B+"/"+l.BPort] = true, true
	}
	next := func(s Spec) string { // the next free uplink port
		m, _ := pack.ModelOf(s.SKU)
		st := pack.OS[s.OS].PortStyle
		for _, u := range pack.Uplinks(m) {
			p := portName(st, u)
			if !used[s.ID+"/"+p] {
				used[s.ID+"/"+p] = true
				return p
			}
		}
		for i := pack.PortCount(m); i >= 1; i-- { // then from the top down
			p := portName(st, i)
			if !used[s.ID+"/"+p] {
				used[s.ID+"/"+p] = true
				return p
			}
		}
		return ""
	}
	link := func(a, b Spec) *Link {
		pa, pb := next(a), next(b)
		if pa == "" || pb == "" {
			return nil
		}
		return &Link{A: a.ID, APort: pa, B: b.ID, BPort: pb}
	}
	var out []Link
	add1 := func(l *Link) {
		if l != nil {
			out = append(out, *l)
		}
	}
	switch preset {
	case CableChain, CableRing:
		seq := add
		if len(have) > 0 && len(add) > 0 { // added to a lab: they go on from its last switch
			seq = append([]Spec{have[len(have)-1]}, add...)
		}
		for i := 0; i+1 < len(seq); i++ {
			add1(link(seq[i], seq[i+1]))
		}
		if preset == CableRing && len(seq) > 2 {
			add1(link(seq[len(seq)-1], seq[0]))
		}
	case CableLeafSpine:
		var cores, leaves []Spec
		if len(all) > 0 { // the lab's first two switches are the cores
			cores = all[:min(2, len(all))]
		}
		isCore := map[string]bool{}
		for _, c := range cores {
			isCore[c.ID] = true
		}
		for _, s := range add {
			if !isCore[s.ID] {
				leaves = append(leaves, s)
			}
		}
		newCore := false
		for _, c := range cores {
			for _, a := range add {
				newCore = newCore || a.ID == c.ID
			}
		}
		if len(cores) == 2 && newCore { // the cores' inter-switch link
			add1(link(cores[0], cores[1]))
		}
		for _, l := range leaves {
			for _, c := range cores {
				// a core uses its access ports for leaves (1/1, 1/2…), uplinks for the ISC
				cp := ""
				m, _ := pack.ModelOf(c.SKU)
				for i := 1; i <= pack.PortCount(m); i++ {
					p := portName(pack.OS[c.OS].PortStyle, i)
					if !used[c.ID+"/"+p] {
						used[c.ID+"/"+p] = true
						cp = p
						break
					}
				}
				lp := next(l)
				if cp != "" && lp != "" {
					out = append(out, Link{A: c.ID, APort: cp, B: l.ID, BPort: lp})
				}
			}
		}
	}
	return out
}
