package session

import (
	"os"
	"path/filepath"
)

// ImportSource is a place on this computer where another terminal program
// keeps its sessions, ready to import as it is.
type ImportSource struct {
	App  string `json:"app"`  // "SecureCRT", "PuTTY", "MobaXterm", "SuperPuTTY", "OpenSSH"
	Path string `json:"path"` // what Import reads (a folder or a file)
	What string `json:"what"` // e.g. "configuration folder"
}

// FindImportSources lists the session stores of other terminal programs in
// their usual places. home is the user's home folder, appData the roaming
// application data folder on Windows ("" elsewhere), goos the system.
func FindImportSources(home, appData, goos string) []ImportSource {
	var out []ImportSource
	add := func(app, what string, ok func(string) bool, paths ...string) {
		for _, p := range paths {
			if p != "" && ok(p) {
				out = append(out, ImportSource{App: app, Path: p, What: what})
				return
			}
		}
	}
	j := filepath.Join
	// SecureCRT: the Config folder (with a Sessions folder inside)
	crt := []string{}
	switch goos {
	case "windows":
		if appData != "" {
			crt = append(crt, j(appData, "VanDyke", "Config"))
		}
	case "darwin":
		crt = append(crt, j(home, "Library", "Application Support", "VanDyke", "SecureCRT", "Config"))
	default:
		crt = append(crt, j(home, ".vandyke", "SecureCRT", "Config"))
	}
	add("SecureCRT", "configuration folder", func(p string) bool { return isDirectory(j(p, "Sessions")) }, crt...)
	// PuTTY / KiTTY on Linux and macOS: one file per session
	if goos != "windows" {
		add("PuTTY", "sessions folder", hasFiles, j(home, ".putty", "sessions"))
	}
	if goos == "windows" {
		docs := j(home, "Documents")
		add("SuperPuTTY", "Sessions.xml", isFile, j(docs, "SuperPuTTY", "Sessions.xml"))
		mx := []string{j(docs, "MobaXterm", "MobaXterm.ini")}
		if appData != "" {
			mx = append(mx, j(appData, "MobaXterm", "MobaXterm.ini"))
		}
		add("MobaXterm", "MobaXterm.ini", isFile, mx...)
	}
	add("OpenSSH", "~/.ssh/config", isFile, j(home, ".ssh", "config"))
	return out
}

func isDirectory(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

func hasFiles(p string) bool {
	es, err := os.ReadDir(p)
	if err != nil {
		return false
	}
	for _, e := range es {
		if !e.IsDir() {
			return true
		}
	}
	return false
}
