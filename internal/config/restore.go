package config

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// A backup zip (BackupConfig) holds "Termwright/…" (the data folder) and,
// when the sessions folder is elsewhere, "Sessions/…". Restoring unpacks it
// into dataDir/restore-pending; the next start copies it over the data folder
// before the settings are read, so nothing running writes over it.

const restoreDir = "restore-pending"

// RestoreInfo describes a backup before it is restored.
type RestoreInfo struct {
	Files    int `json:"files"`
	Sessions int `json:"sessions"`
}

const (
	maxRestoreFile  = 20 << 20
	maxRestoreTotal = 1 << 30
)

// StageRestore checks a backup and unpacks it for the next start.
func StageRestore(dataDir, zipPath string) (RestoreInfo, error) {
	var info RestoreInfo
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return info, errors.New("not a Termwright backup (it is not a zip file)")
	}
	defer zr.Close()
	ok := false
	for _, f := range zr.File {
		if f.Name == "Termwright/settings.json" {
			ok = true
		}
	}
	if !ok {
		return info, errors.New("not a Termwright backup (no Termwright/settings.json in it)")
	}
	stage := filepath.Join(dataDir, restoreDir)
	if err := os.RemoveAll(stage); err != nil { // an earlier restore that was never applied
		return info, err
	}
	var total int64
	for _, f := range zr.File {
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "/") || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			continue
		}
		top, rest, _ := strings.Cut(name, "/")
		if (top != "Termwright" && top != "Sessions") || rest == "" || strings.HasPrefix(rest, restoreDir+"/") {
			continue
		}
		if f.UncompressedSize64 > maxRestoreFile {
			continue
		}
		if total += int64(f.UncompressedSize64); total > maxRestoreTotal {
			return info, errors.New("the backup is too large")
		}
		dst := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return info, err
		}
		if err := copyZipFile(f, dst); err != nil {
			return info, err
		}
		info.Files++
		if top == "Sessions" || strings.HasPrefix(rest, "Sessions/") || strings.HasPrefix(rest, "sessions/") {
			info.Sessions++
		}
	}
	return info, nil
}

func copyZipFile(f *zip.File, dst string) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, io.LimitReader(r, maxRestoreFile+1)); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// PendingRestore reports whether a restore waits for the next start.
func PendingRestore(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, restoreDir, "Termwright", "settings.json"))
	return err == nil
}

// ApplyPendingRestore copies a staged backup over the data folder (and the
// sessions folder the restored settings name). Files that are not in the
// backup stay. Call it before Load.
func ApplyPendingRestore(dataDir string) error {
	stage := filepath.Join(dataDir, restoreDir)
	if !PendingRestore(dataDir) {
		return nil
	}
	sessions := Defaults(dataDir).SessionsDir
	if b, err := os.ReadFile(filepath.Join(stage, "Termwright", "settings.json")); err == nil {
		var s struct {
			SessionsDir string `json:"sessionsDir"`
		}
		if json.Unmarshal(b, &s) == nil && s.SessionsDir != "" {
			sessions = s.SessionsDir
		}
	}
	err := filepath.WalkDir(stage, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(stage, p)
		top, rest, _ := strings.Cut(filepath.ToSlash(rel), "/")
		var dst string
		switch top {
		case "Termwright":
			dst = filepath.Join(dataDir, filepath.FromSlash(rest))
		case "Sessions":
			dst = filepath.Join(sessions, filepath.FromSlash(rest))
		default:
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o600)
	})
	if err != nil {
		return fmt.Errorf("restoring the backup: %w", err)
	}
	return os.RemoveAll(stage)
}
