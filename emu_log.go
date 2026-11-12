package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The emulator's own log: one file (emulator.log in the lab folder, kept to
// three files of 5 MB) with everything the emulated lab does, at a level the
// user chooses - error, warn, info (default), debug (each command run on a
// host, with how long it took and how it ended) or verbose (those commands and
// what they printed, and what was typed on the consoles).

const (
	emuLvError = iota
	emuLvWarn
	emuLvInfo
	emuLvDebug
	emuLvVerbose
)

var emuLvNames = []string{"error", "warn", "info", "debug", "verbose"}

var emuLog = struct {
	sync.Mutex
	path  string
	level int
}{level: emuLvInfo}

// emuLevel turns a level name into its number (info when unknown).
func emuLevel(s string) int {
	for i, n := range emuLvNames {
		if strings.EqualFold(n, s) {
			return i
		}
	}
	if strings.EqualFold(s, "ok") {
		return emuLvInfo
	}
	return emuLvInfo
}

// emuLogOn: whether a level is written now.
func emuLogOn(level int) bool {
	emuLog.Lock()
	defer emuLog.Unlock()
	return emuLog.path != "" && level <= emuLog.level
}

// emuLogf writes a line: time, level, host, switch, text (long texts are cut at 8 KB).
func emuLogf(level int, host, sw, format string, args ...any) {
	emuLog.Lock()
	defer emuLog.Unlock()
	if emuLog.path == "" || level > emuLog.level {
		return
	}
	text := fmt.Sprintf(format, args...)
	if len(text) > 8192 {
		text = text[:8192] + " …(cut)"
	}
	if host != "" {
		host = host[strings.LastIndex(host, "/")+1:]
	}
	line := fmt.Sprintf("%s %-7s %-16s %-16s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), strings.ToUpper(emuLvNames[level]), orStr(host, "-"), orStr(sw, "-"), strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\n    "))
	if fi, err := os.Stat(emuLog.path); err == nil && fi.Size() > 5<<20 { // keep three files of 5 MB
		_ = os.Rename(emuLog.path+".1", emuLog.path+".2")
		_ = os.Rename(emuLog.path, emuLog.path+".1")
	}
	f, err := os.OpenFile(emuLog.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

// emuLogSetup points the log at the lab folder with the saved level (emuLab.mu held).
func (a *App) emuLogSetup() {
	emuLog.Lock()
	emuLog.path = filepath.Join(a.labDir(), "emulator.log")
	emuLog.level = emuLevel(orStr(emuLab.f.LogLevel, "info"))
	emuLog.Unlock()
}

// EmuLogInfo is the log's level, file and its end.
type EmuLogInfo struct {
	Level string `json:"level"`
	Path  string `json:"path"`
	Tail  string `json:"tail"`
}

// EmuLog returns the end of the emulator's log (lines: how many, at most 5000).
func (a *App) EmuLog(lines int) (EmuLogInfo, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.mu.Unlock()
	emuLog.Lock()
	p, lv := emuLog.path, emuLvNames[emuLog.level]
	emuLog.Unlock()
	b, _ := os.ReadFile(p)
	l := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if lines <= 0 || lines > 5000 {
		lines = 5000
	}
	if len(l) > lines {
		l = l[len(l)-lines:]
	}
	return EmuLogInfo{Level: lv, Path: p, Tail: strings.Join(l, "\n")}, nil
}

// EmuSetLogLevel sets how much the emulator's log keeps: error, warn, info, debug or verbose.
func (a *App) EmuSetLogLevel(level string) (EmuLogInfo, error) {
	lv := -1
	for i, n := range emuLvNames {
		if n == strings.ToLower(level) {
			lv = i
		}
	}
	if lv < 0 {
		return EmuLogInfo{}, fmt.Errorf("the level is error, warn, info, debug or verbose")
	}
	emuLab.mu.Lock()
	a.emuLoad()
	emuLab.f.LogLevel = emuLvNames[lv]
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuLogInfo{}, err
	}
	emuLog.Lock()
	emuLog.level = lv
	emuLog.Unlock()
	emuLogf(emuLvInfo, "", "", "log level %s", emuLvNames[lv])
	return a.EmuLog(500)
}
