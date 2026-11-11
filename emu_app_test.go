package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
