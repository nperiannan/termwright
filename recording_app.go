package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/recording"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Session recordings: every connection of a session (or all sessions) saved
// as an asciinema .cast file and replayed in a player tab.

// RecordingDir is the folder recordings are saved in.
func (a *App) RecordingDir() string {
	if d := strings.TrimSpace(a.cfg.Get().RecordDir); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Documents", "Termwright Recordings")
}

func (a *App) recordings() recording.Store { return recording.Store{Dir: a.RecordingDir()} }

// Recordings lists the recordings, newest first.
func (a *App) Recordings() ([]recording.Info, error) { return a.recordings().List() }

// LoadRecording reads a recording for the player. Files outside the
// recordings folder can be opened too (asciinema files from elsewhere).
func (a *App) LoadRecording(path string) (*recording.Recording, error) {
	if !strings.EqualFold(filepath.Ext(path), ".cast") {
		return nil, errors.New("not a recording (.cast) file")
	}
	return recording.Load(path)
}

// ChooseRecording asks for a .cast file to play.
func (a *App) ChooseRecording() (string, error) {
	d := a.RecordingDir()
	_ = os.MkdirAll(d, 0o700)
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Play Recording", DefaultDirectory: d,
		Filters: []wruntime.FileFilter{{DisplayName: "Recordings (*.cast)", Pattern: "*.cast"}}})
}

// StartSessionRecording starts recording a terminal tab.
func (a *App) StartSessionRecording(tabID string) (string, error) {
	return a.terms.StartSessionRecording(tabID)
}

// StopSessionRecording stops recording a terminal tab.
func (a *App) StopSessionRecording(tabID string) error { return a.terms.StopSessionRecording(tabID) }

// SessionRecording is the file a tab is being recorded to ("" if none).
func (a *App) SessionRecording(tabID string) string { return a.terms.SessionRecording(tabID) }

// AddRecordingMarker adds a marker with a note to a tab's recording.
func (a *App) AddRecordingMarker(tabID, note string) error { return a.terms.AddMarker(tabID, note) }

// ShowRecordingFolder opens the recordings folder in the file manager.
func (a *App) ShowRecordingFolder() error {
	d := a.RecordingDir()
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	return a.ShowInFolder(d)
}

// pruneRecordings removes recordings older than the configured number of
// days, at start and twice a day.
func (a *App) pruneRecordings(ctx context.Context) {
	for {
		if days := a.cfg.Get().RecordKeepDays; days > 0 {
			a.recordings().Prune(days, time.Now())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(12 * time.Hour):
		}
	}
}
