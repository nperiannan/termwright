package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// The lab's change history: what Claude (the connector, the AI agent) and the API changed in the
// emulated lab, each change with the lab as it was before it. Lab changes are not asked (automated
// tests must not wait for a person); instead each one can be undone: Revert brings the lab back to
// how it was before a change - devices added since are removed, cables, CPU, computers' addresses,
// test ports and streams are set back, devices are started or stopped as they were. Commands typed on
// consoles are listed but not undone.

// EmuChange is one change, for the window and the tools.
type EmuChange struct {
	ID         string `json:"id"`
	At         string `json:"at"`
	Who        string `json:"who"`  // Claude, API
	Tool       string `json:"tool"` // the tool that made it (lab_cable, send_command…)
	What       string `json:"what"`
	Result     string `json:"result"`             // "ok", or the error
	Revertible bool   `json:"revertible"`         // the lab as it was before is kept
	Note       string `json:"note,omitempty"`     // what Revert cannot undo
	Reverted   string `json:"reverted,omitempty"` // when the lab was brought back to before it
}

// emuChangeRec is a change as kept on disk: with the lab before it and the devices that ran.
type emuChangeRec struct {
	EmuChange
	Before  *emu.Lab `json:"before,omitempty"`
	Running []string `json:"running,omitempty"`
}

// emuHistMax: how many changes are kept.
const emuHistMax = 200

var emuHist struct {
	mu     sync.Mutex
	loaded bool
	list   []emuChangeRec
}

func (a *App) emuHistPath() string { return filepath.Join(a.labDir(), "lab-history.json") }

// emuHistLoad reads the history once (emuHist.mu held).
func (a *App) emuHistLoad() {
	if emuHist.loaded {
		return
	}
	emuHist.loaded = true
	if b, err := os.ReadFile(a.emuHistPath()); err == nil {
		_ = json.Unmarshal(b, &emuHist.list)
	}
}

// emuHistSave writes it (emuHist.mu held).
func (a *App) emuHistSave() {
	if len(emuHist.list) > emuHistMax {
		emuHist.list = emuHist.list[len(emuHist.list)-emuHistMax:]
	}
	b, err := json.Marshal(emuHist.list)
	if err != nil {
		return
	}
	_ = os.MkdirAll(a.labDir(), 0o700)
	tmp := a.emuHistPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, a.emuHistPath())
	}
}

var emuAskPrefix = regexp.MustCompile(`^(Claude|The AI) \(through the Termwright connector\) asks to |^Claude asks to `)

// emuChangeWhat turns the text of an approval question into what was done.
func emuChangeWhat(title, msg string) string {
	w := strings.TrimSpace(emuAskPrefix.ReplaceAllString(strings.TrimSpace(msg), ""))
	if w == "" {
		w = title
	}
	if i := strings.Index(w, "\n"); i > 0 {
		w = strings.TrimSpace(w[:i])
	}
	w = strings.TrimSuffix(w, ".")
	if len(w) > 0 {
		w = strings.ToUpper(w[:1]) + w[1:]
	}
	return w
}

// emuSnapshot is the lab now (a copy) and the devices that run (emuLab.mu held).
func emuSnapshot() (*emu.Lab, []string) {
	if emuLab.f.Lab == nil {
		return nil, nil
	}
	b, err := json.Marshal(emuLab.f.Lab)
	if err != nil {
		return nil, nil
	}
	var l emu.Lab
	if json.Unmarshal(b, &l) != nil {
		return nil, nil
	}
	var run []string
	for id := range emuLab.status.Up {
		run = append(run, id)
	}
	slices.Sort(run)
	return &l, run
}

// emuWhoName is how the history names who made a change.
func emuWhoName(who string) string {
	switch who {
	case "api":
		return "API"
	case "ai":
		return "Claude"
	case "agent":
		return "AI agent"
	}
	return orStr(who, "Claude")
}

// emuChangeBegin starts a change record: the lab as it is before the change.
func (a *App) emuChangeBegin(who, tool, title, msg string) *emuChangeRec {
	emuLab.mu.Lock()
	a.emuLoad()
	before, run := emuSnapshot()
	emuLab.mu.Unlock()
	r := &emuChangeRec{EmuChange: EmuChange{ID: randHex(4), At: time.Now().Format(time.RFC3339), Who: emuWhoName(who), Tool: tool, What: emuChangeWhat(title, msg), Revertible: before != nil},
		Before: before, Running: run}
	if tool == "lab_console" {
		r.Note = "the commands typed on the console are not undone"
	}
	return r
}

// emuChangeEnd keeps a change once it ran.
func (a *App) emuChangeEnd(r *emuChangeRec, out string, err error) {
	r.Result = "ok"
	if err != nil {
		r.Result = err.Error()
	} else if s := strings.TrimSpace(out); s != "" && len(s) < 160 && !strings.Contains(s, "\n") {
		r.Result = s
	}
	emuHist.mu.Lock()
	a.emuHistLoad()
	emuHist.list = append(emuHist.list, *r)
	a.emuHistSave()
	emuHist.mu.Unlock()
}

// emuChangeNote records a change that cannot be undone (a command typed on a lab device's console).
func (a *App) emuChangeNote(who, tool, what, result, note string) {
	r := &emuChangeRec{EmuChange: EmuChange{ID: randHex(4), At: time.Now().Format(time.RFC3339), Who: emuWhoName(who), Tool: tool, What: what, Note: note}}
	var err error
	if result != "ok" {
		err = errors.New(result)
	}
	a.emuChangeEnd(r, "", err)
}

// EmuChanges is the history, newest first.
func (a *App) EmuChanges() []EmuChange {
	emuHist.mu.Lock()
	defer emuHist.mu.Unlock()
	a.emuHistLoad()
	out := make([]EmuChange, 0, len(emuHist.list))
	for i := len(emuHist.list) - 1; i >= 0; i-- {
		out = append(out, emuHist.list[i].EmuChange)
	}
	return out
}

// emuChangeByID finds a change (a copy).
func (a *App) emuChangeByID(id string) (emuChangeRec, error) {
	emuHist.mu.Lock()
	defer emuHist.mu.Unlock()
	a.emuHistLoad()
	for _, r := range emuHist.list {
		if r.ID == id {
			return r, nil
		}
	}
	return emuChangeRec{}, errors.New("no such change in the history")
}

// EmuRevertPlan says what Revert would do to bring the lab back to before a change.
func (a *App) EmuRevertPlan(id string) ([]string, error) {
	r, err := a.emuChangeByID(id)
	if err != nil {
		return nil, err
	}
	if r.Before == nil {
		return nil, errors.New("this change cannot be undone: " + orStr(r.Note, "the lab before it was not kept"))
	}
	return a.emuReconcile(r.Before, r.Running, false), nil
}

// EmuRevert brings the lab back to how it was before a change; it returns what it did.
func (a *App) EmuRevert(id string) ([]string, error) { return a.emuRevertAs(id, "You") }

// emuRevertAs reverts for someone (You, Claude, API: as the history shows it).
func (a *App) emuRevertAs(id, who string) ([]string, error) {
	defer emuAs("user")()
	r, err := a.emuChangeByID(id)
	if err != nil {
		return nil, err
	}
	if r.Before == nil {
		return nil, errors.New("this change cannot be undone: " + orStr(r.Note, "the lab before it was not kept"))
	}
	rec := a.emuChangeBegin("user", "revert", "", "Back to before: "+r.What)
	rec.Who = who
	done := a.emuReconcile(r.Before, r.Running, true)
	emuHist.mu.Lock()
	for i := range emuHist.list {
		if emuHist.list[i].ID == id {
			emuHist.list[i].Reverted = time.Now().Format(time.RFC3339)
		}
	}
	emuHist.mu.Unlock()
	a.emuChangeEnd(rec, strings.Join(done, "; "), nil)
	emuLab.mu.Lock()
	emuEv("", "info", fmt.Sprintf("reverted to before %q: %s", r.What, strings.Join(done, "; ")))
	emuLab.mu.Unlock()
	return done, nil
}

// emuReconcile makes the lab like before (apply) or says what that takes: devices added since are
// removed, cables, CPU, computers' addresses, test ports and streams are set back, and devices are
// started or stopped as they were. Devices removed since cannot come back (their disks are gone).
func (a *App) emuReconcile(before *emu.Lab, running []string, apply bool) []string {
	emuLab.mu.Lock()
	a.emuLoad()
	now, run := emuSnapshot()
	emuLab.mu.Unlock()
	if now == nil {
		return []string{"there is no lab now"}
	}
	var steps []string
	do := func(text string, f func() error) {
		if !apply {
			steps = append(steps, text)
			return
		}
		if err := f(); err != nil {
			steps = append(steps, text+" - failed: "+err.Error())
		} else {
			steps = append(steps, text)
		}
	}
	name := func(l *emu.Lab, id string) string {
		if v := l.VMByID(id); v != nil {
			return v.Name
		}
		return id
	}
	inBefore := func(id string) *emu.VM { return before.VMByID(id) }
	// devices added since: removed
	var extra []string
	for _, v := range now.VMs {
		if inBefore(v.ID) == nil {
			extra = append(extra, v.ID)
		}
	}
	if len(extra) > 0 {
		var names []string
		for _, id := range extra {
			names = append(names, name(now, id))
		}
		do("remove "+strings.Join(names, ", ")+" (added since)", func() error { _, err := a.EmuRemove(extra, true); return err })
	}
	for _, v := range before.VMs {
		if now.VMByID(v.ID) == nil {
			steps = append(steps, v.Name+" was removed since and cannot come back (its disk is gone)")
		}
	}
	// cables
	key := func(c emu.Cable) string { return fmt.Sprintf("%s:%d-%s:%d", c.A, c.APort, c.B, c.BPort) }
	had := map[string]bool{}
	for _, c := range before.Cables {
		had[key(c)] = true
	}
	has := map[string]bool{}
	for _, c := range now.Cables {
		has[key(c)] = true
		if !had[key(c)] && inBefore(c.A) != nil && inBefore(c.B) != nil {
			c := c
			do(fmt.Sprintf("unplug the cable %s port %d - %s port %d", name(now, c.A), c.APort, name(now, c.B), c.BPort), func() error { _, err := a.EmuDisconnect(c.A, strconv.Itoa(c.APort)); return err })
		}
	}
	for _, c := range before.Cables {
		if !has[key(c)] && now.VMByID(c.A) != nil && now.VMByID(c.B) != nil {
			c := c
			do(fmt.Sprintf("cable %s port %d - %s port %d again", name(now, c.A), c.APort, name(now, c.B), c.BPort), func() error {
				_, err := a.EmuConnect(EmuLinkView{A: c.A, APort: strconv.Itoa(c.APort), B: c.B, BPort: strconv.Itoa(c.BPort)})
				return err
			})
		}
	}
	// per device: CPU, a computer's network, power
	wasUp := map[string]bool{}
	for _, id := range running {
		wasUp[id] = true
	}
	isUp := map[string]bool{}
	for _, id := range run {
		isUp[id] = true
	}
	var start, stop []string
	for _, b := range before.VMs {
		v := now.VMByID(b.ID)
		if v == nil {
			continue
		}
		if b.CPUReqM() != v.CPUReqM() || b.CPULimM() != v.CPULimM() {
			b := b
			do(fmt.Sprintf("CPU of %s back to request %s, limit %s", b.Name, mOrNone(b.CPUReqM()), mOrNone(b.CPULimM())), func() error { _, err := a.EmuSetCPUReq(b.ID, b.CPUReqM(), b.CPULimM()); return err })
		}
		if !emuSameNet(b, *v) {
			b := b
			ports := map[int]bool{}
			for p := range b.Addrs {
				ports[p] = true
			}
			for p := range b.Addrs6 {
				ports[p] = true
			}
			for p := range v.Addrs {
				ports[p] = true
			}
			for p := range v.Addrs6 {
				ports[p] = true
			}
			do(fmt.Sprintf("addresses of %s back as they were", b.Name), func() error {
				for p := range ports {
					if _, err := a.EmuSetComputerNet(b.ID, EmuCompNet{Port: p, IPv4: b.Addrs[p], IPv6: b.Addrs6[p], Gw: b.DataGw, Gw6: b.DataGw6, DNS: b.DNS}); err != nil {
						return err
					}
				}
				return nil
			})
		}
		if wasUp[b.ID] && !isUp[b.ID] {
			start = append(start, b.ID)
		}
		if !wasUp[b.ID] && isUp[b.ID] {
			stop = append(stop, b.ID)
		}
	}
	// test ports and streams
	tp := map[string]emu.Tester{}
	for _, t := range now.Testers {
		tp[t.ID] = t
	}
	remap := map[string]string{}
	for _, t := range before.Testers {
		cur, ok := tp[t.ID]
		switch {
		case !ok && now.VMByID(t.VM) != nil:
			t := t
			do(fmt.Sprintf("test port %s on %s port %d again", t.Name, name(now, t.VM), t.Port), func() error {
				gw, ip := t.GW, t.IP
				if t.DHCP {
					ip = "dhcp"
				}
				v, err := a.EmuTesterAdd(t.VM, strconv.Itoa(t.Port), ip, gw, t.VLAN)
				if err != nil {
					return err
				}
				if n := len(v.Testers); n > 0 {
					nt := v.Testers[n-1]
					remap[t.ID] = nt.ID
					old := t
					old.ID, old.MAC = nt.ID, nt.MAC
					_, err = a.EmuTesterSet(old)
				}
				return err
			})
		case ok && !emuSameTester(t, cur):
			t := t
			do("test port "+t.Name+" back as it was", func() error { _, err := a.EmuTesterSet(t); return err })
		}
	}
	keep := map[string]bool{}
	for _, t := range before.Testers {
		keep[t.ID] = true
	}
	for _, t := range now.Testers {
		if !keep[t.ID] {
			t := t
			do("remove the test port "+t.Name+" (added since)", func() error { _, err := a.EmuTesterRemove(t.ID); return err })
		}
	}
	st := map[string]emu.Stream{}
	for _, s := range now.Streams {
		st[s.ID] = s
	}
	keepS := map[string]bool{}
	for _, s := range before.Streams {
		keepS[s.ID] = true
		cur, ok := st[s.ID]
		if ok && emuSameJSON(s, cur) {
			continue
		}
		s := s
		do("stream "+s.Name+" back as it was", func() error {
			if n, ok := remap[s.From]; ok {
				s.From = n
			}
			if n, ok := remap[s.To]; ok {
				s.To = n
			}
			if _, ok := st[s.ID]; !ok {
				s.ID = ""
			}
			_, err := a.EmuStreamSave(s)
			return err
		})
	}
	for _, s := range now.Streams {
		if !keepS[s.ID] {
			s := s
			do("remove the stream "+s.Name+" (added since)", func() error { _, err := a.EmuStreamRemove(s.ID); return err })
		}
	}
	// positions on the canvas: put back as they were
	if apply {
		emuLab.mu.Lock()
		if l := emuLab.f.Lab; l != nil {
			for i := range l.VMs {
				if b := before.VMByID(l.VMs[i].ID); b != nil {
					l.VMs[i].X, l.VMs[i].Y = b.X, b.Y
				}
			}
			_ = a.emuSave()
		}
		emuLab.mu.Unlock()
	}
	if len(stop) > 0 {
		do("stop "+names(now, stop), func() error { _, err := a.EmuStop(stop); return err })
	}
	if len(start) > 0 {
		do("start "+names(now, start), func() error { _, err := a.EmuStart(start); return err })
	}
	if len(steps) == 0 {
		steps = append(steps, "nothing to change: the lab is as it was")
	}
	return steps
}

func names(l *emu.Lab, ids []string) string {
	var out []string
	for _, id := range ids {
		if v := l.VMByID(id); v != nil {
			out = append(out, v.Name)
		}
	}
	return strings.Join(out, ", ")
}

func mOrNone(m int) string {
	if m == 0 {
		return "none"
	}
	return strconv.Itoa(m) + "m"
}

func emuSameJSON(x, y any) bool {
	a, _ := json.Marshal(x)
	b, _ := json.Marshal(y)
	return string(a) == string(b)
}

func emuSameNet(a, b emu.VM) bool {
	return emuSameJSON(a.Addrs, b.Addrs) && emuSameJSON(a.Addrs6, b.Addrs6) && a.DataGw == b.DataGw && a.DataGw6 == b.DataGw6 && a.DNS == b.DNS
}

// emuSameTester compares what a person sets on a test port (not its place on the canvas or what it learnt).
func emuSameTester(a, b emu.Tester) bool {
	a.X, a.Y, b.X, b.Y = 0, 0, 0, 0
	a.Lease, b.Lease, a.Lease6, b.Lease6 = "", "", "", ""
	return emuSameJSON(a, b)
}

// mcpLabPath says whether a session is a device of Termwright's own labs (the emulated or the simulated
// lab): Claude works on those without asking, and what it types is kept in the change history.
func (a *App) mcpLabPath(path string) bool {
	emuLab.mu.Lock()
	ef := orStr(emuLab.f.Folder, "Emulated Lab")
	emuLab.mu.Unlock()
	simLab.mu.Lock()
	sf := orStr(simLab.meta.Folder, labFolderDefault)
	simLab.mu.Unlock()
	in := func(f string) bool { return f != "" && (path == f || strings.HasPrefix(path, f+"/")) }
	return in(ef) || in(sf)
}

// emuPending: changes the AI panel began with its own lab commands (start, cable, LAG…), until they end.
var emuPending sync.Map

// EmuAgentChangeBegin keeps the lab as it is before the AI panel runs one of its own lab commands
// (they are not asked); EmuAgentChangeEnd puts the change in the history. It returns the change's id.
func (a *App) EmuAgentChangeBegin(what, verb string) string {
	r := a.emuChangeBegin("agent", "lab_"+verb, "", what)
	emuPending.Store(r.ID, r)
	return r.ID
}

// EmuAgentChangeEnd keeps a change the AI panel began ("ok", or what went wrong).
func (a *App) EmuAgentChangeEnd(id, result string) {
	v, ok := emuPending.LoadAndDelete(id)
	if !ok {
		return
	}
	var err error
	if result != "" && result != "ok" {
		err = errors.New(result)
	}
	a.emuChangeEnd(v.(*emuChangeRec), "", err)
}
