package main

import (
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"github.com/ulikunitz/xz"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/sim"
)

// The emulated lab: real network OS images (the vendor's VM images) booted
// with QEMU/KVM on a Linux host that Termwright reaches over SSH (package emu).
// Kept in <data>/lab/emu.json; image passwords in the keychain.

// EmuHost is a KVM host: an SSH session and what it offers.
type EmuHost struct {
	Session string       `json:"session"`
	Name    string       `json:"name"`
	Info    emu.HostInfo `json:"info"`
	Checked string       `json:"checked"`
}

type emuFile struct {
	Hosts     []EmuHost         `json:"hosts"`
	Images    []emu.Image       `json:"images"`
	Templates []emu.Template    `json:"templates"`
	Host      string            `json:"host"` // the lab's host (session path)
	Folder    string            `json:"folder"`
	Lab       *emu.Lab          `json:"lab"`
	Sessions  map[string]string `json:"sessions"`            // VM → its SSH session
	Consoles  map[string]string `json:"consoles"`            // VM → its console session
	Topology  string            `json:"topology,omitempty"`  // the name the open lab is saved under ("" = not saved yet)
	IconStyle string            `json:"iconStyle,omitempty"` // how devices are drawn ("" = boxes)
	SyncAll   bool              `json:"syncAll,omitempty"`   // copy new images to every host
	NoKeep20  bool              `json:"noKeep20,omitempty"`  // copy even when a host would keep less than a fifth of its disk free
	LogLevel  string            `json:"logLevel,omitempty"`
	Captures  []EmuCapture      `json:"captures,omitempty"` // packet captures, running and done (files on the hosts)  // the emulator log's level ("" = info)
}

// emuTopo is a saved topology: a lab with its switches, cables, management
// and sessions, opened again later (on the same hosts).
type emuTopo struct {
	Name     string            `json:"name"`
	Saved    string            `json:"saved"`
	Host     string            `json:"host"`
	Folder   string            `json:"folder"`
	Lab      *emu.Lab          `json:"lab"`
	Sessions map[string]string `json:"sessions"`
	Consoles map[string]string `json:"consoles"`
}

// EmuHostUse is one host of the lab: how its switches are managed and what they use.
type EmuHostUse struct {
	Host     string  `json:"host"` // session path
	Name     string  `json:"name"`
	Addr     string  `json:"addr"`
	Mgmt     string  `json:"mgmt"`
	MgmtNet  string  `json:"mgmtNet"`
	CloudGw  string  `json:"cloudGw"`
	LanFixed bool    `json:"lanFixed"`
	CPUs     int     `json:"cpus"` // the host's cores
	MemTotal int64   `json:"memTotal"`
	MemFree  int64   `json:"memFree"`
	Load     float64 `json:"load"`
	Cores    float64 `json:"cores"` // what its switches use now
	VCPUs    int     `json:"vcpus"` // given to its switches
	RAM      int     `json:"ram"`
	Switches int     `json:"switches"`
	Running  int     `json:"running"`
	Main     bool    `json:"main"`
}

// EmuTopoInfo is a saved topology in the list.
type EmuTopoInfo struct {
	Name     string   `json:"name"`
	Saved    string   `json:"saved"`
	Hosts    []string `json:"hosts"`
	Switches int      `json:"switches"`
	Open     bool     `json:"open"`
	Running  int      `json:"running"` // its switches running now (also when it is not open)
	RunOn    []string `json:"runOn,omitempty"`
}

type emuConn struct {
	c     *ssh.Client
	close func()
}

var emuLab struct {
	hostStatus       map[string]emu.Status // each host's last status
	mu               sync.Mutex
	f                emuFile
	loaded           bool
	conns            map[string]*emuConn
	status           emu.Status
	statusAt         time.Time
	polling          bool
	state            map[string]string // VM → what it does now (uploading, day-0 …) when not just booting/running
	errs             map[string]string // VM → why it failed
	day0             map[string]bool   // day-0 runs now
	upload           map[string]string // image → upload progress ("41 %")
	starting         map[string]time.Time
	ticks            map[string]float64 // CPU ticks at the last poll
	ticksAt          time.Time
	cpu              map[string]float64   // CPU use now, in cores (1 = one core busy)
	ready            map[string]bool      // the console showed the ready text since the VM started (it may scroll out of the log's end later)
	bootAns          map[string]time.Time // when a boot menu was last answered
	trafHosts        map[string]bool      // hosts traffic.py runs on (or ran on: told to end)
	addrOK           map[string]bool      // the switch showed its management address (since it started)
	moveTo           map[string]string    // switches moving to another host: that host
	personaAfterBoot map[string]bool      // the persona file is written once the VM has booted (its folder did not exist yet)
	perr             map[string]string    // why the persona file could not be written
	packProfiles     []emu.Image          // the device pack's image profiles (refreshed by EmuState)
	personaTried     map[string]bool      // that was tried once already (no second round)
	pinfo            map[string]string    // where the persona file went, or what the check after boot saw
	pcheck           map[string]bool      // the persona was checked on the CLI since the VM started
	resumed          map[string]bool      // it started from its saved running state (no boot, no day 0)
	lldp             map[string]string    // cable → what the last cabling check found (LLDP)
	events           []EmuEvent           // the Events log, newest last
	download         map[string]string    // image → download progress ("waiting", "41 %")
	dlQueue          []string             // images waiting to download, in order
	dlCur            string               // the image downloading now
	dlCancel         context.CancelFunc   // stops it
	dlRunning        bool
}

// EmuDevice is a VM as the Lab window shows it (the fields of a simulated switch, and more).
type EmuDevice struct {
	Host         string       `json:"host"` // the KVM host it runs on (session path)
	ID           string       `json:"id"`
	OS           string       `json:"os"` // device type
	Model        string       `json:"model"`
	Image        string       `json:"image"`
	Version      string       `json:"version"`  // the software it runs: as it reports it, else the image's
	HWSerial     string       `json:"hwSerial"` // the serial number it reports
	Serial       string       `json:"serial"`
	MAC          string       `json:"mac"`
	Hostname     string       `json:"hostname"`
	MgmtIP       string       `json:"mgmtIp"`
	Port         int          `json:"port"`
	Ports        int          `json:"ports"`
	Running      bool         `json:"running"`
	Logins       int          `json:"logins"`
	Unsaved      bool         `json:"unsaved"`
	X            int          `json:"x"`
	Y            int          `json:"y"`
	State        string       `json:"state"` // stopped, uploading, booting, day0, running, failed
	StateText    string       `json:"stateText"`
	CPU          float64      `json:"cpu"`                 // % of its vCPUs, now
	MemUsed      int64        `json:"memUsed"`             // MB its VM uses now (resident)
	Saved        bool         `json:"saved,omitempty"`     // stopped with its running state saved: it starts in seconds
	Role         string       `json:"role,omitempty"`      // drawn as (set by the user, for its kind of device)
	IconStyle    string       `json:"iconStyle,omitempty"` // its kind's icon style in this topology ("" = the default)
	Uplinks      []string     `json:"uplinks,omitempty"`   // its data ports on the host's network
	MovingTo     string       `json:"movingTo,omitempty"`  // the host it is moving to (session path), while it moves
	Cores        float64      `json:"cores"`               // host cores it uses now
	CPUs         int          `json:"cpus"`
	RAM          int          `json:"ram"`
	Console      int          `json:"console"`
	SSH          string       `json:"ssh"` // how its management is reached, as text
	Error        string       `json:"error"`
	DiskGB       int          `json:"diskGb"`
	Template     string       `json:"template"`
	Persona      *emu.Persona `json:"persona"`
	PersonaOK    bool         `json:"personaOk"` // the persona file in its disk is the current one
	PersonaError string       `json:"personaError"`
	PersonaInfo  string       `json:"personaInfo"` // where the file was written, what the CLI showed after boot
}

// EmuLinkView is a cable, with port names.
type EmuLinkView struct {
	A     string      `json:"a"`
	APort string      `json:"aPort"`
	B     string      `json:"b"`
	BPort string      `json:"bPort"`
	Up    bool        `json:"up"`
	Down  bool        `json:"down"`
	LagA  *EmuLagView `json:"lagA,omitempty"` // the LAG its port on A is in
	LagB  *EmuLagView `json:"lagB,omitempty"`
	Lldp  string      `json:"lldp,omitempty"` // the last cabling check: "ok …", "wrong: …" or "none: …"
}

// EmuLagView is a LAG at one end of a cable.
type EmuLagView struct {
	Name   string `json:"name"`
	Mode   string `json:"mode"`
	Source string `json:"source"`
}

// EmuImageView is an image with its state on the lab's host.
type EmuImageView struct {
	emu.Image
	HasPassword bool   `json:"hasPassword"`
	OnHost      bool   `json:"onHost"`
	Upload      string `json:"upload"`
	Download    string `json:"download"` // download to this computer: "41 %"…
	Missing     bool   `json:"missing"`  // the file is not on this computer (any more)
}

// EmuView is the emulated lab for the Lab window (shaped like LabView).
// EmuEvent is something Termwright did to a switch, or saw it do (the Events log).
type EmuEvent struct {
	At    time.Time `json:"at"`
	VM    string    `json:"vm"`
	Name  string    `json:"name"`
	Level string    `json:"level"` // "info", "ok", "warn", "error"
	Text  string    `json:"text"`
	Host  string    `json:"host,omitempty"` // the host it happened on (when it happened: a switch may move later)
	By    string    `json:"by,omitempty"`   // who caused it: user, ai, system
}

// emuEv adds to the Events log (emuLab.mu held).
func emuEv(id, level, text string) { emuEvOn("", id, level, text) }

// emuEvOn adds to the Events log for a host ("" = the switch's own host).
func emuEvOn(host, id, level, text string) {
	name := orStr(id, "images")
	if l := emuLab.f.Lab; l != nil {
		if v := l.VMByID(id); v != nil {
			name = v.Name
			if host == "" {
				host = emuHostOf(*v)
			}
		}
	}
	e := EmuEvent{At: time.Now(), VM: id, Name: name, Level: level, Text: text, Host: host, By: emuWho()}
	emuLogf(emuLevel(level), host, name, "%s", text)
	emuLab.events = append(emuLab.events, e)
	if n := len(emuLab.events); n > 400 {
		emuLab.events = append([]EmuEvent(nil), emuLab.events[n-300:]...)
	}
	emuEvKeep(e)
}

// emuEventsFile keeps the Events log on disk (one JSON line each), so it is
// still there after Termwright starts again ("" = not kept, before the lab is loaded).
var emuEventsFile string

func emuEvKeep(e EmuEvent) {
	if emuEventsFile == "" {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(emuEventsFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
	_ = f.Close()
}

// emuEventsLoad reads the kept Events log (its last 300), and shortens the file when it has grown long.
func emuEventsLoad(path string) []EmuEvent {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var out []EmuEvent
	for _, ln := range lines {
		var e EmuEvent
		if json.Unmarshal([]byte(ln), &e) == nil && !e.At.IsZero() {
			out = append(out, e)
		}
	}
	if len(out) > 300 {
		out = out[len(out)-300:]
	}
	if len(lines) > 1000 {
		var w strings.Builder
		for _, e := range out {
			if j, err := json.Marshal(e); err == nil {
				w.Write(append(j, '\n'))
			}
		}
		_ = os.WriteFile(path, []byte(w.String()), 0o600)
	}
	return out
}

// emuBootAnswer types a boot menu's answer on a switch's console (its boot stopped there).
func (a *App) emuBootAnswer(host string, v emu.VM, ans emu.Answer) {
	emuLab.mu.Lock()
	port := a.emuFor(host).ConsolePort(v)
	emuLab.mu.Unlock()
	say := func(level, text string) {
		emuLab.mu.Lock()
		emuEv(v.ID, level, text)
		emuLab.mu.Unlock()
	}
	cl, err := a.emuClient(context.Background(), host)
	if err != nil {
		say("warn", fmt.Sprintf("its boot stopped at %q: type %s on its console", ans.Match, orStr(ans.Send, "Enter")))
		return
	}
	conn, err := cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		say("warn", fmt.Sprintf("its boot stopped at %q: type %s in its console (it is open elsewhere)", ans.Match, orStr(ans.Send, "Enter")))
		return
	}
	c := emu.NewConsole(conn)
	defer c.Close()
	time.Sleep(300 * time.Millisecond)
	typed := orStr(ans.Send, "Enter")
	if ans.Key && ans.Send != "" {
		// one key: an Enter after it would stay in the buffer and open the next
		// stop of the boot (EXOS: its BusyBox shell); Enter only if the menu waits for a line
		n := len(c.Text())
		if err := c.Key(ans.Send); err != nil {
			say("warn", "boot menu: "+err.Error())
			return
		}
		time.Sleep(3 * time.Second)
		if after := c.Text()[min(n, len(c.Text())):]; len(strings.TrimSpace(after)) <= len(ans.Send)+4 { // only its echo: the menu wants Enter
			if err := c.Send(""); err != nil {
				say("warn", "boot menu: "+err.Error())
				return
			}
			typed += " and Enter"
		} else {
			typed += " (one key, no Enter)"
		}
	} else if err := c.Send(ans.Send); err != nil {
		say("warn", "boot menu: "+err.Error())
		return
	}
	time.Sleep(500 * time.Millisecond)
	say("info", fmt.Sprintf("its boot stopped at %q - typed %s so it goes on booting", ans.Match, typed))
}

func imOrNil(im *emu.Image) *emu.Image {
	if im == nil {
		return &emu.Image{}
	}
	return im
}

// EmuConsoleLog is everything a switch's console showed since it started (from
// its log on the host), with what Termwright did to it: to keep, or to send
// for a boot or login that went wrong.
func (a *App) EmuConsoleLog(id string) (string, error) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return "", errors.New("no such switch")
	}
	vv, host := *v, emuHostOf(*v)
	dir := a.emuFor(host).VMDir()
	im := emuImage(vv.Image)
	var ev []string
	for _, e := range emuLab.events {
		if e.VM == id {
			ev = append(ev, e.At.Format("2006-01-02 15:04:05")+"  "+strings.ToUpper(e.Level)+"  "+e.Text)
		}
	}
	emuLab.mu.Unlock()
	out, err := a.emuRun(host, "tail -c 4000000 "+emu.Q(dir+"/"+id+".log")+" 2>/dev/null", 2*time.Minute)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Termwright emulated lab - console of %s\nHost: %s\nImage: ", vv.Name, host)
	if im != nil {
		fmt.Fprintf(&b, "%s (%s)", im.Name, im.FileName)
	}
	fmt.Fprintf(&b, "\nSaved: %s\n\n===== Events =====\n%s\n\n===== Console =====\n%s\n", time.Now().Format(time.RFC1123), strings.Join(ev, "\n"), emu.CleanConsole(out))
	return b.String(), nil
}

// EmuDay0Again types a running switch's day 0 (its address, persona check) again,
// after it failed: its disk and configuration stay.
func (a *App) EmuDay0Again(id string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	if emuLab.f.Lab == nil || emuLab.f.Lab.VMByID(id) == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such switch")
	}
	delete(emuLab.day0, id)
	delete(emuLab.errs, id)
	delete(emuLab.pcheck, id)
	emuEv(id, "info", "typing day 0 again")
	emuLab.statusAt = time.Time{}
	emuLab.mu.Unlock()
	return a.EmuState()
}

// emuEvent adds to the Events log.
func emuEvent(id, level, text string) {
	emuLab.mu.Lock()
	emuEv(id, level, text)
	emuLab.mu.Unlock()
}

type EmuView struct {
	Kind          string            `json:"kind"` // "emu"
	Events        []EmuEvent        `json:"events"`
	DownloadDir   string            `json:"downloadDir"`   // where image downloads go on this computer
	DownloadQueue []string          `json:"downloadQueue"` // the image downloading now, then the waiting ones in order
	Exists        bool              `json:"exists"`
	Running       bool              `json:"running"`
	Folder        string            `json:"folder"`
	MgmtNet       string            `json:"mgmtNet"`
	Topology      string            `json:"topology,omitempty"` // the name the open lab is saved under
	CloudGw       string            `json:"cloudGw,omitempty"`  // cloud: the gateway the switches use
	LanFixed      bool              `json:"lanFixed,omitempty"` // lan: fixed addresses (no DHCP)
	Mgmt          string            `json:"mgmt"`
	Devices       []EmuDevice       `json:"devices"`
	Links         []EmuLinkView     `json:"links"`
	Sessions      map[string]string `json:"sessions"`
	Consoles      map[string]string `json:"consoles"`
	Models        []LabModel        `json:"models"`
	OS            map[string]LabOS  `json:"os"`
	Host          string            `json:"host"`
	HostInfo      *emu.HostInfo     `json:"hostInfo"`
	Hosts         []EmuHost         `json:"hosts"`
	Images        []EmuImageView    `json:"images"`
	KVM           bool              `json:"kvm"`
	Templates     []emu.Template    `json:"templates"`
	Usage         EmuUsage          `json:"usage"`
	HostUse       []EmuHostUse      `json:"hostUse"`
	Fast          bool              `json:"fast"`      // Fast start: Stop saves the switches' running state
	IconStyle     string            `json:"iconStyle"` // how devices are drawn by default // every host of the lab: its management and what it gives the switches
	DeviceTypes   []EmuDeviceType   `json:"deviceTypes"`
}

// EmuUsage is what the lab takes of its host now (stopped switches take nothing).
type EmuUsage struct {
	HostCPUs     int     `json:"hostCpus"`
	HostMemTotal int64   `json:"hostMemTotal"` // MB
	HostMemFree  int64   `json:"hostMemFree"`  // MB, available now
	Load         float64 `json:"load"`
	Running      int     `json:"running"`
	VCPUs        int     `json:"vcpus"` // of the running switches
	RAM          int     `json:"ram"`   // MB, of the running switches
	Cores        float64 `json:"cores"` // host cores they use now
	DiskGB       int     `json:"diskGb"`
	Saved        int     `json:"saved"` // MB, about what the saved running states take on the hosts' disks
}

// EmuGroup asks for Count VMs of an image.
type EmuGroup struct {
	Template    string `json:"template"` // a template (its image, resources, persona, serial and MAC start), or:
	Model       string `json:"model"`    // a persona model of the image ("" = the image as it is)
	Module      string `json:"module"`
	SerialStart string `json:"serialStart"`
	MACStart    string `json:"macStart"`
	Image       string `json:"image"`
	Count       int    `json:"count"`
	Names       string `json:"names"`
	CPUs        int    `json:"cpus"`
	RAM         int    `json:"ram"`
	DiskGB      int    `json:"diskGb"`
}

// EmuRequest makes the emulated lab (or adds VMs to it).
type EmuRequest struct {
	Host    string     `json:"host"` // session path
	Groups  []EmuGroup `json:"groups"`
	Cabling string     `json:"cabling"`
	Mgmt    string     `json:"mgmt"` // auto, forward, bridge
	MgmtNet string     `json:"mgmtNet"`
	Start   bool       `json:"start"`
}

func (a *App) emuFilePath() string { return filepath.Join(a.labDir(), "emu.json") }

// emuLoad reads the file once (lock held).
func (a *App) emuLoad() {
	if emuLab.loaded {
		return
	}
	emuLab.loaded = true
	defer a.emuLogSetup()
	emuLab.conns, emuLab.state, emuLab.errs, emuLab.day0, emuLab.upload, emuLab.starting = map[string]*emuConn{}, map[string]string{}, map[string]string{}, map[string]bool{}, map[string]string{}, map[string]time.Time{}
	emuLab.ticks, emuLab.cpu, emuLab.ready, emuLab.personaAfterBoot = map[string]float64{}, map[string]float64{}, map[string]bool{}, map[string]bool{}
	emuLab.personaTried, emuLab.perr, emuLab.bootAns, emuLab.addrOK, emuLab.moveTo = map[string]bool{}, map[string]string{}, map[string]time.Time{}, map[string]bool{}, map[string]string{}
	emuLab.trafHosts = map[string]bool{}
	emuLab.pinfo, emuLab.pcheck, emuLab.resumed, emuLab.lldp = map[string]string{}, map[string]bool{}, map[string]bool{}, map[string]string{}
	emuLab.download = map[string]string{}
	if b, err := os.ReadFile(a.emuFilePath()); err == nil {
		_ = json.Unmarshal(b, &emuLab.f)
	}
	if emuLab.f.Folder == "" {
		emuLab.f.Folder = "Emulated Lab"
	}
	_ = os.MkdirAll(a.labDir(), 0o700)
	emuEventsFile = filepath.Join(a.labDir(), "events.jsonl")
	emuLab.events = append(emuEventsLoad(emuEventsFile), emuLab.events...)
}

// emuSave writes the file (lock held).
func (a *App) emuSave() error {
	_ = os.MkdirAll(a.labDir(), 0o700)
	if emuLab.f.Topology != "" { // the open topology is kept as it changes
		_ = a.emuWriteTopo()
	}
	f := emuLab.f
	f.Images = append([]emu.Image(nil), f.Images...)
	for i := range f.Images {
		f.Images[i].Password = ""
	}
	b, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return err
	}
	tmp := a.emuFilePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.emuFilePath())
}

func emuImageKey(id string) string { return "emu-image:" + id }

func emuImage(id string) *emu.Image {
	for i := range emuLab.f.Images {
		if emuLab.f.Images[i].ID == id {
			return &emuLab.f.Images[i]
		}
	}
	return nil
}

func emuImages() map[string]*emu.Image {
	m := map[string]*emu.Image{}
	for i := range emuLab.f.Images {
		im := &emuLab.f.Images[i]
		m[im.ID] = im
	}
	return m
}

// emuDial logs in to a host (tests replace it).
var emuDial = func(a *App, ctx context.Context, s session.Session) (*ssh.Client, func(), error) {
	return a.terms.Client(ctx, "emu-"+randHex(4), s)
}

// emuClient is the SSH connection to a host (kept open, made again when it broke).
func (a *App) emuClient(ctx context.Context, host string) (*ssh.Client, error) {
	emuLab.mu.Lock()
	c := emuLab.conns[host]
	emuLab.mu.Unlock()
	if c != nil {
		if _, _, err := c.c.SendRequest("keepalive@termwright", true, nil); err == nil {
			return c.c, nil
		}
		c.close()
	}
	s, err := a.store.Get(host)
	if err != nil {
		return nil, fmt.Errorf("KVM host session %q: %w", host, err)
	}
	s = a.withFolderDefaults(s)
	if s.Protocol != session.ProtoSSH2 {
		return nil, errors.New("the KVM host must be an SSH session")
	}
	cl, closeFn, err := emuDial(a, ctx, s)
	if err != nil {
		return nil, fmt.Errorf("could not log in to %s: %w", s.Name, err)
	}
	emuLab.mu.Lock()
	emuLab.conns[host] = &emuConn{c: cl, close: closeFn}
	emuLab.mu.Unlock()
	return cl, nil
}

// emuRun runs a shell script on a host and returns what it printed.
func (a *App) emuRun(host, script string, timeout time.Duration) (out string, err error) {
	if emuLogOn(emuLvDebug) {
		t0 := time.Now()
		defer func() {
			first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(script), "\n", 2)[0])
			if len(first) > 160 {
				first = first[:160] + "…"
			}
			res := "ok"
			if err != nil {
				res = "failed: " + err.Error()
			}
			emuLogf(emuLvDebug, host, "", "ran %q (%d lines) in %.1fs: %s", first, strings.Count(script, "\n")+1, time.Since(t0).Seconds(), res)
			if emuLogOn(emuLvVerbose) {
				red, _ := aiRedact(script, false)
				ro, _ := aiRedact(out, false)
				emuLogf(emuLvVerbose, host, "", "script:\n%s\noutput:\n%s", red, ro)
			}
		}()
	}
	return a.emuRun0(host, script, timeout)
}

func (a *App) emuRun0(host, script string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for try := 0; ; try++ {
		cl, err := a.emuClient(ctx, host)
		if err != nil {
			return "", err
		}
		s, err := cl.NewSession()
		if err != nil {
			emuLab.mu.Lock()
			delete(emuLab.conns, host)
			emuLab.mu.Unlock()
			if try == 0 {
				continue
			}
			return "", err
		}
		s.Stdin = strings.NewReader(script)
		done := make(chan struct{})
		var out []byte
		emuGo(func() { out, err = s.CombinedOutput("sh -s"); close(done) })
		select {
		case <-done:
		case <-ctx.Done():
			_ = s.Close()
			return string(out), errors.New("the host did not answer in time")
		}
		_ = s.Close()
		var ee *ssh.ExitError
		if err != nil && !errors.As(err, &ee) {
			return string(out), err
		}
		return string(out), nil
	}
}

// EmuCheckHost checks a KVM host (an SSH session) and keeps it in the list.
func (a *App) EmuCheckHost(sessionPath string) (EmuHost, error) {
	defer emuAs("user")()
	if err := a.ready(); err != nil {
		return EmuHost{}, err
	}
	emuLab.mu.Lock()
	a.emuLoad()
	dir := "termwright-lab"
	if l := emuLab.f.Lab; l != nil && emuLab.f.Host == sessionPath {
		dir = l.Dir
	}
	emuLab.mu.Unlock()
	out, err := a.emuRun(sessionPath, emu.ProbeScript(dir), 60*time.Second)
	if err != nil {
		return EmuHost{}, err
	}
	name := sessionPath
	if s, err := a.store.Get(sessionPath); err == nil {
		name = s.Name
	}
	h := EmuHost{Session: sessionPath, Name: name, Info: emu.ParseProbe(out, dir), Checked: time.Now().Format(time.RFC3339)}
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	found := false
	for i := range emuLab.f.Hosts {
		if emuLab.f.Hosts[i].Session == sessionPath {
			emuLab.f.Hosts[i], found = h, true
		}
	}
	if !found {
		emuLab.f.Hosts = append(emuLab.f.Hosts, h)
	}
	return h, a.emuSave()
}

// EmuRemoveHost forgets a host (not the lab's own).
func (a *App) EmuRemoveHost(sessionPath string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	if emuLab.f.Host == sessionPath && emuLab.f.Lab != nil && len(emuLab.f.Lab.VMs) > 0 {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("the lab runs on this host: remove its switches first")
	}
	var keep []EmuHost
	for _, h := range emuLab.f.Hosts {
		if h.Session != sessionPath {
			keep = append(keep, h)
		}
	}
	emuLab.f.Hosts = keep
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// fileSHA256 hashes a file.
func fileSHA256(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// EmuAddImage adds a VM image file of this computer (with a profile to edit).
func (a *App) EmuAddImage(p string) (EmuView, error) {
	defer emuAs("user")()
	sum, size, err := fileSHA256(p)
	if err != nil {
		return EmuView{}, err
	}
	base := filepath.Base(p)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	im := emu.Image{ID: randHex(4), Name: name, File: p, FileName: base, Size: size, SHA256: sum, DeviceType: guessDeviceType(name), CPUs: 1, RAM: 2048, NICs: 8, NIC: "e1000", Disk: "ide", Ready: "login:", Login: "admin",
		Version: versionIn(base)}
	if pr, ver := a.emuRecognise(base); pr != nil {
		keep := im
		im = *pr
		im.ID, im.File, im.FileName, im.Size, im.SHA256, im.Match = keep.ID, keep.File, keep.FileName, keep.Size, keep.SHA256, ""
		im.Version = orStr(ver, keep.Version)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.f.Images = append(emuLab.f.Images, im)
	err = a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// guessDeviceType names a built-in device type from an image name ("vyos-1.4" → VYOS).
func guessDeviceType(name string) string {
	low := strings.ToLower(name)
	for _, t := range []string{"exos", "voss", "boss", "nxos", "nx-os", "eos", "junos", "vyos", "iosxe", "ios", "linux"} {
		if strings.Contains(low, t) {
			return strings.ToUpper(strings.ReplaceAll(t, "-", ""))
		}
	}
	return ""
}

// EmuImportAppliance reads a GNS3 appliance file: one image per version it
// lists, with its file when that lies next to the appliance file.
func (a *App) EmuImportAppliance(p string) (EmuView, error) {
	defer emuAs("user")()
	b, err := os.ReadFile(p)
	if err != nil {
		return EmuView{}, err
	}
	return a.emuImportAppliance(b, filepath.Dir(p))
}

// EmuCatalogItem is a public appliance of the GNS3 registry Termwright offers.
type EmuCatalogItem struct {
	File  string `json:"file"` // its name in the registry (without .gns3a)
	Name  string `json:"name"`
	Group string `json:"group"`
	Note  string `json:"note"`
	Added bool   `json:"added"` // images of it are in the list already
	Count int    `json:"count"` // how many images of it are in the list
	json  string // a built-in appliance (not in the GNS3 registry)
}

// emuSonicVS: SONiC's virtual switch, which the GNS3 registry has no file
// for; its builds come from the SONiC build server (gzip, unpacked on download).
const emuSonicVS = `{"name": "SONiC-VS", "category": "multilayer_switch", "registry_version": 3,
 "default_username": "admin", "default_password": "YourPaSsWoRd",
 "usage": "Login admin / YourPaSsWoRd. Data ports are Ethernet0, Ethernet4, Ethernet8…",
 "port_name_format": "Ethernet{n*4-4}",
 "qemu": {"adapter_type": "e1000", "adapters": 10, "ram": 4096, "cpus": 2, "hda_disk_interface": "virtio", "arch": "x86_64"},
 "images": [
  {"filename": "sonic-vs-master.img", "version": "master", "direct_download_url": "https://sonic-build.azurewebsites.net/api/sonic/artifacts?branchName=master&platform=vs&target=target/sonic-vs.img.gz", "download_url": "https://sonic-build.azurewebsites.net/ui/sonic/pipelines"},
  {"filename": "sonic-vs-202411.img", "version": "202411", "direct_download_url": "https://sonic-build.azurewebsites.net/api/sonic/artifacts?branchName=202411&platform=vs&target=target/sonic-vs.img.gz", "download_url": "https://sonic-build.azurewebsites.net/ui/sonic/pipelines"}],
 "versions": [{"name": "202411", "images": {"hda_disk_image": "sonic-vs-202411.img"}}, {"name": "master", "images": {"hda_disk_image": "sonic-vs-master.img"}}]}`

// emuCatalog: QEMU appliances of the public GNS3 registry (disks, CD-ROMs
// and BIOS files; container appliances cannot run on a KVM host).
var emuCatalog = []EmuCatalogItem{
	{File: "sonic-vs", Name: "SONiC-VS", Group: "Data-centre switches", Note: "Open source data-centre switch OS (virtual switch build). Free download from the SONiC build server; login admin / YourPaSsWoRd.", json: emuSonicVS},
	{File: "vyos", Name: "VyOS", Group: "Routers and firewalls", Note: "Open source router. Stable images need a (free) account at the project's site; rolling builds are free."},
	{File: "frr", Name: "FRR (FRRouting)", Group: "Routers and firewalls", Note: "Open source routing suite on Alpine Linux. Free download."},
	{File: "bird", Name: "BIRD", Group: "Routers and firewalls", Note: "Open source routing daemon. Free download."},
	{File: "openwrt", Name: "OpenWrt", Group: "Routers and firewalls", Note: "Open source router OS. Free download."},
	{File: "mikrotik-chr", Name: "MikroTik Cloud Hosted Router", Group: "Routers and firewalls", Note: "Free download (speed limited without a licence)."},
	{File: "opnsense", Name: "OPNsense", Group: "Routers and firewalls", Note: "Open source firewall."},
	{File: "ipfire", Name: "IPFire", Group: "Routers and firewalls", Note: "Open source firewall. Free download."},
	{File: "cumulus-vx", Name: "Cumulus VX", Group: "Data-centre switches", Note: "Linux switch OS. Free download."},
	{File: "exos", Name: "EXOS VM", Group: "Data-centre switches", Note: "Switch OS VM. Free download."},
	{File: "extreme-networks-voss", Name: "VOSS VM", Group: "Data-centre switches", Note: "Switch OS VM. Free download."},
	{File: "arista-veos", Name: "Arista vEOS", Group: "Data-centre switches", Note: "Boots from a small Aboot disk with the EOS disk as the second one. Both files from the vendor with your account."},
	{File: "cisco-iosv", Name: "Cisco IOSv", Group: "Vendor images (sign-in needed)", Note: "The router image from the vendor with your account; the small start-up disk downloads freely."},
	{File: "cisco-nxosv9k", Name: "Cisco Nexus 9000v", Group: "Vendor images (sign-in needed)", Note: "The switch image from the vendor with your account; the UEFI BIOS file downloads freely. Needs 8 GB of memory."},
	{File: "fortigate", Name: "FortiGate VM", Group: "Vendor images (sign-in needed)", Note: "The firewall image from the vendor with your account; the empty log disk downloads freely."},
	{File: "cisco-iosvl2", Name: "Cisco IOSvL2", Group: "Vendor images (sign-in needed)", Note: "Get the image from the vendor with your account, then choose the file."},
	{File: "cisco-csr1000v", Name: "Cisco CSR 1000v", Group: "Vendor images (sign-in needed)", Note: "Get the image from the vendor with your account, then choose the file."},
	{File: "juniper-vsrx", Name: "Juniper vSRX", Group: "Vendor images (sign-in needed)", Note: "Get the image from the vendor with your account, then choose the file."},
	{File: "alpine-linux-virt", Name: "Alpine Linux", Group: "Linux and test tools", Note: "Small Linux for hosts and servers. Free download."},
	{File: "debian", Name: "Debian", Group: "Linux and test tools", Note: "Linux. Free download."},
	{File: "ubuntu-cloud", Name: "Ubuntu Cloud", Group: "Linux and test tools", Note: "Ubuntu server with a cloud-init CD-ROM (user ubuntu). Free download."},
	{File: "fedora-cloud", Name: "Fedora Cloud", Group: "Linux and test tools", Note: "Fedora server with a cloud-init CD-ROM. Free download."},
	{File: "almalinux", Name: "AlmaLinux", Group: "Linux and test tools", Note: "Enterprise Linux with a cloud-init CD-ROM. Free download."},
	{File: "tinycore-linux", Name: "Tiny Core Linux", Group: "Linux and test tools", Note: "Very small Linux for end hosts. Free download."},
	{File: "kali-linux", Name: "Kali Linux", Group: "Linux and test tools", Note: "Security testing Linux. Free download."},
	{File: "ostinato", Name: "Ostinato", Group: "Linux and test tools", Note: "Traffic generator."},
}

// EmuCatalog lists the public appliances (and which are in the image list).
func (a *App) EmuCatalog() []EmuCatalogItem {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	a.emuLoad()
	out := append([]EmuCatalogItem(nil), emuCatalog...)
	for i := range out {
		for _, im := range emuLab.f.Images {
			if strings.EqualFold(im.Source, "GNS3 registry "+out[i].File) {
				out[i].Added = true
				out[i].Count++
			}
		}
	}
	return out
}

// EmuAddFromCatalog fetches a public appliance from the GNS3 registry and adds its versions.
func (a *App) EmuAddFromCatalog(file string) (EmuView, error) {
	defer emuAs("user")()
	var item *EmuCatalogItem
	for i := range emuCatalog {
		if emuCatalog[i].File == file {
			item = &emuCatalog[i]
		}
	}
	if item == nil {
		return EmuView{}, errors.New("not in the list: " + file)
	}
	if item.json != "" {
		return a.emuImportAppliance([]byte(item.json), "", "GNS3 registry "+file)
	}
	resp, err := http.Get("https://raw.githubusercontent.com/GNS3/gns3-registry/master/appliances/" + file + ".gns3a")
	if err != nil {
		return EmuView{}, fmt.Errorf("the GNS3 registry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return EmuView{}, fmt.Errorf("the GNS3 registry answered %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return EmuView{}, err
	}
	return a.emuImportAppliance(b, "", "GNS3 registry "+file)
}

// EmuRemoveFromCatalog takes the images added from a public appliance out of
// the list (refused while switches use one), stops their downloads and, with
// files, deletes the files Termwright downloaded for them (not ones chosen elsewhere).
func (a *App) EmuRemoveFromCatalog(file string, files bool) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	src := "GNS3 registry " + file
	drop := map[string]bool{}
	for _, im := range emuLab.f.Images {
		if strings.EqualFold(im.Source, src) {
			drop[im.ID] = true
		}
	}
	if l := emuLab.f.Lab; l != nil {
		for _, v := range l.VMs {
			if drop[v.Image] {
				emuLab.mu.Unlock()
				return EmuView{}, fmt.Errorf("switch %s uses one of these images: remove it first", v.Name)
			}
		}
	}
	for _, t := range emuLab.f.Templates {
		if drop[t.Image] {
			emuLab.mu.Unlock()
			return EmuView{}, fmt.Errorf("template %s uses one of these images: remove it first", t.Name)
		}
	}
	for id := range drop {
		if emuLab.dlCur == id && emuLab.dlCancel != nil {
			emuLab.dlCancel()
		}
		emuLab.dlQueue = slices.DeleteFunc(emuLab.dlQueue, func(x string) bool { return x == id })
		delete(emuLab.download, id)
	}
	var keep []emu.Image
	var gone []string
	for _, im := range emuLab.f.Images {
		if !drop[im.ID] {
			keep = append(keep, im)
			continue
		}
		_ = secrets.Delete(emuImageKey(im.ID))
		gone = append(gone, im.File)
		for _, d := range im.Disks {
			gone = append(gone, d.File)
		}
	}
	emuLab.f.Images = keep
	used := map[string]bool{}
	for _, im := range keep {
		used[im.File] = true
		for _, d := range im.Disks {
			used[d.File] = true
		}
	}
	dl := a.emuDownloadDir() + string(filepath.Separator)
	deleted := 0
	if files {
		for _, f := range gone {
			if f != "" && !used[f] && strings.HasPrefix(f, dl) {
				if os.Remove(f) == nil {
					deleted++
				}
			}
		}
	}
	emuEv("", "info", fmt.Sprintf("%s: %d images removed, %d downloaded files deleted", file, len(drop), deleted))
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// emuImportAppliance adds an appliance's versions as images (dir: where its
// image files may lie; source: what the images say they came from).
func (a *App) emuImportAppliance(b []byte, dir string, source ...string) (EmuView, error) {
	ap, err := emu.ParseAppliance(b)
	if err != nil {
		return EmuView{}, err
	}
	var add []emu.Image
	for _, pr := range ap.Profiles {
		pr.ID = randHex(4)
		pr.DeviceType = guessDeviceType(pr.Name)
		if known, ver := a.emuRecognise(pr.FileName); known != nil && pr.FileName != "" {
			// the device pack knows this image: its profile, with the appliance's file and download
			keep := pr
			pr = *known
			pr.ID, pr.FileName, pr.Size, pr.URL, pr.MD5, pr.Match = keep.ID, keep.FileName, keep.Size, keep.URL, keep.MD5, ""
			pr.Version = orStr(keep.Version, ver)
		}
		if len(source) > 0 {
			pr.Source = source[0]
		}
		if pr.Login == "" && ap.User != "" {
			pr.Login = ap.User
		}
		if ap.Password != "" {
			_ = secrets.Set(emuImageKey(pr.ID), ap.Password)
		}
		for i := range pr.Disks {
			for _, d := range []string{dir, a.emuDownloadDir()} {
				if d == "" {
					continue
				}
				if sum, size, err := fileSHA256(filepath.Join(d, pr.Disks[i].FileName)); err == nil {
					pr.Disks[i].File, pr.Disks[i].SHA256, pr.Disks[i].Size = filepath.Join(d, pr.Disks[i].FileName), sum, size
					break
				}
			}
		}
		if pr.FileName != "" {
			for _, d := range []string{dir, a.emuDownloadDir()} {
				if d == "" {
					continue
				}
				f := filepath.Join(d, pr.FileName)
				if sum, size, err := fileSHA256(f); err == nil {
					pr.File, pr.SHA256, pr.Size = f, sum, size
					break
				}
			}
		}
		add = append(add, pr)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.f.Images = append(emuLab.f.Images, add...)
	err = a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuClearEvents empties the Events log.
func (a *App) EmuClearEvents() (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	emuLab.events = nil
	if emuEventsFile != "" {
		_ = os.WriteFile(emuEventsFile, nil, 0o600)
	}
	emuLab.mu.Unlock()
	return a.EmuState()
}

// emuDownloadDir is where downloaded images are kept on this computer.
func (a *App) emuDownloadDir() string { return filepath.Join(a.labDir(), "images") }

// EmuDownload downloads the files of images that have a download link (from a
// GNS3 appliance) to this computer, one after the other, and checks their MD5
// sums; each becomes the image's file. Progress shows on the images.
func (a *App) EmuDownload(ids []string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	for _, id := range ids {
		if im := emuImage(id); im != nil && emuLab.download[id] == "" && emuNeedsDownload(im) {
			emuLab.dlQueue = append(emuLab.dlQueue, id)
			emuLab.download[id] = "waiting"
		}
	}
	start := !emuLab.dlRunning && len(emuLab.dlQueue) > 0
	if start {
		emuLab.dlRunning = true
	}
	emuLab.mu.Unlock()
	if start {
		emuGo(func() { a.emuDownloadWorker() })
	}
	return a.EmuState()
}

// emuDownloadWorker downloads the queue, one image after the other.
func (a *App) emuDownloadWorker() {
	n := 0
	for {
		emuLab.mu.Lock()
		if len(emuLab.dlQueue) == 0 {
			emuLab.dlRunning, emuLab.dlCur, emuLab.dlCancel = false, "", nil
			emuLab.mu.Unlock()
			if n > 0 {
				a.emuNotify(nil, "Downloads finished")
			}
			return
		}
		id := emuLab.dlQueue[0]
		emuLab.dlQueue = emuLab.dlQueue[1:]
		im := emuImage(id)
		if im == nil {
			delete(emuLab.download, id)
			emuLab.mu.Unlock()
			continue
		}
		cp := *im
		ctx, cancel := context.WithCancel(context.Background())
		emuLab.dlCur, emuLab.dlCancel = id, cancel
		emuLab.download[id] = "0 %"
		emuLab.mu.Unlock()
		err := a.emuDownloadOne(ctx, cp)
		stopped := ctx.Err() != nil
		cancel()
		n++
		emuLab.mu.Lock()
		delete(emuLab.download, id)
		emuLab.dlCur, emuLab.dlCancel = "", nil
		switch {
		case stopped:
			emuEv("", "warn", cp.Name+" "+cp.Version+": download stopped")
		case err != nil:
			emuEv("", "error", cp.Name+" "+cp.Version+": download failed: "+err.Error())
		default:
			emuEv("", "ok", cp.Name+" "+cp.Version+": downloaded and checked ("+cp.FileName+")")
			emuGo(func() { a.emuSyncAfter(cp.ID) })
		}
		emuLab.mu.Unlock()
		if !stopped {
			a.emuNotify(err, "")
		}
	}
}

// EmuDownloadStop stops an image's download, or takes it out of the queue.
func (a *App) EmuDownloadStop(id string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	if emuLab.dlCur == id && emuLab.dlCancel != nil {
		emuLab.dlCancel()
	} else {
		emuLab.dlQueue = slices.DeleteFunc(emuLab.dlQueue, func(x string) bool { return x == id })
		delete(emuLab.download, id)
	}
	emuLab.mu.Unlock()
	return a.EmuState()
}

// EmuDownloadMove moves a waiting download: "first", "up", "down" or "last".
func (a *App) EmuDownloadMove(id, where string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	q := emuLab.dlQueue
	i := slices.Index(q, id)
	if i >= 0 {
		q = slices.Delete(q, i, i+1)
		j := i
		switch where {
		case "first":
			j = 0
		case "up":
			j = max(0, i-1)
		case "down":
			j = min(len(q), i+1)
		case "last":
			j = len(q)
		}
		emuLab.dlQueue = slices.Insert(q, j, id)
	}
	emuLab.mu.Unlock()
	return a.EmuState()
}

// emuNeedsDownload: a file of the image is missing and has a download link.
func emuNeedsDownload(im *emu.Image) bool {
	if im.File == "" && im.URL != "" {
		return true
	}
	for _, d := range im.Disks {
		if d.File == "" && d.URL != "" {
			return true
		}
	}
	return false
}

// emuDownloadOne downloads the missing files of an image (its disk, then the others).
func (a *App) emuDownloadOne(ctx context.Context, im emu.Image) error {
	if im.File == "" && im.URL != "" {
		f, sum, size, err := a.emuFetch(ctx, im.ID, "", im.URL, im.FileName, im.MD5, im.Size)
		if err != nil {
			return err
		}
		emuLab.mu.Lock()
		if x := emuImage(im.ID); x != nil {
			x.File, x.SHA256, x.Size = f, sum, size
			_ = a.emuSave()
		}
		emuLab.mu.Unlock()
	}
	for i, d := range im.Disks {
		if d.File != "" || d.URL == "" {
			continue
		}
		f, sum, size, err := a.emuFetch(ctx, im.ID, d.Slot, d.URL, d.FileName, d.MD5, d.Size)
		if err != nil {
			return fmt.Errorf("%s (%s): %w", emuDiskName(d.Slot), d.FileName, err)
		}
		emuLab.mu.Lock()
		if x := emuImage(im.ID); x != nil && i < len(x.Disks) {
			x.Disks[i].File, x.Disks[i].SHA256, x.Disks[i].Size = f, sum, size
			_ = a.emuSave()
		}
		emuLab.mu.Unlock()
	}
	return nil
}

// emuFetch downloads a file to the download folder and checks its MD5 (a
// .zip is unpacked: the file of that name inside it).
func (a *App) emuFetch(ctx context.Context, id, slot, url, fileName, md5sum string, size int64) (string, string, int64, error) {
	dir := a.emuDownloadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", 0, err
	}
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return "", "", 0, errors.New("not a web link: " + url)
	}
	dst := filepath.Join(dir, filepath.Base(fileName))
	if sum, sz, err := fileSHA256(dst); err == nil && md5sum != "" && fileMD5(dst) == strings.ToLower(md5sum) {
		return dst, sum, sz, nil // downloaded before
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", "", 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", 0, fmt.Errorf("the server answered %s", resp.Status)
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return "", "", 0, errors.New("the link gave a web page, not the file: open the download page and choose the file")
	}
	zipped := strings.Contains(strings.ToLower(url), ".zip")
	packed := emuPacking(url, fileName)
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return "", "", 0, err
	}
	total := size
	if resp.ContentLength > 0 {
		total = resp.ContentLength
	}
	label := ""
	if slot != "" {
		label = emuDiskName(slot) + " "
	}
	pw := &progressWriter{w: f, total: total, fn: func(pct int) {
		emuLab.mu.Lock()
		emuLab.download[id] = label + strconv.Itoa(pct) + " %"
		emuLab.mu.Unlock()
	}}
	_, err = io.Copy(pw, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(part)
		return "", "", 0, err
	}
	if packed != "" {
		emuLab.mu.Lock()
		emuLab.download[id] = label + "100 % unpacking"
		emuLab.mu.Unlock()
		if err := unpackTo(packed, part, dst+".unz"); err != nil {
			_ = os.Remove(part)
			return "", "", 0, err
		}
		_ = os.Remove(part)
		part = dst + ".unz"
	}
	if zipped {
		if err := unzipOne(part, filepath.Base(fileName), dst+".unz"); err != nil {
			_ = os.Remove(part)
			return "", "", 0, err
		}
		_ = os.Remove(part)
		part = dst + ".unz"
	}
	if sum := fileMD5(part); md5sum != "" && !strings.EqualFold(sum, md5sum) {
		_ = os.Remove(part)
		return "", "", 0, fmt.Errorf("MD5 %s, the appliance says %s - the file was not kept", sum, md5sum)
	}
	if err := os.Rename(part, dst); err != nil {
		return "", "", 0, err
	}
	sum, sz, err := fileSHA256(dst)
	return dst, sum, sz, err
}

func fileMD5(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// emuPacking says how a download is packed ("gz", "xz", "bz2" or ""),
// from the link, when the wanted file is not itself packed that way.
func emuPacking(url, fileName string) string {
	u, f := strings.ToLower(url), strings.ToLower(fileName)
	for _, k := range []string{"gz", "xz", "bz2"} {
		if strings.Contains(u, "."+k) && !strings.HasSuffix(f, "."+k) {
			return k
		}
	}
	return ""
}

// unpackTo unpacks a gzip, xz or bzip2 file.
func unpackTo(kind, src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	var zr io.Reader
	switch kind {
	case "xz":
		zr, err = xz.NewReader(bufio.NewReaderSize(f, 1<<20))
	case "bz2":
		zr = bzip2.NewReader(bufio.NewReaderSize(f, 1<<20))
	default:
		zr, err = gzip.NewReader(f)
	}
	if err != nil {
		return fmt.Errorf("not a %s file: %w", kind, err)
	}
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, io.LimitReader(zr, 64<<30)); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// unzipOne writes the member called name (or the only file) of a zip archive to dst.
func unzipOne(zipFile, name, dst string) error {
	zr, err := zip.OpenReader(zipFile)
	if err != nil {
		return fmt.Errorf("not a zip archive: %w", err)
	}
	defer zr.Close()
	var pick *zip.File
	for _, f := range zr.File {
		if filepath.Base(f.Name) == name || (pick == nil && !f.FileInfo().IsDir() && len(zr.File) == 1) {
			pick = f
		}
	}
	if pick == nil {
		return errors.New(name + " is not in the archive")
	}
	r, err := pick.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, io.LimitReader(r, 64<<30)); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// EmuSetDiskFile gives an image's other disk (hdb…, cdrom, bios) its file.
func (a *App) EmuSetDiskFile(id, slot, p string) (EmuView, error) {
	defer emuAs("user")()
	sum, size, err := fileSHA256(p)
	if err != nil {
		return EmuView{}, err
	}
	emuLab.mu.Lock()
	a.emuLoad()
	im := emuImage(id)
	found := false
	if im != nil {
		for i := range im.Disks {
			if im.Disks[i].Slot == slot {
				im.Disks[i].File, im.Disks[i].SHA256, im.Disks[i].Size, found = p, sum, size, true
				if im.Disks[i].FileName == "" {
					im.Disks[i].FileName = filepath.Base(p)
				}
			}
		}
	}
	if !found {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such disk")
	}
	err = a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuSetImageFile gives an image (from an appliance) its file.
func (a *App) EmuSetImageFile(id, p string) (EmuView, error) {
	defer emuAs("user")()
	sum, size, err := fileSHA256(p)
	if err != nil {
		return EmuView{}, err
	}
	emuLab.mu.Lock()
	a.emuLoad()
	im := emuImage(id)
	if im == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such image")
	}
	im.File, im.SHA256, im.Size = p, sum, size
	if im.FileName == "" {
		im.FileName = filepath.Base(p)
	}
	err = a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuSaveImage saves an image's profile; its password goes to the keychain
// ("" keeps the saved one, clearPassword removes it).
func (a *App) EmuSaveImage(im emu.Image, clearPassword bool) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	cur := emuImage(im.ID)
	if cur == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such image")
	}
	if im.Password != "" {
		_ = secrets.Set(emuImageKey(im.ID), im.Password)
	} else if clearPassword {
		_ = secrets.Delete(emuImageKey(im.ID))
	}
	im.Password = ""
	im.File, im.FileName, im.Size, im.SHA256 = cur.File, cur.FileName, cur.Size, cur.SHA256 // the file stays
	if im.NICs < 1 {
		im.NICs = 1
	}
	*cur = im
	err := a.emuSave()
	var users []emu.VM
	if l := emuLab.f.Lab; l != nil {
		for _, v := range l.VMs {
			if v.Image == im.ID {
				users = append(users, v)
			}
		}
	}
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	for _, v := range users {
		a.emuLogins(v) // the switches' sessions take the new login
	}
	return a.EmuState()
}

// EmuRemoveImage forgets an image that no switch uses (its copy on the hosts stays).
func (a *App) EmuRemoveImage(id string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	if l := emuLab.f.Lab; l != nil {
		for _, v := range l.VMs {
			if v.Image == id {
				emuLab.mu.Unlock()
				return EmuView{}, errors.New("switches of the lab use this image: remove them first")
			}
		}
	}
	var keep []emu.Image
	for _, im := range emuLab.f.Images {
		if im.ID != id {
			keep = append(keep, im)
		}
	}
	emuLab.f.Images = keep
	_ = secrets.Delete(emuImageKey(id))
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// emuUpload copies an image to a host once (a .sha256 file next to it says it is there).
func (a *App) emuUpload(host string, im emu.Image, dir string) error {
	if err := a.emuUploadFile(host, im, im.FileName, im.File, im.SHA256, im.Size, dir, ""); err != nil {
		return err
	}
	for _, d := range im.Disks {
		if err := a.emuUploadFile(host, im, d.FileName, d.File, d.SHA256, d.Size, dir, d.Slot); err != nil {
			return err
		}
	}
	return nil
}

// emuDiskName says what a disk slot is.
func emuDiskName(slot string) string {
	switch slot {
	case "cdrom":
		return "CD-ROM"
	case "bios":
		return "BIOS"
	case "":
		return "disk"
	}
	return "disk " + strings.TrimPrefix(slot, "hd")
}

func (a *App) emuUploadFile(host string, im emu.Image, fileName, file, sha string, size int64, dir, slot string) error {
	remote := dir + "/images/" + fileName
	// one copy of a file to a host at a time: a second start waits for the first copy, then finds the file there
	mu, _ := emuCopyLocks.LoadOrStore(host+"|"+remote, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	// the same file on the host already: its checksum (written when it was copied), else its size when the checksum is not known here
	out, _ := a.emuRun(host, "cat "+emu.Q(remote+".sha256")+" 2>/dev/null; echo; stat -c 'size=%s' "+emu.Q(remote)+" 2>/dev/null", 30*time.Second)
	there := regexp.MustCompile(`\b[0-9a-f]{64}\b`).FindString(out)
	m := regexp.MustCompile(`size=(\d+)`).FindStringSubmatch(out)
	// the file itself must be there too (with its size, when known): a checksum file alone is not an image
	sized := m != nil && (size <= 0 || m[1] == strconv.FormatInt(size, 10))
	if sized && (sha != "" && there == sha || sha == "" && size > 0) {
		return nil
	}
	why := "it is not on the host yet"
	if m == nil && there != "" {
		why = "only its checksum file is on the host, the image itself is missing"
	} else if m != nil {
		why = "the file on the host is a different one"
	}
	emuLab.mu.Lock()
	emuEvOn(host, "", "info", fmt.Sprintf("copying %s to %s: %s", fileName, host[strings.LastIndex(host, "/")+1:], why))
	emuLab.mu.Unlock()
	if file == "" {
		if slot != "" {
			return fmt.Errorf("the %s file of the image %s (%s) is not chosen yet: Images → Download or Choose", emuDiskName(slot), im.Name, fileName)
		}
		return fmt.Errorf("the file of the image %s is not chosen yet", im.Name)
	}
	im.File, im.SHA256, im.Size = file, sha, size
	src, err := os.Open(im.File)
	if err != nil {
		return fmt.Errorf("image %s: the file on this computer could not be opened: %w", im.Name, err)
	}
	defer src.Close()
	if st, err := src.Stat(); err == nil && im.Size <= 0 {
		im.Size = st.Size()
	}
	tmp := remote + ".part"
	// room on the host first: a full disk is the most common reason a copy fails half way
	if out, err := a.emuRun(host, "mkdir -p "+emu.Q(dir+"/images")+" && df -Pk "+emu.Q(dir+"/images")+" | tail -1; stat -c 'part=%s' "+emu.Q(tmp)+" 2>/dev/null", 30*time.Second); err == nil {
		have := int64(0)
		if m := regexp.MustCompile(`part=(\d+)`).FindStringSubmatch(out); m != nil {
			have, _ = strconv.ParseInt(m[1], 10, 64)
		}
		if f := strings.Fields(strings.SplitN(out, "\n", 2)[0]); len(f) >= 4 {
			if kb, err := strconv.ParseInt(f[3], 10, 64); err == nil && im.Size > 0 && kb*1024 < im.Size-have+64<<20 {
				return fmt.Errorf("copying %s to the host: not enough disk space in %s (%s free, %s needed); free some space on the host or set another lab folder for it in Hosts", im.Name, dir, emuBytes(kb*1024), emuBytes(im.Size-have))
			}
		}
	}
	var lastErr error
	for try := 1; try <= 4; try++ {
		if try > 1 {
			emuLab.mu.Lock()
			emuEvOn(host, "", "warn", fmt.Sprintf("copying %s to %s: %s; trying again (%d of 4), going on where it stopped", fileName, host[strings.LastIndex(host, "/")+1:], emuCopyWhy(lastErr), try))
			emuLab.mu.Unlock()
			time.Sleep(time.Duration(try*3) * time.Second)
		}
		lastErr = a.emuCopyOnce(host, im, src, tmp, dir)
		if lastErr == nil {
			break
		}
		if !emuCopyRetry(lastErr) {
			break
		}
	}
	emuLab.mu.Lock()
	delete(emuLab.upload, im.ID)
	emuLab.mu.Unlock()
	if lastErr != nil {
		return fmt.Errorf("copying %s to the host failed: %s (%v)", im.Name, emuCopyWhy(lastErr), lastErr)
	}
	cl, err := a.emuClient(context.Background(), host)
	if err != nil {
		return err
	}
	sc, err := sftp.NewClient(cl)
	if err != nil {
		return fmt.Errorf("SFTP to the host: %w", err)
	}
	defer sc.Close()
	if fi, err := sc.Stat(tmp); err == nil && im.Size > 0 && fi.Size() != im.Size {
		_ = sc.Remove(tmp)
		return fmt.Errorf("copying %s to the host failed: the copy on the host has %s, not %s; it was removed, Start again to copy it anew", im.Name, emuBytes(fi.Size()), emuBytes(im.Size))
	}
	if im.SHA256 != "" { // the copy checked on the host, so a damaged one is never used
		out, err := a.emuRun(host, "sha256sum "+emu.Q(tmp)+" 2>/dev/null || shasum -a 256 "+emu.Q(tmp), 10*time.Minute)
		if got := regexp.MustCompile(`\b[0-9a-f]{64}\b`).FindString(out); err == nil && got != "" && got != im.SHA256 {
			_ = sc.Remove(tmp)
			return fmt.Errorf("copying %s to the host failed: the copy on the host is damaged (its checksum differs); it was removed, Start again to copy it anew", im.Name)
		}
	}
	_ = sc.Remove(remote + ".sha256") // first: never a checksum file without its image
	_ = sc.Remove(remote)
	_ = sc.Remove(remote + emu.BootedSuffix) // the booted copy of the old file
	if err := sc.Rename(tmp, remote); err != nil {
		return err
	}
	if f, err := sc.Create(remote + ".sha256"); err == nil {
		_, _ = f.Write([]byte(im.SHA256 + "\n"))
		_ = f.Close()
	}
	return nil
}

var emuCopyLocks sync.Map

// emuCopyOnce copies src to tmp on the host, going on from what an earlier try left there.
func (a *App) emuCopyOnce(host string, im emu.Image, src *os.File, tmp, dir string) error {
	cl, err := a.emuClient(context.Background(), host)
	if err != nil {
		return err
	}
	sc, err := sftp.NewClient(cl, sftp.UseConcurrentWrites(true))
	if err != nil {
		return fmt.Errorf("SFTP to the host: %w", err)
	}
	defer sc.Close()
	_ = sc.MkdirAll(dir + "/images")
	off := int64(0)
	if fi, err := sc.Stat(tmp); err == nil && fi.Size() < im.Size {
		off = fi.Size() &^ (1<<20 - 1) // whole MiB, so a half written block is written again
	}
	dst, err := sc.OpenFile(tmp, os.O_WRONLY|os.O_CREATE)
	if err != nil {
		return fmt.Errorf("could not write %s on the host: %w", tmp, err)
	}
	if off == 0 {
		_ = dst.Truncate(0)
	}
	if _, err := dst.Seek(off, io.SeekStart); err != nil {
		off = 0
		_ = dst.Truncate(0)
		_, _ = dst.Seek(0, io.SeekStart)
	}
	if _, err := src.Seek(off, io.SeekStart); err != nil {
		_ = dst.Close()
		return err
	}
	pw := &progressWriter{w: dst, n: off, total: im.Size, fn: func(pct int) {
		emuLab.mu.Lock()
		emuLab.upload[im.ID] = strconv.Itoa(pct) + " %"
		emuLab.mu.Unlock()
		emuSyncPct(host, im.ID, pct)
	}, txt: func(t string) {
		emuLab.mu.Lock()
		for id, to := range emuLab.moveTo { // a move waiting for this image there shows how far it is
			if to == host && strings.HasPrefix(emuLab.state[id], "moving: the image on the new host") {
				emuLab.state[id] = "moving: the image on the new host · " + t
			}
		}
		emuLab.upload[im.ID] = t
		emuLab.mu.Unlock()
		emuSyncText(host, im.ID, t)
	}}
	_, err = io.Copy(pw, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		emuLab.mu.Lock()
		if c := emuLab.conns[host]; c != nil {
			c.close()
			delete(emuLab.conns, host)
		}
		emuLab.mu.Unlock()
	}
	return err
}

// emuCopyRetry: a dropped connection is worth another try, a full disk or a refused write is not.
func emuCopyRetry(err error) bool {
	m := strings.ToLower(err.Error())
	for _, k := range []string{"no space", "quota", "permission denied", "read-only", "could not open", "not an ssh", "session"} {
		if strings.Contains(m, k) {
			return false
		}
	}
	return true
}

// emuCopyWhy says in plain words why a copy to a host failed.
func emuCopyWhy(err error) string {
	m := strings.ToLower(err.Error())
	switch {
	case strings.Contains(m, "no space") || strings.Contains(m, "quota"):
		return "the host's disk is full"
	case strings.Contains(m, "permission denied"):
		return "the host user may not write to the lab folder"
	case strings.Contains(m, "read-only"):
		return "the lab folder on the host is read-only"
	case strings.Contains(m, "eof") || strings.Contains(m, "connection") || strings.Contains(m, "broken pipe") || strings.Contains(m, "closed") || strings.Contains(m, "timeout") || strings.Contains(m, "timed out"):
		return "the connection to the host dropped"
	case strings.Contains(m, "log in"):
		return "could not log in to the host"
	}
	return "an error on the host"
}

// emuSetKVM records that a host has (or no longer has) usable KVM, and says so in the Events (emuLab.mu held).
func (a *App) emuSetKVM(host string, kvm bool) {
	for i := range emuLab.f.Hosts {
		h := &emuLab.f.Hosts[i]
		if h.Session != host || (h.Info.KVM == "yes") == kvm {
			continue
		}
		if kvm {
			h.Info.KVM = "yes"
			h.Info.Problems = slices.DeleteFunc(h.Info.Problems, func(p string) bool { return strings.Contains(p, "KVM") || strings.Contains(p, "/dev/kvm") })
			emuEvOn(host, "", "ok", "KVM is on now on "+host[strings.LastIndex(host, "/")+1:]+": its switches run at full speed (a running one gets it when it starts again)")
		} else {
			h.Info.KVM = "none"
			emuEvOn(host, "", "warn", "KVM is gone on "+host[strings.LastIndex(host, "/")+1:]+": its switches run in software emulation, 5 to 10 times slower")
		}
		if l := emuLab.f.Lab; l != nil && host == emuLab.f.Host {
			l.KVM = kvm
		}
		_ = a.emuSave()
	}
}

var emuKVMSeen = map[string]time.Time{} // when a host without KVM was last looked at (emuLab.mu)

// EmuHostKVM looks at a host's /dev/kvm now (a second), records a change and says whether it is usable.
func (a *App) EmuHostKVM(host string) (bool, error) {
	out, err := a.emuRun(host, "[ -r /dev/kvm ] && [ -w /dev/kvm ] && echo kvm=yes; true", 20*time.Second)
	if err != nil {
		return false, err
	}
	kvm := strings.Contains(out, "kvm=yes")
	emuLab.mu.Lock()
	a.emuSetKVM(host, kvm)
	emuLab.mu.Unlock()
	return kvm, nil
}

// emuStartWhy adds what to do to a start failure QEMU (or the shell) reported.
func emuStartWhy(m string) string {
	switch {
	case strings.Contains(m, "/dev/tap"):
		return m + " - its port on the host's network (macvtap) could not be made: the host needs sudo without a password for ip link (Hosts → Set Up), or choose another management for it"
	case strings.Contains(m, "Could not access KVM") || strings.Contains(m, "/dev/kvm"):
		return m + " - the host user may not use KVM: add it to the kvm group (Hosts → Set Up), then log in again"
	case strings.Contains(m, "Could not open") && strings.Contains(m, "backing"):
		return m + " - the image under its disk is missing on this host: Images → On the Hosts copies it"
	}
	return m
}

// emuBytes is a size in plain units.
func emuBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	}
	return fmt.Sprintf("%d KB", n>>10)
}

type progressWriter struct {
	w     io.Writer
	n     int64
	total int64
	last  int
	fn    func(int)
	txt   func(string) // "41 % · 1.2 GB of 2.9 GB · 18 MB/s · about 2 min left", once a second
	from  int64        // where it started (a copy that goes on)
	start time.Time
	shown time.Time
}

// emuRate says how far a copy is and how long it takes at this speed.
func emuRate(done, total, from int64, start time.Time) string {
	s := ""
	if total > 0 {
		s = fmt.Sprintf("%d %% · %s of %s", done*100/total, emuBytes(done), emuBytes(total))
	} else {
		s = emuBytes(done)
	}
	if sec := time.Since(start).Seconds(); sec >= 2 && done > from {
		bps := float64(done-from) / sec
		s += " · " + emuBytes(int64(bps)) + "/s"
		if total > done && bps > 0 {
			left := time.Duration(float64(total-done)/bps) * time.Second
			switch {
			case left < time.Minute:
				s += fmt.Sprintf(" · about %d s left", int(left.Seconds())+1)
			case left < time.Hour:
				s += fmt.Sprintf(" · about %d min left", int(left.Minutes()+0.5))
			default:
				s += fmt.Sprintf(" · about %.1f h left", left.Hours())
			}
		}
	}
	return s
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.n += int64(n)
	if p.txt != nil {
		if p.start.IsZero() {
			p.start, p.from = time.Now(), p.n-int64(n)
		}
		if time.Since(p.shown) >= time.Second {
			p.shown = time.Now()
			p.txt(emuRate(p.n, p.total, p.from, p.start))
		}
	}
	if p.total > 0 && p.fn != nil {
		if pct := int(p.n * 100 / p.total); pct != p.last {
			p.last = pct
			p.fn(pct)
		}
	}
	return n, err
}

// EmuUpload copies an image to the lab's host now (in the background).
func (a *App) EmuUpload(id, host string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	im := emuImage(id)
	if host == "" {
		host = emuLab.f.Host
	}
	dir := "termwright-lab"
	if emuLab.f.Lab != nil {
		dir = emuLab.f.Lab.Dir
	}
	if im == nil || host == "" {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("choose an image and a KVM host")
	}
	cp := *im
	emuLab.upload[id] = "0 %"
	emuLab.mu.Unlock()
	emuGo(func() {
		err := a.emuUpload(host, cp, dir)
		emuLab.mu.Lock()
		delete(emuLab.upload, id)
		emuLab.mu.Unlock()
		a.emuNotify(err, "Image "+cp.Name+" is on the host")
	})
	return a.EmuState()
}

func (a *App) emuNotify(err error, ok string) {
	if a.ctx == nil {
		return
	}
	if err != nil {
		wruntime.EventsEmit(a.ctx, "emu:error", err.Error())
	} else if ok != "" {
		wruntime.EventsEmit(a.ctx, "emu:done", ok)
	}
}

// EmuAdd makes the emulated lab on a host, or adds VMs to it.
func (a *App) EmuAdd(r EmuRequest) (EmuView, error) {
	defer emuAs("user")()
	if err := a.ready(); err != nil {
		return EmuView{}, err
	}
	n := 0
	for _, g := range r.Groups {
		n += g.Count
	}
	if n == 0 {
		return EmuView{}, errors.New("no switches chosen")
	}
	emuLab.mu.Lock()
	a.emuLoad()
	var info *emu.HostInfo
	for i := range emuLab.f.Hosts {
		if emuLab.f.Hosts[i].Session == r.Host {
			info = &emuLab.f.Hosts[i].Info
		}
	}
	emuLab.mu.Unlock()
	if info == nil || (r.Mgmt == "cloud" || r.Mgmt == "lan") && info.LanAddr == "" { // checked by an older Termwright: without the host's address
		h, err := a.EmuCheckHost(r.Host)
		if err != nil {
			return EmuView{}, err
		}
		info = &h.Info
	}
	if !info.OK() {
		return EmuView{}, errors.New("the host cannot run VMs: " + strings.Join(info.Problems, " "))
	}
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil || len(l.VMs) == 0 {
		mgmt := r.Mgmt
		if mgmt == "" || mgmt == "auto" {
			mgmt = emuBestMgmt(info)
		}
		if err := emuMgmtOK(info, mgmt); err != nil {
			emuLab.mu.Unlock()
			return EmuView{}, err
		}
		oth := a.emuOthers() // a folder, ports and a bridge network no other topology has: they may run at the same time
		l = &emu.Lab{ID: emuFreeLabID(oth), Dir: "termwright-lab", Mgmt: mgmt, LanIf: info.LanIf, LanKind: info.LanKind, MgmtNet: orStr(r.MgmtNet, emuFreeBridgeNet(oth)), Base: emuFreeBase(oth), KVM: info.KVM == "yes"}
		if mgmt == "cloud" || mgmt == "lan" && strings.TrimSpace(r.MgmtNet) != "" {
			if err := emuCloud(l, info, r.MgmtNet); err != nil {
				emuLab.mu.Unlock()
				return EmuView{}, err
			}
			l.LanFixed = mgmt == "lan"
		}
		emuLab.f.Lab, emuLab.f.Host = l, r.Host
		emuLab.f.Sessions, emuLab.f.Consoles = map[string]string{}, map[string]string{}
	} else if r.Host != "" && r.Host != emuLab.f.Host && !slices.Contains(l.Hosts(emuLab.f.Host), r.Host) {
		// a further host of the lab: its switches get its own management
		mgmt := r.Mgmt
		if mgmt == "" || mgmt == "auto" {
			mgmt = emuBestMgmt(info)
		}
		if err := emuMgmtOK(info, mgmt); err != nil {
			emuLab.mu.Unlock()
			return EmuView{}, err
		}
		tmp := &emu.Lab{Mgmt: mgmt, LanIf: info.LanIf, LanKind: info.LanKind, MgmtNet: orStr(r.MgmtNet, "10.99.0.0/24")}
		if mgmt == "cloud" || mgmt == "lan" && strings.TrimSpace(r.MgmtNet) != "" {
			if err := emuCloud(tmp, info, r.MgmtNet); err != nil {
				emuLab.mu.Unlock()
				return EmuView{}, err
			}
			tmp.LanFixed = mgmt == "lan"
		}
		l.SetNet(r.Host, emuLab.f.Host, tmp.Net("", ""))
	}
	if r.Host == "" {
		r.Host = emuLab.f.Host
	}
	if l.Addrs == nil {
		l.Addrs = map[string]string{}
	}
	if ip := strings.Split(info.LanAddr, "/")[0]; ip != "" {
		l.Addrs[r.Host] = ip
	}
	onHost := ""
	if r.Host != emuLab.f.Host {
		onHost = r.Host
	}
	lv := l.For(r.Host, emuLab.f.Host)
	imgs := emuImages()
	next := 0
	names := map[string]bool{}
	for _, v := range l.VMs {
		if v.Index >= next {
			next = v.Index + 1
		}
		names[v.Name] = true
	}
	var add []emu.VM
	for _, g := range r.Groups {
		var tp *emu.Template
		if g.Template != "" {
			if tp = emuTemplate(g.Template); tp == nil {
				emuLab.mu.Unlock()
				return EmuView{}, errors.New("unknown template")
			}
			g.Image = tp.Image
			g.CPUs, g.RAM = orInt(g.CPUs, tp.CPUs), orInt(g.RAM, tp.RAM)
		}
		// the template's serial numbers and MACs count on from the switches made from it before
		made := 0
		for _, v := range l.VMs {
			if tp != nil && v.Template == tp.ID {
				made++
			}
		}
		im := imgs[g.Image]
		if im == nil {
			emuLab.mu.Unlock()
			return EmuView{}, errors.New("unknown image")
		}
		names2 := orStr(g.Names, "sw-{n}")
		for i, k := 1, 0; k < g.Count; i++ {
			name := strings.ReplaceAll(names2, "{n}", strconv.Itoa(i))
			if !strings.Contains(names2, "{n}") && (g.Count > 1 || names[names2]) {
				name = names2 + "-" + strconv.Itoa(i)
			}
			if names[name] {
				continue
			}
			names[name] = true
			v := emu.VM{ID: "v" + randHex(3), Name: name, Image: im.ID, Index: next, CPUs: orInt(g.CPUs, im.CPUs), RAM: orInt(g.RAM, im.RAM), Host: onHost}
			switch {
			case g.SerialStart != "":
				v.Serial = emu.NextSerial(g.SerialStart, k)
			case tp != nil && tp.SerialStart != "":
				v.Serial = emu.NextSerial(tp.SerialStart, made+k)
			case im.Serial != "":
				v.Serial = sim.Generate(im.Serial)
			}
			v.DiskGB = g.DiskGB
			if g.MACStart != "" {
				v.BaseMAC = emu.NextMAC(g.MACStart, k)
			}
			if g.Model != "" && im.Persona != nil {
				v.Persona = &emu.Persona{Slots: []emu.Slot{{Num: 1, Model: g.Model, Serial: v.Serial, Module: g.Module, Master: true}}}
			}
			if tp != nil {
				v.Template, v.DiskGB = tp.ID, orInt(g.DiskGB, tp.DiskGB)
				if tp.MACStart != "" && g.MACStart == "" {
					v.BaseMAC = emu.NextMAC(tp.MACStart, made+k)
				}
				if tp.Model != "" && im.Persona != nil && g.Model == "" {
					v.Persona = &emu.Persona{Slots: []emu.Slot{{Num: 1, Model: tp.Model, Serial: v.Serial, Module: tp.Module, Master: true}}, Values: tp.Values}
				}
			}
			v.MgmtIP, v.Gateway = lv.FreeAddress(v)
			lv.VMs = append(lv.VMs, v)
			next++
			k++
			add = append(add, v)
		}
	}
	l.VMs = append(l.VMs, add...)
	emuPlace(add, l.VMs)
	l.Wire(add, r.Cabling, imgs)
	if err := a.emuSave(); err != nil {
		emuLab.mu.Unlock()
		return EmuView{}, err
	}
	emuLab.mu.Unlock()
	for _, v := range add {
		a.emuSessions(v)
	}
	emuLab.mu.Lock()
	_ = a.emuSave()
	emuLab.mu.Unlock()
	if r.Start {
		ids := make([]string, len(add))
		for i, v := range add {
			ids[i] = v.ID
		}
		return a.EmuStart(ids)
	}
	return a.EmuState()
}

// emuFor is the lab as one host runs it (mu held): its management, and the
// other hosts' addresses for the cables between them.
func (a *App) emuFor(host string) *emu.Lab {
	l := emuLab.f.Lab
	if l == nil {
		return nil
	}
	if l.Addrs == nil {
		l.Addrs = map[string]string{}
	}
	for _, h := range emuLab.f.Hosts {
		if ip := strings.Split(h.Info.LanAddr, "/")[0]; ip != "" {
			l.Addrs[h.Session] = ip
		}
	}
	c := l.For(host, emuLab.f.Host)
	if len(emuLab.f.Hosts) > 1 { // switches can move between hosts: a CPU every host has
		c.CPU = emu.CommonCPU
	}
	c.Taken = a.emuOthers().addrs // other topologies' switches may run at the same time
	// each host its own acceleration: one without KVM emulates in software
	for _, h := range emuLab.f.Hosts {
		if h.Session == host && h.Info.KVM != "" {
			c.KVM = h.Info.KVM == "yes"
		}
	}
	return c
}

// emuHostOf is the host a VM runs on (mu held).
func emuHostOf(v emu.VM) string {
	if emuLab.f.Lab == nil {
		return emuLab.f.Host
	}
	return emuLab.f.Lab.HostOf(v, emuLab.f.Host)
}

// emuHostOfID is the host a VM runs on (no lock held).
func (a *App) emuHostOfID(id string) string {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	if v := emuLab.f.Lab.VMByID(id); v != nil {
		return emuHostOf(*v)
	}
	return emuLab.f.Host
}

// emuGroups splits VMs (all when ids is empty) by the host they run on, the
// lab's own host first.
func (a *App) emuGroups(ids []string) ([]string, map[string][]string) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil || emuLab.f.Host == "" {
		return nil, nil
	}
	if len(ids) == 0 {
		for _, v := range l.VMs {
			ids = append(ids, v.ID)
		}
	}
	by := map[string][]string{}
	var hosts []string
	for _, id := range ids {
		if v := l.VMByID(id); v != nil {
			h := emuHostOf(*v)
			if _, ok := by[h]; !ok {
				hosts = append(hosts, h)
			}
			by[h] = append(by[h], id)
		}
	}
	return hosts, by
}

func randByte() byte { b, _ := hex.DecodeString(randHex(1)); return b[0] }

func orStr(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func orInt(v ...int) int {
	for _, x := range v {
		if x > 0 {
			return x
		}
	}
	return 0
}

// emuPlace: the first two VMs of a lab on top, the others below.
func emuPlace(add, all []emu.VM) {
	top, bottom := 40, 40
	for _, v := range all {
		if v.X != 0 || v.Y != 0 {
			if v.Y < 200 {
				top = max(top, v.X+200)
			} else {
				bottom = max(bottom, v.X+180)
			}
		}
	}
	for i := range all {
		v := &all[i]
		isNew := false
		for _, n := range add {
			isNew = isNew || n.ID == v.ID
		}
		if !isNew {
			continue
		}
		if v.Index < 2 {
			v.X, v.Y, top = top, 60, top+260
		} else {
			v.X, v.Y, bottom = bottom, 300, bottom+180
		}
	}
}

// emuSessions makes a VM's SSH and console sessions (lock not held).
// emuTarget is where a VM's SSH session goes (and through which jump host).
func emuTarget(l *emu.Lab, v emu.VM, host string) (string, int, string) {
	switch l.Mgmt {
	case "cloud":
		if v.Direct { // this computer reaches it straight
			return strings.Split(v.MgmtIP, "/")[0], 22, ""
		}
		return strings.Split(v.MgmtIP, "/")[0], 22, host
	case "bridge":
		return strings.Split(v.MgmtIP, "/")[0], 22, host
	case "nat":
		return orStr(strings.Split(v.MgmtIP, "/")[0], "0.0.0.0"), 22, host // reached through the host
	case "lan":
		if l.LanKind == "macvtap" || v.Direct {
			return orStr(strings.Split(v.MgmtIP, "/")[0], "0.0.0.0"), 22, "" // the host cannot reach its own macvtap guests: straight from this computer
		}
		return orStr(strings.Split(v.MgmtIP, "/")[0], "0.0.0.0"), 22, host
	}
	return "127.0.0.1", l.SSHPort(v), host
}

// emuRetarget points a VM's SSH session where its management is now.
func (a *App) emuRetarget(v emu.VM) {
	emuLab.mu.Lock()
	l, path := emuLab.f.Lab, emuLab.f.Sessions[v.ID]
	host := emuHostOf(v)
	if l != nil {
		l = a.emuFor(host)
	}
	if l == nil || path == "" {
		emuLab.mu.Unlock()
		return
	}
	hostname, port, jump := emuTarget(l, v, host)
	emuLab.mu.Unlock()
	if s, err := a.store.Get(path); err == nil {
		s.Hostname, s.Port, s.JumpSession = hostname, port, jump
		_, _ = a.store.Save(s, path)
	}
	emuLab.mu.Lock()
	con := emuLab.f.Consoles[v.ID]
	emuLab.mu.Unlock()
	if s, err := a.store.Get(con); err == nil && con != "" && s.JumpSession != host { // its console is on its host
		s.JumpSession = host
		_, _ = a.store.Save(s, con)
	}
	a.emuLogins(v)
}

// emuLogins gives a switch's SSH and console sessions the image's login
// (sessions made before this, or after the image's password changed).
func (a *App) emuLogins(v emu.VM) {
	emuLab.mu.Lock()
	paths := []string{emuLab.f.Sessions[v.ID], emuLab.f.Consoles[v.ID]}
	emuLab.mu.Unlock()
	for _, p := range paths {
		if s, err := a.store.Get(p); err == nil && p != "" {
			a.emuLogin(&s, v)
			if saved, err := a.store.Save(s, p); err == nil {
				a.emuLoginPassword(saved.Path, v)
			}
		}
	}
}

// emuLogin gives a switch's SSH session the image's own login: not the
// device type's (that is for real switches of that kind), and with the
// image's password even when it is empty (EXOS' admin has none), so the
// session logs in without asking.
func (a *App) emuLogin(s *session.Session, v emu.VM) {
	emuLab.mu.Lock()
	im := emuImage(v.Image)
	login := "admin"
	if im != nil {
		login = orStr(im.Login, "admin")
	}
	emuLab.mu.Unlock()
	s.Username, s.OwnLogin, s.SavePassword = login, true, true
}

func (a *App) emuLoginPassword(path string, v emu.VM) {
	pw, _ := secrets.Lookup(emuImageKey(v.Image))
	if pw == "" {
		emuLab.mu.Lock()
		if im := emuImage(v.Image); im != nil {
			pw = im.DefaultPassword
		}
		emuLab.mu.Unlock()
	}
	_ = a.SetSessionPassword(path, pw, true)
}

func (a *App) emuSessions(v emu.VM) {
	emuLab.mu.Lock()
	l, folder := emuLab.f.Lab, emuLab.f.Folder
	host := emuHostOf(v)
	if l != nil {
		l = a.emuFor(host)
	}
	im := emuImage(v.Image)
	if im == nil || l == nil {
		emuLab.mu.Unlock()
		return
	}
	login, dt, nm := orStr(im.Login, "admin"), im.DeviceType, im.Name
	hostname, port, jump := emuTarget(l, v, host)
	conPort := l.ConsolePort(v)
	emuLab.mu.Unlock()
	free := func(folder, name string) string {
		n := name
		for i := 2; ; i++ {
			if _, err := a.store.Get(session.JoinPath(folder, n)); err != nil {
				return n
			}
			n = name + " (" + strconv.Itoa(i) + ")"
		}
	}
	ss := session.Session{Name: free(folder, v.Name), Folder: folder, Protocol: session.ProtoSSH2, Hostname: hostname, Port: port, Username: login, OwnLogin: true, SavePassword: true, DeviceType: dt, JumpSession: jump,
		Description: []string{fmt.Sprintf("Emulated %s (Termwright lab) on its KVM host - management %s", nm, v.MgmtIP)}}
	if saved, err := a.store.Save(ss, ""); err == nil {
		a.emuLoginPassword(saved.Path, v)
		emuLab.mu.Lock()
		emuLab.f.Sessions[v.ID] = saved.Path
		emuLab.mu.Unlock()
	}
	cf := session.JoinPath(folder, "Consoles")
	cs := session.Session{Name: free(cf, v.Name+" console"), Folder: cf, Protocol: session.ProtoTelnet, Hostname: "127.0.0.1", Port: conPort, Username: login, OwnLogin: true, SavePassword: true, DeviceType: dt, JumpSession: host,
		Description: []string{"Serial console of the emulated switch " + v.Name}}
	if saved, err := a.store.Save(cs, ""); err == nil {
		a.emuLoginPassword(saved.Path, v)
		emuLab.mu.Lock()
		emuLab.f.Consoles[v.ID] = saved.Path
		emuLab.mu.Unlock()
	}
}

// emuStart uploads the images the VMs need and starts them; their boot is then watched.
func (a *App) emuStart(host string, ids []string) {
	emuLab.mu.Lock()
	l := a.emuFor(host) // the lab as this host runs it (its management, tunnels to the other hosts)
	if l == nil {
		emuLab.mu.Unlock()
		return
	}
	imgs := map[string]emu.Image{}
	var vms []emu.VM
	for _, id := range ids { // two switches with one address (from before addresses skipped the ones in use): this one moves on
		if v := l.VMByID(id); v != nil && v.MgmtIP != "" && v.MgmtIP != "10.0.2.15/24" {
			for _, o := range l.VMs {
				if o.ID != v.ID && o.MgmtIP == v.MgmtIP && emuHostOf(o) == emuHostOf(*v) || o.ID == v.ID && l.Taken[v.MgmtIP] {
					was := v.MgmtIP
					v.MgmtIP, v.Gateway = l.FreeAddress(*v)
					v.Direct = false
					if im := emuImage(v.Image); im != nil && strings.Contains(strings.Join(im.Day0, "\n"), "{mgmt") {
						v.Day0Done = false
					}
					emuEv(v.ID, "warn", fmt.Sprintf("its management address %s was %s's too: it gets %s", strings.Split(was, "/")[0], o.Name, strings.Split(v.MgmtIP, "/")[0]))
					_ = a.emuSave()
					break
				}
			}
		}
	}
	for _, id := range ids {
		if v := l.VMByID(id); v != nil {
			vms = append(vms, *v)
			if im := emuImage(v.Image); im != nil {
				imgs[im.ID] = *im
			}
			emuLab.state[id] = "uploading"
			delete(emuLab.errs, id)
			delete(emuLab.addrOK, id)
		}
	}
	dir := l.Dir
	emuLab.mu.Unlock()
	failAll := func(err error) {
		emuLab.mu.Lock()
		for _, v := range vms {
			delete(emuLab.state, v.ID)
			emuLab.errs[v.ID] = err.Error()
			emuEvOn(host, v.ID, "error", "did not start: "+err.Error())
		}
		emuLab.mu.Unlock()
		a.emuNotify(err, "")
	}
	for _, im := range imgs {
		if err := a.emuUpload(host, im, dir); err != nil {
			failAll(err)
			return
		}
		a.emuSyncAfter(im.ID)
	}
	emuLab.mu.Lock()
	ptrs := map[string]*emu.Image{}
	for k := range imgs {
		im := imgs[k]
		ptrs[k] = &im
	}
	// persona files go into the disks before the VMs start
	type pw struct {
		v      emu.VM
		script string
		hash   string
	}
	var writes []pw
	for _, v := range vms {
		if im := ptrs[v.Image]; im != nil && im.Persona != nil && v.Persona != nil {
			if txt, err := im.Persona.Render(v, v.Persona); err == nil && txt != "" && emu.Hash(txt) != v.Persona.Applied {
				writes = append(writes, pw{v, l.WriteFileScript(v, im.Persona.File, txt, im.Persona.FSHas...), emu.Hash(txt)})
			} else if err != nil {
				emuLab.errs[v.ID] = err.Error()
			}
		}
	}
	if len(writes) > 0 {
		var wv []emu.VM
		for _, w := range writes {
			wv = append(wv, w.v)
			emuLab.state[w.v.ID] = "persona"
		}
		prep := l.PrepareScript(wv, ptrs)
		emuLab.mu.Unlock()
		_, _ = a.emuRun(host, prep, 5*time.Minute)
		for _, w := range writes {
			out, err := a.emuRun(host, w.script, 5*time.Minute)
			emuLab.mu.Lock()
			switch {
			case err == nil && strings.HasPrefix(lastLine(out), "ok "):
				if vv := l.VMByID(w.v.ID); vv != nil && vv.Persona != nil {
					vv.Persona.Applied = w.hash
				}
				emuLab.pinfo[w.v.ID] = "written to " + strings.TrimPrefix(lastLine(out), "ok ")
				emuEv(w.v.ID, "ok", "persona "+emuLab.pinfo[w.v.ID]+", read back")
				delete(emuLab.personaTried, w.v.ID)
				delete(emuLab.perr, w.v.ID)
			case strings.Contains(out, "no filesystem") && !emuLab.personaTried[w.v.ID]:
				emuLab.personaAfterBoot[w.v.ID] = true // its folder appears on the first boot
				emuLab.personaTried[w.v.ID] = true
				emuEv(w.v.ID, "info", "persona: "+strings.TrimPrefix(lastLine(out), "fail ")+" - it boots once, then the file is written")
			default:
				emuLab.perr[w.v.ID] = strings.TrimSpace(strings.TrimPrefix(lastLine(out+errText(err)), "fail "))
				emuEv(w.v.ID, "error", "persona not written: "+emuLab.perr[w.v.ID])
			}
			emuLab.mu.Unlock()
		}
		emuLab.mu.Lock()
		_ = a.emuSave()
		for i := range vms {
			if vv := l.VMByID(vms[i].ID); vv != nil {
				vms[i] = *vv
			}
		}
	}
	script := l.StartScript(vms, ptrs)
	for _, v := range vms {
		emuLab.state[v.ID] = "starting"
		emuLab.starting[v.ID] = time.Now()
		resume := v.FastKey != "" && ptrs[v.Image] != nil && v.FastKey == l.FastKey(v, ptrs[v.Image])
		if resume {
			emuEv(v.ID, "info", "starting on "+host+" from its saved state")
		} else {
			emuEv(v.ID, "info", "starting on "+host)

			delete(emuLab.pcheck, v.ID) // the persona is checked again on this boot
		}
		delete(emuLab.ready, v.ID) // its console log starts empty
		delete(emuLab.resumed, v.ID)
	}
	emuLab.mu.Unlock()
	out, err := a.emuRun(host, script, 3*time.Minute)
	if err != nil {
		failAll(err)
		return
	}
	emuLab.mu.Lock()
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimSpace(line), " ", 3)
		if len(f) == 2 && (f[0] == "resumed" || f[0] == "started") {
			if vv := l.VMByID(f[1]); vv != nil && vv.FastKey != "" {
				vv.FastKey = "" // used (or dropped): a stop without saving must not bring the old state back
				_ = a.emuSave()
			}
			if f[0] == "resumed" {
				emuLab.ready[f[1]], emuLab.resumed[f[1]] = true, true
				emuEv(f[1], "ok", "resumed from its saved state - configuration, address and sessions as they were")
			}
		}
		if g := strings.Fields(strings.TrimSpace(line)); len(g) > 3 && g[0] == "cmd" {
			cmd := strings.Join(g[2:], " ")
			if vv := emuLab.f.Lab.VMByID(g[1]); vv != nil {
				emuLogf(emuLvInfo, host, vv.Name, "QEMU runs it with: %s", cmd)
				if c := emu.CPUOf(cmd); c != "" && vv.CPU != "tcg" { // the CPU it really has (an image's own -cpu wins): a live move gives the same one
					vv.CPU = c
					_ = a.emuSave()
				}
			}
		}
		if g := strings.Fields(strings.TrimSpace(line)); len(g) == 3 && g[0] == "accel" {
			kvm := g[1] == "kvm"
			if kvm != l.KVM { // the host's KVM changed since it was checked: what it has now counts
				a.emuSetKVM(host, kvm)
			}
			if vv := emuLab.f.Lab.VMByID(g[2]); vv != nil { // the CPU it runs with (a live move needs it on the other host too)
				vv.CPU = "tcg"
				if kvm {
					vv.CPU = orStr(l.CPU, "host")
				}
				_ = a.emuSave()
			}
			if !kvm {
				emuEv(g[2], "warn", "no KVM on "+host[strings.LastIndex(host, "/")+1:]+" (no usable /dev/kvm): it runs in software emulation, 5 to 10 times slower - a switch can take 10 minutes or more to boot, and one that checks its own timing may restart. Turn on nested virtualization for that VM (VMware: Expose hardware assisted virtualization to the guest OS), then Hosts → Check Again")
			}
		}
		if len(f) >= 2 && f[0] == "fail" {
			if f[1] == "all" { // nothing could start
				for _, v := range vms {
					emuLab.errs[v.ID] = strings.TrimSpace(strings.Join(f[2:], " "))
					emuEv(v.ID, "error", "did not start: "+emuLab.errs[v.ID])
				}
				continue
			}
			emuLab.errs[f[1]] = emuStartWhy(strings.TrimSpace(strings.Join(f[2:], " ")))
			emuEv(f[1], "error", "did not start: "+emuLab.errs[f[1]])
		}
	}
	var gone []string
	for _, v := range vms {
		delete(emuLab.state, v.ID)
		if emuLab.f.Lab == nil || emuLab.f.Lab.VMByID(v.ID) == nil {
			gone = append(gone, v.ID) // removed while it started
		}
	}
	emuLab.statusAt = time.Time{}
	emuLab.mu.Unlock()
	if len(gone) > 0 {
		_, _ = a.emuRun(host, l.ResetScript(gone), time.Minute)
	}
	a.emuLinksDown(host, vms, ptrs)
	emuGo(func() { a.emuWatch(host, ids) })
}

// emuLinksDown takes the link of ports without a cable (and of pulled
// cables) down in VMs that just started, as GNS3 does: a NIC without a
// network would show its link up.
func (a *App) emuLinksDown(host string, vms []emu.VM, imgs map[string]*emu.Image) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	type job struct {
		id    string
		mon   int
		links []string
	}
	var jobs []job
	if l != nil {
		for _, v := range vms {
			if im := imgs[v.Image]; im != nil && l.VMByID(v.ID) != nil {
				if d := l.DownLinks(v, im); len(d) > 0 {
					jobs = append(jobs, job{v.ID, l.MonitorPort(v), d})
				}
			}
		}
	}
	emuLab.mu.Unlock()
	if len(jobs) == 0 {
		return
	}
	cl, err := a.emuClient(context.Background(), host)
	if err != nil {
		return
	}
	for _, j := range jobs {
		conn, err := cl.Dial("tcp", "127.0.0.1:"+strconv.Itoa(j.mon))
		if err != nil {
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		buf := make([]byte, 4096)
		_, _ = conn.Read(buf) // the monitor's greeting
		for _, n := range j.links {
			_, _ = fmt.Fprintf(conn, "set_link %s off\r\n", n)
			time.Sleep(50 * time.Millisecond)
		}
		_, _ = conn.Read(buf)
		_ = conn.Close()
		emuEvent(j.id, "info", fmt.Sprintf("link down on %d ports without a cable", len(j.links)))
	}
}

// emuWatch follows the VMs' boot (the console logs) and types day 0 on the ones that need it.
func (a *App) emuWatch(host string, ids []string) {
	pending := map[string]bool{}
	for _, id := range ids {
		pending[id] = true
	}
	deadline := time.Now().Add(30 * time.Minute)
	for len(pending) > 0 && time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		st := a.emuPoll(true)
		emuLab.mu.Lock()
		l := a.emuFor(host)
		if l == nil || !slices.Contains(l.Hosts(emuLab.f.Host), host) {
			emuLab.mu.Unlock()
			return
		}
		var day0 []emu.VM
		for id := range pending {
			v := l.VMByID(id)
			if v == nil {
				delete(pending, id)
				continue
			}
			if _, up := st.Up[id]; !up {
				if time.Since(emuLab.starting[id]) > 20*time.Second {
					delete(pending, id) // stopped (or it never started)
				}
				continue
			}
			im := emuImage(v.Image)
			if im == nil || !emuLab.ready[id] {
				continue
			}
			delete(pending, id)
			if emuLab.resumed[id] { // as it was saved: booted, day 0 and its persona done
				continue
			}
			if emuLab.personaAfterBoot[id] {
				delete(emuLab.personaAfterBoot, id)
				emuGo(func() { a.emuPersonaReboot(host, id) })
				continue
			}
			needDay0 := !v.Day0Done && len(im.Day0) > 0
			needCheck := im.Persona != nil && im.Persona.Check != "" && v.Persona != nil && v.Persona.Applied != "" && !emuLab.pcheck[id]
			if (needDay0 || needCheck || l.Mgmt == "lan" && !l.LanFixed || l.Fixed() && len(im.AddrSet) > 0) && !emuLab.day0[id] {
				day0 = append(day0, *v)
				emuLab.day0[id] = true
			}
		}
		emuLab.mu.Unlock()
		for _, v := range day0 {
			emuGo(func() { a.emuDay0(host, v) })
		}
	}
}

// emuDay0 types an image's first-boot commands on a VM's console.
func (a *App) emuDay0(host string, v emu.VM) {
	defer func() {
		emuLab.mu.Lock()
		delete(emuLab.day0, v.ID)
		delete(emuLab.state, v.ID)
		emuLab.mu.Unlock()
	}()
	emuLab.mu.Lock()
	l := a.emuFor(host)
	im0 := emuImage(v.Image)
	if l == nil || im0 == nil {
		emuLab.mu.Unlock()
		return
	}
	im := *im0
	port := l.ConsolePort(v)
	cur := l.VMByID(v.ID)
	doDay0 := cur != nil && !cur.Day0Done && len(im.Day0) > 0
	lan := l.Mgmt == "lan" && !l.LanFixed // DHCP on the host's network
	fixed := l.Fixed() && len(im.AddrSet) > 0 && cur != nil && cur.MgmtIP != ""
	if cur != nil {
		v = *cur // its address as it is now
	}
	if doDay0 {
		emuLab.state[v.ID] = "day0"
	} else {
		emuLab.state[v.ID] = "dhcp"
	}
	emuLab.mu.Unlock()
	im.Password, _ = secrets.Lookup(emuImageKey(im.ID))
	im.Password = orStr(im.Password, im.DefaultPassword) // the factory password while none is set
	fail := func(err error) {
		emuLab.mu.Lock()
		emuLab.errs[v.ID] = "Day-0 configuration: " + err.Error()
		emuEv(v.ID, "error", emuLab.errs[v.ID])
		emuLab.mu.Unlock()
	}
	cl, err := a.emuClient(context.Background(), host)
	if err != nil {
		fail(err)
		return
	}
	conn, err := cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		fail(fmt.Errorf("console: %w (is the console open in a tab? close it and use Back to Day-0)", err))
		return
	}
	c := emu.NewConsole(conn)
	c.OnAnswer = func(q, sent string) {
		if sent == "" {
			sent = "Enter"
		}
		emuLogf(emuLvVerbose, host, v.Name, "console: %q answered with %s", q, sent)
		emuLab.mu.Lock()
		emuEv(v.ID, "info", fmt.Sprintf("answered %q with %s", q, sent))
		emuLab.mu.Unlock()
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if fixed {
		// the management address is fixed: typed at each start (the switch's own
		// configuration need not be saved for it), before day 0, whose commands
		// may need it (SSH, NTP, a saved configuration with it)
		emuLab.mu.Lock()
		emuLab.state[v.ID] = "address"
		emuLab.mu.Unlock()
		set := im
		set.Day0 = im.AddrSet
		if err := emu.Day0(ctx, c, &set, v, v.Index+1, nil); err != nil {
			fail(emuWithTail(fmt.Errorf("setting its address %s: %w", v.MgmtIP, err), c))
			return
		}
		// the switch says which address it has: a command it refused (an old
		// address in the way) shows here, before the session points there
		if ip := strings.Split(v.MgmtIP, "/")[0]; im.AddrCmd != "" {
			out, err := emu.Ask(ctx, c, &im, im.AddrCmd)
			if err == nil && !strings.Contains(out, ip) {
				o := strings.Join(strings.Fields(emu.CleanConsole(out)), " ")
				if len(o) > 300 {
					o = o[:300] + "…"
				}
				fail(fmt.Errorf("the switch did not take the address %s (%s shows: %s) - check the image's Set address commands", ip, im.AddrCmd, o))
				return
			}
		}
		emuLab.mu.Lock()
		emuLab.addrOK[v.ID] = true
		if cur := emuLab.f.Lab.VMByID(v.ID); cur != nil && cur.AddrOn != v.MgmtIP {
			cur.AddrOn = v.MgmtIP // kept: after a restart of Termwright, or a fast start, it still has it
			_ = a.emuSave()
		}
		emuEv(v.ID, "ok", "has the address "+strings.Split(v.MgmtIP, "/")[0]+" (gateway "+orStr(v.Gateway, "none")+") - its SSH session points there")
		emuLab.mu.Unlock()
		a.emuRetarget(v)
	}
	if doDay0 {
		err = emu.Day0(ctx, c, &im, v, v.Index+1, func(i int, cmd string) {
			emuLogf(emuLvDebug, host, v.Name, "day 0 types: %s", cmd)
			emuLab.mu.Lock()
			emuLab.state[v.ID] = fmt.Sprintf("day0 %d/%d", i+1, len(im.Day0))
			emuLab.mu.Unlock()
		})
		if err != nil {
			fail(emuWithTail(err, c))
			return
		}
		emuLab.mu.Lock()
		if vv := l.VMByID(v.ID); vv != nil {
			vv.Day0Done = true
		}
		delete(emuLab.errs, v.ID)
		emuEv(v.ID, "ok", "day-0 typed: "+strings.Join(im.Day0, " · "))
		_ = a.emuSave()
		emuLab.mu.Unlock()
	}
	a.emuCheckPersona(ctx, c, &im, v)
	a.emuReadFacts(ctx, c, &im, v)
	if im.LagCmd != "" { // its LAGs, for the topology
		if out, err := emu.Ask(ctx, c, &im, im.LagCmd); err == nil {
			a.emuKeepLags(v, &im, emu.ParseLags(emu.CleanConsole(out)))
		}
	}
	if lan {
		// the management port is on the host's network: its address comes from DHCP
		emuLab.mu.Lock()
		emuLab.state[v.ID] = "dhcp"
		emuLab.mu.Unlock()
		if im.AddrCmd == "" {
			emuLab.mu.Lock()
			emuLab.errs[v.ID] = "Its address is unknown: give the image an address command (Images), or look it up on the console"
			emuLab.mu.Unlock()
			return
		}
		var addr string
		for try := 0; try < 12 && ctx.Err() == nil; try++ {
			if addr, err = emu.AskAddress(ctx, c, &im); err == nil {
				break
			}
			time.Sleep(10 * time.Second)
		}
		if addr == "" {
			hint := " Check that a DHCP server serves the host's network."
			emuLab.mu.Lock()
			for _, h := range emuLab.f.Hosts {
				if h.Session == host && h.Info.Virt != "" && h.Info.Virt != "none" && h.Info.Virt != "unknown" {
					hint = " The host is itself a VM (" + h.Info.Virt + "): its virtual switch must let the switches' MAC addresses through (VMware: Promiscuous mode, MAC address changes and Forged transmits on its port group)."
				}
			}
			emuLab.mu.Unlock()
			fail(emuWithTail(fmt.Errorf("no DHCP address after 2 minutes (%v).%s", err, hint), c))
			return
		}
		a.emuSetAddress(v.ID, addr)
		a.emuNotify(nil, v.Name+" has the address "+strings.Split(addr, "/")[0])
		a.emuReach(host, v.ID)
		return
	}
	a.emuNotify(nil, v.Name+" is configured")
	a.emuReach(host, v.ID)
}

// emuReach tries a switch's management address from the host (ping, SSH,
// Telnet) and says what answers in the Events log: where a session fails,
// this tells the switch apart from the way to it.
func (a *App) emuReach(host, id string) {
	emuLab.mu.Lock()
	l := a.emuFor(host)
	var ip string
	if l != nil && l.Mgmt != "forward" && l.Mgmt != "" {
		if v := l.VMByID(id); v != nil {
			ip = strings.Split(v.MgmtIP, "/")[0]
		}
	}
	if ip != "" && l.Mgmt == "lan" && l.LanKind == "macvtap" { // Linux does not let a host talk to its own macvtap guests
		emuEv(id, "info", "on the host's network through macvtap: the host itself cannot reach it (a Linux rule), the network and this computer can - SSH to "+ip)
		ip = ""
	}
	emuLab.mu.Unlock()
	if _, err := netip.ParseAddr(ip); err != nil {
		return
	}
	script := fmt.Sprintf("ip=%s; P=$(command -v ping || echo busybox ping); if $P -c1 -W2 $ip >/dev/null 2>&1; then echo ping=yes; else echo ping=no; fi\nfor p in 22 23 80; do if timeout 3 bash -c \"</dev/tcp/$ip/$p\" 2>/dev/null; then echo p$p=open; else echo p$p=closed; fi; done\necho \"route=$(ip -o route get $ip 2>/dev/null | head -1)\"\n", ip)
	r := map[string]string{}
	for try := 0; try < 6; try++ { // a switch starts its services after its login prompt: tried for a minute
		if try > 0 {
			time.Sleep(10 * time.Second)
		}
		out, err := a.emuRun(host, script, 30*time.Second)
		if err != nil {
			return
		}
		r = map[string]string{}
		for _, line := range strings.Split(out, "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
				r[k] = v
			}
		}
		if r["p22"] == "open" {
			break
		}
	}
	level := "ok"
	msg := fmt.Sprintf("from the host %s: ping %s, SSH (22) %s, Telnet (23) %s, HTTP (80) %s", ip, map[string]string{"yes": "answers", "no": "no answer"}[r["ping"]], r["p22"], r["p23"], r["p80"])
	if r["p22"] != "open" {
		level = "warn"
		if r["ping"] != "yes" {
			msg += " - the host cannot reach it: check the switch's address on its console (" + strings.TrimSpace(r["route"]) + ")"
		} else {
			msg += " - it answers but SSH is off: enable SSH on the switch (EXOS: enable ssh2)"
		}
	}
	emuLab.mu.Lock()
	emuEv(id, level, msg)
	emuLab.mu.Unlock()
	a.emuDirect(id, ip)
}

// emuDirect tries a switch on the host's network from this computer: when its
// SSH answers, its session connects straight, else through the host.
func (a *App) emuDirect(id, ip string) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l != nil {
		if v := l.VMByID(id); v != nil {
			l = a.emuFor(emuHostOf(*v))
		}
	}
	if l == nil || !(l.Mgmt == "cloud" || l.Mgmt == "lan") {
		emuLab.mu.Unlock()
		return
	}
	emuLab.mu.Unlock()
	direct := false
	for try := 0; try < 3 && !direct; try++ {
		if c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "22"), 3*time.Second); err == nil {
			_ = c.Close()
			direct = true
		}
	}
	emuLab.mu.Lock()
	v := l.VMByID(id)
	if v == nil {
		emuLab.mu.Unlock()
		return
	}
	changed := v.Direct != direct
	v.Direct = direct
	if direct {
		emuEv(id, "ok", "this computer reaches "+ip+" straight: its SSH session connects without the jump host")
	} else {
		emuEv(id, "info", "this computer does not reach "+ip+" (SSH): its SSH session goes through the host")
	}
	vv := *v
	if changed {
		_ = a.emuSave()
	}
	emuLab.mu.Unlock()
	if changed {
		a.emuRetarget(vv)
	}
}

// emuBestMgmt is the management a new lab gets: the host's network on a
// real host, else the host's NAT network (DHCP, like GNS3's NAT node), else
// a lab bridge, else port forwarding.
func emuBestMgmt(info *emu.HostInfo) string {
	vm := info.Virt != "" && info.Virt != "none" && info.Virt != "unknown"
	switch {
	case info.Sudo && info.LanKind != "" && !vm:
		return "lan"
	case info.Sudo && info.Nat != "":
		return "nat"
	case info.Sudo && info.LanKind != "":
		return "lan"
	case info.Sudo:
		return "bridge"
	}
	return "forward"
}

// emuMgmtOK says why a host cannot give its switches that management.
func emuMgmtOK(info *emu.HostInfo, mgmt string) error {
	switch mgmt {
	case "lan":
		if info.Wifi {
			return errors.New("the host's network port is Wi-Fi: switches cannot join it; use the host's NAT network or port forwarding")
		}
		if !info.Sudo || info.LanKind == "" {
			return errors.New("the host's network needs password-less sudo on the host (or a network port to join); use port forwarding")
		}
	case "nat":
		if !info.Sudo {
			return errors.New("the NAT network needs password-less sudo on the host; use port forwarding")
		}
		if info.Nat == "" && !info.Libvirt {
			return errors.New("the host has no NAT network (libvirt's virbr0): run Set Up QEMU/KVM on the host first")
		}
	case "bridge":
		if !info.Sudo {
			return errors.New("a bridge needs password-less sudo on the host; use port forwarding")
		}
	case "cloud":
		if !info.Sudo {
			return errors.New("the host's network (routed) needs password-less sudo on the host; use port forwarding")
		}
		if info.Uplink == "" || info.LanAddr == "" {
			return errors.New("the host has no network port with an address to route the switches onto")
		}
	case "forward":
	default:
		return errors.New("unknown management: " + mgmt)
	}
	return nil
}

// EmuSetMgmt changes how the lab's switches are managed (the switches must
// be stopped; it applies when they start): "lan", "nat", "bridge" or "forward".
func (a *App) EmuSetMgmt(mgmt, mgmtNet string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	host := emuLab.f.Host
	emuLab.mu.Unlock()
	return a.EmuSetHostMgmt(host, mgmt, mgmtNet)
}

// EmuSetHostMgmt changes how one host's switches are managed (they are off).
func (a *App) EmuSetHostMgmt(host, mgmt, mgmtNet string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	emuLab.mu.Unlock()
	st := a.emuPollFresh()
	emuLab.mu.Lock()
	up := false
	for _, v := range emuLab.f.Lab.VMs {
		if _, ok := st.Up[v.ID]; ok && emuHostOf(v) == host {
			up = true
		}
	}
	if up {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("stop the switches first: the management changes while they are off")
	}
	var info *emu.HostInfo
	for i := range emuLab.f.Hosts {
		if emuLab.f.Hosts[i].Session == host {
			info = &emuLab.f.Hosts[i].Info
		}
	}
	emuLab.mu.Unlock()
	if info == nil || info.Dir == "" || (mgmt == "cloud" || mgmt == "lan") && info.LanAddr == "" { // checked by an older Termwright: without the host's address
		h, err := a.EmuCheckHost(host)
		if err != nil {
			return EmuView{}, err
		}
		info = &h.Info
	}
	if err := emuMgmtOK(info, mgmt); err != nil {
		return EmuView{}, err
	}
	emuLab.mu.Lock()
	l0 := emuLab.f.Lab
	if l0 == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	l = a.emuFor(host) // its view: the management set on it is kept for the host below
	l.LanFixed = false
	if mgmt == "cloud" || mgmt == "lan" && strings.TrimSpace(mgmtNet) != "" { // lan: fixed addresses on a network without DHCP
		if err := emuCloud(l, info, mgmtNet); err != nil {
			emuLab.mu.Unlock()
			return EmuView{}, err
		}
		l.LanFixed = mgmt == "lan"
	} else if mgmt == "bridge" && strings.TrimSpace(mgmtNet) != "" {
		if _, err := netip.ParsePrefix(strings.TrimSpace(mgmtNet)); err != nil {
			emuLab.mu.Unlock()
			return EmuView{}, fmt.Errorf("management network %q: use an IPv4 network like 10.99.0.0/24", mgmtNet)
		}
		l.MgmtNet = strings.TrimSpace(mgmtNet)
	}
	if mgmt != "cloud" && !l.LanFixed && (l.Mgmt == "cloud" || l.Mgmt == "lan") {
		l.MgmtNet = "10.99.0.0/24" // the cloud's first address is no bridge network
	}
	old := l.Mgmt
	l.Mgmt, l.LanIf, l.LanKind = mgmt, info.LanIf, info.LanKind
	l0.SetNet(host, emuLab.f.Host, l.Net("", ""))
	var vms []emu.VM
	for i := range l0.VMs { // all of this host's addresses afresh, in order
		if emuHostOf(l0.VMs[i]) == host {
			l0.VMs[i].MgmtIP = ""
		}
	}
	for i := range l0.VMs {
		v := &l0.VMs[i] // the lab's own VMs
		if emuHostOf(*v) != host {
			continue
		}
		v.Direct = false // checked again once it has its address
		l.VMs = l0.VMs
		v.MgmtIP, v.Gateway = l.FreeAddress(*v)
		if old != mgmt {
			if im := emuImage(v.Image); im != nil && (strings.Contains(strings.Join(im.Day0, "\n"), "{mgmt") || strings.Contains(strings.Join(im.Day0, "\n"), "{gateway}")) {
				v.Day0Done = false // its address is part of day 0
			}
		}
	}
	for _, v := range l0.VMs {
		if emuHostOf(v) == host {
			vms = append(vms, v)
		}
	}
	fixed := l.Mgmt == "cloud" || l.Mgmt == "lan" && l.LanFixed
	_ = a.emuSave()
	emuLab.mu.Unlock()
	for _, v := range vms {
		a.emuRetarget(v)
	}
	if fixed { // the switches are off: an address that answers now is another device's
		var ips []string
		for _, v := range vms {
			if ip := strings.Split(v.MgmtIP, "/")[0]; ip != "" {
				ips = append(ips, ip)
			}
		}
		if used := a.emuAddrInUse(host, ips); len(used) > 0 {
			var l []string
			for _, v := range vms {
				if mac, ok := used[strings.Split(v.MgmtIP, "/")[0]]; ok {
					msg := strings.Split(v.MgmtIP, "/")[0] + " is already used on the network (by " + orStr(mac, "a device") + "): pick other addresses for the switches (Management)"
					emuLab.mu.Lock()
					emuEv(v.ID, "error", msg)
					emuLab.mu.Unlock()
					l = append(l, strings.Split(v.MgmtIP, "/")[0]+" ("+orStr(mac, "?")+")")
				}
			}
			view, _ := a.EmuState()
			return view, errors.New("addresses already used on the network by other devices: " + strings.Join(l, ", ") + " - the switches would clash with them; pick a free range")
		}
	}
	return a.EmuState()
}

// emuAddrInUse pings addresses from the host and says which answer (with the
// MAC address that answered): used by other devices while the switches are off.
func (a *App) emuAddrInUse(host string, ips []string) map[string]string {
	if len(ips) == 0 {
		return nil
	}
	script := "P=$(command -v ping || echo busybox ping)\nfor ip in " + strings.Join(ips, " ") + "; do if $P -c1 -W1 $ip >/dev/null 2>&1; then echo \"used $ip $(ip neigh show $ip | awk '{print $5}' | head -1)\"; fi; done\n"
	out, err := a.emuRun(host, script, time.Duration(len(ips)*2+10)*time.Second)
	if err != nil {
		return nil
	}
	used := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "used" {
			used[f[1]] = strings.Join(f[2:], "")
		}
	}
	return used
}

// emuCloud sets a lab up for the host's network, routed: spec is the first
// switch's address (its prefix length from the host's own when left out) and,
// after a space, the gateway (the host's default gateway when left out):
// "10.234.179.146/24 10.234.179.254".
func emuCloud(l *emu.Lab, info *emu.HostInfo, spec string) error {
	host, err := netip.ParsePrefix(info.LanAddr)
	if err != nil {
		return errors.New("the host's own address on its network is unknown: check the host again (Hosts)")
	}
	f := strings.Fields(spec)
	if len(f) == 0 {
		return fmt.Errorf("give the first switch's address on the host's network (%s), for example %s", host.Masked(), host.Masked().Addr().Next().Next())
	}
	first := f[0]
	if !strings.Contains(first, "/") {
		first += "/" + strconv.Itoa(host.Bits())
	}
	p, err := netip.ParsePrefix(first)
	if err != nil || !p.Addr().Is4() {
		return fmt.Errorf("%q is no IPv4 address", f[0])
	}
	if !host.Masked().Contains(p.Addr()) {
		return fmt.Errorf("%s is not on the host's network %s", p.Addr(), host.Masked())
	}
	gw := info.Gateway
	if len(f) > 1 {
		gw = f[1]
	}
	if g, err := netip.ParseAddr(gw); gw != "" && (err != nil || !host.Masked().Contains(g)) {
		return fmt.Errorf("the gateway %s is not on the host's network %s", gw, host.Masked())
	}
	last := p.Addr()
	for i := 1; i < max(len(l.VMs), 1); i++ {
		last = last.Next()
	}
	for _, a := range []string{host.Addr().String(), gw} {
		if aa, err := netip.ParseAddr(a); err == nil && p.Addr().Compare(aa) <= 0 && aa.Compare(last) <= 0 {
			return fmt.Errorf("the switches' addresses %s - %s take %s (the host or the gateway): start after it", p.Addr(), last, a)
		}
	}
	if !host.Masked().Contains(last) || !host.Masked().Contains(last.Next()) { // the last address is the broadcast
		return fmt.Errorf("%d switches from %s run past the network %s", len(l.VMs), p.Addr(), host.Masked())
	}
	l.MgmtNet, l.CloudGw, l.CloudIf, l.HostIP = p.String(), gw, info.Uplink, host.Addr().String()
	return nil
}

// emuWithTail adds what the console showed to an error (unless it says so already).
func emuWithTail(err error, c *emu.Console) error {
	if err == nil || strings.Contains(err.Error(), "the console showed") {
		return err
	}
	return fmt.Errorf("%w - the console showed: %s", err, c.Tail(220))
}

// emuReadFacts reads the software version and serial number the switch reports (the image's facts commands).
func (a *App) emuReadFacts(ctx context.Context, c *emu.Console, im *emu.Image, v emu.VM) {
	if len(im.Facts) == 0 {
		return
	}
	fw, sn := "", ""
	for _, f := range im.Facts {
		var out string
		for _, cmd := range strings.Split(f.Cmd, "\n") {
			if cmd = strings.TrimSpace(cmd); cmd != "" {
				o, err := emu.Ask(ctx, c, im, cmd)
				if err != nil {
					emuLogf(emuLvDebug, emuHostOfID0(v), v.Name, "facts: %s: %v", cmd, err)
					break
				}
				out += o
			}
		}
		out = emu.CleanConsole(out)
		grab := func(re string) string {
			if re == "" {
				return ""
			}
			r, err := regexp.Compile(re)
			if err != nil {
				return ""
			}
			m := r.FindStringSubmatch(out)
			for _, g := range m[min(1, len(m)):] { // the first group that matched (a pattern can have alternatives)
				if g = strings.TrimSpace(g); g != "" {
					return g
				}
			}
			return ""
		}
		fw, sn = orStr(fw, grab(f.Version)), orStr(sn, grab(f.Serial))
	}
	if fw == "" && sn == "" {
		return
	}
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	if cur := emuLab.f.Lab.VMByID(v.ID); cur != nil && (cur.FW != fw || cur.HWSerial != sn) {
		cur.FW, cur.HWSerial = orStr(fw, cur.FW), orStr(sn, cur.HWSerial)
		_ = a.emuSave()
		emuEv(v.ID, "info", fmt.Sprintf("it reports software %s, serial number %s", orStr(cur.FW, "?"), orStr(cur.HWSerial, "?")))
	}
}

// emuHostOfID0 is a VM's host for the log (no lock needed: the name only).
func emuHostOfID0(v emu.VM) string { return v.Host }

// emuCheckPersona asks the switch, on its CLI, which model it is (the
// profile's check command) once per boot: a switch that booted as the plain
// image shows a persona error.
func (a *App) emuCheckPersona(ctx context.Context, c *emu.Console, im *emu.Image, v emu.VM) {
	emuLab.mu.Lock()
	cur := emuLab.f.Lab.VMByID(v.ID)
	if im.Persona == nil || im.Persona.Check == "" || cur == nil || cur.Persona == nil || cur.Persona.Applied == "" || len(cur.Persona.Slots) == 0 || emuLab.pcheck[v.ID] {
		emuLab.mu.Unlock()
		return
	}
	emuLab.pcheck[v.ID] = true
	emuLab.state[v.ID] = "persona check"
	model := cur.Persona.Slots[0].Model
	emuLab.mu.Unlock()
	var out string
	var err error
	for _, cmd := range strings.Split(im.Persona.Check, "\n") { // "enable\nshow inventory"
		if cmd = strings.TrimSpace(cmd); cmd != "" && err == nil {
			var o string
			o, err = emu.Ask(ctx, c, im, cmd)
			out += o
		}
	}
	// not that model: is the file where the switch looks for it?
	where := ""
	if err == nil && !strings.Contains(strings.ToLower(out), strings.ToLower(model)) {
		dir, base := path.Dir(im.Persona.File), path.Base(im.Persona.File)
		if ls, lerr := emu.Ask(ctx, c, im, "ls "+dir); lerr == nil {
			ls = emu.CleanConsole(ls)
			switch {
			case strings.Contains(ls, base):
				where = fmt.Sprintf("; %s is in %s on the switch, so it read it and kept its first model: this image may take a persona only on its very first boot - Back to Day 0 writes it before that", base, dir)
			case strings.Contains(strings.ToLower(ls), "no such") || strings.Contains(strings.ToLower(ls), "invalid") || strings.Contains(strings.ToLower(ls), "error"):
			default:
				where = fmt.Sprintf("; %s is not in %s on the switch: it was written to another filesystem of the disk", base, dir)
			}
		}
	}
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	switch {
	case err != nil:
		emuLab.pinfo[v.ID] = "not checked: " + err.Error()
		emuEv(v.ID, "warn", "persona "+emuLab.pinfo[v.ID])
	case strings.Contains(strings.ToLower(out), strings.ToLower(model)):
		emuLab.pinfo[v.ID] = "checked: " + strings.ReplaceAll(im.Persona.Check, "\n", " + ") + " shows " + model
		delete(emuLab.perr, v.ID)
		if c := emuLab.f.Lab.VMByID(v.ID); c != nil && c.Persona != nil {
			c.Persona.Took, c.Persona.Fail = c.Persona.Applied, ""
			_ = a.emuSave()
		}
		emuEv(v.ID, "ok", "persona "+emuLab.pinfo[v.ID])
	default:
		defer func() {
			if c := emuLab.f.Lab.VMByID(v.ID); c != nil && c.Persona != nil {
				c.Persona.Took, c.Persona.Fail = "", emuLab.perr[v.ID]
				_ = a.emuSave()
			}
			emuEv(v.ID, "error", "no persona: "+emuLab.perr[v.ID])
		}()
		emuLab.perr[v.ID] = fmt.Sprintf("the switch did not take the persona: %q does not show %s (it reads %s at boot - see where it was written below)%s", strings.ReplaceAll(im.Persona.Check, "\n", " + "), model, im.Persona.File, where)
		o := emu.CleanConsole(out)
		o = regexp.MustCompile(`\*{5,}|Command Execution Time:[^\n]*`).ReplaceAllString(o, " ")
		if o := strings.Join(strings.Fields(o), " "); o != "" {
			if len(o) > 200 {
				o = o[:200] + "…"
			}
			emuLab.perr[v.ID] += " - it printed: " + o
		}
	}
}

// emuSetAddress keeps a VM's management address (learned from DHCP) and
// points its SSH session there.
func (a *App) emuSetAddress(id, addr string) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	path := emuLab.f.Sessions[id]
	if l != nil {
		if v := l.VMByID(id); v != nil {
			v.MgmtIP, v.AddrOn = addr, addr
		}
	}
	delete(emuLab.errs, id)
	emuLab.addrOK[id] = true
	emuEv(id, "ok", "has the address "+strings.Split(addr, "/")[0]+" - its SSH session points there")
	_ = a.emuSave()
	emuLab.mu.Unlock()
	if s, err := a.store.Get(path); err == nil {
		s.Hostname, s.Port = strings.Split(addr, "/")[0], 22
		_, _ = a.store.Save(s, path)
	}
}

// emuPoll reads the VMs' state on the host (at most every 2 s; tails: with
// console log ends for the ones not ready yet).
// emuPollFresh reads the VMs' state now: it waits for a poll already
// running (whose answer may be from before a stop) and polls again.
func (a *App) emuPollFresh() emu.Status {
	for i := 0; i < 100; i++ {
		emuLab.mu.Lock()
		busy := emuLab.polling
		emuLab.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	return a.emuPoll(true)
}

func (a *App) emuPoll(force bool) emu.Status {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil || len(l.VMs) == 0 || emuLab.polling || !force && time.Since(emuLab.statusAt) < 2*time.Second {
		st := emuLab.status
		emuLab.mu.Unlock()
		return st
	}
	emuLab.polling = true
	main := emuLab.f.Host
	hosts := l.Hosts(main)
	scripts := map[string]string{}
	for _, h := range hosts {
		var tails []string
		ready := map[string]string{}
		for _, v := range l.VMs {
			if l.HostOf(v, main) == h {
				tails = append(tails, v.ID)
				if im := emuImage(v.Image); im != nil && !emuLab.ready[v.ID] {
					ready[v.ID] = im.Ready
				}
			}
		}
		scripts[h] = a.emuFor(h).StatusScript(tails, ready)
	}
	emuLab.mu.Unlock()
	// every host of the lab at once
	outs, errs := map[string]string{}, map[string]error{}
	var wg sync.WaitGroup
	var omu sync.Mutex
	for _, h := range hosts {
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			out, err := a.emuRun(h, scripts[h], 20*time.Second)
			omu.Lock()
			outs[h], errs[h] = out, err
			omu.Unlock()
		}(h)
	}
	wg.Wait()
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	emuLab.polling = false
	if emuLab.hostStatus == nil {
		emuLab.hostStatus = map[string]emu.Status{}
	}
	now := time.Now()
	dt := now.Sub(emuLab.ticksAt).Seconds()
	ticks := map[string]float64{}
	for _, h := range hosts {
		if errs[h] != nil {
			continue // its switches keep their last state
		}
		st := emu.ParseStatus(outs[h], base64.StdEncoding.DecodeString)
		// CPU use since the last poll (ps would give the average since the VM started)
		if dt > 0.5 && st.Clk > 0 {
			for id, t := range st.Up {
				if prev, ok := emuLab.ticks[id]; ok && t >= prev {
					emuLab.cpu[id] = (t - prev) / st.Clk / dt
				}
			}
		}
		for id, t := range st.Up {
			ticks[id] = t
		}
		emuLab.hostStatus[h] = st
	}
	emuLab.ticks, emuLab.ticksAt = ticks, now
	// the lab's status: every host's switches; the host figures are its own host's
	m := emu.Status{Up: map[string]float64{}, RSS: map[string]int64{}, Log: map[string]string{}, Ready: map[string]bool{}, Leases: map[string]string{}}
	if st, ok := emuLab.hostStatus[main]; ok {
		m.Clk, m.MemTotal, m.MemFree, m.Cores, m.Load, m.NatNet = st.Clk, st.MemTotal, st.MemFree, st.Cores, st.Load, st.NatNet
	}
	for _, h := range hosts {
		st := emuLab.hostStatus[h]
		for k, v := range st.Up {
			m.Up[k] = v
		}
		for k, v := range st.RSS {
			m.RSS[k] = v
		}
		for k, v := range st.Log {
			m.Log[k] = v
		}
		for k, v := range st.Ready {
			m.Ready[k] = v
		}
	}
	for _, v := range l.VMs {
		if _, up := m.Up[v.ID]; !up {
			delete(emuLab.ready, v.ID)
		} else if im := emuImage(v.Image); im != nil && !emuLab.ready[v.ID] && (im.ReadyIn(m.Log[v.ID]) || m.Ready[v.ID]) {
			emuLab.ready[v.ID] = true
			emuEv(v.ID, "ok", "booted - the console shows "+orStr(im.Ready, "its prompt"))
		} else if im != nil && !emuLab.ready[v.ID] {
			// what the console says went wrong (once in a while each): the switch reboots on its own and seems to be booting for ever
			for _, f := range im.BootFailIn(m.Log[v.ID]) {
				if k := v.ID + "|fail|" + emuDigits.ReplaceAllString(f, "#"); time.Since(emuLab.bootAns[k]) > 10*time.Minute { // the same line with other numbers (seconds, addresses) once
					emuLab.bootAns[k] = time.Now()
					hint := ""
					if strings.Contains(f, "Illegal instruction") {
						hint = " - its CPU model lacks an instruction the image uses (a -cpu in the image's More QEMU options?)"
					}
					emuEv(v.ID, "error", "its boot failed: "+f+hint)
				}
			}
			// a menu the boot stopped at (a key reached it early): its answer, once in a while
			if ans, ok := im.BootStop(m.Log[v.ID]); ok && time.Since(emuLab.bootAns[v.ID+"|"+ans.Match]) > 30*time.Second {
				emuLab.bootAns[v.ID+"|"+ans.Match] = time.Now() // each menu once in a while (one leads to the next)
				emuGo(func() { a.emuBootAnswer(emuHostOf(v), v, ans) })
			}
		}
	}
	for _, h := range hosts {
		st, ok := emuLab.hostStatus[h]
		if !ok || l.Net(h, main).Mgmt != "nat" { // addresses from the NAT network's DHCP leases: no CLI needed (any image)
			continue
		}
		bits := "24"
		if _, b, ok := strings.Cut(st.NatNet, "/"); ok {
			bits = b
		}
		for i := range l.VMs {
			v := &l.VMs[i]
			if _, up := st.Up[v.ID]; !up || l.HostOf(*v, main) != h {
				continue
			}
			ip := st.Leases[strings.ToLower(l.MAC(*v, 0))]
			if ip == "" { // an OS that answers with its own management MAC
				ip = st.Leases[strings.ToLower(l.MgmtMAC(*v, emuImage(v.Image)))]
			}
			if ip != "" && v.MgmtIP != ip+"/"+bits {
				v.Gateway, v.MgmtIP = strings.Split(st.NatNet, "/")[0], ip+"/"+bits
				go func(id, name, addr, host string) {
					a.emuSetAddress(id, addr)
					a.emuNotify(nil, name+" has the address "+strings.Split(addr, "/")[0])
					for end := time.Now().Add(5 * time.Minute); time.Now().Before(end); time.Sleep(5 * time.Second) {
						emuLab.mu.Lock()
						ready := emuLab.ready[id]
						emuLab.mu.Unlock()
						if ready {
							a.emuReach(host, id)
							return
						}
					}
				}(v.ID, v.Name, ip+"/"+bits, h)
			}
		}
	}
	emuLab.status = m
	emuLab.statusAt = now
	return emuLab.status
}

// EmuState returns the emulated lab (and reads its VMs' state on the host).
func (a *App) EmuState() (EmuView, error) {
	prof := a.emuPackProfiles()
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.packProfiles = prof
	// a host without KVM is looked at again each minute: KVM turned on there shows (and counts) at once
	for _, h := range emuLab.f.Hosts {
		if h.Info.KVM != "yes" && time.Since(emuKVMSeen[h.Session]) > time.Minute {
			emuKVMSeen[h.Session] = time.Now()
			go func(s string) { _, _ = a.EmuHostKVM(s) }(h.Session)
		}
	}
	if up, pw := emuTopUp(emuLab.f.Images, prof); up {
		_ = a.emuSave()
		if l := emuLab.f.Lab; l != nil && len(pw) > 0 { // their sessions take the factory password
			var vs []emu.VM
			for _, v := range l.VMs {
				if slices.Contains(pw, v.Image) {
					vs = append(vs, v)
				}
			}
			emuGo(func() {
				for _, v := range vs {
					a.emuLogins(v)
				}
			})
		}
	}
	has := emuLab.f.Lab != nil && len(emuLab.f.Lab.VMs) > 0
	emuLab.mu.Unlock()
	if has {
		// the switches' state comes from the hosts over SSH (seconds with a slow
		// host): it is fetched in the background, the view answers at once with
		// the last one, and the next refresh shows the new
		emuGo(func() { a.emuPoll(false) })
	}
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	return a.emuView(), nil
}

func (a *App) emuView() EmuView {
	f := &emuLab.f
	v := EmuView{Kind: "emu", DownloadDir: a.emuDownloadDir(), DownloadQueue: slices.DeleteFunc(append([]string{emuLab.dlCur}, emuLab.dlQueue...), func(x string) bool { return x == "" }), Events: append([]EmuEvent(nil), emuLab.events[max(0, len(emuLab.events)-200):]...), DeviceTypes: a.emuDeviceTypes(emuLab.packProfiles), Templates: f.Templates, Folder: f.Folder, Host: f.Host, Hosts: f.Hosts, Sessions: f.Sessions, Consoles: f.Consoles, OS: map[string]LabOS{}, Topology: f.Topology}
	for i := range f.Hosts {
		if f.Hosts[i].Session == f.Host {
			v.HostInfo = &f.Hosts[i].Info
		}
	}
	onHost := map[string]bool{}
	for _, im := range f.Images {
		if im.Version == "" {
			im.Version = versionIn(im.FileName)
		}
		iv := EmuImageView{Image: im, Upload: emuLab.upload[im.ID], Download: emuLab.download[im.ID]}
		iv.Password = ""
		if pw, _ := secrets.Lookup(emuImageKey(im.ID)); pw != "" {
			iv.HasPassword = true
		}
		if im.File != "" {
			if _, err := os.Stat(im.File); err != nil {
				iv.Missing = true
			}
		} else {
			iv.Missing = true
		}
		for _, d := range im.Disks {
			if d.File == "" {
				iv.Missing = true
			} else if _, err := os.Stat(d.File); err != nil {
				iv.Missing = true
			}
		}
		v.Images = append(v.Images, iv)
		dt := orStr(im.DeviceType, "VM")
		v.OS[dt] = LabOS{Name: dt, Short: strings.ToUpper(dt)}
		v.Models = append(v.Models, LabModel{SKU: im.ID, Family: "Images", OS: []string{dt}, OSName: map[string]string{dt: im.Name}, Ports: im.DataPorts(),
			Layout: fmt.Sprintf("%d vCPU · %d MB · %d × %s", orInt(im.CPUs, 1), orInt(im.RAM, 1024), im.DataPorts(), orStr(im.NIC, "e1000"))})
	}
	l := f.Lab
	if l == nil {
		return v
	}
	v.Mgmt, v.MgmtNet, v.KVM, v.CloudGw, v.LanFixed, v.Fast = l.Mgmt, l.MgmtNet, l.KVM, l.CloudGw, l.LanFixed, !l.NoFast
	v.IconStyle = orStr(f.IconStyle, "box")
	st := emuLab.status
	v.Usage.HostCPUs, v.Usage.HostMemTotal, v.Usage.HostMemFree, v.Usage.Load = st.Cores, st.MemTotal, st.MemFree, st.Load
	for _, h := range l.Hosts(f.Host) {
		hs, n := emuLab.hostStatus[h], l.Net(h, f.Host)
		u := EmuHostUse{Host: h, Name: h[strings.LastIndex(h, "/")+1:], Addr: l.Addrs[h], Mgmt: n.Mgmt, MgmtNet: n.MgmtNet, CloudGw: n.CloudGw, LanFixed: n.LanFixed,
			CPUs: hs.Cores, MemTotal: hs.MemTotal, MemFree: hs.MemFree, Load: hs.Load, Main: h == f.Host}
		for _, vm := range l.VMs {
			if l.HostOf(vm, f.Host) != h {
				continue
			}
			u.Switches++
			if _, up := hs.Up[vm.ID]; up {
				u.Running++
				u.VCPUs += vm.CPUs
				u.RAM += vm.RAM
				u.Cores += emuLab.cpu[vm.ID]
			}
		}
		v.HostUse = append(v.HostUse, u)
	}
	for _, vm := range l.VMs {
		im := emuImage(vm.Image)
		d := EmuDevice{ID: vm.ID, Hostname: vm.Name, Image: vm.Image, MgmtIP: vm.MgmtIP, MAC: l.MAC(vm, 0), X: vm.X, Y: vm.Y, CPUs: vm.CPUs, RAM: vm.RAM,
			Console: l.ConsolePort(vm), Port: l.SSHPort(vm), Error: emuLab.errs[vm.ID]}
		if im != nil {
			d.Model, d.OS, d.Ports = im.Name, orStr(im.DeviceType, "VM"), im.DataPorts()
			onHost[im.ID] = true
		}
		vl := a.emuFor(emuHostOf(vm)) // its host's management
		d.Host = emuHostOf(vm)
		switch {
		case (vl.Mgmt == "lan" || vl.Mgmt == "nat") && vm.MgmtIP == "":
			d.SSH = "DHCP (not known yet)"
		case vl.Mgmt == "nat":
			d.SSH = strings.Split(vm.MgmtIP, "/")[0] + ":22 through the host"
		case vl.Mgmt == "bridge" || vl.Mgmt == "cloud":
			d.SSH = strings.Split(vm.MgmtIP, "/")[0] + ":22 through the host"
		case vl.Mgmt == "lan":
			d.SSH = strings.Split(vm.MgmtIP, "/")[0] + ":22"
		default:
			d.SSH = "127.0.0.1:" + strconv.Itoa(d.Port) + " on the host"
		}
		_, up := st.Up[vm.ID]
		// a fixed address is set by day 0: until then the switch does not have it
		if up && vl.Fixed() && len(imOrNil(im).AddrSet) > 0 && !emuLab.addrOK[vm.ID] && vm.AddrOn != vm.MgmtIP {
			d.SSH += " · " + "planned, not on the switch yet"
		}
		// % of its vCPUs; QEMU's own threads (disk, network, emulation) can add a little more
		d.CPU = emuLab.cpu[vm.ID] * 100 / float64(orInt(vm.CPUs, 1))
		d.Cores = emuLab.cpu[vm.ID]
		d.MemUsed = st.RSS[vm.ID]
		d.Serial, d.HWSerial = vm.Serial, vm.HWSerial
		if im != nil {
			d.Version = orStr(vm.FW, im.Version)
		}
		d.MovingTo = emuLab.moveTo[vm.ID]
		switch s := emuLab.state[vm.ID]; {
		case s == "uploading":
			d.State, d.StateText = "uploading", "copying the image"
			if im != nil && emuLab.upload[im.ID] != "" {
				d.StateText += " · " + emuLab.upload[im.ID]
			}
		case s == "starting":
			d.State, d.StateText = "booting", "starting"
		case s == "persona":
			d.State, d.StateText = "booting", "writing its persona file"
		case s == "dhcp":
			d.State, d.StateText, d.Running = "day0", "waiting for its DHCP address", true
		case s == "persona check":
			d.State, d.StateText, d.Running = "day0", "checking its persona", true
		case strings.HasPrefix(s, "day0"):
			d.State, d.StateText, d.Running = "day0", "day-0 config "+strings.TrimPrefix(strings.TrimPrefix(s, "day0"), " "), true
		case strings.HasPrefix(s, "moving"):
			d.State, d.StateText, d.Running = "day0", "moving · "+strings.TrimPrefix(s, "moving: "), up
		case s == "saving":
			d.State, d.StateText, d.Running = "day0", "saving its running state", true
		case !up:
			d.State, d.StateText = "stopped", "stopped"
			if vm.FastKey != "" {
				d.StateText, d.Saved = "stopped · state saved", true
				v.Usage.Saved += orInt(vm.RAM, 1024) // about what the saved state takes on the host's disk
			}
			if d.Error != "" {
				d.State, d.StateText = "failed", "failed"
			}
		case im != nil && !emuLab.ready[vm.ID]:
			d.State, d.StateText, d.Running = "booting", "booting", true
			if t, ok := emuLab.starting[vm.ID]; ok {
				d.StateText += " · " + fmtDur(time.Since(t))
			}
		default:
			d.State, d.StateText, d.Running = "running", "running", true
			if d.Error != "" {
				d.StateText = "running · day-0 failed"
			} else if emuLab.perr[vm.ID] != "" {
				d.StateText = "running · no persona"
			}
		}
		d.DiskGB, d.Template, d.Persona, d.PersonaError, d.PersonaInfo = vm.DiskGB, vm.Template, vm.Persona, emuLab.perr[vm.ID], emuLab.pinfo[vm.ID]
		d.Role = vm.Role
		if p, ok := l.Icons[emu.IconKey(vm)]; ok {
			d.Role, d.IconStyle = orStr(p.Role, d.Role), p.Style
		}
		if im != nil {
			for _, n := range vm.Uplinks {
				d.Uplinks = append(d.Uplinks, im.PortNameOf(n))
			}
		}
		if im != nil && im.Persona != nil && vm.Persona != nil {
			if txt, err := im.Persona.Render(vm, vm.Persona); err == nil && emu.Hash(txt) == vm.Persona.Applied {
				// written; with a check command it counts once the switch showed the model (kept across restarts of Termwright)
				switch {
				case im.Persona.Check == "" || vm.Persona.Took == vm.Persona.Applied:
					d.PersonaOK = true
				case vm.Persona.Fail != "" && d.PersonaError == "":
					d.PersonaError = vm.Persona.Fail
				case d.PersonaInfo == "":
					d.PersonaInfo = "written, not checked on the switch yet"
				}
			}
		}
		if up {
			v.Usage.Running++
			v.Usage.VCPUs += orInt(vm.CPUs, 1)
			v.Usage.RAM += orInt(vm.RAM, 1024)
			v.Usage.Cores += emuLab.cpu[vm.ID]
		}
		v.Usage.DiskGB += vm.DiskGB
		if d.Running {
			v.Running = true
		}
		v.Devices = append(v.Devices, d)
	}
	for _, c := range l.Cables {
		a, b := l.VMByID(c.A), l.VMByID(c.B)
		if a == nil || b == nil {
			continue
		}
		ia, ib := emuImage(a.Image), emuImage(b.Image)
		lv := EmuLinkView{A: c.A, B: c.B, APort: strconv.Itoa(c.APort), BPort: strconv.Itoa(c.BPort), Down: c.Down}
		if ia != nil {
			lv.APort = ia.PortNameOf(c.APort)
		}
		if ib != nil {
			lv.BPort = ib.PortNameOf(c.BPort)
		}
		_, ua := st.Up[c.A]
		_, ub := st.Up[c.B]
		lv.Up = ua && ub && !c.Down
		if g := l.LagOf(c.A, c.APort); g != nil {
			lv.LagA = &EmuLagView{Name: g.Name, Mode: g.Mode, Source: g.Source}
		}
		if g := l.LagOf(c.B, c.BPort); g != nil {
			lv.LagB = &EmuLagView{Name: g.Name, Mode: g.Mode, Source: g.Source}
		}
		lv.Lldp = emuLab.lldp[emuCableKey(c)]
		v.Links = append(v.Links, lv)
	}
	for i := range v.Images {
		v.Images[i].OnHost = onHost[v.Images[i].ID] && !v.Images[i].Missing
	}
	v.Exists = len(v.Devices) > 0
	return v
}

func fmtDur(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func (a *App) emuIDs(ids []string) (string, []string) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		return "", nil
	}
	if len(ids) == 0 {
		for _, v := range l.VMs {
			ids = append(ids, v.ID)
		}
	}
	return emuLab.f.Host, ids
}

// EmuStart starts VMs (all when ids is empty).
func (a *App) EmuStart(ids []string) (EmuView, error) {
	defer emuAs("user")()
	host, ids := a.emuIDs(ids)
	if host == "" {
		return EmuView{}, errors.New("there is no emulated lab")
	}
	emuLab.mu.Lock()
	var vs []emu.VM
	for _, id := range ids {
		if v := emuLab.f.Lab.VMByID(id); v != nil {
			vs = append(vs, *v)
		}
	}
	emuLab.mu.Unlock()
	for _, v := range vs {
		a.emuLogins(v)
	}
	hosts, by := a.emuGroups(ids)
	for _, h := range hosts { // each host starts its switches
		emuGo(func() { a.emuStart(h, by[h]) })
	}
	time.Sleep(50 * time.Millisecond)
	return a.EmuState()
}

// EmuStop stops VMs (all when ids is empty). A switch that runs (booted, day 0
// done) has its running state saved into its disk first, unless the lab has
// Fast start off: it then starts again in seconds. The others: like pulling the power.
func (a *App) EmuStop(ids []string) (EmuView, error) {
	defer emuAs("user")()
	return a.emuStop(ids, true)
}

// EmuShutDown stops VMs without saving their state: they boot next time.
func (a *App) EmuShutDown(ids []string) (EmuView, error) {
	defer emuAs("user")()
	return a.emuStop(ids, false)
}

func (a *App) emuStop(ids []string, save bool) (EmuView, error) {
	host, ids := a.emuIDs(ids)
	if host == "" {
		return EmuView{}, errors.New("there is no emulated lab")
	}
	st := a.emuPollFresh()
	hosts, by := a.emuGroups(ids)
	for _, h := range hosts {
		emuLab.mu.Lock()
		l := a.emuFor(h)
		var keep, cold []string
		for _, id := range by[h] {
			_, up := st.Up[id]
			if save && !emuLab.f.Lab.NoFast && up && emuLab.ready[id] && !emuLab.day0[id] && emuLab.state[id] == "" {
				keep = append(keep, id)
				emuLab.state[id] = "saving"
			} else {
				cold = append(cold, id)
				emuEv(id, "info", "stopped")
			}
		}
		script := l.StopScript(cold) + l.DropFastScript(cold)
		if len(keep) > 0 {
			script = l.SaveScript(keep) + script
		}
		for _, id := range cold {
			if v := emuLab.f.Lab.VMByID(id); v != nil {
				v.FastKey = ""
			}
		}
		emuLab.mu.Unlock()
		out, err := a.emuRun(h, script, 15*time.Minute)
		emuLab.mu.Lock()
		for _, id := range keep {
			delete(emuLab.state, id)
		}
		for _, line := range strings.Split(out, "\n") {
			f := strings.SplitN(strings.TrimSpace(line), " ", 3)
			if len(f) >= 2 && slices.Contains(keep, f[1]) {
				v := emuLab.f.Lab.VMByID(f[1])
				im := emuImage("")
				if v != nil {
					im = emuImage(v.Image)
				}
				switch {
				case f[0] == "saved" && v != nil && im != nil:
					v.FastKey = l.FastKey(*v, im)
					emuEv(f[1], "ok", "stopped with its running state saved - it starts again in seconds")
				case f[0] == "fail":
					emuEv(f[1], "warn", "stopped, but its running state was not saved ("+strings.Join(f[2:], " ")+") - it boots next time")
				}
			}
		}
		_ = a.emuSave()
		emuLab.mu.Unlock()
		if err != nil {
			return EmuView{}, err
		}
	}
	a.emuPollFresh()
	return a.EmuState()
}

// EmuSetFast turns Fast start on or off for the lab: off, Stop shuts the
// switches down and the saved states go (they boot each time).
func (a *App) EmuSetFast(on bool) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	l.NoFast = !on
	if !on {
		for i := range l.VMs {
			l.VMs[i].FastKey = "" // they boot next time; the snapshots go when they start
		}
	}
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuRestart stops and starts VMs (a changed cable needs this).
func (a *App) EmuRestart(ids []string) (EmuView, error) {
	defer emuAs("user")()
	if _, err := a.EmuShutDown(ids); err != nil { // a real boot
		return EmuView{}, err
	}
	return a.EmuStart(ids)
}

// EmuReset stops VMs, throws their disks away and boots them as new: day 0 is typed again.
func (a *App) EmuReset(ids []string) (EmuView, error) {
	defer emuAs("user")()
	host, ids := a.emuIDs(ids)
	if host == "" {
		return EmuView{}, errors.New("there is no emulated lab")
	}
	_ = host
	hosts, by := a.emuGroups(ids)
	for _, h := range hosts {
		emuLab.mu.Lock()
		script := a.emuFor(h).ResetScript(by[h])
		for _, id := range by[h] {
			if v := emuLab.f.Lab.VMByID(id); v != nil {
				// a new disk from the image: its persona file is written again (before its first boot), nothing saved
				v.Day0Done, v.Direct, v.FastKey, v.AddrOn = false, false, "", ""
				delete(emuLab.addrOK, id)
				if v.Persona != nil {
					v.Persona.Applied, v.Persona.Took, v.Persona.Fail = "", "", ""
				}
			}
			delete(emuLab.perr, id)
			delete(emuLab.pinfo, id)
			delete(emuLab.personaTried, id)
			delete(emuLab.personaAfterBoot, id)
		}
		_ = a.emuSave()
		emuLab.mu.Unlock()
		if _, err := a.emuRun(h, script, time.Minute); err != nil {
			return EmuView{}, err
		}
	}
	return a.EmuStart(ids)
}

// EmuRemove removes VMs from the lab: they stop, their disks on the host are
// deleted, and their sessions too when sessions is set.
func (a *App) EmuRemove(ids []string, sessions bool) (EmuView, error) {
	defer emuAs("user")()
	host, ids := a.emuIDs(ids)
	if host == "" {
		return EmuView{}, errors.New("there is no emulated lab")
	}
	_ = host
	hosts, by := a.emuGroups(ids)
	for _, h := range hosts {
		emuLab.mu.Lock()
		on := 0
		for _, v := range emuLab.f.Lab.VMs {
			if emuHostOf(v) == h {
				on++
			}
		}
		script := a.emuFor(h).ResetScript(by[h])
		if len(by[h]) >= on { // the host's last switches: the lab's bridge there goes too
			script += "sudo -n ip link del " + emuLab.f.Lab.Bridge() + " 2>/dev/null\n"
		}
		emuLab.mu.Unlock()
		_, _ = a.emuRun(h, script, time.Minute)
	}
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	gone := map[string]bool{}
	for _, id := range ids {
		gone[id] = true
	}
	var keep []emu.VM
	for _, v := range l.VMs {
		if !gone[v.ID] {
			keep = append(keep, v)
		}
	}
	var cables []emu.Cable
	for _, c := range l.Cables {
		if !gone[c.A] && !gone[c.B] {
			cables = append(cables, c)
		}
	}
	l.VMs, l.Cables = keep, cables
	var paths []string
	for id := range gone {
		if sessions {
			paths = append(paths, emuLab.f.Sessions[id], emuLab.f.Consoles[id])
		}
		delete(emuLab.f.Sessions, id)
		delete(emuLab.f.Consoles, id)
		delete(emuLab.errs, id)
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
	return a.EmuState()
}

// emuPort reads a port name ("1/3" or "3") of a VM as its number.
func emuPort(im *emu.Image, name string) int {
	for n := 1; n <= im.DataPorts(); n++ {
		if im.PortNameOf(n) == name || strconv.Itoa(n) == name {
			return n
		}
	}
	return 0
}

// EmuConnect adds a cable; running VMs get it when they restart.
func (a *App) EmuConnect(k EmuLinkView) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	va, vb := l.VMByID(k.A), l.VMByID(k.B)
	if va == nil || vb == nil || k.A == k.B {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("choose two switches")
	}
	pa, pb := emuPort(emuImage(va.Image), k.APort), emuPort(emuImage(vb.Image), k.BPort)
	for _, c := range l.Cables {
		if c.A == k.A && c.APort == pa || c.B == k.A && c.BPort == pa || c.A == k.B && c.APort == pb || c.B == k.B && c.BPort == pb {
			emuLab.mu.Unlock()
			return EmuView{}, errors.New("a port has a cable already")
		}
	}
	if slices.Contains(va.Uplinks, pa) || slices.Contains(vb.Uplinks, pb) {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("a port is on the host's network: take it off first")
	}
	if l.TesterOn(k.A, pa) != nil || l.TesterOn(k.B, pb) != nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("a port has a test port: remove it first (Traffic)")
	}
	if pa == 0 || pb == 0 {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("unknown port")
	}
	l.Cables = append(l.Cables, emu.Cable{A: k.A, APort: pa, B: k.B, BPort: pb})
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuDisconnect removes the cable on a port.
func (a *App) EmuDisconnect(id, port string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	v := l.VMByID(id)
	if v == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such switch")
	}
	p := emuPort(emuImage(v.Image), port)
	var keep []emu.Cable
	for _, c := range l.Cables {
		if !(c.A == id && c.APort == p || c.B == id && c.BPort == p) {
			keep = append(keep, c)
		}
	}
	l.Cables = keep
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuSetLink pulls a cable (up=false) or plugs it in again, on running VMs at once (QEMU monitor).
func (a *App) EmuSetLink(id, port string, up bool) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	v := l.VMByID(id)
	if v == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such switch")
	}
	p := emuPort(emuImage(v.Image), port)
	type end struct {
		mon, n int
		host   string
	}
	var ends []end
	for i := range l.Cables {
		c := &l.Cables[i]
		if c.A == id && c.APort == p || c.B == id && c.BPort == p {
			c.Down = !up
			if x := l.VMByID(c.A); x != nil {
				ends = append(ends, end{l.MonitorPort(*x), c.APort, emuHostOf(*x)})
			}
			if x := l.VMByID(c.B); x != nil {
				ends = append(ends, end{l.MonitorPort(*x), c.BPort, emuHostOf(*x)})
			}
		}
	}
	_ = a.emuSave()
	emuLab.mu.Unlock()
	for _, e := range ends { // each end on its own host
		cl, err := a.emuClient(context.Background(), e.host)
		if err != nil {
			continue
		}
		{
			if conn, err := cl.Dial("tcp", "127.0.0.1:"+strconv.Itoa(e.mon)); err == nil {
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				_, _ = fmt.Fprintf(conn, "set_link n%d %s\r\n", e.n, map[bool]string{true: "on", false: "off"}[up])
				buf := make([]byte, 512)
				_, _ = conn.Read(buf)
				time.Sleep(200 * time.Millisecond)
				_ = conn.Close()
			}
		}
	}
	return a.EmuState()
}

// EmuPlace keeps where a VM is drawn.
func (a *App) EmuPlace(id string, x, y int) error {
	defer emuAs("user")()
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	a.emuLoad()
	if l := emuLab.f.Lab; l != nil {
		if v := l.VMByID(id); v != nil {
			v.X, v.Y = x, y
			return a.emuSave()
		}
	}
	return nil
}

// EmuFreePorts lists a VM's data ports without a cable.
func (a *App) EmuFreePorts(id string) []string {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	a.emuLoad()
	return a.emuFreePortsLocked(id)
}

// emuFreePortsLocked: EmuFreePorts with emuLab.mu held.
func (a *App) emuFreePortsLocked(id string) []string {
	l := emuLab.f.Lab
	if l == nil {
		return nil
	}
	v := l.VMByID(id)
	if v == nil {
		return nil
	}
	im := emuImage(v.Image)
	if im == nil {
		return nil
	}
	used := map[int]bool{}
	for _, n := range v.Uplinks {
		used[n] = true
	}
	for _, t := range l.Testers {
		if t.VM == id {
			used[t.Port] = true
		}
	}
	for _, c := range l.Cables {
		if c.A == id {
			used[c.APort] = true
		}
		if c.B == id {
			used[c.BPort] = true
		}
	}
	var out []string
	for n := 1; n <= im.DataPorts(); n++ {
		if !used[n] {
			out = append(out, im.PortNameOf(n))
		}
	}
	return out
}

// EmuConsoleTail is the end of a VM's console (what it showed last).
func (a *App) EmuConsoleTail(id string) string {
	a.emuPoll(true)
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	lines := strings.Split(strings.TrimRight(emuLab.status.Log[id], "\n "), "\n")
	if len(lines) > 14 {
		lines = lines[len(lines)-14:]
	}
	return strings.Join(lines, "\n")
}

// emuShutdown closes the host connections (the VMs keep running on the host).
func (a *App) emuShutdown() {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	for k, c := range emuLab.conns {
		c.close()
		delete(emuLab.conns, k)
	}
}

// EmuSetupLine is a line of a host's set-up.
type EmuSetupLine struct {
	Run  string `json:"run"`
	Kind string `json:"kind"` // step, note, fail, line
	Text string `json:"text"`
}

// EmuSetupDone ends a set-up: the host as checked afterwards.
type EmuSetupDone struct {
	Run   string   `json:"run"`
	Error string   `json:"error"`
	Host  *EmuHost `json:"host"`
}

// EmuSetupHost installs QEMU on a Linux host and lets its user use KVM
// (package emu, SetupScript), in the background; password is the sudo
// password when sudo asks for one ("" = sudo without a password). Its lines
// come as "emu:setup" events, the end as "emu:setup:done".
func (a *App) EmuSetupHost(sessionPath, password string) (string, error) {
	defer emuAs("user")()
	if err := a.ready(); err != nil {
		return "", err
	}
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.mu.Unlock()
	id := randHex(4)
	emit := func(kind, text string) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "emu:setup", EmuSetupLine{Run: id, Kind: kind, Text: text})
		}
	}
	emuGo(func() {
		err := a.emuSetup(sessionPath, password, emit)
		// the kvm group counts for new logins only
		emuLab.mu.Lock()
		if c := emuLab.conns[sessionPath]; c != nil {
			c.close()
			delete(emuLab.conns, sessionPath)
		}
		emuLab.mu.Unlock()
		done := EmuSetupDone{Run: id}
		if err != nil {
			done.Error = err.Error()
		}
		if h, err2 := a.EmuCheckHost(sessionPath); err2 == nil {
			done.Host = &h
		} else if done.Error == "" {
			done.Error = err2.Error()
		}
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "emu:setup:done", done)
		}
	})
	return id, nil
}

func (a *App) emuSetup(host, password string, emit func(kind, text string)) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cl, err := a.emuClient(ctx, host)
	if err != nil {
		return err
	}
	s, err := cl.NewSession()
	if err != nil {
		return err
	}
	defer s.Close()
	if password != "" {
		s.Stdin = strings.NewReader(password + "\n")
	}
	out, err := s.StdoutPipe()
	if err != nil {
		return err
	}
	// its errors (sudo's among them) into the same stream: StdoutPipe leaves Stderr unset
	if err := s.Start("exec 2>&1; " + emu.SetupCommand(password != "")); err != nil {
		return err
	}
	failed, done, sudoAsk := "", false, false
	buf := make([]byte, 0, 4096)
	readLine := func(line string) {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "@@step "):
			emit("step", strings.TrimPrefix(line, "@@step "))
		case strings.HasPrefix(line, "@@note "):
			emit("note", strings.TrimPrefix(line, "@@note "))
		case strings.HasPrefix(line, "@@fail "):
			failed = strings.TrimPrefix(line, "@@fail ")
			emit("fail", failed)
		case line == "@@done":
			done = true
		case strings.Contains(line, "a password is required") || strings.Contains(line, "incorrect password") || strings.Contains(line, "Sorry, try again"):
			sudoAsk = true
		case line != "":
			emit("line", line)
		}
	}
	rd := make([]byte, 4096)
	for {
		n, rerr := out.Read(rd)
		buf = append(buf, rd[:n]...)
		for {
			i := strings.IndexByte(string(buf), '\n')
			if i < 0 {
				break
			}
			readLine(string(buf[:i]))
			buf = buf[i+1:]
		}
		if rerr != nil {
			break
		}
	}
	if len(buf) > 0 {
		readLine(string(buf))
	}
	werr := s.Wait()
	switch {
	case sudoAsk && password == "":
		return errors.New("sudo asks for a password on this host: enter it and set up again")
	case sudoAsk:
		return errors.New("sudo did not take the password")
	case failed != "":
		return errors.New(failed)
	case !done:
		if werr != nil {
			return fmt.Errorf("the set-up stopped: %v", werr)
		}
		return errors.New("the set-up stopped")
	}
	return nil
}

// EmuSetIdentity changes a VM's serial number (its SMBIOS serial) and base MAC
// address ("" mac = from the lab); the VM gets them when it starts next.
func (a *App) EmuSetIdentity(id, serial, mac string) (EmuView, error) {
	defer emuAs("user")()
	mac = strings.ToLower(strings.TrimSpace(mac))
	if mac != "" {
		if hw, err := net.ParseMAC(mac); err != nil || len(hw) != 6 || hw[0]&1 == 1 {
			return EmuView{}, fmt.Errorf("%q is not a MAC address of a port", mac)
		}
	}
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such switch")
	}
	v.Serial, v.BaseMAC = strings.TrimSpace(serial), mac
	if v.Persona != nil && len(v.Persona.Slots) > 0 {
		v.Persona.Slots[0].Serial = v.Serial
	}
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuSetSerials gives VMs serial numbers from a list, in order (empty
// lines skipped); fromFormat makes new ones in each image's serial format instead.
func (a *App) EmuSetSerials(ids []string, serials []string, fromFormat bool) (EmuView, error) {
	defer emuAs("user")()
	var list []string
	seen := map[string]bool{}
	for _, s := range serials {
		if s = strings.TrimSpace(s); s != "" {
			if seen[strings.ToLower(s)] {
				return EmuView{}, fmt.Errorf("serial number %s is in the list twice", s)
			}
			seen[strings.ToLower(s)] = true
			list = append(list, s)
		}
	}
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	if len(ids) == 0 {
		for _, v := range l.VMs {
			ids = append(ids, v.ID)
		}
	}
	var none []string
	for i, id := range ids {
		v := l.VMByID(id)
		if v == nil {
			continue
		}
		if fromFormat {
			if im := emuImage(v.Image); im != nil && im.Serial != "" {
				v.Serial = sim.Generate(im.Serial)
			} else {
				none = append(none, v.Name)
			}
		} else if len(list) == 1 {
			v.Serial = emu.NextSerial(list[0], i) // the first one given, the others count up
		} else if i < len(list) {
			v.Serial = list[i]
		}
		if v.Persona != nil && len(v.Persona.Slots) > 0 {
			v.Persona.Slots[0].Serial = v.Serial
		}
	}
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	v, _ := a.EmuState()
	if len(none) > 0 {
		return v, fmt.Errorf("no serial number format in the image of %s: set one in Images", strings.Join(none, ", "))
	}
	return v, nil
}

func emuTemplate(id string) *emu.Template {
	for i := range emuLab.f.Templates {
		if emuLab.f.Templates[i].ID == id {
			return &emuLab.f.Templates[i]
		}
	}
	return nil
}

// emuPersonaReboot powers a VM off, writes its persona file into its disk and starts it again.
func (a *App) emuPersonaReboot(host, id string) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil || l.VMByID(id) == nil {
		emuLab.mu.Unlock()
		return
	}
	emuLab.state[id] = "persona"
	emuEv(id, "info", "powering off to write the persona file (its folder exists now)")
	emuLab.mu.Unlock()
	a.emuPowerOff(host, id, 90*time.Second)
	// its disk now: booted once, nothing of its own in it yet - kept as the image's booted copy
	emuLab.mu.Lock()
	script := ""
	if lv := a.emuFor(host); lv != nil && lv.VMByID(id) != nil {
		if im := emuImage(lv.VMByID(id).Image); im != nil {
			script = lv.BootedScript(*lv.VMByID(id), im)
		}
	}
	emuLab.mu.Unlock()
	if script != "" {
		if out, _ := a.emuRun(host, script, 10*time.Minute); strings.Contains(out, "booted") {
			emuLab.mu.Lock()
			emuEv(id, "info", "kept a copy of the image that booted once: the next switches from it on this host get their persona before their first boot (one boot)")
			emuLab.mu.Unlock()
		}
	}
	emuLab.mu.Lock()
	delete(emuLab.state, id)
	emuLab.mu.Unlock()
	a.emuStart(host, []string{id}) // writes the file (the disk has its folder now), then boots
}

// EmuApplyPersona writes the switches' persona files: running ones are powered off for it and started again.
func (a *App) EmuApplyPersona(ids []string) (EmuView, error) {
	defer emuAs("user")()
	host, ids := a.emuIDs(ids)
	if host == "" {
		return EmuView{}, errors.New("there is no emulated lab")
	}
	st := a.emuPollFresh()
	_ = host
	for _, id := range ids {
		emuLab.mu.Lock()
		h := emuLab.f.Host
		if v := emuLab.f.Lab.VMByID(id); v != nil {
			h = emuHostOf(*v)
		}
		emuLab.mu.Unlock()
		if _, up := st.Up[id]; up {
			emuGo(func() { a.emuPersonaReboot(h, id) })
		} else {
			emuGo(func() { a.emuStart(h, []string{id}) })
		}
	}
	time.Sleep(50 * time.Millisecond)
	return a.EmuState()
}

// EmuSetPersona keeps a switch's persona (model, serial numbers, modules, stack
// members); it is written into its disk on the next start or EmuApplyPersona.
func (a *App) EmuSetPersona(id string, p emu.Persona) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such switch")
	}
	if len(p.Slots) == 0 {
		v.Persona = nil
	} else {
		if v.Persona != nil {
			p.Applied = v.Persona.Applied
		}
		for i := range p.Slots {
			p.Slots[i].Num = i + 1
		}
		v.Persona = &p
		if p.Slots[0].Serial != "" {
			v.Serial = p.Slots[0].Serial // the first slot's serial is the switch's
		}
	}
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuPersonaPreview renders the persona file a switch would get with p.
func (a *App) EmuPersonaPreview(id string, p emu.Persona) (string, error) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	l := emuLab.f.Lab
	if l == nil || l.VMByID(id) == nil {
		return "", errors.New("no such switch")
	}
	v := *l.VMByID(id)
	im := emuImage(v.Image)
	if im == nil || im.Persona == nil {
		return "", errors.New("its image has no persona")
	}
	for i := range p.Slots {
		p.Slots[i].Num = i + 1
	}
	return im.Persona.Render(v, &p)
}

// EmuSetResources changes a switch's vCPUs, memory (MB) and disk size (GB, it
// only grows); it gets them when it starts next.
func (a *App) EmuSetResources(id string, cpus, ram, diskGB int) (EmuView, error) {
	defer emuAs("user")()
	if cpus < 1 || ram < 128 || diskGB < 0 {
		return EmuView{}, errors.New("at least 1 vCPU and 128 MB")
	}
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such switch")
	}
	if diskGB > 0 && diskGB < v.DiskGB {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("a disk can only grow")
	}
	v.CPUs, v.RAM, v.DiskGB = cpus, ram, diskGB
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuSaveTemplate adds or changes a template (no ID = a new one).
func (a *App) EmuSaveTemplate(t emu.Template) (EmuView, error) {
	defer emuAs("user")()
	if strings.TrimSpace(t.Name) == "" {
		return EmuView{}, errors.New("give the template a name")
	}
	if t.MACStart != "" && emu.NextMAC(t.MACStart, 0) == "" {
		return EmuView{}, fmt.Errorf("%q is not a MAC address", t.MACStart)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	if emuImage(t.Image) == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("choose the template's image")
	}
	if t.ID == "" {
		t.ID = "t" + randHex(3)
		emuLab.f.Templates = append(emuLab.f.Templates, t)
	} else if cur := emuTemplate(t.ID); cur != nil {
		*cur = t
	} else {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such template")
	}
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuRemoveTemplate forgets a template (switches made from it stay).
func (a *App) EmuRemoveTemplate(id string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	var keep []emu.Template
	for _, t := range emuLab.f.Templates {
		if t.ID != id {
			keep = append(keep, t)
		}
	}
	emuLab.f.Templates = keep
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// emuProfiles is a file of image profiles and templates (for example from a private pack).
type emuProfiles struct {
	Images    []emu.Image    `json:"images"`
	Templates []emu.Template `json:"templates"` // Image is the image profile's name
}

// EmuImportProfiles reads image profiles (how to boot an image, its persona)
// and templates from a JSON file. A profile whose file name matches an image
// already added updates it (its file stays); others are added without a file.
func (a *App) EmuImportProfiles(p string) (EmuView, error) {
	defer emuAs("user")()
	b, err := os.ReadFile(p)
	if err != nil {
		return EmuView{}, err
	}
	var pf emuProfiles
	if err := json.Unmarshal(b, &pf); err != nil {
		return EmuView{}, fmt.Errorf("not a profile file: %w", err)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	byName := map[string]string{}
	for _, im := range pf.Images {
		var cur *emu.Image
		for i := range emuLab.f.Images {
			x := &emuLab.f.Images[i]
			if im.FileName != "" && strings.EqualFold(x.FileName, im.FileName) || strings.EqualFold(x.Name, im.Name) {
				cur = x
				break
			}
		}
		if cur != nil {
			keepID, file, fn, size, sum := cur.ID, cur.File, cur.FileName, cur.Size, cur.SHA256
			*cur = im
			cur.ID, cur.File, cur.FileName, cur.Size, cur.SHA256 = keepID, file, orStr(fn, im.FileName), size, sum
			byName[im.Name] = keepID
			continue
		}
		im.ID, im.File, im.SHA256 = randHex(4), "", ""
		im.Password = ""
		emuLab.f.Images = append(emuLab.f.Images, im)
		byName[im.Name] = im.ID
	}
	for _, t := range pf.Templates {
		id := byName[t.Image]
		if id == "" {
			for _, x := range emuLab.f.Images {
				if strings.EqualFold(x.Name, t.Image) {
					id = x.ID
				}
			}
		}
		if id == "" {
			continue
		}
		t.ID, t.Image = "t"+randHex(3), id
		replaced := false
		for i := range emuLab.f.Templates {
			if strings.EqualFold(emuLab.f.Templates[i].Name, t.Name) {
				t.ID = emuLab.f.Templates[i].ID
				emuLab.f.Templates[i], replaced = t, true
			}
		}
		if !replaced {
			emuLab.f.Templates = append(emuLab.f.Templates, t)
		}
	}
	err = a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuSetMACs gives switches base MAC addresses from start, a block of 64 each.
func (a *App) EmuSetMACs(ids []string, start string) (EmuView, error) {
	defer emuAs("user")()
	if emu.NextMAC(start, 0) == "" {
		return EmuView{}, fmt.Errorf("%q is not a MAC address", start)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	if len(ids) == 0 {
		for _, v := range l.VMs {
			ids = append(ids, v.ID)
		}
	}
	for i, id := range ids {
		if v := l.VMByID(id); v != nil {
			v.BaseMAC = emu.NextMAC(start, i)
		}
	}
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// emuPowerOff asks a VM to shut down (ACPI, as the power button) and stops it
// when it has not after wait.
func (a *App) emuPowerOff(host, id string, wait time.Duration) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	v := l.VMByID(id)
	if v == nil {
		emuLab.mu.Unlock()
		return
	}
	mon, stop := l.MonitorPort(*v), a.emuFor(host).StopScript([]string{id})
	emuLab.mu.Unlock()
	if cl, err := a.emuClient(context.Background(), host); err == nil {
		if conn, err := cl.Dial("tcp", "127.0.0.1:"+strconv.Itoa(mon)); err == nil {
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			_, _ = fmt.Fprintf(conn, "system_powerdown\r\n")
			buf := make([]byte, 512)
			_, _ = conn.Read(buf)
			time.Sleep(300 * time.Millisecond)
			_ = conn.Close()
		}
	}
	for end := time.Now().Add(wait); time.Now().Before(end); time.Sleep(2 * time.Second) {
		if _, up := a.emuPollFresh().Up[id]; !up {
			break
		}
	}
	_, _ = a.emuRun(host, stop, time.Minute)
}

// versionIn finds the software version in a file name: the first run of
// numbers like 9.4.0.0 that does not start inside a word ("NOSGNS3.9.4.0.0"
// is 9.4.0.0, "vm-33.4.1.15" is 33.4.1.15, "v9.1" counts after a v).
var versionAt = regexp.MustCompile(`^\d+\.\d+\.\d+(?:\.\d+)?`)

func versionIn(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			continue
		}
		if i > 0 {
			c := name[i-1]
			if c >= '0' && c <= '9' || (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') && c != 'v' && c != 'V' {
				continue // inside a number or a word ("GNS3")
			}
		}
		if v := versionAt.FindString(name[i:]); v != "" {
			return v
		}
	}
	return ""
}

// versionIn finds a version number in a file name ("vm-33.4.1.15.x86_64" → 33.4.1.15).

// emuPackProfiles are the image profiles of the loaded device pack (its emulator.json).
func (a *App) emuPackProfiles() []emu.Image {
	simLab.mu.Lock()
	defer simLab.mu.Unlock()
	_ = a.labLoad()
	if simLab.pack == nil || len(simLab.pack.Emulator) == 0 {
		return nil
	}
	var pf emuProfiles
	if json.Unmarshal(simLab.pack.Emulator, &pf) != nil {
		return nil
	}
	return pf.Images
}

var emuDigits = regexp.MustCompile(`[0-9a-fx]*[0-9][0-9a-fx]*`)

// emuSubseq: every line of a is in b, in the same order.
func emuSubseq(a, b []string) bool {
	i := 0
	for _, x := range b {
		if i < len(a) && a[i] == x {
			i++
		}
	}
	return i == len(a)
}

// emuTopUp gives images made from a pack profile what a newer pack adds to
// it: console answers and the persona check (what the user set stays).
func emuTopUp(imgs []emu.Image, prof []emu.Image) (changed bool, pw []string) {
	for i := range imgs {
		im := &imgs[i]
		for _, p := range prof {
			if p.Name != im.Name {
				continue
			}
			for _, pa := range p.Answers {
				has := false
				for _, a := range im.Answers {
					has = has || a.Match == pa.Match
				}
				if !has {
					im.Answers, changed = append(im.Answers, pa), true
				}
			}
			if len(p.Day0) > len(im.Day0) && emuSubseq(im.Day0, p.Day0) { // the pack's day-0 grew (save configuration, LLDP on): the user's own lines would not be in it
				im.Day0, changed = p.Day0, true
			}
			if im.LagCmd == "" && p.LagCmd != "" {
				im.LagCmd, changed = p.LagCmd, true
			}
			if len(im.LagSet) == 0 && len(p.LagSet) > 0 {
				im.LagSet, im.LagDel, changed = p.LagSet, p.LagDel, true
			}
			if len(p.Facts) > 0 && !slices.Equal(im.Facts, p.Facts) { // the pack's, as it improves them
				im.Facts, changed = p.Facts, true
			}
			if im.MgmtMACAlign == 0 && p.MgmtMACAlign > 0 {
				im.MgmtMACAlign, im.MgmtMACOffset, changed = p.MgmtMACAlign, p.MgmtMACOffset, true
			}
			if im.TCGCPU == "" && p.TCGCPU != "" {
				im.TCGCPU, changed = p.TCGCPU, true
			}
			if p.NIC != "" && p.NIC != im.PackNIC { // the pack's card changed: images on its old one (not chosen by the user) take the new one
				if im.NIC == im.PackNIC || im.PackNIC == "" && im.NIC == "e1000" {
					im.NIC = p.NIC
				}
				im.PackNIC, changed = p.NIC, true
			}
			if len(p.BootFail) > 0 && !slices.Equal(im.BootFail, p.BootFail) {
				im.BootFail, changed = p.BootFail, true
			}
			if len(p.BootAnswers) > 0 && !slices.Equal(im.BootAnswers, p.BootAnswers) { // the pack's, as it improves them
				im.BootAnswers, changed = p.BootAnswers, true
			}
			if im.NeighborCmd == "" && p.NeighborCmd != "" {
				im.NeighborCmd, changed = p.NeighborCmd, true
			}
			if len(p.AddrSet) > len(im.AddrSet) && (len(im.AddrSet) == 0 || slices.Equal(im.AddrSet, p.AddrSet[len(p.AddrSet)-len(im.AddrSet):])) {
				im.AddrSet, changed = p.AddrSet, true // the pack's commands grew (an old address is removed first)
			}
			if p.Persona != nil && im.Persona != nil && im.Persona.Check == "" && p.Persona.Check != "" {
				im.Persona.Check, changed = p.Persona.Check, true
			}
			if im.DefaultPassword == "" && p.DefaultPassword != "" {
				im.DefaultPassword, changed = p.DefaultPassword, true
				pw = append(pw, im.ID)
			}
			if strings.EqualFold(im.Ready, "login:") && p.Ready != "" && !strings.EqualFold(p.Ready, "login:") { // the pack waits for a later sign of a finished boot
				im.Ready, changed = p.Ready, true
			}
			if p.Persona != nil && im.Persona != nil && im.Persona.SerialMax == 0 && p.Persona.SerialMax > 0 {
				im.Persona.SerialMax, changed = p.Persona.SerialMax, true
			}
			if p.Persona != nil && im.Persona != nil && len(im.Persona.FSHas) == 0 && len(p.Persona.FSHas) > 0 {
				im.Persona.FSHas, changed = p.Persona.FSHas, true
			}
		}
	}
	return changed, pw
}

// emuRecognise picks the pack's profile whose file name pattern matches, with the version it names.
func (a *App) emuRecognise(file string) (*emu.Image, string) {
	for _, pr := range a.emuPackProfiles() {
		if pr.Match == "" {
			continue
		}
		re, err := regexp.Compile(pr.Match)
		if err != nil {
			continue
		}
		m := re.FindStringSubmatch(file)
		if m == nil {
			continue
		}
		ver := ""
		if i := re.SubexpIndex("version"); i > 0 && i < len(m) {
			ver = m[i]
		}
		p := pr
		return &p, ver
	}
	return nil, ""
}

// EmuDeviceType is a device the emulated lab can make: a persona model of an
// image kind, or a plain image kind, with the images (versions) that can be it.
type EmuDeviceType struct {
	Key     string            `json:"key"`     // "<kind>|<model>"
	Model   string            `json:"model"`   // "" = the image as it is
	Kind    string            `json:"kind"`    // the image kind (its profile name, e.g. what the pack calls it)
	OSName  string            `json:"osName"`  // shown after the model
	Modules []string          `json:"modules"` // modules it can take
	ModName string            `json:"modName"` // what a module is called
	Images  []string          `json:"images"`  // image ids, newest version first
	CPUs    int               `json:"cpus"`
	RAM     int               `json:"ram"`
	Values  map[string]string `json:"values"`
}

// emuDeviceTypes lists what can be made (lock held; profiles from the pack).
func (a *App) emuDeviceTypes(profiles []emu.Image) []EmuDeviceType {
	kindOf := func(im emu.Image) string { return orStr(personaKind(im), im.Name) }
	byKind := map[string][]emu.Image{}
	var order []string
	add := func(k string) {
		if _, ok := byKind[k]; !ok {
			order = append(order, k)
			byKind[k] = nil
		}
	}
	for _, pr := range profiles {
		add(kindOf(pr))
	}
	for _, im := range emuLab.f.Images {
		if im.Version == "" {
			im.Version = versionIn(im.FileName)
		}
		k := kindOf(im)
		add(k)
		byKind[k] = append(byKind[k], im)
	}
	var out []EmuDeviceType
	for _, k := range order {
		ims := byKind[k]
		sort.SliceStable(ims, func(i, j int) bool { return versionLess(ims[j].Version, ims[i].Version) })
		var ids []string
		for _, im := range ims {
			if im.HasFiles() {
				ids = append(ids, im.ID)
			}
		}
		// the persona of the kind: an image's, or the pack profile's
		var def *emu.PersonaDef
		ref := emu.Image{}
		for _, im := range ims {
			if im.Persona != nil {
				def, ref = im.Persona, im
				break
			}
		}
		if def == nil {
			for _, pr := range profiles {
				if kindOf(pr) == k && pr.Persona != nil {
					def, ref = pr.Persona, pr
				}
			}
		}
		if def == nil {
			if len(ims) > 0 {
				ref = ims[0]
			}
			out = append(out, EmuDeviceType{Key: k + "|", Kind: k, OSName: ref.DeviceType, Images: ids, CPUs: ref.CPUs, RAM: ref.RAM})
			continue
		}
		vals := map[string]string{}
		for _, v := range def.Values {
			vals[v.Key] = v.Default
		}
		for _, m := range def.Models {
			out = append(out, EmuDeviceType{Key: k + "|" + m.Name, Model: m.Name, Kind: k, OSName: orStr(def.OSName, ref.DeviceType), Modules: m.Modules, ModName: def.Modules, Images: ids, CPUs: ref.CPUs, RAM: ref.RAM, Values: vals})
		}
	}
	return out
}

// personaKind names the kind of an image with a persona (images of one kind are its versions).
func personaKind(im emu.Image) string {
	if im.Persona == nil {
		return ""
	}
	return im.Name
}

// versionLess compares dotted versions numerically.
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// EmuSuggestion is a first serial number and MAC address that look real for a model.
type EmuSuggestion struct {
	Serial string `json:"serial"`
	MAC    string `json:"mac"`
}

// EmuSuggest makes a serial number and a base MAC in the format the device pack
// gives for the model (or the nearest model of its family), else neutral ones.
func (a *App) EmuSuggest(model string) EmuSuggestion {
	simLab.mu.Lock()
	p := simLab.pack
	simLab.mu.Unlock()
	if p != nil {
		m, ok := p.ModelOf(model)
		if !ok {
			fam := strings.SplitN(model, "-", 2)[0]
			for _, x := range p.Models {
				if strings.HasPrefix(x.SKU, fam+"-") && x.Serial != "" {
					m, ok = x, true
					break
				}
			}
		}
		if ok && m.Serial != "" {
			mac := sim.Generate(m.MAC)
			if emu.NextMAC(mac, 0) == "" {
				mac = sim.Generate("02:{h:2}:{h:2}:{h:2}:{h:2}:00")
			}
			return EmuSuggestion{Serial: sim.Generate(m.Serial), MAC: macBlock(mac)}
		}
	}
	return EmuSuggestion{Serial: sim.Generate("{L:ABCDEFHJKLMNPRSTUVWXY}{L:ABCDEFHJKLMNPRSTUVWXY}{YY}{WW}{L:GKQ}-{N:5}"), MAC: macBlock(sim.Generate("02:{h:2}:{h:2}:{h:2}:{h:2}:00"))}
}

// macBlock rounds a MAC down to a block of 64 (the switch's ports follow it).
func macBlock(mac string) string {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return mac
	}
	hw[5] &^= 63
	return hw.String()
}

// EmuSetLag makes (or changes) a LAG on a switch in Termwright: its ports
// (by name), "lacp" or "static"; no ports removes it. The ports leave other
// LAGs set in Termwright on that switch.
func (a *App) EmuSetLag(vm, name, mode string, portNames []string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil || l.VMByID(vm) == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such switch")
	}
	im := emuImage(l.VMByID(vm).Image)
	var ports []int
	for _, pn := range portNames {
		n := 0
		if im != nil {
			n = im.DataPortOf(pn)
		}
		if n == 0 {
			emuLab.mu.Unlock()
			return EmuView{}, fmt.Errorf("no port %s on that switch", pn)
		}
		ports = append(ports, n)
	}
	if mode != "static" {
		mode = "lacp"
	}
	if name == "" {
		used := map[string]bool{}
		for _, g := range l.Lags {
			if g.VM == vm {
				used[g.Name] = true
			}
		}
		for n := 1; ; n++ {
			if name = "LAG " + strconv.Itoa(n); !used[name] {
				break
			}
		}
	}
	var keep []emu.Lag
	for _, g := range l.Lags {
		if g.VM == vm && g.Source == "user" {
			if g.Name == name {
				continue
			}
			g.Ports = slices.DeleteFunc(slices.Clone(g.Ports), func(p int) bool { return slices.Contains(ports, p) })
			if len(g.Ports) == 0 {
				continue
			}
		}
		keep = append(keep, g)
	}
	if len(ports) > 0 {
		keep = append(keep, emu.Lag{VM: vm, Name: name, Mode: mode, Ports: slices.Sorted(slices.Values(ports)), Source: "user"})
	}
	l.Lags = keep
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuReadLags asks running switches (all when ids is empty) for their LAGs
// on the console (the image's LAG command) and keeps what they say.
func (a *App) EmuReadLags(ids []string) (EmuView, error) {
	host, ids := a.emuIDs(ids)
	if host == "" {
		return EmuView{}, errors.New("there is no emulated lab")
	}
	st := a.emuPollFresh()
	for _, id := range ids {
		if _, up := st.Up[id]; !up {
			continue
		}
		emuLab.mu.Lock()
		v := emuLab.f.Lab.VMByID(id)
		var vv emu.VM
		var im *emu.Image
		if v != nil {
			vv, im = *v, emuImage(v.Image)
		}
		emuLab.mu.Unlock()
		if im == nil || im.LagCmd == "" {
			continue
		}
		if err := a.emuReadLag(a.emuHostOfID(vv.ID), vv, *im); err != nil {
			emuLab.mu.Lock()
			emuEv(id, "warn", "LAGs not read: "+err.Error())
			emuLab.mu.Unlock()
			continue
		}
	}
	return a.EmuState()
}

// emuReadLag reads one switch's LAGs on its console.
func (a *App) emuReadLag(host string, v emu.VM, im emu.Image) error {
	out, err := a.emuOnConsole(host, v, im, false, im.LagCmd)
	if err != nil {
		return err
	}
	a.emuKeepLags(v, &im, emu.ParseLags(out))
	return nil
}

// emuOnConsole logs in on a switch's console and types commands; it returns
// what they printed. confirm: a question they ask is answered yes.
func (a *App) emuOnConsole(host string, v emu.VM, im emu.Image, confirm bool, cmds ...string) (string, error) {
	emuLab.mu.Lock()
	l := a.emuFor(host)
	if l == nil {
		emuLab.mu.Unlock()
		return "", errors.New("there is no emulated lab")
	}
	port := l.ConsolePort(v)
	emuLab.mu.Unlock()
	im.Password, _ = secrets.Lookup(emuImageKey(im.ID))
	im.Password = orStr(im.Password, im.DefaultPassword) // the factory password while none is set
	cl, err := a.emuClient(context.Background(), host)
	if err != nil {
		return "", err
	}
	conn, err := cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return "", fmt.Errorf("console: %w (is it open in a tab or the console pane?)", err)
	}
	c := emu.NewConsole(conn)
	c.Confirm = confirm
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var b strings.Builder
	for _, cmd := range cmds {
		out, err := emu.Ask(ctx, c, &im, cmd)
		b.WriteString(emu.CleanConsole(out))
		b.WriteString("\n")
		if err != nil {
			return b.String(), fmt.Errorf("%s: %w", cmd, err)
		}
	}
	return b.String(), nil
}

// EmuLagCommands are the commands that would make (or remove) a LAG of the
// topology on its switch - shown before they are typed.
func (a *App) EmuLagCommands(vm, name string, remove bool) ([]string, error) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	g, im, err := emuLagOf(vm, name)
	if err != nil {
		return nil, err
	}
	return im.LagCommands(g, remove)
}

func emuLagOf(vm, name string) (emu.Lag, *emu.Image, error) {
	l := emuLab.f.Lab
	if l == nil || l.VMByID(vm) == nil {
		return emu.Lag{}, nil, errors.New("no such switch")
	}
	im := emuImage(l.VMByID(vm).Image)
	if im == nil {
		return emu.Lag{}, nil, errors.New("the switch has no image")
	}
	for _, g := range l.Lags {
		if g.VM == vm && g.Name == name {
			return g, im, nil
		}
	}
	return emu.Lag{}, nil, fmt.Errorf("no LAG %q on that switch", name)
}

// EmuApplyLag types a LAG of the topology on its switch (or removes it from
// the switch) on the console, then reads the switch's LAGs back.
func (a *App) EmuApplyLag(vm, name string, remove bool) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	g, im, err := emuLagOf(vm, name)
	var cmds []string
	if err == nil {
		cmds, err = im.LagCommands(g, remove)
	}
	var v emu.VM
	var imc emu.Image
	if err == nil {
		v, imc = *emuLab.f.Lab.VMByID(vm), *im
	}
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	if _, up := a.emuPollFresh().Up[vm]; !up {
		return EmuView{}, errors.New("start the switch first")
	}
	host := a.emuHostOfID(vm)
	out, err := a.emuOnConsole(host, v, imc, true, cmds...)
	emuLab.mu.Lock()
	what := "set on the switch"
	if remove {
		what = "removed from the switch"
	}
	if err != nil {
		emuEv(vm, "error", fmt.Sprintf("LAG %s not %s: %v", name, what, err))
	} else if bad := emuCLIError(out); bad != "" {
		err = fmt.Errorf("the switch said: %s", bad)
		emuEv(vm, "error", fmt.Sprintf("LAG %s not %s: %s", name, what, bad))
	} else {
		emuEv(vm, "ok", fmt.Sprintf("LAG %s %s: %s", name, what, strings.Join(cmds, " · ")))
	}
	emuLab.mu.Unlock()
	if imc.LagCmd != "" {
		_ = a.emuReadLag(host, v, imc) // what the switch has now
	}
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// emuCLIError finds an error line in what CLI commands printed.
func emuCLIError(out string) string {
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		low := strings.ToLower(t)
		if strings.HasPrefix(low, "error") || strings.HasPrefix(low, "%") || strings.Contains(low, "invalid input") || strings.HasPrefix(low, "^") && len(t) > 1 {
			return t
		}
	}
	return ""
}

// emuKeepLags keeps the LAGs a switch reported (in place of the ones read before).
func (a *App) emuKeepLags(v emu.VM, im *emu.Image, found []emu.SwitchLag) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	l := emuLab.f.Lab
	if l == nil {
		return
	}
	keep := slices.DeleteFunc(slices.Clone(l.Lags), func(g emu.Lag) bool { return g.VM == v.ID && g.Source == "switch" })
	var names []string
	for _, f := range found {
		g := emu.Lag{VM: v.ID, Name: f.Name, Mode: f.Mode, Source: "switch"}
		for _, p := range f.Ports {
			if n := im.DataPortOf(p); n > 0 {
				g.Ports = append(g.Ports, n)
			}
		}
		if len(g.Ports) > 0 {
			keep = append(keep, g)
			names = append(names, fmt.Sprintf("%s (%s, %d ports)", g.Name, g.Mode, len(g.Ports)))
		}
	}
	l.Lags = keep
	if len(names) > 0 {
		emuEv(v.ID, "ok", "LAGs on the switch: "+strings.Join(names, ", "))
	} else {
		emuEv(v.ID, "info", "no LAGs on the switch")
	}
	_ = a.emuSave()
}

func (a *App) emuTopoDir() string { return filepath.Join(a.labDir(), "topologies") }

func (a *App) emuTopoPath(name string) string {
	return filepath.Join(a.emuTopoDir(), url.PathEscape(name)+".json")
}

// emuWriteTopo writes the open lab to its topology file (mu held).
func (a *App) emuWriteTopo() error {
	f := emuLab.f
	t := emuTopo{Name: f.Topology, Saved: time.Now().Format(time.RFC3339), Host: f.Host, Folder: f.Folder, Lab: f.Lab, Sessions: f.Sessions, Consoles: f.Consoles}
	b, err := json.MarshalIndent(t, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.emuTopoDir(), 0o700); err != nil {
		return err
	}
	p := a.emuTopoPath(f.Topology)
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

func emuTopoName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 || strings.ContainsAny(name, "/\\:*?\"<>|") {
		return "", errors.New("give the topology a name (no / \\ : * ? \" < > |)")
	}
	return name, nil
}

// EmuTopologies lists the saved topologies, newest first.
func (a *App) EmuTopologies() []EmuTopoInfo {
	emuLab.mu.Lock()
	a.emuLoad()
	open := emuLab.f.Topology
	emuLab.mu.Unlock()
	ents, _ := os.ReadDir(a.emuTopoDir())
	out := []EmuTopoInfo{} // empty, not null: the topology menu opens with no saved one too
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(a.emuTopoDir(), e.Name()))
		if err != nil {
			continue
		}
		var t emuTopo
		if json.Unmarshal(b, &t) != nil || t.Name == "" {
			continue
		}
		info := EmuTopoInfo{Name: t.Name, Saved: t.Saved, Open: t.Name == open}
		if t.Lab != nil {
			info.Switches = len(t.Lab.VMs)
			info.Hosts = t.Lab.Hosts(t.Host)
		} else if t.Host != "" {
			info.Hosts = []string{t.Host}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Saved > out[j].Saved })
	for _, r := range a.EmuRunning() { // running now, also in the background
		for i := range out {
			if out[i].Name == r.Topology {
				out[i].Running += len(r.Switches)
				out[i].RunOn = append(out[i].RunOn, r.Host[strings.LastIndex(r.Host, "/")+1:])
			}
		}
	}
	return out
}

// EmuSaveTopology saves the open lab under a name (and keeps it there as it changes).
func (a *App) EmuSaveTopology(name string) (EmuView, error) {
	defer emuAs("user")()
	name, err := emuTopoName(name)
	if err != nil {
		return EmuView{}, err
	}
	emuLab.mu.Lock()
	a.emuLoad()
	if emuLab.f.Lab == nil || len(emuLab.f.Lab.VMs) == 0 {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("the lab has no switches to save yet")
	}
	if name != emuLab.f.Topology {
		if _, err := os.Stat(a.emuTopoPath(name)); err == nil {
			emuLab.mu.Unlock()
			return EmuView{}, fmt.Errorf("a topology named %q is there already: pick another name", name)
		}
	}
	emuLab.f.Topology = name
	err = a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// emuRunningAny says whether switches of the open lab run (no lock held).
func (a *App) emuRunningAny() bool {
	emuLab.mu.Lock()
	has := emuLab.f.Lab != nil && len(emuLab.f.Lab.VMs) > 0 && emuLab.f.Host != ""
	emuLab.mu.Unlock()
	return has && len(a.emuPollFresh().Up) > 0
}

// emuSwitchTo makes another topology the open one (mu held): runtime state
// of the old one goes.
func (a *App) emuSwitchTo(t emuTopo) {
	emuLab.f.Topology, emuLab.f.Host, emuLab.f.Folder, emuLab.f.Lab = t.Name, t.Host, orStr(t.Folder, "Emulated Lab"), t.Lab
	emuLab.f.Sessions, emuLab.f.Consoles = t.Sessions, t.Consoles
	if emuLab.f.Sessions == nil {
		emuLab.f.Sessions = map[string]string{}
	}
	if emuLab.f.Consoles == nil {
		emuLab.f.Consoles = map[string]string{}
	}
	emuLab.state, emuLab.errs, emuLab.day0, emuLab.starting = map[string]string{}, map[string]string{}, map[string]bool{}, map[string]time.Time{}
	emuLab.ready, emuLab.personaAfterBoot, emuLab.personaTried, emuLab.perr = map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]string{}
	emuLab.pinfo, emuLab.pcheck, emuLab.ticks, emuLab.cpu = map[string]string{}, map[string]bool{}, map[string]float64{}, map[string]float64{}
	emuLab.status, emuLab.statusAt = emu.Status{}, time.Time{}
}

// EmuOpenTopology opens a saved topology (the open one is kept as it is; its
// running switches go on running) and, with start, starts all its switches.
func (a *App) EmuOpenTopology(name string, start bool) (EmuView, error) {
	defer emuAs("user")()
	return a.EmuOpenTopologyOn(name, start, nil)
}

// EmuOpenTopologyOn opens a saved topology with its hosts replaced (old →
// new host session): its switches on a replaced host start there from their
// image, with that host's best management.
func (a *App) EmuOpenTopologyOn(name string, start bool, hosts map[string]string) (EmuView, error) {
	defer emuAs("user")()
	b, err := os.ReadFile(a.emuTopoPath(name))
	if err != nil {
		return EmuView{}, fmt.Errorf("no topology named %q", name)
	}
	var t emuTopo
	if err := json.Unmarshal(b, &t); err != nil {
		return EmuView{}, fmt.Errorf("topology %q: %w", name, err)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	same := emuLab.f.Topology == t.Name
	emuLab.mu.Unlock()
	// the open topology's running switches keep running in the background (Hosts and the topology list show them)
	emuLab.mu.Lock()
	if !same {
		if emuLab.f.Topology != "" {
			_ = a.emuWriteTopo()
		}
		a.emuSwitchTo(t)
	}
	moved, err := a.emuRemap(hosts)
	if err != nil {
		emuLab.mu.Unlock()
		return EmuView{}, err
	}
	err = a.emuSave()
	host := emuLab.f.Host
	var ids []string
	if emuLab.f.Lab != nil {
		for _, v := range emuLab.f.Lab.VMs {
			ids = append(ids, v.ID)
		}
	}
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	for _, v := range moved { // their sessions: the new host's management
		a.emuRetarget(v)
	}
	emuLab.mu.Lock()
	var bare []emu.VM // a copied or imported topology: its switches get sessions of their own
	if emuLab.f.Lab != nil {
		for _, v := range emuLab.f.Lab.VMs {
			if emuLab.f.Sessions[v.ID] == "" {
				bare = append(bare, v)
			}
		}
	}
	emuLab.mu.Unlock()
	for _, v := range bare {
		a.emuSessions(v)
	}
	if start && host != "" && len(ids) > 0 {
		return a.EmuStart(ids)
	}
	return a.EmuState()
}

// EmuNewTopology starts an empty lab under a name (the open one is kept; its
// running switches go on running).
func (a *App) EmuNewTopology(name string) (EmuView, error) {
	defer emuAs("user")()
	name, err := emuTopoName(name)
	if err != nil {
		return EmuView{}, err
	}
	if _, err := os.Stat(a.emuTopoPath(name)); err == nil {
		return EmuView{}, fmt.Errorf("a topology named %q is there already: open it, or pick another name", name)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	if emuLab.f.Topology != "" {
		_ = a.emuWriteTopo()
	} else if emuLab.f.Lab != nil && len(emuLab.f.Lab.VMs) > 0 { // an unsaved lab is kept under a name of its own
		emuLab.f.Topology = "Lab " + time.Now().Format("2006-01-02 15:04")
		_ = a.emuWriteTopo()
	}
	a.emuSwitchTo(emuTopo{Name: name, Host: emuLab.f.Host, Folder: "Emulated Lab/" + name})
	err = a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuDeleteTopology forgets a saved topology that is not open (its switches'
// sessions and disks on the host stay until removed in it).
func (a *App) EmuDeleteTopology(name string) error {
	defer emuAs("user")()
	emuLab.mu.Lock()
	open := emuLab.f.Topology
	emuLab.mu.Unlock()
	if name == open {
		return errors.New("that topology is open: open another one first")
	}
	return os.Remove(a.emuTopoPath(name))
}

// EmuMoveVM moves a stopped switch to another host of yours: it starts there
// from the image again (its disk on the old host goes; day 0 is typed again),
// with that host's management; cables to switches on other hosts become UDP
// tunnels between the hosts.
func (a *App) EmuMoveVM(id, host string) (EmuView, error) {
	defer emuAs("user")()
	v, err := a.emuMoveVM(id, host)
	if err != nil {
		emuLab.mu.Lock()
		emuEv(id, "error", "not moved to "+host[strings.LastIndex(host, "/")+1:]+": "+err.Error())
		emuLab.mu.Unlock()
	}
	return v, err
}

func (a *App) emuMoveVM(id, host string) (EmuView, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	known := false
	for _, h := range emuLab.f.Hosts {
		known = known || h.Session == host
	}
	if v == nil || host == "" {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such switch")
	}
	if !known {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("add that host first (Hosts)")
	}
	from := emuHostOf(*v)
	if from == host {
		emuLab.mu.Unlock()
		return a.EmuState()
	}
	emuLab.mu.Unlock()
	if _, up := a.emuPollFresh().Up[id]; up {
		return EmuView{}, errors.New("stop the switch first: it moves while it is off")
	}
	emuLab.mu.Lock()
	reset := a.emuFor(from).ResetScript([]string{id})
	emuLab.mu.Unlock()
	_, _ = a.emuRun(from, reset, time.Minute) // its disk on the old host
	var info *emu.HostInfo
	emuLab.mu.Lock()
	for i := range emuLab.f.Hosts {
		if emuLab.f.Hosts[i].Session == host {
			info = &emuLab.f.Hosts[i].Info
		}
	}
	if l.Nets == nil || host != emuLab.f.Host {
		if _, ok := l.Nets[host]; !ok && host != emuLab.f.Host && info != nil {
			mgmt := emuBestMgmt(info)
			if mgmt == "cloud" || mgmt == "lan" && info.LanKind == "" {
				mgmt = "nat"
			}
			l.SetNet(host, emuLab.f.Host, emu.HostNet{Mgmt: mgmt, LanIf: info.LanIf, LanKind: info.LanKind, MgmtNet: "10.99.0.0/24"})
		}
	}
	v = l.VMByID(id)
	v.Host = ""
	if host != emuLab.f.Host {
		v.Host = host
	}
	v.Day0Done, v.Direct = false, false
	if v.Persona != nil {
		v.Persona.Applied = ""
	}
	lv := a.emuFor(host)
	v.MgmtIP, v.Gateway = lv.FreeAddress(*v)
	emuEv(id, "info", "moved to "+host[strings.LastIndex(host, "/")+1:]+": it starts there from the image")
	vv := *v
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	a.emuRetarget(vv)
	return a.EmuState()
}

// EmuSetRole sets what a kind of device is drawn as in this topology (every
// switch of that model or image, new ones too; "" = from its model).
func (a *App) EmuSetRole(id, role string) (EmuView, error) {
	defer emuAs("user")()
	switch role {
	case "", "access", "core", "router", "fw", "ap", "server", "host", "cloud":
	default:
		return EmuView{}, fmt.Errorf("no role %q", role)
	}
	return a.emuSetIcon(id, func(p *emu.IconPick) { p.Role = role })
}

// EmuSetIconStyle sets the icon style of a kind of device in this topology
// ("" = the default), or with id "" the default style for all.
func (a *App) EmuSetIconStyle(id, style string) (EmuView, error) {
	defer emuAs("user")()
	switch style {
	case "", "box", "iso", "classic", "blue", "line", "front", "slate", "steel", "rack":
	default:
		return EmuView{}, fmt.Errorf("no icon style %q", style)
	}
	if id == "" {
		emuLab.mu.Lock()
		a.emuLoad()
		emuLab.f.IconStyle = style
		err := a.emuSave()
		emuLab.mu.Unlock()
		if err != nil {
			return EmuView{}, err
		}
		return a.EmuState()
	}
	return a.emuSetIcon(id, func(p *emu.IconPick) { p.Style = style })
}

func (a *App) emuSetIcon(id string, set func(*emu.IconPick)) (EmuView, error) {
	emuLab.mu.Lock()
	var err error
	if l := emuLab.f.Lab; l == nil || l.VMByID(id) == nil {
		err = errors.New("no such switch")
	} else {
		v := l.VMByID(id)
		k := emu.IconKey(*v)
		if l.Icons == nil {
			l.Icons = map[string]emu.IconPick{}
		}
		p := l.Icons[k]
		set(&p)
		if p.Role == "" && p.Style == "" {
			delete(l.Icons, k)
		} else {
			l.Icons[k] = p
		}
		for i := range l.VMs { // the old per-switch choice gives way to the kind's
			if emu.IconKey(l.VMs[i]) == k {
				l.VMs[i].Role = ""
			}
		}
		err = a.emuSave()
	}
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// emuRemap moves the open lab to other hosts (mu held): old host → new host.
func (a *App) emuRemap(m map[string]string) ([]emu.VM, error) {
	l, main := emuLab.f.Lab, emuLab.f.Host
	if l == nil || len(m) == 0 {
		return nil, nil
	}
	info := func(h string) *emu.HostInfo {
		for i := range emuLab.f.Hosts {
			if emuLab.f.Hosts[i].Session == h {
				return &emuLab.f.Hosts[i].Info
			}
		}
		return nil
	}
	to := func(h string) string {
		if n := m[h]; n != "" {
			return n
		}
		return h
	}
	for _, n := range m {
		if n != "" && info(n) == nil {
			return nil, fmt.Errorf("%s is not one of your hosts (Hosts)", n)
		}
	}
	newMain := to(main)
	best := func(h string) emu.HostNet { // what a host the lab did not use gets (as Move to Host)
		i := info(h)
		if i == nil {
			return emu.HostNet{Mgmt: "forward"}
		}
		mg := emuBestMgmt(i)
		if mg == "cloud" || mg == "lan" && i.LanKind == "" {
			mg = "nat"
		}
		return emu.HostNet{Mgmt: mg, LanIf: i.LanIf, LanKind: i.LanKind, MgmtNet: "10.99.0.0/24"}
	}
	// management per host: a host that stays keeps its own; a new one gets its best
	nets := map[string]emu.HostNet{}
	for _, h := range l.Hosts(main) {
		if to(h) == h {
			nets[h] = l.Net(h, main)
		} else {
			nets[to(h)] = best(to(h))
		}
	}
	moved := map[string]bool{}
	for i := range l.VMs {
		v := &l.VMs[i]
		old := orStr(v.Host, main)
		nw := to(old)
		if nw != old {
			moved[v.ID] = true
			v.Day0Done, v.Direct, v.FastKey = false, false, ""
			if v.Persona != nil {
				v.Persona.Applied, v.Persona.Took, v.Persona.Fail = "", "", ""
			}
		}
		v.Host = ""
		if nw != newMain {
			v.Host = nw
		}
	}
	emuLab.f.Host = newMain
	l.Nets = nil
	for h, n := range nets {
		l.SetNet(h, newMain, n)
	}
	var out []emu.VM
	for i := range l.VMs {
		if v := &l.VMs[i]; moved[v.ID] {
			lv := a.emuFor(orStr(v.Host, newMain))
			v.MgmtIP, v.Gateway = lv.FreeAddress(*v)
			out = append(out, *v)
		}
	}
	return out, nil
}

// emuReadTopo reads a saved topology (the open one as it is now).
func (a *App) emuReadTopo(name string) (emuTopo, error) {
	emuLab.mu.Lock()
	if emuLab.f.Topology == name {
		_ = a.emuWriteTopo()
	}
	emuLab.mu.Unlock()
	var t emuTopo
	b, err := os.ReadFile(a.emuTopoPath(name))
	if err != nil {
		return t, fmt.Errorf("no topology named %q", name)
	}
	if err := json.Unmarshal(b, &t); err != nil || t.Lab == nil {
		return t, fmt.Errorf("topology %q cannot be read", name)
	}
	return t, nil
}

func (a *App) emuPutTopo(t emuTopo) error {
	t.Saved = time.Now().Format(time.RFC3339)
	b, err := json.MarshalIndent(t, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.emuTopoDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(a.emuTopoPath(t.Name), b, 0o600)
}

// emuFresh makes a topology a new lab of its own: its own disks on the hosts
// (switches boot from their images), its own sessions folder.
func emuFresh(t *emuTopo, o emuOther) {
	l := *t.Lab
	l.ID, l.Base = emuFreeLabID(o), emuFreeBase(o) // it may run beside the one it came from
	if l.Mgmt == "bridge" && o.nets[l.MgmtNet] {
		l.MgmtNet = emuFreeBridgeNet(o)
	}
	l.VMs = append([]emu.VM(nil), l.VMs...)
	for i := range l.VMs {
		v := &l.VMs[i]
		v.Day0Done, v.Direct, v.FastKey = false, false, ""
		if v.Persona != nil {
			p := *v.Persona
			p.Applied, p.Took, p.Fail = "", "", ""
			v.Persona = &p
		}
	}
	t.Lab = &l
	t.Folder = "Emulated Lab/" + t.Name
	t.Sessions, t.Consoles = nil, nil
}

func (a *App) emuFreeTopoName(name string) string {
	n := name
	for i := 2; ; i++ {
		if _, err := os.Stat(a.emuTopoPath(n)); err != nil {
			return n
		}
		n = fmt.Sprintf("%s (%d)", name, i)
	}
}

// EmuDuplicateTopology saves a copy of a topology under another name: a lab
// of its own (its switches boot from their images, sessions of their own).
func (a *App) EmuDuplicateTopology(name, newName string) ([]EmuTopoInfo, error) {
	defer emuAs("user")()
	newName, err := emuTopoName(newName)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(a.emuTopoPath(newName)); err == nil {
		return nil, fmt.Errorf("a topology named %q is there already", newName)
	}
	t, err := a.emuReadTopo(name)
	if err != nil {
		return nil, err
	}
	t.Name = newName
	emuLab.mu.Lock()
	oth := a.emuOthers()
	emuLab.mu.Unlock()
	emuFresh(&t, oth)
	if err := a.emuPutTopo(t); err != nil {
		return nil, err
	}
	return a.EmuTopologies(), nil
}

// EmuRenameTopology gives a saved topology another name (the open one too).
func (a *App) EmuRenameTopology(name, newName string) ([]EmuTopoInfo, error) {
	defer emuAs("user")()
	newName, err := emuTopoName(newName)
	if err != nil {
		return nil, err
	}
	if newName == name {
		return a.EmuTopologies(), nil
	}
	if _, err := os.Stat(a.emuTopoPath(newName)); err == nil {
		return nil, fmt.Errorf("a topology named %q is there already", newName)
	}
	emuLab.mu.Lock()
	open := emuLab.f.Topology == name
	if open {
		emuLab.f.Topology = newName
		err = a.emuWriteTopo()
		if err == nil {
			err = a.emuSave()
		}
	}
	emuLab.mu.Unlock()
	if !open {
		var t emuTopo
		if t, err = a.emuReadTopo(name); err == nil {
			t.Name = newName
			err = a.emuPutTopo(t)
		}
	}
	if err != nil {
		return nil, err
	}
	_ = os.Remove(a.emuTopoPath(name))
	return a.EmuTopologies(), nil
}

// EmuExportTopology writes a topology to a file (.twlab) to open on another computer.
func (a *App) EmuExportTopology(name, path string) error {
	defer emuAs("user")()
	t, err := a.emuReadTopo(name)
	if err != nil {
		return err
	}
	t.Sessions, t.Consoles = nil, nil // this computer's sessions
	b, err := json.MarshalIndent(struct {
		Kind string `json:"kind"`
		emuTopo
	}{"termwright-lab-topology", t}, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// EmuImportTopology adds a topology from a .twlab file (a lab of its own; when
// its hosts are not yours, Open asks which of yours to use).
func (a *App) EmuImportTopology(path string) ([]EmuTopoInfo, error) {
	defer emuAs("user")()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t emuTopo
	if err := json.Unmarshal(b, &t); err != nil || t.Lab == nil || t.Name == "" {
		return nil, errors.New("not a Termwright lab topology")
	}
	if t.Name, err = emuTopoName(t.Name); err != nil {
		return nil, err
	}
	t.Name = a.emuFreeTopoName(t.Name)
	emuLab.mu.Lock()
	oth := a.emuOthers()
	emuLab.mu.Unlock()
	emuFresh(&t, oth)
	if err := a.emuPutTopo(t); err != nil {
		return nil, err
	}
	return a.EmuTopologies(), nil
}

// EmuSetUplink puts a data port of a switch on its host's network (like a
// cloud node on that port: the host's bridge, or macvtap on its network port),
// or takes it off; it applies when the switch starts.
func (a *App) EmuSetUplink(id, port string, on bool) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such switch")
	}
	im := emuImage(v.Image)
	n := emuPort(im, port)
	if n == 0 {
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("no data port %s on that switch", port)
	}
	if on {
		for _, c := range l.Cables {
			if c.A == id && c.APort == n || c.B == id && c.BPort == n {
				emuLab.mu.Unlock()
				return EmuView{}, errors.New("that port has a cable: remove it first")
			}
		}
		if ln := a.emuFor(emuHostOf(*v)); ln == nil || ln.LanIf == "" {
			emuLab.mu.Unlock()
			return EmuView{}, errors.New("the host's network port is not known: check the host again (Hosts)")
		}
		if !slices.Contains(v.Uplinks, n) {
			v.Uplinks = append(v.Uplinks, n)
		}
		emuEv(id, "info", "port "+port+" goes on the host's network when it starts")
	} else {
		v.Uplinks = slices.DeleteFunc(v.Uplinks, func(x int) bool { return x == n })
		emuEv(id, "info", "port "+port+" comes off the host's network when it restarts")
	}
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}
