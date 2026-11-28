package app

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/sim"
)

// EmuDupRequest duplicates switches of the emulated lab (right-click → Duplicate in the topology).
type EmuDupRequest struct {
	IDs    []string `json:"ids"`    // the switches to copy; several are copied together, with the cables between them
	Count  int      `json:"count"`  // copies of each (1 when 0)
	Cables bool     `json:"cables"` // also cable each copy to the original's other neighbours (their next free ports)
	Start  bool     `json:"start"`  // start the copies
	// What a copy gets: "image" - a fresh switch from its image; "" or "software" - a fresh switch with the
	// software the original runs now (an image of that version, else its disk with the configuration set back
	// to factory default); "config" - its disk: its software and its configuration.
	Mode string `json:"mode,omitempty"`
}

// emuSameVersion: the version a switch reports is the image's (33.4.1.15 and "33.4.1.15 patch1-2" are the same).
func emuSameVersion(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	return a == "" || b == "" || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// emuDupFree: no switch is at (x, y) or close enough for the two to overlap on the canvas.
func emuDupFree(all []emu.VM, x, y int) bool {
	for _, v := range all {
		if v.X == 0 && v.Y == 0 {
			continue // not placed yet: drawn where the canvas puts it
		}
		if abs(v.X-x) < 150 && abs(v.Y-y) < 110 {
			return false
		}
	}
	return true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// emuDupSpot is where a copy of src goes, as the topology is laid out: one switch in the free places to the
// right of it in its row, then the rows below, as wide as the topology; a group as a block of the same shape
// beside it, then below it. dx, dy move every switch of src.
func emuDupSpot(all, src []emu.VM) (dx, dy int) {
	const cw, rh = 170, 160
	minX, minY, maxX, maxY := src[0].X, src[0].Y, src[0].X, src[0].Y
	for _, v := range src {
		minX, minY, maxX, maxY = min(minX, v.X), min(minY, v.Y), max(maxX, v.X), max(maxY, v.Y)
	}
	labMin, labMax := minX, maxX
	for _, v := range all {
		if v.X != 0 || v.Y != 0 {
			labMin, labMax = min(labMin, v.X), max(labMax, v.X)
		}
	}
	bw, bh := maxX-minX+cw, maxY-minY+rh
	width := max(labMax, labMin+6*cw, maxX) // a row as wide as the topology (at least seven switches)
	fits := func(ox, oy int) bool {
		for _, v := range src {
			if !emuDupFree(all, v.X+ox, v.Y+oy) {
				return false
			}
		}
		return true
	}
	for ox := bw; maxX+ox <= width; ox += cw { // beside it, in its row
		if fits(ox, 0) {
			return ox, 0
		}
	}
	for row := 1; row < 2000; row++ { // the rows below, from the left of the topology
		for x := labMin; x+(maxX-minX) <= width; x += cw {
			if fits(x-minX, row*bh) {
				return x - minX, row * bh
			}
		}
	}
	return bw, 0
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
	type dupJob struct {
		src  emu.VM
		dst  string
		wipe bool
	}
	var jobs []dupJob
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
			switch {
			case r.Mode == "config":
				jobs = append(jobs, dupJob{src: v, dst: c.ID})
			case r.Mode != "image" && v.FW != "" && !emuSameVersion(v.FW, im.Version): // upgraded on the switch
				if x := emuImageOfVersion(imgs, im, v.FW); x != "" {
					c.Image = x
				} else {
					c.Wipe = true
					jobs = append(jobs, dupJob{src: v, dst: c.ID, wipe: true})
				}
			}
			host := emuHostOf(v)
			lv := l.For(host, emuLab.f.Host)
			lv.VMs = append(lv.VMs, add...)
			c.MgmtIP, c.Gateway = lv.FreeAddress(c)
			of[v.ID] = c.ID
			add = append(add, c)
		}
		// this round's copies: placed as a block of the originals' shape where nothing is (as the topology is laid out)
		ox, oy := emuDupSpot(append(slices.Clone(l.VMs), add[:len(add)-len(src)]...), src)
		for i := len(add) - len(src); i < len(add); i++ {
			o := src[i-(len(add)-len(src))]
			add[i].X, add[i].Y = o.X+ox, o.Y+oy
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
	// copies of disks: each original's disk once for all its copies (a running one pauses for it)
	type diskCopy struct {
		host, src, mon string
		run            bool
		port           int
		dsts           []string
		slots          []string
		wipe           bool
	}
	var dcs []*diskCopy
	if len(jobs) > 0 {
		st := emuLab.status
		for _, j := range jobs {
			var d *diskCopy
			for _, x := range dcs {
				if x.src == j.src.ID {
					d = x
				}
			}
			if d == nil {
				host := emuHostOf(j.src)
				lv := a.emuFor(host)
				_, up := st.Up[j.src.ID]
				d = &diskCopy{host: host, src: j.src.ID, run: up, port: lv.MonitorPort(j.src), mon: lv.VMDir()}
				if im := imgs[j.src.Image]; im != nil {
					for _, x := range im.Disks {
						if strings.HasPrefix(x.Slot, "hd") {
							d.slots = append(d.slots, x.Slot)
						}
					}
				}
				dcs = append(dcs, d)
			}
			d.dsts = append(d.dsts, j.dst)
		}
	}
	emuLab.mu.Unlock()
	for _, d := range dcs {
		var b strings.Builder
		for _, dst := range d.dsts {
			fmt.Fprintf(&b, "cp --reflink=auto -f %s/%s.qcow2 %s/%s.qcow2 || echo 'fail %s';\n", emu.Q(d.mon), d.src, emu.Q(d.mon), dst, dst)
			for _, sl := range d.slots {
				fmt.Fprintf(&b, "cp --reflink=auto -f %s/%s-%s.qcow2 %s/%s-%s.qcow2 2>/dev/null;\n", emu.Q(d.mon), d.src, sl, emu.Q(d.mon), dst, sl)
			}
		}
		var h *emuHMP
		if d.run { // a consistent copy: the switch pauses for it (what it saved is what the copies get)
			if m, err := a.emuMonitor(d.host, d.port); err == nil {
				h = m
				_, _ = h.do("stop", 5*time.Second)
			}
		}
		out, err := a.emuRun(d.host, b.String(), 10*time.Minute)
		if h != nil {
			_, _ = h.do("cont", 5*time.Second)
			h.close()
		}
		emuLab.mu.Lock()
		for _, dst := range d.dsts {
			if err != nil || strings.Contains(out, "fail "+dst) {
				emuEv(dst, "error", "its disk could not be copied from "+emuNameOf(d.src)+" - it starts from its image instead: "+strings.TrimSpace(out+" "+errText(err)))
				if c := l.VMByID(dst); c != nil {
					c.Wipe = false
				}
				continue
			}
			what := "with its configuration and software"
			if c := l.VMByID(dst); c != nil && c.Wipe {
				what = "for its software (its configuration is set back to factory default on its first boot)"
			}
			emuEv(dst, "info", "copied the disk of "+emuNameOf(d.src)+" "+what)
		}
		_ = a.emuSave()
		emuLab.mu.Unlock()
	}
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

// emuImageOfVersion is an image of the same device type as im in the given software version ("" = none).
func emuImageOfVersion(imgs map[string]*emu.Image, im *emu.Image, ver string) string {
	for id, x := range imgs {
		if x != im && x.Version != "" && strings.EqualFold(x.DeviceType, im.DeviceType) && emuSameVersion(ver, x.Version) && (x.Persona != nil) == (im.Persona != nil) {
			return id
		}
	}
	return ""
}

// emuNameOf is a switch's name (mu held).
func emuNameOf(id string) string {
	if v := emuLab.f.Lab.VMByID(id); v != nil {
		return v.Name
	}
	return id
}

// emuWipe sets a copied disk's configuration back to factory default (the device type's commands, e.g.
// "unconfigure switch all") on its first boot, then boots it again for its own day 0.
func (a *App) emuWipe(host, id string) {
	emuLab.mu.Lock()
	v := emuLab.f.Lab.VMByID(id)
	if v == nil {
		emuLab.mu.Unlock()
		return
	}
	v.Wipe = false
	vv, im := *v, emuImage(v.Image)
	_ = a.emuSave()
	emuLab.mu.Unlock()
	if im == nil {
		return
	}
	dt, _ := a.deviceTypeNamed(im.DeviceType)
	cmds := dt.Factory()
	if len(cmds) == 0 {
		emuLab.mu.Lock()
		emuEv(id, "warn", "the device type "+orStr(im.DeviceType, "of its image")+" has no factory-default commands (Global Options → Device Types): the copy keeps the configuration of the switch it was copied from")
		emuLab.mu.Unlock()
		return
	}
	emuLab.mu.Lock()
	emuLab.state[id] = "factory default"
	emuEv(id, "info", "setting the copied configuration back to factory default: "+strings.Join(cmds, " · "))
	emuLab.mu.Unlock()
	_, err := a.emuOnConsole(host, vv, *im, true, cmds...)
	time.Sleep(45 * time.Second) // it erases and restarts by itself; then a clean start for its day 0
	a.emuPowerOff(host, id, 90*time.Second)
	emuLab.mu.Lock()
	if c := emuLab.f.Lab.VMByID(id); c != nil {
		c.Day0Done = false
	}
	delete(emuLab.state, id)
	if err != nil {
		emuEv(id, "info", "the console went quiet after the factory-default commands (it restarts): "+err.Error())
	}
	_ = a.emuSave()
	emuLab.mu.Unlock()
	a.emuStart(host, []string{id})
}
