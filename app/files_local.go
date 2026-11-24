package app

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/termwright/termwright/internal/term"
	"github.com/termwright/termwright/internal/trash"
)

// FileStat is a file's size and modification time (Unix milliseconds).
type FileStat struct {
	Size int64 `json:"size"`
	Mod  int64 `json:"mod"`
}

// SFTPStat: the size and modification time of a file of a Files panel.
func (a *App) SFTPStat(id, p string) (FileStat, error) {
	n, mod, err := a.terms.SFTPStat(id, p)
	return FileStat{n, mod}, err
}

// TextTail is the end of a file the viewer follows, and the file's size.
type TextTail struct {
	Text string `json:"text"`
	Size int64  `json:"size"`
}

// SFTPReadTail reads the last part of a file (up to 2 MB) - a log the viewer follows.
func (a *App) SFTPReadTail(id, p string) (TextTail, error) {
	t, n, err := a.terms.SFTPReadTail(id, p, 2<<20)
	return TextTail{t, n}, err
}

// SFTPSaveText saves a text file edited in the viewer. expect: its modification time when it was opened;
// a file changed since then is not overwritten. It returns the new modification time.
func (a *App) SFTPSaveText(id, p, text string, expect int64) (int64, error) {
	if len(text) > 5<<20 {
		return 0, errors.New("the text is over 5 MB: open the file in an editor")
	}
	return a.terms.SFTPSaveText(id, p, text, expect)
}

// LocalShowInFolder shows a file of a Local Shell tab's Files panel in Finder, Explorer or the file manager.
func (a *App) LocalShowInFolder(id, p string) error {
	if !a.terms.SFTPIsLocal(id) {
		return errors.New("not this computer's files")
	}
	return a.ShowInFolder(term.LocalPathOf(p))
}

// LocalTrashFiles moves files of a Local Shell tab's Files panel to the Trash (Recycle Bin), not deleting them.
func (a *App) LocalTrashFiles(id string, paths []string) error {
	if !a.terms.SFTPIsLocal(id) {
		return errors.New("not this computer's files")
	}
	for _, p := range paths {
		if err := trash.Move(term.LocalPathOf(p)); err != nil {
			return fmt.Errorf("%s: %v", filepath.Base(term.LocalPathOf(p)), err)
		}
	}
	return nil
}
