package main

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/session"
)

// TestEmuLab runs the emulated lab end to end against an SSH server with qemu
// (software emulation is fine): TW_EMU_SSH=host:port, TW_EMU_SSH_USER,
// TW_EMU_SSH_KEY (a private key file), and TW_EMU_KERNEL / TW_EMU_INITRD for
// a tiny Linux whose /init shows "fake login:" on the serial console.
func TestEmuLab(t *testing.T) {
	addr, keyFile := os.Getenv("TW_EMU_SSH"), os.Getenv("TW_EMU_SSH_KEY")
	kernel, initrd := os.Getenv("TW_EMU_KERNEL"), os.Getenv("TW_EMU_INITRD")
	if addr == "" || keyFile == "" || kernel == "" {
		t.Skip("TW_EMU_SSH / TW_EMU_SSH_KEY / TW_EMU_KERNEL not set")
	}
	kb, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	keyring.MockInit()
	signer, err := ssh.ParsePrivateKey(kb)
	if err != nil {
		t.Fatal(err)
	}
	old := emuDial
	emuDial = func(a *App, ctx context.Context, s session.Session) (*ssh.Client, func(), error) {
		c, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{User: os.Getenv("TW_EMU_SSH_USER"), Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second})
		if err != nil {
			return nil, nil, err
		}
		return c, func() { _ = c.Close() }, nil
	}
	defer func() { emuDial = old }()

	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = false, emuFile{}
	defer func() { a.emuShutdown(); emuLab.loaded, emuLab.f = false, emuFile{} }()
	host, err := st.Save(session.Session{Name: "kvm-1", Protocol: session.ProtoSSH2, Hostname: "127.0.0.1", Port: 2299, Username: "root"}, "")
	if err != nil {
		t.Fatal(err)
	}

	h, err := a.EmuCheckHost(host.Path)
	if err != nil || !h.Info.OK() || h.Info.QEMU == "" {
		t.Fatalf("host %+v %v", h, err)
	}
	img := filepath.Join(dir, "fake-nos-1.0.qcow2")
	if out, err := exec.Command("qemu-img", "create", "-q", "-f", "qcow2", img, "16M").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	v, err := a.EmuAddImage(img)
	if err != nil || len(v.Images) != 1 {
		t.Fatalf("image %+v %v", v.Images, err)
	}
	im := v.Images[0].Image
	im.RAM, im.NICs, im.Ready, im.PortName = 256, 4, "fake login:", "1/{n}"
	im.Answers = []emu.Answer{{Match: "[y/N]", Send: "n"}}
	im.Day0 = []string{"hostname {hostname}", "ip {mgmtIp}"}
	im.Extra = "-kernel " + kernel + " -initrd " + initrd + " -append 'console=ttyS0 quiet panic=-1'"
	if _, err := a.EmuSaveImage(im, false); err != nil {
		t.Fatal(err)
	}
	v, err = a.EmuAdd(EmuRequest{Host: host.Path, Groups: []EmuGroup{{Image: im.ID, Count: 2, Names: "sw-{n}"}}, Cabling: "chain", Mgmt: "forward", Start: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = a.EmuRemove(nil, true) }()
	if len(v.Devices) != 2 || len(v.Links) != 1 || v.Links[0].APort != "1/1" || len(v.Sessions) != 2 || len(v.Consoles) != 2 {
		t.Fatalf("view %+v", v)
	}
	s1, err := st.Get(v.Sessions[v.Devices[0].ID])
	if err != nil || s1.JumpSession != host.Path || s1.Hostname != "127.0.0.1" || s1.Port != v.Devices[0].Port {
		t.Fatalf("session %+v %v", s1, err)
	}
	if (s1.DeviceType != "" && !s1.OwnLogin) || s1.Username == "" {
		t.Fatalf("the session does not use the image's own login: %+v", s1)
	}
	if pw, ok := secrets.Lookup(s1.Path); !ok {
		t.Errorf("no password saved for the session (want the image's, even empty): %q", pw)
	}
	if c1, err := st.Get(v.Consoles[v.Devices[0].ID]); err != nil || c1.Username != s1.Username {
		t.Errorf("the console does not log in as the image's user: %+v %v", c1.Username, err)
	} else if _, ok := secrets.Lookup(c1.Path); !ok {
		t.Errorf("no password saved for the console")
	}
	// boot and day 0
	deadline := time.Now().Add(150 * time.Second)
	for time.Now().Before(deadline) {
		v, _ = a.EmuState()
		emuLab.mu.Lock()
		done := emuLab.f.Lab.VMs[0].Day0Done && emuLab.f.Lab.VMs[1].Day0Done
		emuLab.mu.Unlock()
		if done && v.Devices[0].State == "running" && v.Devices[1].State == "running" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if v.Devices[0].State != "running" || v.Devices[1].State != "running" {
		t.Fatalf("not running: %+v\n%s", v.Devices, a.EmuConsoleTail(v.Devices[0].ID))
	}
	var evs []string
	for _, e := range v.Events {
		evs = append(evs, e.Name+": "+e.Text)
	}
	if j := strings.Join(evs, "\n"); !strings.Contains(j, "sw-1: starting on") || !strings.Contains(j, "sw-2: booted") || !strings.Contains(j, "day-0 typed") {
		t.Fatalf("events:\n%s", j)
	}
	tail := a.EmuConsoleTail(v.Devices[1].ID)
	if !strings.Contains(tail, "ran: hostname sw-2") || !strings.Contains(tail, "answer=n") {
		t.Fatalf("console tail:\n%s", tail)
	}
	if _, err := a.EmuSetMgmt("bridge", ""); err == nil || !strings.Contains(err.Error(), "stop the switches") {
		t.Errorf("management changed under running switches: %v", err)
	}
	// a poll that was running before the stop does not hide it
	if _, err := a.EmuStop(nil); err != nil {
		t.Fatal(err)
	}
	if st := a.emuPollFresh(); len(st.Up) != 0 {
		t.Fatalf("still up after Stop: %v", st.Up)
	}
	if v, _ := a.EmuState(); !v.Devices[0].Saved || v.Devices[0].StateText != "stopped · state saved" {
		t.Fatalf("not saved: %+v", v.Devices[0])
	}
	if _, err := a.EmuStart(nil); err != nil {
		t.Fatal(err)
	}
	// Fast start: they resume from the saved state, no boot and no day 0
	for end := time.Now().Add(60 * time.Second); time.Now().Before(end); time.Sleep(time.Second) {
		if v, _ = a.EmuState(); !v.Devices[0].Running || v.Devices[0].State != "running" || v.Devices[1].State != "running" {
			continue
		}
		break
	}
	evs = nil
	for _, e := range v.Events {
		evs = append(evs, e.Name+": "+e.Text)
	}
	if j := strings.Join(evs, "\n"); strings.Count(j, "resumed from its saved state") != 2 || v.Devices[0].State != "running" {
		t.Fatalf("not resumed: %+v\n%s", v.Devices, j)
	}
	// a LAG marked in the topology, typed on the switch's console
	emuLab.mu.Lock()
	emuLab.f.Images[0].LagSet = map[string][]string{"lacp": {"lag add {ports} master {master}"}}
	emuLab.mu.Unlock()
	if _, err := a.EmuSetLag(v.Devices[0].ID, "LAG 1", "lacp", []string{v.Links[0].APort}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EmuApplyLag(v.Devices[0].ID, "LAG 1", false); err != nil {
		t.Fatal(err)
	}
	if tail := a.EmuConsoleTail(v.Devices[0].ID); !strings.Contains(tail, "ran: lag add "+v.Links[0].APort+" master "+v.Links[0].APort) {
		t.Fatalf("LAG not typed:\n%s", tail)
	}
	if v.Links[0].Up != true {
		t.Fatal("link not up")
	}
	if v, err = a.EmuSetLink(v.Devices[0].ID, "1/1", false); err != nil || v.Links[0].Up || !v.Links[0].Down {
		t.Fatalf("pull %+v %v", v.Links, err)
	}
	if v, err = a.EmuStop(nil); err != nil || v.Running {
		t.Fatalf("stop %+v %v", v.Devices, err)
	}
}

// TestEmuSetup runs the host set-up (packages, KVM module, kvm group) over SSH
// on the TW_EMU_SSH host - it installs QEMU there, so only on a test machine.
func TestEmuSetup(t *testing.T) {
	addr, keyFile := os.Getenv("TW_EMU_SSH"), os.Getenv("TW_EMU_SSH_KEY")
	if addr == "" || keyFile == "" || os.Getenv("TW_EMU_SETUP") == "" {
		t.Skip("TW_EMU_SSH / TW_EMU_SSH_KEY / TW_EMU_SETUP not set")
	}
	kb, _ := os.ReadFile(keyFile)
	signer, err := ssh.ParsePrivateKey(kb)
	if err != nil {
		t.Fatal(err)
	}
	old := emuDial
	emuDial = func(a *App, ctx context.Context, s session.Session) (*ssh.Client, func(), error) {
		c, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{User: os.Getenv("TW_EMU_SSH_USER"), Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.InsecureIgnoreHostKey()})
		if err != nil {
			return nil, nil, err
		}
		return c, func() { _ = c.Close() }, nil
	}
	defer func() { emuDial = old }()
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = false, emuFile{}
	defer func() { a.emuShutdown(); emuLab.loaded, emuLab.f = false, emuFile{} }()
	host, _ := st.Save(session.Session{Name: "kvm-1", Protocol: session.ProtoSSH2, Hostname: "127.0.0.1", Port: 2299, Username: os.Getenv("TW_EMU_SSH_USER")}, "")
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.mu.Unlock()
	var lines []string
	if err := a.emuSetup(host.Path, "", func(k, s string) { lines = append(lines, k+": "+s) }); err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(lines, "\n"))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "step: packages") || !strings.Contains(joined, "step: group") {
		t.Fatal(joined)
	}
	h, err := a.EmuCheckHost(host.Path)
	if err != nil || h.Info.QEMU == "" || h.Info.Pkg != "apt" {
		t.Fatalf("%+v %v", h.Info, err)
	}
}

// TestEmuPersona writes a persona file into a switch's disk before it boots
// (TW_EMU_GUESTFS: a qcow2 with /scratch; plus the TestEmuLab variables).
func TestEmuPersona(t *testing.T) {
	addr, keyFile, kernel, initrd, gf := os.Getenv("TW_EMU_SSH"), os.Getenv("TW_EMU_SSH_KEY"), os.Getenv("TW_EMU_KERNEL"), os.Getenv("TW_EMU_INITRD"), os.Getenv("TW_EMU_GUESTFS")
	if addr == "" || keyFile == "" || kernel == "" || gf == "" {
		t.Skip("TW_EMU_SSH / TW_EMU_SSH_KEY / TW_EMU_KERNEL / TW_EMU_GUESTFS not set")
	}
	kb, _ := os.ReadFile(keyFile)
	signer, _ := ssh.ParsePrivateKey(kb)
	old := emuDial
	emuDial = func(a *App, ctx context.Context, s session.Session) (*ssh.Client, func(), error) {
		c, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{User: os.Getenv("TW_EMU_SSH_USER"), Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.InsecureIgnoreHostKey()})
		if err != nil {
			return nil, nil, err
		}
		return c, func() { _ = c.Close() }, nil
	}
	defer func() { emuDial = old }()
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = false, emuFile{}
	defer func() { a.emuShutdown(); emuLab.loaded, emuLab.f = false, emuFile{} }()
	host, _ := st.Save(session.Session{Name: "kvm-1", Protocol: session.ProtoSSH2, Hostname: "127.0.0.1", Port: 2299, Username: "root"}, "")
	if _, err := a.EmuCheckHost(host.Path); err != nil {
		t.Fatal(err)
	}
	img := filepath.Join(dir, "dt.qcow2")
	b, _ := os.ReadFile(gf)
	_ = os.WriteFile(img, b, 0o644)
	v, err := a.EmuAddImage(img)
	if err != nil {
		t.Fatal(err)
	}
	im := v.Images[0].Image
	im.RAM, im.NICs, im.Ready = 256, 2, "fake login:"
	im.Extra = "-kernel " + kernel + " -initrd " + initrd + " -append 'console=ttyS0 quiet panic=-1'"
	im.Persona = &emu.PersonaDef{Name: "Persona", File: "/scratch/persona.yaml", Template: "model: {{(index .Slots 0).Model}} serial: {{(index .Slots 0).Serial}}\n",
		Models: []emu.PersonaModel{{Name: "M-24"}}}
	if _, err := a.EmuSaveImage(im, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EmuSaveTemplate(emu.Template{Name: "M-24", Image: im.ID, CPUs: 1, RAM: 256, Model: "M-24", SerialStart: "AB-0099"}); err != nil {
		t.Fatal(err)
	}
	tid := emuLab.f.Templates[0].ID
	v, err = a.EmuAdd(EmuRequest{Host: host.Path, Groups: []EmuGroup{{Template: tid, Count: 2, Names: "dt-{n}"}}, Mgmt: "forward", Start: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = a.EmuRemove(nil, true) }()
	if v.Devices[1].Serial != "AB-0100" {
		t.Fatalf("serials %s %s", v.Devices[0].Serial, v.Devices[1].Serial)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		v, _ = a.EmuState()
		if v.Devices[0].PersonaOK && v.Devices[1].PersonaOK && v.Devices[0].State == "running" {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if !v.Devices[0].PersonaOK || !v.Devices[1].PersonaOK {
		t.Fatalf("persona not written: %+v", v.Devices)
	}
	emuLab.mu.Lock()
	disk := emuLab.f.Lab.VMDir() + "/" + v.Devices[1].ID + ".qcow2"
	emuLab.mu.Unlock()
	out, _ := a.emuRun(host.Path, "LIBGUESTFS_BACKEND=direct guestfish --ro -a "+disk+" -m /dev/sda1 cat /scratch/persona.yaml", 3*time.Minute)
	if !strings.Contains(out, "model: M-24 serial: AB-0100") {
		t.Fatalf("persona file: %q", out)
	}
	// stopped and started again, an unchanged persona is not written (and the switch not booted twice)
	id := v.Devices[1].ID
	writes := func() (n int) {
		emuLab.mu.Lock()
		defer emuLab.mu.Unlock()
		for _, e := range emuLab.events {
			if e.VM == id && (strings.Contains(e.Text, "persona written") || strings.Contains(e.Text, "powering off")) {
				n++
			}
		}
		return n
	}
	before := writes()
	if _, err := a.EmuStop([]string{id}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EmuStart([]string{id}); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(2 * time.Minute); time.Now().Before(end); time.Sleep(2 * time.Second) {
		if v, _ = a.EmuState(); v.Devices[1].State == "running" {
			break
		}
	}
	if n := writes(); n != before {
		t.Fatalf("the persona was written again on a plain restart (%d -> %d)", before, n)
	}
}
func TestVersionIn(t *testing.T) {
	for in, want := range map[string]string{"NOSGNS3.9.4.0.0.qcow2": "9.4.0.0", "vm-33.4.1.15.x86_64.qcow2": "33.4.1.15", "EXOS-VM_v32.6.3.126.qcow2": "32.6.3.126", "VOSSGEN-9.1.0.0.qcow2": "9.1.0.0", "demo-vm-1.2.3.qcow2": "1.2.3", "noble.img": ""} {
		if got := versionIn(in); got != want {
			t.Errorf("%s: %q want %q", in, got, want)
		}
	}
}

// The public appliance list (TW_NET=1: fetches from the GNS3 registry).
func TestEmuCatalog(t *testing.T) {
	if os.Getenv("TW_NET") == "" {
		t.Skip("TW_NET not set")
	}
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = false, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	for _, c := range emuCatalog {
		v, err := a.EmuAddFromCatalog(c.File)
		if err != nil {
			t.Errorf("%s: %v", c.File, err)
			continue
		}
		n := 0
		for _, im := range v.Images {
			if im.Source == "GNS3 registry "+c.File {
				n++
				if im.FileName == "" || im.Version == "" || (im.URL == "" && im.Page == "") {
					t.Errorf("%s: %+v", c.File, im.Image)
				}
			}
		}
		if n == 0 {
			t.Errorf("%s: no images", c.File)
		}
	}
	if l := a.EmuCatalog(); !l[0].Added || l[0].Count != 2 {
		t.Errorf("not marked added: %+v", l[0])
	}
	v, err := a.EmuRemoveFromCatalog("frr", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, im := range v.Images {
		if im.Source == "GNS3 registry frr" {
			t.Fatal("frr still there")
		}
	}
	for _, im := range v.Images {
		if im.Source == "GNS3 registry sonic-vs" && (im.PortNameOf(2) != "Ethernet4" || im.Login != "admin" || im.URL == "") {
			t.Errorf("sonic: %+v", im.Image)
		}
	}
}

func TestEmuUnpack(t *testing.T) {
	cases := map[string]string{
		"https://x/ipfire.img.xz":                   "xz",
		"https://x/a.img.gz":                        "gz",
		"https://x/a?target=target/sonic-vs.img.gz": "gz",
		"https://x/a.qcow2.bz2":                     "bz2",
		"https://x/a.qcow2":                         "",
	}
	for u, want := range cases {
		if got := emuPacking(u, "a.img"); got != want {
			t.Errorf("%s: %q, want %q", u, got, want)
		}
	}
	if got := emuPacking("https://x/a.img.gz", "a.img.gz"); got != "" {
		t.Errorf("a wanted .gz file was unpacked: %q", got)
	}
	dir := t.TempDir()
	want := strings.Repeat("termwright disk ", 5000)
	src := filepath.Join(dir, "plain")
	if err := os.WriteFile(src, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	for kind, tool := range map[string]string{"xz": "xz", "gz": "gzip", "bz2": "bzip2"} {
		if _, err := exec.LookPath(tool); err != nil {
			continue
		}
		out, err := exec.Command(tool, "-c", src).Output()
		if err != nil {
			t.Fatal(err)
		}
		packed := filepath.Join(dir, "p."+kind)
		os.WriteFile(packed, out, 0o644)
		dst := filepath.Join(dir, "u."+kind)
		if err := unpackTo(kind, packed, dst); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if b, _ := os.ReadFile(dst); string(b) != want {
			t.Errorf("%s: unpacked %d bytes, want %d", kind, len(b), len(want))
		}
	}
	if err := unpackTo("xz", src, filepath.Join(dir, "bad")); err == nil {
		t.Error("a plain file passed as xz")
	}
}

func TestEmuCloud(t *testing.T) {
	info := &emu.HostInfo{LanAddr: "10.234.179.20/24", Gateway: "10.234.179.254", Uplink: "ens160", Sudo: true}
	l := &emu.Lab{VMs: []emu.VM{{ID: "a"}, {ID: "b"}}}
	if err := emuCloud(l, info, "10.234.179.146"); err != nil || l.MgmtNet != "10.234.179.146/24" || l.CloudGw != "10.234.179.254" || l.CloudIf != "ens160" || l.HostIP != "10.234.179.20" {
		t.Fatalf("%v %+v", err, l)
	}
	for spec, bad := range map[string]string{"": "first switch", "10.1.1.5": "not on the host's network", "10.234.179.19": "take 10.234.179.20", "10.234.179.254 10.234.179.1": "run past", "10.234.179.30 10.9.9.9": "gateway"} {
		if err := emuCloud(l, info, spec); err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("%q: %v (want %q)", spec, err, bad)
		}
	}
	if err := emuMgmtOK(info, "cloud"); err != nil {
		t.Error(err)
	}
}

// TestEmuModes boots a switch with a network (TW_EMU_BENCH: the netnos
// initrd; the host of netnos/bench.sh at 10.77.0.2:2299) in each management
// mode and checks that it gets its address and answers SSH, Telnet and HTTP
// from the host - and, on the host's network, from the network too.
func TestEmuModes(t *testing.T) {
	initrd, kernel, keyFile := os.Getenv("TW_EMU_BENCH"), os.Getenv("TW_EMU_KERNEL"), os.Getenv("TW_EMU_SSH_KEY")
	if initrd == "" || kernel == "" || keyFile == "" {
		t.Skip("TW_EMU_BENCH / TW_EMU_KERNEL / TW_EMU_SSH_KEY not set")
	}
	kb, _ := os.ReadFile(keyFile)
	signer, _ := ssh.ParsePrivateKey(kb)
	old := emuDial
	emuDial = func(a *App, ctx context.Context, s session.Session) (*ssh.Client, func(), error) {
		c, err := ssh.Dial("tcp", "10.77.0.2:2299", &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second})
		if err != nil {
			return nil, nil, err
		}
		return c, func() { _ = c.Close() }, nil
	}
	defer func() { emuDial = old }()
	modes := []string{"forward", "nat", "bridge", "cloud", "lan", "lan-fixed", "conflict"}
	if m := os.Getenv("TW_EMU_MODES"); m != "" {
		modes = strings.Split(m, ",")
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			if mode == "conflict" { // fixed addresses another device answers on
				keyring.MockInit()
				dir := t.TempDir()
				cfg, _ := config.Load(dir)
				st, _ := session.NewStore(dir + "/Sessions")
				a := &App{cfg: cfg, store: st}
				emuLab.loaded, emuLab.f = false, emuFile{}
				defer func() { a.emuShutdown(); emuLab.loaded, emuLab.f = false, emuFile{} }()
				host, _ := st.Save(session.Session{Name: "kvm-b", Protocol: session.ProtoSSH2, Hostname: "10.77.0.2", Port: 2299, Username: "root"}, "")
				if _, err := a.EmuCheckHost(host.Path); err != nil {
					t.Fatal(err)
				}
				img := filepath.Join(dir, "c.qcow2")
				_ = exec.Command("qemu-img", "create", "-q", "-f", "qcow2", img, "16M").Run()
				v, err := a.EmuAddImage(img)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := a.EmuAdd(EmuRequest{Host: host.Path, Groups: []EmuGroup{{Image: v.Images[0].ID, Count: 2, Names: "c-{n}"}}, Mgmt: "forward"}); err != nil {
					t.Fatal(err)
				}
				defer func() { _, _ = a.EmuRemove(nil, true) }()
				if _, err := a.EmuSetMgmt("lan", "10.50.0.141 10.50.0.254"); err == nil || !strings.Contains(err.Error(), "10.50.0.142") || strings.Contains(err.Error(), "10.50.0.141 ") {
					t.Fatalf("conflict not found: %v", err)
				}
				return
			}
			keyring.MockInit()
			dir := t.TempDir()
			cfg, _ := config.Load(dir)
			st, _ := session.NewStore(dir + "/Sessions")
			a := &App{cfg: cfg, store: st}
			emuLab.loaded, emuLab.f = false, emuFile{}
			emuLab.mu.Lock()
			emuLab.events = nil
			emuLab.mu.Unlock()
			defer func() { a.emuShutdown(); emuLab.loaded, emuLab.f = false, emuFile{} }()
			host, _ := st.Save(session.Session{Name: "kvm-b", Protocol: session.ProtoSSH2, Hostname: "10.77.0.2", Port: 2299, Username: "root"}, "")
			if _, err := a.EmuCheckHost(host.Path); err != nil {
				t.Fatal(err)
			}
			img := filepath.Join(dir, "netnos-1.0.qcow2")
			if out, err := exec.Command("qemu-img", "create", "-q", "-f", "qcow2", img, "16M").CombinedOutput(); err != nil {
				t.Fatal(string(out))
			}
			v, err := a.EmuAddImage(img)
			if err != nil {
				t.Fatal(err)
			}
			im := v.Images[0].Image
			im.RAM, im.NICs, im.NIC, im.Ready, im.Login = 256, 2, "e1000", "netnos login:", "admin"
			im.AddrCmd, im.AddrSet = "show ip", []string{"setip {mgmtAddr}/{mgmtBits} {gateway}"}
			im.Extra = "-kernel " + kernel + " -initrd " + initrd + " -append 'console=ttyS0 quiet panic=-1'"
			if _, err := a.EmuSaveImage(im, false); err != nil {
				t.Fatal(err)
			}
			net, mgmt := "", mode
			switch mode {
			case "cloud":
				net = "10.50.0.146 10.50.0.254"
			case "lan-fixed": // the host's network without DHCP: fixed addresses
				net, mgmt = "10.50.0.150 10.50.0.254", "lan"
			}
			if _, err := a.EmuAdd(EmuRequest{Host: host.Path, Groups: []EmuGroup{{Image: im.ID, Count: 1, Names: "sw-{n}"}}, Mgmt: mgmt, MgmtNet: net, Start: true}); err != nil {
				t.Fatal(err)
			}
			defer func() { _, _ = a.EmuRemove(nil, true) }()
			var reach, ip string
			for end := time.Now().Add(4 * time.Minute); time.Now().Before(end) && reach == "" && !(mode == "forward" && len(v.Devices) > 0 && v.Devices[0].State == "running"); time.Sleep(3 * time.Second) {
				v, _ = a.EmuState()
				for _, e := range v.Events {
					if strings.HasPrefix(e.Text, "from the host") || strings.Contains(e.Text, "through macvtap") {
						reach = e.Text
					}
				}
				if len(v.Devices) > 0 {
					ip = strings.Split(v.Devices[0].MgmtIP, "/")[0]
				}
			}
			var evs []string
			for _, e := range v.Events {
				evs = append(evs, e.Level+" "+e.Text)
			}
			if mode == "forward" {
				if v.Devices[0].State != "running" {
					t.Fatalf("not running:\n%s", strings.Join(evs, "\n"))
				}
				return // reached on 127.0.0.1 on the host: no address of its own
			}
			if (mode == "lan" || mode == "lan-fixed") && v.Mgmt == "lan" { // macvtap here: only the network reaches it
				for _, e := range v.Events {
					if strings.Contains(e.Text, "through macvtap") {
						reach = "ping answers SSH (22) open Telnet (23) open HTTP (80) open (from the network)"
					}
				}
			}
			if reach == "" || !strings.Contains(reach, "ping answers") || !strings.Contains(reach, "SSH (22) open") || !strings.Contains(reach, "Telnet (23) open") || !strings.Contains(reach, "HTTP (80) open") {
				diag, _ := a.emuRun(host.Path, "bridge link; ip -br addr; ip neigh; ip route", 20*time.Second)
				t.Fatalf("%s: address %q, reach %q\nevents:\n%s\nconsole: %s\nhost:\n%s", mode, ip, reach, strings.Join(evs, "\n"), a.EmuConsoleTail(v.Devices[0].ID), diag)
			}
			if mode == "cloud" { // this computer reaches it: the session goes straight
				for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(time.Second) {
					if ss, err := st.Get(v.Sessions[v.Devices[0].ID]); err == nil && ss.JumpSession == "" && ss.Hostname == ip {
						break
					}
				}
				if ss, err := st.Get(v.Sessions[v.Devices[0].ID]); err != nil || ss.JumpSession != "" || ss.Hostname != ip {
					t.Errorf("cloud: the session does not go straight: %+v %v", ss, err)
				}
			}
			if mode == "lan-fixed" && ip != "10.50.0.150" {
				t.Errorf("lan-fixed: address %q, want the fixed 10.50.0.150", ip)
			}
			if mode == "cloud" || mode == "lan" || mode == "lan-fixed" { // from the host's network too
				for _, port := range []string{"22", "23", "80"} {
					if out, err := exec.Command("ip", "netns", "exec", "twl", "timeout", "3", "bash", "-c", "</dev/tcp/"+ip+"/"+port).CombinedOutput(); err != nil {
						time.Sleep(3 * time.Second) // once more: the switch may still be starting a service
						if out, err = exec.Command("ip", "netns", "exec", "twl", "timeout", "3", "bash", "-c", "</dev/tcp/"+ip+"/"+port).CombinedOutput(); err != nil {
							t.Errorf("%s: %s:%s not reachable from the host's network: %s %v", mode, ip, port, out, err)
						}
					}
				}
			}
			t.Logf("%s: %s", mode, reach)
		})
	}
}

func TestEmuSetLag(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = true, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	t.Setenv("HOME", dir)
	emuLab.f.Images = []emu.Image{{ID: "i1", NICs: 9, PortName: "1/{n}"}}
	emuLab.f.Lab = &emu.Lab{ID: "a1", VMs: []emu.VM{{ID: "v1", Image: "i1"}, {ID: "v2", Image: "i1", Index: 1}},
		Cables: []emu.Cable{{A: "v1", APort: 1, B: "v2", BPort: 1}, {A: "v1", APort: 2, B: "v2", BPort: 2}, {A: "v1", APort: 3, B: "v2", BPort: 3}}}
	if _, err := a.EmuSetLag("v1", "", "lacp", []string{"1/1", "1/2"}); err != nil {
		t.Fatal(err)
	}
	a.emuKeepLags(emuLab.f.Lab.VMs[1], &emuLab.f.Images[0], []emu.SwitchLag{{Name: "1", Mode: "static", Ports: []string{"1/2", "1/3"}}})
	l := emuLab.f.Lab
	if g := l.LagOf("v1", 2); g == nil || g.Name != "LAG 1" || g.Mode != "lacp" {
		t.Fatalf("user lag %+v", g)
	}
	if g := l.LagOf("v2", 3); g == nil || g.Source != "switch" || l.LagOf("v2", 1) != nil {
		t.Fatalf("switch lag %+v", l.Lags)
	}
	if _, err := a.EmuSetLag("v1", "LAG 1", "", nil); err != nil || l.LagOf("v1", 1) != nil {
		t.Fatalf("removed? %v %+v", err, l.Lags)
	}
	if _, err := a.EmuSetLag("v1", "", "lacp", []string{"9/9"}); err == nil {
		t.Fatal("a port that is not there")
	}
}

func TestEmuTopologies(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = false, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	a.emuLoad()
	emuLab.f.Lab = &emu.Lab{ID: "a1", VMs: []emu.VM{{ID: "v1", Name: "core-1"}, {ID: "v2", Name: "core-2"}}}
	if _, err := a.EmuSaveTopology("EP1 scale"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EmuNewTopology("MLAG lab"); err != nil {
		t.Fatal(err)
	}
	if emuLab.f.Lab != nil || emuLab.f.Topology != "MLAG lab" || emuLab.f.Folder != "Emulated Lab/MLAG lab" {
		t.Fatalf("new: %+v", emuLab.f)
	}
	l := a.EmuTopologies()
	if len(l) != 2 || !slices.ContainsFunc(l, func(x EmuTopoInfo) bool { return x.Name == "EP1 scale" && x.Switches == 2 && !x.Open }) {
		t.Fatalf("list %+v", l)
	}
	if _, err := a.EmuSaveTopology("EP1 scale"); err == nil {
		t.Error("an empty lab saved over another")
	}
	if _, err := a.EmuOpenTopology("EP1 scale", false); err != nil {
		t.Fatal(err)
	}
	if emuLab.f.Lab == nil || len(emuLab.f.Lab.VMs) != 2 || emuLab.f.Topology != "EP1 scale" {
		t.Fatalf("open: %+v", emuLab.f.Lab)
	}
	if err := a.EmuDeleteTopology("EP1 scale"); err == nil {
		t.Error("the open topology deleted")
	}
	if err := a.EmuDeleteTopology("MLAG lab"); err != nil || len(a.EmuTopologies()) != 1 {
		t.Errorf("delete: %v %+v", err, a.EmuTopologies())
	}
	if _, err := emuTopoName("a/b"); err == nil {
		t.Error("a name with /")
	}
}

// TestEmuTwoHosts runs one switch on each of two hosts (the bench's twh and
// twh2) with a cable between them: a UDP tunnel between the hosts.
func TestEmuTwoHosts(t *testing.T) {
	initrd, kernel, keyFile := os.Getenv("TW_EMU_BENCH"), os.Getenv("TW_EMU_KERNEL"), os.Getenv("TW_EMU_SSH_KEY")
	if initrd == "" || kernel == "" || keyFile == "" {
		t.Skip("TW_EMU_BENCH / TW_EMU_KERNEL / TW_EMU_SSH_KEY not set")
	}
	kb, _ := os.ReadFile(keyFile)
	signer, _ := ssh.ParsePrivateKey(kb)
	old := emuDial
	emuDial = func(a *App, ctx context.Context, s session.Session) (*ssh.Client, func(), error) {
		user := "root"
		if u := os.Getenv("TW_EMU_HOSTB_USER"); u != "" && s.Hostname == "10.77.0.6" {
			user = u // a home of its own, as a second machine has (needed to move a switch between them)
		}
		c, err := ssh.Dial("tcp", s.Hostname+":2299", &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second})
		if err != nil {
			return nil, nil, err
		}
		return c, func() { _ = c.Close() }, nil
	}
	defer func() { emuDial = old }()
	keyring.MockInit()
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = false, emuFile{}
	emuLab.mu.Lock()
	emuLab.events = nil
	emuLab.mu.Unlock()
	defer func() { a.emuShutdown(); emuLab.loaded, emuLab.f = false, emuFile{} }()
	ha, _ := st.Save(session.Session{Name: "kvm-a", Protocol: session.ProtoSSH2, Hostname: "10.77.0.2", Port: 2299, Username: "root"}, "")
	hb, _ := st.Save(session.Session{Name: "kvm-b", Protocol: session.ProtoSSH2, Hostname: "10.77.0.6", Port: 2299, Username: "root"}, "")
	for _, h := range []string{ha.Path, hb.Path} {
		if _, err := a.EmuCheckHost(h); err != nil {
			t.Fatal(err)
		}
	}
	img := filepath.Join(dir, "netnos-1.0.qcow2")
	_ = exec.Command("qemu-img", "create", "-q", "-f", "qcow2", img, "16M").Run()
	v, err := a.EmuAddImage(img)
	if err != nil {
		t.Fatal(err)
	}
	im := v.Images[0].Image
	im.RAM, im.NICs, im.NIC, im.Ready, im.Login = 256, 3, "e1000", "netnos login:", "admin"
	im.Day0 = []string{"x ip link set eth1 up", "x ip addr add 10.9.9.{n}/24 dev eth1"}
	im.Extra = "-kernel " + kernel + " -initrd " + initrd + " -append 'console=ttyS0 quiet panic=-1'"
	if _, err := a.EmuSaveImage(im, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EmuAdd(EmuRequest{Host: ha.Path, Groups: []EmuGroup{{Image: im.ID, Count: 1, Names: "a-{n}"}}, Mgmt: "forward"}); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = a.EmuRemove(nil, true) }()
	if v, err = a.EmuAdd(EmuRequest{Host: hb.Path, Groups: []EmuGroup{{Image: im.ID, Count: 1, Names: "b-{n}"}}, Cabling: "chain", Mgmt: "forward", Start: true}); err != nil {
		t.Fatal(err)
	}
	if len(v.Devices) != 2 || v.Devices[1].Host != hb.Path || len(v.Links) != 1 || len(v.HostUse) != 2 || v.HostUse[1].Mgmt != "forward" {
		t.Fatalf("view: devices %+v links %+v hosts %+v", v.Devices, v.Links, v.HostUse)
	}
	if _, err := a.EmuStart(nil); err != nil {
		t.Fatal(err)
	}
	// both boot and type day 0 (each on its host)
	for end := time.Now().Add(4 * time.Minute); time.Now().Before(end); time.Sleep(3 * time.Second) {
		emuLab.mu.Lock()
		done := emuLab.f.Lab.VMs[0].Day0Done && emuLab.f.Lab.VMs[1].Day0Done
		emuLab.mu.Unlock()
		if done {
			break
		}
	}
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	va := *l.VMByID(v.Devices[0].ID)
	conPort := l.ConsolePort(va)
	emuLab.mu.Unlock()
	if !va.Day0Done {
		v, _ = a.EmuState()
		t.Fatalf("day 0 not typed: %+v", v.Events)
	}
	// a-1 reaches b-1 over the cable between the hosts
	cl, err := a.emuClient(context.Background(), ha.Path)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(conPort)))
	if err != nil {
		t.Fatal(err)
	}
	c := emu.NewConsole(conn)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := emu.Ask(ctx, c, &im, "x ping -c 3 -W 2 10.9.9.2")
	if err != nil || !strings.Contains(out, " 0% packet loss") && !strings.Contains(out, "3 packets received") {
		t.Fatalf("no traffic over the tunnel: %v\n%s", err, out)
	}
	if v, _ = a.EmuState(); v.HostUse[0].Running+v.HostUse[1].Running != 2 {
		t.Errorf("host use %+v", v.HostUse)
	}
	// the hosts reach each other on UDP, both ways
	hl, err := a.EmuCheckTunnels()
	if err != nil || len(hl) != 2 || !hl[0].OK || !hl[1].OK {
		t.Errorf("tunnel check: %v %+v", err, hl)
	}
	// the Images on the Hosts page: both hosts have the image; they reach each other on UDP and TCP
	mx, err := a.EmuImageMatrix()
	if err != nil || len(mx.Hosts) != 2 || len(mx.Images) == 0 {
		t.Fatalf("matrix: %v %+v", err, mx)
	}
	for _, h := range mx.Hosts {
		if h.Error != "" || h.CPUs == 0 || h.MemTotal == 0 || h.DiskTotal == 0 || h.ImagesDir == "" {
			t.Errorf("host in the matrix: %+v", h)
		}
		if st := mx.Cells[h.Session][mx.Images[0].ID]; st != "ok" {
			t.Errorf("%s: %s", h.Name, st)
		}
	}
	if _, err := a.EmuCheckReach(); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(40 * time.Second); time.Now().Before(end); time.Sleep(time.Second) {
		if mx, _ = a.EmuImageMatrix(); !mx.Checking {
			break
		}
	}
	if len(mx.Reach) != 2 || mx.Reach[0].UDP != "ok" || mx.Reach[0].TCP != "ok" || mx.Reach[1].UDP != "ok" {
		t.Errorf("reach: %+v", mx.Reach)
	}
	if _, err := a.EmuSyncImages(nil, nil, false); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("sync with all there: %v", err)
	}
	if os.Getenv("TW_EMU_HOSTB_USER") == "" {
		return // moving a switch needs the hosts' own homes (as two machines have)
	}
	// b-1 moves live to kvm-a and keeps running; the cable to a-1 keeps working (a relay on kvm-b)
	vb := v.Devices[1].ID
	moved := func(what string) {
		for end := time.Now().Add(6 * time.Minute); time.Now().Before(end); time.Sleep(2 * time.Second) {
			emuLab.mu.Lock()
			busy := emuLab.state[vb]
			emuLab.mu.Unlock()
			if busy == "" {
				break
			}
		}
		v, _ = a.EmuState()
		var evs []string
		for _, e := range v.Events {
			evs = append(evs, e.Name+": "+e.Text)
		}
		if j := strings.Join(evs, "\n"); !strings.Contains(j, what) {
			t.Fatalf("not moved (%s):\n%s", what, j)
		}
		out, err := emu.Ask(ctx, c, &im, "x ping -c 3 -W 2 10.9.9.2")
		if err != nil || !strings.Contains(out, " 0% packet loss") && !strings.Contains(out, "3 packets received") {
			t.Fatalf("no traffic after the move (%s): %v\n%s", what, err, out)
		}
	}
	if _, err := a.EmuMigrate(vb, ha.Path, true); err != nil {
		t.Fatal(err)
	}
	moved("moved live to kvm-a")
	if v.Devices[1].Host != ha.Path || !v.Devices[1].Running {
		t.Fatalf("after the live move: %+v", v.Devices[1])
	}
	// and back to kvm-b by saving, copying and resuming it
	if _, err := a.EmuMigrate(vb, hb.Path, false); err != nil {
		t.Fatal(err)
	}
	moved("it resumed there")
	// the AI connector's console tool: logs in and types on a switch's console
	if out, err := a.emuConsoleRun(vb, []string{"x echo tw-console-ok"}, time.Minute); err != nil || !strings.Contains(out, "tw-console-ok") {
		t.Errorf("console run: %v\n%s", err, out)
	}
	if out, err := a.emuMCP("lab_events", map[string]any{"last": float64(5)}); err != nil || out == "" {
		t.Errorf("lab_events: %v %q", err, out)
	}
	if out, err := a.emuMCP("lab_images", map[string]any{}); err != nil || !strings.Contains(out, "Images (id") {
		t.Errorf("lab_images: %v %q", err, out)
	}
	// Remove Host: what it can take back or wipe; refused while switches of the topology run there
	ri, err := a.EmuRemoveHostInfo(hb.Path)
	if err != nil || ri.Error != "" || ri.ImagesBytes == 0 || ri.LabDirs == 0 || ri.Switches == 0 {
		t.Errorf("remove info: %v %+v", err, ri)
	}
	if _, err := a.EmuRemoveHostWith(EmuRemoveHostReq{Host: hb.Path, Images: true}); err == nil || !strings.Contains(err.Error(), "move or remove them first") {
		t.Errorf("remove with switches there: %v", err)
	}
}

// A newer pack gives existing images its factory password and its later ready text.
func TestEmuTopUp(t *testing.T) {
	imgs := []emu.Image{{ID: "a", Name: "VOSS", Ready: "Login:"}, {ID: "b", Name: "EXOS", Ready: "login:"}}
	prof := []emu.Image{{Name: "VOSS", Ready: "Boot sequence successful", DefaultPassword: "x"}, {Name: "EXOS", Ready: "login:"}}
	up, pw := emuTopUp(imgs, prof)
	if !up || len(pw) != 1 || pw[0] != "a" || imgs[0].DefaultPassword != "x" || imgs[0].Ready != "Boot sequence successful" || imgs[1].Ready != "login:" {
		t.Fatal(up, pw, imgs)
	}
	if up, _ := emuTopUp(imgs, prof); up {
		t.Fatal("topped up twice")
	}
}

// The AI agent sees the lab: switches with their sessions, cables with LAGs and tunnels, the hosts.
func TestEmuAgentText(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = true, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	t.Setenv("HOME", dir)
	emuLab.f.Host = "Lab/kvm-1"
	emuLab.f.Images = []emu.Image{{ID: "i1", Name: "EXOS-VM", DeviceType: "EXOS", Version: "33.6", NICs: 9, PortName: "1/{n}"}}
	emuLab.f.Sessions = map[string]string{"v1": "Emulated Lab/core-1"}
	emuLab.f.Consoles = map[string]string{"v1": "Emulated Lab/Consoles/core-1 console"}
	emuLab.f.Lab = &emu.Lab{ID: "a1", Mgmt: "cloud", MgmtNet: "10.1.1.10/24", VMs: []emu.VM{{ID: "v1", Name: "core-1", Image: "i1", MgmtIP: "10.1.1.10/24"}, {ID: "v2", Name: "acc-1", Image: "i1", Index: 1, Host: "Lab/kvm-2"}},
		Cables: []emu.Cable{{A: "v1", APort: 1, B: "v2", BPort: 1}},
		Lags:   []emu.Lag{{VM: "v1", Name: "1", Mode: "lacp", Ports: []int{1}, Source: "switch"}}}
	got := a.emuAgentText()
	t.Log(got)
	for _, want := range []string{"- core-1 | ", `"Emulated Lab/core-1"`, "acc-1", "kvm-2", "core-1 1/1 <-> acc-1 1/1", "LAG 1 (lacp) on core-1", "UDP tunnel", "KVM hosts:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("no %q in:\n%s", want, got)
		}
	}
}

func TestEmuNeighbors(t *testing.T) {
	out := "LLDP Port  Neighbor Chassis ID  Neighbor Port ID  TTL  Age  Neighbor System Name\n=====\n1   00:04:96:9C:E5:00   1   120   7   core-2\n2   00:04:96:9C:E5:00   5   120   7   acc-9\n3   00:04:96:11:22:00   7   120   7   5520-24T\n"
	n := emuNeighbors(out, map[string]bool{"1": true, "2": true, "3": true, "4": true})
	names := []string{"core-1", "core-2", "acc-9"}
	for _, c := range []struct{ port, peer, pport, want string }{
		{"1", "core-2", "1", "ok: LLDP sees core-2 1"},
		{"2", "core-2", "2", "wrong: the port sees acc-9 instead of core-2"},
		{"3", "core-2", "7", "ok: LLDP sees port 7 (the switches do not send their lab names)"},
		{"4", "core-2", "4", "none: no LLDP neighbour on that port (link down, or LLDP off)"},
	} {
		if g := emuJudge(n[c.port], c.peer, c.pport, names); g != c.want {
			t.Errorf("port %s: %q", c.port, g)
		}
	}
}

// Opening a topology where a host is gone: its switches go to another host, with that host's management.
func TestEmuRemap(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = true, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	t.Setenv("HOME", dir)
	emuLab.f.Host = "Lab/kvm-1"
	emuLab.f.Hosts = []EmuHost{{Session: "Lab/kvm-1", Info: emu.HostInfo{LanAddr: "10.0.0.1/24", Sudo: true, Nat: "192.168.122.1/24"}}, {Session: "Lab/kvm-3", Info: emu.HostInfo{LanAddr: "10.0.0.3/24", Sudo: true, Nat: "192.168.122.1/24"}}}
	emuLab.f.Lab = &emu.Lab{ID: "a1", Mgmt: "cloud", MgmtNet: "10.0.0.50/24", VMs: []emu.VM{{ID: "v1", Name: "core-1", Day0Done: true}, {ID: "v2", Name: "acc-1", Index: 1, Host: "Lab/kvm-2", Day0Done: true, FastKey: "k"}},
		Nets: map[string]emu.HostNet{"Lab/kvm-2": {Mgmt: "bridge"}}}
	moved, err := a.emuRemap(map[string]string{"Lab/kvm-2": "Lab/kvm-3"})
	l := emuLab.f.Lab
	if err != nil || len(moved) != 1 || l.VMs[1].Host != "Lab/kvm-3" || l.VMs[1].Day0Done || l.VMs[1].FastKey != "" || !l.VMs[0].Day0Done || l.Mgmt != "cloud" {
		t.Fatalf("%v %+v", err, l.VMs)
	}
	if _, ok := l.Nets["Lab/kvm-2"]; ok || l.Nets["Lab/kvm-3"].Mgmt != "nat" {
		t.Fatalf("nets %+v", l.Nets)
	}
	if _, err := a.emuRemap(map[string]string{"Lab/kvm-1": "Lab/kvm-9"}); err == nil {
		t.Fatal("a host that is not there")
	}
}

func TestEmuTopologyCopies(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = false, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	a.emuLoad()
	emuLab.f.Lab = &emu.Lab{ID: "a1", VMs: []emu.VM{{ID: "v1", Name: "core-1", Day0Done: true}}}
	emuLab.f.Sessions = map[string]string{"v1": "Emulated Lab/core-1"}
	if _, err := a.EmuSaveTopology("EP1"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EmuDuplicateTopology("EP1", "EP1 copy"); err != nil {
		t.Fatal(err)
	}
	c, err := a.emuReadTopo("EP1 copy")
	if err != nil || c.Lab.ID == "a1" || c.Lab.VMs[0].Day0Done || c.Sessions != nil || c.Folder != "Emulated Lab/EP1 copy" {
		t.Fatalf("copy %+v %v", c, err)
	}
	if _, err := a.EmuRenameTopology("EP1", "EP1 main"); err != nil || emuLab.f.Topology != "EP1 main" {
		t.Fatalf("rename %v %q", err, emuLab.f.Topology)
	}
	f := filepath.Join(dir, "x.twlab")
	if err := a.EmuExportTopology("EP1 main", f); err != nil {
		t.Fatal(err)
	}
	l, err := a.EmuImportTopology(f)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range l {
		names = append(names, x.Name)
	}
	if !slices.Contains(names, "EP1 main (2)") || !slices.Contains(names, "EP1 copy") || slices.Contains(names, "EP1") {
		t.Fatal(names)
	}
}

// Draw As goes for every switch of the same kind in the topology.
func TestEmuIcons(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = true, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	t.Setenv("HOME", dir)
	emuLab.f.Images = []emu.Image{{ID: "i1", Name: "EXOS-VM"}}
	five := &emu.Persona{Slots: []emu.Slot{{Model: "5720-48MW"}}}
	emuLab.f.Lab = &emu.Lab{ID: "a1", VMs: []emu.VM{{ID: "v1", Image: "i1", Persona: five}, {ID: "v2", Image: "i1", Index: 1, Persona: five}, {ID: "v3", Image: "i1", Index: 2}}}
	if _, err := a.EmuSetRole("v1", "core"); err != nil {
		t.Fatal(err)
	}
	v, err := a.EmuSetIconStyle("v2", "line")
	if err != nil {
		t.Fatal(err)
	}
	if v.Devices[1].Role != "core" || v.Devices[1].IconStyle != "line" || v.Devices[2].Role != "" || v.IconStyle != "box" {
		t.Fatalf("%+v", v.Devices)
	}
	if v, _ = a.EmuSetIconStyle("", "classic"); v.IconStyle != "classic" {
		t.Fatal(v.IconStyle)
	}
	if _, err := a.EmuSetIconStyle("v1", "nope"); err == nil {
		t.Fatal("unknown style")
	}
}

// The Events log is kept on disk: it is back after Termwright starts again, and Clear empties it there too.
func TestEmuEventsKept(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	defer func() { emuLab.loaded, emuLab.f, emuLab.events, emuEventsFile = false, emuFile{}, nil, "" }()
	emuLab.loaded, emuLab.f, emuLab.events = false, emuFile{}, nil
	a.emuLoad()
	emuEv("", "error", "persona not written: long reason")
	emuLab.loaded, emuLab.events = false, nil // Termwright starts again
	a.emuLoad()
	if len(emuLab.events) != 1 || emuLab.events[0].Text != "persona not written: long reason" {
		t.Fatalf("%+v", emuLab.events)
	}
	if _, err := a.EmuClearEvents(); err != nil {
		t.Fatal(err)
	}
	emuLab.loaded, emuLab.events = false, nil
	a.emuLoad()
	if len(emuLab.events) != 0 {
		t.Fatalf("cleared: %+v", emuLab.events)
	}
}

func TestEmuCopyWhy(t *testing.T) {
	for _, c := range []struct {
		err   string
		why   string
		retry bool
	}{
		{"sftp: write: no space left on device", "the host's disk is full", false},
		{"connection lost", "the connection to the host dropped", true},
		{"unexpected EOF", "the connection to the host dropped", true},
		{"open x: permission denied", "the host user may not write to the lab folder", false},
	} {
		e := errors.New(c.err)
		if g := emuCopyWhy(e); g != c.why {
			t.Errorf("%q: %q", c.err, g)
		}
		if emuCopyRetry(e) != c.retry {
			t.Errorf("%q: retry %v", c.err, !c.retry)
		}
	}
	if emuBytes(3<<30) != "3.0 GB" || emuBytes(5<<20) != "5 MB" {
		t.Error(emuBytes(3<<30), emuBytes(5<<20))
	}
}

// A switch with an address on one host's network moves to a host on another network: it gets an address there.
func TestEmuMoveTargetNetwork(t *testing.T) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	saved := emuLab.f
	defer func() { emuLab.f = saved }()
	emuLab.f.Host = "B/teams"
	emuLab.f.Lab = &emu.Lab{ID: "x", Dir: "termwright-lab", Mgmt: "cloud", MgmtNet: "10.234.179.146/24", HostIP: "10.234.179.144/24", VMs: []emu.VM{{ID: "v1", Name: "s1", MgmtIP: "10.234.179.149/24"}}}
	emuLab.f.Hosts = []EmuHost{{Session: "B/other", Info: emu.HostInfo{LanAddr: "10.127.25.68/24", Sudo: true}}, {Session: "B/same", Info: emu.HostInfo{LanAddr: "10.234.179.20/24", Sudo: true}}}
	a := &App{}
	v := emuLab.f.Lab.VMs[0]
	if r, err := a.emuMoveTarget(v, "B/same"); err != nil || r {
		t.Errorf("same network: %v %v", r, err)
	}
	if r, err := a.emuMoveTarget(v, "B/other"); err != nil || !r {
		t.Errorf("other network: %v %v", r, err)
	}
	if n := emuLab.f.Lab.Net("B/other", "B/teams"); n.Mgmt != "bridge" {
		t.Errorf("the new host's management: %+v", n)
	}
	emuLab.f.Lab.VMs = append(emuLab.f.Lab.VMs, emu.VM{ID: "v2", Host: "B/other"})
	emuLab.f.Lab.SetNet("B/other", "B/teams", emu.HostNet{Mgmt: "forward"})
	if _, err := a.emuMoveTarget(v, "B/other"); err == nil || !strings.Contains(err.Error(), "port forwarding") {
		t.Errorf("fixed to port forwarding: %v", err)
	}
}

func TestEmuRate(t *testing.T) {
	if g := emuRate(1<<30, 4<<30, 0, time.Now().Add(-10*time.Second)); g != "25 % · 1.0 GB of 4.0 GB · 102 MB/s · about 31 s left" {
		t.Error(g)
	}
	if g := emuRate(5<<20, 0, 0, time.Now()); g != "5 MB" {
		t.Error(g)
	}
}

// KVM turned on at a host counts at once, with a line in the Events.
func TestEmuSetKVM(t *testing.T) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	saved, ev := emuLab.f, emuLab.events
	defer func() { emuLab.f, emuLab.events = saved, ev }()
	emuLab.f.Hosts = []EmuHost{{Session: "B/u", Info: emu.HostInfo{KVM: "none", Problems: []string{"No /dev/kvm: software emulation"}}}}
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{cfg: cfg}
	a.emuSetKVM("B/u", true)
	if h := emuLab.f.Hosts[0].Info; h.KVM != "yes" || len(h.Problems) != 0 {
		t.Errorf("%+v", h)
	}
	if e := emuLab.events[len(emuLab.events)-1]; !strings.Contains(e.Text, "KVM is on now on u") || e.Host != "B/u" {
		t.Errorf("%+v", e)
	}
}

// The emulator log: lines at the level chosen, nothing below it.
func TestEmuLog(t *testing.T) {
	d := t.TempDir()
	emuLog.Lock()
	op, ol := emuLog.path, emuLog.level
	emuLog.path, emuLog.level = d+"/emulator.log", emuLvInfo
	emuLog.Unlock()
	defer func() { emuLog.Lock(); emuLog.path, emuLog.level = op, ol; emuLog.Unlock() }()
	emuLogf(emuLvInfo, "B/teams-vm", "sw1", "booted")
	emuLogf(emuLvDebug, "B/teams-vm", "", "ran something")
	b, _ := os.ReadFile(d + "/emulator.log")
	if !strings.Contains(string(b), "INFO    teams-vm") || !strings.Contains(string(b), "booted") || strings.Contains(string(b), "ran something") {
		t.Errorf("%s", b)
	}
}
