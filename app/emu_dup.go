package app

import (
	"errors"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/sim"
)

// EmuDupRequest duplicates switches of the emulated lab (right-click → Duplicate in the topology).
type EmuDupRequest struct {
	IDs    []string `json:"ids"`    // the switches to copy; several are copied together, with the cables between them
	Count  int      `json:"count"`  // copies of each (1 when 0)
	Cables bool     `json:"cables"` // also cable each copy to the original's other neighbours (their next free ports)
	Start  bool     `json:"start"`  // start the copies
}

var dupNumRe = regexp.MustCompile(`^(.*?)(\d+)$`)

// emuNextName is the next free name like name: 5520-24t-1 → 5520-24t-2 (the highest number in use + 1), core → core-2.
func emuNextName(name string, used map[string]bool) string {
	base, width := name+"-", 0
	if m := dupNumRe.FindStringSubmatch(name); m != nil {
		base, width = m[1], len(m[2])
	}
	hi := 1
	for n := range used {
		if strings.HasPrefix(n, base) {
			if k, err := strconv.Atoi(n[len(base):]); err == nil && k > hi {
				hi = k
			}
		}
	}
	for k := hi + 1; ; k++ {
		s := strconv.Itoa(k)
		if width > 1 && len(s) < width { // keep leading zeros: sw01 → sw02
			s = strings.Repeat("0", width-len(s)) + s
		}
		if !used[base+s] {
			return base + s
		}
	}
}

// emuNextSerial is the next serial number after s that no switch (or stack member) has.
func emuNextSerial(s string, used map[string]bool) string {
	if s == "" {
		return ""
	}
	for k := 1; k < 1_000_000; k++ {
		if n := emu.NextSerial(s, k); !used[n] {
			return n
		}
	}
	return ""
}

// emuNextMAC is the next block of 64 addresses after mac that no switch's block overlaps.
func emuNextMAC(mac string, taken []uint64) string {
	for k := 1; k < 100_000; k++ {
		n := emu.NextMAC(mac, k)
		x, ok := macNum(n)
		if !ok {
			return ""
		}
		if !slices.ContainsFunc(taken, func(t uint64) bool { return x < t+64 && t < x+64 }) {
			return n
		}
	}
	return ""
}

func macNum(s string) (uint64, bool) {
	hw, err := net.ParseMAC(s)
	if err != nil || len(hw) != 6 {
		return 0, false
	}
	return uint64(hw[0])<<40 | uint64(hw[1])<<32 | uint64(hw[2])<<24 | uint64(hw[3])<<16 | uint64(hw[4])<<8 | uint64(hw[5]), true
}

// emuDupVM is a copy of v as a new switch: the same image, model, modules and resources; a new id, index,
// serial numbers and base MAC; no configuration, saved state, cables or addresses of its own.
func emuDupVM(v emu.VM, name string, index int, im *emu.Image, serials map[string]bool, macs *[]uint64) emu.VM {
	c := emu.VM{ID: "v" + randHex(3), Name: name, Image: v.Image, Index: index, CPUs: v.CPUs, RAM: v.RAM, Host: v.Host,
		DiskGB: v.DiskGB, Template: v.Template, Role: v.Role, CPUMax: v.CPUMax, CPUWeight: v.CPUWeight, CPUReq: v.CPUReq, CPULim: v.CPULim,
		Desktop: v.Desktop, DNS: v.DNS}
	switch {
	case v.Serial != "":
		c.Serial = emuNextSerial(v.Serial, serials)
	case im != nil && im.Serial != "":
		c.Serial = sim.Generate(im.Serial)
	}
	if c.Serial != "" {
		serials[c.Serial] = true
	}
	if v.BaseMAC != "" {
		if c.BaseMAC = emuNextMAC(v.BaseMAC, *macs); c.BaseMAC != "" {
			x, _ := macNum(c.BaseMAC)
			*macs = append(*macs, x)
		}
	}
	if p := v.Persona; p != nil {
		np := &emu.Persona{Values: map[string]string{}}
		for k, x := range p.Values {
			np.Values[k] = x
		}
		for _, s := range p.Slots {
			s.Ports = slices.Clone(s.Ports)
			switch {
			case s.Serial == v.Serial && c.Serial != "":
				s.Serial = c.Serial
			case s.Serial != "": // another stack member: the next free serial after its own
				s.Serial = emuNextSerial(s.Serial, serials)
				serials[s.Serial] = true
			}
			np.Slots = append(np.Slots, s)
		}
		c.Persona = np
	}
	if v.Routing != nil {
		r := *v.Routing
		c.Routing = &r
	}
	if v.Server != nil {
		s := *v.Server
		c.Server = &s
	}
	return c
}

// EmuDuplicate adds copies of switches next to them: the next free names (5520-24t-1 → 5520-24t-2),
// serial numbers and MACs counting on so none repeats, the same image, model and resources on the same
// host. Several switches are copied together with the cables between them; the copies boot from day 0.
func (a *App) EmuDuplicate(r EmuDupRequest) (EmuView, error) {
	defer emuAs("user")()
	if err := a.ready(); err != nil {
		return EmuView{}, err
	}
	if r.Count <= 0 {
		r.Count = 1
	}
	if r.Count > 500 {
		return EmuView{}, errors.New("at most 500 copies at a time")
	}
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil || len(r.IDs) == 0 {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no switches chosen")
	}
	var src []emu.VM
	for _, id := range r.IDs {
		if v := l.VMByID(id); v != nil && !slices.ContainsFunc(src, func(x emu.VM) bool { return x.ID == id }) {
			src = append(src, *v)
		}
	}
	if len(src) == 0 {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("those switches are not in the lab")
	}
	imgs := emuImages()
	names, serials, next := map[string]bool{}, map[string]bool{}, 0
	var macs []uint64
	for _, v := range l.VMs {
		names[v.Name] = true
		if v.Serial != "" {
			serials[v.Serial] = true
		}
		if v.HWSerial != "" {
			serials[v.HWSerial] = true
		}
		if v.Persona != nil {
			for _, s := range v.Persona.Slots {
				if s.Serial != "" {
					serials[s.Serial] = true
				}
			}
		}
		if x, ok := macNum(v.BaseMAC); ok {
			macs = append(macs, x)
		}
		if v.Index >= next {
			next = v.Index + 1
		}
	}
	// the selection's box: each round of copies goes below it
	minX, minY, maxY := src[0].X, src[0].Y, src[0].Y
	for _, v := range src {
		minX, minY, maxY = min(minX, v.X), min(minY, v.Y), max(maxY, v.Y)
	}
	dy := maxY - minY + 160
	inSrc := map[string]bool{}
	for _, v := range src {
		inSrc[v.ID] = true
	}
	var add []emu.VM
	var cables []emu.Cable
	for round := 1; round <= r.Count; round++ {
		of := map[string]string{} // original → its copy in this round
		for _, v := range src {
			im := imgs[v.Image]
			if im == nil {
				emuLab.mu.Unlock()
				return EmuView{}, errors.New(v.Name + ": its image is no longer in the lab")
			}
			name := emuNextName(v.Name, names)
			names[name] = true
			c := emuDupVM(v, name, next, im, serials, &macs)
			next++
			host := emuHostOf(v)
			lv := l.For(host, emuLab.f.Host)
			lv.VMs = append(lv.VMs, add...)
			c.MgmtIP, c.Gateway = lv.FreeAddress(c)
			c.X, c.Y = v.X, v.Y+dy*round
			if len(src) == 1 { // one switch: the copies in a row beside it
				c.X, c.Y = v.X+170*round, v.Y+70
			}
			of[v.ID] = c.ID
			add = append(add, c)
		}
		for _, k := range l.Cables {
			switch {
			case inSrc[k.A] && inSrc[k.B]: // inside the selection: the same cable between the copies
				cables = append(cables, emu.Cable{A: of[k.A], APort: k.APort, B: of[k.B], BPort: k.BPort})
			case r.Cables && (inSrc[k.A] || inSrc[k.B]):
				mine, port, peer := k.A, k.APort, k.B
				if !inSrc[k.A] {
					mine, port, peer = k.B, k.BPort, k.A
				}
				pv := l.VMByID(peer)
				if pv == nil {
					continue // a hub, a cloud: drawn by the user
				}
				tmp := *l
				tmp.Cables = append(slices.Clone(l.Cables), cables...)
				fp := tmp.FreeDataPort(*pv, imgs[pv.Image])
				if fp == 0 {
					continue
				}
				cables = append(cables, emu.Cable{A: of[mine], APort: port, B: peer, BPort: fp})
			}
		}
	}
	l.VMs = append(l.VMs, add...)
	l.Cables = append(l.Cables, cables...)
	if err := a.emuSave(); err != nil {
		emuLab.mu.Unlock()
		return EmuView{}, err
	}
	emuLab.mu.Unlock()
	for _, v := range add {
		a.emuSessions(v)
	}
	emuLab.mu.Lock()
	_ = a.emuSave()
	emuLab.mu.Unlock()
	if r.Start {
		ids := make([]string, len(add))
		for i, v := range add {
			ids[i] = v.ID
		}
		return a.EmuStart(ids)
	}
	return a.EmuState()
}
