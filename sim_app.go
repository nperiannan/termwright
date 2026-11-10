package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/sim"
)

// The simulated lab: switches simulated on this computer (each an SSH server
// on 127.0.0.1), their sessions in a Session Manager folder, kept in
// <data>/lab/lab.json. What the switches are - models, commands, outputs -
// comes from a device pack (.vm) the user loads and agrees to; Termwright
// carries none. The pack is kept in <data>/lab/packs/.

const labFolderDefault = "Simulated Lab"

type labFile struct {
	Specs    []sim.Spec        `json:"specs"`
	Links    []sim.Link        `json:"links"`
	MgmtNet  string            `json:"mgmtNet"`
	Folder   string            `json:"folder"`
	Sessions map[string]string `json:"sessions"` // switch id → session path
	Running  bool              `json:"running"`  // started again with Termwright
	Outputs  []sim.Output      `json:"outputs,omitempty"`
	Pack     *LabPackInfo      `json:"pack,omitempty"` // the pack agreed to
}

// LabPackInfo describes a device pack (for the consent dialog and the lab).
type LabPackInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Vendor      string   `json:"vendor"`
	Description string   `json:"description"`
	License     string   `json:"license"`
	Hash        string   `json:"hash"`
	OS          []string `json:"os"`
	Models      int      `json:"models"`
	File        string   `json:"file,omitempty"`     // in <data>/lab/packs
	Accepted    string   `json:"accepted,omitempty"` // when the user agreed (RFC 3339)
	Error       string   `json:"error,omitempty"`    // the kept pack could not be loaded
}

// LabModel is a pack's model for the wizard.
type LabModel struct {
	SKU    string            `json:"sku"`
	Family string            `json:"family"`
	OS     []string          `json:"os"`
	OSName map[string]string `json:"osName"`
	Ports  int               `json:"ports"`
	Layout string            `json:"layout"` // "16 2.5G multirate PoE, 8 1G copper"
}

var simLab struct {
	mu   sync.Mutex
	lab  *sim.Lab
	meta labFile
	key  ssh.Signer
	pack *sim.Pack
}

// LabRequest creates a lab or adds switches to it.
type LabRequest struct {
	Groups   []sim.Group `json:"groups"`
	MgmtNet  string      `json:"mgmtNet"`
	Cabling  string      `json:"cabling"`
	Password string      `json:"password"` // "" = any password
	Start    bool        `json:"start"`
}

// LabView is the lab as the Lab window shows it.
type LabView struct {
	Exists   bool              `json:"exists"`
	Running  bool              `json:"running"`
	Folder   string            `json:"folder"`
	MgmtNet  string            `json:"mgmtNet"`
	Devices  []sim.DeviceView  `json:"devices"`
	Links    []sim.LinkView    `json:"links"`
	Sessions map[string]string `json:"sessions"`
	Models   []LabModel        `json:"models"`
	Outputs  []sim.Output      `json:"outputs"`
	Pack     *LabPackInfo      `json:"pack"`
	Waiting  int               `json:"waiting"` // switches kept while their pack is not loaded
	OS       map[string]LabOS  `json:"os"`
}

// LabOS is an operating system of the pack.
type LabOS struct {
	Name      string `json:"name"`
	Short     string `json:"short"`
	ConfigCmd string `json:"configCommand"`
}

func (a *App) labDir() string { return filepath.Join(a.cfg.DataDir(), "lab") }

// labLoad reads the lab once (mu held).
func (a *App) labLoad() error {
	if simLab.lab != nil {
		return nil
	}
	if err := a.ready(); err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(a.labDir(), "lab.json"))
	if err == nil {
		if err := json.Unmarshal(b, &simLab.meta); err != nil {
			return fmt.Errorf("lab.json: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if simLab.meta.Sessions == nil {
		simLab.meta.Sessions = map[string]string{}
	}
	simLab.pack = nil
	if pi := simLab.meta.Pack; pi != nil {
		pi.Error = ""
		p, err := sim.LoadPackFile(filepath.Join(a.labDir(), "packs", pi.File))
		switch {
		case err != nil:
			pi.Error = err.Error()
		case p.Hash != pi.Hash:
			pi.Error = "the pack file changed since you agreed to it - load it again"
		default:
			simLab.pack = p
		}
	}
	simLab.lab = sim.NewLab(simLab.pack, simLab.meta.Specs, simLab.meta.Links)
	simLab.lab.SetOutputs(simLab.meta.Outputs)
	simLab.lab.OnSave(func() { simLab.mu.Lock(); _ = a.labSave(); simLab.mu.Unlock() })
	return nil
}

// labSave writes the lab (mu held).
func (a *App) labSave() error {
	if simLab.lab == nil {
		return nil
	}
	if simLab.pack != nil { // without its pack the lab is not loaded: keep what is written
		simLab.meta.Specs, simLab.meta.Links, simLab.meta.Outputs = simLab.lab.Specs(), simLab.lab.Links(), simLab.lab.Outputs()
	}
	if err := os.MkdirAll(a.labDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(simLab.meta, "", " ")
	tmp := filepath.Join(a.labDir(), "lab.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(a.labDir(), "lab.json"))
}

// labKey reads or makes the switches' SSH host key (mu held).
func (a *App) labKey() (ssh.Signer, error) {
	if simLab.key != nil {
		return simLab.key, nil
	}
	f := filepath.Join(a.labDir(), "hostkey.pem")
	if b, err := os.ReadFile(f); err == nil {
		if k, err := ssh.ParsePrivateKey(b); err == nil {
			simLab.key = k
			simLab.lab.SetKey(k)
			return k, nil
		}
	}
	pem, k, err := sim.NewHostKey()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(a.labDir(), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(f, pem, 0o600); err != nil {
		return nil, err
	}
	simLab.key = k
	simLab.lab.SetKey(k)
	return k, nil
}

// labTrust puts the switches' host key in Termwright's known hosts so the
// first login does not ask about it (mu held).
func (a *App) labTrust(ports []int) {
	k, err := a.labKey()
	if err != nil {
		return
	}
	f := filepath.Join(a.cfg.DataDir(), "known_hosts")
	have, _ := os.ReadFile(f)
	var add []string
	for _, p := range ports {
		line := knownhosts.Line([]string{knownhosts.Normalize("127.0.0.1:" + strconv.Itoa(p))}, k.PublicKey())
		if !strings.Contains(string(have), line) {
			add = append(add, line)
		}
	}
	if len(add) == 0 {
		return
	}
	fh, err := os.OpenFile(f, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer fh.Close()
	if len(have) > 0 && have[len(have)-1] != '\n' {
		_, _ = fh.WriteString("\n")
	}
	_, _ = fh.WriteString(strings.Join(add, "\n") + "\n")
}

func (a *App) labView() LabView {
	v := LabView{Folder: simLab.meta.Folder, MgmtNet: simLab.meta.MgmtNet, Sessions: simLab.meta.Sessions, Pack: simLab.meta.Pack}
	if p := simLab.pack; p != nil {
		v.OS = map[string]LabOS{}
		for id, o := range p.OS {
			short := o.Short
			if short == "" {
				short = strings.ToUpper(id)
			}
			v.OS[id] = LabOS{Name: o.Name, Short: short, ConfigCmd: o.ConfigCmd}
		}
		for _, m := range p.SortedModels() {
			lm := LabModel{SKU: m.SKU, Family: m.Family, OS: m.OS, OSName: map[string]string{}, Ports: p.PortCount(m), Layout: p.Describe(m)}
			for _, o := range m.OS {
				lm.OSName[o] = p.OS[o].Name
			}
			v.Models = append(v.Models, lm)
		}
	}
	if simLab.lab == nil {
		return v
	}
	if simLab.pack == nil {
		v.Waiting = len(simLab.meta.Specs)
	}
	v.Devices, v.Links = simLab.lab.View()
	v.Outputs = simLab.lab.Outputs()
	v.Exists = len(v.Devices) > 0
	v.Running = simLab.lab.Running()
	return v
}

var labEnsured bool

// LabState returns the lab (empty when there is none yet).
func (a *App) LabState() (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	if !labEnsured && simLab.pack != nil {
		labEnsured = true
		if a.labEnsureSessions() {
			_ = a.labSave()
		}
	}
	return a.labView(), nil
}

// LabAdd creates the lab or adds switches to it, makes their sessions and,
// when asked, starts them.
func (a *App) LabAdd(r LabRequest) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	if simLab.meta.Folder == "" {
		simLab.meta.Folder = labFolderDefault
	}
	if strings.TrimSpace(r.MgmtNet) == "" {
		r.MgmtNet = simLab.meta.MgmtNet
	}
	if r.MgmtNet == "" {
		r.MgmtNet = "10.99.0.0/24"
	}
	if simLab.pack == nil {
		return a.labView(), errors.New("load a device pack (.vm) first")
	}
	specs, links, err := sim.Plan(simLab.pack, r.Groups, r.MgmtNet, r.Cabling, r.Password, freePortFrom(22001), simLab.lab.Specs(), simLab.lab.Links())
	if err != nil {
		return a.labView(), err
	}
	if len(specs) == 0 {
		return a.labView(), errors.New("choose at least one switch")
	}
	placeNew(specs, simLab.lab.Specs())
	simLab.meta.MgmtNet = r.MgmtNet
	simLab.lab.Add(specs, links)
	var ports []int
	for _, s := range specs {
		ports = append(ports, s.Port)
		path, err := a.labSession(s, r.Password)
		if err != nil {
			return a.labView(), err
		}
		simLab.meta.Sessions[s.ID] = path
	}
	a.labTrust(ports)
	if r.Start {
		if _, err := a.labKey(); err != nil {
			return a.labView(), err
		}
		for _, s := range specs {
			if err := simLab.lab.StartDevice(s.ID); err != nil {
				_ = a.labSave()
				return a.labView(), err
			}
		}
		simLab.meta.Running = true
	}
	return a.labView(), a.labSave()
}

// labSession saves the session of a switch (a free name in the lab folder).
func (a *App) labSession(s sim.Spec, password string) (string, error) {
	dt := ""
	if o := simLab.pack.OS[s.OS]; o != nil {
		dt = o.DeviceType
	}
	name := s.Saved.Hostname
	for i := 2; ; i++ {
		if _, err := a.store.Get(session.JoinPath(simLab.meta.Folder, name)); err != nil {
			break
		}
		name = s.Saved.Hostname + " (" + strconv.Itoa(i) + ")"
	}
	ss := session.Session{Name: name, Folder: simLab.meta.Folder, Protocol: session.ProtoSSH2, Hostname: "127.0.0.1", Port: s.Port, Username: "admin", DeviceType: dt,
		Description: []string{fmt.Sprintf("Simulated %s (Termwright lab) - management %s", s.Model, s.Saved.MgmtIP)}}
	saved, err := a.store.Save(ss, "")
	if err != nil {
		return "", err
	}
	if password != "" {
		_ = a.SetSessionPassword(saved.Path, password, false)
		if cur, err := a.store.Get(saved.Path); err == nil && !cur.SavePassword {
			cur.SavePassword = true
			_, _ = a.store.Save(cur, saved.Path)
		}
	}
	return saved.Path, nil
}

// freePortFrom: the first port from p that nothing listens on.
func freePortFrom(p int) int {
	for ; p < 65000; p++ {
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err == nil {
			ln.Close()
			return p
		}
	}
	return 0
}

// placeNew gives new switches a place in the picture: the first two of a
// new lab (the cores of leaf-spine) in the top row, the rest below, after
// the switches there already.
func placeNew(add, have []sim.Spec) {
	rows := map[int]int{}
	for _, s := range have {
		if s.X+230 > rows[s.Y] {
			rows[s.Y] = s.X + 230
		}
	}
	for i := range add {
		y := 280
		if len(have) == 0 && i < 2 {
			y = 60
		}
		x := rows[y]
		if x == 0 {
			x = 40
		}
		add[i].X, add[i].Y = x, y
		rows[y] = x + 230
	}
}

// LabStart starts switches (none = all) and remembers that the lab runs.
func (a *App) LabStart(ids []string) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	if _, err := a.labKey(); err != nil {
		return a.labView(), err
	}
	if len(ids) == 0 {
		ids = simLab.lab.SortedIDs()
	}
	var errs []string
	for _, id := range ids {
		if err := simLab.lab.StartDevice(id); err != nil {
			errs = append(errs, err.Error())
		}
	}
	simLab.meta.Running = simLab.lab.Running()
	a.labEnsureSessions()
	_ = a.labSave()
	if len(errs) > 0 {
		return a.labView(), errors.New(strings.Join(errs, "; "))
	}
	return a.labView(), nil
}

// LabStop stops switches (none = all); what was not saved on them is lost.
func (a *App) LabStop(ids []string) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	if len(ids) == 0 {
		simLab.lab.Stop()
	} else {
		for _, id := range ids {
			simLab.lab.StopDevice(id)
		}
	}
	simLab.meta.Running = simLab.lab.Running()
	return a.labView(), a.labSave()
}

// LabRemove removes switches; their sessions too when sessions is true.
func (a *App) LabRemove(ids []string, sessions bool) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	simLab.lab.Remove(ids)
	for _, id := range ids {
		if p := simLab.meta.Sessions[id]; p != "" && sessions {
			_ = a.DeleteSession(p)
		}
		delete(simLab.meta.Sessions, id)
	}
	simLab.meta.Running = simLab.lab.Running()
	return a.labView(), a.labSave()
}

// LabConnect adds a cable.
func (a *App) LabConnect(k sim.Link) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	if err := simLab.lab.Connect(k); err != nil {
		return a.labView(), err
	}
	return a.labView(), a.labSave()
}

// LabDisconnect removes the cable on a port.
func (a *App) LabDisconnect(id, port string) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	simLab.lab.Disconnect(id, port)
	return a.labView(), a.labSave()
}

// LabPlace keeps where a switch is drawn.
func (a *App) LabPlace(id string, x, y int) error {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return err
	}
	simLab.lab.Place(id, x, y)
	return a.labSave()
}

// LabFreePorts lists a switch's ports without a cable.
func (a *App) LabFreePorts(id string) []string {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if a.labLoad() != nil {
		return nil
	}
	return simLab.lab.FreePorts(id)
}

// LabShow runs a show command on a switch (for the inspector).
func (a *App) LabShow(id, cmd string) string {
	simLab.mu.Lock()
	lab := simLab.lab
	simLab.mu.Unlock()
	if lab == nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(cmd)), "show ") {
		return ""
	}
	return lab.Show(id, cmd)
}

// labAutoStart starts the lab with Termwright when it ran when Termwright closed.
func (a *App) labAutoStart(_ context.Context) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if a.labLoad() != nil || !simLab.meta.Running {
		return
	}
	if _, err := a.labKey(); err != nil {
		return
	}
	a.labEnsureSessions()
	_ = a.labSave()
	for _, id := range simLab.lab.SortedIDs() {
		_ = simLab.lab.StartDevice(id)
	}
}

// labEnsureSessions makes the sessions of switches whose session is gone (a
// deleted folder, a lab from an older version) again (mu held). It says
// whether it made any.
func (a *App) labEnsureSessions() bool {
	if simLab.lab == nil || simLab.pack == nil {
		return false
	}
	made := false
	for _, sp := range simLab.lab.Specs() {
		if p := simLab.meta.Sessions[sp.ID]; p != "" {
			if _, err := a.store.Get(p); err == nil {
				continue
			}
		}
		if p, err := a.labSession(sp, ""); err == nil {
			simLab.meta.Sessions[sp.ID] = p
			made = true
		}
	}
	if made {
		var ports []int
		for _, sp := range simLab.lab.Specs() {
			ports = append(ports, sp.Port)
		}
		a.labTrust(ports)
	}
	return made
}

// LabNewSerials gives switches new serial numbers in their model's format
// from the device pack (all of them when ids is empty).
func (a *App) LabNewSerials(ids []string) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var none []string
	for _, sp := range simLab.lab.Specs() {
		if len(ids) > 0 && !want[sp.ID] {
			continue
		}
		m, ok := simLab.pack.ModelOf(sp.SKU)
		if !ok {
			m, ok = simLab.pack.ModelOf(sp.Model)
		}
		if !ok || m.Serial == "" {
			none = append(none, sp.Saved.Hostname)
			continue
		}
		simLab.lab.SetIdentity(sp.ID, sim.Generate(m.Serial), "")
	}
	if err := a.labSave(); err != nil {
		return a.labView(), err
	}
	if len(none) > 0 {
		return a.labView(), fmt.Errorf("the device pack has no serial number format for %s (their model is not in the pack)", strings.Join(none, ", "))
	}
	return a.labView(), nil
}

// labShutdown stops the switches (the lab starts again next time).
func (a *App) labShutdown() {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if simLab.lab != nil {
		simLab.lab.Stop()
	}
}

// LabSetIdentity changes a switch's serial number and MAC address.
func (a *App) LabSetIdentity(id, serial, mac string) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	if m := strings.TrimSpace(mac); m != "" {
		if _, err := net.ParseMAC(m); err != nil {
			return a.labView(), fmt.Errorf("%q is not a MAC address", m)
		}
	}
	if !simLab.lab.SetIdentity(id, serial, strings.ToLower(strings.TrimSpace(mac))) {
		return a.labView(), errors.New("no such switch")
	}
	return a.labView(), a.labSave()
}

// LabSetSerials gives switches serial numbers from a list, in order (empty lines are skipped).
func (a *App) LabSetSerials(ids []string, serials []string) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	var list []string
	for _, s := range serials {
		if s = strings.TrimSpace(s); s != "" {
			list = append(list, s)
		}
	}
	seen := map[string]bool{}
	for _, s := range list {
		if seen[strings.ToLower(s)] {
			return a.labView(), fmt.Errorf("serial number %s is in the list twice", s)
		}
		seen[strings.ToLower(s)] = true
	}
	for i, id := range ids {
		switch {
		case len(list) == 1:
			simLab.lab.SetIdentity(id, emu.NextSerial(list[0], i), "") // the first one given, the others count up
		case i < len(list):
			simLab.lab.SetIdentity(id, list[i], "")
		}
	}
	return a.labView(), a.labSave()
}

// LabSetMACs gives switches MAC addresses from start, a block of 64 each.
func (a *App) LabSetMACs(ids []string, start string) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	if emu.NextMAC(start, 0) == "" {
		return a.labView(), fmt.Errorf("%q is not a MAC address", start)
	}
	if len(ids) == 0 {
		ids = simLab.lab.SortedIDs()
	}
	for i, id := range ids {
		simLab.lab.SetIdentity(id, "", emu.NextMAC(start, i))
	}
	return a.labView(), a.labSave()
}

// LabSetOutputs keeps the outputs of show commands (pasted from real switches).
func (a *App) LabSetOutputs(o []sim.Output) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	var keep []sim.Output
	for _, x := range o {
		x.Command = strings.Join(strings.Fields(x.Command), " ")
		if !strings.HasPrefix(strings.ToLower(x.Command), "show ") || len(x.Text) > 200000 {
			continue
		}
		keep = append(keep, x)
	}
	simLab.lab.SetOutputs(keep)
	return a.labView(), a.labSave()
}

// LabPackInfo reads a device pack for the consent dialog (nothing is kept yet).
func (a *App) LabPackInfo(path string) (LabPackInfo, error) {
	p, err := sim.LoadPackFile(path)
	if err != nil {
		return LabPackInfo{}, err
	}
	return packInfo(p), nil
}

func packInfo(p *sim.Pack) LabPackInfo {
	return LabPackInfo{ID: p.ID, Name: p.Name, Version: p.Version, Vendor: p.Vendor, Description: p.Description, License: p.License, Hash: p.Hash, OS: p.OSIDs, Models: len(p.Models)}
}

// LabPackAccept keeps a device pack the user agreed to (hash = the one shown).
// A lab with switches can only take a newer version of its own pack.
func (a *App) LabPackAccept(path, hash string) (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return a.labView(), err
	}
	p, err := sim.LoadPackFile(path)
	if err != nil {
		return a.labView(), err
	}
	if p.Hash == "" {
		return a.labView(), errors.New("load the packed .vm file, not a folder")
	}
	if p.Hash != hash {
		return a.labView(), errors.New("the file changed after you looked at it - open it again")
	}
	if cur := simLab.meta.Pack; cur != nil && cur.ID != p.ID && a.labCount() > 0 {
		return a.labView(), fmt.Errorf("the lab's switches come from %s - remove them before loading another pack", cur.Name)
	}
	dir := filepath.Join(a.labDir(), "packs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return a.labView(), err
	}
	name := p.ID + "-" + strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(p.Version) + ".vm"
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
		return a.labView(), err
	}
	info := packInfo(p)
	info.File, info.Accepted = name, time.Now().UTC().Format(time.RFC3339)
	running := simLab.lab.Running()
	simLab.lab.Stop()
	if simLab.pack != nil {
		simLab.meta.Specs, simLab.meta.Links, simLab.meta.Outputs = simLab.lab.Specs(), simLab.lab.Links(), simLab.lab.Outputs()
	}
	simLab.meta.Pack = &info
	simLab.lab = nil
	if err := a.labSave2(); err != nil {
		return a.labView(), err
	}
	if err := a.labLoad(); err != nil {
		return a.labView(), err
	}
	if running {
		if _, err := a.labKey(); err == nil {
			for _, id := range simLab.lab.SortedIDs() {
				_ = simLab.lab.StartDevice(id)
			}
		}
	}
	return a.labView(), nil
}

// LabPackRemove forgets the device pack (only without switches).
func (a *App) LabPackRemove() (LabView, error) {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	if err := a.labLoad(); err != nil {
		return LabView{}, err
	}
	if a.labCount() > 0 {
		return a.labView(), errors.New("remove the switches first")
	}
	if pi := simLab.meta.Pack; pi != nil && pi.File != "" {
		_ = os.Remove(filepath.Join(a.labDir(), "packs", pi.File))
	}
	simLab.meta.Pack, simLab.pack = nil, nil
	simLab.lab = sim.NewLab(nil, nil, nil)
	return a.labView(), a.labSave()
}

// labSave2 writes the meta as it is (the lab is being reloaded; mu held).
func (a *App) labSave2() error {
	if err := os.MkdirAll(a.labDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(simLab.meta, "", " ")
	return os.WriteFile(filepath.Join(a.labDir(), "lab.json"), b, 0o600)
}

// labCount: the lab's switches, also those waiting for their pack (mu held).
func (a *App) labCount() int {
	if simLab.pack == nil {
		return len(simLab.meta.Specs)
	}
	return len(simLab.lab.Specs())
}
