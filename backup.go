package main

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupConfig writes a zip of the settings, sessions and other configuration
// (not logs, not passwords - those stay in the OS keychain) to
// Documents/Termwright Backups and returns its path. Used before installing
// an update, especially a nightly build.
func (a *App) BackupConfig() (string, error) {
	s := a.cfg.Get()
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Documents", "Termwright Backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "termwright-backup-"+time.Now().Format("20060102-150405")+".zip")
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(f)
	skip := []string{filepath.Clean(s.LogDir), filepath.Clean(dir)}
	add := func(root, prefix string) error {
		return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable entries are left out
			}
			for _, x := range skip {
				if x != "." && p == x {
					return filepath.SkipDir
				}
			}
			if d.IsDir() {
				switch strings.ToLower(d.Name()) {
				case "logs", "log", "cache", "tmp":
					return filepath.SkipDir
				}
				return nil
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > 20<<20 {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return nil
			}
			hdr, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			hdr.Name, hdr.Method = filepath.ToSlash(filepath.Join(prefix, rel)), zip.Deflate
			w, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			src, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer src.Close()
			_, err = io.Copy(w, src)
			return err
		})
	}
	data := a.cfg.DataDir()
	err = add(data, "Termwright")
	if sd := filepath.Clean(s.SessionsDir); err == nil && sd != "." && !strings.HasPrefix(sd, filepath.Clean(data)+string(filepath.Separator)) {
		err = add(sd, "Sessions")
	}
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	return dest, nil
}
