package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/secretref"
	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/sshkey"
	"github.com/termwright/termwright/internal/term"
	"github.com/termwright/termwright/internal/update"
	"github.com/termwright/termwright/internal/vault"
)

// Version is set at build time with -ldflags "-X main.Version=x.y.z".
var Version = "0.12.6-dev"

// App is the object bound to the frontend. Every exported method is callable
// from TypeScript as window.go.main.App.<Method>.
type App struct {
	ctx      context.Context
	mu       sync.Mutex
	cfg      *config.Manager
	store    *session.Store
	terms    *term.Manager
	quitting bool
	initErr  error
	locked   bool // saved passwords unavailable until the master password is entered
	refs     *secretref.Resolver
	screens  sync.Map // screen request id → chan *term.ScreenSnap
}

// NewApp creates the application.
func NewApp() *App { return &App{} }

type emitter struct{ a *App }

func (e emitter) Emit(name string, data any) {
	if e.a.ctx != nil {
		wruntime.EventsEmit(e.a.ctx, name, data)
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := a.init(); err != nil {
		a.initErr = err
	}
}

func (a *App) init() error {
	dir, err := config.DataDir()
	if err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	a.cfg = cfg
	st, err := session.NewStore(cfg.Get().SessionsDir)
	if err != nil {
		return err
	}
	a.store = st
	a.terms = term.NewManager(emitter{a}, dir, func() string { return a.cfg.Get().LogDir })
	a.terms.LoadSession = func(p string) (session.Session, error) { return a.store.Get(p) }
	a.terms.DownloadDir = func() string { return a.cfg.Get().DownloadDir }
	a.terms.Clipboard = func() string {
		t, _ := wruntime.ClipboardGetText(a.ctx)
		return t
	}
	a.terms.Credential = a.credential
	a.terms.PythonPath = func() string { return a.cfg.Get().PythonPath }
	a.terms.DeviceType = func(name string) (session.DeviceType, bool) {
		for _, d := range a.cfg.Get().DeviceTypes {
			if strings.EqualFold(d.Name, name) {
				return d, true
			}
		}
		return session.DeviceType{}, false
	}
	a.applyAlerts()
	term.Version = Version
	a.terms.KeyPassphrase = func(path string) string {
		if a.isLocked() {
			return ""
		}
		return secrets.Get("keypass:" + path)
	}
	a.locked = cfg.Get().MasterHash != ""
	a.terms.SavedPassword = func(p string) (string, bool) {
		if a.isLocked() {
			return "", false
		}
		return a.secret(p)
	}
	a.terms.DevicePassword = func(name string) (string, bool) {
		if a.isLocked() {
			return "", false
		}
		return a.secret("devicetype:" + strings.ToLower(name))
	}
	a.terms.OnDevicePasswordTyped = func(name, pw string) {
		if err := a.SetDeviceTypePassword(name, pw, pw == ""); err != nil {
			emitter{a}.Emit("toast", map[string]string{"level": "error", "message": "Could not save the password: " + err.Error()})
			return
		}
		msg := "Saved for device type " + name
		if pw == "" {
			msg = "Device type " + name + ": no password - you will not be asked again"
		}
		emitter{a}.Emit("toast", map[string]string{"level": "success", "message": msg})
	}
	a.terms.ScreenSnapshot = a.screenSnapshot
	a.terms.OnKeyPassphraseUsed = func(path, pw string) {
		if err := a.saveKeyPassphrase(path, pw); err != nil {
			emitter{a}.Emit("toast", map[string]string{"level": "error", "message": "Could not save passphrase: " + err.Error()})
		}
	}
	a.terms.OnPasswordUsed = func(p, pw string) {
		if cur, ok := secrets.Lookup(p); ok && secretref.IsRef(cur) {
			return // keep the password-manager reference
		}
		if err := secrets.Set(p, pw); err != nil {
			emitter{a}.Emit("toast", map[string]string{"level": "error", "message": "Could not save password: " + err.Error()})
			return
		}
		if s, err := a.store.Get(p); err == nil && !s.SavePassword { // keep the session's checkbox in sync
			s.SavePassword = true
			_, _ = a.store.Save(s, s.Path)
		}
	}
	return nil
}

func (a *App) shutdown(context.Context) {
	if a.terms != nil {
		a.terms.CloseAll()
	}
}

// beforeClose asks the UI to confirm quitting while sessions are connected.
func (a *App) beforeClose(ctx context.Context) bool {
	a.saveWindow(ctx)
	a.mu.Lock()
	q := a.quitting
	a.mu.Unlock()
	if q || a.cfg == nil || !a.cfg.Get().ConfirmClose {
		return false
	}
	wruntime.EventsEmit(ctx, "app:confirm-quit")
	return true
}

// Quit closes the application without asking again.
func (a *App) Quit() {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	wruntime.Quit(a.ctx)
}

func (a *App) ready() error {
	if a.initErr != nil {
		return a.initErr
	}
	if a.store == nil {
		return errors.New("application is still starting")
	}
	return nil
}

// AppInfo describes the running application.
type AppInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	DataDir     string `json:"dataDir"`
	SessionsDir string `json:"sessionsDir"`
	InitError   string `json:"initError"`
	Locked      bool   `json:"locked"`
	HasMaster   bool   `json:"hasMaster"`
}

// GetAppInfo returns version and platform details.
func (a *App) GetAppInfo() AppInfo {
	ai := AppInfo{Name: "Termwright", Version: Version, OS: runtime.GOOS, Arch: runtime.GOARCH}
	if a.cfg != nil {
		ai.DataDir = a.cfg.DataDir()
	}
	if a.store != nil {
		ai.SessionsDir = a.store.Root()
	}
	if a.initErr != nil {
		ai.InitError = a.initErr.Error()
	}
	ai.Locked = a.isLocked()
	if a.cfg != nil {
		ai.HasMaster = a.cfg.Get().MasterHash != ""
	}
	return ai
}

// ---- settings ----

// GetSettings returns the global options.
func (a *App) GetSettings() (config.Settings, error) {
	if err := a.ready(); err != nil {
		return config.Settings{}, err
	}
	return a.cfg.Get(), nil
}

// SaveSettings stores the global options.
func (a *App) SaveSettings(s config.Settings) error {
	if err := a.ready(); err != nil {
		return err
	}
	if s.SessionsDir != a.store.Root() {
		st, err := session.NewStore(s.SessionsDir)
		if err != nil {
			return err
		}
		a.store = st
	}
	s.MasterHash = a.cfg.Get().MasterHash // changed only through SetMasterPassword
	if err := a.cfg.Save(s); err != nil {
		return err
	}
	if bad := a.applyAlerts(); len(bad) > 0 {
		return errors.New("alert pattern: " + strings.Join(bad, "; "))
	}
	return nil
}

// ---- sessions ----

// GetSessionTree returns the session folder tree.
func (a *App) GetSessionTree() (*session.Node, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.Tree()
}

// GetSession loads one session.
func (a *App) GetSession(path string) (session.Session, error) {
	if err := a.ready(); err != nil {
		return session.Session{}, err
	}
	return a.store.Get(path)
}

// NewSessionDefaults returns a pre-filled session for the editor.
func (a *App) NewSessionDefaults() (session.Session, error) {
	if err := a.ready(); err != nil {
		return session.Session{}, err
	}
	return a.store.NewSession(), nil
}

// SaveSession creates or updates a session. oldPath is "" for new sessions.
func (a *App) SaveSession(s session.Session, oldPath string) (session.Session, error) {
	if err := a.ready(); err != nil {
		return s, err
	}
	saved, err := a.store.Save(s, oldPath)
	if err != nil {
		return saved, err
	}
	if oldPath != "" && oldPath != saved.Path {
		if pw, ok := secrets.Lookup(oldPath); ok {
			_ = secrets.Set(saved.Path, pw)
			_ = secrets.Delete(oldPath)
		}
	}
	if !saved.SavePassword {
		_ = secrets.Delete(saved.Path)
	}
	return saved, nil
}

// DeleteSession removes a session and its saved password.
func (a *App) DeleteSession(path string) error {
	if err := a.ready(); err != nil {
		return err
	}
	_ = secrets.Delete(path)
	return a.store.Delete(path)
}

// DuplicateSession copies a session and returns the new path.
func (a *App) DuplicateSession(path string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	return a.store.Duplicate(path)
}

// CreateFolder creates a session folder.
func (a *App) CreateFolder(path string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.CreateFolder(path)
}

// RenameFolder renames or moves a folder.
func (a *App) RenameFolder(oldPath, newPath string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.RenameFolder(oldPath, newPath)
}

// DeleteFolder removes a folder with all its sessions.
func (a *App) DeleteFolder(path string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.DeleteFolder(path)
}

// ForgetPassword removes a saved password.
func (a *App) ForgetPassword(path string) error { return secrets.Delete(path) }

// ImportSessions imports from a config directory, .zip, .xml, .ini or ssh config.
func (a *App) ImportSessions(src string, opt session.ImportOptions) (*session.ImportResult, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	res, err := a.store.Import(src, opt)
	if res != nil {
		a.storeImportedPasswords(res)
	}
	return res, err
}

// storeImportedPasswords moves passwords decrypted during an import into the OS keychain.
func (a *App) storeImportedPasswords(res *session.ImportResult) {
	for p, pw := range res.Passwords {
		if err := secrets.Set(p, pw); err != nil {
			res.PasswordErrors = append(res.PasswordErrors, p+": "+err.Error())
			continue
		}
		res.PasswordsImported++
	}
	res.Passwords = nil
}

// ExportSessions exports sessions ("dir", "zip" or "xml").
func (a *App) ExportSessions(paths []string, dest, format string) (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	return a.store.Export(paths, dest, format)
}

// ListSerialPorts lists serial devices.
func (a *App) ListSerialPorts() []string { return term.SerialPorts() }

// DefaultSSHConfigPath returns ~/.ssh/config.
func (a *App) DefaultSSHConfigPath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".ssh", "config")
}

// ---- terminals ----

// OpenTab creates a tab for a saved session.
func (a *App) OpenTab(path string) (term.TabInfo, error) {
	if err := a.ready(); err != nil {
		return term.TabInfo{}, err
	}
	s, err := a.store.Get(path)
	if err != nil {
		return term.TabInfo{}, err
	}
	a.cfg.AddRecent(s.Path)
	return a.terms.Create(a.withFolderDefaults(s)), nil
}

// OpenAdHoc creates a tab for an unsaved session (quick connect / local shell).
func (a *App) OpenAdHoc(s session.Session) (term.TabInfo, error) {
	if err := a.ready(); err != nil {
		return term.TabInfo{}, err
	}
	s.Path = ""
	return a.terms.Create(s), nil
}

// StartTab connects (or reconnects) a tab.
func (a *App) StartTab(id string, cols, rows int) error { return a.terms.Start(id, cols, rows) }

// SendInput writes keyboard input (UTF-8 text) to a tab.
func (a *App) SendInput(id, data string) error { return a.terms.Write(id, []byte(data)) }

// SendBinary writes base64-encoded raw bytes to a tab.
func (a *App) SendBinary(id, b64 string) error {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return err
	}
	return a.terms.Write(id, b)
}

// ResizeTab updates the terminal size.
func (a *App) ResizeTab(id string, cols, rows int) error { return a.terms.Resize(id, cols, rows) }

// DisconnectTab closes the connection but keeps the tab.
func (a *App) DisconnectTab(id string) error { return a.terms.Disconnect(id) }

// CloseTab disconnects and forgets a tab.
func (a *App) CloseTab(id string) error { return a.terms.Close(id) }

// GetTabInfo returns tab details.
func (a *App) GetTabInfo(id string) (term.TabInfo, error) { return a.terms.Info(id) }

// RefreshTabSession reloads a tab's saved session so the next reconnect uses
// edited settings. path is the session's (possibly new) path; "" keeps the
// current one.
func (a *App) RefreshTabSession(id, path string) error {
	ti, err := a.terms.Info(id)
	if err != nil {
		return err
	}
	if path == "" {
		path = ti.Session.Path
	}
	if path == "" {
		return nil
	}
	s, err := a.store.Get(path)
	if err != nil {
		return err
	}
	s = a.withFolderDefaults(s)
	return a.terms.UpdateSession(id, s)
}

// StartLog starts logging; returns the log path.
func (a *App) StartLog(id, path string, appendMode, raw bool) (string, error) {
	return a.terms.StartLog(id, path, appendMode, raw)
}

// StopLog stops logging.
func (a *App) StopLog(id string) error { return a.terms.StopLog(id) }

// AnswerPrompt delivers the user's answer to a backend prompt.
func (a *App) AnswerPrompt(id string, r term.PromptReply) { a.terms.Prompts().Answer(id, r) }

// ---- files & dialogs ----

// FileFilter is a dialog filter.
type FileFilter struct {
	DisplayName string `json:"displayName"`
	Pattern     string `json:"pattern"`
}

func toFilters(f []FileFilter) []wruntime.FileFilter {
	out := make([]wruntime.FileFilter, 0, len(f))
	for _, x := range f {
		out = append(out, wruntime.FileFilter{DisplayName: x.DisplayName, Pattern: x.Pattern})
	}
	return out
}

// ChooseOpenFile shows an open-file dialog.
func (a *App) ChooseOpenFile(title string, filters []FileFilter) (string, error) {
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: title, Filters: toFilters(filters), ShowHiddenFiles: true})
}

// ChooseDirectory shows a directory picker.
func (a *App) ChooseDirectory(title string) (string, error) {
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: title, CanCreateDirectories: true, ShowHiddenFiles: true})
}

// ChooseSaveFile shows a save dialog.
func (a *App) ChooseSaveFile(title, defaultName string, filters []FileFilter) (string, error) {
	return wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: title, DefaultFilename: defaultName, Filters: toFilters(filters), CanCreateDirectories: true})
}

// SaveTextFile writes text (e.g. captured scrollback) to a file.
func (a *App) SaveTextFile(path, content string) error {
	if path == "" {
		return errors.New("no file selected")
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// ReadTextFile reads a small text file (e.g. a script to send).
func (a *App) ReadTextFile(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.Size() > 8<<20 {
		return "", errors.New("file is larger than 8 MB")
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

// ---- SFTP & transfers ----

// SFTPOpen opens an SFTP browser on a connected SSH2 tab; returns id and home dir.
// mode: "" follows the session, "jump" = the host logged in to, "target" = the
// host of the session's "ssh … host" remote command (through the connection).
func (a *App) SFTPOpen(tabID, mode string) (map[string]string, error) {
	id, home, err := a.terms.SFTPOpen(tabID, mode)
	if err != nil {
		return nil, err
	}
	return map[string]string{"id": id, "home": home, "target": a.terms.SFTPTarget(id)}, nil
}

// SFTPConnect opens a standalone SFTP session for a saved session (its own
// SSH login, no terminal). Returns id, home and info.
func (a *App) SFTPConnect(path, mode string) (map[string]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	s, err := a.store.Get(path)
	if err != nil {
		return nil, err
	}
	a.cfg.AddRecent(s.Path)
	id, home, info, err := a.terms.SFTPConnect(a.withFolderDefaults(s), mode)
	if err != nil {
		return nil, err
	}
	return map[string]string{"id": id, "home": home, "info": info, "target": a.terms.SFTPTarget(id)}, nil
}

// SFTPSetRoot switches an SFTP tab to root (via sudo) or back; returns the folder to show.
func (a *App) SFTPSetRoot(id string, root bool) (string, error) { return a.terms.SFTPSetRoot(id, root) }

// SFTPRunAs switches an SFTP tab to the user a command logs in to (e.g. "sudo su - oracle"),
// asking for passwords it prompts for; "" returns to the login user. Returns folder and user.
// SFTPLoginShell returns the login shell name on the host an SFTP tab browses ("" if unknown).
func (a *App) SFTPLoginShell(id string) string { return a.terms.SFTPLoginShell(id) }

func (a *App) SFTPRunAs(id, command string) (map[string]string, error) {
	home, user, err := a.terms.SFTPRunAs(id, command)
	if err != nil {
		return nil, err
	}
	return map[string]string{"home": home, "user": user}, nil
}

// SFTPHopTarget returns the user@host of a session's "ssh … host" remote command, or "".
func (a *App) SFTPHopTarget(s session.Session) string {
	if h, ok := session.HopOf(s); ok {
		return h.Target
	}
	return ""
}

// SFTPList lists a remote directory.
func (a *App) SFTPList(id, dir string) ([]term.FileEntry, error) { return a.terms.SFTPList(id, dir) }

// SFTPRealPath normalises a remote path.
func (a *App) SFTPRealPath(id, p string) (string, error) { return a.terms.SFTPRealPath(id, p) }

// SFTPMkdir creates a remote folder.
func (a *App) SFTPMkdir(id, p string) error { return a.terms.SFTPMkdir(id, p) }

// SFTPRename renames a remote file.
func (a *App) SFTPRename(id, from, to string) error { return a.terms.SFTPRename(id, from, to) }

// SFTPRemove deletes a remote file or folder.
func (a *App) SFTPRemove(id, p string) error { return a.terms.SFTPRemove(id, p) }

// SFTPClose closes an SFTP browser.
func (a *App) SFTPClose(id string) { a.terms.SFTPClose(id) }

// LocalList lists a folder on this computer.
func (a *App) LocalList(dir string) ([]term.FileEntry, error) { return term.LocalList(dir) }

// LocalHome returns the user's home folder.
func (a *App) LocalHome() string {
	h, _ := os.UserHomeDir()
	return h
}

// LocalParent returns the parent folder of a local path.
func (a *App) LocalParent(p string) string { return filepath.Dir(p) }

// LocalJoin joins local path elements.
func (a *App) LocalJoin(dir, name string) string { return filepath.Join(dir, name) }

// LocalMkdir creates a local folder.
func (a *App) LocalMkdir(p string) error { return os.MkdirAll(p, 0o755) }

// StartTransfer uploads ("up") or downloads ("down") files and folders.
func (a *App) StartTransfer(sftpID, direction string, sources []string, destDir string) (string, error) {
	return a.terms.StartTransfer(sftpID, direction, sources, destDir)
}

// ResumeTransfer continues an interrupted SFTP copy (partial files are appended, complete files skipped).
func (a *App) ResumeTransfer(id string) (string, error) { return a.terms.ResumeTransfer(id) }

// SendYmodem uploads files with YMODEM batch (start "rb" on the device first).
func (a *App) SendYmodem(tabID string, files []string) (string, error) {
	return a.terms.SendYmodem(tabID, files)
}

// ReceiveYmodem receives a YMODEM batch into the download folder.
func (a *App) ReceiveYmodem(tabID string) (string, error) { return a.terms.ReceiveYmodem(tabID) }

// CancelTransfer stops a transfer.
func (a *App) CancelTransfer(id string) { a.terms.CancelTransfer(id) }

// SendXmodem uploads a file with XMODEM (128 or 1024 byte blocks).
func (a *App) SendXmodem(tabID, file string, blockSize int) (string, error) {
	return a.terms.SendXmodem(tabID, file, blockSize)
}

// ---- scripting ----

// RunScriptFile runs a JavaScript file against a tab.
func (a *App) RunScriptFile(tabID, file string) (string, error) {
	return a.terms.RunScriptFile(tabID, file, "")
}

// CancelScript stops a running script.
func (a *App) CancelScript(runID string) { a.terms.CancelScript(runID) }

// StartRecording records typed commands of a tab as a script.
func (a *App) StartRecording(tabID string) error { return a.terms.StartRecording(tabID) }

// StopRecording returns the recorded script source.
func (a *App) StopRecording(tabID string) (string, error) { return a.terms.StopRecording(tabID) }

// ZmodemSend answers a remote "rz" with files (empty list = cancel).
func (a *App) ZmodemSend(tabID string, files []string) (string, error) {
	return a.terms.ZmodemSend(tabID, files)
}

// ReceiveXmodem receives a file with XMODEM into outPath.
func (a *App) ReceiveXmodem(tabID, outPath string) (string, error) {
	return a.terms.ReceiveXmodem(tabID, outPath)
}

// SCPTransfer copies a file or folder with SCP ("up": local->remote, "down": remote->local folder).
func (a *App) SCPTransfer(tabID, direction, src, dest string) (string, error) {
	return a.terms.SCPTransfer(tabID, direction, src, dest)
}

// SCPTransferSFTP copies a file or folder with SCP over an SFTP tab's connection.
func (a *App) SCPTransferSFTP(sftpID, direction, src, dest string) (string, error) {
	return a.terms.SCPTransferSFTP(sftpID, direction, src, dest)
}

// StartSync copies newer/missing files between a local and a remote folder.
func (a *App) StartSync(sftpID, direction, localDir, remoteDir string) (string, error) {
	return a.terms.StartSync(sftpID, direction, localDir, remoteDir)
}

// ChooseOpenFiles shows a multi-select open dialog.
func (a *App) ChooseOpenFiles(title string) ([]string, error) {
	return wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{Title: title, ShowHiddenFiles: true})
}

// SendBreak sends a break signal (serial console, telnet, SSH).
func (a *App) SendBreak(tabID string) error { return a.terms.SendBreak(tabID) }

// ---- credentials, master password, password vault ----

func (a *App) isLocked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.locked
}

func (a *App) credential(name string) (string, string, bool) {
	for _, c := range a.cfg.Get().Credentials {
		if c.Name == name {
			pw := ""
			if !a.isLocked() {
				pw, _ = a.secret("credential:" + name)
			}
			return c.Username, pw, true
		}
	}
	return "", "", false
}

// SetCredentialPassword stores (or with "" removes) a credential's password.
func (a *App) SetCredentialPassword(name, password string) error {
	if password == "" {
		return secrets.Delete("credential:" + name)
	}
	return secrets.Set("credential:"+name, password)
}

// HasCredentialPassword reports whether a credential has a saved password.
func (a *App) HasCredentialPassword(name string) bool { return secrets.Get("credential:"+name) != "" }

// UnlockPasswords checks the master password and unlocks saved passwords.
func (a *App) UnlockPasswords(password string) bool {
	if !vault.CheckPassword(password, a.cfg.Get().MasterHash) {
		return false
	}
	a.mu.Lock()
	a.locked = false
	a.mu.Unlock()
	return true
}

// SetMasterPassword sets, changes or (with newPassword "") removes the master
// password. The current one is required when one exists.
func (a *App) SetMasterPassword(current, newPassword string) error {
	s := a.cfg.Get()
	if s.MasterHash != "" && !vault.CheckPassword(current, s.MasterHash) {
		return errors.New("the current master password is wrong")
	}
	s.MasterHash = ""
	if newPassword != "" {
		h, err := vault.HashPassword(newPassword)
		if err != nil {
			return err
		}
		s.MasterHash = h
	}
	return a.cfg.Save(s)
}

type vaultPayload struct {
	Passwords   map[string]string   `json:"passwords"` // "session:<path>" / "credential:<name>"
	Credentials []config.Credential `json:"credentials"`
}

// ExportVault writes all saved passwords and credentials to an encrypted file.
func (a *App) ExportVault(dest, passphrase string) (int, error) {
	if a.isLocked() {
		return 0, errors.New("unlock saved passwords with the master password first")
	}
	p := vaultPayload{Passwords: map[string]string{}, Credentials: a.cfg.Get().Credentials}
	paths, err := a.store.AllSessions(nil)
	if err != nil {
		return 0, err
	}
	for _, sp := range paths {
		if pw, ok := secrets.Lookup(sp); ok {
			p.Passwords["session:"+sp] = pw
		}
	}
	for _, c := range p.Credentials {
		if pw := secrets.Get("credential:" + c.Name); pw != "" {
			p.Passwords["credential:"+c.Name] = pw
		}
	}
	for _, d := range a.cfg.Get().DeviceTypes {
		k := "devicetype:" + strings.ToLower(d.Name)
		if pw, ok := secrets.Lookup(k); ok {
			p.Passwords[k] = pw
		}
	}
	for _, k := range a.keyPassphrasePaths() {
		if pw, ok := secrets.Lookup("keypass:" + k); ok {
			p.Passwords["keypass:"+k] = pw
		}
	}
	data, err := vault.Seal(passphrase, p)
	if err != nil {
		return 0, err
	}
	return len(p.Passwords), os.WriteFile(dest, data, 0o600)
}

// ImportVault restores passwords and credentials from an encrypted file.
func (a *App) ImportVault(src, passphrase string) (int, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return 0, err
	}
	var p vaultPayload
	if err := vault.Open(passphrase, data, &p); err != nil {
		return 0, err
	}
	s := a.cfg.Get()
	for _, c := range p.Credentials {
		found := false
		for i := range s.Credentials {
			if s.Credentials[i].Name == c.Name {
				s.Credentials[i] = c
				found = true
			}
		}
		if !found {
			s.Credentials = append(s.Credentials, c)
		}
	}
	if err := a.cfg.Save(s); err != nil {
		return 0, err
	}
	n := 0
	for k, pw := range p.Passwords {
		key := k
		if strings.HasPrefix(k, "session:") {
			key = strings.TrimPrefix(k, "session:")
			if ss, err := a.store.Get(key); err == nil && !ss.SavePassword {
				ss.SavePassword = true
				_, _ = a.store.Save(ss, ss.Path)
			}
		}
		if strings.HasPrefix(k, "keypass:") {
			if a.saveKeyPassphrase(strings.TrimPrefix(k, "keypass:"), pw) == nil {
				n++
			}
			continue
		}
		if err := secrets.Set(key, pw); err == nil {
			n++
		}
	}
	return n, nil
}

// ---- SFTP site-to-site ----

// StartRemoteCopy copies items from one SFTP session to another (server to server).
func (a *App) StartRemoteCopy(srcID, dstID string, sources []string, destDir string) (string, error) {
	return a.terms.StartRemoteCopy(srcID, dstID, sources, destDir)
}

// ---- updates ----

const tokenKey = "github-token"

// SetUpdateToken stores (or with "" removes) the GitHub token used to read
// releases of a private repository.
func (a *App) SetUpdateToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return secrets.Delete(tokenKey)
	}
	return secrets.Set(tokenKey, token)
}

// HasUpdateToken reports whether a GitHub token is saved.
func (a *App) HasUpdateToken() bool { return secrets.Get(tokenKey) != "" }

// CheckForUpdate looks up the newest stable release and the newest nightly
// build. The channel setting only decides what the start-up check announces.
func (a *App) CheckForUpdate() update.Status {
	s := a.cfg.Get()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return update.CheckAll(ctx, s.UpdateRepo, secrets.Get(tokenKey), Version)
}

// DownloadUpdate downloads the installer into the downloads folder and returns its path.
// Progress is reported with "update:progress" events.
func (a *App) DownloadUpdate(assetURL, name string) (string, error) {
	name = filepath.Base(name)
	if name == "." || name == "/" || name == "" {
		return "", errors.New("invalid file name")
	}
	dir := a.cfg.Get().DownloadDir
	if dir == "" {
		h, _ := os.UserHomeDir()
		dir = filepath.Join(h, "Downloads")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, name)
	last := time.Now()
	err := update.Download(context.Background(), assetURL, secrets.Get(tokenKey), dest, func(done, total int64) {
		if time.Since(last) > 150*time.Millisecond || done == total {
			last = time.Now()
			wruntime.EventsEmit(a.ctx, "update:progress", map[string]int64{"done": done, "total": total})
		}
	})
	return dest, err
}

// OpenUpdate opens a downloaded installer (DMG, setup.exe or .deb) with the system.
func (a *App) OpenUpdate(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// ---- SSH key login setup ----

// KeySetupInfo holds suggestions for the key setup dialog.
type KeySetupInfo struct {
	DefaultPath string   `json:"defaultPath"`
	Comment     string   `json:"comment"`
	Existing    []string `json:"existing"` // private keys found in ~/.ssh
}

// KeySetupDefaults suggests a key file name for host and lists existing keys.
func (a *App) KeySetupDefaults(host string) KeySetupInfo {
	info := KeySetupInfo{DefaultPath: sshkey.DefaultPath(host, "ed25519"), Existing: []string{}}
	u, _ := os.Hostname()
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	info.Comment = sshkey.CleanComment(user + "@" + strings.Split(u, ".")[0] + "-termwright")
	h, _ := os.UserHomeDir()
	ents, _ := os.ReadDir(filepath.Join(h, ".ssh"))
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, ".pub") || strings.HasPrefix(n, "known_hosts") || n == "config" || n == "authorized_keys" {
			continue
		}
		p := filepath.Join(h, ".ssh", n)
		if data, err := os.ReadFile(p); err == nil && (strings.Contains(string(data[:min(len(data), 100)]), "PRIVATE KEY") || sshkey.IsPPK(data)) {
			info.Existing = append(info.Existing, p)
		}
	}
	return info
}

// GenerateKeyPair creates a new key pair and returns the public key line.
func (a *App) GenerateKeyPair(kind, path, comment, passphrase string) (string, error) {
	return sshkey.Generate(kind, sshkeyPath(path), comment, passphrase)
}

// PublicKeyOf returns the public key line of an existing private key file.
func (a *App) PublicKeyOf(path, passphrase string) (string, error) {
	return sshkey.PublicLine(sshkeyPath(path), passphrase)
}

// InstallPublicKey adds a public key to the authorized keys of the account a
// connected SSH2 tab is logged in to.
func (a *App) InstallPublicKey(tabID, line string) (string, error) {
	return a.terms.InstallPublicKey(tabID, line)
}

// TestKeyLogin checks that the key alone can log in to the tab's host.
func (a *App) TestKeyLogin(tabID, path, passphrase string) error {
	return a.terms.TestKeyLogin(tabID, sshkeyPath(path), passphrase)
}

// UseKeyForSession makes a saved session log in with the key (public key
// first, the other methods stay as fallback). A passphrase can be saved in
// the OS keychain so it is not asked on every connect.
func (a *App) UseKeyForSession(sessionPath, keyPath, passphrase string, savePassphrase bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	s, err := a.store.Get(sessionPath)
	if err != nil {
		return err
	}
	keyPath = sshkeyPath(keyPath)
	s.IdentityFile = keyPath
	methods := []string{"publickey"}
	for _, m := range strings.Split(s.AuthMethods, ",") {
		if m = strings.TrimSpace(m); m != "" && m != "publickey" {
			methods = append(methods, m)
		}
	}
	if len(methods) == 1 {
		methods = append(methods, "keyboard-interactive", "password")
	}
	s.AuthMethods = strings.Join(methods, ",")
	if _, err := a.store.Save(s, s.Path); err != nil {
		return err
	}
	if passphrase != "" && savePassphrase {
		return a.saveKeyPassphrase(keyPath, passphrase)
	}
	return nil
}

func sshkeyPath(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~") {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[1:])
		}
	}
	return p
}

// applyAlerts hands the enabled alert rules to the terminal manager.
func (a *App) applyAlerts() []string {
	var rules []term.AlertRule
	for _, r := range a.cfg.Get().Alerts {
		if r.Enabled {
			rules = append(rules, term.AlertRule{Pattern: r.Pattern, Regex: r.Regex, CaseSensitive: r.CaseSensitive, Sound: r.Sound, Desktop: r.Desktop})
		}
	}
	return a.terms.SetAlerts(rules)
}

// Notify shows an operating-system notification (used for output alerts).
func (a *App) Notify(title, body string) error {
	if len(body) > 240 {
		body = body[:240]
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		q := func(s string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) }
		cmd = exec.Command("osascript", "-e", `display notification "`+q(body)+`" with title "`+q(title)+`" sound name "Ping"`)
	case "windows":
		ps := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
		script := "Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; " +
			"$n = New-Object System.Windows.Forms.NotifyIcon; $n.Icon = [System.Drawing.SystemIcons]::Warning; $n.Visible = $true; " +
			"$n.ShowBalloonTip(6000, '" + ps(title) + "', '" + ps(body) + "', [System.Windows.Forms.ToolTipIcon]::Warning); Start-Sleep 7; $n.Dispose()"
		cmd = exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", script)
	default:
		cmd = exec.Command("notify-send", "-a", "Termwright", title, body)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// withFolderDefaults fills empty session fields from the defaults of the
// session's folder and its parent folders (the nearest folder wins).
func (a *App) withFolderDefaults(s session.Session) session.Session {
	defs := map[string]config.FolderDefault{}
	for _, d := range a.cfg.Get().FolderDefaults {
		defs[strings.Trim(d.Folder, "/")] = d
	}
	if len(defs) == 0 {
		return s
	}
	folder := s.Folder
	if folder == "" && strings.Contains(s.Path, "/") {
		folder = s.Path[:strings.LastIndex(s.Path, "/")]
	}
	set := func(dst *string, v string) {
		if *dst == "" && v != "" {
			*dst = v
		}
	}
	for f := strings.Trim(folder, "/"); ; {
		if d, ok := defs[f]; ok {
			set(&s.Username, d.Username)
			set(&s.DeviceType, d.DeviceType)
			set(&s.JumpSession, d.JumpSession)
			set(&s.IdentityFile, d.IdentityFile)
			set(&s.KeywordSet, d.KeywordSet)
			set(&s.ColorScheme, d.ColorScheme)
		}
		if f == "" {
			break
		}
		if i := strings.LastIndex(f, "/"); i >= 0 {
			f = f[:i]
		} else {
			f = ""
		}
	}
	return s
}

// ImportNetBox imports devices with a primary IP from NetBox
// (/api/dcim/devices/). filter is an optional query string such as
// "site=dc1&role=leaf". An empty token uses the one saved in the keychain;
// a new token is saved.
func (a *App) ImportNetBox(baseURL, token, filter string, opt session.ImportOptions) (*session.ImportResult, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if token == "" {
		token = secrets.Get("netbox-token")
	} else {
		_ = secrets.Set("netbox-token", token)
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
		return nil, errors.New("enter the NetBox URL, e.g. https://netbox.example.com")
	}
	next := baseURL + "/api/dcim/devices/?limit=500&has_primary_ip=true"
	if f := strings.TrimLeft(strings.TrimSpace(filter), "?&"); f != "" {
		next += "&" + f
	}
	client := &http.Client{Timeout: 60 * time.Second}
	var devs []session.NetBoxDevice
	for pages := 0; next != "" && pages < 100; pages++ {
		req, err := http.NewRequest(http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Token "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		var page struct {
			Next    *string                `json:"next"`
			Results []session.NetBoxDevice `json:"results"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&page)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("NetBox: %s", resp.Status)
		}
		if err != nil {
			return nil, fmt.Errorf("NetBox: %w", err)
		}
		devs = append(devs, page.Results...)
		next = ""
		if page.Next != nil {
			next = *page.Next
		}
	}
	return a.store.ImportSessions(session.NetBoxSessions(devs), opt), nil
}

// ImportAWS imports running EC2 instances using the AWS CLI (`aws ec2
// describe-instances`) with the given profile and region.
func (a *App) ImportAWS(profile, region string, usePublic bool, opt session.ImportOptions) (*session.ImportResult, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	aws, err := exec.LookPath("aws")
	if err != nil {
		return nil, errors.New("the AWS CLI (aws) was not found - install it and run 'aws configure' first")
	}
	args := []string{"ec2", "describe-instances", "--output", "json"}
	if profile = strings.TrimSpace(profile); profile != "" {
		args = append(args, "--profile", profile)
	}
	if region = strings.TrimSpace(region); region != "" {
		args = append(args, "--region", region)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, aws, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("aws: %s", strings.TrimSpace(stderr.String()+" "+err.Error()))
	}
	folder := region
	if folder == "" {
		folder = profile
	}
	list, err := session.AWSSessions(out, folder, usePublic)
	if err != nil {
		return nil, err
	}
	return a.store.ImportSessions(list, opt), nil
}

// domReady restores the saved window position.
func (a *App) domReady(ctx context.Context) {
	if a.cfg == nil {
		return
	}
	w := a.cfg.Get().Window
	if !w.Maximised && (w.X != 0 || w.Y != 0) && w.X > -50 && w.Y >= 0 && w.X < 10000 && w.Y < 10000 {
		wruntime.WindowSetPosition(ctx, w.X, w.Y)
	}
}

// saveWindow remembers the window size, position and maximised state.
func (a *App) saveWindow(ctx context.Context) {
	if a.cfg == nil || ctx == nil {
		return
	}
	s := a.cfg.Get()
	s.Window.Maximised = wruntime.WindowIsMaximised(ctx)
	if !s.Window.Maximised && !wruntime.WindowIsMinimised(ctx) {
		s.Window.Width, s.Window.Height = wruntime.WindowGetSize(ctx)
		s.Window.X, s.Window.Y = wruntime.WindowGetPosition(ctx)
	}
	_ = a.cfg.Save(s)
}

// SetDeviceTypePassword saves the password of a device type's login in the
// OS keychain ("" with empty=true saves an intentionally empty password;
// "" with empty=false removes it).
func (a *App) SetDeviceTypePassword(name, password string, empty bool) error {
	key := "devicetype:" + strings.ToLower(strings.TrimSpace(name))
	if password == "" && !empty {
		return secrets.Delete(key)
	}
	return secrets.Set(key, password)
}

// DeviceTypePasswordState reports "set", "empty" or "" (none) for a device type.
func (a *App) DeviceTypePasswordState(name string) string {
	return pwState("devicetype:" + strings.ToLower(strings.TrimSpace(name)))
}

// RenameDeviceTypePassword moves a saved device-type password to a new name.
func (a *App) RenameDeviceTypePassword(oldName, newName string) {
	oldKey, newKey := "devicetype:"+strings.ToLower(oldName), "devicetype:"+strings.ToLower(newName)
	if oldKey == newKey {
		return
	}
	if pw, ok := secrets.Lookup(oldKey); ok {
		if secrets.Set(newKey, pw) == nil {
			_ = secrets.Delete(oldKey)
		}
	}
}

// SetSessionPassword saves (or with "" and empty=false removes) a session's password.
func (a *App) SetSessionPassword(path, password string, empty bool) error {
	if password == "" && !empty {
		return secrets.Delete(path)
	}
	return secrets.Set(path, password)
}

// SessionPasswordState reports "set", "empty" or "" for a saved session.
func (a *App) SessionPasswordState(path string) string { return pwState(path) }

func pwState(key string) string {
	pw, ok := secrets.Lookup(key)
	switch {
	case !ok:
		return ""
	case pw == "":
		return "empty"
	case secretref.IsRef(pw):
		return "ref:" + pw // a password-manager reference is not secret, so it can be shown
	}
	return "set"
}

// ---- password managers ----

func (a *App) resolver() *secretref.Resolver {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.refs == nil {
		a.refs = &secretref.Resolver{Opt: secretref.Options{
			VaultToken: func() string { return secrets.Get("vault-token") },
		}}
	}
	a.refs.Opt.VaultAddr = a.cfg.Get().VaultAddr
	return a.refs
}

// secret returns a saved password; a stored password-manager reference
// (op://, bw://, vault://, cmd://) is resolved at this moment. On failure the
// user sees why and is asked for the password as usual.
func (a *App) secret(key string) (string, bool) {
	v, ok := secrets.Lookup(key)
	if !ok || !secretref.IsRef(v) {
		return v, ok
	}
	pw, err := a.resolveRef(v)
	if err != nil {
		emitter{a}.Emit("toast", map[string]string{"level": "error", "message": "Password manager: " + err.Error()})
		return "", false
	}
	return pw, true
}

func (a *App) resolveRef(ref string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return a.resolver().Resolve(ctx, ref, func(title, msg string) (string, error) {
		r, err := a.terms.Prompts().Ask(ctx, term.Prompt{Kind: "credentials", Title: title, Message: msg,
			Fields: []term.PromptField{{Label: "Master password", Secret: true}}})
		if err != nil {
			return "", err
		}
		return r.Values[0], nil
	})
}

// TestSecretRef fetches a password-manager reference and reports how long the
// secret is (the secret itself never goes to the UI).
func (a *App) TestSecretRef(ref string) (string, error) {
	if !secretref.IsRef(ref) {
		return "", errors.New("a reference starts with op://, bw://, vault:// or cmd://")
	}
	a.resolver().Forget()
	pw, err := a.resolveRef(ref)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("OK - got a %d-character secret", len([]rune(pw))), nil
}

// ForgetCachedSecrets clears secrets fetched from password managers (and a Bitwarden unlock).
func (a *App) ForgetCachedSecrets() { a.resolver().Forget() }

// SetVaultToken saves (or with "" removes) the HashiCorp Vault token in the OS keychain.
func (a *App) SetVaultToken(token string) error {
	if token == "" {
		return secrets.Delete("vault-token")
	}
	return secrets.Set("vault-token", token)
}

// HasVaultToken reports whether a Vault token is saved.
func (a *App) HasVaultToken() bool { _, ok := secrets.Lookup("vault-token"); return ok }

// ---- SSH key passphrases ----

// KeyPassphraseInfo is a private key with a passphrase saved in the OS keychain.
type KeyPassphraseInfo struct {
	Path     string   `json:"path"`
	Exists   bool     `json:"exists"`   // the key file is still there
	Sessions []string `json:"sessions"` // sessions using it as identity file
}

// keyPassphrasePaths lists key files that may have a saved passphrase: the
// remembered list plus every session's identity file (older saves).
func (a *App) keyPassphrasePaths() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range a.cfg.Get().KeyPassphrases {
		add(p)
	}
	if a.store != nil {
		if paths, err := a.store.AllSessions(nil); err == nil {
			for _, sp := range paths {
				if s, err := a.store.Get(sp); err == nil && s.IdentityFile != "" {
					add(sshkeyPath(s.IdentityFile))
				}
			}
		}
	}
	return out
}

// KeyPassphrases lists the key files whose passphrase is saved.
func (a *App) KeyPassphrases() ([]KeyPassphraseInfo, error) {
	if a.isLocked() {
		return nil, errors.New("unlock saved passwords with the master password first")
	}
	users := map[string][]string{}
	if a.store != nil {
		if paths, err := a.store.AllSessions(nil); err == nil {
			for _, sp := range paths {
				if s, err := a.store.Get(sp); err == nil && s.IdentityFile != "" {
					k := sshkeyPath(s.IdentityFile)
					users[k] = append(users[k], sp)
				}
			}
		}
	}
	out := []KeyPassphraseInfo{}
	for _, p := range a.keyPassphrasePaths() {
		if _, ok := secrets.Lookup("keypass:" + p); !ok {
			continue
		}
		_, err := os.Stat(p)
		out = append(out, KeyPassphraseInfo{Path: p, Exists: err == nil, Sessions: users[p]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// SetKeyPassphrase checks the passphrase against the key file and saves it.
func (a *App) SetKeyPassphrase(path, passphrase string) error {
	path = sshkeyPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := sshkey.ParseKey(data, passphrase); err != nil {
		return fmt.Errorf("the passphrase does not unlock %s: %w", filepath.Base(path), err)
	}
	return a.saveKeyPassphrase(path, passphrase)
}

// ForgetKeyPassphrase removes a saved key passphrase.
func (a *App) ForgetKeyPassphrase(path string) error {
	err := secrets.Delete("keypass:" + path)
	s := a.cfg.Get()
	s.KeyPassphrases = slices.DeleteFunc(slices.Clone(s.KeyPassphrases), func(p string) bool { return p == path })
	if e := a.cfg.Save(s); e != nil {
		return e
	}
	return err
}

func (a *App) saveKeyPassphrase(path, passphrase string) error {
	if err := secrets.Set("keypass:"+path, passphrase); err != nil {
		return err
	}
	s := a.cfg.Get()
	if !slices.Contains(s.KeyPassphrases, path) {
		s.KeyPassphrases = append(slices.Clone(s.KeyPassphrases), path)
		return a.cfg.Save(s)
	}
	return nil
}

// ---- rendered screen for scripts (crt.Screen.Get / CurrentRow) ----

var screenSeq atomic.Uint64

// screenSnapshot asks the terminal view for what it shows right now.
func (a *App) screenSnapshot(tabID string) *term.ScreenSnap {
	if a.ctx == nil {
		return nil
	}
	id := fmt.Sprintf("s%d", screenSeq.Add(1))
	ch := make(chan *term.ScreenSnap, 1)
	a.screens.Store(id, ch)
	defer a.screens.Delete(id)
	emitter{a}.Emit("screen:request", map[string]string{"id": id, "tabId": tabID})
	select {
	case s := <-ch:
		return s
	case <-time.After(1500 * time.Millisecond):
		return nil
	}
}

// ScreenReply is called by the terminal view with a requested screen (nil when it has none).
func (a *App) ScreenReply(id string, snap *term.ScreenSnap) {
	if v, ok := a.screens.Load(id); ok {
		select {
		case v.(chan *term.ScreenSnap) <- snap:
		default:
		}
	}
}
