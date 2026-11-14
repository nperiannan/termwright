package sim

import (
	"bytes"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// What a pack's templates see (and the functions they can call).

// TData is the data of an output template.
type TData struct {
	Sys       SysView
	Model     map[string]string // the model's fields (part, rev, bootrom…) and generated values
	VLANs     []VLANView
	Ports     []PortView
	Neighbors []Neighbor
	FDB       []FDBEntry
	ARP       []ARPEntry
	Static    []RouteView // static routes kept from the configuration
	KV        map[string]string
	Extra     []Extra
	ExtraCtx  []ExtraGroup // kept lines by context ("" first)
	Args      map[string]string
	Ping      *PingView
	Err       ErrView
	Unsaved   bool
	Paging    bool
	CmdNo     int
	Mode      string
	Ctx       CtxView
	Sel       []string // ports chosen by ports.select
	Log       []string // newest first
	Now       time.Time
}

// SysView: the switch itself.
type SysView struct {
	ID, Hostname, Location, Contact, MAC, Model, SKU, Version, Serial, OS string
	MgmtIP, MgmtAddr, MgmtMask, MgmtNet                                   string
	MgmtBits                                                              int
	Booted                                                                time.Time
	BootCount                                                             int
	Ports                                                                 int
}

// VLANView is a VLAN for the outputs.
type VLANView struct {
	Name, IP, Addr, Mask, Net string
	Tag, Bits, Active, Total  int
	Untagged, Tagged, Ports   []string
	ActivePorts               []string
}

// PortView is a port for the outputs.
type PortView struct {
	Name, Desc, Type, Speed, VLAN string // VLAN: its only VLAN, or "(n)"
	Index, VLANs                  int
	Enabled, Up, Tagged           bool
}

// Neighbor is what LLDP sees on a port.
type Neighbor struct{ Port, Name, PeerPort, MAC string }

// FDBEntry is a learned MAC address.
type FDBEntry struct {
	MAC, VLAN, Port string
	Tag             int
}

// ARPEntry is a learned address.
type ARPEntry struct{ Addr, MAC, VLAN string }

// RouteView is a static route.
type RouteView struct{ Net, Addr, Mask, Gateway string }

// PingView is the result of a ping or traceroute.
type PingView struct {
	Host      string
	Count     int
	Route, OK bool
	MS        float64
	Mgmt      bool
	VR        string
}

// ErrView is an action's error.
type ErrView struct{ Code, Value string }

// CtxView: the interface being configured.
type CtxView struct {
	Ports []string
	VLAN  int
}

// ExtraGroup: kept lines of one context.
type ExtraGroup struct {
	Ctx   string
	Lines []string
}

var tmplFuncs = template.FuncMap{
	"pad":   func(w int, v any) string { return fmt.Sprintf("%-*v", w, v) },
	"rpad":  func(w int, v any) string { return fmt.Sprintf("%*v", w, v) },
	"upper": strings.ToUpper, "lower": strings.ToLower, "join": func(sep string, a []string) string { return strings.Join(a, sep) },
	"repeat": strings.Repeat, "trim": strings.TrimSpace, "hasPrefix": strings.HasPrefix, "trimPrefix": strings.TrimPrefix,
	"trimSuffix": strings.TrimSuffix, "contains": strings.Contains, "split": strings.Split, "replace": strings.ReplaceAll,
	"add": func(a, b int) int { return a + b }, "sub": func(a, b int) int { return a - b }, "mul": func(a, b int) int { return a * b },
	"div": func(a, b int) int {
		if b == 0 {
			return 0
		}
		return a / b
	},
	"mod": func(a, b int) int {
		if b == 0 {
			return 0
		}
		return a % b
	},
	"fadd": func(a, b float64) float64 { return a + b },
	"fmul": func(a float64, b int) float64 { return a * float64(b) },
	"seq": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	},
	"default": func(def string, v string) string {
		if v == "" {
			return def
		}
		return v
	},
	"date": func(layout string, t time.Time) string { return t.Format(layout) },
	"utc":  func(t time.Time) time.Time { return t.UTC() },
	"since": func(t time.Time) map[string]int {
		s := int(time.Since(t).Seconds())
		return map[string]int{"days": s / 86400, "hours": s / 3600 % 24, "minutes": s / 60 % 60, "seconds": s % 60, "total": s}
	},
	"atoi": func(s string) int { n, _ := strconv.Atoi(s); return n },
	"has": func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	},
	"vlan": func(list []VLANView, ref string) *VLANView {
		for i := range list {
			if strings.EqualFold(list[i].Name, ref) || "#"+strconv.Itoa(list[i].Tag) == ref {
				return &list[i]
			}
		}
		return nil
	},
	"ranges": ranges,
	"lines": func(extra []Extra, prefix string) []string {
		var out []string
		for _, e := range extra {
			if strings.HasPrefix(strings.ToLower(e.Line), strings.ToLower(prefix)+" ") {
				out = append(out, strings.TrimSpace(e.Line[len(prefix):]))
			}
		}
		return out
	},
	"itoa":    strconv.Itoa,
	"newer":   newerThan,
	"maskLen": maskLen,
	"lenMask": lenMask,
	"hash": func(s string, n int) int {
		h := 0
		for _, c := range s {
			h = h*31 + int(c)
		}
		if h < 0 {
			h = -h
		}
		if n <= 0 {
			return h
		}
		return h % n
	},
}

// data builds a template's data (lab lock held).
func (s *Shell) data(a args) TData {
	d := s.d
	t := TData{Model: map[string]string{}, KV: map[string]string{}, Args: map[string]string(a), Unsaved: d.unsaved, Paging: s.paging, CmdNo: s.cmdNo,
		Mode: s.mode, Ctx: CtxView{Ports: s.ifPorts, VLAN: s.ifVLAN}, Now: time.Now(), Extra: d.cur.Extra, Ping: s.ping, Err: s.err, Sel: s.sel}
	for i := len(d.log) - 1; i >= 0; i-- {
		t.Log = append(t.Log, d.log[i])
	}
	for k, v := range d.model.Fields {
		t.Model[k] = v
	}
	for k, v := range d.Fields {
		t.Model[k] = v
	}
	for k, v := range d.cur.KV {
		t.KV[k] = v
	}
	t.Sys = SysView{ID: d.ID, Hostname: d.cur.Hostname, Location: d.cur.Location, Contact: d.cur.Contact, MAC: d.MAC, Model: d.Model, SKU: d.SKU,
		Version: d.Version, Serial: d.Serial, OS: d.OS, MgmtIP: d.cur.MgmtIP, Booted: d.booted, BootCount: d.bootCount(), Ports: len(d.cur.Ports)}
	if p, err := netip.ParsePrefix(d.cur.MgmtIP); err == nil {
		t.Sys.MgmtAddr, t.Sys.MgmtMask, t.Sys.MgmtNet, t.Sys.MgmtBits = p.Addr().String(), lenMask(p.Bits()), p.Masked().Addr().String(), p.Bits()
	}
	style := d.style()
	for _, v := range d.cur.VLANs {
		vv := VLANView{Name: v.Name, Tag: v.Tag, IP: v.IP, Total: len(v.Ports), Active: countUp(d, v)}
		if p, err := netip.ParsePrefix(v.IP); err == nil {
			vv.Addr, vv.Mask, vv.Net, vv.Bits = p.Addr().String(), lenMask(p.Bits()), p.Masked().Addr().String(), p.Bits()
		}
		vv.Untagged, vv.Tagged = sortPorts(sortedPorts(v.Ports, false)), sortPorts(sortedPorts(v.Ports, true))
		vv.Ports = sortPorts(append(append([]string{}, vv.Untagged...), vv.Tagged...))
		for _, p := range vv.Ports {
			if d.portUp(p) {
				vv.ActivePorts = append(vv.ActivePorts, p)
			}
		}
		t.VLANs = append(t.VLANs, vv)
	}
	for i, p := range d.cur.Ports {
		pv := PortView{Name: p.Name, Desc: p.Desc, Type: p.Type, Index: i + 1, Enabled: p.Enabled, Up: d.portUp(p.Name), Tagged: p.Tagged}
		if pt, ok := d.lab.pack.PortTypes[p.Type]; ok {
			pv.Type, pv.Speed = pt.Name, pt.Speed
		}
		for _, v := range d.cur.VLANs {
			if _, in := v.Ports[p.Name]; in {
				pv.VLAN = v.Name
				pv.VLANs++
			}
		}
		if pv.VLANs > 1 {
			pv.VLAN = "(" + strconv.Itoa(pv.VLANs) + ")"
		}
		t.Ports = append(t.Ports, pv)
	}
	for _, n := range d.neighbors() {
		t.Neighbors = append(t.Neighbors, Neighbor{Port: n[0], Name: n[1], PeerPort: n[2], MAC: n[3]})
	}
	for _, e := range d.learned() {
		t.FDB = append(t.FDB, FDBEntry{MAC: e.mac, VLAN: e.vlan, Tag: e.tag, Port: e.port})
	}
	for _, e := range d.arp() {
		t.ARP = append(t.ARP, ARPEntry{Addr: e[0], MAC: e[1], VLAN: e[2]})
	}
	for _, r := range d.staticRoutes() {
		rv := RouteView{Net: r[0], Gateway: r[1]}
		if p, err := netip.ParsePrefix(r[0]); err == nil {
			rv.Addr, rv.Mask = p.Masked().Addr().String(), lenMask(p.Bits())
		}
		t.Static = append(t.Static, rv)
	}
	idx := map[string]int{}
	for _, e := range d.cur.Extra {
		i, ok := idx[e.Ctx]
		if !ok {
			i = len(t.ExtraCtx)
			idx[e.Ctx] = i
			t.ExtraCtx = append(t.ExtraCtx, ExtraGroup{Ctx: e.Ctx})
		}
		t.ExtraCtx[i].Lines = append(t.ExtraCtx[i].Lines, e.Line)
	}
	sort.SliceStable(t.ExtraCtx, func(i, j int) bool { return t.ExtraCtx[i].Ctx == "" && t.ExtraCtx[j].Ctx != "" })
	_ = style
	return t
}

// render runs a template of the device's OS ("" output when it has none).
func (s *Shell) render(name string, a args) string {
	t := s.d.osd.tmpl.Lookup(name)
	if t == nil {
		return ""
	}
	var b bytes.Buffer
	if err := t.Execute(&b, s.data(a)); err != nil {
		return "(template " + name + ": " + err.Error() + ")\n"
	}
	return b.String()
}
