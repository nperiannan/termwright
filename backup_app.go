package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/backup"
	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

// BackupResult is the outcome of one session in a backup run.
type BackupResult struct {
	RunID   string `json:"runId"`
	Path    string `json:"path"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Changed bool   `json:"changed"`
	File    string `json:"file,omitempty"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
}

var (
	backupMu   sync.Mutex
	backupRuns = map[string]context.CancelFunc{}
	backupBusy = map[string]bool{} // session paths being backed up
)

// BackupDir is the folder the copies are saved in.
func (a *App) BackupDir() string {
	if d := strings.TrimSpace(a.cfg.Get().BackupDir); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Documents", "Termwright Config Backups")
}

func (a *App) backups() *backup.Store { return &backup.Store{Dir: a.BackupDir()} }

// BackupSessions expands targets (session paths, folders ending in "/") to
// the sessions that can be backed up, sorted.
func (a *App) BackupSessions(targets []string) ([]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	root, err := a.store.Tree()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var walk func(n *session.Node, all bool)
	want := map[string]bool{}
	for _, t := range targets {
		want[strings.TrimSuffix(t, "/")] = true
	}
	walk = func(n *session.Node, all bool) {
		for _, c := range n.Children {
			in := all || want[c.Path]
			if c.IsFolder {
				walk(c, in)
			} else if in && backupable(c.Protocol) && c.OpenAs != "sftp" {
				seen[c.Path] = true
			}
		}
	}
	walk(root, false)
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func backupable(proto string) bool {
	switch proto {
	case session.ProtoSSH2, "SSH", session.ProtoTelnet, session.ProtoRaw, session.ProtoSerial:
		return true
	}
	return false
}

// BackupRun backs up the configuration of targets in the background (4 at a
// time) and reports each session with "backup:progress" and the end with
// "backup:done". job names the backup job ("" = on demand).
func (a *App) BackupRun(job string, targets []string) (string, error) {
	paths, err := a.BackupSessions(targets)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", errors.New("no sessions to back up (serial, SSH, Telnet and Raw sessions can be backed up)")
	}
	var spec config.BackupJob
	for _, j := range a.cfg.Get().BackupJobs {
		if j.Name == job {
			spec = j
		}
	}
	id := randHex(6)
	ctx, cancel := context.WithCancel(context.Background())
	backupMu.Lock()
	backupRuns[id] = cancel
	backupMu.Unlock()
	go func() {
		defer func() {
			backupMu.Lock()
			delete(backupRuns, id)
			backupMu.Unlock()
			cancel()
		}()
		st := a.backups()
		var mu sync.Mutex
		done, failed, changed := 0, 0, 0
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		for _, p := range paths {
			if ctx.Err() != nil {
				break
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(p string) {
				defer func() { <-sem; wg.Done() }()
				r := a.backupOne(ctx, st, p, spec)
				mu.Lock()
				done++
				if !r.OK {
					failed++
				} else if r.Changed {
					changed++
				}
				r.RunID, r.Done, r.Total = id, done, len(paths)
				mu.Unlock()
				emitter{a}.Emit("backup:progress", r)
			}(p)
		}
		wg.Wait()
		if job != "" {
			_ = st.SetJobRun(job, time.Now())
		}
		emitter{a}.Emit("backup:done", map[string]any{"runId": id, "job": job, "total": len(paths), "done": done,
			"failed": failed, "changed": changed, "cancelled": ctx.Err() != nil})
	}()
	return id, nil
}

// BackupCancel stops a running backup (devices in progress finish their command).
func (a *App) BackupCancel(runID string) {
	backupMu.Lock()
	if c := backupRuns[runID]; c != nil {
		c()
	}
	backupMu.Unlock()
}

func (a *App) backupOne(ctx context.Context, st *backup.Store, p string, job config.BackupJob) BackupResult {
	res := BackupResult{Path: p}
	backupMu.Lock()
	if backupBusy[p] {
		backupMu.Unlock()
		res.Error = "already being backed up"
		return res
	}
	backupBusy[p] = true
	backupMu.Unlock()
	defer func() { backupMu.Lock(); delete(backupBusy, p); backupMu.Unlock() }()
	finish := func(err error, file string, changed bool) BackupResult {
		files, _ := st.Files(p)
		s := backup.Status{Time: time.Now().Format(time.RFC3339), OK: err == nil, Changed: changed, File: file, Copies: len(files)}
		if err != nil {
			s.Error = err.Error()
			if prev, ok := st.Status()[p]; ok && prev.File != "" {
				s.File = prev.File // keep pointing at the last good copy
			}
		}
		_ = st.SetStatus(p, s)
		res.OK, res.Changed, res.File = err == nil, changed, s.File
		if err != nil {
			res.Error = err.Error()
		}
		return res
	}
	s, err := a.store.Get(p)
	if err != nil {
		return finish(err, "", false)
	}
	s = a.withFolderDefaults(s)
	var cmds []string
	prompt := ""
	if s.DeviceType != "" && a.terms.DeviceType != nil {
		if dt, ok := a.terms.DeviceType(s.DeviceType); ok {
			cmds, prompt = dt.Backup(), dt.Prompt
		}
	}
	if len(cmds) == 0 {
		for _, l := range strings.Split(job.Commands, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				cmds = append(cmds, l)
			}
		}
	}
	if len(cmds) == 0 {
		if s.DeviceType == "" {
			return finish(errors.New("no backup command: give the session a device type, or set the job's commands"), "", false)
		}
		return finish(fmt.Errorf("no backup command for device type %s (Global Options → Device Types)", s.DeviceType), "", false)
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := a.terms.Capture(cctx, s, cmds, prompt)
	if err != nil {
		return finish(err, "", false)
	}
	if strings.TrimSpace(out) == "" {
		return finish(errors.New("the device returned no output"), "", false)
	}
	file, changed, err := st.Save(p, out, !job.SaveAll, job.Keep, time.Now())
	return finish(err, file, changed)
}

// BackupStatus returns the last backup result of every session.
func (a *App) BackupStatus() map[string]backup.Status { return a.backups().Status() }

// BackupFiles lists a session's saved copies, newest first.
func (a *App) BackupFiles(path string) ([]backup.File, error) { return a.backups().Files(path) }

// BackupJobRuns returns when each job last ran.
func (a *App) BackupJobRuns() map[string]string {
	st := a.backups()
	out := map[string]string{}
	for _, j := range a.cfg.Get().BackupJobs {
		if t := st.JobRun(j.Name); !t.IsZero() {
			out[j.Name] = t.Format(time.RFC3339)
		}
	}
	return out
}

// ShowInFolder opens the system file manager at a backup file or folder.
func (a *App) ShowInFolder(path string) error {
	if path == "" {
		path = a.BackupDir()
		_ = os.MkdirAll(path, 0o700)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if fi.IsDir() {
			cmd = exec.Command("open", path)
		} else {
			cmd = exec.Command("open", "-R", path)
		}
	case "windows":
		if fi.IsDir() {
			cmd = exec.Command("explorer", path)
		} else {
			cmd = exec.Command("explorer", "/select,", path)
		}
	default:
		if !fi.IsDir() {
			path = filepath.Dir(path)
		}
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// backupScheduler runs scheduled backup jobs while Termwright is open.
func (a *App) backupScheduler(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if a.ready() != nil {
				continue
			}
			st := a.backups()
			for _, j := range a.cfg.Get().BackupJobs {
				if backup.Due(j.Schedule, j.At, st.JobRun(j.Name), now) {
					_ = st.SetJobRun(j.Name, now) // once per slot, even if starting fails
					if _, err := a.BackupRun(j.Name, j.Targets); err != nil {
						emitter{a}.Emit("toast", map[string]string{"level": "error", "message": "Backup " + j.Name + ": " + err.Error()})
					}
				}
			}
		}
	}
}
