package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// Hot-plugged cables: a cable added between two running switches on one host works at once. A port that had
// no cable when its switch started sends to the UDP port a test port would use; the host's patch panel
// (patch.py) joins two such ports until the switches restart with the cable. A cable removed from running
// switches is pulled at once (its links go down).

func emuPeerKey(vm string, port int) string { return vm + ":" + strconv.Itoa(port) }

// emuCablesOf: a switch's cables, port → the other end.
func emuCablesOf(l *emu.Lab, id string) map[int]string {
	m := map[int]string{}
	for _, c := range l.Cables {
		if c.A == id {
			m[c.APort] = emuPeerKey(c.B, c.BPort)
		}
		if c.B == id {
			m[c.BPort] = emuPeerKey(c.A, c.APort)
		}
	}
	return m
}

// emuCablesSeen notes, for running switches Termwright did not start itself (it ran before), the cables they
// have now as the ones they started with (mu held; up: the running ones).
func emuCablesSeen(l *emu.Lab, up map[string]float64) {
	if emuLab.runCables == nil {
		emuLab.runCables = map[string]map[int]string{}
	}
	for id := range up {
		if _, ok := emuLab.runCables[id]; !ok && l.VMByID(id) != nil {
			emuLab.runCables[id] = emuCablesOf(l, id)
		}
	}
}

// emuPatchPairs: per host, the switch ports the patch panel joins (mu held): the cables between running
// switches of one host that at least one end started without. Each end is [where its QEMU sends, where it listens].
func emuPatchPairs(l *emu.Lab, up map[string]float64) map[string][][4]int {
	out := map[string][][4]int{}
	for _, c := range l.Cables {
		_, ua := up[c.A]
		_, ub := up[c.B]
		va, vb := l.VMByID(c.A), l.VMByID(c.B)
		if !ua || !ub || va == nil || vb == nil || emuHostOf(*va) != emuHostOf(*vb) {
			continue
		}
		na := emuLab.runCables[c.A][c.APort] == emuPeerKey(c.B, c.BPort)
		nb := emuLab.runCables[c.B][c.BPort] == emuPeerKey(c.A, c.APort)
		if na && nb {
			continue // both started with it: a cable of their own
		}
		end := func(v *emu.VM, n int, native bool, o *emu.VM, on int) (int, int) {
			if native {
				return l.UDPPort(*o, on), l.UDPPort(*v, n)
			}
			return l.TesterPorts(*v, n)
		}
		al, as := end(va, c.APort, na, vb, c.BPort)
		bl, bs := end(vb, c.BPort, nb, va, c.APort)
		h := emuHostOf(*va)
		out[h] = append(out[h], [4]int{al, as, bl, bs})
	}
	return out
}

// emuPatchSync makes the hosts' patch panels match the lab's cables and the running switches.
func (a *App) emuPatchSync(up map[string]float64) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return
	}
	pairs := emuPatchPairs(l, up)
	type job struct{ host, script string }
	var jobs []job
	if emuLab.patchHosts == nil {
		emuLab.patchHosts = map[string]bool{}
	}
	for _, h := range l.Hosts(emuLab.f.Host) {
		if len(pairs[h]) == 0 && !emuLab.patchHosts[h] {
			continue
		}
		emuLab.patchHosts[h] = len(pairs[h]) > 0
		jobs = append(jobs, job{h, a.emuFor(h).PatchScript(pairs[h])})
	}
	emuLab.mu.Unlock()
	for _, j := range jobs {
		if out, err := a.emuRun(j.host, j.script, 30*time.Second); err != nil || !strings.Contains(out, "patch-ok") {
			emuLab.mu.Lock()
			emuEv("", "warn", "the patch panel for cables added to running switches did not start on "+j.host[strings.LastIndex(j.host, "/")+1:]+": "+strings.TrimSpace(out+" "+errText(err)))
			emuLab.mu.Unlock()
		}
	}
}

// emuCableLinks turns both ends of a cable between running switches on or off (QEMU monitor), at once.
func (a *App) emuCableLinks(c emu.Cable, on bool, up map[string]float64) {
	for _, e := range []struct {
		id string
		n  int
	}{{c.A, c.APort}, {c.B, c.BPort}} {
		if _, ok := up[e.id]; !ok {
			continue
		}
		emuLab.mu.Lock()
		v := emuLab.f.Lab.VMByID(e.id)
		if v == nil {
			emuLab.mu.Unlock()
			continue
		}
		host := emuHostOf(*v)
		mon := a.emuFor(host).MonitorPort(*v)
		emuLab.mu.Unlock()
		h, err := a.emuMonitor(host, mon)
		if err != nil {
			continue
		}
		_, _ = h.do(fmt.Sprintf("set_link n%d %s", e.n, map[bool]string{true: "on", false: "off"}[on]), 10*time.Second)
		h.close()
	}
}

// emuHotCable puts a cable added (on) or removed between running switches into effect at once; it says
// what still needs a restart ("" = nothing).
func (a *App) emuHotCable(c emu.Cable, on bool, up map[string]float64) string {
	_, ua := up[c.A]
	_, ub := up[c.B]
	if !ua && !ub {
		return ""
	}
	emuLab.mu.Lock()
	va, vb := emuLab.f.Lab.VMByID(c.A), emuLab.f.Lab.VMByID(c.B)
	cross := va != nil && vb != nil && emuHostOf(*va) != emuHostOf(*vb)
	names := ""
	if va != nil && vb != nil {
		names = va.Name + " and " + vb.Name
	}
	emuLab.mu.Unlock()
	if on && cross && ua && ub {
		return "the switches are on two hosts: restart " + names + " for the cable to work"
	}
	a.emuPatchSync(up)
	if ua && ub || !on {
		a.emuCableLinks(c, on, up)
	}
	return ""
}
