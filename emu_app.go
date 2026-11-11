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
	"os"
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
	Sessions  map[string]string `json:"sessions"` // VM → its SSH session
	Consoles  map[string]string `json:"consoles"` // VM → its console session
}

type emuConn struct {
	c     *ssh.Client
	close func()
}

var emuLab struct {
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
	cpu              map[string]float64 // CPU use now, in cores (1 = one core busy)
	ready            map[string]bool    // the console showed the ready text since the VM started (it may scroll out of the log's end later)
	personaAfterBoot map[string]bool    // the persona file is written once the VM has booted (its folder did not exist yet)
	perr             map[string]string  // why the persona file could not be written
	packProfiles     []emu.Image        // the device pack's image profiles (refreshed by EmuState)
	personaTried     map[string]bool    // that was tried once already (no second round)
	pinfo            map[string]string  // where the persona file went, or what the check after boot saw
	pcheck           map[string]bool    // the persona was checked on the CLI since the VM started
	events           []EmuEvent         // the Events log, newest last
	download         map[string]string  // image → download progress ("waiting", "41 %")
	dlQueue          []string           // images waiting to download, in order
	dlCur            string             // the image downloading now
	dlCancel         context.CancelFunc // stops it
	dlRunning        bool
}

// EmuDevice is a VM as the Lab window shows it (the fields of a simulated switch, and more).
type EmuDevice struct {
	ID           string       `json:"id"`
	OS           string       `json:"os"` // device type
	Model        string       `json:"model"`
	Image        string       `json:"image"`
	Version      string       `json:"version"`
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
	CPU          float64      `json:"cpu"`   // % of its vCPUs, now
	Cores        float64      `json:"cores"` // host cores it uses now
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
}

// emuEv adds to the Events log (emuLab.mu held).
func emuEv(id, level, text string) {
	name := orStr(id, "images")
	if l := emuLab.f.Lab; l != nil {
		if v := l.VMByID(id); v != nil {
			name = v.Name
		}
	}
	emuLab.events = append(emuLab.events, EmuEvent{At: time.Now(), VM: id, Name: name, Level: level, Text: text})
	if n := len(emuLab.events); n > 400 {
		emuLab.events = append([]EmuEvent(nil), emuLab.events[n-300:]...)
	}
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
	CloudGw       string            `json:"cloudGw,omitempty"` // cloud: the gateway the switches use
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
	emuLab.conns, emuLab.state, emuLab.errs, emuLab.day0, emuLab.upload, emuLab.starting = map[string]*emuConn{}, map[string]string{}, map[string]string{}, map[string]bool{}, map[string]string{}, map[string]time.Time{}
	emuLab.ticks, emuLab.cpu, emuLab.ready, emuLab.personaAfterBoot = map[string]float64{}, map[string]float64{}, map[string]bool{}, map[string]bool{}
	emuLab.personaTried, emuLab.perr = map[string]bool{}, map[string]string{}
	emuLab.pinfo, emuLab.pcheck = map[string]string{}, map[string]bool{}
	emuLab.download = map[string]string{}
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
	emuLab.mu.Lock()
	emuLab.events = nil
	emuLab.mu.Unlock()
	return a.EmuState()
}

// emuDownloadDir is where downloaded images are kept on this computer.
func (a *App) emuDownloadDir() string { return filepath.Join(a.labDir(), "images") }

// EmuDownload downloads the files of images that have a download link (from a
// GNS3 appliance) to this computer, one after the other, and checks their MD5
// sums; each becomes the image's file. Progress shows on the images.
func (a *App) EmuDownload(ids []string) (EmuView, error) {
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
		go a.emuDownloadWorker()
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
		}
		emuLab.mu.Unlock()
		if !stopped {
			a.emuNotify(err, "")
		}
	}
}

// EmuDownloadStop stops an image's download, or takes it out of the queue.
func (a *App) EmuDownloadStop(id string) (EmuView, error) {
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
	if out, _ := a.emuRun(host, "cat "+emu.Q(remote+".sha256")+" 2>/dev/null", 30*time.Second); strings.TrimSpace(out) == sha && sha != "" {
		return nil
	}
	if file == "" {
		if slot != "" {
			return fmt.Errorf("the %s file of the image %s (%s) is not chosen yet: Images → Download or Choose", emuDiskName(slot), im.Name, fileName)
		}
		return fmt.Errorf("the file of the image %s is not chosen yet", im.Name)
	}
	im.File, im.SHA256, im.Size = file, sha, size
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
			mgmt = emuBestMgmt(info)
		}
		if err := emuMgmtOK(info, mgmt); err != nil {
			emuLab.mu.Unlock()
			return EmuView{}, err
		}
		l = &emu.Lab{ID: randHex(1), Dir: "termwright-lab", Mgmt: mgmt, LanIf: info.LanIf, LanKind: info.LanKind, MgmtNet: orStr(r.MgmtNet, "10.99.0.0/24"), Base: 20000 + int(randByte())%40*100, KVM: info.KVM == "yes"}
		if mgmt == "cloud" {
			if err := emuCloud(l, info, r.MgmtNet); err != nil {
				emuLab.mu.Unlock()
				return EmuView{}, err
			}
		}
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
			v := emu.VM{ID: "v" + randHex(3), Name: name, Image: im.ID, Index: next, CPUs: orInt(g.CPUs, im.CPUs), RAM: orInt(g.RAM, im.RAM)}
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
// emuTarget is where a VM's SSH session goes (and through which jump host).
func emuTarget(l *emu.Lab, v emu.VM, host string) (string, int, string) {
	switch l.Mgmt {
	case "bridge", "cloud":
		return strings.Split(v.MgmtIP, "/")[0], 22, host // cloud: on the host's network, through the host so it works from anywhere the host does
	case "nat":
		return orStr(strings.Split(v.MgmtIP, "/")[0], "0.0.0.0"), 22, host // reached through the host
	case "lan":
		if l.LanKind == "macvtap" {
			return orStr(strings.Split(v.MgmtIP, "/")[0], "0.0.0.0"), 22, "" // the host cannot reach its own macvtap guests: straight from this computer
		}
		return orStr(strings.Split(v.MgmtIP, "/")[0], "0.0.0.0"), 22, host
	}
	return "127.0.0.1", l.SSHPort(v), host
}

// emuRetarget points a VM's SSH session where its management is now.
func (a *App) emuRetarget(v emu.VM) {
	emuLab.mu.Lock()
	l, host, path := emuLab.f.Lab, emuLab.f.Host, emuLab.f.Sessions[v.ID]
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
	_ = a.SetSessionPassword(path, pw, true)
}

func (a *App) emuSessions(v emu.VM) {
	emuLab.mu.Lock()
	l, host, folder := emuLab.f.Lab, emuLab.f.Host, emuLab.f.Folder
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
		emuEv(v.ID, "info", "starting on "+host)
		delete(emuLab.pcheck, v.ID) // the persona is checked again on this boot
		delete(emuLab.ready, v.ID)  // its console log starts empty
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
			if f[1] == "all" { // nothing could start
				for _, v := range vms {
					emuLab.errs[v.ID] = strings.TrimSpace(strings.Join(f[2:], " "))
					emuEv(v.ID, "error", "did not start: "+emuLab.errs[v.ID])
				}
				continue
			}
			emuLab.errs[f[1]] = strings.TrimSpace(strings.Join(f[2:], " "))
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
	go a.emuWatch(host, ids)
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
			if im == nil || !emuLab.ready[id] {
				continue
			}
			delete(pending, id)
			if emuLab.personaAfterBoot[id] {
				delete(emuLab.personaAfterBoot, id)
				go a.emuPersonaReboot(host, id)
				continue
			}
			needDay0 := !v.Day0Done && len(im.Day0) > 0
			needCheck := im.Persona != nil && im.Persona.Check != "" && v.Persona != nil && v.Persona.Applied != "" && !emuLab.pcheck[id]
			if (needDay0 || needCheck || l.Mgmt == "lan" || l.Fixed() && len(im.AddrSet) > 0) && !emuLab.day0[id] {
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
		emuLab.mu.Lock()
		emuEv(v.ID, "info", fmt.Sprintf("answered %q with %s", q, sent))
		emuLab.mu.Unlock()
	}
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
	if fixed {
		// the management address is fixed: typed at each start (the switch's own
		// configuration need not be saved for it)
		emuLab.mu.Lock()
		emuLab.state[v.ID] = "address"
		emuLab.mu.Unlock()
		set := im
		set.Day0 = im.AddrSet
		if err := emu.Day0(ctx, c, &set, v, v.Index+1, nil); err != nil {
			fail(emuWithTail(fmt.Errorf("setting its address %s: %w", v.MgmtIP, err), c))
			return
		}
		emuLab.mu.Lock()
		emuEv(v.ID, "ok", "has the address "+strings.Split(v.MgmtIP, "/")[0]+" (gateway "+orStr(v.Gateway, "none")+") - its SSH session points there")
		emuLab.mu.Unlock()
		a.emuRetarget(v)
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
		return
	}
	a.emuNotify(nil, v.Name+" is configured")
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
	emuLab.mu.Lock()
	l, host := emuLab.f.Lab, emuLab.f.Host
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	emuLab.mu.Unlock()
	st := a.emuPollFresh()
	emuLab.mu.Lock()
	if len(st.Up) > 0 {
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
	if info == nil || info.Dir == "" {
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
	l = emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	if mgmt == "cloud" {
		if err := emuCloud(l, info, mgmtNet); err != nil {
			emuLab.mu.Unlock()
			return EmuView{}, err
		}
	} else if mgmt == "bridge" && strings.TrimSpace(mgmtNet) != "" {
		if _, err := netip.ParsePrefix(strings.TrimSpace(mgmtNet)); err != nil {
			emuLab.mu.Unlock()
			return EmuView{}, fmt.Errorf("management network %q: use an IPv4 network like 10.99.0.0/24", mgmtNet)
		}
		l.MgmtNet = strings.TrimSpace(mgmtNet)
	}
	if mgmt != "cloud" && l.Mgmt == "cloud" {
		l.MgmtNet = "10.99.0.0/24" // the cloud's first address is no bridge network
	}
	old := l.Mgmt
	l.Mgmt, l.LanIf, l.LanKind = mgmt, info.LanIf, info.LanKind
	for i := range l.VMs {
		v := &l.VMs[i]
		v.MgmtIP, v.Gateway = l.Address(v.Index)
		if old != mgmt {
			if im := emuImage(v.Image); im != nil && (strings.Contains(strings.Join(im.Day0, "\n"), "{mgmt") || strings.Contains(strings.Join(im.Day0, "\n"), "{gateway}")) {
				v.Day0Done = false // its address is part of day 0
			}
		}
	}
	vms := append([]emu.VM(nil), l.VMs...)
	_ = a.emuSave()
	emuLab.mu.Unlock()
	for _, v := range vms {
		a.emuRetarget(v)
	}
	return a.EmuState()
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
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	switch {
	case err != nil:
		emuLab.pinfo[v.ID] = "not checked: " + err.Error()
		emuEv(v.ID, "warn", "persona "+emuLab.pinfo[v.ID])
	case strings.Contains(strings.ToLower(out), strings.ToLower(model)):
		emuLab.pinfo[v.ID] = "checked: " + strings.ReplaceAll(im.Persona.Check, "\n", " + ") + " shows " + model
		delete(emuLab.perr, v.ID)
		emuEv(v.ID, "ok", "persona "+emuLab.pinfo[v.ID])
	default:
		defer func() { emuEv(v.ID, "error", "no persona: "+emuLab.perr[v.ID]) }()
		emuLab.perr[v.ID] = fmt.Sprintf("the switch did not take the persona: %q does not show %s (it reads %s at boot - see where it was written below)", im.Persona.Check, model, im.Persona.File)
		if o := strings.Join(strings.Fields(emu.CleanConsole(out)), " "); o != "" {
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
			v.MgmtIP = addr
		}
	}
	delete(emuLab.errs, id)
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
		for _, v := range l.VMs {
			if _, up := st.Up[v.ID]; !up {
				delete(emuLab.ready, v.ID)
			} else if im := emuImage(v.Image); im != nil && !emuLab.ready[v.ID] && im.ReadyIn(st.Log[v.ID]) {
				emuLab.ready[v.ID] = true
				emuEv(v.ID, "ok", "booted - the console shows "+orStr(im.Ready, "its prompt"))
			}
		}
		if l.Mgmt == "nat" { // addresses from the NAT network's DHCP leases: no CLI needed (any image)
			bits := "24"
			if _, b, ok := strings.Cut(st.NatNet, "/"); ok {
				bits = b
			}
			for i := range l.VMs {
				v := &l.VMs[i]
				if _, up := st.Up[v.ID]; !up {
					continue
				}
				if ip := st.Leases[strings.ToLower(l.MAC(*v, 0))]; ip != "" && v.MgmtIP != ip+"/"+bits {
					v.Gateway, v.MgmtIP = strings.Split(st.NatNet, "/")[0], ip+"/"+bits
					go func(id, name, addr string) {
						a.emuSetAddress(id, addr)
						a.emuNotify(nil, name+" has the address "+strings.Split(addr, "/")[0])
					}(v.ID, v.Name, ip+"/"+bits)
				}
			}
		}
		emuLab.status = st
		emuLab.statusAt = now
	}
	return emuLab.status
}

// EmuState returns the emulated lab (and reads its VMs' state on the host).
func (a *App) EmuState() (EmuView, error) {
	prof := a.emuPackProfiles()
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.packProfiles = prof
	if emuTopUp(emuLab.f.Images, prof) {
		_ = a.emuSave()
	}
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
	v := EmuView{Kind: "emu", DownloadDir: a.emuDownloadDir(), DownloadQueue: slices.DeleteFunc(append([]string{emuLab.dlCur}, emuLab.dlQueue...), func(x string) bool { return x == "" }), Events: append([]EmuEvent(nil), emuLab.events[max(0, len(emuLab.events)-200):]...), DeviceTypes: a.emuDeviceTypes(emuLab.packProfiles), Templates: f.Templates, Folder: f.Folder, Host: f.Host, Hosts: f.Hosts, Sessions: f.Sessions, Consoles: f.Consoles, OS: map[string]LabOS{}}
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
	v.Mgmt, v.MgmtNet, v.KVM, v.CloudGw = l.Mgmt, l.MgmtNet, l.KVM, l.CloudGw
	st := emuLab.status
	v.Usage.HostCPUs, v.Usage.HostMemTotal, v.Usage.HostMemFree, v.Usage.Load = st.Cores, st.MemTotal, st.MemFree, st.Load
	for _, vm := range l.VMs {
		im := emuImage(vm.Image)
		d := EmuDevice{ID: vm.ID, Hostname: vm.Name, Image: vm.Image, MgmtIP: vm.MgmtIP, MAC: l.MAC(vm, 0), X: vm.X, Y: vm.Y, CPUs: vm.CPUs, RAM: vm.RAM,
			Console: l.ConsolePort(vm), Port: l.SSHPort(vm), Error: emuLab.errs[vm.ID]}
		if im != nil {
			d.Model, d.OS, d.Ports = im.Name, orStr(im.DeviceType, "VM"), im.DataPorts()
			onHost[im.ID] = true
		}
		switch {
		case (l.Mgmt == "lan" || l.Mgmt == "nat") && vm.MgmtIP == "":
			d.SSH = "DHCP (not known yet)"
		case l.Mgmt == "nat":
			d.SSH = strings.Split(vm.MgmtIP, "/")[0] + ":22 through the host"
		case l.Mgmt == "bridge" || l.Mgmt == "cloud":
			d.SSH = strings.Split(vm.MgmtIP, "/")[0] + ":22 through the host"
		case l.Mgmt == "lan":
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
		case s == "persona":
			d.State, d.StateText = "booting", "writing its persona file"
		case s == "dhcp":
			d.State, d.StateText, d.Running = "day0", "waiting for its DHCP address", true
		case s == "persona check":
			d.State, d.StateText, d.Running = "day0", "checking its persona", true
		case strings.HasPrefix(s, "day0"):
			d.State, d.StateText, d.Running = "day0", "day-0 config "+strings.TrimPrefix(strings.TrimPrefix(s, "day0"), " "), true
		case !up:
			d.State, d.StateText = "stopped", "stopped"
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
		if im != nil && im.Persona != nil && vm.Persona != nil {
			if txt, err := im.Persona.Render(vm, vm.Persona); err == nil {
				d.PersonaOK = emu.Hash(txt) == vm.Persona.Applied
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
	for _, id := range ids {
		emuEv(id, "info", "stopped")
	}
	emuLab.mu.Unlock()
	if _, err := a.emuRun(host, script, time.Minute); err != nil {
		return EmuView{}, err
	}
	a.emuPollFresh()
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
	emuLab.mu.Lock()
	delete(emuLab.state, id)
	emuLab.mu.Unlock()
	a.emuStart(host, []string{id}) // writes the file (the disk has its folder now), then boots
}

// EmuApplyPersona writes the switches' persona files: running ones are powered off for it and started again.
func (a *App) EmuApplyPersona(ids []string) (EmuView, error) {
	host, ids := a.emuIDs(ids)
	if host == "" {
		return EmuView{}, errors.New("there is no emulated lab")
	}
	st := a.emuPollFresh()
	for _, id := range ids {
		if _, up := st.Up[id]; up {
			go a.emuPersonaReboot(host, id)
		} else {
			go a.emuStart(host, []string{id})
		}
	}
	time.Sleep(50 * time.Millisecond)
	return a.EmuState()
}

// EmuSetPersona keeps a switch's persona (model, serial numbers, modules, stack
// members); it is written into its disk on the next start or EmuApplyPersona.
func (a *App) EmuSetPersona(id string, p emu.Persona) (EmuView, error) {
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
	mon, stop := l.MonitorPort(*v), l.StopScript([]string{id})
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

// emuTopUp gives images made from a pack profile what a newer pack adds to
// it: console answers and the persona check (what the user set stays).
func emuTopUp(imgs []emu.Image, prof []emu.Image) bool {
	changed := false
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
			if len(im.AddrSet) == 0 && len(p.AddrSet) > 0 {
				im.AddrSet, changed = p.AddrSet, true
			}
			if p.Persona != nil && im.Persona != nil && im.Persona.Check == "" && p.Persona.Check != "" {
				im.Persona.Check, changed = p.Persona.Check, true
			}
			if p.Persona != nil && im.Persona != nil && len(im.Persona.FSHas) == 0 && len(p.Persona.FSHas) > 0 {
				im.Persona.FSHas, changed = p.Persona.FSHas, true
			}
		}
	}
	return changed
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
