package sim

import (
	"errors"
	"fmt"
	"sort"
)

// DeviceView is a switch as the Lab window shows it.
type DeviceView struct {
	ID       string `json:"id"`
	OS       string `json:"os"`
	Model    string `json:"model"`
	Version  string `json:"version"`
	Serial   string `json:"serial"`
	MAC      string `json:"mac"`
	Hostname string `json:"hostname"` // the running name (it may be changed on the switch)
	MgmtIP   string `json:"mgmtIp"`
	Port     int    `json:"port"`
	Ports    int    `json:"ports"`
	Running  bool   `json:"running"`
	Logins   int    `json:"logins"`
	Unsaved  bool   `json:"unsaved"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
}

// LinkView is a cable with its state.
type LinkView struct {
	Link
	Up bool `json:"up"`
}

// View returns the switches and cables.
func (l *Lab) View() ([]DeviceView, []LinkView) {
	logins := l.Logins()
	l.mu.Lock()
	defer l.mu.Unlock()
	var ds []DeviceView
	for _, d := range l.devices {
		ds = append(ds, DeviceView{ID: d.ID, OS: d.OS, Model: d.Model, Version: d.Version, Serial: d.Serial, MAC: d.MAC, Hostname: d.cur.Hostname, MgmtIP: d.cur.MgmtIP,
			Port: d.Port, Ports: len(d.cur.Ports), Running: l.lns[d.ID] != nil, Logins: logins[d.ID], Unsaved: d.unsaved, X: d.X, Y: d.Y})
	}
	var ls []LinkView
	for _, k := range l.links {
		a := l.Device(k.A)
		up := a != nil && l.lns[k.A] != nil && l.lns[k.B] != nil && a.portUp(k.APort)
		ls = append(ls, LinkView{Link: k, Up: up})
	}
	return ds, ls
}

// Add puts new switches and cables in the lab (not started).
func (l *Lab) Add(specs []Spec, links []Link) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range specs {
		if d := l.newDevice(s); d != nil {
			l.devices = append(l.devices, d)
		}
	}
	l.links = append(l.links, links...)
}

// Remove stops and removes switches with their cables.
func (l *Lab) Remove(ids []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gone := map[string]bool{}
	for _, id := range ids {
		if d := l.Device(id); d != nil {
			l.stopLocked(d)
			gone[id] = true
		}
	}
	var ds []*Device
	for _, d := range l.devices {
		if !gone[d.ID] {
			ds = append(ds, d)
		}
	}
	l.devices = ds
	var ls []Link
	for _, k := range l.links {
		if !gone[k.A] && !gone[k.B] {
			ls = append(ls, k)
		}
	}
	l.links = ls
}

// Connect adds a cable between two free ports.
func (l *Lab) Connect(k Link) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, b := l.Device(k.A), l.Device(k.B)
	switch {
	case a == nil || b == nil:
		return errors.New("no such switch")
	case a == b:
		return errors.New("a cable needs two different switches")
	case a.savedPort(k.APort) == nil:
		return fmt.Errorf("%s has no port %s", a.Saved.Hostname, k.APort)
	case b.savedPort(k.BPort) == nil:
		return fmt.Errorf("%s has no port %s", b.Saved.Hostname, k.BPort)
	}
	if o, _ := l.peer(a, k.APort); o != nil {
		return fmt.Errorf("%s port %s is cabled already", a.Saved.Hostname, k.APort)
	}
	if o, _ := l.peer(b, k.BPort); o != nil {
		return fmt.Errorf("%s port %s is cabled already", b.Saved.Hostname, k.BPort)
	}
	l.links = append(l.links, k)
	return nil
}

// Disconnect removes the cable on a switch's port.
func (l *Lab) Disconnect(id, port string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var ls []Link
	for _, k := range l.links {
		if !(k.A == id && k.APort == port) && !(k.B == id && k.BPort == port) {
			ls = append(ls, k)
		}
	}
	l.links = ls
}

// Place moves a switch in the topology picture.
func (l *Lab) Place(id string, x, y int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if d := l.Device(id); d != nil {
		d.X, d.Y = x, y
	}
}

// FreePorts: a switch's ports without a cable, in order.
func (l *Lab) FreePorts(id string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.Device(id)
	if d == nil {
		return nil
	}
	var out []string
	for _, p := range d.Saved.Ports {
		if o, _ := l.peer(d, p.Name); o == nil {
			out = append(out, p.Name)
		}
	}
	return out
}

// Show runs a read-only command on a switch and returns what it prints.
func (l *Lab) Show(id, cmd string) string {
	d := l.Device(id)
	if d == nil {
		return ""
	}
	var b stringsWriter
	s := newShell(d, &b)
	if d.osd.ExecMode != "" {
		s.mode = d.osd.ExecMode
	}
	s.exec(cmd)
	s.flush()
	return b.String()
}

func (d *Device) savedPort(name string) *Port {
	for _, p := range d.Saved.Ports {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// SortedIDs: the switch ids in name order.
func (l *Lab) SortedIDs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, d := range l.devices {
		out = append(out, d.ID)
	}
	sort.Slice(out, func(i, j int) bool { return l.Device(out[i]).Saved.Hostname < l.Device(out[j]).Saved.Hostname })
	return out
}

type stringsWriter struct{ b []byte }

func (w *stringsWriter) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }
func (w *stringsWriter) String() string              { return string(w.b) }
