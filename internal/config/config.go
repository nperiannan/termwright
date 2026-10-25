// Package config manages Termwright's data directory and global options.
package config

import (
	"encoding/json"
	"github.com/termwright/termwright/internal/session"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Button is a button-bar entry that sends text to the active (or all) tabs.
type Button struct {
	Label  string `json:"label"`
	Send   string `json:"send"` // supports \r \n \t \e escapes
	ToAll  bool   `json:"toAll"`
	Action string `json:"action"` // optional app action, e.g. "sftp-tab"
	Script string `json:"script"` // optional .js file to run
}

// Keyword is one highlighted word or pattern.
type Keyword struct {
	Text          string `json:"text"`
	Regex         bool   `json:"regex"`
	CaseSensitive bool   `json:"caseSensitive"`
	Color         string `json:"color"` // CSS color for the background
}

// KeyBinding maps a key combination to text to send, an app action or a script.
type KeyBinding struct {
	Keys   string `json:"keys"`   // e.g. "Ctrl+Shift+F1", "Alt+1", "F5"
	Send   string `json:"send"`   // text with \r \n \t \e \xHH escapes
	Action string `json:"action"` // menu action name, e.g. "reconnect"
	Script string `json:"script"` // path of a .js file to run
}

// Credential is a named user name; its password is kept in the OS keychain.
type Credential struct {
	Name     string `json:"name"`
	Username string `json:"username"`
}

// ColorScheme is a user-defined terminal color scheme (CSS colors keyed by
// background, foreground, cursor, selectionBackground, black ... brightWhite).
type ColorScheme struct {
	Name   string            `json:"name"`
	Colors map[string]string `json:"colors"`
}

// KeywordSet is a named list of keywords a session can use.
type KeywordSet struct {
	Name     string    `json:"name"`
	Keywords []Keyword `json:"keywords"`
}

// Settings are the global options.
type Settings struct {
	SessionsDir         string               `json:"sessionsDir"`
	LogDir              string               `json:"logDir"`
	DownloadDir         string               `json:"downloadDir"`
	FontFamily          string               `json:"fontFamily"`
	FontSize            int                  `json:"fontSize"`
	Theme               string               `json:"theme"`               // ui theme: dark | light | system
	TerminalScheme      string               `json:"terminalScheme"`      // scheme name or "Auto" (follow the UI theme)
	ForceTermScheme     bool                 `json:"forceTerminalScheme"` // global scheme wins over session schemes
	CursorStyle         string               `json:"cursorStyle"`         // block | underline | bar
	CursorBlink         bool                 `json:"cursorBlink"`
	Scrollback          int                  `json:"scrollback"`
	CopyOnSelect        bool                 `json:"copyOnSelect"`
	RightClickPaste     bool                 `json:"rightClickPaste"`
	ConfirmClose        bool                 `json:"confirmClose"`
	ShowButtonBar       bool                 `json:"showButtonBar"`
	ShowCommandWin      bool                 `json:"showCommandWindow"`
	ShowSessionTree     bool                 `json:"showSessionManager"`
	PasteLineDelayMs    int                  `json:"pasteLineDelayMs"`
	Buttons             []Button             `json:"buttons"`
	RecentSessions      []string             `json:"recentSessions"`
	KeywordSets         []KeywordSet         `json:"keywordSets"`
	DefaultKeywordSet   string               `json:"defaultKeywordSet"`
	ShowConnectBar      bool                 `json:"showConnectBar"`
	InWindowMenu        bool                 `json:"inWindowMenu"` // menu bar inside the window (default on macOS)
	Credentials         []Credential         `json:"credentials"`
	MasterHash          string               `json:"masterHash"` // scrypt verifier; empty = no master password
	KeyMap              []KeyBinding         `json:"keyMap"`
	CustomSchemes       []ColorScheme        `json:"customSchemes"`
	AccentColor         string               `json:"accentColor"`
	RestoreTabs         bool                 `json:"restoreTabs"`
	OpenTabs            []string             `json:"openTabs"`
	Bookmarks           []Bookmark           `json:"bookmarks"`
	SFTPCwdAuto         bool                 `json:"sftpCwdAuto"`              // turn on folder reporting in the shell when an SFTP tab opens from it
	SFTPFollowTerminal  bool                 `json:"sftpFollowTerminal"`       // SFTP tabs follow the terminal folder
	SessionManagerRight bool                 `json:"sessionManagerRight"`      // Session Manager on the right side of the window
	VaultAddr           string               `json:"vaultAddr,omitempty"`      // HashiCorp Vault URL for vault:// references ("" = $VAULT_ADDR)
	KeyPassphrases      []string             `json:"keyPassphrases,omitempty"` // key files whose passphrase is in the OS keychain (no secrets here)
	UpdateChannel       string               `json:"updateChannel"`            // stable | nightly | off
	UpdateRepo          string               `json:"updateRepo"`               // GitHub owner/name
	UpdateOnStart       bool                 `json:"updateOnStart"`
	PythonPath          string               `json:"pythonPath"` // interpreter for .py scripts ("" = find python3)
	DeviceTypes         []session.DeviceType `json:"deviceTypes"`
	Alerts              []Alert              `json:"alerts"`
	ConfirmPaste        bool                 `json:"confirmPaste"` // ask before pasting several lines
	Snippets            []Snippet            `json:"snippets"`
	Workspaces          []Workspace          `json:"workspaces"`
	FolderDefaults      []FolderDefault      `json:"folderDefaults"`
	Window              WindowState          `json:"window"`
}

// WindowState remembers the main window size and position.
type WindowState struct {
	Width, Height int
	X, Y          int
	Maximised     bool
}

// Snippet is a saved command (or several lines) in the command library.
// {{name}} or {{name:default}} placeholders are asked for when it is sent.
type Snippet struct {
	Name        string `json:"name"`
	Folder      string `json:"folder"`
	Text        string `json:"text"`
	Description string `json:"description"`
}

// Workspace is a saved set of tabs and the layout they were shown in.
type Workspace struct {
	Name   string         `json:"name"`
	Layout string         `json:"layout"`
	Tabs   []WorkspaceTab `json:"tabs"`
}

// WorkspaceTab is one saved session in a workspace.
type WorkspaceTab struct {
	Session string `json:"session"`
	Kind    string `json:"kind"` // terminal | sftp
}

// FolderDefault holds values inherited by every session in a folder (and its
// sub-folders) when the session leaves them empty.
type FolderDefault struct {
	Folder       string `json:"folder"`
	Username     string `json:"username"`
	DeviceType   string `json:"deviceType"`
	JumpSession  string `json:"jumpSession"`
	IdentityFile string `json:"identityFile"`
	KeywordSet   string `json:"keywordSet"`
	ColorScheme  string `json:"colorScheme"`
}

// Alert notifies when output in any tab matches a pattern.
type Alert struct {
	Pattern       string `json:"pattern"`
	Regex         bool   `json:"regex"`
	CaseSensitive bool   `json:"caseSensitive"`
	Enabled       bool   `json:"enabled"`
	Sound         bool   `json:"sound"`
	Desktop       bool   `json:"desktop"` // operating-system notification
}

// Bookmark is a saved SFTP folder. Session is the session path (or host for
// unsaved sessions) for remote bookmarks and empty for local ones.
type Bookmark struct {
	Name    string `json:"name"`
	Session string `json:"session"`
	Side    string `json:"side"` // local | remote
	Path    string `json:"path"`
}

// Defaults returns the factory settings.
func Defaults(dataDir string) Settings {
	return Settings{
		SessionsDir:     filepath.Join(dataDir, "Config", "Sessions"),
		LogDir:          filepath.Join(dataDir, "Logs"),
		DownloadDir:     defaultDownloads(),
		FontFamily:      "Menlo, Consolas, 'DejaVu Sans Mono', 'Courier New', monospace",
		FontSize:        14,
		Theme:           "dark",
		TerminalScheme:  "Auto",
		CursorStyle:     "block",
		CursorBlink:     true,
		Scrollback:      10000,
		CopyOnSelect:    true,
		RightClickPaste: true,
		ConfirmClose:    true,
		ShowButtonBar:   true,
		ShowCommandWin:  false,
		ShowSessionTree: true,
		Buttons: []Button{
			{Label: "show version", Send: `show version\r`},
			{Label: "Ctrl+C", Send: `\x03`},
		},
		RecentSessions: []string{},
		ShowConnectBar: true,
		InWindowMenu:   runtime.GOOS == "darwin",
		KeywordSets: []KeywordSet{{
			Name: "Network",
			Keywords: []Keyword{
				{Text: `\b(error|failed|failure|down|denied|critical|invalid)\b`, Regex: true, Color: "#7a1f1f"},
				{Text: `\b(warning|warn|disabled|blocked)\b`, Regex: true, Color: "#6b5310"},
				{Text: `\b(up|ok|success|enabled|active|forwarding)\b`, Regex: true, Color: "#1f5a2c"},
				{Text: `\b\d{1,3}(\.\d{1,3}){3}(/\d{1,2})?\b`, Regex: true, Color: "#1f3f6b"},
			},
		}},
		DefaultKeywordSet:  "Network",
		Bookmarks:          []Bookmark{},
		UpdateChannel:      "stable",
		SFTPCwdAuto:        true,
		SFTPFollowTerminal: true,
		UpdateRepo:         PublicRepo,
		UpdateOnStart:      true,
		DeviceTypes:        session.BuiltinDeviceTypes(),
		Snippets: []Snippet{
			{Name: "Show VLAN", Folder: "Examples", Text: "show vlan {{vlan_id:10}}", Description: "Asks for the VLAN id"},
			{Name: "Interface status", Folder: "Examples", Text: "show interfaces gigabitEthernet {{port:1/1}}"},
			{Name: "Save config", Folder: "Examples", Text: "save config", Description: "VOSS / EXOS"},
		},
		ConfirmPaste: true,
		Alerts: []Alert{
			{Pattern: `%[A-Z0-9_]+-[0-2]-`, Regex: true, CaseSensitive: true, Sound: true, Desktop: true},
			{Pattern: `\b(CRITICAL|link down|Link Down)\b`, Regex: true, CaseSensitive: true, Sound: true, Desktop: true},
		},
	}
}

func defaultDownloads() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "Downloads")
	}
	return ""
}

// Manager loads and saves settings.
type Manager struct {
	mu      sync.Mutex
	dataDir string
	cur     Settings
}

// DataDir returns the per-user Termwright directory.
func DataDir() (string, error) {
	// Portable mode: a "termwright-data" folder next to the program (or next
	// to Termwright.app on macOS) holds everything; nothing goes to the profile.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		cands := []string{filepath.Join(dir, "termwright-data")}
		if strings.HasSuffix(dir, filepath.Join(".app", "Contents", "MacOS")) {
			cands = append(cands, filepath.Join(dir, "..", "..", "..", "termwright-data"))
		}
		for _, c := range cands {
			if isDir(c) {
				return filepath.Clean(c), nil
			}
		}
	}
	for _, env := range []string{"TERMWRIGHT_HOME"} {
		if d := os.Getenv(env); d != "" {
			return d, os.MkdirAll(d, 0o700)
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, "Termwright")
	if _, err := os.Stat(d); os.IsNotExist(err) {
		// Installs from before the rename: move the old folder (settings,
		// sessions, logs, known_hosts) to the new name. If that is not
		// possible, keep using the old folder.
		if old := filepath.Join(base, "OpenCRT"); isDir(old) {
			if err := os.Rename(old, d); err != nil {
				return old, nil
			}
			movedFrom = old
		}
	}
	return d, os.MkdirAll(d, 0o700)
}

// movedFrom is the pre-rename data folder when DataDir just moved it; Load
// then rewrites settings paths that pointed inside it.
var movedFrom string

// fixMovedPaths updates absolute paths that pointed into the old data folder.
func fixMovedPaths(s *Settings, oldDir, newDir string) bool {
	changed := false
	for _, p := range []*string{&s.SessionsDir, &s.LogDir, &s.DownloadDir} {
		if *p == oldDir || strings.HasPrefix(*p, oldDir+string(filepath.Separator)) {
			*p = newDir + strings.TrimPrefix(*p, oldDir)
			changed = true
		}
	}
	return changed
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Load reads settings from dataDir/settings.json.
func Load(dataDir string) (*Manager, error) {
	m := &Manager{dataDir: dataDir, cur: Defaults(dataDir)}
	data, err := os.ReadFile(m.file())
	if err == nil {
		if err := json.Unmarshal(data, &m.cur); err != nil {
			return nil, err
		}
		if movedFrom != "" && fixMovedPaths(&m.cur, movedFrom, dataDir) {
			movedFrom = ""
			_ = m.Save(m.cur)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if migrateUpdateRepo(&m.cur) {
		_ = m.Save(m.cur)
	}
	if m.cur.SessionsDir == "" {
		m.cur.SessionsDir = Defaults(dataDir).SessionsDir
	}
	if m.cur.LogDir == "" {
		m.cur.LogDir = Defaults(dataDir).LogDir
	}
	return m, nil
}

// PublicRepo is the public Termwright repository that releases are published to.
const PublicRepo = "nperiannan/termwright"

// migrateUpdateRepo points settings saved by pre-release builds (old name, or
// the former private repository, both with the nightly channel as default) to the
// public repository and its stable channel. Forks set by the user are kept.
func migrateUpdateRepo(s *Settings) bool {
	switch strings.ToLower(strings.TrimSpace(s.UpdateRepo)) {
	case "", "nperiannan/opencrt", "nperiannan/termwright-private":
		if s.UpdateRepo != "" && s.UpdateChannel == "nightly" {
			s.UpdateChannel = "stable"
		}
		s.UpdateRepo = PublicRepo
		return true
	}
	return false
}

func (m *Manager) file() string { return filepath.Join(m.dataDir, "settings.json") }

// DataDir returns the directory settings live in.
func (m *Manager) DataDir() string { return m.dataDir }

// Get returns a copy of the settings.
func (m *Manager) Get() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cur
}

// Save persists the settings.
func (m *Manager) Save(s Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.Buttons == nil {
		s.Buttons = []Button{}
	}
	if s.RecentSessions == nil {
		s.RecentSessions = []string{}
	}
	if s.Credentials == nil {
		s.Credentials = []Credential{}
	}
	if s.CustomSchemes == nil {
		s.CustomSchemes = []ColorScheme{}
	}
	if s.Snippets == nil {
		s.Snippets = []Snippet{}
	}
	if s.Workspaces == nil {
		s.Workspaces = []Workspace{}
	}
	if s.FolderDefaults == nil {
		s.FolderDefaults = []FolderDefault{}
	}
	if s.DeviceTypes == nil {
		s.DeviceTypes = []session.DeviceType{}
	}
	if s.Alerts == nil {
		s.Alerts = []Alert{}
	}
	if s.Bookmarks == nil {
		s.Bookmarks = []Bookmark{}
	}
	if s.OpenTabs == nil {
		s.OpenTabs = []string{}
	}
	if s.KeyMap == nil {
		s.KeyMap = []KeyBinding{}
	}
	if s.KeywordSets == nil {
		s.KeywordSets = []KeywordSet{}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.file()+".tmp", data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(m.file()+".tmp", m.file()); err != nil {
		return err
	}
	m.cur = s
	return nil
}

// AddRecent records a recently used session path (max 10).
func (m *Manager) AddRecent(p string) {
	s := m.Get()
	out := []string{p}
	for _, r := range s.RecentSessions {
		if r != p && len(out) < 10 {
			out = append(out, r)
		}
	}
	s.RecentSessions = out
	_ = m.Save(s)
}
