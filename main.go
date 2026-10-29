// Termwright - an open-source, cross-platform terminal emulator and session manager.
//
// Copyright 2026 The Termwright Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"embed"
	"encoding/json"
	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/discover"
	"log"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed frontend/src/app/core/shortcuts.json
var shortcutsJSON []byte

// shortcuts maps menu actions to keys (see shortcuts.json).
var shortcuts = func() map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(shortcutsJSON, &m)
	return m
}()

func main() {
	discover.RaiseFileLimit()
	app := NewApp()
	width, height, start := 1280, 820, options.Normal
	modern := true
	if dir, err := config.DataDir(); err == nil {
		if c, err := config.Load(dir); err == nil {
			if w := c.Get().Window; w.Width >= 760 && w.Height >= 480 && w.Width < 20000 && w.Height < 20000 {
				width, height = w.Width, w.Height
			}
			modern = c.Get().Layout != "classic"
			if c.Get().Window.Maximised {
				start = options.Maximised
			}
		}
	}
	// macOS, modern layout: the tabs share the title bar with the window buttons.
	var titleBar *mac.TitleBar
	if modern && runtime.GOOS == "darwin" {
		titleBar = mac.TitleBarHiddenInset()
		insetTitle = true
	}
	err := wails.Run(&options.App{
		Title:                    "Termwright",
		Width:                    width,
		Height:                   height,
		WindowStartState:         start,
		OnDomReady:               app.domReady,
		MinWidth:                 760,
		MinHeight:                480,
		AssetServer:              &assetserver.Options{Assets: assets},
		BackgroundColour:         &options.RGBA{R: 30, G: 31, B: 34, A: 255},
		OnStartup:                app.startup,
		OnShutdown:               app.shutdown,
		OnBeforeClose:            app.beforeClose,
		Menu:                     buildMenu(app),
		Bind:                     []interface{}{app},
		EnableDefaultContextMenu: false,
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
		Mac: &mac.Options{
			TitleBar: titleBar,
			About: &mac.AboutInfo{
				Title:   "Termwright " + Version,
				Message: "Open-source terminal emulator and session manager.\nApache License 2.0",
			},
		},
		Linux: &linux.Options{ProgramName: "termwright"},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// insetTitle is true when the window has no title bar of its own (macOS,
// modern layout); the UI then leaves room for the window buttons.
var insetTitle bool

// buildMenu creates the native application menu. Items emit "menu" events
// that the Angular UI handles, so the same actions work from the toolbar,
// keyboard shortcuts and the menu.
func buildMenu(app *App) *menu.Menu {
	emit := func(action string) func(*menu.CallbackData) {
		return func(*menu.CallbackData) {
			if app.ctx != nil {
				wruntime.EventsEmit(app.ctx, "menu", action)
			}
		}
	}
	isMac := runtime.GOOS == "darwin"
	// On macOS Cmd+C/V are free; elsewhere Ctrl+C/V belong to the terminal so
	// copy/paste use Ctrl+Shift+C/V (Ctrl+Insert/Shift+Insert also work).
	acc := func(k string) *keys.Accelerator {
		if isMac {
			return keys.CmdOrCtrl(k)
		}
		return keys.Combo(k, keys.CmdOrCtrlKey, keys.ShiftKey)
	}

	// shortcuts shared with the in-window menus (frontend/src/app/core/shortcuts.json)
	sc := func(action string) *keys.Accelerator {
		k, ok := shortcuts[action]
		if !ok || k == "" {
			return nil
		}
		if strings.HasPrefix(k, "+") {
			k = strings.ToLower(k[1:])
			if isMac {
				return keys.Combo(k, keys.CmdOrCtrlKey, keys.ShiftKey)
			}
			return keys.Combo(k, keys.OptionOrAltKey, keys.ShiftKey)
		}
		return acc(strings.ToLower(k))
	}
	add := func(m *menu.Menu, label, action string) { m.AddText(label, sc(action), emit(action)) }

	m := menu.NewMenu()
	if isMac {
		m.Append(menu.AppMenu())
	}

	file := m.AddSubmenu("File")
	add(file, "Connect...", "connect")
	add(file, "Quick Connect...", "quick-connect")
	add(file, "Connect Local Shell", "local-shell")
	add(file, "New Session...", "new-session")
	file.AddSeparator()
	add(file, "Reconnect", "reconnect")
	add(file, "Disconnect", "disconnect")
	add(file, "Clone Session", "clone-tab")
	file.AddSeparator()
	add(file, "Workspaces...", "workspaces")
	add(file, "Save Workspace...", "save-workspace")
	file.AddSeparator()
	add(file, "Log Session...", "log-toggle")
	add(file, "Record Session", "cast-toggle")
	add(file, "Add Marker to Recording...", "cast-marker")
	add(file, "Recordings...", "recordings")
	add(file, "Save Scrollback As...", "save-scrollback")
	add(file, "Save Scrollback As HTML...", "save-scrollback-html")
	add(file, "Save Selection As...", "save-selection")
	add(file, "Save Screen As Image...", "capture-screen")
	file.AddSeparator()
	add(file, "Import Sessions...", "import")
	add(file, "Discover Devices...", "discover")
	add(file, "Configuration Backups...", "backups")
	add(file, "Change Checklists...", "checklists")
	add(file, "Run and Collect...", "collect")
	add(file, "Export Sessions...", "export")
	file.AddSeparator()
	add(file, "Close Tab", "close-tab")
	if !isMac {
		file.AddText("Exit", nil, func(*menu.CallbackData) { wruntime.Quit(app.ctx) })
	}

	edit := m.AddSubmenu("Edit")
	add(edit, "Copy", "copy")
	add(edit, "Paste", "paste")
	if isMac {
		edit.AddText("Cut", keys.CmdOrCtrl("x"), emit("cut"))
	}
	add(edit, "Copy and Paste", "copy-paste")
	add(edit, "Select All", "select-all")
	add(edit, "Find...", "find")
	add(edit, "Save Selection as Device Fact...", "teach-fact")
	edit.AddSeparator()
	add(edit, "Clear Scrollback", "clear-scrollback")
	add(edit, "Clear Screen", "clear-screen")
	add(edit, "Reset Terminal", "reset")
	edit.AddSeparator()
	add(edit, "Timestamps On / Off", "toggle-timestamps")
	add(edit, "Auto Scroll On / Off", "toggle-autoscroll")

	view := m.AddSubmenu("View")
	add(view, "Quick Open...", "quick-open")
	view.AddSeparator()
	add(view, "Session Manager", "toggle-session-manager")
	add(view, "Session Manager Left / Right", "toggle-session-manager-side")
	add(view, "Command Window", "toggle-command-window")
	add(view, "Files Panel", "files-panel")
	add(view, "Button Bar", "toggle-button-bar")
	add(view, "Connect Bar", "toggle-connect-bar")
	add(view, "Menu Bar in Window", "toggle-window-menu")
	add(view, "Modern / Classic Layout", "toggle-layout")
	add(view, "Tile / Glyph Icons", "toggle-icon-style")
	view.AddSeparator()
	add(view, "Tabbed", "layout-tabs")
	add(view, "Tile Side by Side", "layout-tile-v")
	add(view, "Tile Stacked", "layout-tile-h")
	add(view, "Tile Grid", "layout-grid")
	view.AddSeparator()
	view.AddText("Zoom In", keys.CmdOrCtrl("="), emit("zoom-in"))
	view.AddText("Zoom Out", keys.CmdOrCtrl("-"), emit("zoom-out"))
	view.AddText("Reset Zoom", keys.CmdOrCtrl("0"), emit("zoom-reset"))
	view.AddSeparator()
	view.AddText("Full Screen", keys.Key("f11"), func(*menu.CallbackData) {
		if wruntime.WindowIsFullscreen(app.ctx) {
			wruntime.WindowUnfullscreen(app.ctx)
		} else {
			wruntime.WindowFullscreen(app.ctx)
		}
	})

	xfer := m.AddSubmenu("Transfer")
	add(xfer, "Open SFTP Tab", "sftp-tab")
	add(xfer, "Send File with XMODEM...", "xmodem")
	add(xfer, "Receive File with XMODEM...", "xmodem-receive")
	add(xfer, "Send Files with YMODEM...", "ymodem-send")
	add(xfer, "Receive Files with YMODEM", "ymodem-receive")
	add(xfer, "Send Text File...", "send-file")
	xfer.AddSeparator()
	add(xfer, "SCP Upload File...", "scp-upload")
	add(xfer, "SCP Upload Folder...", "scp-upload-folder")
	add(xfer, "SCP Download...", "scp-download")

	script := m.AddSubmenu("Script")
	add(script, "Command Library...", "command-library")
	add(script, "Command History...", "history")
	script.AddSeparator()
	add(script, "Run Script...", "run-script")
	add(script, "Cancel Running Scripts", "cancel-scripts")
	script.AddSeparator()
	add(script, "Start / Stop Script Recording", "record-toggle")

	opts := m.AddSubmenu("Options")
	add(opts, "Session Options...", "session-options")
	opts.AddText("Global Options...", keys.CmdOrCtrl(","), emit("global-options"))
	opts.AddSeparator()
	add(opts, "Passwords & Credentials...", "passwords")
	add(opts, "Set Up Key Login / Generate Key...", "key-setup")

	tabs := m.AddSubmenu("Tab")
	add(tabs, "Compare Outputs...", "compare")
	tabs.AddSeparator()
	tabs.AddText("Next Tab", keys.Control("tab"), emit("next-tab"))
	tabs.AddText("Previous Tab", keys.Combo("tab", keys.ControlKey, keys.ShiftKey), emit("prev-tab"))
	add(tabs, "Send Command to All Tabs...", "send-all")
	add(tabs, "Send Break", "send-break")
	tabs.AddSeparator()
	add(tabs, "Lock Session", "lock")
	add(tabs, "Lock All Sessions", "lock-all")

	help := m.AddSubmenu("Help")
	add(help, "Welcome...", "welcome")
	add(help, "User Guide", "user-guide")
	add(help, "Check for Updates...", "check-updates")
	add(help, "Keyboard Shortcuts", "shortcuts")
	add(help, "About Termwright", "about")
	return m
}
