package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/session"
)

// An install makes an image from an ISO file: a blank disk and the ISO in a
// QEMU of its own on a host. Alpine Linux is installed by typing on the
// installer's serial console; for other ISOs the installer's console is opened
// as a session and the user installs by hand. The disk then becomes an image
// (a qcow2 file, kept on this computer and on that host).

// EmuInstall is one install of an ISO, running or finished.
type EmuInstall struct {
	ID      string `json:"id"`
	Name    string `json:"name"` // the image's name
	Host    string `json:"host"` // the KVM host (a session path)
	Mode    string `json:"mode"` // "alpine" (typed by Termwright) or "manual" (the user installs on the console)
	ISO     string `json:"iso"`  // the ISO on this computer
	ISOName string `json:"isoName"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	GB      int    `json:"gb"` // the disk, rounded up to whole GB
	MB      int    `json:"mb"` // the disk in MB (exact)
	RAM     int    `json:"ram"`
	CPUs    int    `json:"cpus"`
	Dir     string `json:"dir"`  // the lab's folder on the host
	Port    int    `json:"port"` // the installer's console on the host (127.0.0.1)
	Stage   string `json:"stage"`
	Msg     string `json:"msg"`
	Err     string `json:"err,omitempty"`
	Image   string `json:"image,omitempty"`   // the image it made
	Console string `json:"console,omitempty"` // the console session (manual installs)
	Started string `json:"started"`
	// Step is the one the install is on (see emuInstallStage); Marks tell when each began, so the window can show how long each took.
	Step  string           `json:"step"`
	Marks []EmuInstallMark `json:"marks,omitempty"`
	File  string           `json:"file,omitempty"` // the image file being made on the host
	// What follows is read from the host while the install runs (filled in for the window, not saved).
	Tail string  `json:"tail,omitempty"` // the last lines of the installer's console
	Disk int64   `json:"disk,omitempty"` // the installer's disk on the host: bytes written so far (the file grows as the system is installed)
	Part int64   `json:"part,omitempty"` // the image being made: bytes so far
	Pct  float64 `json:"pct,omitempty"`  // the conversion to the image: percent
	Idle int     `json:"idle,omitempty"` // seconds since the installer's console showed anything
	// The verification boot: the image made is booted once (on a throwaway layer) to see it comes up.
	NoVerify   bool   `json:"noVerify,omitempty"`
	VerifyPort int    `json:"verifyPort,omitempty"` // its console on the host (its monitor is the next port)
	Verified   string `json:"verified,omitempty"`   // what the verification boot found ("login prompt in 42 s")
	VerifyErr  string `json:"verifyErr,omitempty"`  // why it is not verified (the image is kept all the same)
}

// EmuInstallMark is the time an install began a step.
type EmuInstallMark struct {
	Step string `json:"step"`
	At   string `json:"at"`
}

// emuInstallStage is the stage each step belongs to. The steps of an automatic
// install: copy, start, boot, login, install, poweroff, convert, fetch; of a
// manual one: copy, start, console, shutdown, convert, fetch.
var emuInstallStage = map[string]string{"copy": instCopying, "start": instStarting, "boot": instInstalling, "login": instInstalling, "install": instInstalling,
	"poweroff": instInstalling, "console": instWaiting, "shutdown": instSaving, "convert": instSaving, "fetch": instFetching, "verify": instVerifying}

// markStep moves a job to a step (the caller holds emuLab.mu).
func (j *EmuInstall) markStep(step string) {
	if st := emuInstallStage[step]; st != "" {
		j.Stage = st
	}
	if j.Step != step {
		j.Step = step
		j.Marks = append(j.Marks, EmuInstallMark{Step: step, At: time.Now().Format(time.RFC3339)})
	}
}

// Install stages. Running ones: copying, starting, installing, saving, fetching; waiting is the user's turn.
const (
	instCopying    = "copying"
	instStarting   = "starting"
	instInstalling = "installing"
	instWaiting    = "waiting"
	instSaving     = "saving"
	instFetching   = "fetching"
	instVerifying  = "verifying"
	instDone       = "done"
	instFailed     = "failed"
	instCancelled  = "cancelled"
)

// EmuInstallRequest is what the Install from ISO dialog sends.
type EmuInstallRequest struct {
	Name     string `json:"name"`
	ISO      string `json:"iso"`
	Mode     string `json:"mode"`         // "alpine" or "manual"
	GB       int    `json:"gb"`           // the disk in GB; MB wins when it is set
	MB       int    `json:"mb,omitempty"` // the disk in MB (for sizes that are not whole GB)
	RAM      int    `json:"ram"`
	CPUs     int    `json:"cpus"`
	Host     string `json:"host"`
	Password string `json:"password"`           // root's: set by the Alpine install; in a manual install the one the user sets (kept with the image)
	NoVerify bool   `json:"noVerify,omitempty"` // do not boot the image once at the end to check it comes up
}

// emuInstallPowerWait: how long a manual install's machine gets to shut down by itself before it is stopped.
var emuInstallPowerWait = 90 * time.Second

var emuInst struct {
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	live   map[string]*instLive
}

// instLive is what is read from the host while an install runs.
type instLive struct {
	console, log string // the last lines: of the console Termwright types on, of the host's log of it
	disk, part   int64
	pct          float64
	idle         int
}

// emuInstInit makes the maps (emuInst.mu is held).
func emuInstInit() {
	if emuInst.cancel == nil {
		emuInst.cancel, emuInst.live = map[string]context.CancelFunc{}, map[string]*instLive{}
	}
}

// emuInstLive is the live part of a job (emuInst.mu is held).
func emuInstLive(id string) *instLive {
	emuInstInit()
	if emuInst.live[id] == nil {
		emuInst.live[id] = &instLive{}
	}
	return emuInst.live[id]
}

func emuInstKey(id string) string { return "emu-install:" + id }

func (j *EmuInstall) spec() emu.InstallSpec {
	return emu.InstallSpec{ID: j.ID, Dir: j.Dir, ISO: j.ISOName, GB: j.GB, MB: j.MB, RAM: j.RAM, CPUs: j.CPUs}
}

func emuInstance(id string) *EmuInstall {
	for i := range emuLab.f.Installs {
		if emuLab.f.Installs[i].ID == id {
			return &emuLab.f.Installs[i]
		}
	}
	return nil
}

// emuInstallRunning says a job is busy on its own (no user turn).
func emuInstallRunning(stage string) bool {
	switch stage {
	case instCopying, instStarting, instInstalling, instSaving, instFetching, instVerifying:
		return true
	}
	return false
}

// emuInstallSet changes a job and saves.
func (a *App) emuInstallSet(id string, fn func(j *EmuInstall)) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	if j := emuInstance(id); j != nil {
		fn(j)
		_ = a.emuSave()
	}
}

// emuInstallsLoaded: jobs a stop of Termwright left in the middle cannot go on (the typing is lost).
func emuInstallsLoaded() {
	for i := range emuLab.f.Installs {
		if j := &emuLab.f.Installs[i]; emuInstallRunning(j.Stage) {
			j.Stage, j.Err = instFailed, "Termwright was closed while this ran. Remove it (the installer on the host is stopped) and start it again."
		}
	}
}

// emuInstallViews are the jobs for the window (the live bits filled in).
func emuInstallViews() []EmuInstall {
	out := append([]EmuInstall(nil), emuLab.f.Installs...)
	emuInst.mu.Lock()
	defer emuInst.mu.Unlock()
	for i := range out {
		if l := emuInst.live[out[i].ID]; l != nil && (emuInstallRunning(out[i].Stage) || out[i].Stage == instWaiting) {
			out[i].Tail, out[i].Disk, out[i].Part, out[i].Pct, out[i].Idle = orStr(l.console, l.log), l.disk, l.part, l.pct, l.idle
		}
		if out[i].Stage == instCopying {
			if p := emuLab.upload[out[i].ID]; p != "" {
				out[i].Msg = "copying the ISO to the host: " + p
			}
		}
	}
	return out
}

var emuNonName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// EmuInstallStart starts an install from an ISO file of this computer.
func (a *App) EmuInstallStart(r EmuInstallRequest) (EmuView, error) {
	defer emuAs("user")()
	r.Name = strings.TrimSpace(r.Name)
	if r.ISO == "" {
		return EmuView{}, errors.New("choose the ISO file")
	}
	if !strings.EqualFold(filepath.Ext(r.ISO), ".iso") {
		return EmuView{}, errors.New("the file is not an .iso file")
	}
	if r.Mode != "alpine" {
		r.Mode = "manual"
	}
	if r.Mode == "alpine" && r.Password == "" {
		return EmuView{}, errors.New("give the root password the new Alpine system gets")
	}
	if strings.ContainsAny(r.Password, "'\"\\\r\n") {
		return EmuView{}, errors.New("the password must not contain quotes, backslashes or line breaks")
	}
	base := filepath.Base(r.ISO)
	if r.Name == "" {
		r.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if r.MB < 0 {
		return EmuView{}, errors.New("the disk size must not be negative")
	}
	if r.MB == 0 {
		if r.GB < 1 {
			r.GB = 8
		}
		r.MB = r.GB * 1024
	}
	if r.MB < 100 {
		return EmuView{}, errors.New("the disk must be at least 100 MB")
	}
	if r.MB > 512*1024 {
		return EmuView{}, errors.New("the disk must be at most 512 GB")
	}
	r.GB = (r.MB + 1023) / 1024
	if r.RAM < 256 {
		r.RAM = 1024
	}
	if r.CPUs < 1 {
		r.CPUs = 2
	}
	st, err := os.Stat(r.ISO)
	if err != nil {
		return EmuView{}, err
	}
	emuLab.mu.Lock()
	a.emuLoad()
	host := orStr(r.Host, emuLab.f.Host)
	if host == "" {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no KVM host yet: add one in Hosts first")
	}
	dir := "termwright-lab"
	if emuLab.f.Lab != nil {
		dir = emuLab.f.Lab.Dir
	}
	for _, j := range emuLab.f.Installs {
		if emuInstallRunning(j.Stage) && j.Host == host {
			emuLab.mu.Unlock()
			return EmuView{}, fmt.Errorf("the install of %s is still running on this host; wait for it or cancel it", j.Name)
		}
	}
	j := EmuInstall{ID: randHex(4), Name: r.Name, Host: host, Mode: r.Mode, ISO: r.ISO, ISOName: emuNonName.ReplaceAllString(base, "_"), Size: st.Size(), GB: r.GB, MB: r.MB, RAM: r.RAM, CPUs: r.CPUs, Dir: dir, NoVerify: r.NoVerify,
		Stage: instCopying, Msg: "checking the ISO file", Started: time.Now().Format(time.RFC3339)}
	j.markStep("copy")
	if r.Password != "" {
		_ = secrets.Set(emuInstKey(j.ID), r.Password)
	}
	emuLab.f.Installs = append(emuLab.f.Installs, j)
	emuLab.upload[j.ID] = "0 %"
	emuEvOn(host, "", "info", fmt.Sprintf("install %s: copying %s to the host", j.Name, j.ISOName))
	err = a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	emuInst.mu.Lock()
	emuInstInit()
	emuInst.cancel[j.ID] = cancel
	emuInst.mu.Unlock()
	emuGo(func() { a.emuInstallRun(ctx, j.ID) })
	return a.EmuState()
}

// emuInstallFail ends a job with a reason.
func (a *App) emuInstallFail(id string, err error) {
	var host, name string
	cancelled := false
	a.emuInstallSet(id, func(j *EmuInstall) {
		if cancelled = j.Stage == instCancelled; !cancelled {
			j.Stage, j.Err, j.Msg = instFailed, err.Error(), ""
		}
		host, name = j.Host, j.Name
	})
	emuLab.mu.Lock()
	delete(emuLab.upload, id)
	if !cancelled {
		emuEvOn(host, "", "error", fmt.Sprintf("install %s failed: %s", name, err))
	}
	emuLab.mu.Unlock()
	if !cancelled {
		a.emuNotify(err, "")
	}
}

func (a *App) emuInstallMsg(id, stage, msg string) {
	a.emuInstallSet(id, func(j *EmuInstall) {
		if j.Stage == instCancelled {
			return
		}
		if stage != "" {
			j.Stage = stage
		}
		j.Msg = msg
	})
}

// emuInstallStep moves a job to a step, with the message to show.
func (a *App) emuInstallStep(id, step, msg string) {
	a.emuInstallSet(id, func(j *EmuInstall) {
		if j.Stage == instCancelled {
			return
		}
		j.markStep(step)
		j.Msg = msg
	})
}

// emuInstallWatch reads the installer's progress on its host every few seconds
// while the job runs: how much of the disk is written, the conversion's
// percent, how long the console has been quiet, the end of the console.
func (a *App) emuInstallWatch(id string) {
	for {
		emuLab.mu.Lock()
		var j EmuInstall
		if jp := emuInstance(id); jp != nil {
			j = *jp
		}
		emuLab.mu.Unlock()
		switch j.Stage {
		case instStarting, instInstalling, instWaiting, instSaving:
		default:
			emuInst.mu.Lock()
			delete(emuInst.live, id)
			emuInst.mu.Unlock()
			return
		}
		if out, err := a.emuRun(j.Host, j.spec().ProgressScript(j.File), 30*time.Second); err == nil {
			p := emu.ParseProgress(out)
			emuInst.mu.Lock()
			l := emuInstLive(id)
			l.disk, l.part, l.pct, l.idle, l.log = p.Disk, p.Part, p.Pct, p.Idle, emu.LastLines(p.Log, 12, 160)
			emuInst.mu.Unlock()
		}
		time.Sleep(4 * time.Second)
	}
}

// emuInstallRun copies the ISO, starts the installer and, for Alpine, types the install.
func (a *App) emuInstallRun(ctx context.Context, id string) {
	emuLab.mu.Lock()
	jp := emuInstance(id)
	if jp == nil {
		emuLab.mu.Unlock()
		return
	}
	j := *jp
	emuLab.mu.Unlock()
	sum, size, err := fileSHA256(j.ISO) // its checksum tells whether the host has it already
	if err != nil {
		a.emuInstallFail(id, err)
		return
	}
	j.SHA256, j.Size = sum, size
	a.emuInstallSet(id, func(x *EmuInstall) { x.SHA256, x.Size = sum, size })
	if err := a.emuUploadFile(j.Host, emu.Image{ID: j.ID, Name: j.Name}, j.ISOName, j.ISO, j.SHA256, j.Size, j.Dir, "cdrom"); err != nil {
		a.emuInstallFail(id, err)
		return
	}
	emuLab.mu.Lock()
	delete(emuLab.upload, id)
	emuLab.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	a.emuInstallStep(id, "start", "starting the installer on the host")
	out, err := a.emuRun(j.Host, j.spec().StartScript(), 2*time.Minute)
	_, port, fail := emu.ParseInstall(out)
	if err != nil || fail != "" || port == 0 {
		if err == nil {
			err = errors.New(orStr(fail, "the installer did not start: "+strings.TrimSpace(out)))
		}
		a.emuInstallFail(id, err)
		return
	}
	a.emuInstallSet(id, func(j *EmuInstall) { j.Port = port })
	j.Port = port
	emuGo(func() { a.emuInstallWatch(id) })
	if j.Mode == "manual" {
		sp := a.emuInstallSession(j)
		a.emuInstallSet(id, func(j *EmuInstall) {
			if j.Stage == instCancelled {
				return
			}
			j.markStep("console")
			j.Console = sp
			j.Msg = "the installer runs: open its console and install; press Finish when it is done"
		})
		return
	}
	a.emuInstallStep(id, "boot", "waiting for the installer to boot")
	pw, _ := secrets.Lookup(emuInstKey(id))
	cl, err := a.emuClient(ctx, j.Host)
	if err != nil {
		a.emuInstallFail(id, err)
		return
	}
	var conn net.Conn
	for try := 0; try < 20; try++ { // the console port opens a moment after QEMU starts
		if conn, err = cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		a.emuInstallFail(id, fmt.Errorf("the installer's console: %w", err))
		return
	}
	c := emu.NewConsole(conn)
	stop := make(chan struct{})
	emuGo(func() { // the end of the console, for the window
		for {
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Second):
				emuInst.mu.Lock()
				emuInstLive(id).console = c.Lines(12, 160)
				emuInst.mu.Unlock()
			}
		}
	})
	dns := emu.DefaultDNS[0]
	emuLab.mu.Lock()
	if emuLab.f.Lab != nil {
		dns = emuLab.f.Lab.DNSServers()[0]
	}
	emuLab.mu.Unlock()
	err = emu.InstallAlpine(ctx, c, emu.AlpineOpts{Hostname: "alpine", Password: pw, DNS: dns}, func(step, msg string) { a.emuInstallStep(id, step, msg) })
	close(stop)
	_ = c.Close()
	if err != nil {
		if ctx.Err() == nil {
			a.emuInstallFail(id, err)
		}
		return
	}
	// the machine powers itself off: wait for it
	a.emuInstallStep(id, "poweroff", "waiting for the installer to power off")
	if !a.emuInstallWaitDown(ctx, j, 5*time.Minute) {
		if ctx.Err() == nil {
			_, _ = a.emuRun(j.Host, j.spec().StopScript(), time.Minute)
		}
	}
	if ctx.Err() != nil {
		return
	}
	a.emuInstallSave(ctx, id)
}

// emuInstallWaitDown waits until the installer's QEMU has ended.
func (a *App) emuInstallWaitDown(ctx context.Context, j EmuInstall, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		out, err := a.emuRun(j.Host, j.spec().StatusScript(), 30*time.Second)
		if up, _, _ := emu.ParseInstall(out); err == nil && !up {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(3 * time.Second):
		}
	}
	return false
}

// emuInstallSession makes the console session of a manual install (a telnet session through the host).
func (a *App) emuInstallSession(j EmuInstall) string {
	emuLab.mu.Lock()
	cf := session.JoinPath(emuLab.f.Folder, "Consoles")
	emuLab.mu.Unlock()
	n := "install " + j.Name
	for i := 2; ; i++ {
		if _, err := a.store.Get(session.JoinPath(cf, n)); err != nil {
			break
		}
		n = "install " + j.Name + " (" + strconv.Itoa(i) + ")"
	}
	cs := session.Session{Name: n, Folder: cf, Protocol: session.ProtoTelnet, Hostname: "127.0.0.1", Port: j.Port, JumpSession: j.Host,
		Description: []string{"Serial console of the installer of " + j.Name + " (Termwright, Install from ISO)"}}
	if saved, err := a.store.Save(cs, ""); err == nil {
		return saved.Path
	}
	return ""
}

// EmuInstallFinish ends a manual install: the machine is shut down and its disk becomes an image.
func (a *App) EmuInstallFinish(id string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	j := emuInstance(id)
	if j == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such install")
	}
	if j.Stage != instWaiting {
		st := j.Stage
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("this install is not waiting for you (it is %s)", st)
	}
	j.markStep("shutdown")
	j.Msg = "shutting the machine down"
	jc := *j
	_ = a.emuSave()
	emuLab.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	emuInst.mu.Lock()
	emuInstInit()
	emuInst.cancel[id] = cancel
	emuInst.mu.Unlock()
	emuGo(func() {
		// a gentle shutdown first (the monitor's power button), then the process
		if cl, err := a.emuClient(ctx, jc.Host); err == nil {
			if m, err := cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(jc.Port+1))); err == nil {
				_, _ = m.Write([]byte("system_powerdown\n"))
				time.Sleep(time.Second)
				_ = m.Close()
			}
		}
		if !a.emuInstallWaitDown(ctx, jc, emuInstallPowerWait) && ctx.Err() == nil {
			_, _ = a.emuRun(jc.Host, jc.spec().StopScript(), time.Minute)
		}
		if ctx.Err() == nil {
			a.emuInstallSave(ctx, id)
		}
	})
	return a.EmuState()
}

// emuInstallSave turns the installed disk into an image: converted on the host, then brought here.
func (a *App) emuInstallSave(ctx context.Context, id string) {
	emuLab.mu.Lock()
	jp := emuInstance(id)
	if jp == nil {
		emuLab.mu.Unlock()
		return
	}
	j := *jp
	taken := map[string]bool{}
	for _, im := range emuLab.f.Images {
		taken[im.FileName] = true
	}
	emuLab.mu.Unlock()
	fn := emuNonName.ReplaceAllString(j.Name, "_") + ".qcow2"
	for n := 2; taken[fn]; n++ {
		fn = emuNonName.ReplaceAllString(j.Name, "_") + "-" + strconv.Itoa(n) + ".qcow2"
	}
	a.emuInstallSet(id, func(x *EmuInstall) { x.File = fn })
	a.emuInstallStep(id, "convert", "making the image file on the host (compressing the disk)")
	out, err := a.emuRun(j.Host, j.spec().SaveScript(fn), 15*time.Minute)
	var size int64
	var sum string
	if m := regexp.MustCompile(`(?m)^saved (\d+) ([0-9a-f]{64})$`).FindStringSubmatch(out); m != nil {
		size, _ = strconv.ParseInt(m[1], 10, 64)
		sum = m[2]
	} else {
		if err == nil {
			err = errors.New(orStr(strings.TrimPrefix(strings.TrimSpace(out), "fail "), "the disk could not be converted"))
		}
		a.emuInstallFail(id, err)
		return
	}
	// a copy of the file on this computer: other hosts get it from here
	local := filepath.Join(a.emuDownloadDir(), fn)
	if err := a.emuInstallFetch(ctx, j, "images/"+fn, local, size, sum); err != nil {
		if ctx.Err() == nil {
			a.emuInstallFail(id, err)
		}
		return
	}
	pw, _ := secrets.Lookup(emuInstKey(id))
	im := emu.Image{ID: randHex(4), Name: j.Name, File: local, FileName: fn, Size: size, SHA256: sum, DeviceType: "Linux", CPUs: 1, RAM: j.RAM, NICs: 3, NIC: "virtio-net-pci", Disk: "virtio", Ready: emuInstallReady(j.Mode), Login: "root",
		Version: versionIn(j.ISOName), Source: "installed from " + j.ISOName}
	emu.LinuxHost(&im)
	emuLab.mu.Lock()
	emuLab.f.Images = append(emuLab.f.Images, im)
	if pw != "" {
		emuPwChanged(im.ID)
		_ = secrets.Set(emuImageKey(im.ID), pw)
	}
	_ = secrets.Delete(emuInstKey(id))
	emuLab.mu.Unlock()
	verified, verr := "", ""
	if !j.NoVerify && ctx.Err() == nil {
		verified, verr = a.emuInstallVerify(ctx, id, j, fn)
	}
	_, _ = a.emuRun(j.Host, j.spec().CleanScript(), time.Minute)
	if j.Console != "" {
		_ = a.store.Delete(j.Console)
	}
	a.emuInstallSet(id, func(x *EmuInstall) {
		x.Stage, x.Msg, x.Image, x.Console, x.Err, x.File = instDone, "", im.ID, "", "", ""
		x.Verified, x.VerifyErr, x.VerifyPort = verified, verr, 0
		x.Step = "done"
		x.Marks = append(x.Marks, EmuInstallMark{Step: "done", At: time.Now().Format(time.RFC3339)})
	})
	emuLab.mu.Lock()
	emuEvOn(j.Host, "", "ok", fmt.Sprintf("install %s done: the image %s (%s) is ready", j.Name, im.Name, emuBytes(size)))
	emuLab.mu.Unlock()
	a.emuNotify(nil, "Image "+im.Name+" is ready")
}

// emuInstallFetch copies a file of the host (relative to the home folder) to this computer and checks its sum.
func (a *App) emuInstallFetch(ctx context.Context, j EmuInstall, remote, local string, size int64, sum string) error {
	a.emuInstallStep(j.ID, "fetch", "copying the image to this computer")
	cl, err := a.emuClient(ctx, j.Host)
	if err != nil {
		return err
	}
	sc, err := sftp.NewClient(cl)
	if err != nil {
		return fmt.Errorf("SFTP to the host: %w", err)
	}
	defer sc.Close()
	src, err := sc.Open(j.Dir + "/" + remote)
	if err != nil {
		return fmt.Errorf("the image file on the host: %w", err)
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
		return err
	}
	tmp := local + ".part"
	dst, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	pw := &progressWriter{w: io.MultiWriter(dst, h), total: size, fn: func(pct int) {
		a.emuInstallMsg(j.ID, instFetching, fmt.Sprintf("copying the image to this computer: %d %%", pct))
	}}
	_, err = io.Copy(pw, &ctxReader{ctx, src})
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("copying the image to this computer: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		_ = os.Remove(tmp)
		return errors.New("the copy of the image on this computer differs from the one on the host (checksum); it was removed")
	}
	return os.Rename(tmp, local)
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// EmuInstallCancel stops an install and throws away its disk (the ISO stays on the host).
func (a *App) EmuInstallCancel(id string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	j := emuInstance(id)
	if j == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such install")
	}
	jc := *j
	if jc.Stage == instDone || jc.Stage == instCancelled || jc.Stage == instFailed {
		emuLab.mu.Unlock()
		return a.EmuState()
	}
	j.Stage, j.Msg = instCancelled, ""
	delete(emuLab.upload, id)
	_ = a.emuSave()
	emuLab.mu.Unlock()
	emuInst.mu.Lock()
	if c := emuInst.cancel[id]; c != nil {
		c()
	}
	emuInst.mu.Unlock()
	emuGo(func() { a.emuInstallClean(jc) })
	return a.EmuState()
}

// emuInstallClean stops the installer on its host and removes its disk and console session.
func (a *App) emuInstallClean(j EmuInstall) {
	if j.Dir != "" && j.Host != "" {
		_, _ = a.emuRun(j.Host, j.spec().StopScript()+j.spec().CleanScript(), time.Minute)
	}
	if j.Console != "" {
		_ = a.store.Delete(j.Console)
	}
	_ = secrets.Delete(emuInstKey(j.ID))
}

// EmuInstallRemove takes a finished, failed or cancelled install out of the list.
func (a *App) EmuInstallRemove(id string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	j := emuInstance(id)
	if j == nil {
		emuLab.mu.Unlock()
		return a.EmuState()
	}
	if emuInstallRunning(j.Stage) || j.Stage == instWaiting {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("cancel the install first")
	}
	jc := *j
	var keep []EmuInstall
	for _, x := range emuLab.f.Installs {
		if x.ID != id {
			keep = append(keep, x)
		}
	}
	emuLab.f.Installs = keep
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	emuGo(func() { a.emuInstallClean(jc) })
	return a.EmuState()
}

// emuInstallProgress describes what is read from the host: " - disk written 142.0 MB, console quiet for 7 s" (for the connector's status and the log).
func emuInstallProgress(j EmuInstall) string {
	var parts []string
	if j.Disk > 0 && j.Step != "convert" && j.Step != "fetch" {
		parts = append(parts, "installer disk written "+emuBytes(j.Disk))
	}
	if j.Step == "convert" {
		parts = append(parts, fmt.Sprintf("image %.0f %% (%s)", j.Pct, emuBytes(j.Part)))
	}
	if j.Idle >= 10 && (j.Step == "boot" || j.Step == "login" || j.Step == "install" || j.Step == "console") {
		parts = append(parts, fmt.Sprintf("console quiet for %d s", j.Idle))
	}
	if len(parts) == 0 {
		return ""
	}
	return " - " + strings.Join(parts, ", ")
}

// emuInstallReady is the text that tells an installed system has booted: a login prompt, or - an installed
// Alpine gives root a shell on the serial console at once - its welcome text.
func emuInstallReady(mode string) string {
	if mode == "alpine" {
		return "login:|Welcome to Alpine"
	}
	return "login:"
}

// emuInstallVerifyWait: how long the verification boot gets to show a login prompt.
var emuInstallVerifyWait = 5 * time.Minute

// emuInstallVerify boots the image made once, on a throwaway layer, and waits for its login prompt (the text
// that tells it is up). It returns what it found, or why it is not verified (the image is kept either way).
func (a *App) emuInstallVerify(ctx context.Context, id string, j EmuInstall, fileName string) (string, string) {
	a.emuInstallStep(id, "verify", "booting the new image once to check it comes up")
	sp := j.spec()
	out, err := a.emuRun(j.Host, sp.VerifyScript(fileName), 2*time.Minute)
	_, port, fail := emu.ParseInstall(out + "\nstarted")
	if err != nil || fail != "" || port == 0 {
		return "", "the verification boot did not start: " + orStr(fail, strings.TrimSpace(out+" "+errText(err)))
	}
	defer func() { _, _ = a.emuRun(j.Host, sp.VerifyStopScript(), time.Minute) }()
	a.emuInstallSet(id, func(x *EmuInstall) { x.VerifyPort = port })
	cl, err := a.emuClient(ctx, j.Host)
	if err != nil {
		return "", err.Error()
	}
	var conn net.Conn
	for try := 0; try < 20; try++ {
		if conn, err = cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return "", "its console: " + err.Error()
	}
	c := emu.NewConsole(conn)
	defer c.Close()
	start := time.Now()
	re := regexp.MustCompile(emuInstallReady(j.Mode))
	for time.Since(start) < emuInstallVerifyWait && ctx.Err() == nil {
		if re.MatchString(c.Lines(40, 200)) {
			return fmt.Sprintf("it booted to its login prompt in %d s", int(time.Since(start).Seconds())), ""
		}
		emuInst.mu.Lock()
		emuInstLive(id).console = c.Lines(12, 160)
		emuInst.mu.Unlock()
		time.Sleep(2 * time.Second)
	}
	return "", fmt.Sprintf("no login prompt within %d minutes of booting it (it may still be fine: try it in a lab)", int(emuInstallVerifyWait.Minutes()))
}
