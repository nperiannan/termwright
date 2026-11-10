package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
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
	Hosts    []EmuHost         `json:"hosts"`
	Images   []emu.Image       `json:"images"`
	Host     string            `json:"host"` // the lab's host (session path)
	Folder   string            `json:"folder"`
	Lab      *emu.Lab          `json:"lab"`
	Sessions map[string]string `json:"sessions"` // VM → its SSH session
	Consoles map[string]string `json:"consoles"` // VM → its console session
}

type emuConn struct {
	c     *ssh.Client
	close func()
}

var emuLab struct {
	mu       sync.Mutex
	f        emuFile
	loaded   bool
	conns    map[string]*emuConn
	status   emu.Status
	statusAt time.Time
	polling  bool
	state    map[string]string // VM → what it does now (uploading, day-0 …) when not just booting/running
	errs     map[string]string // VM → why it failed
	day0     map[string]bool   // day-0 runs now
	upload   map[string]string // image → upload progress ("41 %")
	starting map[string]time.Time
	ticks    map[string]float64 // CPU ticks at the last poll
	ticksAt  time.Time
	cpu      map[string]float64 // CPU use now, in cores (1 = one core busy)
}

// EmuDevice is a VM as the Lab window shows it (the fields of a simulated switch, and more).
type EmuDevice struct {
	ID        string  `json:"id"`
	OS        string  `json:"os"` // device type
	Model     string  `json:"model"`
	Image     string  `json:"image"`
	Version   string  `json:"version"`
	Serial    string  `json:"serial"`
	MAC       string  `json:"mac"`
	Hostname  string  `json:"hostname"`
	MgmtIP    string  `json:"mgmtIp"`
	Port      int     `json:"port"`
	Ports     int     `json:"ports"`
	Running   bool    `json:"running"`
	Logins    int     `json:"logins"`
	Unsaved   bool    `json:"unsaved"`
	X         int     `json:"x"`
	Y         int     `json:"y"`
	State     string  `json:"state"` // stopped, uploading, booting, day0, running, failed
	StateText string  `json:"stateText"`
	CPU       float64 `json:"cpu"`   // % of its vCPUs, now
	Cores     float64 `json:"cores"` // host cores it uses now
	CPUs      int     `json:"cpus"`
	RAM       int     `json:"ram"`
	Console   int     `json:"console"`
	SSH       string  `json:"ssh"` // how its management is reached, as text
	Error     string  `json:"error"`
}

// EmuLinkView is a cable, with port names.
type EmuLinkView struct {
	A     string `json:"a"`
	APort string `json:"aPort"`
	B     string `json:"b"`
	BPort string `json:"bPort"`
	Up    bool   `json:"up"`
	Down  bool   `json:"down"`
}

// EmuImageView is an image with its state on the lab's host.
type EmuImageView struct {
	emu.Image
	HasPassword bool   `json:"hasPassword"`
	OnHost      bool   `json:"onHost"`
	Upload      string `json:"upload"`
	Missing     bool   `json:"missing"` // the file is not on this computer (any more)
}

// EmuView is the emulated lab for the Lab window (shaped like LabView).
type EmuView struct {
	Kind     string            `json:"kind"` // "emu"
	Exists   bool              `json:"exists"`
	Running  bool              `json:"running"`
	Folder   string            `json:"folder"`
	MgmtNet  string            `json:"mgmtNet"`
	Mgmt     string            `json:"mgmt"`
	Devices  []EmuDevice       `json:"devices"`
	Links    []EmuLinkView     `json:"links"`
	Sessions map[string]string `json:"sessions"`
	Consoles map[string]string `json:"consoles"`
	Models   []LabModel        `json:"models"`
	OS       map[string]LabOS  `json:"os"`
	Host     string            `json:"host"`
	HostInfo *emu.HostInfo     `json:"hostInfo"`
	Hosts    []EmuHost         `json:"hosts"`
	Images   []EmuImageView    `json:"images"`
	KVM      bool              `json:"kvm"`
}

// EmuGroup asks for Count VMs of an image.
type EmuGroup struct {
	Image string `json:"image"`
	Count int    `json:"count"`
	Names string `json:"names"`
	CPUs  int    `json:"cpus"`
	RAM   int    `json:"ram"`
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
	emuLab.conns, emuLab.state, emuLab.errs, emuLab.day0, emuLab.upload, emuLab.starting = map[string]*emuConn{}, map[string]string{}, map[string]string{}, map[string]bool{}, map[string]string{}, map[string]time.Time{}
	emuLab.ticks, emuLab.cpu = map[string]float64{}, map[string]float64{}
	if b, err := os.ReadFile(a.emuFilePath()); err == nil {
		_ = json.Unmarshal(b, &emuLab.f)
	}
	if emuLab.f.Folder == "" {
		emuLab.f.Folder = "Emulated Lab"
	}
}

// emuSave writes the file (lock held).
func (a *App) emuSave() error {
	_ = os.MkdirAll(a.labDir(), 0o700)
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
func (a *App) emuRun(host, script string, timeout time.Duration) (string, error) {
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
		go func() { out, err = s.CombinedOutput("sh -s"); close(done) }()
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
	sum, size, err := fileSHA256(p)
	if err != nil {
		return EmuView{}, err
	}
	base := filepath.Base(p)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	im := emu.Image{ID: randHex(4), Name: name, File: p, FileName: base, Size: size, SHA256: sum, DeviceType: guessDeviceType(name), CPUs: 1, RAM: 2048, NICs: 8, NIC: "e1000", Disk: "ide", Ready: "login:", Login: "admin"}
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
	b, err := os.ReadFile(p)
	if err != nil {
		return EmuView{}, err
	}
	ap, err := emu.ParseAppliance(b)
	if err != nil {
		return EmuView{}, err
	}
	var add []emu.Image
	for _, pr := range ap.Profiles {
		pr.ID = randHex(4)
		pr.DeviceType = guessDeviceType(pr.Name)
		if pr.FileName != "" {
			f := filepath.Join(filepath.Dir(p), pr.FileName)
			if sum, size, err := fileSHA256(f); err == nil {
				pr.File, pr.SHA256, pr.Size = f, sum, size
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

// EmuSetImageFile gives an image (from an appliance) its file.
func (a *App) EmuSetImageFile(id, p string) (EmuView, error) {
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
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuRemoveImage forgets an image that no switch uses (its copy on the hosts stays).
func (a *App) EmuRemoveImage(id string) (EmuView, error) {
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
	remote := dir + "/images/" + im.FileName
	if out, _ := a.emuRun(host, "cat "+emu.Q(remote+".sha256")+" 2>/dev/null", 30*time.Second); strings.TrimSpace(out) == im.SHA256 && im.SHA256 != "" {
		return nil
	}
	if im.File == "" {
		return fmt.Errorf("the file of the image %s is not chosen yet", im.Name)
	}
	src, err := os.Open(im.File)
	if err != nil {
		return fmt.Errorf("image %s: %w", im.Name, err)
	}
	defer src.Close()
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
	tmp := remote + ".part"
	dst, err := sc.Create(tmp)
	if err != nil {
		return fmt.Errorf("could not write %s on the host: %w", tmp, err)
	}
	pw := &progressWriter{w: dst, total: im.Size, fn: func(pct int) {
		emuLab.mu.Lock()
		emuLab.upload[im.ID] = strconv.Itoa(pct) + " %"
		emuLab.mu.Unlock()
	}}
	_, err = io.Copy(pw, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	emuLab.mu.Lock()
	delete(emuLab.upload, im.ID)
	emuLab.mu.Unlock()
	if err != nil {
		_ = sc.Remove(tmp)
		return fmt.Errorf("copying %s to the host: %w", im.Name, err)
	}
	_ = sc.Remove(remote)
	if err := sc.Rename(tmp, remote); err != nil {
		return err
	}
	if f, err := sc.Create(remote + ".sha256"); err == nil {
		_, _ = f.Write([]byte(im.SHA256 + "\n"))
		_ = f.Close()
	}
	return nil
}

type progressWriter struct {
	w     io.Writer
	n     int64
	total int64
	last  int
	fn    func(int)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.n += int64(n)
	if p.total > 0 {
		if pct := int(p.n * 100 / p.total); pct != p.last {
			p.last = pct
			p.fn(pct)
		}
	}
	return n, err
}

// EmuUpload copies an image to the lab's host now (in the background).
func (a *App) EmuUpload(id, host string) (EmuView, error) {
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
	go func() {
		err := a.emuUpload(host, cp, dir)
		emuLab.mu.Lock()
		delete(emuLab.upload, id)
		emuLab.mu.Unlock()
		a.emuNotify(err, "Image "+cp.Name+" is on the host")
	}()
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
	if emuLab.f.Lab != nil && len(emuLab.f.Lab.VMs) > 0 && r.Host != "" && r.Host != emuLab.f.Host {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("the emulated lab runs on another host: add the switches there, or remove its switches first")
	}
	var info *emu.HostInfo
	for i := range emuLab.f.Hosts {
		if emuLab.f.Hosts[i].Session == r.Host {
			info = &emuLab.f.Hosts[i].Info
		}
	}
	emuLab.mu.Unlock()
	if info == nil {
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
			switch {
			case info.Sudo && info.LanKind != "":
				mgmt = "lan"
			case info.Sudo:
				mgmt = "bridge"
			default:
				mgmt = "forward"
			}
		}
		if mgmt == "lan" && (!info.Sudo || info.LanKind == "") {
			emuLab.mu.Unlock()
			if info.Wifi {
				return EmuView{}, errors.New("the host's network port is Wi-Fi: switches cannot join it; use a bridge on the host or port forwarding")
			}
			return EmuView{}, errors.New("the host's network needs password-less sudo on the host (or a network port to join); use port forwarding")
		}
		if mgmt == "bridge" && !info.Sudo {
			emuLab.mu.Unlock()
			return EmuView{}, errors.New("a bridge needs password-less sudo on the host; use port forwarding")
		}
		l = &emu.Lab{ID: randHex(1), Dir: "termwright-lab", Mgmt: mgmt, LanIf: info.LanIf, LanKind: info.LanKind, MgmtNet: orStr(r.MgmtNet, "10.99.0.0/24"), Base: 20000 + int(randByte())%40*100, KVM: info.KVM == "yes"}
		emuLab.f.Lab, emuLab.f.Host = l, r.Host
		emuLab.f.Sessions, emuLab.f.Consoles = map[string]string{}, map[string]string{}
	}
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
		im := imgs[g.Image]
		if im == nil {
			emuLab.mu.Unlock()
			return EmuView{}, errors.New("unknown image")
		}
		names2 := orStr(g.Names, "sw-{n}")
		for i, k := 1, 0; k < g.Count; i++ {
			name := strings.ReplaceAll(names2, "{n}", strconv.Itoa(i))
			if !strings.Contains(names2, "{n}") && g.Count > 1 {
				name = names2 + "-" + strconv.Itoa(i)
			}
			if names[name] {
				continue
			}
			names[name] = true
			v := emu.VM{ID: "v" + randHex(3), Name: name, Image: im.ID, Index: next, CPUs: orInt(g.CPUs, im.CPUs), RAM: orInt(g.RAM, im.RAM)}
			if im.Serial != "" {
				v.Serial = sim.Generate(im.Serial)
			}
			v.MgmtIP, v.Gateway = l.Address(next)
			next++
			k++
			add = append(add, v)
		}
	}
	l.VMs = append(l.VMs, add...)
	emuPlace(add, l.VMs)
	l.Wire(add, r.Cabling, imgs)
	host := emuLab.f.Host
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
		go a.emuStart(host, ids)
	}
	return a.EmuState()
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
func (a *App) emuSessions(v emu.VM) {
	emuLab.mu.Lock()
	l, host, folder := emuLab.f.Lab, emuLab.f.Host, emuLab.f.Folder
	im := emuImage(v.Image)
	if im == nil || l == nil {
		emuLab.mu.Unlock()
		return
	}
	login, dt, nm := orStr(im.Login, "admin"), im.DeviceType, im.Name
	hostname, port, jump := "127.0.0.1", l.SSHPort(v), host
	switch {
	case l.Mgmt == "bridge":
		hostname, port = strings.Split(v.MgmtIP, "/")[0], 22
	case l.Mgmt == "lan":
		hostname, port = orStr(strings.Split(v.MgmtIP, "/")[0], "0.0.0.0"), 22
		if l.LanKind == "macvtap" {
			jump = "" // the host cannot reach its own macvtap guests: straight from this computer
		}
	}
	conPort := l.ConsolePort(v)
	emuLab.mu.Unlock()
	pw, _ := secrets.Lookup(emuImageKey(v.Image))
	free := func(folder, name string) string {
		n := name
		for i := 2; ; i++ {
			if _, err := a.store.Get(session.JoinPath(folder, n)); err != nil {
				return n
			}
			n = name + " (" + strconv.Itoa(i) + ")"
		}
	}
	ss := session.Session{Name: free(folder, v.Name), Folder: folder, Protocol: session.ProtoSSH2, Hostname: hostname, Port: port, Username: login, DeviceType: dt, JumpSession: jump,
		Description: []string{fmt.Sprintf("Emulated %s (Termwright lab) on its KVM host - management %s", nm, v.MgmtIP)}}
	if saved, err := a.store.Save(ss, ""); err == nil {
		if pw != "" {
			_ = a.SetSessionPassword(saved.Path, pw, false)
			if cur, err := a.store.Get(saved.Path); err == nil && !cur.SavePassword {
				cur.SavePassword = true
				_, _ = a.store.Save(cur, saved.Path)
			}
		}
		emuLab.mu.Lock()
		emuLab.f.Sessions[v.ID] = saved.Path
		emuLab.mu.Unlock()
	}
	cf := session.JoinPath(folder, "Consoles")
	cs := session.Session{Name: free(cf, v.Name+" console"), Folder: cf, Protocol: session.ProtoTelnet, Hostname: "127.0.0.1", Port: conPort, DeviceType: dt, JumpSession: host,
		Description: []string{"Serial console of the emulated switch " + v.Name}}
	if saved, err := a.store.Save(cs, ""); err == nil {
		emuLab.mu.Lock()
		emuLab.f.Consoles[v.ID] = saved.Path
		emuLab.mu.Unlock()
	}
}

// emuStart uploads the images the VMs need and starts them; their boot is then watched.
func (a *App) emuStart(host string, ids []string) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return
	}
	imgs := map[string]emu.Image{}
	var vms []emu.VM
	for _, id := range ids {
		if v := l.VMByID(id); v != nil {
			vms = append(vms, *v)
			if im := emuImage(v.Image); im != nil {
				imgs[im.ID] = *im
			}
			emuLab.state[id] = "uploading"
			delete(emuLab.errs, id)
		}
	}
	dir := l.Dir
	emuLab.mu.Unlock()
	failAll := func(err error) {
		emuLab.mu.Lock()
		for _, v := range vms {
			delete(emuLab.state, v.ID)
			emuLab.errs[v.ID] = err.Error()
		}
		emuLab.mu.Unlock()
		a.emuNotify(err, "")
	}
	for _, im := range imgs {
		if err := a.emuUpload(host, im, dir); err != nil {
			failAll(err)
			return
		}
	}
	emuLab.mu.Lock()
	ptrs := map[string]*emu.Image{}
	for k := range imgs {
		im := imgs[k]
		ptrs[k] = &im
	}
	script := l.StartScript(vms, ptrs)
	for _, v := range vms {
		emuLab.state[v.ID] = "starting"
		emuLab.starting[v.ID] = time.Now()
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
		if len(f) >= 2 && f[0] == "fail" {
			emuLab.errs[f[1]] = strings.TrimSpace(strings.Join(f[2:], " "))
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
	go a.emuWatch(host, ids)
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
		l := emuLab.f.Lab
		if l == nil || emuLab.f.Host != host {
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
			if im == nil || !im.ReadyIn(st.Log[id]) {
				continue
			}
			delete(pending, id)
			needDay0 := !v.Day0Done && len(im.Day0) > 0
			if (needDay0 || l.Mgmt == "lan") && !emuLab.day0[id] {
				day0 = append(day0, *v)
				emuLab.day0[id] = true
			}
		}
		emuLab.mu.Unlock()
		for _, v := range day0 {
			go a.emuDay0(host, v)
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
	l := emuLab.f.Lab
	im0 := emuImage(v.Image)
	if l == nil || im0 == nil {
		emuLab.mu.Unlock()
		return
	}
	im := *im0
	port := l.ConsolePort(v)
	cur := l.VMByID(v.ID)
	doDay0 := cur != nil && !cur.Day0Done && len(im.Day0) > 0
	lan := l.Mgmt == "lan"
	if doDay0 {
		emuLab.state[v.ID] = "day0"
	} else {
		emuLab.state[v.ID] = "dhcp"
	}
	emuLab.mu.Unlock()
	im.Password, _ = secrets.Lookup(emuImageKey(im.ID))
	fail := func(err error) {
		emuLab.mu.Lock()
		emuLab.errs[v.ID] = "Day-0 configuration: " + err.Error()
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
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if doDay0 {
		err = emu.Day0(ctx, c, &im, v, v.Index+1, func(i int, cmd string) {
			emuLab.mu.Lock()
			emuLab.state[v.ID] = fmt.Sprintf("day0 %d/%d", i+1, len(im.Day0))
			emuLab.mu.Unlock()
		})
		if err != nil {
			fail(err)
			return
		}
		emuLab.mu.Lock()
		if vv := l.VMByID(v.ID); vv != nil {
			vv.Day0Done = true
		}
		delete(emuLab.errs, v.ID)
		_ = a.emuSave()
		emuLab.mu.Unlock()
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
			fail(fmt.Errorf("no DHCP address after 2 minutes: %v", err))
			return
		}
		a.emuSetAddress(v.ID, addr)
		a.emuNotify(nil, v.Name+" has the address "+strings.Split(addr, "/")[0])
		return
	}
	a.emuNotify(nil, v.Name+" is configured")
}

// emuSetAddress keeps a VM's management address (learned from DHCP) and
// points its SSH session there.
func (a *App) emuSetAddress(id, addr string) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	path := emuLab.f.Sessions[id]
	if l != nil {
		if v := l.VMByID(id); v != nil {
			v.MgmtIP = addr
		}
	}
	delete(emuLab.errs, id)
	_ = a.emuSave()
	emuLab.mu.Unlock()
	if s, err := a.store.Get(path); err == nil {
		s.Hostname, s.Port = strings.Split(addr, "/")[0], 22
		_, _ = a.store.Save(s, path)
	}
}

// emuPoll reads the VMs' state on the host (at most every 2 s; tails: with
// console log ends for the ones not ready yet).
func (a *App) emuPoll(force bool) emu.Status {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil || len(l.VMs) == 0 || emuLab.polling || !force && time.Since(emuLab.statusAt) < 2*time.Second {
		st := emuLab.status
		emuLab.mu.Unlock()
		return st
	}
	emuLab.polling = true
	var tails []string
	for _, v := range l.VMs {
		tails = append(tails, v.ID)
	}
	script, host := l.StatusScript(tails), emuLab.f.Host
	emuLab.mu.Unlock()
	out, err := a.emuRun(host, script, 20*time.Second)
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	emuLab.polling = false
	if err == nil {
		st := emu.ParseStatus(out, base64.StdEncoding.DecodeString)
		now := time.Now()
		// CPU use since the last poll (ps would give the average since the VM started)
		if dt := now.Sub(emuLab.ticksAt).Seconds(); dt > 0.5 && st.Clk > 0 {
			for id, t := range st.Up {
				if prev, ok := emuLab.ticks[id]; ok && t >= prev {
					emuLab.cpu[id] = (t - prev) / st.Clk / dt
				}
			}
		}
		emuLab.ticks, emuLab.ticksAt = map[string]float64{}, now
		for id, t := range st.Up {
			emuLab.ticks[id] = t
		}
		emuLab.status = st
		emuLab.statusAt = now
	}
	return emuLab.status
}

// EmuState returns the emulated lab (and reads its VMs' state on the host).
func (a *App) EmuState() (EmuView, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	has := emuLab.f.Lab != nil && len(emuLab.f.Lab.VMs) > 0
	emuLab.mu.Unlock()
	if has {
		a.emuPoll(false)
	}
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	return a.emuView(), nil
}

func (a *App) emuView() EmuView {
	f := &emuLab.f
	v := EmuView{Kind: "emu", Folder: f.Folder, Host: f.Host, Hosts: f.Hosts, Sessions: f.Sessions, Consoles: f.Consoles, OS: map[string]LabOS{}}
	for i := range f.Hosts {
		if f.Hosts[i].Session == f.Host {
			v.HostInfo = &f.Hosts[i].Info
		}
	}
	onHost := map[string]bool{}
	for _, im := range f.Images {
		iv := EmuImageView{Image: im, Upload: emuLab.upload[im.ID]}
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
	v.Mgmt, v.MgmtNet, v.KVM = l.Mgmt, l.MgmtNet, l.KVM
	st := emuLab.status
	for _, vm := range l.VMs {
		im := emuImage(vm.Image)
		d := EmuDevice{ID: vm.ID, Hostname: vm.Name, Image: vm.Image, MgmtIP: vm.MgmtIP, MAC: l.MAC(vm, 0), X: vm.X, Y: vm.Y, CPUs: vm.CPUs, RAM: vm.RAM,
			Console: l.ConsolePort(vm), Port: l.SSHPort(vm), Error: emuLab.errs[vm.ID]}
		if im != nil {
			d.Model, d.OS, d.Ports = im.Name, orStr(im.DeviceType, "VM"), im.DataPorts()
			onHost[im.ID] = true
		}
		switch {
		case l.Mgmt == "lan" && vm.MgmtIP == "":
			d.SSH = "DHCP (not known yet)"
		case l.Mgmt == "bridge" || l.Mgmt == "lan":
			d.SSH = strings.Split(vm.MgmtIP, "/")[0] + ":22"
		default:
			d.SSH = "127.0.0.1:" + strconv.Itoa(d.Port) + " on the host"
		}
		_, up := st.Up[vm.ID]
		// % of its vCPUs; QEMU's own threads (disk, network, emulation) can add a little more
		d.CPU = emuLab.cpu[vm.ID] * 100 / float64(orInt(vm.CPUs, 1))
		d.Cores = emuLab.cpu[vm.ID]
		d.Serial = vm.Serial
		switch s := emuLab.state[vm.ID]; {
		case s == "uploading":
			d.State, d.StateText = "uploading", "copying the image"
			if im != nil && emuLab.upload[im.ID] != "" {
				d.StateText += " · " + emuLab.upload[im.ID]
			}
		case s == "starting":
			d.State, d.StateText = "booting", "starting"
		case s == "dhcp":
			d.State, d.StateText, d.Running = "day0", "waiting for its DHCP address", true
		case strings.HasPrefix(s, "day0"):
			d.State, d.StateText, d.Running = "day0", "day-0 config "+strings.TrimPrefix(strings.TrimPrefix(s, "day0"), " "), true
		case !up:
			d.State, d.StateText = "stopped", "stopped"
			if d.Error != "" {
				d.State, d.StateText = "failed", "failed"
			}
		case im != nil && !im.ReadyIn(st.Log[vm.ID]):
			d.State, d.StateText, d.Running = "booting", "booting", true
			if t, ok := emuLab.starting[vm.ID]; ok {
				d.StateText += " · " + fmtDur(time.Since(t))
			}
		default:
			d.State, d.StateText, d.Running = "running", "running", true
			if d.Error != "" {
				d.StateText = "running · day-0 failed"
			}
		}
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
	host, ids := a.emuIDs(ids)
	if host == "" {
		return EmuView{}, errors.New("there is no emulated lab")
	}
	go a.emuStart(host, ids)
	time.Sleep(50 * time.Millisecond)
	return a.EmuState()
}

// EmuStop stops VMs (all when ids is empty): like pulling the power.
func (a *App) EmuStop(ids []string) (EmuView, error) {
	host, ids := a.emuIDs(ids)
	if host == "" {
		return EmuView{}, errors.New("there is no emulated lab")
	}
	emuLab.mu.Lock()
	script := emuLab.f.Lab.StopScript(ids)
	emuLab.mu.Unlock()
	if _, err := a.emuRun(host, script, time.Minute); err != nil {
		return EmuView{}, err
	}
	a.emuPoll(true)
	return a.EmuState()
}

// EmuRestart stops and starts VMs (a changed cable needs this).
func (a *App) EmuRestart(ids []string) (EmuView, error) {
	if _, err := a.EmuStop(ids); err != nil {
		return EmuView{}, err
	}
	return a.EmuStart(ids)
}

// EmuReset stops VMs, throws their disks away and boots them as new: day 0 is typed again.
func (a *App) EmuReset(ids []string) (EmuView, error) {
	host, ids := a.emuIDs(ids)
	if host == "" {
		return EmuView{}, errors.New("there is no emulated lab")
	}
	emuLab.mu.Lock()
	script := emuLab.f.Lab.ResetScript(ids)
	for _, id := range ids {
		if v := emuLab.f.Lab.VMByID(id); v != nil {
			v.Day0Done = false
		}
	}
	_ = a.emuSave()
	emuLab.mu.Unlock()
	if _, err := a.emuRun(host, script, time.Minute); err != nil {
		return EmuView{}, err
	}
	return a.EmuStart(ids)
}

// EmuRemove removes VMs from the lab: they stop, their disks on the host are
// deleted, and their sessions too when sessions is set.
func (a *App) EmuRemove(ids []string, sessions bool) (EmuView, error) {
	host, ids := a.emuIDs(ids)
	if host == "" {
		return EmuView{}, errors.New("there is no emulated lab")
	}
	emuLab.mu.Lock()
	script := emuLab.f.Lab.ResetScript(ids)
	emuLab.mu.Unlock()
	_, _ = a.emuRun(host, script, time.Minute)
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
	}
	var ends []end
	for i := range l.Cables {
		c := &l.Cables[i]
		if c.A == id && c.APort == p || c.B == id && c.BPort == p {
			c.Down = !up
			if x := l.VMByID(c.A); x != nil {
				ends = append(ends, end{l.MonitorPort(*x), c.APort})
			}
			if x := l.VMByID(c.B); x != nil {
				ends = append(ends, end{l.MonitorPort(*x), c.BPort})
			}
		}
	}
	host := emuLab.f.Host
	_ = a.emuSave()
	emuLab.mu.Unlock()
	if cl, err := a.emuClient(context.Background(), host); err == nil {
		for _, e := range ends {
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
	go func() {
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
	}()
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
	s.Stderr = s.Stdout
	if err := s.Start(emu.SetupCommand(password != "")); err != nil {
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
		} else if i < len(list) {
			v.Serial = list[i]
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
