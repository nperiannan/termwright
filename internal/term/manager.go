// Package term manages terminal connections (SSH2, Telnet, Serial, local shell)
// and streams their output to the UI.
package term

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/logging"
	"github.com/termwright/termwright/internal/recording"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/smartcard"
)

// Conn is an open terminal connection.
type Conn interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	Close() error
	// Info describes the connection (e.g. negotiated cipher) for the status bar.
	Info() string
}

// Emitter delivers events to the UI.
type Emitter interface {
	Emit(event string, data any)
}

// PromptField is one input in a prompt dialog.
type PromptField struct {
	Label  string `json:"label"`
	Secret bool   `json:"secret"`
	Value  string `json:"value"`
}

// Prompt is a question the backend needs the user to answer.
type Prompt struct {
	ID            string        `json:"id"`
	TabID         string        `json:"tabId"`
	Kind          string        `json:"kind"` // credentials | hostkey | confirm
	Title         string        `json:"title"`
	Message       string        `json:"message"`
	Fields        []PromptField `json:"fields"`
	Choices       []string      `json:"choices"`
	AllowRemember bool          `json:"allowRemember"`
	RememberLabel string        `json:"rememberLabel,omitempty"` // text of the remember box (default "Save password (OS keychain)")
	Danger        bool          `json:"danger"`
}

// PromptReply is the user's answer.
type PromptReply struct {
	OK       bool     `json:"ok"`
	Values   []string `json:"values"`
	Choice   string   `json:"choice"`
	Remember bool     `json:"remember"`
}

// ErrCancelled is returned when the user cancels a prompt.
var ErrCancelled = errors.New("cancelled by user")

// Status values sent to the UI.
const (
	StateIdle         = "idle"
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StateDisconnected = "disconnected"
	StateError        = "error"
)

// TabInfo describes a tab.
type TabInfo struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	State     string          `json:"state"`
	Info      string          `json:"info"`
	Session   session.Session `json:"session"`
	LogFile   string          `json:"logFile"`
	Connected time.Time       `json:"connected"`
}

// Tab is one terminal tab.
type Tab struct {
	mu        sync.Mutex
	id        string
	sess      session.Session
	conn      Conn
	state     string
	info      string
	logger    *logging.Logger
	logon     *logonRunner
	cancel    context.CancelFunc
	cols      int
	rows      int
	connected time.Time
	gen       int
	retries   int
	triggers  *triggerRunner
	subs      map[int]func([]byte)
	subSeq    int
	capture   func([]byte) // when set, output goes only here (file transfers)
	recorder  *recorder
	cast      *recording.Writer // session recording (.cast)
	zwin      []byte            // tail of recent output for ZMODEM auto-detection
	zpending  bool              // waiting for the user to pick files for "rz"
	seq       int64             // creation order (scripts address tabs by index)
	alertBuf  string            // partial output line for alert matching
	logonPass string            // password typed for \w during this connection
	alertAt   map[int]time.Time
	lastMsg   string // last status message (errors of background connections)
}

// subscribe registers fn to receive raw output of a tab; call the returned
// function to unsubscribe.
func (t *Tab) subscribe(fn func([]byte)) func() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.subs == nil {
		t.subs = map[int]func([]byte){}
	}
	t.subSeq++
	id := t.subSeq
	t.subs[id] = fn
	return func() {
		t.mu.Lock()
		delete(t.subs, id)
		t.mu.Unlock()
	}
}

// Manager owns all tabs.
type Manager struct {
	mu      sync.Mutex
	tabs    map[string]*Tab
	seq     int64
	alertMu sync.RWMutex
	alerts  []alertRule
	sftps   map[string]*sftpSess
	xfers   map[string]context.CancelFunc
	jobs    map[string]xferJob
	scripts map[string]*scriptRun
	emit    Emitter
	prompts *Prompts
	quiet   sync.Map // background tabs (Capture): nothing may be asked
	dataDir string
	logDir  func() string
	// OnPasswordUsed is called after a successful login with a password the
	// user asked to remember.
	OnPasswordUsed func(sessionPath, password string)
	// SavedPassword returns a stored password for a session path.
	SavedPassword func(sessionPath string) (string, bool)
	// DevicePassword returns the password saved for a device type.
	DevicePassword func(name string) (string, bool)
	// OnDevicePasswordTyped saves a password typed at a logon prompt for a
	// device type ("" = the device has no password).
	OnDevicePasswordTyped func(name, password string)
	// DownloadDir returns where received files (ZMODEM) are saved.
	DownloadDir func() string
	// Credential returns the user name and password of a named credential.
	Credential func(name string) (user, password string, ok bool)
	// Clipboard returns the clipboard text (logon action \v).
	Clipboard func() string
	// DeviceType looks up a device type (login profile) by name.
	DeviceType func(name string) (session.DeviceType, bool)
	// PythonPath returns the configured Python interpreter ("" = find python3).
	PythonPath func() string
	// KeyPassphrase returns a saved passphrase for a private key file ("" if none).
	KeyPassphrase func(path string) string
	// ScreenSnapshot returns the visible screen of a tab as the user sees it
	// (from the terminal view); nil when no view is available.
	ScreenSnapshot func(tabID string) *ScreenSnap
	// OnKeyPassphraseUsed is called after a key was unlocked with a typed
	// passphrase the user asked to remember.
	OnKeyPassphraseUsed func(path, passphrase string)
	// SmartCards signs with keys on smart cards (PKCS#11).
	SmartCards *smartcard.Manager
	// SmartCardLibraries are the PKCS#11 libraries to use ("" = the usual places).
	SmartCardLibraries func() []string
	// RecordDir is the folder session recordings are saved in.
	RecordDir func() string
	// RecordAll reports whether every connection of a session is recorded
	// (global or folder setting); the session's own setting is checked too.
	RecordAll func(s session.Session) bool
	// LoadSession loads a saved session (used for jump hosts).
	LoadSession func(path string) (session.Session, error)
}

// NewManager creates a manager.
func NewManager(emit Emitter, dataDir string, logDir func() string) *Manager {
	m := &Manager{
		tabs: map[string]*Tab{}, emit: emit, dataDir: dataDir, logDir: logDir,
		sftps: map[string]*sftpSess{}, xfers: map[string]context.CancelFunc{}, jobs: map[string]xferJob{}, scripts: map[string]*scriptRun{},
		prompts: NewPrompts(emit),
	}
	m.prompts.quiet = func(tabID string) bool { _, ok := m.quiet.Load(tabID); return ok }
	return m
}

// lastStatus returns the last status message of a tab.
func (m *Manager) lastStatus(id string) string {
	t, err := m.get(id)
	if err != nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastMsg
}

// Prompts returns the prompt broker.
func (m *Manager) Prompts() *Prompts { return m.prompts }

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Create registers a new tab for a session (does not connect yet).
func (m *Manager) Create(s session.Session) TabInfo {
	t := &Tab{id: newID(), sess: s, state: StateIdle, cols: s.Cols, rows: s.Rows}
	m.mu.Lock()
	m.seq++
	t.seq = m.seq
	m.tabs[t.id] = t
	m.mu.Unlock()
	return t.snapshot()
}

func (t *Tab) snapshot() TabInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	ti := TabInfo{ID: t.id, Title: title(t.sess), State: t.state, Info: t.info, Session: t.sess, Connected: t.connected}
	if t.logger != nil {
		ti.LogFile = t.logger.Path()
	}
	return ti
}

func title(s session.Session) string {
	if s.Name != "" {
		return s.Name
	}
	switch s.Protocol {
	case session.ProtoSerial:
		return s.ComPort
	case session.ProtoLocal:
		return "Local Shell"
	}
	if s.Username != "" {
		return s.Username + "@" + s.Hostname
	}
	return s.Hostname
}

func (m *Manager) get(id string) (*Tab, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tabs[id]
	if !ok {
		return nil, fmt.Errorf("unknown tab %s", id)
	}
	return t, nil
}

// Info returns a tab snapshot.
func (m *Manager) Info(id string) (TabInfo, error) {
	t, err := m.get(id)
	if err != nil {
		return TabInfo{}, err
	}
	return t.snapshot(), nil
}

func (m *Manager) status(t *Tab, state, msg string) {
	t.mu.Lock()
	t.state = state
	t.lastMsg = msg
	info := t.info
	t.mu.Unlock()
	m.emit.Emit("term:status:"+t.id, map[string]string{"state": state, "message": msg, "info": info})
}

// Start connects the tab. It returns immediately; progress is reported via
// status events. Calling Start on a disconnected tab reconnects it.
func (m *Manager) Start(id string, cols, rows int) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	t.mu.Lock()
	if t.state == StateConnecting || t.state == StateConnected {
		t.mu.Unlock()
		return nil
	}
	if cols > 0 && rows > 0 {
		t.cols, t.rows = cols, rows
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.gen++
	gen := t.gen
	t.mu.Unlock()
	go m.run(ctx, t, gen)
	return nil
}

func (m *Manager) run(ctx context.Context, t *Tab, gen int) {
	t.mu.Lock()
	if dt, ok := m.deviceOf(t.sess); ok && dt.Username != "" && (!t.sess.OwnLogin || t.sess.Username == "") {
		t.sess.Username = dt.Username // the device type's login unless the session has its own
	}
	s, cols, rows, retrying := t.sess, t.cols, t.rows, t.retries > 0
	t.mu.Unlock()
	m.status(t, StateConnecting, "Connecting to "+title(s)+"...")
	dial := func(ctx context.Context, addr string) (net.Conn, string, error) { return m.dialNet(ctx, t, s, addr, 0) }
	var conn Conn
	var err error
	switch s.Protocol {
	case session.ProtoSSH2, "SSH":
		conn, err = m.dialSSH(ctx, t, s, cols, rows)
	case session.ProtoTelnet:
		conn, err = dialTelnet(ctx, s, cols, rows, dial)
	case session.ProtoRaw:
		conn, err = dialRaw(ctx, s, dial)
	case session.ProtoSerial:
		conn, err = openSerial(s)
	case session.ProtoLocal:
		conn, err = startLocal(s, cols, rows)
	default:
		err = fmt.Errorf("protocol %q is not supported yet", s.Protocol)
	}
	if err != nil {
		msg := err.Error()
		if errors.Is(err, ErrCancelled) || ctx.Err() != nil {
			m.status(t, StateError, "Connection cancelled")
			return
		}
		m.status(t, StateError, msg)
		if retrying && !strings.Contains(msg, "unable to authenticate") {
			m.retry(ctx, t, gen)
		}
		return
	}
	t.mu.Lock()
	if t.gen != gen || ctx.Err() != nil {
		t.mu.Unlock()
		_ = conn.Close()
		return
	}
	t.conn = conn
	t.info = conn.Info()
	t.connected = time.Now()
	t.retries = 0
	t.logon = nil
	actions := s.LogonActions
	initialCR := s.InitialCR
	if dt, ok := m.deviceOf(s); ok {
		proto := s.Protocol
		if s.ConsoleServer != "" {
			proto = session.ProtoConsole // the device's own login prompts appear at the console, even over SSH
		}
		if _, bg := m.quiet.Load(t.id); bg {
			// background capture: it runs the after-login steps itself, one at a prompt
			actions = append(dt.LoginOnly(proto), actions...)
		} else {
			actions = append(dt.Steps(proto), actions...)
		}
		initialCR = initialCR || dt.InitialCR
	}
	if len(actions) > 0 && (s.AutomateLogon || s.DeviceType != "") {
		t.logon = newLogonRunner(actions)
	}
	t.alertBuf, t.logonPass = "", ""
	t.triggers = nil
	if len(s.Triggers) > 0 {
		tr, terr := newTriggerRunner(s.Triggers)
		if terr != nil {
			m.emit.Emit("toast", map[string]string{"level": "error", "message": "Trigger: " + terr.Error()})
		} else {
			t.triggers = tr
		}
	}
	t.mu.Unlock()
	if s.LogOnConnect {
		if _, err := m.StartLog(t.id, "", s.LogAppend, s.LogRaw); err != nil {
			m.emit.Emit("toast", map[string]string{"level": "error", "message": "Log: " + err.Error()})
		}
	}
	if s.RecordSession || (m.RecordAll != nil && m.RecordAll(s)) {
		if _, err := m.StartSessionRecording(t.id); err != nil {
			m.emit.Emit("toast", map[string]string{"level": "error", "message": "Recording: " + err.Error()})
		}
	}
	m.status(t, StateConnected, "Connected")
	if initialCR {
		_, _ = conn.Write([]byte("\r"))
	}
	if s.UseLogonScript && s.LogonScript != "" {
		if ext := strings.ToLower(filepath.Ext(s.LogonScript)); ext != ".js" && ext != ".mjs" && ext != ".py" && ext != ".vbs" {
			m.emit.Emit("toast", map[string]string{"level": "warn", "message": "Logon script " + filepath.Base(s.LogonScript) + " skipped: Termwright runs Python (.py), VBScript (.vbs, Windows) and JavaScript (.js) scripts"})
		} else if _, err := m.RunScriptFile(t.id, s.LogonScript, s.LogonArgs); err != nil {
			m.emit.Emit("toast", map[string]string{"level": "error", "message": "Logon script: " + err.Error()})
		}
	}
	if t.logon != nil {
		m.runLogon(t, conn, t.logon.feed(nil))
	}
	m.pump(t, conn)
	_ = m.StopSessionRecording(t.id) // one recording per connection
	t.mu.Lock()
	same := t.conn == conn
	if same {
		t.conn = nil
	}
	t.mu.Unlock()
	_ = conn.Close()
	if same {
		m.status(t, StateDisconnected, "Disconnected")
		m.retry(ctx, t, gen)
	}
}

// retry schedules an automatic reconnect for sessions with Auto Reconnect.
func (m *Manager) retry(ctx context.Context, t *Tab, gen int) {
	t.mu.Lock()
	if !t.sess.AutoReconnect || t.gen != gen || ctx.Err() != nil || t.retries >= 20 {
		t.mu.Unlock()
		return
	}
	t.retries++
	n := t.retries
	t.mu.Unlock()
	delays := []int{2, 5, 10, 20, 30}
	d := delays[len(delays)-1]
	if n-1 < len(delays) {
		d = delays[n-1]
	}
	m.status(t, StateConnecting, fmt.Sprintf("Reconnecting in %ds (attempt %d)...", d, n))
	go func() {
		select {
		case <-time.After(time.Duration(d) * time.Second):
		case <-ctx.Done():
			return
		}
		t.mu.Lock()
		ok := t.gen == gen
		t.mu.Unlock()
		if ok {
			m.run(ctx, t, gen)
		}
	}()
}

// runLogon sends the given logon actions in order (pauses run in the
// background so output keeps flowing).
func (m *Manager) runLogon(t *Tab, conn Conn, actions []session.LogonAction) {
	if len(actions) == 0 {
		return
	}
	t.mu.Lock()
	sess := t.sess
	t.mu.Unlock()
	var parts []sendPart
	for _, a := range actions {
		user := sess.Username
		pass, havePass := m.loginPassword(sess)
		if a.Credential != "" && m.Credential != nil {
			if u, p, ok := m.Credential(a.Credential); ok {
				user, pass, havePass = u, p, true
			} else {
				m.emit.Emit("toast", map[string]string{"level": "error", "message": "Logon action: credential \"" + a.Credential + "\" not found"})
			}
		}
		if user == "" && strings.Contains(a.Send, `\s`) {
			user = m.askLogon(t, "Username", false, "")
			sess.Username = user
		}
		if !havePass && strings.Contains(a.Send, `\w`) {
			if t.logonPass == "" {
				t.logonPass = m.askLogon(t, "Password", true, user)
			}
			pass = t.logonPass
		}
		parts = append(parts, expandSend(a.Send, a.TrailingCR, user, pass, m.Clipboard)...)
	}
	hasPause := false
	for _, p := range parts {
		hasPause = hasPause || p.pause
	}
	send := func() {
		for _, p := range parts {
			if p.pause {
				time.Sleep(time.Second)
				continue
			}
			_, _ = conn.Write([]byte(p.text))
		}
	}
	if hasPause {
		go send()
	} else {
		send()
	}
}

// pump copies connection output to the UI, log and logon automation.
func (m *Manager) pump(t *Tab, conn Conn) {
	chunks := make(chan []byte, 256)
	go func() {
		defer close(chunks)
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				c := make([]byte, n)
				copy(c, buf[:n])
				chunks <- c
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					t.mu.Lock()
					t.info = err.Error()
					t.mu.Unlock()
				}
				return
			}
		}
	}()
	event := "term:data:" + t.id
	var batch []byte
	for c := range chunks {
		batch = append(batch[:0], c...)
	drain:
		for len(batch) < 256*1024 {
			select {
			case more, ok := <-chunks:
				if !ok {
					break drain
				}
				batch = append(batch, more...)
			default:
				break drain
			}
		}
		t.mu.Lock()
		lg, lr, tr, capFn, rec, cw := t.logger, t.logon, t.triggers, t.capture, t.recorder, t.cast
		subs := make([]func([]byte), 0, len(t.subs))
		for _, f := range t.subs {
			subs = append(subs, f)
		}
		t.mu.Unlock()
		if capFn != nil {
			capFn(append([]byte(nil), batch...))
			continue
		}
		if idx, kind := m.zdetect(t, batch); idx >= 0 {
			if idx > 0 {
				m.emit.Emit(event, base64.StdEncoding.EncodeToString(batch[:idx]))
			}
			rest := append([]byte(nil), batch[idx:]...)
			if kind == zRQINIT {
				m.startZReceive(t, rest)
			} else {
				m.emit.Emit("zmodem:send-request", t.id)
			}
			continue
		}
		for _, f := range subs {
			f(batch)
		}
		if rec != nil {
			rec.output(batch)
		}
		if cw != nil {
			cw.Output(batch)
		}
		if tr != nil {
			if out := tr.feed(batch); out != nil {
				_, _ = conn.Write(out)
			}
		}
		if lg != nil {
			lg.Write(batch)
		}
		m.emit.Emit(event, base64.StdEncoding.EncodeToString(batch))
		m.checkAlerts(t, batch)
		if lr != nil {
			m.runLogon(t, conn, lr.feed(batch))
			if lr.done() {
				t.mu.Lock()
				t.logon = nil
				t.mu.Unlock()
			}
		}
	}
}

// Write sends user input to a tab.
func (m *Manager) Write(id string, data []byte) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	t.mu.Lock()
	c, rec := t.conn, t.recorder
	t.mu.Unlock()
	if c == nil {
		return nil
	}
	if rec != nil {
		rec.input(data)
	}
	_, err = c.Write(data)
	return err
}

// SendBreak sends a serial/telnet/SSH break signal on a tab.
func (m *Manager) SendBreak(id string) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	t.mu.Lock()
	c := t.conn
	t.mu.Unlock()
	b, ok := c.(interface{ SendBreak() error })
	if !ok || c == nil {
		return errors.New("break is not supported for this connection")
	}
	return b.SendBreak()
}

// rawWrite writes to the connection without recording.
func (t *Tab) rawWrite(p []byte) error {
	t.mu.Lock()
	c := t.conn
	t.mu.Unlock()
	if c == nil {
		return errors.New("not connected")
	}
	_, err := c.Write(p)
	return err
}

func (t *Tab) isConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn != nil && t.state == StateConnected
}

// Resize changes the terminal size.
func (m *Manager) Resize(id string, cols, rows int) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	if cols <= 0 || rows <= 0 {
		return nil
	}
	t.mu.Lock()
	changed := t.cols != cols || t.rows != rows
	t.cols, t.rows = cols, rows
	c, cw := t.conn, t.cast
	t.mu.Unlock()
	if c == nil {
		return nil
	}
	if cw != nil && changed {
		cw.Resize(cols, rows)
	}
	return c.Resize(cols, rows)
}

// Disconnect closes the connection but keeps the tab.
func (m *Manager) Disconnect(id string) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	t.mu.Lock()
	c, cancel := t.conn, t.cancel
	t.conn = nil
	t.gen++
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.prompts.CancelTab(id)
	m.closeTabExtras(id)
	_ = m.StopSessionRecording(id)
	if c != nil {
		_ = c.Close()
	}
	m.status(t, StateDisconnected, "Disconnected")
	return nil
}

// Close disconnects and removes a tab.
func (m *Manager) Close(id string) error {
	_ = m.Disconnect(id)
	_ = m.StopLog(id)
	m.mu.Lock()
	delete(m.tabs, id)
	m.mu.Unlock()
	return nil
}

// CloseAll closes every tab (used on shutdown).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.tabs))
	for id := range m.tabs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Close(id)
	}
}

// StartLog starts logging a tab. An empty path uses the session's log file
// pattern or a default name in the log directory.
func (m *Manager) StartLog(id, path string, appendMode, raw bool) (string, error) {
	t, err := m.get(id)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	s := t.sess
	old := t.logger
	t.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	vars := logging.Vars{Host: s.Hostname, Session: s.Path, Port: s.Port, User: s.Username, Proto: s.Protocol}
	if s.Protocol == session.ProtoSerial {
		vars.Host = s.ComPort
	}
	if vars.Session == "" {
		vars.Session = title(s)
	}
	if path == "" {
		path = s.LogFile
	}
	if path == "" {
		path = filepath.Join(m.logDir(), "%S-%Y%M%D-%h%m%s.log")
	}
	path = logging.Expand(path, vars, time.Now())
	if strings.HasPrefix(path, "~") {
		if h, err := userHome(); err == nil {
			path = filepath.Join(h, path[1:])
		}
	}
	lg, err := logging.Open(logging.Options{Path: path, Append: appendMode, Raw: raw, LinePrefix: s.LogEachLine, Vars: vars})
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	t.logger = lg
	t.mu.Unlock()
	m.emit.Emit("term:log:"+id, path)
	return path, nil
}

// StopLog stops logging.
func (m *Manager) StopLog(id string) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	t.mu.Lock()
	lg := t.logger
	t.logger = nil
	t.mu.Unlock()
	if lg != nil {
		m.emit.Emit("term:log:"+id, "")
		return lg.Close()
	}
	return nil
}

// UpdateSession replaces the session settings used for the next (re)connect.
func (m *Manager) UpdateSession(id string, s session.Session) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.sess = s
	t.mu.Unlock()
	return nil
}

// deviceOf returns the session's device type, if it has one.
func (m *Manager) deviceOf(s session.Session) (session.DeviceType, bool) {
	if s.DeviceType == "" || m.DeviceType == nil {
		return session.DeviceType{}, false
	}
	return m.DeviceType(s.DeviceType)
}

// askLogon asks for a username or password that a logon step needs. A
// password can be remembered: for the device type when the session uses the
// device type's login (an empty answer then means "no password"), otherwise
// for the session.
func (m *Manager) askLogon(t *Tab, label string, secret bool, user string) string {
	t.mu.Lock()
	s := t.sess
	t.mu.Unlock()
	target := s.Hostname + s.ComPort
	if user != "" {
		target = user + "@" + target
	}
	p := Prompt{TabID: t.id, Kind: "credentials", Title: "Login - " + title(s),
		Message: label + " for " + target, Fields: []PromptField{{Label: label, Secret: secret}}}
	forDevice := secret && s.DeviceType != "" && !s.OwnLogin && m.OnDevicePasswordTyped != nil
	switch {
	case forDevice:
		p.AllowRemember = true
		p.RememberLabel = "Remember for device type " + s.DeviceType + " (leave the password empty if the device has none)"
	case secret && s.Path != "" && m.OnPasswordUsed != nil:
		p.AllowRemember = true
	}
	r, err := m.prompts.Ask(context.Background(), p)
	if err != nil {
		if errors.Is(err, ErrNeedsInput) {
			// background connection: give up instead of sending an empty answer
			_ = m.Disconnect(t.id)
			m.status(t, StateError, "no saved "+strings.ToLower(label)+" for "+target)
		}
		return ""
	}
	if r.Remember {
		if forDevice {
			m.OnDevicePasswordTyped(s.DeviceType, r.Values[0])
		} else if r.Values[0] != "" {
			m.OnPasswordUsed(s.Path, r.Values[0])
		}
	}
	return r.Values[0]
}

// loginPassword returns the password for a session: the device type's when
// the session inherits its login, otherwise the session's saved password.
func (m *Manager) loginPassword(s session.Session) (string, bool) {
	if s.DeviceType != "" && !s.OwnLogin && m.DevicePassword != nil {
		if pw, ok := m.DevicePassword(s.DeviceType); ok {
			return pw, true
		}
	}
	if s.Path != "" && m.SavedPassword != nil {
		return m.SavedPassword(s.Path)
	}
	return "", false
}

// ScreenSnap is the visible terminal screen: one string per row (exactly as
// rendered, trailing blanks kept) and the 1-based cursor position.
type ScreenSnap struct {
	Lines []string `json:"lines"`
	Row   int      `json:"row"`
	Col   int      `json:"col"`
	Rows  int      `json:"rows"`
	Cols  int      `json:"cols"`
}
