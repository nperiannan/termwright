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
	"time"
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
	SessionsDir          string                       `json:"sessionsDir"`
	LogDir               string                       `json:"logDir"`
	DownloadDir          string                       `json:"downloadDir"`
	FontFamily           string                       `json:"fontFamily"`
	FontSize             int                          `json:"fontSize"`
	LineHeight           float64                      `json:"lineHeight"`         // terminal line spacing (1.0 = the font's own; 0 = 1.0)
	Theme                string                       `json:"theme"`              // ui theme: dark | light | system
	UIFont               string                       `json:"uiFont"`             // interface font: "" (system), vscode, inter, plex
	UIFontSize           int                          `json:"uiFontSize"`         // interface text size in px (13-17; 0 = 13)
	ThemeFollowsSystem   bool                         `json:"themeFollowsSystem"` // set once when the default became "system" (0.20.1)
	HighlightOptIn       bool                         `json:"highlightOptIn"`     // set once when keyword highlighting became off by default (0.20.3)
	Layout               string                       `json:"layout"`             // "" = modern (title bar with tabs), "classic" = toolbar, tab strip and menus
	ModernLayout         bool                         `json:"modernLayout"`       // set once when the modern layout became the default (0.21.0)
	HighlightStyle       string                       `json:"highlightStyle"`     // keyword highlighting: "" = colour the text, "background" = colour the background
	DeviceFacts          map[string]map[string]string `json:"deviceFacts"`        // session path → fact name → value ("_at" = when read)
	FactRulesAdded       bool                         `json:"factRulesAdded"`     // set once when the built-in device types got their fact rules (0.21.0)
	KeywordsOn           bool                         `json:"keywordsOn"`
	IconStyle            string                       `json:"iconStyle"`           // device and session icons: "" = coloured tiles, "glyph" = the symbol only
	CompactRows          bool                         `json:"compactRows"`         // modern layout: one-line Session Manager rows
	WelcomeSeen          string                       `json:"welcomeSeen"`         // version whose welcome flow was finished or skipped ("0.21")
	HideTerminalBar      bool                         `json:"hideTerminalBar"`     // no bar (timestamps, auto scroll, clear) under SSH / Telnet / local tabs
	TerminalScheme       string                       `json:"terminalScheme"`      // scheme name or "Auto" (follow the UI theme)
	ForceTermScheme      bool                         `json:"forceTerminalScheme"` // global scheme wins over session schemes
	CursorStyle          string                       `json:"cursorStyle"`         // block | underline | bar
	CursorBlink          bool                         `json:"cursorBlink"`
	Scrollback           int                          `json:"scrollback"`
	CopyOnSelect         bool                         `json:"copyOnSelect"`
	RightClickPaste      bool                         `json:"rightClickPaste"`
	ConfirmClose         bool                         `json:"confirmClose"`
	ShowButtonBar        bool                         `json:"showButtonBar"`
	ShowCommandWin       bool                         `json:"showCommandWindow"`
	ShowSessionTree      bool                         `json:"showSessionManager"`
	PasteLineDelayMs     int                          `json:"pasteLineDelayMs"`
	Buttons              []Button                     `json:"buttons"`
	RecentSessions       []string                     `json:"recentSessions"`
	Favorites            []string                     `json:"favorites"`        // session paths shown at the top of the Session Manager
	LastConnected        map[string]string            `json:"lastConnected"`    // session path → RFC 3339 time of the last connect
	ShowReachability     bool                         `json:"showReachability"` // Session Manager: dot per session from a quick TCP check
	BackupDir            string                       `json:"backupDir"`        // Configuration Backups folder ("" = Documents/Termwright Config Backups)
	BackupJobs           []BackupJob                  `json:"backupJobs"`
	Checklists           []Checklist                  `json:"checklists"`
	Collections          []Collection                 `json:"collections"`
	CollectDir           string                       `json:"collectDir"`              // "" = Documents/Termwright Collected
	ChecklistDir         string                       `json:"checklistDir"`            // "" = Documents/Termwright Checklists
	RecordHistory        *bool                        `json:"recordHistory,omitempty"` // nil = on
	RecordSessions       bool                         `json:"recordSessions"`          // record every connection (.cast)
	RecordDir            string                       `json:"recordDir"`               // "" = Documents/Termwright Recordings
	RecordKeepDays       int                          `json:"recordKeepDays"`          // 0 = keep
	SmartCardLibraries   []string                     `json:"smartCardLibraries"`      // PKCS#11 libraries; empty = the usual places
	SmartCardPinEachTime bool                         `json:"smartCardPinEachTime"`    // ask the PIN for every login
	KeywordSets          []KeywordSet                 `json:"keywordSets"`
	DefaultKeywordSet    string                       `json:"defaultKeywordSet"`
	ShowConnectBar       bool                         `json:"showConnectBar"`
	InWindowMenu         bool                         `json:"inWindowMenu"` // menu bar inside the window (default on macOS)
	Credentials          []Credential                 `json:"credentials"`
	MasterHash           string                       `json:"masterHash"` // scrypt verifier; empty = no master password
	KeyMap               []KeyBinding                 `json:"keyMap"`
	CustomSchemes        []ColorScheme                `json:"customSchemes"`
	AccentColor          string                       `json:"accentColor"`
	RestoreTabs          bool                         `json:"restoreTabs"`
	OpenTabs             []string                     `json:"openTabs"`
	Bookmarks            []Bookmark                   `json:"bookmarks"`
	SFTPCwdAuto          bool                         `json:"sftpCwdAuto"`              // turn on folder reporting in the shell when an SFTP tab opens from it
	SFTPFollowTerminal   bool                         `json:"sftpFollowTerminal"`       // SFTP tabs follow the terminal folder
	SFTPFollowUser       bool                         `json:"sftpFollowUser"`           // SFTP tabs browse as the terminal's user (root via sudo)
	SessionManagerRight  bool                         `json:"sessionManagerRight"`      // Session Manager on the right side of the window
	VaultAddr            string                       `json:"vaultAddr,omitempty"`      // HashiCorp Vault URL for vault:// references ("" = $VAULT_ADDR)
	KeyPassphrases       []string                     `json:"keyPassphrases,omitempty"` // key files whose passphrase is in the OS keychain (no secrets here)
	UpdateChannel        string                       `json:"updateChannel"`            // stable | nightly | off
	UpdateRepo           string                       `json:"updateRepo"`               // GitHub owner/name
	UpdateOnStart        bool                         `json:"updateOnStart"`
	PythonPath           string                       `json:"pythonPath"` // interpreter for .py scripts ("" = find python3)
	DeviceTypes          []session.DeviceType         `json:"deviceTypes"`
	Alerts               []Alert                      `json:"alerts"`
	ConfirmPaste         bool                         `json:"confirmPaste"`     // ask before pasting several lines
	QuickConnectSave     bool                         `json:"quickConnectSave"` // Quick Connect saves the session (switch on by default; remembered)
	NoMoveConfirm        bool                         `json:"noMoveConfirm"`    // moving sessions or folders in the Session Manager does not ask first
	Snippets             []Snippet                    `json:"snippets"`
	Workspaces           []Workspace                  `json:"workspaces"`
	FolderDefaults       []FolderDefault              `json:"folderDefaults"`
	Window               WindowState                  `json:"window"`
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
	// DeviceTypes the command is meant for; empty = any. A folder named
	// after a device type ("VOSS / Troubleshooting") counts too.
	DeviceTypes []string `json:"deviceTypes"`
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
	Environment  string `json:"environment"`
	Record       bool   `json:"record,omitempty"`    // record the sessions of the folder
	SmartCard    string `json:"smartCard,omitempty"` // "any" or a key fingerprint
}

// BackupJob backs up the configuration of sessions, on demand or on a schedule.
type BackupJob struct {
	Name    string   `json:"name"`
	Targets []string `json:"targets"` // session paths; folders end with "/"
	// Schedule: "" (on demand), "daily" or "weekdays", at At ("HH:MM", local time).
	Schedule string `json:"schedule"`
	At       string `json:"at"`
	Keep     int    `json:"keep"`     // copies per device (0 = 30)
	SaveAll  bool   `json:"saveAll"`  // save a copy every run, also when nothing changed
	Commands string `json:"commands"` // for sessions without a device type with backup commands
}

// Checklist is a set of show commands run on devices before and after a change.
type Checklist struct {
	Name     string              `json:"name"`
	Targets  []string            `json:"targets"` // session paths; folders end with "/"
	Commands []ChecklistCommands `json:"commands"`
}

// Collection runs commands on many devices and collects values from the
// output into a table (Run and Collect).
type Collection struct {
	Name     string              `json:"name"`
	Targets  []string            `json:"targets"`
	Commands []ChecklistCommands `json:"commands"`
	Columns  []CollectColumn     `json:"columns"`
}

// CollectColumn is one value taken from each device's output: the first
// group of Pattern (a regular expression), or the whole match.
type CollectColumn struct {
	Name       string `json:"name"`
	Pattern    string `json:"pattern"`
	DeviceType string `json:"deviceType"` // "" = any device type
}

// ChecklistCommands are the commands for one device type ("" = any other).
type ChecklistCommands struct {
	DeviceType string `json:"deviceType"`
	Commands   string `json:"commands"` // one per line
}

// For returns the commands for a device type.
func (c Checklist) For(deviceType string) []string {
	pick := ""
	found := false
	for _, x := range c.Commands {
		if x.DeviceType != "" && strings.EqualFold(x.DeviceType, deviceType) {
			pick, found = x.Commands, true
		}
	}
	if !found {
		for _, x := range c.Commands {
			if x.DeviceType == "" {
				pick = x.Commands
			}
		}
	}
	var out []string
	for _, l := range strings.Split(pick, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
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
		LineHeight:      1,
		Theme:           "system",
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
				{Text: `\b(warning|warn|disabled?|blocked)\b`, Regex: true, Color: "#6b5310"},
				{Text: `\b(up|ok|success|enabled?|active|forwarding)\b`, Regex: true, Color: "#1f5a2c"},
				{Text: `\b\d{1,3}(\.\d{1,3}){3}(/\d{1,2})?\b`, Regex: true, Color: "#1f3f6b"},
			},
		}},
		DefaultKeywordSet:  "Network", // up / down, enable / disable, errors and addresses in colour; "None" turns it off
		Bookmarks:          []Bookmark{},
		UpdateChannel:      "stable",
		SFTPCwdAuto:        true,
		SFTPFollowTerminal: true,
		SFTPFollowUser:     true,
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
	if d := os.Getenv("TERMWRIGHT_HOME"); d != "" {
		return d, os.MkdirAll(d, 0o700)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, "Termwright")
	return d, os.MkdirAll(d, 0o700)
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
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	changed := defaultUpdateRepo(&m.cur)
	// Until 0.20.0 the theme was dark unless changed; it now follows the
	// system (once - a theme chosen afterwards stays).
	if !m.cur.ThemeFollowsSystem {
		m.cur.Theme, m.cur.ThemeFollowsSystem, changed = "system", true, true
	}
	// Keyword highlighting is off by default since 0.20.3 (once - a default
	// set picked afterwards stays).
	if !m.cur.HighlightOptIn {
		m.cur.DefaultKeywordSet, m.cur.HighlightOptIn, changed = "", true, true
	}
	// Device facts (0.21.0): the built-in device types get their fact rules
	// (once; rules edited afterwards stay).
	if !m.cur.FactRulesAdded {
		for i, d := range m.cur.DeviceTypes {
			if len(d.Facts) == 0 {
				m.cur.DeviceTypes[i].Facts = session.BuiltinFacts(d.Name)
			}
		}
		m.cur.FactRulesAdded, changed = true, true
	}
	// Keyword highlighting is on by default (0.21.0): once, the Network set
	// becomes the default when none was chosen, and its built-in words also
	// match "enable" and "disable" (sets edited by the user stay as they are).
	if !m.cur.KeywordsOn {
		for i, k := range m.cur.KeywordSets {
			if k.Name != "Network" {
				continue
			}
			for j, w := range k.Keywords {
				switch w.Text {
				case `\b(warning|warn|disabled|blocked)\b`:
					m.cur.KeywordSets[i].Keywords[j].Text = `\b(warning|warn|disabled?|blocked)\b`
				case `\b(up|ok|success|enabled|active|forwarding)\b`:
					m.cur.KeywordSets[i].Keywords[j].Text = `\b(up|ok|success|enabled?|active|forwarding)\b`
				}
			}
			if m.cur.DefaultKeywordSet == "" {
				m.cur.DefaultKeywordSet = "Network"
			}
		}
		m.cur.KeywordsOn, changed = true, true
	}
	// macOS terminal text (0.21.0): the old default Menlo 14 becomes Menlo 12
	// (once; a size the user chose stays).
	// The modern layout (0.21.0) keeps the Button Bar out of the way (once -
	// it can be shown again from View).
	if !m.cur.ModernLayout {
		m.cur.ShowButtonBar, m.cur.ModernLayout, changed = false, true, true
	}
	if changed {
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

// defaultUpdateRepo fills in the public repository when none is set.
func defaultUpdateRepo(s *Settings) bool {
	if strings.TrimSpace(s.UpdateRepo) == "" {
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
	if s.Favorites == nil {
		s.Favorites = []string{}
	}
	if s.LastConnected == nil {
		s.LastConnected = map[string]string{}
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
	if s.BackupJobs == nil {
		s.BackupJobs = []BackupJob{}
	}
	if s.Checklists == nil {
		s.Checklists = []Checklist{}
	}
	if s.SmartCardLibraries == nil {
		s.SmartCardLibraries = []string{}
	}
	if s.Collections == nil {
		s.Collections = []Collection{}
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
	lc := make(map[string]string, len(s.LastConnected)+1) // a copy: Get shares the map
	for k, v := range s.LastConnected {
		lc[k] = v
	}
	lc[p] = time.Now().UTC().Format(time.RFC3339)
	s.LastConnected = lc
	_ = m.Save(s)
}

// SetFacts stores facts read for a session (merged with those it has);
// returns all of them.
func (m *Manager) SetFacts(path string, f map[string]string) map[string]string {
	s := m.Get()
	if s.DeviceFacts == nil {
		s.DeviceFacts = map[string]map[string]string{}
	}
	cur := map[string]string{}
	for k, v := range s.DeviceFacts[path] {
		cur[k] = v
	}
	for k, v := range f {
		if v == "" {
			delete(cur, k) // an empty value removes the fact
		} else {
			cur[k] = v
		}
	}
	s.DeviceFacts[path] = cur
	_ = m.Save(s)
	return cur
}

// MovePaths keeps favourites, recent sessions and last-connected times when a
// session (or, with folder, every session under a folder) is renamed or moved.
// An empty newPath forgets them (deleted).
func (m *Manager) MovePaths(oldPath, newPath string, folder bool) {
	s := m.Get()
	conv := func(p string) (string, bool) {
		switch {
		case p == oldPath && !folder:
		case folder && strings.HasPrefix(p, oldPath+"/"):
			if newPath == "" {
				return "", true
			}
			return newPath + p[len(oldPath):], true
		default:
			return p, false
		}
		return newPath, true
	}
	changed := false
	list := func(l []string) []string {
		out := make([]string, 0, len(l))
		seen := map[string]bool{}
		for _, p := range l {
			q, ok := conv(p)
			changed = changed || ok
			if q != "" && !seen[q] {
				seen[q] = true
				out = append(out, q)
			}
		}
		return out
	}
	s.Favorites, s.RecentSessions, s.OpenTabs = list(s.Favorites), list(s.RecentSessions), list(s.OpenTabs)
	lc := map[string]string{}
	for p, t := range s.LastConnected {
		q, ok := conv(p)
		changed = changed || ok
		if q != "" {
			lc[q] = t
		}
	}
	s.LastConnected = lc
	if len(s.DeviceFacts) > 0 {
		df := map[string]map[string]string{}
		for p, f := range s.DeviceFacts {
			q, ok := conv(p)
			changed = changed || ok
			if q != "" {
				df[q] = f
			}
		}
		s.DeviceFacts = df
	}
	if changed {
		_ = m.Save(s)
	}
}
