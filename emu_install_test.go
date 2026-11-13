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

func TestEmuCloudCatalogParses(t *testing.T) {
	n := 0
	for _, it := range emuCatalog {
		if !it.seed {
			continue
		}
		n++
		ap, err := emu.ParseAppliance([]byte(it.json))
		if err != nil || len(ap.Profiles) == 0 {
			t.Fatalf("%s: %v %d", it.File, err, len(ap.Profiles))
		}
		for _, p := range ap.Profiles {
			if !strings.HasPrefix(p.URL, "https://") || p.FileName == "" || p.Disk != "virtio" || p.NIC != "virtio-net-pci" || p.Login != "root" || p.Prompt == "" {
				t.Errorf("%s %s: %+v", it.File, p.Name, p)
			}
		}
		if ap.Password != emuSeedPassword {
			t.Errorf("%s: password %q", it.File, ap.Password)
		}
	}
	if n < 4 {
		t.Fatalf("only %d cloud images in the catalog", n)
	}
}

func TestEmuSeedDisk(t *testing.T) {
	keyring.MockInit()
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	d, err := a.emuSeedDisk(emu.Seed{User: "root", Password: "pw1"})
	if err != nil || d.Slot != "cdrom" || d.Size <= 0 || d.SHA256 == "" {
		t.Fatalf("%+v %v", d, err)
	}
	if b, err := os.ReadFile(d.File); err != nil || int64(len(b)) != d.Size || string(b[16*2048+1:16*2048+6]) != "CD001" {
		t.Fatalf("the file: %v", err)
	}
	d2, _ := a.emuSeedDisk(emu.Seed{User: "root", Password: "pw2"})
	if d2.FileName == d.FileName {
		t.Fatal("another password must give another file")
	}
}

func TestEmuInstallStartChecks(t *testing.T) {
	keyring.MockInit()
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	emuLab.loaded, emuLab.f = false, emuFile{}
	defer func() { emuLab.loaded, emuLab.f = false, emuFile{} }()
	iso := filepath.Join(dir, "alpine-virt-3.22.1-x86_64.iso")
	_ = os.WriteFile(iso, []byte("x"), 0o600)
	for _, c := range []struct {
		r    EmuInstallRequest
		want string
	}{
		{EmuInstallRequest{}, "choose the ISO"},
		{EmuInstallRequest{ISO: filepath.Join(dir, "a.img")}, "not an .iso"},
		{EmuInstallRequest{ISO: iso, Mode: "alpine"}, "root password"},
		{EmuInstallRequest{ISO: iso, Mode: "alpine", Password: "a'b"}, "must not contain"},
		{EmuInstallRequest{ISO: iso, Mode: "manual", GB: 1000}, "at most 512"},
		{EmuInstallRequest{ISO: iso, Mode: "manual", MB: 50}, "at least 100 MB"},
		{EmuInstallRequest{ISO: iso, Mode: "manual", MB: 600000}, "at most 512"},
		{EmuInstallRequest{ISO: iso, Mode: "manual", MB: -5}, "must not be negative"},
		{EmuInstallRequest{ISO: filepath.Join(dir, "none.iso"), Mode: "manual"}, "no such file"},
		{EmuInstallRequest{ISO: iso, Mode: "manual"}, "no KVM host"},
	} {
		if _, err := a.EmuInstallStart(c.r); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: error %v, want %q", c.r, err, c.want)
		}
	}
}

func TestEmuInstallsLoaded(t *testing.T) {
	emuLab.f.Installs = []EmuInstall{{ID: "a", Stage: instInstalling}, {ID: "b", Stage: instWaiting}, {ID: "c", Stage: instDone}}
	defer func() { emuLab.f.Installs = nil }()
	emuInstallsLoaded()
	if emuLab.f.Installs[0].Stage != instFailed || emuLab.f.Installs[0].Err == "" || emuLab.f.Installs[1].Stage != instWaiting || emuLab.f.Installs[2].Stage != instDone {
		t.Fatalf("%+v", emuLab.f.Installs)
	}
}

// TestEmuInstallManual runs a manual install end to end against an SSH server with qemu (software
// emulation is fine; the "ISO" does not boot, which is enough for the plumbing): TW_EMU_SSH=host:port,
// TW_EMU_SSH_USER, TW_EMU_SSH_KEY. It copies the ISO, starts the installer, waits for the user, then
// saves the disk as an image that appears here with its file and checksum.
func TestEmuInstallManual(t *testing.T) {
	addr, keyFile := os.Getenv("TW_EMU_SSH"), os.Getenv("TW_EMU_SSH_KEY")
	if addr == "" || keyFile == "" {
		t.Skip("TW_EMU_SSH / TW_EMU_SSH_KEY not set")
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
	oldWait := emuInstallPowerWait
	emuInstallPowerWait = 4 * time.Second
	defer func() { emuInstallPowerWait = oldWait }()

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
	if _, err := a.EmuCheckHost(host.Path); err != nil {
		t.Fatal(err)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.f.Host = host.Path
	emuLab.mu.Unlock()
	iso := filepath.Join(dir, "my distro 1.0.iso")
	if out, err := exec.Command("sh", "-c", "head -c 3000000 /dev/zero > '"+iso+"'").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	v, err := a.EmuInstallStart(EmuInstallRequest{Name: "my-distro", ISO: iso, Mode: "manual", GB: 1, RAM: 256, CPUs: 1, Password: "rootpw"})
	if err != nil || len(v.Installs) != 1 {
		t.Fatalf("start: %v %+v", err, v.Installs)
	}
	id := v.Installs[0].ID
	defer func() { _, _ = a.EmuInstallCancel(id); time.Sleep(2 * time.Second) }()
	wait := func(stage string, d time.Duration) EmuInstall {
		end := time.Now().Add(d)
		for time.Now().Before(end) {
			emuLab.mu.Lock()
			j := *emuInstance(id)
			emuLab.mu.Unlock()
			if j.Stage == stage {
				return j
			}
			if j.Stage == instFailed {
				t.Fatalf("failed: %s", j.Err)
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatalf("not %s in time", stage)
		return EmuInstall{}
	}
	j := wait(instWaiting, 90*time.Second)
	if j.Port < 24900 || j.Console == "" {
		t.Fatalf("waiting job: %+v", j)
	}
	// the window is told how much of the disk is written (read from the host while the install waits)
	var seen EmuInstall
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end) && seen.Disk == 0; time.Sleep(500 * time.Millisecond) {
		emuLab.mu.Lock()
		for _, x := range emuInstallViews() {
			if x.ID == id {
				seen = x
			}
		}
		emuLab.mu.Unlock()
	}
	if seen.Disk == 0 || !strings.Contains(emuInstallProgress(seen), "installer disk written") {
		t.Errorf("no progress read from the host: %+v", seen)
	}
	cs, err := st.Get(j.Console)
	if err != nil || cs.Protocol != session.ProtoTelnet || cs.Port != j.Port || cs.JumpSession != host.Path {
		t.Fatalf("console session %+v %v", cs, err)
	}
	if _, err := a.EmuInstallFinish(id); err != nil {
		t.Fatal(err)
	}
	j = wait(instDone, 120*time.Second)
	if got := instSteps(j); got != "copy,start,console,shutdown,convert,fetch,done" {
		t.Errorf("steps %s", got)
	}
	v, _ = a.EmuState()
	var im *emu.Image
	for i := range v.Images {
		if v.Images[i].Image.ID == j.Image {
			im = &v.Images[i].Image
		}
	}
	if im == nil || im.FileName != "my-distro.qcow2" || im.File == "" || im.SHA256 == "" || im.Disk != "virtio" || im.DeviceType != "Linux" || im.Login != "root" || !strings.HasPrefix(im.Source, "installed from") {
		t.Fatalf("image %+v", im)
	}
	if sum, size, err := fileSHA256(im.File); err != nil || sum != im.SHA256 || size != im.Size {
		t.Fatalf("the file here: %v %s %d", err, sum, size)
	}
	if pw, _ := secrets.Lookup(emuImageKey(im.ID)); pw != "rootpw" {
		t.Errorf("the image's password was not kept")
	}
	if _, err := st.Get(j.Console); err == nil {
		t.Errorf("the console session was not removed")
	}
	out, _ := a.emuRun(host.Path, "ls termwright-lab/images termwright-lab/install 2>&1", 30*time.Second)
	if !strings.Contains(out, "my-distro.qcow2.sha256") || strings.Contains(out, ".qcow2\n") && strings.Contains(out, j.ID+".qcow2") {
		t.Errorf("host files after the install:\n%s", out)
	}
}

// instSteps lists the steps a job went through, in order.
func instSteps(j EmuInstall) string {
	var l []string
	for _, m := range j.Marks {
		l = append(l, m.Step)
	}
	return strings.Join(l, ",")
}

// fakeQemu stands in for qemu-system-x86_64 on the host: it serves the serial console of a live Alpine
// ISO (login, setup-alpine and its questions, poweroff) on the console port it is given.
const fakeQemu = `#!/usr/local/bin/python3
import os, re, socket, sys
a = sys.argv[1:]
m = re.search(r"port=(\d+)", " ".join(a)); port = int(m.group(1))
pidfile = a[a.index("-pidfile") + 1]
srv = socket.socket(); srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); srv.bind(("127.0.0.1", port)); srv.listen(1)
if os.fork() != 0:
    sys.exit(0)
os.setsid()
nul = os.open(os.devnull, os.O_RDWR)
for i in (0, 1, 2): os.dup2(nul, i)
open(pidfile, "w").write(str(os.getpid()))
def serve(c):
    f = c.makefile("rwb", buffering=0)
    def w(s): c.sendall(s.encode())
    def line():
        b = b""
        while True:
            ch = c.recv(1)
            if not ch: return None
            if ch in (b"\r", b"\n"):
                if b: return b.decode()
                continue
            b += ch
    w("Welcome to Alpine Linux 3.22\r\nKernel 6.12 on x86_64 (/dev/ttyS0)\r\n\r\nlocalhost login: ")
    while True:
        l = line()
        if l is None: return
        if l == "root": w("\r\nWelcome to Alpine!\r\nlocalhost:~# ")
        elif l.startswith("printf "):
            open(pidfile + ".ans", "w").write(l); w("\r\nlocalhost:~# ")
        elif l.startswith("setup-alpine"):
            w("\r\nWARNING: Erase the above disk(s) and continue? [y/N]: ")
            if line() != "y": w("Aborted\r\n"); continue
            w("Installing system on /dev/vda:\r\nNew password: "); p1 = line()
            w("\r\nRetype password: "); p2 = line()
            open(pidfile + ".pw", "w").write(p1 + "|" + p2)
            w("\r\nInstallation is complete. Please reboot.\r\nTWEND-0\r\nlocalhost:~# ")
        elif l == "poweroff":
            os._exit(0)
while True:
    c, _ = srv.accept()
    serve(c)
`

// TestEmuInstallAlpineFake runs the Alpine install end to end with fakeQemu in place of QEMU: the
// console is reached through the host, the installer is typed to, and the disk becomes an image.
// TW_EMU_FAKE_BIN is a folder that is in front of qemu in the SSH server's PATH (the fake is put there
// for the test and removed).
func TestEmuInstallAlpineFake(t *testing.T) {
	addr, keyFile, bin := os.Getenv("TW_EMU_SSH"), os.Getenv("TW_EMU_SSH_KEY"), os.Getenv("TW_EMU_FAKE_BIN")
	if addr == "" || keyFile == "" || bin == "" {
		t.Skip("TW_EMU_SSH / TW_EMU_SSH_KEY / TW_EMU_FAKE_BIN not set")
	}
	fake := filepath.Join(bin, "qemu-system-x86_64")
	if err := os.WriteFile(fake, []byte(fakeQemu), 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(fake)
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
	host, _ := st.Save(session.Session{Name: "kvm-1", Protocol: session.ProtoSSH2, Hostname: "127.0.0.1", Port: 2299, Username: "root"}, "")
	if _, err := a.EmuCheckHost(host.Path); err != nil {
		t.Fatal(err)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.f.Host = host.Path
	emuLab.mu.Unlock()
	iso := filepath.Join(dir, "alpine-virt-3.22.1-x86_64.iso")
	_ = os.WriteFile(iso, []byte(strings.Repeat("alpine", 1000)), 0o600)
	v, err := a.EmuInstallStart(EmuInstallRequest{Name: "alpine-test", ISO: iso, Mode: "alpine", GB: 1, RAM: 256, CPUs: 1, Password: "s3cretpw"})
	if err != nil {
		t.Fatal(err)
	}
	id := v.Installs[0].ID
	end := time.Now().Add(120 * time.Second)
	var j EmuInstall
	for time.Now().Before(end) {
		emuLab.mu.Lock()
		j = *emuInstance(id)
		emuLab.mu.Unlock()
		if j.Stage == instDone || j.Stage == instFailed {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if j.Stage != instDone {
		t.Fatalf("stage %s: %s %s", j.Stage, j.Msg, j.Err)
	}
	if got := instSteps(j); got != "copy,start,boot,login,install,poweroff,convert,fetch,done" {
		t.Errorf("steps %s", got)
	}
	var im *emu.Image
	for i := range emuLab.f.Images {
		if emuLab.f.Images[i].ID == j.Image {
			im = &emuLab.f.Images[i]
		}
	}
	if im == nil || im.Name != "alpine-test" || im.Version == "" || im.File == "" || im.Ready != "login:|Welcome to Alpine" || !strings.HasSuffix(im.Source, "alpine-virt-3.22.1-x86_64.iso") {
		t.Fatalf("image %+v", im)
	}
	if pw, _ := secrets.Lookup(emuImageKey(im.ID)); pw != "s3cretpw" {
		t.Error("the image has not the root password given")
	}
	out, _ := a.emuRun(host.Path, "cd termwright-lab/install && cat "+j.ID+".pid.pw; echo; cat "+j.ID+".pid.ans; echo; for e in qcow2 log port pid; do [ -e "+j.ID+".$e ] && echo left-$e; done; true", 30*time.Second)
	if !strings.Contains(out, "s3cretpw|s3cretpw") || !strings.Contains(out, `HOSTNAMEOPTS="-n alpine"`) || !strings.Contains(out, `DISKOPTS="-m sys /dev/vda"`) {
		t.Errorf("the installer was not typed what it needed:\n%s", out)
	}
	if strings.Contains(out, "left-") {
		t.Errorf("the installer's files were not cleaned:\n%s", out)
	}
	_, _ = a.emuRun(host.Path, "rm -f termwright-lab/install/*.pid.pw termwright-lab/install/*.pid.ans termwright-lab/install/f8885c28.*", 30*time.Second)
}
