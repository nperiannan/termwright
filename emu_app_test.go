package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/emu"
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
}
func TestVersionIn(t *testing.T) {
	for in, want := range map[string]string{"NOSGNS3.9.4.0.0.qcow2": "9.4.0.0", "vm-33.4.1.15.x86_64.qcow2": "33.4.1.15", "EXOS-VM_v32.6.3.126.qcow2": "32.6.3.126", "VOSSGEN-9.1.0.0.qcow2": "9.1.0.0", "demo-vm-1.2.3.qcow2": "1.2.3", "noble.img": ""} {
		if got := versionIn(in); got != want {
			t.Errorf("%s: %q want %q", in, got, want)
		}
	}
}
