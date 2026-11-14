// Package sim runs simulated switches for experiments, described by a device pack: each one
// is an SSH server on this computer with the switch's command line (show
// commands, VLANs, IP interfaces, ports, save, ping between the simulated
// switches over their links). A switch is a few goroutines and a few KB of
// state - no virtual machine - so a lab of dozens runs on a laptop. It is not
// the real operating system: protocols (STP, LACP, MLAG, OSPF…) are not run.
package sim

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Port is one front-panel port.
type Port struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Desc    string `json:"desc,omitempty"`
	Tagged  bool   `json:"tagged,omitempty"` // a trunk port: VLANs added to it are tagged (ports.tagging)
	Type    string `json:"type,omitempty"`   // a port type of the pack (T, MW, X…)
}

// VLAN with its member ports (port name → tagged) and an optional IP interface.
type VLAN struct {
	Name  string          `json:"name"`
	Tag   int             `json:"tag"`
	Ports map[string]bool `json:"ports"`
	IP    string          `json:"ip,omitempty"` // "10.1.1.1/24"
}

// Config is what "save" keeps: the switch's configuration.
type Config struct {
	Hostname string            `json:"hostname"`
	Location string            `json:"location,omitempty"`
	Contact  string            `json:"contact,omitempty"`
	MgmtIP   string            `json:"mgmtIp"` // VR-Mgmt / MgmtRouter, "10.99.0.11/24"
	Ports    []*Port           `json:"ports"`
	VLANs    []*VLAN           `json:"vlans"`
	Extra    []Extra           `json:"extra,omitempty"` // configuration kept without being modelled
	KV       map[string]string `json:"kv,omitempty"`    // values a pack keeps (set kv.<key>)
	Routes   []Route           `json:"routes,omitempty"`
}

// Route is a static route.
type Route struct {
	Net     string `json:"net"`
	Gateway string `json:"gateway"`
}

// Spec describes a simulated switch.
type Spec struct {
	ID       string            `json:"id"`
	OS       string            `json:"os"`               // an OS id of the pack
	Model    string            `json:"model"`            // the system type it shows
	SKU      string            `json:"sku"`              // the pack's model
	Fields   map[string]string `json:"fields,omitempty"` // values generated for it (the model's fields that are formats)
	Version  string            `json:"version"`
	Serial   string            `json:"serial"`
	MAC      string            `json:"mac"`
	Port     int               `json:"port"`               // SSH port on 127.0.0.1
	Password string            `json:"password,omitempty"` // "" = any password
	Saved    Config            `json:"saved"`              // the configuration it boots with
	X        int               `json:"x,omitempty"`        // place in the topology picture
	Y        int               `json:"y,omitempty"`
}

// Link is a cable between two ports of two switches.
type Link struct {
	A     string `json:"a"` // device id
	APort string `json:"aPort"`
	B     string `json:"b"`
	BPort string `json:"bPort"`
}

// Device is a running switch.
type Device struct {
	Spec
	lab     *Lab
	osd     *OSDef
	model   Model
	cur     Config // running configuration
	unsaved bool
	booted  time.Time
	log     []string
}

// Lab is a set of switches and the links between them.
type Lab struct {
	mu      sync.Mutex
	pack    *Pack
	devices []*Device
	links   []Link
	onSave  func()   // a switch saved its configuration
	outputs []Output // show commands with a kept output
	key     ssh.Signer
	lns     map[string]net.Listener // running switches
	conns   sync.Map                // net.Conn → device id
}

// NewLab builds a lab of a pack's switches (not listening yet: see Serve).
// Switches whose OS the pack does not have are left out.
func NewLab(pack *Pack, specs []Spec, links []Link) *Lab {
	l := &Lab{pack: pack, links: links}
	for _, s := range specs {
		if d := l.newDevice(s); d != nil {
			l.devices = append(l.devices, d)
		}
	}
	return l
}

func (l *Lab) newDevice(s Spec) *Device {
	if l.pack == nil || l.pack.OS[s.OS] == nil {
		return nil
	}
	d := &Device{Spec: s, lab: l, osd: l.pack.OS[s.OS], booted: time.Now()}
	d.model, _ = l.pack.ModelOf(s.SKU)
	if len(d.Saved.Ports) == 0 {
		d.Saved = DefaultConfig(d.osd.PortStyle, s.Saved.Hostname, s.Saved.MgmtIP, l.pack.Layout(d.model))
	}
	d.cur = cloneConfig(d.Saved)
	return d
}

// Pack is the lab's device pack.
func (l *Lab) Pack() *Pack { return l.pack }

// OnSave is called (without the lab lock) after a switch saved its configuration.
func (l *Lab) OnSave(f func()) { l.onSave = f }

// Specs returns the switches with their saved configurations, for keeping the lab.
func (l *Lab) Specs() []Spec {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Spec, len(l.devices))
	for i, d := range l.devices {
		out[i] = d.Spec
		out[i].Saved = cloneConfig(d.Saved)
	}
	return out
}

// Links returns the cables.
func (l *Lab) Links() []Link {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Link(nil), l.links...)
}

// Device by id (nil when unknown).
func (l *Lab) Device(id string) *Device {
	for _, d := range l.devices {
		if d.ID == id {
			return d
		}
	}
	return nil
}

// DefaultConfig: the ports of a layout (48 copper when empty), all in the
// default VLAN, enabled; style "slot" names them 1/1, 1/2…
func DefaultConfig(style, hostname, mgmt string, layout []PortGroup) Config {
	if len(layout) == 0 {
		layout = []PortGroup{{Count: 48}}
	}
	c := Config{Hostname: hostname, MgmtIP: mgmt}
	def := &VLAN{Name: "Default", Tag: 1, Ports: map[string]bool{}}
	n := 0
	for _, g := range layout {
		for k := 0; k < g.Count; k++ {
			n++
			name := portName(style, n)
			c.Ports = append(c.Ports, &Port{Name: name, Enabled: true, Type: g.Type})
			def.Ports[name] = false
		}
	}
	c.VLANs = []*VLAN{def}
	return c
}

func portName(style string, n int) string {
	if style == "slot" {
		return "1/" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func (d *Device) style() string { return d.osd.PortStyle }

func cloneConfig(c Config) Config {
	out := c
	out.Ports = nil
	for _, p := range c.Ports {
		q := *p
		out.Ports = append(out.Ports, &q)
	}
	out.Extra = append([]Extra(nil), c.Extra...)
	out.Routes = append([]Route(nil), c.Routes...)
	if c.KV != nil {
		out.KV = map[string]string{}
		for k, v := range c.KV {
			out.KV[k] = v
		}
	}
	out.VLANs = nil
	for _, v := range c.VLANs {
		w := *v
		w.Ports = map[string]bool{}
		for k, t := range v.Ports {
			w.Ports[k] = t
		}
		out.VLANs = append(out.VLANs, &w)
	}
	return out
}

func (d *Device) port(name string) *Port {
	for _, p := range d.cur.Ports {
		if p.Name == name {
			return p
		}
	}
	return nil
}

func (d *Device) vlanByName(name string) *VLAN {
	for _, v := range d.cur.VLANs {
		if strings.EqualFold(v.Name, name) {
			return v
		}
	}
	return nil
}

func (d *Device) vlanByTag(tag int) *VLAN {
	for _, v := range d.cur.VLANs {
		if v.Tag == tag {
			return v
		}
	}
	return nil
}

func (d *Device) changed() { d.unsaved = true }

func (d *Device) logf(f string, v ...any) {
	d.log = append(d.log, time.Now().Format("01/02/2006 15:04:05.00")+" "+fmt.Sprintf(f, v...))
	if len(d.log) > 200 {
		d.log = d.log[len(d.log)-200:]
	}
}

// save makes the running configuration the saved one.
func (d *Device) save() {
	d.Saved = cloneConfig(d.cur)
	d.unsaved = false
	if f := d.lab.onSave; f != nil {
		go f()
	}
}

// reboot drops what was not saved.
func (d *Device) reboot() {
	d.cur = cloneConfig(d.Saved)
	d.unsaved = false
	d.booted = time.Now()
	d.log = nil
}

// portUp: enabled, cabled, and the other end enabled too.
func (d *Device) portUp(name string) bool {
	p := d.port(name)
	if p == nil || !p.Enabled {
		return false
	}
	o, op := d.lab.peer(d, name)
	if o == nil {
		return false
	}
	q := o.port(op)
	return q != nil && q.Enabled
}

// peer: the switch and port at the other end of a port's cable.
func (l *Lab) peer(d *Device, port string) (*Device, string) {
	for _, k := range l.links {
		if k.A == d.ID && k.APort == port {
			return l.Device(k.B), k.BPort
		}
		if k.B == d.ID && k.BPort == port {
			return l.Device(k.A), k.APort
		}
	}
	return nil, ""
}

// PingResult: route = the address is in a connected subnet (otherwise the
// switch cannot send at all); ok = it answers.
type PingResult struct {
	Route bool
	OK    bool
	RTT   time.Duration
}

// ping from d to dst in the management VR/VRF (mgmt) or in-band.
func (d *Device) ping(dst netip.Addr, mgmt bool) PingResult {
	if mgmt {
		src, err := netip.ParsePrefix(d.cur.MgmtIP)
		if err != nil || !src.Contains(dst) {
			return PingResult{}
		}
		if src.Addr() == dst {
			return PingResult{Route: true, OK: true, RTT: 50 * time.Microsecond}
		}
		for _, o := range d.lab.devices {
			if p, err := netip.ParsePrefix(o.cur.MgmtIP); err == nil && p.Addr() == dst {
				return PingResult{Route: true, OK: true, RTT: rtt(d, o)}
			}
		}
		return PingResult{Route: true}
	}
	for _, v := range d.cur.VLANs {
		src, err := netip.ParsePrefix(v.IP)
		if err != nil || !src.Contains(dst) {
			continue
		}
		if src.Addr() == dst {
			return PingResult{Route: true, OK: true, RTT: 50 * time.Microsecond}
		}
		for _, o := range d.lab.reach(d, v.Tag) {
			for _, w := range o.cur.VLANs {
				if p, err := netip.ParsePrefix(w.IP); err == nil && p.Addr() == dst && w.Tag == v.Tag {
					return PingResult{Route: true, OK: true, RTT: rtt(d, o)}
				}
			}
		}
		return PingResult{Route: true}
	}
	return PingResult{}
}

func rtt(a, b *Device) time.Duration {
	h := 0
	for _, c := range a.ID + b.ID {
		h += int(c)
	}
	return time.Duration(400+h%900) * time.Microsecond
}

// reach: the switches a VLAN reaches from d over cables whose ports are up
// and members of that VLAN tag at both ends (d itself not included).
func (l *Lab) reach(d *Device, tag int) []*Device {
	seen := map[*Device]bool{d: true}
	queue := []*Device{d}
	var out []*Device
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		v := x.vlanByTag(tag)
		if v == nil {
			continue
		}
		for p := range v.Ports {
			if !x.portUp(p) {
				continue
			}
			o, op := l.peer(x, p)
			if o == nil || seen[o] {
				continue
			}
			if w := o.vlanByTag(tag); w != nil {
				if _, in := w.Ports[op]; in {
					seen[o] = true
					out = append(out, o)
					queue = append(queue, o)
				}
			}
		}
	}
	return out
}

// neighbors: the cabled ports that are up, for "show lldp neighbors":
// local port, neighbor name, neighbor port, neighbor MAC.
func (d *Device) neighbors() [][4]string {
	var out [][4]string
	for _, p := range d.cur.Ports {
		if !d.portUp(p.Name) {
			continue
		}
		if o, op := d.lab.peer(d, p.Name); o != nil {
			out = append(out, [4]string{p.Name, o.cur.Hostname, op, o.MAC})
		}
	}
	return out
}

// parsePorts reads "1-4,7" (style n) or "1/1-1/4,1/7" (style slot) into port names
// the switch has; bad = a part that is not a port.
func (d *Device) parsePorts(s string) (names []string, bad string) {
	have := map[string]bool{}
	for _, p := range d.cur.Ports {
		have[p.Name] = true
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.EqualFold(part, "all") {
			for _, p := range d.cur.Ports {
				names = append(names, p.Name)
			}
			continue
		}
		a, b, rng := strings.Cut(part, "-")
		if !rng {
			if !have[part] {
				return nil, part
			}
			names = append(names, part)
			continue
		}
		pre := ""
		if i := strings.LastIndex(a, "/"); i >= 0 {
			pre = a[:i+1]
			if j := strings.LastIndex(b, "/"); j >= 0 {
				b = b[j+1:]
			}
			a = a[i+1:]
		}
		x, e1 := strconv.Atoi(a)
		y, e2 := strconv.Atoi(b)
		if e1 != nil || e2 != nil || y < x {
			return nil, part
		}
		for i := x; i <= y; i++ {
			n := pre + strconv.Itoa(i)
			if !have[n] {
				return nil, n
			}
			names = append(names, n)
		}
	}
	return names, ""
}

// portList writes port names back as ranges: "n" style "1-4, 7", "slot" style "1/1-1/4,1/7".
func portList(names []string, style string) string {
	if style == "slot" {
		return ranges(",", true, names)
	}
	return ranges(", ", false, names)
}

// ranges joins port names as ranges; full = the end of a range has its slot too.
func ranges(sep string, full bool, names []string) string {
	type pn struct {
		pre string
		n   int
	}
	var ps []pn
	for _, s := range names {
		pre, num := "", s
		if i := strings.LastIndex(s, "/"); i >= 0 {
			pre, num = s[:i+1], s[i+1:]
		}
		n, _ := strconv.Atoi(num)
		ps = append(ps, pn{pre, n})
	}
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].pre != ps[j].pre {
			return ps[i].pre < ps[j].pre
		}
		return ps[i].n < ps[j].n
	})
	var parts []string
	for i := 0; i < len(ps); {
		j := i
		for j+1 < len(ps) && ps[j+1].pre == ps[i].pre && ps[j+1].n == ps[j].n+1 {
			j++
		}
		a := ps[i].pre + strconv.Itoa(ps[i].n)
		if j > i {
			b := strconv.Itoa(ps[j].n)
			if full {
				b = ps[j].pre + b
			}
			a += "-" + b
		}
		parts = append(parts, a)
		i = j + 1
	}
	return strings.Join(parts, sep)
}

// sortPorts orders port names by slot and number.
func sortPorts(names []string) []string {
	out := append([]string(nil), names...)
	key := func(s string) (string, int) {
		pre, num := "", s
		if i := strings.LastIndex(s, "/"); i >= 0 {
			pre, num = s[:i+1], s[i+1:]
		}
		n, _ := strconv.Atoi(num)
		return pre, n
	}
	sort.Slice(out, func(i, j int) bool {
		a, x := key(out[i])
		b, y := key(out[j])
		if a != b {
			return a < b
		}
		return x < y
	})
	return out
}

func countUp(d *Device, v *VLAN) int {
	n := 0
	for p := range v.Ports {
		if d.portUp(p) {
			n++
		}
	}
	return n
}

// maskLen: "255.255.255.0" → "24" (a length is kept).
func maskLen(m string) string {
	a, err := netip.ParseAddr(m)
	if err != nil {
		return m
	}
	n := 0
	for _, b := range a.AsSlice() {
		for i := 7; i >= 0; i-- {
			if b&(1<<i) != 0 {
				n++
			}
		}
	}
	return strconv.Itoa(n)
}

// lenMask: 24 → "255.255.255.0".
func lenMask(bits int) string {
	var b [4]byte
	for i := 0; i < bits && i < 32; i++ {
		b[i/8] |= 1 << (7 - i%8)
	}
	return netip.AddrFrom4(b).String()
}

// bootCount: a stable number of boots for a switch.
func (d *Device) bootCount() int {
	h := 0
	for _, c := range d.Serial {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return 3 + h%990
}

type learnedMAC struct {
	mac, vlan, port string
	tag             int
}

// learned: MAC addresses seen on up ports (the neighbor's MAC) by VLAN.
func (d *Device) learned() []learnedMAC {
	var out []learnedMAC
	for _, p := range d.cur.Ports {
		if !d.portUp(p.Name) {
			continue
		}
		o, op := d.lab.peer(d, p.Name)
		if o == nil {
			continue
		}
		for _, v := range d.cur.VLANs {
			if _, in := v.Ports[p.Name]; !in {
				continue
			}
			if w := o.vlanByTag(v.Tag); w != nil {
				if _, in := w.Ports[op]; in {
					out = append(out, learnedMAC{mac: o.MAC, vlan: v.Name, tag: v.Tag, port: p.Name})
				}
			}
		}
	}
	return out
}

// arp: the addresses of the other switches that answer in the device's subnets.
func (d *Device) arp() [][3]string {
	var out [][3]string
	for _, v := range d.cur.VLANs {
		src, err := netip.ParsePrefix(v.IP)
		if err != nil {
			continue
		}
		for _, o := range d.lab.reach(d, v.Tag) {
			if w := o.vlanByTag(v.Tag); w != nil {
				if p, err := netip.ParsePrefix(w.IP); err == nil && src.Contains(p.Addr()) {
					out = append(out, [3]string{p.Addr().String(), o.MAC, v.Name})
				}
			}
		}
	}
	return out
}

// staticRoutes: the static routes (net, gateway).
func (d *Device) staticRoutes() [][2]string {
	var out [][2]string
	for _, r := range d.cur.Routes {
		out = append(out, [2]string{r.Net, r.Gateway})
	}
	return out
}

func sortedPorts(m map[string]bool, tagged bool) []string {
	var out []string
	for p, t := range m {
		if t == tagged {
			out = append(out, p)
		}
	}
	return out
}

func uptime(t time.Time) string {
	s := int(time.Since(t).Seconds())
	return fmt.Sprintf("%d days %d hours %d minutes %d seconds", s/86400, s/3600%24, s/60%60, s%60)
}
