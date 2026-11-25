package app

import (
	"os"
	"path/filepath"
	"strings"
)

// Where Termwright keeps things - for Settings → Files & Folders. Folders that cannot be chosen are shown, so
// they can be found (history, known hosts, the settings file).

func homeJoin(p ...string) string {
	h, _ := os.UserHomeDir()
	return filepath.Join(append([]string{h}, p...)...)
}

func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return homeJoin(p[1:])
	}
	return p
}

// ExportDir is the folder test exports (Playwright projects, scripts) are saved in.
func (a *App) ExportDir() string {
	if d := strings.TrimSpace(a.cfg.Get().ExportDir); d != "" {
		return expandTilde(d)
	}
	return homeJoin("Documents", "Termwright Exports")
}

// LocalStartDir is this computer's side of SFTP tabs and where Files panel downloads go: the chosen folder when
// it exists, else the home folder.
func (a *App) LocalStartDir() string {
	if d := strings.TrimSpace(a.cfg.Get().SFTPLocalDir); d != "" {
		d = expandTilde(d)
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
	}
	h, _ := os.UserHomeDir()
	return h
}

// DataPaths are the places Termwright reads and writes, by name.
func (a *App) DataPaths() map[string]string {
	data := a.cfg.DataDir()
	st := a.cfg.Get()
	out := map[string]string{
		"data": data, "settings": filepath.Join(data, "settings.json"), "sessions": st.SessionsDir,
		"history": filepath.Join(data, "history.json"), "apiHistory": a.httpHistFile(), "labHistory": a.emuHistPath(),
		"knownHosts": filepath.Join(data, "known_hosts"), "logs": st.LogDir, "recordings": a.RecordingDir(),
		"backups": a.BackupDir(), "collected": a.CollectDir(), "checklists": a.ChecklistDir(), "downloads": st.DownloadDir,
		"exports": a.ExportDir(), "sftpLocal": a.LocalStartDir(),
	}
	if f, err := a.testsFolder(); err == nil {
		out["tests"] = f.Root
		out["reports"] = filepath.Join(f.Root, "reports")
		out["testHistory"] = filepath.Join(f.Root, "reports", "history.jsonl")
	}
	return out
}

// OpenFolder shows a folder (made when missing) or a file's folder in Finder / Explorer.
func (a *App) OpenFolder(p string) error {
	p = expandTilde(p)
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return a.ShowInFolder(p)
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return err
	}
	return a.OpenUpdate(p)
}
