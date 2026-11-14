package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// Hubs and L2 switches: lab nodes without a VM (emu.Hub). Their legs are switch ports without a cable; hub.py
// on the host joins them (a hub repeats every frame, an L2 switch learns MAC addresses). They work at once on
// running switches, like hot-plugged cables.

// EmuHubLegView is a leg as the canvas shows it.
type EmuHubLegView struct {
	VM       string `json:"vm"`
	Port     int    `json:"port"`
	Name     string `json:"name"`     // the switch's name
	PortName string `json:"portName"` // as the switch calls the port
}

// EmuHubView is a hub or an L2 switch for the canvas.
type EmuHubView struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Kind string          `json:"kind"` // hub, switch
	X    float64         `json:"x"`
	Y    float64         `json:"y"`
	Legs []EmuHubLegView `json:"legs"`
}

// emuHubViews (mu held).
func emuHubViews(l *emu.Lab) []EmuHubView {
	out := []EmuHubView{}
	for _, h := range l.Hubs {
		v := EmuHubView{ID: h.ID, Name: h.Name, Kind: h.Kind, X: h.X, Y: h.Y, Legs: []EmuHubLegView{}}
		for _, g := range h.Legs {
			lv := EmuHubLegView{VM: g.VM, Port: g.Port}
			if vm := l.VMByID(g.VM); vm != nil {
				lv.Name = vm.Name
				if im := emuImage(vm.Image); im != nil {
					lv.PortName = im.PortNameOf(g.Port)
				}
			}
			v.Legs = append(v.Legs, lv)
		}
		out = append(out, v)
	}
	return out
}

// EmuHubAdd puts a hub (kind hub) or an L2 switch (kind switch) on the canvas.
func (a *App) EmuHubAdd(kind, name string, x, y float64) (EmuView, error) {
	defer emuAs("user")()
	if kind != "switch" {
		kind = "hub"
	}
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	if name = strings.TrimSpace(name); name == "" {
		base := map[bool]string{true: "l2sw", false: "hub"}[kind == "switch"]
		for n := 1; ; n++ {
			if name = fmt.Sprintf("%s-%d", base, n); l.HubByID(name) == nil && l.VMByName(name) == nil {
				break
			}
		}
	} else if l.HubByID(name) != nil || l.VMByName(name) != nil {
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("%s is taken", name)
	}
	l.Hubs = append(l.Hubs, emu.Hub{ID: "h" + randHex(3), Name: name, Kind: kind, X: x, Y: y})
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuHubSet renames or moves a hub.
func (a *App) EmuHubSet(id, name string, x, y float64) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var h *emu.Hub
	if l != nil {
		h = l.HubByID(id)
	}
	if h == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such hub")
	}
	if name = strings.TrimSpace(name); name != "" && !strings.EqualFold(name, h.Name) {
		if l.HubByID(name) != nil || l.VMByName(name) != nil {
			emuLab.mu.Unlock()
			return EmuView{}, fmt.Errorf("%s is taken", name)
		}
		h.Name = name
	}
	h.X, h.Y = x, y
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuHubConnect puts a switch's free port on a hub (port: its name or number).
func (a *App) EmuHubConnect(id, vmID, port string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	var h *emu.Hub
	var v *emu.VM
	if l != nil {
		h, v = l.HubByID(id), l.VMByID(vmID)
		if v == nil {
			v = l.VMByName(vmID)
		}
	}
	if h == nil || v == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("choose the hub and the switch")
	}
	im := emuImage(v.Image)
	n := emuPort(im, port)
	if n == 0 || !slices.Contains(a.emuFreePortsLocked(v.ID), im.PortNameOf(n)) {
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("%s %s is not free (a cable, a test port, a hub or the host's network)", v.Name, port)
	}
	if len(h.Legs) >= emu.HubMaxLegs {
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("%s has all its %d ports in use", h.Name, emu.HubMaxLegs)
	}
	for _, g := range h.Legs {
		if o := l.VMByID(g.VM); o != nil && emuHostOf(*o) != emuHostOf(*v) {
			emuLab.mu.Unlock()
			return EmuView{}, fmt.Errorf("%s's switches run on %s: a hub joins switches of one host", h.Name, emuHostOf(*o)[strings.LastIndex(emuHostOf(*o), "/")+1:])
		}
	}
	h.Legs = append(h.Legs, emu.HubLeg{VM: v.ID, Port: n})
	emuEv(v.ID, "info", fmt.Sprintf("port %s on %s", im.PortNameOf(n), h.Name))
	err := a.emuSave()
	vid := v.ID
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	a.emuHubSync()
	if note := a.emuTesterLink(vid, n, true); note != "" {
		emuLab.mu.Lock()
		emuEv(vid, "warn", strings.Replace(note, "for its test port", "for its hub port", 1))
		emuLab.mu.Unlock()
	}
	return a.EmuState()
}

// EmuHubDisconnect takes a switch's port off a hub.
func (a *App) EmuHubDisconnect(id, vmID string, port int) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var h *emu.Hub
	if l != nil {
		h = l.HubByID(id)
	}
	if h == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such hub")
	}
	h.Legs = slices.DeleteFunc(h.Legs, func(g emu.HubLeg) bool { return g.VM == vmID && g.Port == port })
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	a.emuHubSync()
	a.emuTesterLink(vmID, port, false)
	return a.EmuState()
}

// EmuHubRemove removes a hub and takes its ports off it.
func (a *App) EmuHubRemove(id string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var legs []emu.HubLeg
	if l != nil {
		if h := l.HubByID(id); h != nil {
			legs = h.Legs
			l.Hubs = slices.DeleteFunc(l.Hubs, func(x emu.Hub) bool { return x.ID == h.ID })
		}
	}
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	a.emuHubSync()
	for _, g := range legs {
		a.emuTesterLink(g.VM, g.Port, false)
	}
	return a.EmuState()
}

// emuHubSync writes each host's hubs (the legs of its switches) and starts or ends hub.py there.
func (a *App) emuHubSync() {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return
	}
	type hcfg struct {
		ID   string   `json:"id"`
		Kind string   `json:"kind"`
		Legs [][2]int `json:"legs"`
	}
	per := map[string][]hcfg{}
	for _, h := range l.Hubs {
		host := ""
		c := hcfg{ID: h.ID, Kind: h.Kind, Legs: [][2]int{}}
		for _, g := range h.Legs {
			v := l.VMByID(g.VM)
			if v == nil {
				continue
			}
			host = emuHostOf(*v)
			li, se := l.TesterPorts(*v, g.Port)
			c.Legs = append(c.Legs, [2]int{li, se})
		}
		if host != "" && len(c.Legs) > 1 {
			per[host] = append(per[host], c)
		}
	}
	if emuLab.hubHosts == nil {
		emuLab.hubHosts = map[string]bool{}
	}
	type job struct{ host, script string }
	var jobs []job
	for _, h := range l.Hosts(emuLab.f.Host) {
		if len(per[h]) == 0 && !emuLab.hubHosts[h] {
			continue
		}
		emuLab.hubHosts[h] = len(per[h]) > 0
		b, _ := json.Marshal(map[string]any{"hubs": per[h]})
		jobs = append(jobs, job{h, a.emuFor(h).HubScript(b, len(per[h]) > 0)})
	}
	emuLab.mu.Unlock()
	for _, j := range jobs {
		if out, err := a.emuRun(j.host, j.script, 30*time.Second); err != nil || !strings.Contains(out, "hub-ok") {
			emuLab.mu.Lock()
			emuEv("", "warn", "the hubs did not start on "+j.host[strings.LastIndex(j.host, "/")+1:]+": "+strings.TrimSpace(out+" "+errText(err)))
			emuLab.mu.Unlock()
		}
	}
}
