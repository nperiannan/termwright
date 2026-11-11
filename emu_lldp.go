package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/termwright/termwright/internal/emu"
)

// The cabling check: the switches' LLDP neighbours against the cables of the topology.

func emuCableKey(c emu.Cable) string {
	return c.A + "/" + strconv.Itoa(c.APort) + "-" + c.B + "/" + strconv.Itoa(c.BPort)
}

// emuNeighbors reads "show lldp neighbors"-like output: the first word of a
// line is the local port, the rest names the neighbour. It returns, per local
// port, the rest of its line(s).
func emuNeighbors(out string, ports map[string]bool) map[string]string {
	n := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		p := f[0]
		if !ports[p] {
			continue
		}
		n[p] += " " + strings.ToLower(strings.Join(f[1:], " ")) + " "
	}
	return n
}

// emuJudge says what an LLDP line (lower case) shows about a cable to the
// switch peer with port peerPort; names are all the lab's switch names.
func emuJudge(seen, peer, peerPort string, names []string) string {
	has := func(w string) bool {
		w = strings.ToLower(w)
		return strings.Contains(seen, " "+w+" ") || strings.Contains(seen, " "+w+"\t")
	}
	if seen == "" {
		return "none: no LLDP neighbour on that port (link down, or LLDP off)"
	}
	for _, n := range names {
		if has(n) && !strings.EqualFold(n, peer) {
			return "wrong: the port sees " + n + " instead of " + peer
		}
	}
	switch {
	case has(peer) && has(peerPort):
		return "ok: LLDP sees " + peer + " " + peerPort
	case has(peer):
		return "ok: LLDP sees " + peer
	case has(peerPort):
		return "ok: LLDP sees port " + peerPort + " (the switches do not send their lab names)"
	}
	return "wrong: the port sees another device (" + strings.TrimSpace(seen) + ")"
}

// EmuCheckCabling asks the running switches for their LLDP neighbours and
// compares them with the cables: each cable shows ok, wrong or none.
func (a *App) EmuCheckCabling() (EmuView, error) {
	st := a.emuPollFresh()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	var names []string
	for _, v := range l.VMs {
		names = append(names, v.Name)
	}
	type job struct {
		v  emu.VM
		im emu.Image
	}
	var jobs []job
	for _, v := range l.VMs {
		if _, up := st.Up[v.ID]; !up {
			continue
		}
		if im := emuImage(v.Image); im != nil && im.NeighborCmd != "" {
			jobs = append(jobs, job{v, *im})
		}
	}
	cables := append([]emu.Cable(nil), l.Cables...)
	emuLab.mu.Unlock()
	if len(jobs) == 0 {
		return EmuView{}, errors.New("no running switch has an LLDP command in its device profile")
	}
	seen := map[string]map[string]string{} // switch → port → its LLDP line
	for _, j := range jobs {
		ports := map[string]bool{}
		for n := 1; n < max(j.im.NICs, 2); n++ {
			ports[j.im.PortNameOf(n)] = true
		}
		out, err := a.emuOnConsole(a.emuHostOfID(j.v.ID), j.v, j.im, false, j.im.NeighborCmd)
		emuLab.mu.Lock()
		if err != nil {
			emuEv(j.v.ID, "warn", "cabling not checked: "+err.Error())
		} else {
			seen[j.v.ID] = emuNeighbors(out, ports)
		}
		emuLab.mu.Unlock()
	}
	emuLab.mu.Lock()
	ok, bad := 0, 0
	for _, c := range cables {
		va, vb := l.VMByID(c.A), l.VMByID(c.B)
		if va == nil || vb == nil {
			continue
		}
		ia, ib := emuImage(va.Image), emuImage(vb.Image)
		if ia == nil || ib == nil {
			continue
		}
		pa, pb := ia.PortNameOf(c.APort), ib.PortNameOf(c.BPort)
		var res string
		switch {
		case c.Down:
			res = "none: the cable is pulled"
		case seen[c.A] != nil:
			res = emuJudge(seen[c.A][pa], vb.Name, pb, names)
		case seen[c.B] != nil:
			res = emuJudge(seen[c.B][pb], va.Name, pa, names)
		default:
			delete(emuLab.lldp, emuCableKey(c))
			continue
		}
		emuLab.lldp[emuCableKey(c)] = res
		if strings.HasPrefix(res, "ok") {
			ok++
		} else {
			bad++
			emuEv(c.A, "warn", fmt.Sprintf("cable %s %s ⇄ %s %s: %s", va.Name, pa, vb.Name, pb, res))
		}
	}
	if len(jobs) > 0 {
		emuEv(jobs[0].v.ID, "info", fmt.Sprintf("cabling checked with LLDP: %d cables as drawn, %d not", ok, bad))
	}
	emuLab.mu.Unlock()
	return a.EmuState()
}
