package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Files panel: a remote file opened by double-click is copied to a private
// temporary folder and opened with the program the system uses for it; the
// panel watches the copy and offers to upload saved changes.

// programExt are file types the system would run instead of open; they are
// never opened from a server.
var programExt = map[string]bool{
	".exe": true, ".com": true, ".bat": true, ".cmd": true, ".msi": true, ".scr": true, ".pif": true, ".ps1": true,
	".vbs": true, ".vbe": true, ".js": true, ".jse": true, ".wsf": true, ".wsh": true, ".hta": true, ".lnk": true,
	".jar": true, ".app": true, ".command": true, ".tool": true, ".pkg": true, ".dmg": true, ".workflow": true,
	".desktop": true, ".appimage": true, ".run": true, ".reg": true, ".cpl": true, ".msc": true, ".url": true, ".webloc": true,
}

// OpenedFile is a remote file opened on this computer.
type OpenedFile struct {
	Local string `json:"local"`
	Mod   int64  `json:"mod"` // modification time of the copy (ms)
}

// SFTPOpenFile downloads a remote file to a temporary folder and opens it
// with the system's program for its type.
func (a *App) SFTPOpenFile(id, remote string) (OpenedFile, error) {
	name := path.Base(remote)
	if name == "." || name == ".." || name == "/" || strings.ContainsAny(name, `/\`) {
		return OpenedFile{}, fmt.Errorf("cannot open %q", remote)
	}
	if programExt[strings.ToLower(filepath.Ext(name))] {
		return OpenedFile{}, fmt.Errorf("%s is a program or shortcut - Termwright does not run files from a server; use Download instead", name)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	dir := filepath.Join(os.TempDir(), "termwright-open", hex.EncodeToString(b))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return OpenedFile{}, err
	}
	local := filepath.Join(dir, name)
	if err := a.terms.SFTPDownloadFile(id, remote, local); err != nil {
		return OpenedFile{}, err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", local)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", local)
	default:
		cmd = exec.Command("xdg-open", local)
	}
	if err := cmd.Start(); err != nil {
		return OpenedFile{}, err
	}
	go func() { _ = cmd.Wait() }()
	return OpenedFile{Local: local, Mod: modMs(local)}, nil
}

// OpenedFileMod returns the modification time (ms) of a file opened with
// SFTPOpenFile, 0 when it is gone.
func (a *App) OpenedFileMod(local string) int64 {
	if !openedPath(local) {
		return 0
	}
	return modMs(local)
}

// SFTPUploadOpened uploads the saved copy of a file opened with SFTPOpenFile
// back to the server.
func (a *App) SFTPUploadOpened(id, local, remote string) error {
	if !openedPath(local) {
		return fmt.Errorf("not a file opened from the Files panel")
	}
	return a.terms.SFTPUploadFile(id, local, remote)
}

func openedPath(p string) bool {
	root := filepath.Join(os.TempDir(), "termwright-open") + string(filepath.Separator)
	c := filepath.Clean(p)
	return strings.HasPrefix(c, root) && !strings.Contains(c[len(root):], "..")
}

func modMs(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.ModTime().UnixMilli()
}
