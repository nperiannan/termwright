package trash

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// move sends the file or folder to the Recycle Bin through .NET (the path is
// passed in an environment variable, so no quoting is needed).
func move(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	kind := "DeleteFile"
	if fi.IsDir() {
		kind = "DeleteDirectory"
	}
	script := "Add-Type -AssemblyName Microsoft.VisualBasic; [Microsoft.VisualBasic.FileIO.FileSystem]::" + kind +
		"($env:TW_TRASH_PATH, 'OnlyErrorDialogs', 'SendToRecycleBin')"
	c := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	c.Env = append(os.Environ(), "TW_TRASH_PATH="+path)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("could not move %s to the Recycle Bin: %s", filepath.Base(path), strings.TrimSpace(string(out)))
	}
	return nil
}
