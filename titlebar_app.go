package main

import (
	"os/exec"
	"runtime"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// TitleBarDoubleClick does what a double-click on a window title bar does on
// this system: on macOS what System Settings → Desktop & Dock → "Double-click
// a window's title bar to" says (zoom, minimise or nothing), elsewhere maximise
// or restore. The app draws its own title bar, so the system does not.
func (a *App) TitleBarDoubleClick() {
	if a.ctx == nil {
		return
	}
	switch titleBarAction() {
	case "none":
	case "minimize":
		wruntime.WindowMinimise(a.ctx)
	default:
		wruntime.WindowToggleMaximise(a.ctx)
	}
}

// titleBarAction is "maximize", "minimize" or "none".
func titleBarAction() string {
	if runtime.GOOS != "darwin" {
		return "maximize"
	}
	out, err := exec.Command("defaults", "read", "-g", "AppleActionOnDoubleClick").Output()
	if err != nil {
		return "maximize" // not set: macOS zooms
	}
	return parseTitleBarAction(string(out))
}

func parseTitleBarAction(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "minimize", "minimise":
		return "minimize"
	case "none", "do nothing":
		return "none"
	}
	return "maximize" // "Maximize", "Fill" and anything else: zoom
}

// WindowButton is a window button of the app's own title bar (Linux): min, max (maximise or restore) or close
// (as the system's close button: it asks first when Global Options says so).
func (a *App) WindowButton(b string) {
	if a.ctx == nil {
		return
	}
	switch b {
	case "min":
		wruntime.WindowMinimise(a.ctx)
	case "max":
		wruntime.WindowToggleMaximise(a.ctx)
	case "close":
		if !a.beforeClose(a.ctx) {
			a.Quit()
		}
	}
}
