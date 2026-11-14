package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/session"
)

// Hubs and L2 switches: lab nodes without a VM (emu.Hub). Their legs are switch ports without a cable; hub.py
// on the host joins them (a hub repeats every frame, an L2 switch learns MAC addresses). They work at once on
// running switches, like hot-plugged cables.

// EmuHubLegView is a leg as the canvas shows it.
type EmuHubLegView struct {
	VM       string `json:"vm"`
	Port     int    `json:"port"`
	At       int    `json:"at"`       // the hub's own port it is on
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
	// Ports is how many ports it has (hub 8; L2 switch 8-24).
	Ports int `json:"ports"`
	// Cfg is an L2 switch's VLANs and management address.
	Cfg *emu.HubCfg `json:"cfg,omitempty"`
	// Console is its command line's console session (L2 switch).
	Console string `json:"console,omitempty"`
	// SSH is its command line's SSH session (L2 switch; works when the host's Python has paramiko).
	SSH string `json:"ssh,omitempty"`
	// ConPort is its console's TCP port on its host's 127.0.0.1 (L2 switch).
	ConPort int `json:"conPort,omitempty"`
}

// emuHubViews (mu held).
func emuHubViews(l *emu.Lab) []EmuHubView {
	out := []EmuHubView{}
	for _, h := range l.Hubs {
		v := EmuHubView{ID: h.ID, Name: h.Name, Kind: h.Kind, X: h.X, Y: h.Y, Legs: []EmuHubLegView{}, Ports: h.HubPortsOf(), Cfg: h.Cfg}
		if h.Kind == "switch" {
			v.Console, v.SSH, v.ConPort = emuLab.f.Consoles["hub:"+h.ID], emuLab.f.Sessions["hub:"+h.ID], l.HubConsolePort(h)
		}
		for _, g := range h.Legs {
			lv := EmuHubLegView{VM: g.VM, Port: g.Port, At: g.At}
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
	n := 1
	for _, o := range l.Hubs {
		n = max(n, o.N+1)
	}
	l.Hubs = append(l.Hubs, emu.Hub{ID: "h" + randHex(3), Name: name, Kind: kind, Ports: map[bool]int{true: emu.HubSwitchMin, false: emu.HubMaxLegs}[kind == "switch"], N: n, X: x, Y: y})
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	if kind == "switch" { // its command line runs from now on
		emuGo(a.emuHubSync)
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
	emuHubFixAt(h)
	at := h.FreeAt()
	if at == 0 {
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("%s has all its %d ports in use", h.Name, h.HubPortsOf())
	}
	for _, g := range h.Legs {
		if o := l.VMByID(g.VM); o != nil && emuHostOf(*o) != emuHostOf(*v) {
			emuLab.mu.Unlock()
			return EmuView{}, fmt.Errorf("%s's switches run on %s: a hub joins switches of one host", h.Name, emuHostOf(*o)[strings.LastIndex(emuHostOf(*o), "/")+1:])
		}
	}
	h.Legs = append(h.Legs, emu.HubLeg{VM: v.ID, Port: n, At: at})
	emuEv(v.ID, "info", fmt.Sprintf("port %s on %s port %d", im.PortNameOf(n), h.Name, at))
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

// emuHubFixAt gives legs from before hub port numbers their port (in order).
func emuHubFixAt(h *emu.Hub) {
	for i := range h.Legs {
		if h.Legs[i].At == 0 {
			h.Legs[i].At = h.FreeAt()
		}
	}
}

// EmuHubSetPorts changes how many ports an L2 switch has (8-24).
func (a *App) EmuHubSetPorts(id string, n int) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var h *emu.Hub
	if l != nil {
		h = l.HubByID(id)
	}
	switch {
	case h == nil:
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such hub")
	case h.Kind != "switch":
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("a hub has %d ports", emu.HubMaxLegs)
	case n < emu.HubSwitchMin || n > emu.HubSwitchMax:
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("an L2 switch has %d to %d ports", emu.HubSwitchMin, emu.HubSwitchMax)
	}
	emuHubFixAt(h)
	for _, g := range h.Legs {
		if g.At > n {
			emuLab.mu.Unlock()
			return EmuView{}, fmt.Errorf("its port %d is in use: take it off first", g.At)
		}
	}
	h.Ports = n
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	a.emuHubSync()
	return a.EmuState()
}

// EmuHubPortSet sets an L2 switch port: access (vlan) or trunk (vlans tagged, vlan native, 0 = none).
func (a *App) EmuHubPortSet(id string, port int, mode string, vlan int, vlans []int) (EmuView, error) {
	return a.emuHubCfg(id, func(h *emu.Hub, c *emu.HubCfg) error {
		if port < 1 || port > h.HubPortsOf() {
			return fmt.Errorf("%s has ports 1-%d", h.Name, h.HubPortsOf())
		}
		if mode != "access" && mode != "trunk" {
			return errors.New("a port is access or trunk")
		}
		for _, x := range append([]int{vlan}, vlans...) {
			if x < 0 || x > 4094 {
				return fmt.Errorf("VLAN %d is not 1-4094", x)
			}
		}
		if mode == "access" && vlan == 0 {
			vlan = 1
		}
		if mode == "access" {
			vlans = nil
		}
		slices.Sort(vlans)
		c.Ports[strconv.Itoa(port)] = emu.HubPortCfg{Mode: mode, VLAN: vlan, VLANs: slices.Compact(vlans)}
		return nil
	})
}

// EmuHubSetMgmt sets an L2 switch's in-band management address (with its prefix; "" = none) and its VLAN.
func (a *App) EmuHubSetMgmt(id, ip string, vlan int) (EmuView, error) {
	return a.emuHubCfg(id, func(h *emu.Hub, c *emu.HubCfg) error {
		ip = strings.TrimSpace(ip)
		if ip != "" {
			if !strings.Contains(ip, "/") {
				ip += "/24"
			}
			if pfx, err := netip.ParsePrefix(ip); err != nil || !pfx.Addr().Is4() {
				return fmt.Errorf("%s is not an IPv4 address with its prefix", ip)
			}
		}
		if vlan < 1 || vlan > 4094 {
			vlan = 1
		}
		c.MgmtIP, c.MgmtVLAN = ip, vlan
		return nil
	})
}

func (a *App) emuHubCfg(id string, f func(*emu.Hub, *emu.HubCfg) error) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var h *emu.Hub
	if l != nil {
		h = l.HubByID(id)
	}
	if h == nil || h.Kind != "switch" {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such L2 switch")
	}
	c := emu.HubCfg{Ports: map[string]emu.HubPortCfg{}, MgmtVLAN: 1}
	if h.Cfg != nil {
		c = *h.Cfg
		c.Ports = maps.Clone(h.Cfg.Ports)
		if c.Ports == nil {
			c.Ports = map[string]emu.HubPortCfg{}
		}
	}
	if err := f(h, &c); err != nil {
		emuLab.mu.Unlock()
		return EmuView{}, err
	}
	c.Rev++
	h.Cfg = &c
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	a.emuHubSync()
	return a.EmuState()
}

// EmuHubDetail is what an L2 switch or hub does now: frames per port, the MAC table and its configuration
// as it runs (changes typed on its command line come back into the lab here).
type EmuHubDetail struct {
	ID      string      `json:"id"`
	Running bool        `json:"running"`         // hub.py runs it
	In      []int       `json:"in"`              // frames in per hub port (index 0 = port 1)
	MACs    []EmuHubMAC `json:"macs"`            // learned addresses
	Cfg     *emu.HubCfg `json:"cfg,omitempty"`   // as it runs
	SSHOK   bool        `json:"sshOk"`           // its SSH server runs (paramiko on the host)
	Error   string      `json:"error,omitempty"` // why not
}

// EmuHubMAC is a learned MAC address.
type EmuHubMAC struct {
	MAC  string `json:"mac"`
	VLAN int    `json:"vlan"`
	Port int    `json:"port"` // the hub's port
	Age  int    `json:"age"`  // seconds since seen
}

// EmuHubDetail reads a hub's numbers from its host.
func (a *App) EmuHubDetail(id string) (EmuHubDetail, error) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var h *emu.Hub
	if l != nil {
		h = l.HubByID(id)
	}
	if h == nil {
		emuLab.mu.Unlock()
		return EmuHubDetail{}, errors.New("no such hub")
	}
	hid, host, dir := h.ID, emuHubHost(l, *h), l.VMDir()
	emuLab.mu.Unlock()
	d := EmuHubDetail{ID: hid, In: []int{}, MACs: []EmuHubMAC{}}
	out, err := a.emuRun(host, "cat "+emu.Q(dir)+"/hubs-stats.json 2>/dev/null", 15*time.Second)
	if err != nil {
		d.Error = errText(err)
		return d, nil
	}
	var st struct {
		At   float64 `json:"at"`
		Hubs map[string]struct {
			In   []int       `json:"in"`
			MACs [][]any     `json:"macs"`
			Cfg  *emu.HubCfg `json:"cfg"`
			SSH  bool        `json:"ssh"`
		} `json:"hubs"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &st) != nil {
		d.Error = "not running"
		return d, nil
	}
	x, ok := st.Hubs[hid]
	if !ok {
		d.Error = "not running"
		return d, nil
	}
	d.Running, d.In, d.Cfg, d.SSHOK = time.Since(time.Unix(int64(st.At), 0)) < 30*time.Second, x.In, x.Cfg, x.SSH
	for _, m := range x.MACs {
		if len(m) >= 4 {
			mac, _ := m[0].(string)
			vl, _ := m[1].(float64)
			pt, _ := m[2].(float64)
			age, _ := m[3].(float64)
			d.MACs = append(d.MACs, EmuHubMAC{MAC: mac, VLAN: int(vl), Port: int(pt), Age: int(age)})
		}
	}
	if x.Cfg != nil { // typed on its command line: the lab keeps it
		emuLab.mu.Lock()
		if h := emuLab.f.Lab.HubByID(hid); h != nil && (h.Cfg == nil || x.Cfg.Rev > h.Cfg.Rev) {
			c := *x.Cfg
			h.Cfg = &c
			_ = a.emuSave()
		}
		emuLab.mu.Unlock()
	}
	return d, nil
}

// emuHubHost is the host a hub runs on: its switches' (the main host without any).
func emuHubHost(l *emu.Lab, h emu.Hub) string {
	for _, g := range h.Legs {
		if v := l.VMByID(g.VM); v != nil {
			return emuHostOf(*v)
		}
	}
	return emuLab.f.Host
}

// EmuHubRemove removes a hub and takes its ports off it.
func (a *App) EmuHubRemove(id string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var legs []emu.HubLeg
	var paths []string
	if l != nil {
		if h := l.HubByID(id); h != nil {
			legs = h.Legs
			paths = append(paths, emuLab.f.Consoles["hub:"+h.ID], emuLab.f.Sessions["hub:"+h.ID])
			delete(emuLab.f.Consoles, "hub:"+h.ID)
			delete(emuLab.f.Sessions, "hub:"+h.ID)
			l.Hubs = slices.DeleteFunc(l.Hubs, func(x emu.Hub) bool { return x.ID == h.ID })
		}
	}
	err := a.emuSave()
	emuLab.mu.Unlock()
	for _, p := range paths {
		if p != "" {
			_ = a.store.Delete(p)
		}
	}
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
		ID      string      `json:"id"`
		Name    string      `json:"name"`
		Kind    string      `json:"kind"`
		Ports   int         `json:"ports"`
		Legs    [][]any     `json:"legs"` // listen, send, hub port, what is there
		Cfg     *emu.HubCfg `json:"cfg,omitempty"`
		Console int         `json:"console,omitempty"`
		SSH     int         `json:"ssh,omitempty"`
	}
	per := map[string][]hcfg{}
	var mk []emu.Hub // L2 switches without their console and SSH sessions yet
	for i := range l.Hubs {
		h := &l.Hubs[i]
		emuHubFixAt(h)
		host := emuHubHost(l, *h)
		c := hcfg{ID: h.ID, Name: h.Name, Kind: h.Kind, Ports: h.HubPortsOf(), Legs: [][]any{}, Cfg: h.Cfg}
		for _, g := range h.Legs {
			v := l.VMByID(g.VM)
			if v == nil {
				continue
			}
			li, se := l.TesterPorts(*v, g.Port)
			pn := strconv.Itoa(g.Port)
			if im := emuImage(v.Image); im != nil {
				pn = im.PortNameOf(g.Port)
			}
			c.Legs = append(c.Legs, []any{li, se, g.At, v.Name + " " + pn})
		}
		if h.Kind == "switch" {
			if h.N == 0 {
				for _, o := range l.Hubs {
					h.N = max(h.N, o.N+1)
				}
			}
			hl := a.emuFor(host)
			c.Console, c.SSH = hl.HubConsolePort(*h), hl.HubSSHPort(*h)
			if emuLab.f.Consoles["hub:"+h.ID] == "" {
				mk = append(mk, *h)
			}
		}
		if h.Kind == "switch" || len(c.Legs) > 1 {
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
	for _, h := range mk {
		a.emuHubSessions(h)
	}
	for _, j := range jobs {
		if out, err := a.emuRun(j.host, j.script, 30*time.Second); err != nil || !strings.Contains(out, "hub-ok") {
			emuLab.mu.Lock()
			emuEv("", "warn", "the hubs did not start on "+j.host[strings.LastIndex(j.host, "/")+1:]+": "+strings.TrimSpace(out+" "+errText(err)))
			emuLab.mu.Unlock()
		}
	}
}

// emuHubSessions makes an L2 switch's console (telnet) and SSH sessions, through its host.
func (a *App) emuHubSessions(h emu.Hub) {
	emuLab.mu.Lock()
	l, folder := emuLab.f.Lab, emuLab.f.Folder
	if l == nil {
		emuLab.mu.Unlock()
		return
	}
	host := emuHubHost(l, h)
	hl := a.emuFor(host)
	con, ssh := hl.HubConsolePort(h), hl.HubSSHPort(h)
	emuLab.mu.Unlock()
	cf := session.JoinPath(folder, "Consoles")
	cs := session.Session{Name: h.Name + " console", Folder: cf, Protocol: session.ProtoTelnet, Hostname: "127.0.0.1", Port: con, JumpSession: host, DeviceType: "L2 switch",
		Description: []string{"Command line of the lab's L2 switch " + h.Name + " (hub.py on its KVM host)"}}
	ss := session.Session{Name: h.Name, Folder: folder, Protocol: session.ProtoSSH2, Hostname: "127.0.0.1", Port: ssh, Username: "admin", JumpSession: host, DeviceType: "L2 switch",
		Description: []string{"SSH to the lab's L2 switch " + h.Name + " (any password; needs paramiko on its KVM host)"}}
	var cp, sp string
	if saved, err := a.store.Save(cs, ""); err == nil {
		cp = saved.Path
	}
	if saved, err := a.store.Save(ss, ""); err == nil {
		sp = saved.Path
	}
	emuLab.mu.Lock()
	if emuLab.f.Consoles == nil {
		emuLab.f.Consoles = map[string]string{}
	}
	if emuLab.f.Sessions == nil {
		emuLab.f.Sessions = map[string]string{}
	}
	if cp != "" {
		emuLab.f.Consoles["hub:"+h.ID] = cp
	}
	if sp != "" {
		emuLab.f.Sessions["hub:"+h.ID] = sp
	}
	_ = a.emuSave()
	emuLab.mu.Unlock()
}

// emuCloudMax is the most ports a cloud has.
const emuCloudMax = 8

// emuCloudUsed is how many switch ports are on a host's cloud (mu held).
func emuCloudUsed(l *emu.Lab, host string) int {
	n := 0
	for _, v := range l.VMs {
		if emuHostOf(v) == host {
			n += len(v.Uplinks)
		}
	}
	return n
}

// emuCloudPorts is how many ports a host's cloud has: as set, at least those in use (mu held).
func emuCloudPorts(l *emu.Lab, host string) int {
	return max(1, l.CloudPorts[host], emuCloudUsed(l, host))
}

// EmuSetCloudPorts sets how many ports a host's cloud has (1-8, not fewer than are in use).
func (a *App) EmuSetCloudPorts(host string, n int) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	switch {
	case l == nil:
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	case n < 1 || n > emuCloudMax:
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("a cloud has 1 to %d ports", emuCloudMax)
	case n < emuCloudUsed(l, host):
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("%d of its ports are in use: take cables off first", emuCloudUsed(l, host))
	}
	if l.CloudPorts == nil {
		l.CloudPorts = map[string]int{}
	}
	l.CloudPorts[host] = n
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}
