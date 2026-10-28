package trash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// move renames the file into ~/.Trash (the same as Finder's Move to Trash,
// without asking for permission to control Finder). Files on another volume
// cannot be renamed there; they are left alone with an error.
func move(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".Trash")
	base := filepath.Base(path)
	dst := filepath.Join(dir, base)
	if _, err := os.Lstat(dst); err == nil {
		ext := filepath.Ext(base)
		dst = filepath.Join(dir, strings.TrimSuffix(base, ext)+" "+time.Now().Format("15.04.05")+ext)
	}
	if err := os.Rename(path, dst); err != nil {
		return fmt.Errorf("could not move %s to the Trash (%v) - delete it in Finder", base, err)
	}
	return nil
}
