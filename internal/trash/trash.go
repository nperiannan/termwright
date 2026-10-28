// Package trash moves files and folders on this computer to the Trash
// (Recycle Bin on Windows) instead of deleting them, so a delete in the SFTP
// browser's local pane can be undone.
package trash

import (
	"fmt"
	"os"
	"path/filepath"
)

// Move puts path in the Trash. It fails (and leaves the file alone) when the
// Trash cannot be used, rather than deleting the file for good.
func Move(path string) error {
	path = filepath.Clean(path)
	if path == "" || path == string(filepath.Separator) || filepath.Dir(path) == path {
		return fmt.Errorf("refusing to move %q to the Trash", path)
	}
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	return move(path)
}
