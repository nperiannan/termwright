package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

// Sync between your own computers through a folder they share (OneDrive,
// iCloud Drive, Dropbox, a network share): each Termwright writes its sessions
// and settings there as "<its id>.zip" with "<its id>.json", and takes in what
// the others wrote. Passwords never go there (they stay in each computer's
// keychain); team folders come from the team server and are left out.
//
//   - A session changed on one computer is taken by the others, unless it was
//     changed there later.
//   - A session deleted (or moved away) on one computer is deleted on the
//     others, unless it was changed there after that.
//   - Accounts, device types, folder defaults and the library (commands,
//     checklists, buttons…) are merged; Global Options only when chosen.

const syncDirName = "Termwright Sync"

// keep deletions this long, so a computer that was off for a while still learns of them
const syncKeepGone = 90 * 24 * time.Hour

type syncMeta struct {
	ID      string            `json:"id"`
	Machine string            `json:"machine"`
	App     string            `json:"app"`
	At      string            `json:"at"`   // when written (RFC3339)
	Hash    string            `json:"hash"` // of the sessions and settings in the zip
	Gone    map[string]string `json:"gone"` // session path → when it was deleted there
}

type syncState struct {
	Written string            `json:"written"` // hash of what this computer wrote last
	Applied map[string]string `json:"applied"` // peer id → hash taken in
	Known   []string          `json:"known"`   // the sessions at the last sync (to see deletions)
	Gone    map[string]string `json:"gone"`
}

// SyncStatus is shown in Global Options.
type SyncStatus struct {
	Folder string   `json:"folder"`
	Last   string   `json:"last"`  // RFC3339 of the last sync
	Peers  []string `json:"peers"` // the other computers, "name (when)"
	Took   int      `json:"took"`  // sessions taken in at the last sync
	Error  string   `json:"error"`
}

var (
	syncMu     sync.Mutex
	syncStatus SyncStatus
	syncHashes = map[string]syncFileHash{} // per session file, while it does not change (syncMu held)
)

type syncFileHash struct {
	mod  time.Time
	size int64
	sum  [32]byte
}

// syncLoop syncs every 30 seconds while a sync folder is set.
func (a *App) syncLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		if a.cfg != nil && a.cfg.Get().SyncFolder != "" {
			a.syncOnce()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// SyncNow syncs at once and returns the status.
func (a *App) SyncNow() SyncStatus {
	if err := a.ready(); err != nil {
		return SyncStatus{Error: err.Error()}
	}
	a.syncOnce()
	return a.SyncState()
}

// SyncState is the last sync's status.
func (a *App) SyncState() SyncStatus {
	syncMu.Lock()
	defer syncMu.Unlock()
	return syncStatus
}

func (a *App) syncOnce() {
	syncMu.Lock()
	defer syncMu.Unlock()
	st, err := a.syncRun()
	if err != nil {
		st.Error = err.Error()
	}
	st.Last = time.Now().UTC().Format(time.RFC3339)
	syncStatus = st
	if a.ctx != nil && st.Took > 0 {
		wruntime.EventsEmit(a.ctx, "sessions:changed", nil)
	}
}

func (a *App) syncRun() (SyncStatus, error) {
	cfg := a.cfg.Get()
	out := SyncStatus{Folder: cfg.SyncFolder}
	if cfg.SyncFolder == "" {
		return out, nil
	}
	if fi, err := os.Stat(cfg.SyncFolder); err != nil || !fi.IsDir() {
		return out, errors.New("the sync folder is not there (is the drive or cloud folder available?)")
	}
	dir := filepath.Join(cfg.SyncFolder, syncDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return out, err
	}
	if cfg.SyncID == "" {
		cfg.SyncID = randHex(8)
		if err := a.cfg.Save(cfg); err != nil {
			return out, err
		}
	}
	id := cfg.SyncID
	stFile := filepath.Join(a.cfg.DataDir(), "sync-state.json")
	var state syncState
	if b, err := os.ReadFile(stFile); err == nil {
		_ = json.Unmarshal(b, &state)
	}
	if state.Applied == nil {
		state.Applied = map[string]string{}
	}
	if state.Gone == nil {
		state.Gone = map[string]string{}
	}
	now := time.Now().UTC()

	// deletions here since the last sync
	paths := a.syncPaths()
	have := map[string]bool{}
	for _, p := range paths {
		have[p] = true
		delete(state.Gone, p) // there again
	}
	for _, p := range state.Known {
		if !have[p] {
			state.Gone[p] = now.Format(time.RFC3339Nano)
		}
	}
	for p, at := range state.Gone {
		if t, err := time.Parse(time.RFC3339, at); err != nil || now.Sub(t) > syncKeepGone {
			delete(state.Gone, p)
		}
	}

	// what the other computers wrote
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || strings.TrimSuffix(name, ".json") == id {
			continue
		}
		var m syncMeta
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || json.Unmarshal(b, &m) != nil || m.ID == "" {
			continue
		}
		at, _ := time.Parse(time.RFC3339, m.At)
		out.Peers = append(out.Peers, m.Machine+" ("+m.At+")")
		if m.Hash == state.Applied[m.ID] {
			continue
		}
		n, err := a.syncTake(filepath.Join(dir, m.ID+".zip"), m, at, &state, cfg.SyncOptions)
		if err != nil {
			return out, errors.New(m.Machine + ": " + err.Error())
		}
		out.Took += n
		state.Applied[m.ID] = m.Hash
	}

	// what this computer has now
	paths = a.syncPaths()
	h, err := a.syncHash(paths)
	if err != nil {
		return out, err
	}
	zipFile, metaFile := filepath.Join(dir, id+".zip"), filepath.Join(dir, id+".json")
	if _, err := os.Stat(metaFile); h != state.Written || err != nil {
		tmp := zipFile + ".tmp"
		if _, err := a.ExportSessions(paths, tmp, "zip", true); err != nil {
			_ = os.Remove(tmp)
			return out, err
		}
		if err := os.Rename(tmp, zipFile); err != nil {
			return out, err
		}
		host, _ := os.Hostname()
		_, _, machine := a.teamIdentity()
		if machine == "" {
			machine = host
		}
		b, _ := json.MarshalIndent(syncMeta{ID: id, Machine: machine, App: Version, At: now.Format(time.RFC3339Nano), Hash: h, Gone: state.Gone}, "", "  ")
		if err := os.WriteFile(metaFile+".tmp", b, 0o600); err != nil {
			return out, err
		}
		if err := os.Rename(metaFile+".tmp", metaFile); err != nil {
			return out, err
		}
		state.Written = h
	}
	state.Known = paths
	b, _ := json.MarshalIndent(state, "", "  ")
	return out, os.WriteFile(stFile, b, 0o600)
}

// syncTake takes in another computer's sessions and settings; it returns how
// many sessions were added, changed or deleted here.
func (a *App) syncTake(zip string, m syncMeta, at time.Time, state *syncState, options bool) (int, error) {
	n := 0
	// its deletions, unless changed here after them
	for p, when := range m.Gone {
		t, err := time.Parse(time.RFC3339, when)
		if err != nil || !a.store.Exists(p) || a.syncTeamPath(p) {
			continue
		}
		if fi, err := os.Stat(a.syncFile(p)); err == nil && fi.ModTime().After(t) {
			continue
		}
		if a.store.Delete(p) == nil {
			n++
			state.Gone[p] = when
		}
	}
	if _, err := os.Stat(zip); err != nil {
		return n, nil // the zip is not synced to this computer yet: next time
	}
	tmp, err := os.MkdirTemp("", "termwright-sync-")
	if err != nil {
		return n, err
	}
	defer os.RemoveAll(tmp)
	ts, err := session.NewStore(tmp)
	if err != nil {
		return n, err
	}
	if _, err := ts.Import(zip, session.ImportOptions{Conflict: "overwrite"}); err != nil {
		return n, err
	}
	theirs, _ := ts.AllSessions(nil)
	for _, p := range theirs {
		if a.syncTeamPath(p) {
			continue
		}
		if when, gone := state.Gone[p]; gone {
			if t, err := time.Parse(time.RFC3339, when); err == nil && t.After(at) {
				continue // deleted here after they wrote it
			}
		}
		tb, err := os.ReadFile(filepath.Join(tmp, filepath.FromSlash(p)+".ini"))
		if err != nil {
			continue
		}
		if fi, err := os.Stat(a.syncFile(p)); err == nil {
			mine, _ := os.ReadFile(a.syncFile(p))
			if bytes.Equal(mine, tb) || fi.ModTime().After(at) {
				continue // the same, or changed here later
			}
		}
		f, err := ts.ReadRaw(p)
		if err != nil {
			continue
		}
		if err := a.store.WriteRaw(p, f); err == nil {
			n++
			delete(state.Gone, p)
		}
	}
	// accounts, device types, folder defaults and the library; Global Options when chosen
	if data, ok := session.ReadExportFile(zip, config.SettingsFileName); ok {
		parts := []string{config.PartLogins, config.PartLibrary}
		if options {
			parts = append(parts, config.PartOptions)
		}
		cur := a.cfg.Get()
		next, _, err := config.ImportSettings(cur, data, parts, "", func(p string) string { return p })
		if err == nil {
			// this computer's own sync settings stay
			next.SyncFolder, next.SyncID, next.SyncOptions = cur.SyncFolder, cur.SyncID, cur.SyncOptions
			x, _ := json.Marshal(cur)
			y, _ := json.Marshal(next)
			if !bytes.Equal(x, y) {
				_ = a.cfg.Save(next)
			}
		}
	}
	return n, nil
}

// syncPaths: this computer's sessions, without the team folders.
func (a *App) syncPaths() []string {
	all, _ := a.store.AllSessions(nil)
	out := all[:0]
	for _, p := range all {
		if !a.syncTeamPath(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// syncTeamPath: a session of a team folder (it comes from the team server).
func (a *App) syncTeamPath(p string) bool {
	root := strings.SplitN(p, "/", 2)[0]
	for _, t := range a.cfg.Get().Teams {
		if strings.EqualFold(root, teamRoot(t)) || strings.EqualFold(root, t.Name) {
			return true
		}
	}
	return false
}

func (a *App) syncFile(p string) string {
	return filepath.Join(a.store.Root(), filepath.FromSlash(p)+".ini")
}

// syncHash is a hash of the sessions' files and the settings that sync.
func (a *App) syncHash(paths []string) (string, error) {
	h := sha256.New()
	next := map[string]syncFileHash{}
	for _, p := range paths {
		// a file that did not change is not read again (thousands of sessions, every half minute)
		fi, err := os.Stat(a.syncFile(p))
		if err != nil {
			continue
		}
		fh, ok := syncHashes[p]
		if !ok || !fh.mod.Equal(fi.ModTime()) || fh.size != fi.Size() {
			b, err := os.ReadFile(a.syncFile(p))
			if err != nil {
				continue
			}
			sum := sha256.Sum256(b)
			fh = syncFileHash{mod: fi.ModTime(), size: fi.Size(), sum: sum}
		}
		next[p] = fh
		h.Write([]byte(p + "\x00"))
		h.Write(fh.sum[:])
	}
	syncHashes = next
	data, err := config.ExportSettings(a.cfg.Get(), "")
	if err != nil {
		return "", err
	}
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SetSyncFolder turns sync on (a folder) or off ("").
func (a *App) SetSyncFolder(folder string, options bool) (SyncStatus, error) {
	if err := a.ready(); err != nil {
		return SyncStatus{}, err
	}
	s := a.cfg.Get()
	s.SyncFolder, s.SyncOptions = strings.TrimSpace(folder), options
	if err := a.cfg.Save(s); err != nil {
		return SyncStatus{}, err
	}
	if s.SyncFolder == "" {
		syncMu.Lock()
		syncStatus = SyncStatus{}
		syncMu.Unlock()
		return SyncStatus{}, nil
	}
	return a.SyncNow(), nil
}
