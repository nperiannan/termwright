package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// Moving a switch to another KVM host while it keeps what it has (its disk,
// configuration, persona and day 0):
//   live: QEMU live migration - its disks are mirrored to the new host while
//         it runs, then its memory moves; it pauses for well under a second;
//   save: its running state is saved, its disk copied, and it resumes there
//         (down for about the copy time); a stopped switch only has its disk copied.
// Cables keep working: relays on the old host forward what the other
// switches still send there, until they restart.

// emuHMP is a QEMU monitor (HMP over telnet) on a host.
type emuHMP struct {
	c   net.Conn
	r   *bufio.Reader
	buf strings.Builder
}

var hmpJunk = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]|\xff[\xfb-\xfe].|\xff\xff|\r`)

func (a *App) emuMonitor(host string, port int) (*emuHMP, error) {
	cl, err := a.emuClient(context.Background(), host)
	if err != nil {
		return nil, err
	}
	var c net.Conn
	for i := 0; i < 20; i++ { // a QEMU just started opens its monitor a moment later
		if c, err = cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		return nil, fmt.Errorf("its monitor: %w", err)
	}
	h := &emuHMP{c: c, r: bufio.NewReader(c)}
	if _, err := h.until(20 * time.Second); err != nil {
		c.Close()
		return nil, fmt.Errorf("its monitor does not answer: %w", err)
	}
	return h, nil
}

// until reads up to the next "(qemu) " prompt.
func (h *emuHMP) until(wait time.Duration) (string, error) {
	_ = h.c.SetReadDeadline(time.Now().Add(wait))
	h.buf.Reset()
	b := make([]byte, 4096)
	for {
		n, err := h.r.Read(b)
		h.buf.Write(b[:n])
		if t := h.buf.String(); strings.HasSuffix(strings.TrimRight(t, " "), "(qemu)") {
			return hmpJunk.ReplaceAllString(t, ""), nil
		}
		if err != nil {
			return hmpJunk.ReplaceAllString(h.buf.String(), ""), err
		}
	}
}

// do runs a monitor command and returns what it printed.
func (h *emuHMP) do(cmd string, wait time.Duration) (string, error) {
	if _, err := h.c.Write([]byte(cmd + "\n")); err != nil {
		return "", err
	}
	out, err := h.until(wait)
	// the echo of the command first, the prompt last
	if i := strings.Index(out, "\n"); i >= 0 {
		out = out[i+1:]
	}
	out = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(out), "(qemu)"))
	return out, err
}

func (h *emuHMP) close() { h.c.Close() }

// emuCopyFile copies a file from one host to another through this computer
// (the two SSH connections it has; no login between the hosts needed).
func (a *App) emuCopyFile(from, to, src, dst string, txt func(string)) error {
	total := int64(0)
	if out, err := a.emuRun(from, "stat -c %s "+emu.Q(src)+" 2>/dev/null || stat -f %z "+emu.Q(src), 30*time.Second); err == nil {
		total, _ = strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	}
	cf, err := a.emuClient(context.Background(), from)
	if err != nil {
		return err
	}
	ct, err := a.emuClient(context.Background(), to)
	if err != nil {
		return err
	}
	ss, err := cf.NewSession()
	if err != nil {
		return err
	}
	defer ss.Close()
	r, err := ss.StdoutPipe()
	if err != nil {
		return err
	}
	if err := ss.Start("cat " + emu.Q(src)); err != nil {
		return err
	}
	ds, err := ct.NewSession()
	if err != nil {
		return err
	}
	defer ds.Close()
	w, err := ds.StdinPipe()
	if err != nil {
		return err
	}
	var out strings.Builder
	ds.Stdout, ds.Stderr = &out, &out
	if err := ds.Start(fmt.Sprintf("mkdir -p %s && cat > %s.part && mv %s.part %s", emu.Q(path.Dir(dst)), emu.Q(dst), emu.Q(dst), emu.Q(dst))); err != nil {
		return err
	}
	pw := &progressWriter{w: w, total: total, txt: txt}
	_, cerr := io.Copy(pw, r)
	_ = w.Close()
	if err := ds.Wait(); err != nil {
		return fmt.Errorf("copying to the new host: %v %s", err, strings.TrimSpace(out.String()))
	}
	if cerr != nil {
		return cerr
	}
	if total > 0 && pw.n != total {
		return fmt.Errorf("copying to the new host: %s of %s arrived", emuBytes(pw.n), emuBytes(total))
	}
	return ss.Wait()
}

type emuDiskInfo struct {
	Path    string // on the host, relative to its home
	Backing string // the file it is a layer on (its name in the images folder)
	Size    int64
}

func (a *App) emuDisksOn(host string, l *emu.Lab, id string) ([]emuDiskInfo, error) {
	out, err := a.emuRun(host, fmt.Sprintf("for d in %s/%s.qcow2 %s/%s-hd?.qcow2; do [ -f \"$d\" ] && echo \"@@ $d\" && qemu-img info -U --output=json \"$d\"; done", l.VMDir(), id, l.VMDir(), id), time.Minute)
	if err != nil {
		return nil, err
	}
	var ds []emuDiskInfo
	for _, part := range strings.Split(out, "@@ ")[1:] {
		nl := strings.Index(part, "\n")
		if nl < 0 {
			continue
		}
		var j struct {
			Size    int64  `json:"virtual-size"`
			Backing string `json:"backing-filename"`
		}
		_ = json.Unmarshal([]byte(part[nl+1:]), &j)
		ds = append(ds, emuDiskInfo{Path: strings.TrimSpace(part[:nl]), Backing: path.Base(j.Backing), Size: j.Size})
	}
	if len(ds) == 0 {
		return nil, errors.New("its disk is not on its host")
	}
	return ds, nil
}

// emuMoveTarget checks that a switch can move to a host keeping its address.
// emuMoveTarget checks a move to another host; readdr: its management address
// does not work there, so it gets one of the new host's (typed when it starts there).
func (a *App) emuMoveTarget(v emu.VM, to string) (readdr bool, err error) {
	l, main := emuLab.f.Lab, emuLab.f.Host
	from := l.HostOf(v, main)
	if to == from {
		return false, errors.New("it runs there already")
	}
	var info *emu.HostInfo
	for i := range emuLab.f.Hosts {
		if emuLab.f.Hosts[i].Session == to {
			info = &emuLab.f.Hosts[i].Info
		}
	}
	if info == nil || len(info.Problems) > 0 {
		return false, errors.New("that host is not ready: check it under Hosts")
	}
	name := to[strings.LastIndex(to, "/")+1:]
	nf := l.Net(from, main)
	onNet := func(n emu.HostNet) bool { // the host is on the network the addresses come from
		pf, e1 := netip.ParsePrefix(n.MgmtNet)
		ha, e2 := netip.ParsePrefix(info.LanAddr)
		return e1 != nil || e2 != nil || pf.Masked().Contains(ha.Addr())
	}
	hostNet := func(n emu.HostNet) bool { return n.Mgmt == "cloud" || n.Mgmt == "lan" && n.LanFixed }
	if !slices_contains(l.Hosts(main), to) { // a host new to the lab: the same management when it can work there
		n := nf
		n.LanIf, n.LanKind, n.HostIP, n.CloudIf = info.LanIf, info.LanKind, info.LanAddr, info.LanIf
		if hostNet(n) && !onNet(n) { // another network: its own lab bridge (addresses typed at start) - change it in Hosts
			n = emu.HostNet{Mgmt: "bridge", LanIf: info.LanIf, LanKind: info.LanKind, MgmtNet: "10.99.0.0/24"}
			if !info.Sudo {
				n.Mgmt = "forward"
			}
		}
		l.SetNet(to, main, n)
	}
	nt := l.Net(to, main)
	if nt.Mgmt != "forward" && !info.Sudo {
		return false, fmt.Errorf("%s gives its switches their management through sudo (%s), and its user has no sudo without a password: run Set Up for it under Hosts, or set its management to Port forwarding", name, map[string]string{"lan": "the host's network", "cloud": "the host's network, routed", "nat": "NAT", "bridge": "a lab bridge"}[nt.Mgmt])
	}
	lt := *l
	lt.Mgmt, lt.MgmtNet, lt.LanFixed = nt.Mgmt, nt.MgmtNet, nt.LanFixed
	ff := nf.Mgmt == "bridge" || hostNet(nf) // fixed address here (in its configuration)
	same := nt.Mgmt == nf.Mgmt && nt.MgmtNet == nf.MgmtNet && (!hostNet(nt) || onNet(nt))
	switch {
	case same:
		return false, nil
	case lt.Fixed(): // the new host types its own address at each start
		return true, nil
	case ff:
		return false, fmt.Errorf("its fixed address %s does not work on %s, which gives addresses by %s: in Hosts set %s's management to Lab bridge or Host network, routed (then it gets an address there), or use From Its Image", strings.Split(v.MgmtIP, "/")[0], name, map[string]string{"nat": "NAT", "forward": "port forwarding", "lan": "DHCP"}[nt.Mgmt], name)
	}
	return false, nil // DHCP here and there
}

func slices_contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// EmuMigrate moves a switch to another host keeping everything it has: live
// (no downtime, it must run) or by saving, copying and resuming it.
func (a *App) EmuMigrate(id, to string, live bool) (EmuView, error) {
	v, err := a.emuMigrate(id, to, live)
	if err != nil { // a refused move is in the Events too, not only in a message that goes away
		emuLab.mu.Lock()
		emuEv(id, "error", "not moved to "+to[strings.LastIndex(to, "/")+1:]+": "+err.Error())
		emuLab.mu.Unlock()
	}
	return v, err
}

func (a *App) emuMigrate(id, to string, live bool) (EmuView, error) {
	st := a.emuPollFresh()
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
	_, up := st.Up[id]
	if live && !up {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("a live move needs the switch running: start it, or use Copy Its Disk")
	}
	if s := emuLab.state[id]; s != "" {
		emuLab.mu.Unlock()
		return EmuView{}, fmt.Errorf("it is busy (%s)", s)
	}
	readdr, err := a.emuMoveTarget(*v, to)
	if err != nil {
		emuLab.mu.Unlock()
		return EmuView{}, err
	}
	if live && a.emuFor(emuHostOf(*v)).KVM != a.emuFor(to).KVM {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("one host has KVM and the other does not: a running switch cannot change that - use Save, Copy and Resume instead")
	}
	emuLab.state[id] = "moving: to " + to[strings.LastIndex(to, "/")+1:]
	emuLab.moveTo[id] = to
	emuEv(id, "info", "moving to "+to[strings.LastIndex(to, "/")+1:]+map[bool]string{true: " live", false: " with its disk"}[live])
	emuLab.mu.Unlock()
	go func() {
		var err error
		if live {
			if err = a.emuMoveLive(id, to); err == nil && readdr {
				a.emuReaddress(id, to, true)
			}
		} else {
			err = a.emuMoveCopy(id, to, up, readdr)
		}
		emuLab.mu.Lock()
		delete(emuLab.state, id)
		delete(emuLab.moveTo, id)
		if err != nil {
			emuEv(id, "error", "not moved to "+to[strings.LastIndex(to, "/")+1:]+": "+err.Error())
		}
		emuLab.mu.Unlock()
		emuLab.statusAt = time.Time{}
	}()
	time.Sleep(50 * time.Millisecond)
	return a.EmuState()
}

// emuReaddress gives a moved switch an address of its new host's network (its old one does not work there).
// A running one (a live move) gets it typed on its console now - it keeps running; else when it starts.
func (a *App) emuReaddress(id, to string, running bool) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	v := emuLab.f.Lab.VMByID(id)
	if v == nil {
		return
	}
	was := strings.Split(v.MgmtIP, "/")[0]
	v.MgmtIP, v.Gateway = a.emuFor(to).FreeAddress(*v)
	v.Direct = false
	delete(emuLab.addrOK, id)
	when := "typed when it starts"
	if running {
		emuLab.day0[id] = true // the console step types the new address (the old one removed first), as at a start
		when = "typed on its console now - it keeps running"
	}
	emuEv(id, "info", fmt.Sprintf("its address %s does not work on %s's network: it gets %s there (%s)", was, to[strings.LastIndex(to, "/")+1:], strings.Split(v.MgmtIP, "/")[0], when))
	_ = a.emuSave()
	vv := *v
	if running {
		go a.emuDay0(to, vv) // its address step (day 0 is done already)
	} else {
		go a.emuRetarget(vv)
	}
}

// emuMoveProgress shows a move's copy with how far it is, its speed and the time left.
func (a *App) emuMoveProgress(id, step string) func(string) {
	return func(t string) {
		emuLab.mu.Lock()
		if emuLab.moveTo[id] != "" {
			emuLab.state[id] = "moving: " + step + " · " + t
		}
		emuLab.mu.Unlock()
	}
}

func (a *App) emuMoveStep(id, text string) {
	emuLab.mu.Lock()
	emuLab.state[id] = "moving: " + text
	emuEv(id, "info", "moving: "+text)
	emuLab.mu.Unlock()
}

// emuMovePrep: the image (and the booted copy its disk is on) on the new host;
// returns the disks and, per backing file, its name on the new host.
func (a *App) emuMovePrep(id, from, to string) (emu.VM, emu.Image, *emu.Lab, []emuDiskInfo, map[string]string, error) {
	emuLab.mu.Lock()
	vv := *emuLab.f.Lab.VMByID(id)
	im := *emuImage(vv.Image)
	lf := a.emuFor(from)
	dir := emuLab.f.Lab.Dir
	emuLab.mu.Unlock()
	a.emuMoveStep(id, "the image on the new host")
	if err := a.emuUpload(to, im, dir); err != nil {
		return vv, im, lf, nil, nil, err
	}
	disks, err := a.emuDisksOn(from, lf, id)
	if err != nil {
		return vv, im, lf, nil, nil, err
	}
	bases := map[string]string{}
	for _, d := range disks {
		if !strings.HasSuffix(d.Backing, emu.BootedSuffix) {
			bases[d.Backing] = d.Backing
			continue
		}
		// a booted copy is its host's own: the new host gets this one under a name of its own
		name := strings.TrimSuffix(d.Backing, emu.BootedSuffix) + ".booted-" + emuShort(from) + ".qcow2"
		if out, _ := a.emuRun(to, fmt.Sprintf("[ -f %s/%s ] && echo have", lf.ImageDir(), name), time.Minute); !strings.Contains(out, "have") {
			a.emuMoveStep(id, "copying the booted image to the new host")
			if err := a.emuCopyFile(from, to, lf.ImageDir()+"/"+d.Backing, lf.ImageDir()+"/"+name, a.emuMoveProgress(id, "copying the booted image to the new host")); err != nil {
				return vv, im, lf, nil, nil, err
			}
		}
		bases[d.Backing] = name
	}
	return vv, im, lf, disks, bases, nil
}

func emuShort(host string) string {
	h := emu.Hash(host)
	return h[:6]
}

// emuMoved: the switch is on its new host now - relays for its cables on the old
// one, its old disk gone, the lab and its sessions updated.
func (a *App) emuMoved(id, from, to string, lf *emu.Lab, vOld emu.VM, fast string) {
	emuLab.mu.Lock()
	fromIP, toIP := emuLab.f.Lab.Addrs[from], emuLab.f.Lab.Addrs[to]
	emuLab.mu.Unlock()
	script := lf.RelayScript(vOld, fromIP, toIP) + fmt.Sprintf("rm -f %s/%s.qcow2 %s/%s-hd?.qcow2 %s/%s.pid %s/%s.log\n", lf.VMDir(), id, lf.VMDir(), id, lf.VMDir(), id, lf.VMDir(), id)
	if out, err := a.emuRun(from, script, time.Minute); err != nil || !strings.Contains(out, "relays") {
		emuLab.mu.Lock()
		emuEv(id, "warn", "the relays for its cables did not start on the old host (python3?): restart the switches cabled to it to reconnect them")
		emuLab.mu.Unlock()
	}
	emuLab.mu.Lock()
	v := emuLab.f.Lab.VMByID(id)
	if v == nil {
		emuLab.mu.Unlock()
		return
	}
	v.Host = ""
	if to != emuLab.f.Host {
		v.Host = to
	}
	v.Relayed, v.FastKey = true, fast
	vv := *v
	_ = a.emuSave()
	emuLab.mu.Unlock()
	a.emuRetarget(vv)
}

// emuMoveLive: QEMU live migration with its disks mirrored to the new host.
func (a *App) emuMoveLive(id, to string) error {
	emuLab.mu.Lock()
	from := emuHostOf(*emuLab.f.Lab.VMByID(id))
	emuLab.mu.Unlock()
	vOld, im, lf, disks, bases, err := a.emuMovePrep(id, from, to)
	if err != nil {
		return err
	}
	emuLab.mu.Lock()
	toIP := emuLab.f.Lab.Addrs[to]
	// the switch as the new host runs it: listening on all addresses, waiting for its state
	vNew := vOld
	vNew.Host, vNew.Relayed = to, true
	if to == emuLab.f.Host {
		vNew.Host = ""
	}
	tmp := *emuLab.f.Lab
	tmp.VMs = append([]emu.VM(nil), tmp.VMs...)
	for i := range tmp.VMs {
		if tmp.VMs[i].ID == id {
			tmp.VMs[i] = vNew
		}
	}
	real := emuLab.f.Lab
	emuLab.f.Lab = &tmp
	lt := a.emuFor(to)
	emuLab.f.Lab = real
	monPort := lt.MonitorPort(vNew)
	emuLab.mu.Unlock()
	if toIP == "" {
		return errors.New("the new host's address is not known: check it under Hosts")
	}
	migPort, nbdPort := 29400+vOld.Index%300, 29700+vOld.Index%300
	// its disks there, empty layers on the same bases; then it starts, waiting
	var mk strings.Builder
	fmt.Fprintf(&mk, "mkdir -p %s\n", lt.VMDir())
	for _, d := range disks {
		fmt.Fprintf(&mk, "rm -f %s; qemu-img create -q -f qcow2 -F qcow2 -b \"$HOME/%s/%s\" %s %d || echo fail\n", d.Path, lt.ImageDir(), bases[d.Backing], d.Path, d.Size)
	}
	if out, err := a.emuRun(to, mk.String(), time.Minute); err != nil || strings.Contains(out, "fail") {
		return fmt.Errorf("its disk on the new host: %v %s", err, out)
	}
	a.emuMoveStep(id, "starting it on the new host to receive it")
	vIn := vNew
	vIn.Incoming = fmt.Sprintf("tcp:0.0.0.0:%d", migPort)
	if out, err := a.emuRun(to, lt.StartScript([]emu.VM{vIn}, map[string]*emu.Image{im.ID: &im}), 2*time.Minute); err != nil || !strings.Contains(out, "started "+id) {
		return fmt.Errorf("it could not wait on the new host: %v %s", err, strings.TrimSpace(out))
	}
	undo := func() { // the old one keeps running; the new one goes
		_, _ = a.emuRun(to, lt.StopScript([]string{id})+fmt.Sprintf("rm -f %s/%s.qcow2 %s/%s-hd?.qcow2\n", lt.VMDir(), id, lt.VMDir(), id), time.Minute)
	}
	tgt, err := a.emuMonitor(to, monPort)
	if err != nil {
		undo()
		return err
	}
	defer tgt.close()
	src, err := a.emuMonitor(from, lf.MonitorPort(vOld))
	if err != nil {
		undo()
		return err
	}
	defer src.close()
	// QEMU up to 9.0 copies the disk with the memory (its base is on the new host
	// already); a newer one mirrors the disk over NBD first
	help, _ := src.do("help migrate", 10*time.Second)
	inc := strings.Contains(help, "-i")
	cancelJobs := func() {}
	migFlags := "-d -i"
	if !inc {
		migFlags = "-d"
		// which drives: the qcow2 disks in its folder
		blk, _ := src.do("info block", 10*time.Second)
		var drives []string
		for _, line := range strings.Split(blk, "\n") {
			if f := strings.Fields(line); len(f) > 2 && strings.Contains(line, lf.VMDir()+"/"+id) {
				drives = append(drives, strings.TrimSuffix(f[0], ":"))
			}
		}
		if len(drives) == 0 {
			undo()
			return fmt.Errorf("its disks were not found on its monitor: %s", blk)
		}
		if out, err := tgt.do(fmt.Sprintf("nbd_server_start 0.0.0.0:%d", nbdPort), 20*time.Second); err != nil || strings.Contains(strings.ToLower(out), "error") {
			undo()
			return fmt.Errorf("the new host could not take its disk (NBD port %d): %v %s", nbdPort, err, out)
		}
		for _, d := range drives {
			if out, err := tgt.do("nbd_server_add -w "+d, 20*time.Second); err != nil || strings.Contains(strings.ToLower(out), "error") {
				ib, _ := tgt.do("info block", 10*time.Second)
				cmdl, _ := a.emuRun(to, fmt.Sprintf("tr '\\0' ' ' < /proc/$(cat %s/%s.pid)/cmdline", lt.VMDir(), id), 10*time.Second)
				ib += " CMD: " + cmdl
				undo()
				return fmt.Errorf("the new host could not take its disk %s: %v %s [%s]", d, err, out, ib)
			}
		}
		a.emuMoveStep(id, "copying its disk while it runs")
		for _, d := range drives {
			if out, err := src.do(fmt.Sprintf("drive_mirror -n %s nbd://%s:%d/%s raw", d, toIP, nbdPort, d), 30*time.Second); err != nil || strings.Contains(strings.ToLower(out), "error") || strings.Contains(strings.ToLower(out), "fail") {
				_, _ = src.do("block_job_cancel -f "+d, 10*time.Second)
				undo()
				return fmt.Errorf("its disk did not start copying to %s (TCP %d between the hosts?): %s", toIP, nbdPort, out)
			}
		}
		cancelJobs = func() {
			for _, d := range drives {
				_, _ = src.do("block_job_cancel -f "+d, 10*time.Second)
			}
		}
		done := regexp.MustCompile(`Completed (\d+) of (\d+) bytes`)
		for end := time.Now().Add(45 * time.Minute); ; time.Sleep(time.Second) {
			out, err := src.do("info block-jobs", 10*time.Second)
			ready := err == nil
			ms := done.FindAllStringSubmatch(out, -1)
			if len(ms) < len(drives) {
				ready = false
			}
			for _, m := range ms {
				if m[1] != m[2] {
					ready = false
					a.emuMoveStep(id, fmt.Sprintf("copying its disk while it runs (%s of %s MB)", mb(m[1]), mb(m[2])))
				}
			}
			if ready {
				break
			}
			if time.Now().After(end) || strings.Contains(out, "No active jobs") {
				cancelJobs()
				undo()
				return fmt.Errorf("its disk did not get copied: %s", out)
			}
		}

	}
	a.emuMoveStep(id, map[bool]string{true: "moving its disk and memory (live)", false: "moving its memory (live)"}[inc])
	if out, err := src.do(fmt.Sprintf("migrate %s tcp:%s:%d", migFlags, toIP, migPort), 30*time.Second); err != nil || strings.Contains(strings.ToLower(out), "error") {
		cancelJobs()
		undo()
		return fmt.Errorf("its memory did not start moving (TCP %d between the hosts?): %s", migPort, out)
	}
	var status, last string
	liveStep := map[bool]string{true: "moving its disk and memory (live)", false: "moving its memory (live)"}[inc]
	show := a.emuMoveProgress(id, liveStep)
	kb := func(out, what string) int64 {
		m := regexp.MustCompile(`(?m)^` + what + `: (\d+) kbytes`).FindStringSubmatch(out)
		if m == nil {
			return -1
		}
		n, _ := strconv.ParseInt(m[1], 10, 64)
		return n * 1024
	}
	liveStart := time.Now()
	for end := time.Now().Add(30 * time.Minute); ; time.Sleep(500 * time.Millisecond) {
		out, _ := src.do("info migrate", 10*time.Second)
		last = out
		// how far: the memory sent, of all of it (pages written again are sent again, so it can slow near the end)
		if tot, rem := kb(out, "total ram"), kb(out, "remaining ram"); tot > 0 && rem >= 0 {
			show(emuRate(tot-rem, tot, 0, liveStart))
		}
		if m := regexp.MustCompile(`(?m)^Migration status: (\S+)`).FindStringSubmatch(out); m != nil {
			status = m[1]
		}
		if status == "completed" || status == "failed" || status == "cancelled" || time.Now().After(end) {
			break
		}
	}
	if status != "completed" {
		_, _ = src.do("migrate_cancel", 10*time.Second)
		cancelJobs()
		_, _ = src.do("cont", 10*time.Second)
		undo()
		why := status
		if m := regexp.MustCompile(`(?m)^Migration status: failed \((.*)\)`).FindStringSubmatch(last); m != nil {
			why = m[1]
			if strings.Contains(why, "bdrv_inactivate") {
				why = "it was started by an older Termwright: stop and start it once, then move it live"
			}
		} else if os.Getenv("TW_EMU_DEBUG") != "" {
			why += " " + last
		}
		return fmt.Errorf("its memory did not move (%s): it keeps running where it was", why)
	}
	cancelJobs()
	_, _ = src.do("quit", 5*time.Second)
	if !inc {
		_, _ = tgt.do("nbd_server_stop", 10*time.Second)
	}
	if out, _ := tgt.do("info status", 10*time.Second); !strings.Contains(out, "running") {
		_, _ = tgt.do("cont", 10*time.Second)
	}
	_, _ = tgt.do("announce_self", 10*time.Second) // its MAC addresses on the network, from the new place
	a.emuMgmtFollow(id, to, lt, vNew)
	_, _ = a.emuRun(from, lf.StopScript([]string{id}), time.Minute) // its old interfaces and routes
	a.emuMoved(id, from, to, lf, vOld, "")
	emuLab.mu.Lock()
	emuLab.ready[id] = true
	emuEv(id, "ok", "moved live to "+to[strings.LastIndex(to, "/")+1:]+": it kept running (configuration, address and sessions as they were)")
	emuLab.mu.Unlock()
	return nil
}

// emuMgmtFollow: a routed (cloud) address answers from the new host at once.
func (a *App) emuMgmtFollow(id, to string, lt *emu.Lab, v emu.VM) {
	if lt.Mgmt != "cloud" || v.MgmtIP == "" || lt.CloudIf == "" {
		return
	}
	ip := strings.Split(v.MgmtIP, "/")[0]
	_, _ = a.emuRun(to, fmt.Sprintf("command -v arping >/dev/null 2>&1 && sudo -n arping -c 3 -U -I %s %s >/dev/null 2>&1; true", emu.Q(lt.CloudIf), ip), 30*time.Second)
}

func mb(b string) string {
	n, _ := strconv.ParseInt(b, 10, 64)
	return strconv.FormatInt(n>>20, 10)
}

// emuMoveCopy: save its running state (when it runs), copy its disk, resume it there.
func (a *App) emuMoveCopy(id, to string, running, readdr bool) error {
	emuLab.mu.Lock()
	from := emuHostOf(*emuLab.f.Lab.VMByID(id))
	emuLab.mu.Unlock()
	saved := false
	if running && !readdr { // with a new address it boots there: no state to save
		a.emuMoveStep(id, "saving its running state")
		emuLab.mu.Lock()
		lf := a.emuFor(from)
		script := lf.SaveScript([]string{id})
		emuLab.mu.Unlock()
		out, err := a.emuRun(from, script, 15*time.Minute)
		saved = err == nil && strings.Contains(out, "saved "+id)
		if !saved {
			emuLab.mu.Lock()
			emuEv(id, "warn", "its running state was not saved: it boots on the new host")
			emuLab.mu.Unlock()
		}
	}
	if running && readdr {
		a.emuMoveStep(id, "stopping it (it starts there with its new address)")
		emuLab.mu.Lock()
		stop := a.emuFor(from).StopScript([]string{id})
		emuLab.mu.Unlock()
		if _, err := a.emuRun(from, stop, 2*time.Minute); err != nil {
			return fmt.Errorf("stopping it on the old host: %w", err)
		}
	}
	vOld, im, lf, disks, bases, err := a.emuMovePrep(id, from, to)
	if err != nil {
		return err
	}
	for _, d := range disks {
		step := fmt.Sprintf("copying its disk %s to the new host", path.Base(d.Path))
		a.emuMoveStep(id, step)
		if err := a.emuCopyFile(from, to, d.Path, d.Path, a.emuMoveProgress(id, step)); err != nil {
			return err
		}
		if out, err := a.emuRun(to, fmt.Sprintf("qemu-img rebase -u -F qcow2 -b \"$HOME/%s/%s\" %s || echo fail", lf.ImageDir(), bases[d.Backing], d.Path), time.Minute); err != nil || strings.Contains(out, "fail") {
			return fmt.Errorf("its disk on the new host: %v %s", err, out)
		}
	}
	fast := ""
	if saved && !readdr { // with a new address it boots there (its address is typed at the start)
		emuLab.mu.Lock()
		vNew := vOld
		vNew.Host = to
		if to == emuLab.f.Host {
			vNew.Host = ""
		}
		real := emuLab.f.Lab
		tmp := *real
		tmp.VMs = append([]emu.VM(nil), real.VMs...)
		for i := range tmp.VMs {
			if tmp.VMs[i].ID == id {
				tmp.VMs[i] = vNew
			}
		}
		emuLab.f.Lab = &tmp
		fast = a.emuFor(to).FastKey(vNew, &im) // it resumes there
		emuLab.f.Lab = real
		emuLab.mu.Unlock()
	}
	a.emuMoved(id, from, to, lf, vOld, fast)
	if readdr {
		a.emuReaddress(id, to, false)
	}
	if running {
		a.emuMoveStep(id, map[bool]string{true: "resuming it on the new host", false: "starting it on the new host"}[saved])
		emuLab.mu.Lock()
		delete(emuLab.state, id)
		emuLab.mu.Unlock()
		a.emuStart(to, []string{id})
		emuLab.mu.Lock()
		lt, v := a.emuFor(to), *emuLab.f.Lab.VMByID(id)
		emuLab.mu.Unlock()
		if h, err := a.emuMonitor(to, lt.MonitorPort(v)); err == nil {
			_, _ = h.do("announce_self", 10*time.Second)
			h.close()
		}
		a.emuMgmtFollow(id, to, lt, v)
	}
	emuLab.mu.Lock()
	if e := emuLab.errs[id]; running && e != "" { // moved, but it did not start there: say so, not "resumed"
		emuEv(id, "error", "moved to "+to[strings.LastIndex(to, "/")+1:]+" with its disk, but it did not start there: "+e)
	} else {
		emuEv(id, "ok", "moved to "+to[strings.LastIndex(to, "/")+1:]+" with its disk"+map[bool]string{true: " and running state: it resumed there", false: ""}[saved])
	}
	emuLab.mu.Unlock()
	return nil
}
