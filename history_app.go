package main

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/history"
)

var (
	histOnce  sync.Once
	histStore *history.Store
)

func (a *App) history() *history.Store {
	histOnce.Do(func() { histStore = &history.Store{File: filepath.Join(a.cfg.DataDir(), "history.json")} })
	return histStore
}

func (a *App) historyOn() bool {
	v := a.cfg.Get().RecordHistory
	return v == nil || *v
}

// HistoryAdd remembers a command typed in a terminal or the Command Window
// (unless history is turned off in Global Options).
func (a *App) HistoryAdd(e history.Entry) error {
	if a.cfg == nil || !a.historyOn() {
		return nil
	}
	return a.history().Add(e, time.Now())
}

// HistoryList returns the remembered commands, newest first.
func (a *App) HistoryList() []history.Entry {
	if a.cfg == nil {
		return []history.Entry{}
	}
	return a.history().List()
}

// HistoryClear forgets every remembered command.
func (a *App) HistoryClear() error { return a.history().Clear() }
