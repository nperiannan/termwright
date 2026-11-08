// Package fileserve runs small TFTP, FTP and HTTP servers on this computer so
// that network devices can fetch files from it (software images, licences)
// and send files to it (configurations). Every server serves one folder and
// nothing outside it; uploads are allowed only when turned on; the servers
// stop on their own after a time without transfers.
package fileserve

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Kinds of server.
const (
	TFTP = "tftp"
	FTP  = "ftp"
	HTTP = "http"
)

// Config is what the servers use; the folder, address and ports are read when
// a server starts, the upload switches at every request.
type Config struct {
	Folder      string
	Address     string // "" = all addresses
	TFTPPort    int
	FTPPort     int
	HTTPPort    int
	TFTPWrite   bool
	FTPWrite    bool
	FTPUser     string
	FTPPassword string
	Idle        time.Duration // 0 = never stop on its own
}

// Server is the state of one server.
type Server struct {
	Kind    string `json:"kind"`
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	Address string `json:"address"`
	Error   string `json:"error,omitempty"`
}

// State is the state of all servers.
type State struct {
	Servers []Server `json:"servers"`
	Folder  string   `json:"folder"`
	Stopped string   `json:"stopped,omitempty"` // why they stopped on their own
}

// Transfer is one file going to or coming from a device.
type Transfer struct {
	ID     int64     `json:"id"`
	Server string    `json:"server"`
	Peer   string    `json:"peer"`
	File   string    `json:"file"`
	Put    bool      `json:"put"`   // the device sends the file
	State  string    `json:"state"` // running, done, error, refused
	Bytes  int64     `json:"bytes"`
	Size   int64     `json:"size"` // -1 = not known
	Error  string    `json:"error,omitempty"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end,omitempty"`
	last   time.Time // last event sent
}

// File is a file in the folder (for building device commands).
type File struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

const maxTransfers = 200

var idleTick = 15 * time.Second // shorter in tests

type runner interface {
	stop()
	addr() net.Addr
}

// Manager runs the servers.
type Manager struct {
	mu       sync.Mutex
	cfg      Config
	root     *os.Root
	run      map[string]runner
	errs     map[string]string
	xfers    []*Transfer
	nextID   int64
	active   int
	lastUse  time.Time
	stopped  string
	idleStop chan struct{}

	// OnState and OnTransfer are told about changes (from any goroutine).
	OnState    func(State)
	OnTransfer func(Transfer)
}

// New makes a manager with nothing running.
func New() *Manager {
	return &Manager{run: map[string]runner{}, errs: map[string]string{}}
}

// Configure sets the configuration; servers already running keep their
// folder, address and port until they are started again.
func (m *Manager) Configure(c Config) {
	m.mu.Lock()
	m.cfg = c
	m.mu.Unlock()
}

func (m *Manager) config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Running tells whether any server runs.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.run) > 0
}

// State is the state now.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stateLocked()
}

func (m *Manager) stateLocked() State {
	st := State{Folder: m.cfg.Folder, Stopped: m.stopped}
	for _, k := range []string{TFTP, FTP, HTTP} {
		s := Server{Kind: k, Port: m.port(k), Address: m.cfg.Address, Error: m.errs[k]}
		if r, ok := m.run[k]; ok {
			s.Running = true
			if a := r.addr(); a != nil {
				if h, p, err := net.SplitHostPort(a.String()); err == nil {
					s.Port, _ = strconv.Atoi(p)
					if ip := net.ParseIP(h); ip != nil && !ip.IsUnspecified() {
						s.Address = h
					}
				}
			}
		}
		st.Servers = append(st.Servers, s)
	}
	return st
}

func (m *Manager) port(k string) int {
	switch k {
	case TFTP:
		return m.cfg.TFTPPort
	case FTP:
		return m.cfg.FTPPort
	}
	return m.cfg.HTTPPort
}

func (m *Manager) changed() {
	m.mu.Lock()
	st := m.stateLocked()
	f := m.OnState
	m.mu.Unlock()
	if f != nil {
		f(st)
	}
}

// Start starts one server.
func (m *Manager) Start(kind string) error {
	m.mu.Lock()
	if _, ok := m.run[kind]; ok {
		m.mu.Unlock()
		return nil
	}
	c := m.cfg
	if c.Folder == "" {
		m.mu.Unlock()
		return errors.New("choose the folder to serve first")
	}
	if err := os.MkdirAll(c.Folder, 0o755); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("the folder cannot be made: %w", err)
	}
	if m.root == nil || len(m.run) == 0 {
		if m.root != nil {
			m.root.Close()
		}
		r, err := os.OpenRoot(c.Folder)
		if err != nil {
			m.mu.Unlock()
			return fmt.Errorf("the folder cannot be opened: %w", err)
		}
		m.root = r
	}
	port := m.port(kind)
	m.mu.Unlock()

	addr := net.JoinHostPort(c.Address, strconv.Itoa(port))
	var r runner
	var err error
	switch kind {
	case TFTP:
		r, err = m.startTFTP(addr)
	case FTP:
		if c.FTPUser == "" || c.FTPPassword == "" {
			err = errors.New("the FTP server needs a user name and password")
		} else {
			r, err = m.startFTP(addr)
		}
	case HTTP:
		r, err = m.startHTTP(addr)
	default:
		err = fmt.Errorf("unknown server %q", kind)
	}
	m.mu.Lock()
	if err != nil {
		err = listenError(kind, port, err)
		m.errs[kind] = err.Error()
		if len(m.run) == 0 && m.root != nil {
			m.root.Close()
			m.root = nil
		}
		m.mu.Unlock()
		m.changed()
		return err
	}
	delete(m.errs, kind)
	m.run[kind] = r
	m.stopped = ""
	m.lastUse = time.Now()
	if m.idleStop == nil {
		m.idleStop = make(chan struct{})
		go m.idleLoop(m.idleStop)
	}
	m.mu.Unlock()
	m.changed()
	return nil
}

func listenError(kind string, port int, err error) error {
	switch {
	case errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use") || strings.Contains(err.Error(), "Only one usage"):
		return fmt.Errorf("port %d is used by another program - stop it, or choose another port", port)
	case errors.Is(err, syscall.EACCES) || errors.Is(err, os.ErrPermission):
		if runtime.GOOS == "linux" && port < 1024 {
			return fmt.Errorf("port %d needs administrator rights on Linux - choose a port above 1024, or allow it once with: sudo setcap cap_net_bind_service=+ep <path to termwright>", port)
		}
		return fmt.Errorf("port %d cannot be used: %v", port, err)
	case strings.Contains(err.Error(), "assign requested address"):
		return errors.New("this computer no longer has that address - choose another one under Listen on")
	}
	return fmt.Errorf("the %s server cannot start: %w", strings.ToUpper(kind), err)
}

// Stop stops one server.
func (m *Manager) Stop(kind string) {
	m.mu.Lock()
	r, ok := m.run[kind]
	delete(m.run, kind)
	m.stopIfIdleLocked()
	m.mu.Unlock()
	if ok {
		r.stop()
		m.changed()
	}
}

func (m *Manager) stopIfIdleLocked() {
	if len(m.run) == 0 {
		if m.idleStop != nil {
			close(m.idleStop)
			m.idleStop = nil
		}
		if m.root != nil && m.active == 0 {
			m.root.Close()
			m.root = nil
		}
	}
}

// StopAll stops every server.
func (m *Manager) StopAll() { m.stopAll("") }

func (m *Manager) stopAll(why string) {
	m.mu.Lock()
	rs := m.run
	m.run = map[string]runner{}
	m.stopIfIdleLocked()
	if len(rs) > 0 {
		m.stopped = why
	}
	m.mu.Unlock()
	for _, r := range rs {
		r.stop()
	}
	if len(rs) > 0 {
		m.changed()
	}
}

func (m *Manager) idleLoop(done chan struct{}) {
	tk := time.NewTicker(idleTick)
	defer tk.Stop()
	for {
		select {
		case <-done:
			return
		case <-tk.C:
			m.mu.Lock()
			idle := m.cfg.Idle
			stop := idle > 0 && m.active == 0 && time.Since(m.lastUse) > idle
			m.mu.Unlock()
			if stop {
				m.stopAll(fmt.Sprintf("stopped after %d minutes without transfers", int(idle/time.Minute)))
				return
			}
		}
	}
}

// Transfers are the recent transfers, newest first.
func (m *Manager) Transfers() []Transfer {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Transfer, 0, len(m.xfers))
	for i := len(m.xfers) - 1; i >= 0; i-- {
		out = append(out, *m.xfers[i])
	}
	return out
}

// Clear forgets the finished transfers.
func (m *Manager) Clear() {
	m.mu.Lock()
	var keep []*Transfer
	for _, t := range m.xfers {
		if t.State == "running" {
			keep = append(keep, t)
		}
	}
	m.xfers = keep
	m.mu.Unlock()
}

// begin records a transfer; refused ones end at once.
func (m *Manager) begin(server string, peer net.Addr, file string, put bool, size int64) *Transfer {
	h := peer.String()
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	m.mu.Lock()
	m.nextID++
	t := &Transfer{ID: m.nextID, Server: server, Peer: h, File: file, Put: put, State: "running", Size: size, Start: time.Now()}
	m.xfers = append(m.xfers, t)
	if len(m.xfers) > maxTransfers {
		m.xfers = m.xfers[len(m.xfers)-maxTransfers:]
	}
	m.active++
	m.lastUse = time.Now()
	m.mu.Unlock()
	m.tell(t, true)
	return t
}

// progress adds bytes (events at most 4 times a second).
func (m *Manager) progress(t *Transfer, n int64) {
	m.mu.Lock()
	t.Bytes += n
	m.lastUse = time.Now()
	send := time.Since(t.last) > 250*time.Millisecond
	m.mu.Unlock()
	if send {
		m.tell(t, false)
	}
}

// finish ends a transfer: err nil = done.
func (m *Manager) finish(t *Transfer, state string, err error) {
	m.mu.Lock()
	if t.State != "running" {
		m.mu.Unlock()
		return
	}
	t.State, t.End = state, time.Now()
	if err != nil {
		t.Error = err.Error()
	}
	if state == "done" && t.Size < 0 {
		t.Size = t.Bytes
	}
	m.active--
	m.lastUse = time.Now()
	if len(m.run) == 0 && m.active == 0 && m.root != nil {
		m.root.Close()
		m.root = nil
	}
	m.mu.Unlock()
	m.tell(t, true)
}

// refuse records a request that was not served.
func (m *Manager) refuse(server string, peer net.Addr, file string, put bool, why string) {
	t := m.begin(server, peer, file, put, -1)
	m.finish(t, "refused", errors.New(why))
}

func (m *Manager) tell(t *Transfer, force bool) {
	m.mu.Lock()
	if !force && time.Since(t.last) < 250*time.Millisecond {
		m.mu.Unlock()
		return
	}
	t.last = time.Now()
	c := *t
	f := m.OnTransfer
	m.mu.Unlock()
	if f != nil {
		f(c)
	}
}

func (m *Manager) folder() *os.Root {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.root
}

func (m *Manager) canWrite(kind string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if kind == TFTP {
		return m.cfg.TFTPWrite
	}
	return kind == FTP && m.cfg.FTPWrite
}

var errOutside = errors.New("outside the folder")

// CleanName turns a name a device asked for into a path inside the folder
// ("" for the folder itself), or errOutside.
func CleanName(name string) (string, error) {
	n := strings.ReplaceAll(name, `\`, "/")
	if strings.ContainsRune(n, 0) || strings.Contains(n, ":") && runtime.GOOS == "windows" {
		return "", errOutside
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return "", errOutside
		}
	}
	n = strings.TrimPrefix(path.Clean("/"+n), "/")
	if n == "" || n == "." {
		return "", nil
	}
	if !filepath.IsLocal(filepath.FromSlash(n)) {
		return "", errOutside
	}
	return filepath.FromSlash(n), nil
}

// Files lists the files in a folder (three levels deep, at most 500, hidden ones left out).
func Files(dir string) []File {
	var out []File
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || len(out) >= 500 {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if strings.Count(rel, string(filepath.Separator)) >= 2 {
				return filepath.SkipDir
			}
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.Mode().IsRegular() {
			out = append(out, File{Name: filepath.ToSlash(rel), Size: fi.Size()})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}
