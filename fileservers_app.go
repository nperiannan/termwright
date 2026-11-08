package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/fileserve"
	"github.com/termwright/termwright/internal/secrets"
)

// File servers: TFTP, FTP and HTTP on this computer, so that devices can fetch
// software images and licences and send their configurations. Off until the
// user starts one; they stop after a time without transfers and when
// Termwright closes. The FTP password is generated and kept in the keychain.

const ftpPassKey = "termwright:file-servers:ftp"

var (
	fsMgr  = fileserve.New()
	fsOnce sync.Once
)

// FileServersView is everything the File Servers window shows.
type FileServersView struct {
	Settings  config.FileServersSettings `json:"settings"`
	State     fileserve.State            `json:"state"`
	Transfers []fileserve.Transfer       `json:"transfers"`
}

// FSAddress is an address of this computer the servers can listen on.
type FSAddress struct {
	IP      string `json:"ip"`
	Label   string `json:"label"`
	Reaches bool   `json:"reaches"` // the device of the open tab is reached through it
}

func (a *App) fsInit() {
	fsOnce.Do(func() {
		fsMgr.OnState = func(s fileserve.State) {
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "fs:state", s)
			}
		}
		fsMgr.OnTransfer = func(t fileserve.Transfer) {
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "fs:xfer", t)
			}
		}
	})
}

// fsSettings are the saved settings with the defaults filled in.
func (a *App) fsSettings() config.FileServersSettings {
	s := a.cfg.Get().FileServers
	if s.Folder == "" {
		home, _ := os.UserHomeDir()
		s.Folder = filepath.Join(home, "Termwright", "Files")
	}
	if s.TFTPPort == 0 {
		s.TFTPPort = 69
	}
	if s.FTPPort == 0 {
		s.FTPPort = 21
	}
	if s.HTTPPort == 0 {
		s.HTTPPort = 8080
	}
	if s.FTPUser == "" {
		s.FTPUser = "termwright"
	}
	if s.IdleMinutes == 0 {
		s.IdleMinutes = 30
	}
	return s
}

func ftpPassword() (string, error) {
	if p, ok := secrets.Lookup(ftpPassKey); ok && p != "" {
		return p, nil
	}
	return newFTPPassword()
}

func newFTPPassword() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	p := strings.NewReplacer("+", "x", "/", "z").Replace(base64.RawStdEncoding.EncodeToString(b))
	if err := secrets.Set(ftpPassKey, p); err != nil {
		return "", err
	}
	return p, nil
}

func (a *App) fsConfigure() error {
	a.fsInit()
	s := a.fsSettings()
	addr := s.Address
	if addr == "*" { // all addresses
		addr = ""
	}
	c := fileserve.Config{Folder: s.Folder, Address: addr, TFTPPort: s.TFTPPort, FTPPort: s.FTPPort, HTTPPort: s.HTTPPort,
		TFTPWrite: s.TFTPWrite, FTPWrite: s.FTPWrite, FTPUser: s.FTPUser}
	if s.IdleMinutes > 0 {
		c.Idle = time.Duration(s.IdleMinutes) * time.Minute
	}
	p, err := ftpPassword()
	if err != nil {
		return errors.New("the FTP password cannot be kept in the keychain: " + err.Error())
	}
	c.FTPPassword = p
	fsMgr.Configure(c)
	return nil
}

// FileServers is the state for the File Servers window.
func (a *App) FileServers() FileServersView {
	a.fsInit()
	if !fsMgr.Running() {
		_ = a.fsConfigure()
	}
	return FileServersView{Settings: a.fsSettings(), State: fsMgr.State(), Transfers: fsMgr.Transfers()}
}

// FileServersSave saves the settings; the upload switches apply at once, the
// rest when a server starts.
func (a *App) FileServersSave(s config.FileServersSettings) error {
	if err := a.ready(); err != nil {
		return err
	}
	for _, p := range []int{s.TFTPPort, s.FTPPort, s.HTTPPort} {
		if p < 0 || p > 65535 {
			return errors.New("a port is between 1 and 65535")
		}
	}
	s.FTPUser = strings.TrimSpace(s.FTPUser)
	if strings.ContainsAny(s.FTPUser, " \t\r\n") {
		return errors.New("the FTP user name has no spaces")
	}
	s.Folder = strings.TrimSpace(s.Folder)
	cur := a.cfg.Get()
	cur.FileServers = s
	if err := a.cfg.Save(cur); err != nil {
		return err
	}
	return a.fsConfigure()
}

// FileServersStart starts a server ("tftp", "ftp" or "http").
func (a *App) FileServersStart(kind string) error {
	if err := a.fsConfigure(); err != nil {
		return err
	}
	return fsMgr.Start(kind)
}

// FileServersStop stops a server, or all with "".
func (a *App) FileServersStop(kind string) {
	if kind == "" {
		fsMgr.StopAll()
		return
	}
	fsMgr.Stop(kind)
}

// FileServersClear forgets finished transfers.
func (a *App) FileServersClear() { fsMgr.Clear() }

// FileServersFiles lists the files in the folder.
func (a *App) FileServersFiles() []fileserve.File { return fileserve.Files(a.fsSettings().Folder) }

// FileServersPassword is the FTP password (for Copy and device commands).
func (a *App) FileServersPassword() (string, error) { return ftpPassword() }

// FileServersNewPassword makes a new FTP password (used from the next login).
func (a *App) FileServersNewPassword() (string, error) {
	p, err := newFTPPassword()
	if err == nil {
		_ = a.fsConfigure()
	}
	return p, err
}

// FileServersChooseFolder lets the user pick the folder to serve.
func (a *App) FileServersChooseFolder() (string, error) {
	return a.ChooseDirectory("Folder to serve to devices")
}

// FileServersShowFolder opens the folder in the file manager.
func (a *App) FileServersShowFolder() error {
	f := a.fsSettings().Folder
	if err := os.MkdirAll(f, 0o755); err != nil {
		return err
	}
	return a.ShowInFolder(f)
}

// FileServersAddresses are this computer's addresses; host (the open tab's
// device, may be "") marks the one that reaches it. Nothing is sent to the host.
func (a *App) FileServersAddresses(host string) []FSAddress {
	reach := ""
	if h := strings.TrimSpace(host); h != "" {
		if c, err := (&net.Dialer{Timeout: 2 * time.Second}).Dial("udp", net.JoinHostPort(h, "9")); err == nil {
			if ua, ok := c.LocalAddr().(*net.UDPAddr); ok {
				reach = ua.IP.String()
			}
			c.Close()
		}
	}
	var out []FSAddress
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, ad := range addrs {
			ipn, ok := ad.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			ip := ipn.IP.String()
			out = append(out, FSAddress{IP: ip, Label: ip + " - " + i.Name, Reaches: ip == reach})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Reaches && !out[j].Reaches })
	out = append(out, FSAddress{IP: "127.0.0.1", Label: "127.0.0.1 - this computer only", Reaches: reach == "127.0.0.1"})
	return out
}
