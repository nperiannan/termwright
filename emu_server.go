package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/secrets"
)

// Servers in the emulated lab: a Linux computer (Alpine) with HTTP, FTP, TFTP and DHCP for the lab and SSH/SFTP
// for files (internal/emu/server.go). Termwright sets it up on its console after it boots; its files and status
// go over SSH to its management address (through its host). The first server made from a plain Alpine image
// becomes the Server image: later servers start with the packages in them, offline.

// EmuServerFile is a file or folder in a server's shared folder.
type EmuServerFile struct {
	Path string `json:"path"` // under /srv ("cfg/a.cfg")
	Size int64  `json:"size"`
	Mod  string `json:"mod"` // RFC 3339
	Dir  bool   `json:"dir,omitempty"`
}

// EmuServerView is a server for its side pane.
type EmuServerView struct {
	Settings emu.Server       `json:"settings"`
	Addr     string           `json:"addr"` // its address on the DHCP port
	Mgmt     string           `json:"mgmt"` // where SSH/SFTP, FTP and HTTP reach it from the management network ("" = not known yet)
	Status   emu.ServerStatus `json:"status"`
	Files    []EmuServerFile  `json:"files"`
	Image    string           `json:"image"` // "server" (made from the Server image), "making" (the Server image is being made), ""
	Error    string           `json:"error,omitempty"`
}

func emuServerKey(id string) string { return "emu-server:" + id }

// emuServerFreeAddr is an address for a new server's port 1: 10.0.N.10/24, a network no other computer uses (lock held).
func emuServerFreeAddr(l *emu.Lab) string {
	used := map[string]bool{}
	for _, v := range l.VMs {
		for _, a := range v.Addrs {
			if _, n, err := net.ParseCIDR(a); err == nil {
				used[n.String()] = true
			}
		}
	}
	for i := 0; i < 250; i++ {
		if !used[fmt.Sprintf("10.0.%d.0/24", i)] {
			return fmt.Sprintf("10.0.%d.10/24", i)
		}
	}
	return "10.0.0.10/24"
}

// EmuAddServer adds a server (from the Server image when there is one, else from the Alpine computer image): its
// port 1 gets a fixed address, every service is on, its FTP/SSH user lab a new password (in the keychain).
func (a *App) EmuAddServer(host string, start bool) (EmuView, error) {
	defer emuAs("user")()
	if host == emu.LocalHost {
		return EmuView{}, errLocalMCU
	}
	emuLab.mu.Lock()
	a.emuLoad()
	pick := emuComputerImage(true)
	host = orStr(host, emuLab.f.Host)
	id := ""
	if pick != nil {
		id = pick.ID
	}
	before := map[string]bool{}
	if l := emuLab.f.Lab; l != nil {
		for _, v := range l.VMs {
			before[v.ID] = true
		}
	}
	emuLab.mu.Unlock()
	if id == "" {
		return EmuView{}, errors.New("there is no Linux image yet: add Alpine Linux from Images → catalog (Linux and test tools), then add the server")
	}
	if host == "" {
		return EmuView{}, errors.New("the lab has no KVM host yet: make a lab first")
	}
	if _, err := a.EmuAdd(EmuRequest{Host: host, Groups: []EmuGroup{{Image: id, Count: 1, Names: "srv-{n}"}}}); err != nil {
		return EmuView{}, err
	}
	emuLab.mu.Lock()
	var v *emu.VM
	for i := range emuLab.f.Lab.VMs {
		if !before[emuLab.f.Lab.VMs[i].ID] {
			v = &emuLab.f.Lab.VMs[i]
		}
	}
	if v == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("the server was not added")
	}
	addr := emuServerFreeAddr(emuLab.f.Lab)
	if v.Addrs == nil {
		v.Addrs = map[int]string{}
	}
	v.Addrs[1] = addr
	s := emu.ServerDefaults(addr)
	v.Server = &s
	v.Role = orStr(v.Role, "server")
	vid := v.ID
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	if err := secrets.Set(emuServerKey(vid), randHex(6)); err != nil {
		return EmuView{}, fmt.Errorf("its password could not be kept in the keychain: %w", err)
	}
	if start {
		return a.EmuStart([]string{vid})
	}
	return a.EmuState()
}

// emuServerSetup sets a server up on its console after it booted (the boot's console: logged in by then).
func (a *App) emuServerSetup(ctx context.Context, c *emu.Console, im emu.Image, v emu.VM, macs map[int]string) {
	if v.Server == nil {
		return
	}
	pw, _ := secrets.Lookup(emuServerKey(v.ID))
	if pw == "" {
		pw = randHex(6)
		_ = secrets.Set(emuServerKey(v.ID), pw)
	}
	emuLab.mu.Lock()
	emuLab.state[v.ID] = "server"
	emuLab.mu.Unlock()
	out, err := emuConsoleScript(ctx, c, im, emu.ServerScript(*v.Server, pw, macs[orInt(v.Server.DHCPPort, 1)]), 20)
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	if err == nil && !strings.Contains(out, "tw-server-ok") {
		err = errors.New(strings.TrimSpace(strings.TrimPrefix(lastLine(out), "tw-server-fail ")))
	}
	if err != nil {
		emuLab.errs[v.ID] = "server: " + err.Error()
		emuEv(v.ID, "error", "server not set up: "+err.Error())
		return
	}
	on := []string{}
	for _, x := range []struct {
		on   bool
		name string
	}{{v.Server.HTTP, "HTTP"}, {v.Server.FTP, "FTP"}, {v.Server.TFTP, "TFTP"}, {v.Server.DHCP, "DHCP"}} {
		if x.on {
			on = append(on, x.name)
		}
	}
	emuEv(v.ID, "ok", "server: "+orStr(strings.Join(on, ", "), "no services")+" on, SSH/SFTP for files")
	if vv := emuLab.f.Lab.VMByID(v.ID); vv != nil && vv.Server != nil && !vv.Server.Installed {
		vv.Server.Installed = true
		_ = a.emuSave()
		if !im.ServerImage && !emuHasServerImage() && !emuServerImageMaking { // the first: it becomes the Server image
			emuServerImageMaking = true
			id := v.ID
			emuGo(func() { a.emuMakeServerImage(id) })
		}
	}
}

var emuServerImageMaking bool // (mu held)

func emuHasServerImage() bool {
	for _, im := range emuLab.f.Images {
		if im.ServerImage {
			return true
		}
	}
	return false
}

// emuMakeServerImage makes the Server image from a server that was just set up: its disk (with the packages in
// it) copied into a new image on its host while it is paused for a moment, then brought to this computer so
// other hosts get it too.
func (a *App) emuMakeServerImage(id string) {
	defer func() {
		emuLab.mu.Lock()
		emuServerImageMaking = false
		emuLab.mu.Unlock()
	}()
	fail := func(err error) {
		emuLab.mu.Lock()
		emuEv(id, "warn", "Server image not made: "+err.Error()+" (the next server installs its packages again)")
		emuLab.mu.Unlock()
	}
	emuLab.mu.Lock()
	v := emuLab.f.Lab.VMByID(id)
	if v == nil {
		emuLab.mu.Unlock()
		return
	}
	base := emuImage(v.Image)
	if base == nil {
		emuLab.mu.Unlock()
		return
	}
	host := emuHostOf(*v)
	l := a.emuFor(host)
	mon, disk := l.MonitorPort(*v), l.VMDir()+"/"+v.ID+".qcow2"
	im := *base
	im.ID, im.Name, im.ServerImage = randHex(4), strings.TrimSpace(strings.TrimSuffix(base.Name, " Server"))+" Server", true
	im.FileName = strings.TrimSuffix(base.FileName, filepath.Ext(base.FileName)) + "-server.qcow2"
	im.File, im.SHA256, im.Source = "", "", "server image from "+base.Name+" ("+emu.ServerPackages+")"
	dir := l.Dir
	emuEv(id, "info", "making the Server image from it (it pauses for a few seconds)")
	emuLab.mu.Unlock()
	if _, err := a.emuComputerScript(id, "sync; echo ok", 2*time.Minute); err != nil {
		fail(err)
		return
	}
	h, err := a.emuMonitor(host, mon)
	if err != nil {
		fail(err)
		return
	}
	_, _ = h.do("stop", 5*time.Second)
	dst := dir + "/images/" + im.FileName
	out, err := a.emuRun(host, fmt.Sprintf("qemu-img convert -U -c -O qcow2 %s %s.part && mv %s.part %s && sha256sum %s | cut -d' ' -f1 | tee %s.sha256 && stat -c 'size=%%s' %s",
		emu.Q(disk), emu.Q(dst), emu.Q(dst), emu.Q(dst), emu.Q(dst), emu.Q(dst), emu.Q(dst)), 10*time.Minute)
	_, _ = h.do("cont", 5*time.Second)
	h.close()
	if err != nil || !strings.Contains(out, "size=") {
		fail(fmt.Errorf("copying its disk: %s", strings.TrimSpace(out+" "+errText(err))))
		return
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 64 {
			im.SHA256 = line
		}
		if n, ok := strings.CutPrefix(line, "size="); ok {
			im.Size, _ = strconv.ParseInt(n, 10, 64)
		}
	}
	// here too: the other hosts get it from this computer
	local := filepath.Join(a.emuDownloadDir(), im.FileName)
	if err := a.emuFetchFile(host, dst, local); err == nil {
		im.File = local
	}
	emuLab.mu.Lock()
	emuLab.f.Images = append(emuLab.f.Images, im)
	emuEv(id, "ok", "Server image made: "+im.Name+" ("+emuBytes(im.Size)+") - new servers start with their packages, offline")
	_ = a.emuSave()
	emuLab.mu.Unlock()
}

// emuFetchFile copies a file from a host to this computer.
func (a *App) emuFetchFile(host, remote, local string) error {
	cl, err := a.emuClient(context.Background(), host)
	if err != nil {
		return err
	}
	sc, err := sftp.NewClient(cl)
	if err != nil {
		return err
	}
	defer sc.Close()
	src, err := sc.Open(remote)
	if err != nil {
		return err
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return err
	}
	dst, err := os.Create(local + ".part")
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(local + ".part")
		return err
	}
	return os.Rename(local+".part", local)
}

// emuServerSSH logs in to a server as its user over SSH (Dropbear), through its host.
func (a *App) emuServerSSH(id string) (*ssh.Client, *emu.VM, error) {
	emuLab.mu.Lock()
	var v *emu.VM
	if emuLab.f.Lab != nil {
		v = emuLab.f.Lab.VMByID(id)
	}
	if v == nil || v.Server == nil {
		emuLab.mu.Unlock()
		return nil, nil, errors.New("no such server")
	}
	vv := *v
	host := emuHostOf(vv)
	l := a.emuFor(host)
	hostname, port, jump := emuTarget(l, vv, host)
	user := orStr(vv.Server.User, "lab")
	emuLab.mu.Unlock()
	pw, _ := secrets.Lookup(emuServerKey(id))
	addr := net.JoinHostPort(hostname, strconv.Itoa(port))
	var conn net.Conn
	var err error
	if jump == "" {
		conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
	} else {
		cl, cerr := a.emuClient(context.Background(), jump)
		if cerr != nil {
			return nil, nil, cerr
		}
		conn, err = cl.Dial("tcp", addr)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("its SSH (%s): %w - is it running and set up?", addr, err)
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.Password(pw)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 15 * time.Second}) // a lab VM of this topology, reached through its host
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("logging in to it as %s: %w", user, err)
	}
	return ssh.NewClient(cc, chans, reqs), &vv, nil
}

// EmuServerState is a server's settings, services, DHCP leases and files.
func (a *App) EmuServerState(id string) (EmuServerView, error) {
	emuLab.mu.Lock()
	var v *emu.VM
	if emuLab.f.Lab != nil {
		v = emuLab.f.Lab.VMByID(id)
	}
	if v == nil || v.Server == nil {
		emuLab.mu.Unlock()
		return EmuServerView{}, errors.New("no such server")
	}
	sv := EmuServerView{Settings: *v.Server, Addr: v.Addrs[orInt(v.Server.DHCPPort, 1)]}
	if im := emuImage(v.Image); im != nil && im.ServerImage {
		sv.Image = "server"
	} else if emuServerImageMaking {
		sv.Image = "making"
	}
	host := emuHostOf(*v)
	l := a.emuFor(host)
	if h, p, j := emuTarget(l, *v, host); p == 22 {
		sv.Mgmt = h
		if j != "" && l.Mgmt != "cloud" && l.Mgmt != "lan" && l.Mgmt != "bridge" {
			sv.Mgmt = "" // only through its host
		}
	}
	running := emuRunning(id) && emuLab.ready[id]
	emuLab.mu.Unlock()
	if !running {
		return sv, nil
	}
	cl, _, err := a.emuServerSSH(id)
	if err != nil {
		sv.Error = err.Error()
		return sv, nil
	}
	defer cl.Close()
	if s, err := cl.NewSession(); err == nil {
		out, _ := s.CombinedOutput(emu.ServerStatusScript)
		s.Close()
		sv.Status = emu.ParseServerStatus(string(out))
	}
	sc, err := sftp.NewClient(cl)
	if err != nil {
		sv.Error = "its files: " + err.Error()
		return sv, nil
	}
	defer sc.Close()
	w := sc.Walk("/srv")
	for w.Step() && len(sv.Files) < 500 {
		if w.Err() != nil || w.Path() == "/srv" {
			continue
		}
		st := w.Stat()
		sv.Files = append(sv.Files, EmuServerFile{Path: strings.TrimPrefix(w.Path(), "/srv/"), Size: st.Size(), Mod: st.ModTime().Format(time.RFC3339), Dir: st.IsDir()})
	}
	sort.Slice(sv.Files, func(i, j int) bool { return sv.Files[i].Path < sv.Files[j].Path })
	return sv, nil
}

// EmuSetServer changes a server's settings: kept with the topology, applied at once when it runs.
func (a *App) EmuSetServer(id string, s emu.Server) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	var v *emu.VM
	if emuLab.f.Lab != nil {
		v = emuLab.f.Lab.VMByID(id)
	}
	if v == nil || v.Server == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such server")
	}
	if err := s.Check(v.Addrs[orInt(s.DHCPPort, 1)]); err != nil {
		emuLab.mu.Unlock()
		return EmuView{}, err
	}
	s.Installed = v.Server.Installed
	*v.Server = s
	running := emuRunning(id) && emuLab.ready[id]
	mac := ""
	if im := emuImage(v.Image); im != nil {
		mac = a.emuFor(emuHostOf(*v)).PortMAC(*v, im, orInt(s.DHCPPort, 1))
	}
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	if running {
		pw, _ := secrets.Lookup(emuServerKey(id))
		out, err := a.emuComputerScript(id, emu.ServerScript(s, pw, mac), 10*time.Minute)
		if err == nil && !strings.Contains(out, "tw-server-ok") {
			err = errors.New(strings.TrimSpace(strings.TrimPrefix(lastLine(out), "tw-server-fail ")))
		}
		if err != nil {
			return EmuView{}, fmt.Errorf("applying it: %w", err)
		}
	}
	return a.EmuState()
}

// EmuServerPassword is a server's FTP and SSH password (Show).
func (a *App) EmuServerPassword(id string) (string, error) {
	pw, ok := secrets.Lookup(emuServerKey(id))
	if !ok {
		return "", errors.New("it has no password in the keychain")
	}
	return pw, nil
}

// emuSrvPath is a path under the shared folder (no way out of it).
func emuSrvPath(p string) (string, error) {
	c := path.Clean("/srv/" + strings.TrimLeft(filepath.ToSlash(p), "/"))
	if c != "/srv" && !strings.HasPrefix(c, "/srv/") {
		return "", fmt.Errorf("%q is outside the shared folder", p)
	}
	return c, nil
}

// EmuServerUpload copies files from this computer into a server's shared folder (into dir there, "" = its top).
func (a *App) EmuServerUpload(id string, files []string, dir string) (EmuServerView, error) {
	to, err := emuSrvPath(dir)
	if err != nil {
		return EmuServerView{}, err
	}
	cl, _, err := a.emuServerSSH(id)
	if err != nil {
		return EmuServerView{}, err
	}
	sc, err := sftp.NewClient(cl, sftp.UseConcurrentWrites(true))
	if err != nil {
		cl.Close()
		return EmuServerView{}, err
	}
	for _, f := range files {
		src, err := os.Open(f)
		if err != nil {
			sc.Close()
			cl.Close()
			return EmuServerView{}, err
		}
		dst, err := sc.Create(path.Join(to, filepath.Base(f)))
		if err == nil {
			_, err = dst.ReadFrom(src)
			if cerr := dst.Close(); err == nil {
				err = cerr
			}
		}
		src.Close()
		if err != nil {
			sc.Close()
			cl.Close()
			return EmuServerView{}, fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
	}
	sc.Close()
	cl.Close()
	emuLab.mu.Lock()
	emuEv(id, "info", fmt.Sprintf("server: %d file(s) copied in from this computer", len(files)))
	emuLab.mu.Unlock()
	return a.EmuServerState(id)
}

// EmuServerDownload copies a file of a server's shared folder to this computer (to).
func (a *App) EmuServerDownload(id, name, to string) error {
	from, err := emuSrvPath(name)
	if err != nil {
		return err
	}
	cl, _, err := a.emuServerSSH(id)
	if err != nil {
		return err
	}
	defer cl.Close()
	sc, err := sftp.NewClient(cl)
	if err != nil {
		return err
	}
	defer sc.Close()
	src, err := sc.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := src.WriteTo(dst); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// EmuServerDelete deletes files and folders (with what is in them) of a server's shared folder.
func (a *App) EmuServerDelete(id string, names []string) (EmuServerView, error) {
	cl, _, err := a.emuServerSSH(id)
	if err != nil {
		return EmuServerView{}, err
	}
	sc, err := sftp.NewClient(cl)
	if err != nil {
		cl.Close()
		return EmuServerView{}, err
	}
	for _, n := range names {
		p, err := emuSrvPath(n)
		if err == nil && p == "/srv" {
			err = errors.New("the shared folder itself stays")
		}
		if err == nil {
			err = sc.RemoveAll(p)
		}
		if err != nil {
			sc.Close()
			cl.Close()
			return EmuServerView{}, fmt.Errorf("%s: %w", n, err)
		}
	}
	sc.Close()
	cl.Close()
	return a.EmuServerState(id)
}

// EmuServerMkdir makes a folder in a server's shared folder.
func (a *App) EmuServerMkdir(id, name string) (EmuServerView, error) {
	p, err := emuSrvPath(name)
	if err != nil {
		return EmuServerView{}, err
	}
	cl, _, err := a.emuServerSSH(id)
	if err != nil {
		return EmuServerView{}, err
	}
	sc, err := sftp.NewClient(cl)
	if err == nil {
		err = sc.MkdirAll(p)
		sc.Close()
	}
	cl.Close()
	if err != nil {
		return EmuServerView{}, err
	}
	return a.EmuServerState(id)
}
