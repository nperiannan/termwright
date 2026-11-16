package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/emu/fastbin"
	"github.com/termwright/termwright/internal/trex"
)

// What sends the streams: on the KVM hosts the built-in engine (traffic.py) or the fast one (cmd/twfast, embedded
// here and copied to the hosts); a host's spare network card cabled to a real device as a test port; TRex servers,
// whose ports are test ports and whose streams TRex sends at line rate.

// EmuEngines is the Traffic engines dialog.
type EmuEngines struct {
	Engine string          `json:"engine"` // the KVM hosts' engine: python (built in) or fast
	Hosts  []EmuEngineHost `json:"hosts"`  // the lab's hosts with test ports
	Trex   []EmuTrex       `json:"trex"`   // the TRex servers
}

// EmuEngineHost is one host's engine.
type EmuEngineHost struct {
	Host    string  `json:"host"`           // its name
	Engine  string  `json:"engine"`         // python or fast
	Arch    string  `json:"arch,omitempty"` // its CPU (uname -m)
	Running bool    `json:"running"`        // the fast engine reported in the last read
	FPS     float64 `json:"fps,omitempty"`  // the highest rate the fast engine sent there, frames/s
	Note    string  `json:"note,omitempty"` // why the fast engine does not run there
}

// EmuTrex is a TRex server and what Termwright last found there.
type EmuTrex struct {
	ID      string        `json:"id"`                // its id
	Name    string        `json:"name"`              // its name
	Addr    string        `json:"addr"`              // its address
	Port    int           `json:"port,omitempty"`    // its RPC port (default 4501)
	State   string        `json:"state,omitempty"`   // connected, unreachable, or not checked
	Version string        `json:"version,omitempty"` // TRex's version
	Err     string        `json:"err,omitempty"`     // why it could not be reached
	Ports   []EmuTrexPort `json:"ports,omitempty"`   // its ports
}

// EmuTrexPort is a TRex port.
type EmuTrexPort struct {
	ID     int    `json:"id"`               // its number
	Speed  int    `json:"speed,omitempty"`  // Mbit/s
	Link   *bool  `json:"link,omitempty"`   // up
	MAC    string `json:"mac,omitempty"`    // its MAC address
	Driver string `json:"driver,omitempty"` // its DPDK driver
	Cabled string `json:"cabled,omitempty"` // what it is cabled to
	Use    bool   `json:"use,omitempty"`    // it is a test port
	Tester string `json:"tester,omitempty"` // that test port's name
}

// EmuHostNIC is a network card of a host, and whether it can be a test port.
type EmuHostNIC struct {
	Name   string `json:"name"`             // enp3s0, …
	MAC    string `json:"mac"`              // its MAC address
	Up     bool   `json:"up"`               // its link is up
	Speed  int    `json:"speed,omitempty"`  // Mbit/s
	Driver string `json:"driver,omitempty"` // its driver
	Used   string `json:"used,omitempty"`   // why it cannot be a test port (the host's own network, a bridge's, already one)
}

// emuTrexRun is a TRex server's connection, what was found there and the streams of the run.
type emuTrexRun struct {
	c       *trex.Client
	info    EmuTrex
	h       map[int]string // port → handler
	run     uint32
	streams []emuTrexStream
	base    map[int]trex.PortStats // per port, at the start
	err     string
}

// emuTrexStream is a stream sent by TRex: its flow-statistics ids.
type emuTrexStream struct {
	id, from, to string
	port, dport  int
	main         []int // counted ids (IP frames)
	lat          int   // latency id (0: none)
	total        int   // frames it sends
}

// emuEngInit makes the engine maps (emuLab.mu held).
func emuEngInit() {
	if emuTraf.engNote == nil {
		emuTraf.engNote, emuTraf.engFPS, emuTraf.engOn, emuTraf.trex = map[string]string{}, map[string]float64{}, map[string]bool{}, map[string]*emuTrexRun{}
		emuTraf.engArch = map[string]string{}
	}
}

// emuTrafHosts are the hosts the traffic engine runs on: the lab's, and those whose network cards are test ports
// (emuLab.mu held).
func (a *App) emuTrafHosts(l *emu.Lab) []string {
	hs := l.Hosts(emuLab.f.Host)
	for _, t := range l.Testers {
		if t.NIC != "" && !slices.Contains(hs, t.Host) {
			hs = append(hs, t.Host)
		}
	}
	return hs
}

// emuTesterWhere says where a test port is: "on " a switch port (sw), a host's network card or a TRex port.
func (a *App) emuTesterWhere(t emu.Tester, sw string) string {
	cab := ""
	if t.Cabled != "" {
		cab = ", cabled to " + t.Cabled
	}
	switch t.Where() {
	case "nic":
		return "on " + t.NIC + " of " + emuHostName(t.Host) + cab
	case "trex":
		id, p := t.TrexAt()
		emuLab.mu.Lock()
		name := id
		if l := emuLab.f.Lab; l != nil {
			for _, s := range l.Trex {
				if s.ID == id {
					name = s.Name
				}
			}
		}
		emuLab.mu.Unlock()
		return fmt.Sprintf("on TRex %s port %d%s", name, p, cab)
	}
	return "on " + sw
}

// emuMaxPPS is the highest rate of a stream: TRex sends at line rate, the fast engine a few million frames a
// second, the built-in one a hundred thousand (emuLab.mu held).
func emuMaxPPS(l *emu.Lab, s emu.Stream) int {
	if t := l.TesterByID(s.From); t != nil && t.Trex != "" {
		return 100000000
	}
	if l.Engine == "fast" {
		return 2000000
	}
	return 100000
}

// emuFastUpload copies the fast engine for the host's CPU to the lab's folder there (when it is not the same
// already) and stops the old one, which traffic.json starts again.
func (a *App) emuFastUpload(host, dir string) error {
	emuLab.mu.Lock()
	emuEngInit()
	arch := emuTraf.engArch[host]
	emuLab.mu.Unlock()
	if arch == "" {
		out, err := a.emuRun(host, "uname -m", 15*time.Second)
		if err != nil {
			return fmt.Errorf("could not ask the host its CPU: %v", err)
		}
		arch = strings.TrimSpace(out)
		emuLab.mu.Lock()
		emuTraf.engArch[host] = arch
		emuLab.mu.Unlock()
	}
	b := fastbin.For(arch)
	if b == nil {
		if fastbin.For("x86_64") == nil && fastbin.For("aarch64") == nil {
			return errors.New("this build of Termwright has no fast engine (make fastbin): the built-in engine sends")
		}
		return fmt.Errorf("no fast engine for this host's CPU (%s): the built-in engine sends", arch)
	}
	sum := sha256.Sum256(b)
	want := hex.EncodeToString(sum[:])
	if out, _ := a.emuRun(host, "sha256sum "+emu.Q(dir)+"/tw-fast 2>/dev/null", 15*time.Second); strings.HasPrefix(strings.TrimSpace(out), want) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cl, err := a.emuClient(ctx, host)
	if err != nil {
		return err
	}
	sc, err := sftp.NewClient(cl)
	if err != nil {
		return fmt.Errorf("SFTP to the host: %w", err)
	}
	defer sc.Close()
	_ = sc.MkdirAll(dir)
	f, err := sc.Create(dir + "/tw-fast.tmp")
	if err != nil {
		return fmt.Errorf("could not write the fast engine on the host: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()
	_ = sc.Chmod(dir+"/tw-fast.tmp", 0o755)
	_, err = a.emuRun(host, "cd "+emu.Q(dir)+" && mv -f tw-fast.tmp tw-fast && { p=$(cat fast.pid 2>/dev/null); [ -n \"$p\" ] && { kill $p 2>/dev/null || sudo -n kill $p 2>/dev/null; }; rm -f fast.pid; true; }", 15*time.Second)
	return err
}

// EmuEngines is the Traffic engines dialog: the hosts' engine and the TRex servers.
func (a *App) EmuEngines() (EmuEngines, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuEngines{Engine: "python", Hosts: []EmuEngineHost{}, Trex: []EmuTrex{}}, nil
	}
	emuEngInit()
	v := EmuEngines{Engine: orStr(l.Engine, "python"), Hosts: []EmuEngineHost{}, Trex: []EmuTrex{}}
	var ask []string
	for _, h := range a.emuTrafHosts(l) {
		if !emuLab.trafHosts[h] && !slices.ContainsFunc(l.Testers, func(t emu.Tester) bool { return t.Host == h }) && !slices.ContainsFunc(l.VMs, func(x emu.VM) bool { return emuHostOf(x) == h }) {
			continue
		}
		e := EmuEngineHost{Host: emuHostName(h), Engine: v.Engine, Arch: emuTraf.engArch[h], Running: emuTraf.engOn[h], FPS: emuTraf.engFPS[h], Note: emuTraf.engNote[h]}
		if e.Arch == "" {
			ask = append(ask, h)
		}
		v.Hosts = append(v.Hosts, e)
	}
	for _, s := range l.Trex {
		v.Trex = append(v.Trex, a.emuTrexView(l, s))
	}
	emuLab.mu.Unlock()
	if len(ask) > 0 { // the CPUs, once
		for i, h := range ask {
			out, err := a.emuRun(h, "uname -m", 10*time.Second)
			if err != nil {
				continue
			}
			emuLab.mu.Lock()
			emuTraf.engArch[h] = strings.TrimSpace(out)
			emuLab.mu.Unlock()
			for j := range v.Hosts {
				if v.Hosts[j].Host == emuHostName(ask[i]) {
					v.Hosts[j].Arch = strings.TrimSpace(out)
				}
			}
		}
	}
	return v, nil
}

// EmuEngineSet chooses the KVM hosts' engine: python (built in) or fast. Running streams go on with the old one
// until they are started again.
func (a *App) EmuEngineSet(engine string) (EmuEngines, error) {
	defer emuAs("user")()
	if engine != "python" && engine != "fast" {
		return EmuEngines{}, errors.New("the engine is python or fast")
	}
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuEngines{}, errors.New("there is no emulated lab")
	}
	l.Engine = map[string]string{"python": "", "fast": "fast"}[engine]
	if engine == "python" { // its rates
		for i := range l.Streams {
			l.Streams[i].PPS = min(l.Streams[i].PPS, emuMaxPPS(l, l.Streams[i]))
		}
	}
	_ = a.emuSave()
	emuEv("", "info", "traffic engine: "+map[string]string{"python": "built in", "fast": "fast"}[engine])
	emuLab.mu.Unlock()
	err := a.emuTrafficPush()
	v, _ := a.EmuEngines()
	return v, err
}

// emuNICScript lists a host's network cards (not virtual ones): name|mac|state|speed|driver|bridge|addresses|default route.
const emuNICScript = `dr=$(ip route show default 2>/dev/null | awk '{for(i=1;i<NF;i++) if($i=="dev") print $(i+1)}')
for d in /sys/class/net/*; do n=${d##*/}; [ -e "$d/device" ] || continue
m=""; [ -e "$d/master" ] && m=$(basename "$(readlink "$d/master")")
a=$(ip -o addr show dev "$n" scope global 2>/dev/null | awk '{print $4}' | tr '\n' ' ')
dv=$(basename "$(readlink "$d/device/driver" 2>/dev/null)" 2>/dev/null)
r=""; for x in $dr; do [ "$x" = "$n" ] && r=y; done
echo "$n|$(cat $d/address)|$(cat $d/operstate)|$(cat $d/speed 2>/dev/null)|$dv|$m|$a|$r"; done`

// EmuHostNICs lists a host's network cards and which of them can be test ports: a spare card with no address, not
// in a bridge, cabled to a device under test.
func (a *App) EmuHostNICs(host string) ([]EmuHostNIC, error) {
	if !a.emuIsHost(host) {
		return nil, errors.New("no such host")
	}
	out, err := a.emuRun(host, emuNICScript, 20*time.Second)
	if err != nil {
		return nil, err
	}
	emuLab.mu.Lock()
	var used []emu.Tester
	if l := emuLab.f.Lab; l != nil {
		used = slices.Clone(l.Testers)
	}
	emuLab.mu.Unlock()
	nics := []EmuHostNIC{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "|")
		if len(f) < 8 {
			continue
		}
		n := EmuHostNIC{Name: f[0], MAC: f[1], Up: f[2] == "up", Driver: f[4]}
		if sp, err := strconv.Atoi(f[3]); err == nil && sp > 0 {
			n.Speed = sp
		}
		switch {
		case slices.ContainsFunc(used, func(t emu.Tester) bool { return t.Host == host && t.NIC == f[0] }):
			n.Used = "a test port already"
		case f[7] == "y":
			n.Used = "the host's own network (its default route)"
		case strings.TrimSpace(f[6]) != "":
			n.Used = "has an address: " + strings.TrimSpace(f[6])
		case f[5] != "":
			n.Used = "in the bridge " + f[5]
		}
		nics = append(nics, n)
	}
	return nics, nil
}

// emuIsHost tells whether host is one of the KVM hosts.
func (a *App) emuIsHost(host string) bool {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	a.emuLoad()
	return host != "" && (host == emuLab.f.Host || slices.ContainsFunc(emuLab.f.Hosts, func(h EmuHost) bool { return h.Session == host }))
}

// EmuTesterAddNIC makes a host's spare network card a test port: the generator sends and receives on it
// (raw sockets: the host's root, or passwordless sudo), so a real switch cabled to it is tested like a lab's.
func (a *App) EmuTesterAddNIC(host, nic, cabled, ip, gw string, vlan int) (EmuTrafficView, error) {
	defer emuAs("user")()
	nics, err := a.EmuHostNICs(host)
	if err != nil {
		return EmuTrafficView{}, err
	}
	i := slices.IndexFunc(nics, func(n EmuHostNIC) bool { return n.Name == nic })
	switch {
	case i < 0:
		return EmuTrafficView{}, fmt.Errorf("the host has no network card %s", nic)
	case nics[i].Used != "":
		return EmuTrafficView{}, fmt.Errorf("%s cannot be a test port: %s", nic, nics[i].Used)
	}
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuTrafficView{}, errors.New("there is no emulated lab")
	}
	dhcp := strings.EqualFold(ip, "dhcp")
	if dhcp {
		ip = ""
	}
	if ip != "" && !strings.Contains(ip, "/") {
		ip += "/24"
	}
	t := emu.Tester{ID: "t" + randHex(3), Name: emuTesterName(l), Host: host, NIC: nic, Cabled: strings.TrimSpace(cabled), IP: ip, GW: gw, VLAN: min(4094, max(0, vlan)), DHCP: dhcp, LLDP: true,
		MAC: strings.ToLower("02:74:77:" + randHex(1) + ":" + randHex(1) + ":" + randHex(1)), Speed: nics[i].Speed}
	l.Testers = append(l.Testers, t)
	_ = a.emuSave()
	emuEv("", "info", fmt.Sprintf("%s on %s of %s%s", t.Name, nic, emuHostName(host), map[bool]string{true: ", cabled to " + t.Cabled, false: ""}[t.Cabled != ""]))
	emuLab.mu.Unlock()
	err = a.emuTrafficPush()
	tv, _ := a.EmuTraffic()
	if !nics[i].Up {
		tv.Note = nic + " has no link: is its cable in?"
	}
	return tv, err
}

// emuTrexView is a TRex server as last found (emuLab.mu held).
func (a *App) emuTrexView(l *emu.Lab, s emu.TrexServer) EmuTrex {
	v := EmuTrex{ID: s.ID, Name: s.Name, Addr: s.Addr, Port: s.Port, State: "not checked"}
	if r := emuTraf.trex[s.ID]; r != nil {
		v.State, v.Version, v.Err, v.Ports = r.info.State, r.info.Version, r.info.Err, slices.Clone(r.info.Ports)
	}
	for i, p := range v.Ports {
		for _, t := range l.Testers {
			if id, n := t.TrexAt(); id == s.ID && n == p.ID {
				v.Ports[i].Use, v.Ports[i].Tester, v.Ports[i].Cabled = true, t.Name, t.Cabled
			}
		}
	}
	return v
}

// emuTrexConn connects to a TRex server (again, when its connection broke) and reads its ports.
func (a *App) emuTrexConn(s emu.TrexServer, fresh bool) (*emuTrexRun, error) {
	emuLab.mu.Lock()
	emuEngInit()
	r := emuTraf.trex[s.ID]
	if r == nil {
		r = &emuTrexRun{h: map[int]string{}}
		emuTraf.trex[s.ID] = r
	}
	c := r.c
	emuLab.mu.Unlock()
	if c != nil && !fresh {
		return r, nil
	}
	if c != nil {
		_ = c.Close()
	}
	addr := net.JoinHostPort(s.Addr, strconv.Itoa(cmp0(s.Port, 4501)))
	c, err := trex.Dial(addr, 5*time.Second)
	info := EmuTrex{State: "connected"}
	if err != nil {
		info = EmuTrex{State: "unreachable", Err: err.Error()}
	} else {
		info.Version = c.Version
		ps, perr := c.Ports()
		if perr != nil {
			info.Err = perr.Error()
		}
		for _, p := range ps {
			info.Ports = append(info.Ports, EmuTrexPort{ID: p.Index, Speed: p.Speed, Link: p.Link, MAC: p.MAC, Driver: p.Driver})
		}
	}
	emuLab.mu.Lock()
	r.c, r.info, r.h = c, info, map[int]string{}
	emuLab.mu.Unlock()
	return r, err
}

func cmp0(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

// EmuTrexSave adds a TRex server (no id) or changes one, and checks it: its version and ports.
func (a *App) EmuTrexSave(t EmuTrex) (EmuEngines, error) {
	defer emuAs("user")()
	t.Addr, t.Name = strings.TrimSpace(t.Addr), strings.TrimSpace(t.Name)
	if t.Addr == "" || strings.ContainsAny(t.Addr, " /") {
		return EmuEngines{}, errors.New("give the TRex server's address")
	}
	if t.Port < 0 || t.Port > 65535 {
		return EmuEngines{}, errors.New("the port is 1-65535")
	}
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuEngines{}, errors.New("there is no emulated lab")
	}
	s := emu.TrexServer{ID: t.ID, Name: orStr(t.Name, t.Addr), Addr: t.Addr, Port: t.Port}
	if i := slices.IndexFunc(l.Trex, func(x emu.TrexServer) bool { return x.ID == t.ID }); t.ID != "" && i >= 0 {
		l.Trex[i] = s
	} else {
		s.ID = "x" + randHex(3)
		l.Trex = append(l.Trex, s)
		emuEv("", "info", "TRex server "+s.Name+" added")
	}
	_ = a.emuSave()
	emuLab.mu.Unlock()
	_, _ = a.emuTrexConn(s, true) // what it found is in the view
	return a.EmuEngines()
}

// EmuTrexRemove forgets a TRex server, its test ports and their streams.
func (a *App) EmuTrexRemove(id string) (EmuEngines, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l != nil {
		gone := map[string]bool{}
		for _, t := range l.Testers {
			if x, _ := t.TrexAt(); x == id {
				gone[t.ID] = true
			}
		}
		l.Testers = slices.DeleteFunc(l.Testers, func(t emu.Tester) bool { return gone[t.ID] })
		l.Streams = slices.DeleteFunc(l.Streams, func(s emu.Stream) bool { return gone[s.From] || gone[s.To] })
		l.Trex = slices.DeleteFunc(l.Trex, func(s emu.TrexServer) bool { return s.ID == id })
		_ = a.emuSave()
	}
	emuEngInit()
	r := emuTraf.trex[id]
	delete(emuTraf.trex, id)
	emuLab.mu.Unlock()
	if r != nil && r.c != nil {
		for p, h := range r.h {
			r.c.Reset(p, h)
			_ = r.c.Release(p, h)
		}
		_ = r.c.Close()
	}
	return a.EmuEngines()
}

// EmuTrexPort makes a TRex port a test port (use), or not, and notes what it is cabled to.
func (a *App) EmuTrexPort(id string, port int, use bool, cabled string) (EmuEngines, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var s *emu.TrexServer
	if l != nil {
		if i := slices.IndexFunc(l.Trex, func(x emu.TrexServer) bool { return x.ID == id }); i >= 0 {
			s = &l.Trex[i]
		}
	}
	if s == nil {
		emuLab.mu.Unlock()
		return EmuEngines{}, errors.New("no such TRex server")
	}
	emuEngInit()
	var p *EmuTrexPort
	if r := emuTraf.trex[id]; r != nil {
		for i := range r.info.Ports {
			if r.info.Ports[i].ID == port {
				p = &r.info.Ports[i]
			}
		}
	}
	at := fmt.Sprintf("%s/%d", id, port)
	i := slices.IndexFunc(l.Testers, func(t emu.Tester) bool { return t.Trex == at })
	switch {
	case use && i >= 0:
		l.Testers[i].Cabled = strings.TrimSpace(cabled)
	case use && p == nil:
		emuLab.mu.Unlock()
		return EmuEngines{}, errors.New("check the server first: its ports are not known")
	case use:
		t := emu.Tester{ID: "t" + randHex(3), Name: emuTesterName(l), Trex: at, Cabled: strings.TrimSpace(cabled), MAC: strings.ToLower(p.MAC), Speed: p.Speed}
		if _, err := net.ParseMAC(t.MAC); err != nil {
			t.MAC = strings.ToLower("02:74:77:" + randHex(1) + ":" + randHex(1) + ":" + randHex(1))
		}
		l.Testers = append(l.Testers, t)
		emuEv("", "info", fmt.Sprintf("%s on TRex %s port %d", t.Name, s.Name, port))
	case i >= 0:
		tid := l.Testers[i].ID
		emuEv("", "info", l.Testers[i].Name+" removed")
		l.Testers = slices.Delete(l.Testers, i, i+1)
		l.Streams = slices.DeleteFunc(l.Streams, func(x emu.Stream) bool { return x.From == tid || x.To == tid })
	}
	_ = a.emuSave()
	emuLab.mu.Unlock()
	return a.EmuEngines()
}

// emuTrexCheck tells whether a stream can be sent by TRex (from a TRex port: then to one on the same server), and
// tidies it (emuLab.mu held).
func emuTrexCheck(l *emu.Lab, s *emu.Stream) error {
	f, to := l.TesterByID(s.From), l.TesterByID(s.To)
	ft, tt := f != nil && f.Trex != "", to != nil && to.Trex != ""
	if !ft && !tt {
		return nil
	}
	fs, _ := f.TrexAt()
	switch {
	case !ft || !tt:
		return errors.New("a stream from a TRex port goes to a TRex port (and the other way round): TRex sends and counts it")
	case func() bool { x, _ := to.TrexAt(); return x != fs }():
		return errors.New("both TRex ports are on the same TRex server")
	case !slices.Contains([]string{"udp", "tcp", "icmp", "raw", "arp", "custom"}, s.Kind):
		return errors.New("TRex ports send UDP, TCP SYN, ICMP, raw, ARP and custom frames (sessions and captures: from the KVM hosts' test ports)")
	case s.Encap != nil || s.Fuzz != nil || s.Bad != "" || len(s.UDFs) > 0:
		return errors.New("on TRex ports, streams have no extra headers, faults, fuzzing or user-defined fields yet")
	case s.SizeMode == "inc" || s.SizeMode == "random":
		return errors.New("on TRex ports, frame sizes are fixed or IMIX")
	case s.RTT:
		return errors.New("on TRex ports, latency is one way (TRex has one clock for both ports)")
	}
	if _, err := emuTrexFrame(*s, *f, to, max(64, s.Size)); err != nil {
		return err
	}
	return nil
}

// emuTrexOff is where a frame's headers are, for the fields that change.
type emuTrexOff struct {
	l3, l4, ihl int
	v6, ip      bool
	proto       byte
}

// emuTrexFrame is a stream's frame for TRex: Ethernet (with its VLAN tag), IPv4 or IPv6 and UDP, TCP SYN or an ICMP
// echo, padded to size with the FCS TRex adds; ARP, raw L2 and a custom frame as written.
func emuTrexFrame(s emu.Stream, f emu.Tester, to *emu.Tester, size int) ([]byte, error) {
	b, _, err := emuTrexFrameOff(s, f, to, size)
	return b, err
}

func emuTrexFrameOff(s emu.Stream, f emu.Tester, to *emu.Tester, size int) ([]byte, emuTrexOff, error) {
	var o emuTrexOff
	n := max(60, size-4)
	ip4 := func(c string) net.IP { a, _, _ := strings.Cut(c, "/"); return net.ParseIP(a) }
	src, dst := ip4(orStr(s.SrcIP, f.IP)), net.IP(nil)
	v6 := strings.Contains(s.DstIP, ":") || s.DstIP == "" && to != nil && to.IP == "" && to.IP6 != ""
	if v6 {
		src = ip4(orStr(s.SrcIP, f.IP6))
	}
	if s.DstIP != "" {
		dst = net.ParseIP(s.DstIP)
	} else if to != nil {
		dst = ip4(map[bool]string{true: to.IP6, false: to.IP}[v6])
	}
	dmac := s.DstMAC
	switch {
	case dmac != "":
	case s.Kind == "arp" || to == nil:
		dmac = "ff:ff:ff:ff:ff:ff"
	case s.Kind == "raw" || s.Kind == "custom" || v6 || f.GW == "" || emu.SameNet(f.IP, dst.String()):
		dmac = to.MAC
	default:
		return nil, o, errors.New("to another network, a TRex stream needs the gateway's MAC as its destination MAC (TRex ports do not ARP here)")
	}
	dm, err := net.ParseMAC(dmac)
	if err != nil {
		return nil, o, fmt.Errorf("the destination MAC %q is not a MAC address", dmac)
	}
	sm, err := net.ParseMAC(f.MAC)
	if err != nil {
		return nil, o, errors.New("the test port has no MAC address")
	}
	if s.Kind == "custom" {
		b, err := emu.ParseFrameHex(s.Hex)
		if err != nil || len(b) < 14 {
			return nil, o, errors.New("the custom frame is not a frame in hex")
		}
		if s.RewriteMAC {
			copy(b, dm)
			copy(b[6:], sm)
		}
		if len(b) < 60 {
			b = append(b, make([]byte, 60-len(b))...)
		}
		return b, o, nil
	}
	b := append(append([]byte{}, dm...), sm...)
	if s.VLAN > 0 {
		b = binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(b, 0x8100), uint16(s.Prio<<13|s.VLAN))
	}
	switch s.Kind {
	case "raw":
		b = binary.BigEndian.AppendUint16(b, 0x88B5)
		return append(b, make([]byte, max(0, n-len(b)))...), o, nil
	case "arp":
		if src.To4() == nil || dst.To4() == nil {
			return nil, o, errors.New("an ARP stream needs IPv4 addresses")
		}
		b = binary.BigEndian.AppendUint16(b, 0x0806)
		b = append(b, 0, 1, 8, 0, 6, 4, 0, 1)
		b = append(append(append(append(b, sm...), src.To4()...), make([]byte, 6)...), dst.To4()...)
		return append(b, make([]byte, max(0, n-len(b)))...), o, nil
	}
	if src == nil || dst == nil || v6 != (dst.To4() == nil) || v6 != (src.To4() == nil) {
		return nil, o, errors.New("give both TRex test ports addresses of the same kind (IPv4 or IPv6), or the stream its addresses")
	}
	proto := map[string]byte{"udp": 17, "tcp": 6, "icmp": 1}[s.Kind]
	if v6 && proto == 1 {
		proto = 58
	}
	o.l3, o.v6, o.ip, o.proto = len(b)+2, v6, true, proto
	if v6 {
		b = binary.BigEndian.AppendUint16(b, 0x86DD)
		b = append(b, 0x60|byte(s.DSCP>>2), byte(s.DSCP<<6), 0, 0, 0, 0, proto, 64)
		b = append(append(b, src.To16()...), dst.To16()...)
		o.ihl = 40
	} else {
		b = binary.BigEndian.AppendUint16(b, 0x0800)
		b = append(b, 0x45, byte(s.DSCP<<2), 0, 0, 0, 0, 0, 0, 64, proto, 0, 0)
		b = append(append(b, src.To4()...), dst.To4()...)
		o.ihl = 20
	}
	o.l4 = len(b)
	switch s.Kind {
	case "udp":
		b = binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(b, 5000), uint16(cmp0(s.DPort, 5001)))
		b = append(b, 0, 0, 0, 0)
	case "tcp":
		b = binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(b, 5000), uint16(cmp0(s.DPort, 80)))
		b = append(b, 0, 0, 0, 1, 0, 0, 0, 0, 0x50, 0x02, 0xff, 0xff, 0, 0, 0, 0)
	default:
		b = append(b, map[bool]byte{true: 128, false: 8}[v6], 0, 0, 0, 0x74, 0x77, 0, 1)
	}
	b = append(b, []byte("Termwright TRex")...)
	b = append(b, make([]byte, max(0, n-len(b)))...)
	l3len := len(b) - o.l3
	if v6 {
		binary.BigEndian.PutUint16(b[o.l3+4:], uint16(l3len-40))
	} else {
		binary.BigEndian.PutUint16(b[o.l3+2:], uint16(l3len))
		binary.BigEndian.PutUint16(b[o.l3+10:], emuCsum(b[o.l3:o.l3+20], 0))
	}
	l4 := b[o.l4:]
	if s.Kind == "udp" {
		binary.BigEndian.PutUint16(l4[4:], uint16(len(l4)))
	}
	ck := map[string]int{"udp": 6, "tcp": 16, "icmp": 2}[s.Kind]
	if v6 || s.Kind != "udp" { // IPv4 UDP: no checksum (0 is allowed)
		var ph uint32
		if v6 || proto != 1 {
			ph = emuPseudo(src, dst, proto, len(l4))
		}
		c := emuCsum(l4, ph)
		if c == 0 && s.Kind == "udp" {
			c = 0xFFFF
		}
		binary.BigEndian.PutUint16(l4[ck:], c)
	}
	return b, o, nil
}

// emuCsum is the Internet checksum of b, starting from a pseudo-header's sum.
func emuCsum(b []byte, sum uint32) uint16 {
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xFFFF + sum>>16
	}
	return ^uint16(sum)
}

// emuPseudo is the sum of the IPv4 or IPv6 pseudo-header.
func emuPseudo(src, dst net.IP, proto byte, n int) uint32 {
	var sum uint32
	a, b := src.To4(), dst.To4()
	if a == nil || b == nil {
		a, b = src.To16(), dst.To16()
	}
	for _, x := range [][]byte{a, b} {
		for i := 0; i+1 < len(x); i += 2 {
			sum += uint32(x[i])<<8 | uint32(x[i+1])
		}
	}
	return sum + uint32(proto) + uint32(n)
}

// emuTrexVM is the field engine's program for a stream's changing fields (they change together, frame by frame).
func emuTrexVM(s emu.Stream, b []byte, o emuTrexOff) []map[string]any {
	ins := []map[string]any{}
	fixIP := false
	for i, m := range s.Mods {
		off, size := -1, 4
		switch m.Field {
		case "dstMac":
			off = 2
		case "srcMac":
			off = 8
		case "vlan":
			if s.VLAN > 0 {
				off, size = 14, 2
			}
		case "srcIp", "dstIp": // IPv6: the last 4 bytes of the address
			if o.ip {
				off = o.l3 + map[[2]bool]int{{false, true}: 12, {false, false}: 16, {true, true}: 20, {true, false}: 36}[[2]bool{o.v6, m.Field == "srcIp"}]
			}
		case "sport", "dport":
			if o.ip && (o.proto == 6 || o.proto == 17) {
				off, size = o.l4+map[bool]int{true: 0, false: 2}[m.Field == "sport"], 2
			}
		}
		if off < 0 || off+size > len(b) {
			continue
		}
		var base uint64
		for _, x := range b[off : off+size] {
			base = base<<8 | uint64(x)
		}
		top := uint64(1)<<(8*size) - 1
		step := uint64(max(1, m.Step))
		hi := min(top, base+uint64(max(1, m.Count)-1)*step)
		name := fmt.Sprintf("f%d", i)
		ins = append(ins, map[string]any{"type": "flow_var", "name": name, "size": size, "op": map[bool]string{true: "random", false: "inc"}[m.Mode == "random"],
			"init_value": base, "min_value": base, "max_value": hi, "step": step},
			map[string]any{"type": "write_flow_var", "name": name, "pkt_offset": off, "add_value": 0, "is_big_endian": true})
		fixIP = fixIP || m.Field != "dstMac" && m.Field != "srcMac" && m.Field != "vlan"
	}
	if fixIP && !o.v6 {
		ins = append(ins, map[string]any{"type": "fix_checksum_ipv4", "pkt_offset": o.l3})
	}
	if fixIP && (o.proto == 6 || o.v6 && o.proto == 17) {
		ins = append(ins, map[string]any{"type": "fix_checksum_hw", "l2_len": o.l3, "l3_len": o.ihl, "l4_type": map[byte]int{6: 13, 17: 11}[o.proto]})
	}
	return ins
}

// emuTrexStreams are the TRex streams of one of the lab's: one per frame size (IMIX: 64, 594 and 1518 bytes, 7:4:1),
// each counted by its own flow-statistics id, and a latency stream (at most 1000 frames/s) for IP frames.
func emuTrexStreams(s emu.Stream, f emu.Tester, to *emu.Tester, base int) ([]map[string]any, emuTrexStream, error) {
	ts := emuTrexStream{id: s.ID, from: s.From, to: s.To}
	sizes, shares := []int{max(64, s.Size)}, []float64{1}
	if s.SizeMode == "imix" {
		sizes, shares = []int{64, 594, 1518}, []float64{7.0 / 12, 4.0 / 12, 1.0 / 12}
	}
	secs := float64(s.Seconds)
	if s.Count > 0 {
		secs = float64(s.Count) / float64(max(1, s.PPS))
	}
	var out []map[string]any
	for i, sz := range sizes {
		b, o, err := emuTrexFrameOff(s, f, to, sz)
		if err != nil {
			return nil, ts, err
		}
		pps := float64(s.PPS) * shares[i]
		n := int(math.Round(pps * secs))
		if s.Count > 0 && len(sizes) == 1 {
			n = s.Count
		}
		if n <= 0 {
			continue
		}
		st := emuTrexStreamJSON(b, n, pps, emuTrexVM(s, b, o))
		if o.ip {
			st["flow_stats"] = map[string]any{"enabled": true, "stream_id": base + i, "rule_type": "stats"}
			ts.main = append(ts.main, base+i)
		}
		ts.total += n
		out = append(out, st)
		if i == 0 && o.ip { // its latency, measured by TRex on a slower copy
			lp := math.Min(1000, math.Max(1, float64(s.PPS)/10))
			l := emuTrexStreamJSON(b, max(1, int(math.Round(lp*secs))), lp, nil)
			l["flow_stats"] = map[string]any{"enabled": true, "stream_id": base + 3, "rule_type": "latency"}
			ts.lat = base + 3
			out = append(out, l)
		}
	}
	return out, ts, nil
}

func emuTrexStreamJSON(b []byte, n int, pps float64, vm []map[string]any) map[string]any {
	if vm == nil {
		vm = []map[string]any{}
	}
	return map[string]any{"enabled": true, "self_start": true, "isg": 0.0, "next_stream_id": -1, "action_count": 0, "random_seed": 0, "core_id": -1,
		"flags":  3, // the frame's own source and destination MACs
		"packet": map[string]any{"binary": base64.StdEncoding.EncodeToString(b), "meta": ""},
		"mode":   map[string]any{"type": "single_burst", "total_pkts": n, "rate": map[string]any{"type": "pps", "value": pps}},
		"vm":     map[string]any{"instructions": vm, "split_by_var": ""}, "flow_stats": map[string]any{"enabled": false}}
}

// emuTrexStart sends the run's streams between TRex ports (it stops the ones of an earlier run first); what went
// wrong is each stream's error.
func (a *App) emuTrexStart(run uint32, on map[string]bool) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return
	}
	emuEngInit()
	type job struct {
		srv     emu.TrexServer
		streams []emu.Stream
		testers map[string]emu.Tester
		idx     map[string]int
	}
	var jobs []*job
	for _, srv := range l.Trex {
		j := &job{srv: srv, testers: map[string]emu.Tester{}, idx: map[string]int{}}
		for _, t := range l.Testers {
			if id, _ := t.TrexAt(); id == srv.ID {
				j.testers[t.ID] = t
			}
		}
		for i, s := range l.Streams {
			if _, ok := j.testers[s.From]; ok && on[s.ID] {
				j.streams = append(j.streams, s)
				j.idx[s.ID] = i
			}
		}
		if len(j.streams) > 0 || emuTraf.trex[srv.ID] != nil {
			jobs = append(jobs, j)
		}
	}
	emuLab.mu.Unlock()
	for _, j := range jobs {
		r, err := a.emuTrexConn(j.srv, false)
		if err == nil && r.c == nil {
			err = errors.New(r.info.Err)
		}
		if err == nil {
			if _, err = r.c.Call("get_version", nil); err != nil { // a broken connection: once more
				r, err = a.emuTrexConn(j.srv, true)
			}
		}
		emuLab.mu.Lock()
		r = emuTraf.trex[j.srv.ID]
		r.run, r.streams, r.err, r.base = run, nil, "", map[int]trex.PortStats{}
		emuLab.mu.Unlock()
		if err != nil {
			emuLab.mu.Lock()
			r.err = "TRex " + j.srv.Name + ": " + err.Error()
			emuLab.mu.Unlock()
			continue
		}
		for p, h := range r.h { // the last run's
			r.c.Reset(p, h)
		}
		ports := map[int]bool{}
		for _, t := range j.testers {
			if _, p := t.TrexAt(); p >= 0 {
				ports[p] = true
			}
		}
		var errs []string
		for p := range ports {
			if _, ok := r.h[p]; ok {
				continue
			}
			h, err := r.c.Acquire(p, "termwright")
			if err != nil {
				errs = append(errs, fmt.Sprintf("port %d: %v", p, err))
				continue
			}
			r.h[p] = h
			r.c.Reset(p, h)
		}
		for p := range r.h {
			if ps, err := r.c.PortStats(p); err == nil {
				r.base[p] = ps
			}
		}
		var sts []emuTrexStream
		used := map[int]int{}
		for _, s := range j.streams {
			f, to := j.testers[s.From], j.testers[s.To]
			_, fp := f.TrexAt()
			_, tp := to.TrexAt()
			h, ok := r.h[fp]
			if !ok {
				continue
			}
			list, ts, err := emuTrexStreams(s, f, &to, 100+4*j.idx[s.ID])
			if err != nil {
				errs = append(errs, s.Name+": "+err.Error())
				continue
			}
			ts.port, ts.dport = fp, tp
			for _, st := range list {
				used[fp]++
				if err := r.c.AddStream(fp, h, used[fp], st); err != nil {
					errs = append(errs, s.Name+": "+err.Error())
					break
				}
			}
			sts = append(sts, ts)
		}
		for p := range used {
			if err := r.c.Start(p, r.h[p]); err != nil {
				errs = append(errs, fmt.Sprintf("port %d: %v", p, err))
			}
		}
		emuLab.mu.Lock()
		r.streams = sts
		if len(errs) > 0 {
			r.err = "TRex " + j.srv.Name + ": " + strings.Join(errs, "; ")
		}
		emuLab.mu.Unlock()
	}
}

// emuTrexStop stops the TRex ports' streams (their counters stay).
func (a *App) emuTrexStop() {
	emuLab.mu.Lock()
	emuEngInit()
	var rs []*emuTrexRun
	for _, r := range emuTraf.trex {
		if r.c != nil {
			rs = append(rs, r)
		}
	}
	emuLab.mu.Unlock()
	for _, r := range rs {
		for p, h := range r.h {
			_ = r.c.Stop(p, h)
		}
	}
}

// emuTrexStats is the run's counters on the TRex servers, as traffic.py writes its own ("" when there is none).
func (a *App) emuTrexStats(run uint32) string {
	emuLab.mu.Lock()
	emuEngInit()
	var rs []emuTrexRun
	for _, r := range emuTraf.trex {
		if r.run == run && r.c != nil {
			rs = append(rs, *r)
		}
	}
	emuLab.mu.Unlock()
	if len(rs) == 0 {
		return ""
	}
	tx, rx, ooo, dup := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	lat, jit, busy, errs := map[string][]float64{}, map[string][]float64{}, map[string]bool{}, map[string]string{}
	ports := map[string]emuPortCount{}
	for _, r := range rs {
		var ids []int
		for _, s := range r.streams {
			ids = append(ids, s.main...)
			if s.lat > 0 {
				ids = append(ids, s.lat)
			}
		}
		pg, err := map[int]trex.PGID{}, error(nil)
		if len(ids) > 0 {
			pg, err = r.c.PGIDStats(ids)
		}
		state := map[int]string{}
		for p := range r.h {
			state[p] = r.c.PortState(p)
		}
		for _, s := range r.streams {
			if r.err != "" {
				errs[s.id] = r.err
			}
			if err != nil {
				errs[s.id] = "TRex: " + err.Error()
			}
			for _, id := range s.main {
				tx[s.id] += int(pg[id].TX)
				rx[s.id] += int(pg[id].RX)
			}
			if len(s.main) == 0 { // not IP: the ports' counters
				b, x := r.base[s.port], r.base[s.dport]
				if ps, err := r.c.PortStats(s.port); err == nil {
					tx[s.id] = int(ps.OPackets - b.OPackets)
				}
				if ps, err := r.c.PortStats(s.dport); err == nil {
					rx[s.id] = int(ps.IPackets - x.IPackets)
				}
			}
			if l, ok := pg[s.lat]; ok && l.HasLatency > 0 && pg[s.lat].RX > 0 {
				n := pg[s.lat].RX
				lat[s.id] = []float64{l.LatAvg / 1000 * n, l.LatMax / 1000, n, l.LatMin / 1000}
				jit[s.id] = []float64{l.Jit / 1000 * n, n}
				ooo[s.id], dup[s.id] = int(l.OOO), int(l.Dup)
			}
			busy[s.id] = state[s.port] == "TX"
		}
		for p := range r.h { // the test ports' own counters, since the start
			ps, err := r.c.PortStats(p)
			if err != nil {
				continue
			}
			b := r.base[p]
			for _, s := range r.streams {
				for _, id := range []string{s.from, s.to} {
					if _, ok := ports[id]; ok {
						continue
					}
					if (id == s.from && s.port == p) || (id == s.to && s.dport == p) {
						ports[id] = emuPortCount{TX: int(ps.OPackets - b.OPackets), RX: int(ps.IPackets - b.IPackets), TXB: int(ps.OBytes - b.OBytes), RXB: int(ps.IBytes - b.IBytes), Err: int(ps.IErrors - b.IErrors), SRX: map[string]int{}}
					}
				}
			}
		}
		for _, s := range r.streams {
			if c, ok := ports[s.to]; ok {
				c.SRX[s.id] += rx[s.id]
				if x := lat[s.id]; len(x) == 4 {
					c.Lat = [3]float64{c.Lat[0] + x[0], c.Lat[1] + x[2], math.Max(c.Lat[2], x[1])}
				}
				ports[s.to] = c
			}
		}
	}
	b, _ := json.Marshal(map[string]any{"run": run, "tx": tx, "rx": rx, "lat": lat, "jit": jit, "ooo": ooo, "dup": dup, "busy": busy, "err": errs, "ports": ports})
	return string(b)
}
