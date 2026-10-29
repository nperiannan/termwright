package main

import (
	"path/filepath"
	"strings"
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

// HistoryRemove forgets one command (every time it was typed); with a device
// type only where it was typed on that device type.
func (a *App) HistoryRemove(cmd, deviceType string) error {
	_, err := a.history().Remove(func(e history.Entry) bool {
		return e.Cmd == cmd && (deviceType == "" || strings.EqualFold(e.DeviceType, deviceType))
	})
	return err
}

// HistoryClearTypes forgets the commands typed on these device types; "" stands
// for commands without a device type.
func (a *App) HistoryClearTypes(deviceTypes []string) error {
	_, err := a.history().Remove(func(e history.Entry) bool {
		for _, t := range deviceTypes {
			if strings.EqualFold(e.DeviceType, t) {
				return true
			}
		}
		return false
	})
	return err
}
