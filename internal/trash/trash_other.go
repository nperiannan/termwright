//go:build !darwin && !windows

package trash

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// move uses "gio trash" (GNOME, and most desktops have gio); without it the
// file is left alone.
func move(path string) error {
	if _, err := exec.LookPath("gio"); err != nil {
		return fmt.Errorf("could not move %s to the Trash: gio is not installed - delete it in the file manager", filepath.Base(path))
	}
	if out, err := exec.Command("gio", "trash", "--", path).CombinedOutput(); err != nil {
		return fmt.Errorf("could not move %s to the Trash: %s", filepath.Base(path), strings.TrimSpace(string(out)))
	}
	return nil
}
