package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danieljoos/wincred"
	"github.com/gorilla/websocket"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/term"
)

// RemoteInfo describes an open RDP or VNC tab.
type RemoteInfo struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"` // "rdp" or "vnc"
	Title   string          `json:"title"`
	Addr    string          `json:"addr"`   // where the client connects: host:port, or the local tunnel port
	Via     string          `json:"via"`    // "via jump-station" when tunnelled
	Client  string          `json:"client"` // RDP: the program that was started
	WSURL   string          `json:"wsUrl"`  // VNC: WebSocket the viewer connects to
	HasPass bool            `json:"hasPass"`
	Note    string          `json:"note"` // e.g. "type the password in Windows App"
	Session session.Session `json:"session"`
}

type remoteConn struct {
	info   RemoteInfo
	tunnel *term.Tunnel
	token  string
	file   string
	pass   string
}

var (
	remoteMu sync.Mutex
	remotes  = map[string]*remoteConn{}
	wsAddr   string
	wsOnce   sync.Once
	wsErr    error
)

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// OpenRemote opens an RDP session in the computer's Remote Desktop client, or
// prepares Termwright's VNC viewer. Sessions behind a jump host or proxy get
// a local tunnel. s is used when path is empty (unsaved session).
func (a *App) OpenRemote(path string, s session.Session) (RemoteInfo, error) {
	if err := a.ready(); err != nil {
		return RemoteInfo{}, err
	}
	if path != "" {
		var err error
		if s, err = a.store.Get(path); err != nil {
			return RemoteInfo{}, err
		}
		a.cfg.AddRecent(s.Path)
		s = a.withFolderDefaults(s)
	}
	kind := strings.ToLower(s.Protocol)
	if kind != "rdp" && kind != "vnc" {
		return RemoteInfo{}, fmt.Errorf("%s is not a remote desktop protocol", s.Protocol)
	}
	if s.Hostname == "" {
		return RemoteInfo{}, errors.New("no host name")
	}
	port := s.Port
	if port == 0 {
		port = map[string]int{"rdp": 3389, "vnc": 5900}[kind]
	}
	id := randHex(8)
	r := &remoteConn{info: RemoteInfo{ID: id, Kind: kind, Title: s.Name, Session: s}}
	if r.info.Title == "" {
		r.info.Title = s.Hostname
	}
	if s.Path != "" && a.terms.SavedPassword != nil {
		r.pass, r.info.HasPass = a.terms.SavedPassword(s.Path)
	}
	target := net.JoinHostPort(s.Hostname, strconv.Itoa(port))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tu, err := a.terms.OpenTunnel(ctx, s, id, target)
	if err != nil {
		return RemoteInfo{}, err
	}
	r.tunnel, r.info.Via, r.info.Addr = tu, tu.Via, target
	if kind == "vnc" {
		if err := startWS(); err != nil {
			tu.Close()
			return RemoteInfo{}, err
		}
		r.token = randHex(16)
		r.info.WSURL = "ws://" + wsAddr + "/vnc/" + r.token
	} else {
		if !tu.Direct() {
			if r.info.Addr, err = tu.Listen(); err != nil {
				tu.Close()
				return RemoteInfo{}, err
			}
		}
		if err := a.launchRDP(r); err != nil {
			tu.Close()
			return RemoteInfo{}, err
		}
	}
	remoteMu.Lock()
	remotes[id] = r
	remoteMu.Unlock()
	return r.info, nil
}

// ReopenRemote starts the RDP client again for an open RDP tab.
func (a *App) ReopenRemote(id string) error {
	r, err := remoteOf(id)
	if err != nil {
		return err
	}
	if r.info.Kind != "rdp" {
		return nil
	}
	return a.launchRDP(r)
}

// RemoteCredentials returns the user name and saved password for a remote tab
// (the VNC viewer asks when the server wants them).
func (a *App) RemoteCredentials(id string) (map[string]string, error) {
	r, err := remoteOf(id)
	if err != nil {
		return nil, err
	}
	user := r.info.Session.Username
	if r.info.Session.RDPDomain != "" && r.info.Kind == "rdp" {
		user = r.info.Session.RDPDomain + `\` + user
	}
	return map[string]string{"username": user, "password": r.pass}, nil
}

// CopyRemotePassword puts the saved password on the clipboard for 60 seconds
// (for RDP clients that cannot be given it).
func (a *App) CopyRemotePassword(id string) error {
	r, err := remoteOf(id)
	if err != nil {
		return err
	}
	if r.pass == "" || a.ctx == nil {
		return errors.New("no saved password")
	}
	if err := wruntime.ClipboardSetText(a.ctx, r.pass); err != nil {
		return err
	}
	pw := r.pass
	go func() {
		time.Sleep(60 * time.Second)
		if cur, _ := wruntime.ClipboardGetText(a.ctx); cur == pw {
			_ = wruntime.ClipboardSetText(a.ctx, "")
		}
	}()
	return nil
}

// RemoteStatus reports how many tunnelled connections an RDP tab has.
func (a *App) RemoteStatus(id string) (int, error) {
	r, err := remoteOf(id)
	if err != nil {
		return 0, err
	}
	return r.tunnel.Connections(), nil
}

// CloseRemote closes a remote tab: its tunnel, WebSocket and .rdp file.
func (a *App) CloseRemote(id string) {
	remoteMu.Lock()
	r := remotes[id]
	delete(remotes, id)
	remoteMu.Unlock()
	if r == nil {
		return
	}
	r.tunnel.Close()
	if r.file != "" {
		_ = os.Remove(r.file)
	}
}

func remoteOf(id string) (*remoteConn, error) {
	remoteMu.Lock()
	defer remoteMu.Unlock()
	r := remotes[id]
	if r == nil {
		return nil, errors.New("this remote desktop tab is closed")
	}
	return r, nil
}

// ---- RDP ----

// rdpFile is the content of a .rdp file for the address and session options.
func rdpFile(addr string, s session.Session) string {
	var b strings.Builder
	line := func(k, v string) { b.WriteString(k + ":" + v + "\r\n") }
	line("full address:s", addr)
	if u := rdpUser(s); u != "" {
		line("username:s", u)
	}
	if s.RDPFullScreen {
		line("screen mode id:i", "2")
	} else {
		line("screen mode id:i", "1")
	}
	if w, h, ok := rdpSize(s.RDPSize); ok {
		line("desktopwidth:i", strconv.Itoa(w))
		line("desktopheight:i", strconv.Itoa(h))
	}
	line("dynamic resolution:i", "1")
	line("redirectclipboard:i", "1")
	line("authentication level:i", "2")
	line("prompt for credentials:i", "0")
	return b.String()
}

func rdpUser(s session.Session) string {
	if s.Username == "" {
		return ""
	}
	if s.RDPDomain != "" {
		return s.RDPDomain + `\` + s.Username
	}
	return s.Username
}

func rdpSize(v string) (int, int, bool) {
	w, h, ok := strings.Cut(strings.ToLower(strings.TrimSpace(v)), "x")
	if !ok {
		return 0, 0, false
	}
	wi, e1 := strconv.Atoi(strings.TrimSpace(w))
	hi, e2 := strconv.Atoi(strings.TrimSpace(h))
	if e1 != nil || e2 != nil || wi < 200 || hi < 200 {
		return 0, 0, false
	}
	return wi, hi, true
}

// freerdpArgs are the FreeRDP command-line arguments (the password is read from stdin).
func freerdpArgs(addr string, s session.Session, withPass bool) []string {
	args := []string{"/v:" + addr, "/cert:tofu", "+clipboard", "/dynamic-resolution"}
	if s.Username != "" {
		args = append(args, "/u:"+s.Username)
	}
	if s.RDPDomain != "" {
		args = append(args, "/d:"+s.RDPDomain)
	}
	if s.RDPFullScreen {
		args = append(args, "/f")
	} else if w, h, ok := rdpSize(s.RDPSize); ok {
		args = append(args, fmt.Sprintf("/size:%dx%d", w, h))
	}
	if withPass {
		args = append(args, "/from-stdin")
	}
	return append(args, "/title:"+s.Name)
}

func (a *App) launchRDP(r *remoteConn) error {
	s := r.info.Session
	if r.file == "" {
		r.file = filepath.Join(os.TempDir(), "termwright-"+r.info.ID+".rdp")
	}
	if err := os.WriteFile(r.file, []byte(rdpFile(r.info.Addr, s)), 0o600); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		if r.pass != "" && s.Username != "" {
			host, _, _ := net.SplitHostPort(r.info.Addr)
			c := wincred.NewDomainPassword("TERMSRV/" + host)
			c.UserName = rdpUser(s)
			c.SetPassword(r.pass)
			if err := c.Write(); err == nil {
				go func() { time.Sleep(2 * time.Minute); _ = c.Delete() }()
			}
		}
		cmd := exec.Command("mstsc.exe", r.file)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("could not start Remote Desktop (mstsc): %w", err)
		}
		r.info.Client = "Remote Desktop Connection"
		go a.watchRDP(r, cmd)
	case "darwin":
		app := ""
		for _, p := range []string{"/Applications/Windows App.app", "/Applications/Microsoft Remote Desktop.app"} {
			if _, err := os.Stat(p); err == nil {
				app = p
				break
			}
		}
		if app == "" {
			return errors.New("no RDP client found: install Windows App (Microsoft) from the Mac App Store")
		}
		if err := exec.Command("open", "-a", app, r.file).Run(); err != nil {
			return fmt.Errorf("could not open %s: %w", filepath.Base(app), err)
		}
		r.info.Client = strings.TrimSuffix(filepath.Base(app), ".app")
		if r.pass != "" {
			r.info.Note = r.info.Client + " asks for the password: use Copy Password and paste it there."
		}
	default:
		for _, bin := range []string{"xfreerdp3", "xfreerdp", "sdl-freerdp3", "sdl-freerdp", "wlfreerdp"} {
			p, err := exec.LookPath(bin)
			if err != nil {
				continue
			}
			cmd := exec.Command(p, freerdpArgs(r.info.Addr, s, r.pass != "")...)
			if r.pass != "" {
				cmd.Stdin = strings.NewReader(r.pass + "\n")
			}
			if err := cmd.Start(); err != nil {
				return err
			}
			r.info.Client = "FreeRDP"
			go a.watchRDP(r, cmd)
			return nil
		}
		if p, err := exec.LookPath("remmina"); err == nil {
			if err := exec.Command(p, "-c", r.file).Start(); err != nil {
				return err
			}
			r.info.Client = "Remmina"
			return nil
		}
		return errors.New("no RDP client found: install FreeRDP (xfreerdp) or Remmina")
	}
	return nil
}

// watchRDP reports when the RDP client exits.
func (a *App) watchRDP(r *remoteConn, cmd *exec.Cmd) {
	err := cmd.Wait()
	msg := "The Remote Desktop window was closed."
	if err != nil && r.info.Client == "FreeRDP" {
		msg = "FreeRDP ended: " + err.Error()
	}
	emitter{a}.Emit("remote:closed:"+r.info.ID, msg)
}

// ---- VNC: WebSocket to TCP for the built-in viewer ----

func startWS() error {
	wsOnce.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			wsErr = err
			return
		}
		wsAddr = ln.Addr().String()
		mux := http.NewServeMux()
		mux.HandleFunc("/vnc/", serveVNC)
		go func() { _ = (&http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}).Serve(ln) }()
	})
	return wsErr
}

var upgrader = websocket.Upgrader{
	ReadBufferSize: 64 << 10, WriteBufferSize: 64 << 10,
	Subprotocols: []string{"binary"},
	// the page comes from the app (wails:// or http://wails.localhost); the
	// random token in the path is what authorises the connection
	CheckOrigin: func(*http.Request) bool { return true },
}

func serveVNC(w http.ResponseWriter, req *http.Request) {
	token := strings.TrimPrefix(req.URL.Path, "/vnc/")
	var r *remoteConn
	remoteMu.Lock()
	for _, x := range remotes {
		if x.token != "" && x.token == token {
			r = x
		}
	}
	remoteMu.Unlock()
	if r == nil {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	ws, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	c, err := r.tunnel.Dial(ctx)
	cancel()
	if err != nil {
		_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4000, trimReason(err.Error())), time.Now().Add(time.Second))
		return
	}
	defer c.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 64<<10)
		for {
			n, err := c.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		if _, err := c.Write(data); err != nil {
			break
		}
	}
	_ = c.Close()
	<-done
}

// trimReason keeps a WebSocket close reason within its 123-byte limit.
func trimReason(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}
