package main

import (
	"errors"
	"slices"
	"strings"

	"github.com/termwright/termwright/internal/emu"
)

// EmuNoteSave adds a note or an area to the canvas (no id) or changes one.
func (a *App) EmuNoteSave(n emu.Note) (EmuView, error) {
	defer emuAs("user")()
	if n.Kind != "area" {
		n.Kind = "note"
	}
	n.Text = strings.TrimRight(n.Text, " \n")
	if len(n.Text) > 4000 {
		return EmuView{}, errors.New("at most 4,000 characters")
	}
	n.W, n.H = min(4000, max(80, n.W)), min(4000, max(40, n.H))
	n.Color = min(7, max(0, n.Color))
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("there is no emulated lab")
	}
	if n.ID == "" {
		n.ID = "n" + randHex(3)
		l.Notes = append(l.Notes, n)
	} else if i := slices.IndexFunc(l.Notes, func(x emu.Note) bool { return x.ID == n.ID }); i >= 0 {
		l.Notes[i] = n
	} else {
		emuLab.mu.Unlock()
		return EmuView{}, errors.New("no such note")
	}
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuView{}, err
	}
	return a.EmuState()
}

// EmuNoteRemove removes a note or an area.
func (a *App) EmuNoteRemove(id string) (EmuView, error) {
	defer emuAs("user")()
	emuLab.mu.Lock()
	a.emuLoad()
	if l := emuLab.f.Lab; l != nil {
		l.Notes = slices.DeleteFunc(l.Notes, func(x emu.Note) bool { return x.ID == id })
		_ = a.emuSave()
	}
	emuLab.mu.Unlock()
	return a.EmuState()
}
