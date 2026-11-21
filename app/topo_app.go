package app

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/termwright/termwright/internal/textfsm"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/discover"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/term"
	"github.com/termwright/termwright/internal/topo"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// The Network Map: reads the neighbours, ports, LAGs and spanning-tree state of real devices from their
// sessions (read-only commands over the same hidden connection backups use) and joins them into a map.

// TopoRequest is what to read.
type TopoRequest struct {
	// Targets are session or folder paths (a folder = every device session inside it).
	Targets []string `json:"targets"`
	// Scope names the map in memory so the next read of it can tell what changed ("" = from the targets).
	Scope string `json:"scope"`
	// Production: the user agreed to read sessions marked Production.
	Production bool `json:"production"`
	// Collect: "cli" (default), "snmp" (SNMP only), or "both" (the CLI, and SNMP for a device whose login fails).
	Collect string `json:"collect"`
	// Community of SNMP v2c; "" = public. It is only sent to the devices of this read and never kept or logged.
	Community string `json:"community"`
}

// TopoResult is the map and what changed since the last read of the same scope.
type TopoResult struct {
	Map     topo.Map      `json:"map"`
	Changes []topo.Change `json:"changes"` // newest first, kept across reads of the scope
	// NeedsConfirm lists Production sessions when the user has not agreed yet; nothing was read.
	NeedsConfirm []string `json:"needsConfirm,omitempty"`
	Read         int      `json:"read"`   // devices read
	Failed       int      `json:"failed"` // devices that could not be
}

// TopoProgress is one device's progress while a map is read (event "topo:progress"): State is "queued",
// "running", "done" or "error"; Done counts the finished steps (0 = connecting, 1 = connected, 2 = identified,
// 3 = neighbours read, 4 = ports, spanning tree and LAGs read); Now is the command running.
type TopoProgress struct {
	Scope   string `json:"scope"`
	Session string `json:"session"`
	Name    string `json:"name"`
	OS      string `json:"os,omitempty"`
	State   string `json:"state"`
	Done    int    `json:"done"`
	Now     string `json:"now,omitempty"`
	Err     string `json:"err,omitempty"`
	// Phase "build" (all devices answered, the map is being joined) or "end" is sent once with no device.
	Phase   string `json:"phase,omitempty"`
	Elapsed int64  `json:"elapsedMs,omitempty"`
}

func (a *App) emitTopo(p TopoProgress) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "topo:progress", p)
	}
}

// topoWorkers is how many devices are read at the same time.
const topoWorkers = 16

type topoState struct {
	m       topo.Map
	changes []topo.Change
}

var (
	topoMu    sync.Mutex
	topoMaps  = map[string]*topoState{}
	topoBusy  = map[string]bool{}
	topoScope = map[string]bool{}
)

// TopoRead reads the devices of the targets once and returns the map. The window calls it every few minutes
// and on its refresh button; a read of a scope that is still running returns an error instead of piling up.
func (a *App) TopoRead(req TopoRequest) (TopoResult, error) {
	var res TopoResult
	paths, err := a.topoSessions(req.Targets, req.Collect == "snmp")
	if err != nil {
		return res, err
	}
	if len(paths) == 0 {
		return res, errors.New("no device sessions to read (a session needs the SSH or Telnet protocol and a device type such as EXOS, VOSS, Cisco IOS, Arista EOS or Juniper Junos)")
	}
	scope := req.Scope
	if scope == "" {
		scope = strings.Join(req.Targets, "\x00")
	}
	if !req.Production {
		for _, p := range paths {
			if s, err := a.store.Get(p); err == nil && strings.EqualFold(s.Environment, "production") {
				res.NeedsConfirm = append(res.NeedsConfirm, p)
			}
		}
		if len(res.NeedsConfirm) > 0 {
			return res, nil
		}
	}
	topoMu.Lock()
	if topoScope[scope] {
		topoMu.Unlock()
		return res, errors.New("the last read of this map is still running")
	}
	topoScope[scope] = true
	topoMu.Unlock()
	defer func() { topoMu.Lock(); delete(topoScope, scope); topoMu.Unlock() }()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	devs := make([]topo.Device, len(paths))
	var wg sync.WaitGroup
	sem := make(chan struct{}, topoWorkers)
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = p
		if s, err := a.store.Get(p); err == nil {
			names[i] = s.Name
			a.emitTopo(TopoProgress{Scope: scope, Session: p, Name: s.Name, OS: topo.OSFor(s.DeviceType), State: "queued"})
		}
	}
	for i, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, p string) {
			defer func() { <-sem; wg.Done() }()
			t0 := time.Now()
			a.emitTopo(TopoProgress{Scope: scope, Session: p, Name: names[i], State: "running", Done: 0})
			devs[i] = a.topoDevice(withTopoProgress(ctx, a, scope, p, names[i]), p, req.Collect, req.Community)
			d := devs[i]
			ev := TopoProgress{Scope: scope, Session: p, Name: names[i], OS: d.OS, State: "done", Done: 4, Elapsed: time.Since(t0).Milliseconds()}
			if d.Err != "" {
				ev.State, ev.Err, ev.Done = "error", d.Err, 0
			}
			a.emitTopo(ev)
		}(i, p)
	}
	wg.Wait()
	a.emitTopo(TopoProgress{Scope: scope, Phase: "build"})
	defer a.emitTopo(TopoProgress{Scope: scope, Phase: "end"})
	for _, d := range devs {
		if d.Err != "" {
			res.Failed++
		} else {
			res.Read++
		}
	}

	now := time.Now()
	topoMu.Lock()
	st := topoMaps[scope]
	if st == nil {
		st = &topoState{}
		topoMaps[scope] = st
	}
	var prev *topo.Map
	if len(st.m.Nodes) > 0 {
		prev = &st.m
	}
	m := topo.Build(devs, prev, now)
	if prev != nil {
		ch := topo.Diff(*prev, m, now)
		st.changes = append(ch, st.changes...)
		if len(st.changes) > 200 {
			st.changes = st.changes[:200]
		}
	}
	st.m = m
	res.Changes = append([]topo.Change(nil), st.changes...)
	topoMu.Unlock()
	a.topoKnown(&m)
	res.Map = m
	return res, nil
}

type topoProgCtx struct{ scope, session, name string }

// withTopoProgress makes the device's capture report its steps as "topo:progress" events (see topoDevice).
func withTopoProgress(ctx context.Context, a *App, scope, session, name string) context.Context {
	return context.WithValue(ctx, topoProgCtx{}, func(os string) func(done, total int) {
		return func(k, _ int) {
			done, now := topo.StepsDone(os, k)
			a.emitTopo(TopoProgress{Scope: scope, Session: session, Name: name, OS: os, State: "running", Done: done, Now: now})
		}
	})
}

// TopoForget drops what the app remembers of a map (its last read and its changes).
func (a *App) TopoForget(scope string) {
	topoMu.Lock()
	delete(topoMaps, scope)
	topoMu.Unlock()
}

// topoSessions turns folders and sessions into the device sessions to read.
func (a *App) topoSessions(targets []string, anyType bool) ([]string, error) {
	paths, err := a.BackupSessions(targets)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range paths {
		if s, err := a.store.Get(p); err == nil && (anyType || topo.OSFor(s.DeviceType) != "" || a.topoCustom(s.DeviceType) != nil) {
			out = append(out, p)
		}
	}
	return out, nil
}

// topoDevice reads one session. It never returns an error: a device that cannot be read comes back with Err.
func (a *App) topoDevice(ctx context.Context, p, mode, community string) topo.Device {
	s, err := a.store.Get(p)
	if err != nil {
		return topo.Device{Session: p, Name: p, Err: err.Error()}
	}
	t := topo.Target{Session: p, Name: s.Name, OS: topo.OSFor(s.DeviceType)}
	if net.ParseIP(s.Hostname) != nil {
		t.IP = s.Hostname
	}
	topoMu.Lock()
	if topoBusy[p] {
		topoMu.Unlock()
		return topo.Device{Session: p, Name: s.Name, OS: t.OS, IP: t.IP, Err: "already being read"}
	}
	topoBusy[p] = true
	topoMu.Unlock()
	defer func() { topoMu.Lock(); delete(topoBusy, p); topoMu.Unlock() }()

	viaSNMP := func() topo.Device {
		if community == "" {
			community = "public"
		}
		walk := func(ctx context.Context, root string) ([]topo.VB, error) {
			vbs, err := discover.SNMPWalk(ctx, s.Hostname, community, root, 8*time.Second, 4000)
			out := make([]topo.VB, len(vbs))
			for i, v := range vbs {
				out[i] = topo.VB{OID: v.OID, Bytes: v.Bytes, Int: v.Int, IsInt: v.IsInt}
			}
			return out, err
		}
		return topo.CollectSNMP(ctx, t, walk, time.Now())
	}
	if mode == "snmp" {
		return viaSNMP()
	}
	custom := a.topoCustom(s.DeviceType)
	if custom != nil && t.OS == "" {
		t.OS = "custom"
	}
	if t.OS == "" {
		return topo.Device{Session: p, Name: s.Name, Err: "set the session's device type, or give its device type a network map (Options > Device Types > Network Map)"}
	}
	s, err = a.consoleSession(a.withFolderDefaults(s))
	if err != nil {
		return topo.Device{Session: p, Name: s.Name, OS: t.OS, IP: t.IP, Err: err.Error()}
	}
	prompt := ""
	if s.DeviceType != "" && a.terms.DeviceType != nil {
		if dt, ok := a.terms.DeviceType(s.DeviceType); ok {
			prompt = dt.Prompt
		}
	}
	run := func(ctx context.Context, cmds []string) ([]string, error) {
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		if mk, ok := ctx.Value(topoProgCtx{}).(func(string) func(done, total int)); ok {
			cctx = term.WithProgress(cctx, mk(t.OS))
		}
		return a.terms.CaptureEach(cctx, s, cmds, prompt)
	}
	var d topo.Device
	if custom != nil {
		d = topo.CollectCustom(ctx, t, *custom, run, time.Now())
	} else {
		d = topo.Collect(ctx, t, run, time.Now())
	}
	if d.Err != "" && mode == "both" {
		if sd := viaSNMP(); sd.Err == "" {
			sd.Notes = append(sd.Notes, "read by SNMP: the login failed ("+d.Err+")")
			return sd
		}
	}
	return d
}

// topoKnown marks the neighbours without a session that do have one in the Session Manager (by address or
// by name), so the map can offer to open it.
func (a *App) topoKnown(m *topo.Map) {
	root, err := a.store.Tree()
	if err != nil {
		return
	}
	byHost, byName := map[string]string{}, map[string]string{}
	var walk func(n *session.Node)
	walk = func(n *session.Node) {
		for _, c := range n.Children {
			if c.IsFolder {
				walk(c)
				continue
			}
			if c.Hostname != "" {
				byHost[strings.ToLower(c.Hostname)] = c.Path
			}
			byName[topo.NormName(c.Name)] = c.Path
		}
	}
	walk(root)
	for i := range m.Nodes {
		n := &m.Nodes[i]
		if !n.Ghost {
			continue
		}
		if p, ok := byHost[strings.ToLower(n.IP)]; ok && n.IP != "" {
			n.Known = p
		} else if p, ok := byName[topo.NormName(n.Name)]; ok {
			n.Known = p
		}
	}
}

// LayoutGraph arranges boxes and links in a style (auto, layered, spine-leaf, tree, ring, star, mesh, grid,
// organic): the Network Map and the lab canvas use it for "Arrange".
func (a *App) LayoutGraph(req topo.LayoutRequest) topo.LayoutResult { return topo.Layout(req) }

// SaveMapPDF asks where to save and writes a one-page PDF around the map's JPEG picture (base64).
func (a *App) SaveMapPDF(title, name, jpegBase64 string) (string, error) {
	jpg, err := base64.StdEncoding.DecodeString(jpegBase64)
	if err != nil {
		return "", err
	}
	pdf, err := topo.JPEGToPDF(jpg, title)
	if err != nil {
		return "", err
	}
	p, err := a.ChooseSaveFile(title, name, nil)
	if err != nil || p == "" {
		return "", err
	}
	if err := os.WriteFile(p, pdf, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// topoCustom is the template plan for a device type: the type's own network map, else the one Termwright
// ships for its family (Huawei, Ruckus ICX, Eltex); nil when the type is read by the built-in parsers or not at all.
func (a *App) topoCustom(deviceType string) *topo.Custom {
	if deviceType != "" && a.terms.DeviceType != nil {
		if dt, ok := a.terms.DeviceType(deviceType); ok && dt.Map != nil && dt.Map.Set() {
			c := *dt.Map
			return &c
		}
	}
	if c, ok := topo.BuiltinCustom(topo.OSFor(deviceType)); ok {
		return &c
	}
	return nil
}

// TopoTestMap runs a network map template on pasted output, for the Device Types editor: the neighbours (or
// rows) it finds.
func (a *App) TopoTestMap(template, output string) ([]map[string]string, error) {
	t, err := textfsm.Parse(template)
	if err != nil {
		return nil, err
	}
	tab, err := t.Run(output)
	if err != nil {
		return nil, err
	}
	return tab.Maps(), nil
}

// TopoBuiltinMap is the template plan Termwright ships for a device type's name, to start a custom one from.
func (a *App) TopoBuiltinMap(deviceType string) *topo.Custom {
	if c, ok := topo.BuiltinCustom(topo.OSFor(deviceType)); ok {
		return &c
	}
	return nil
}
