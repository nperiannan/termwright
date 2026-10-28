// Termwright - an open-source, cross-platform terminal emulator and session manager.
//
// Copyright 2026 The Termwright Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"embed"
	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/discover"
	"log"
	"runtime"

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

	m := menu.NewMenu()
	if isMac {
		m.Append(menu.AppMenu())
	}

	file := m.AddSubmenu("File")
	file.AddText("Connect...", acc("o"), emit("connect"))
	file.AddText("Quick Connect...", acc("q"), emit("quick-connect"))
	file.AddText("Connect Local Shell", acc("l"), emit("local-shell"))
	file.AddSeparator()
	file.AddText("Reconnect", acc("r"), emit("reconnect"))
	file.AddText("Disconnect", nil, emit("disconnect"))
	file.AddText("Clone Session", nil, emit("clone-tab"))
	file.AddSeparator()
	file.AddText("Workspaces...", nil, emit("workspaces"))
	file.AddText("Save Workspace...", nil, emit("save-workspace"))
	file.AddSeparator()
	file.AddText("Log Session...", nil, emit("log-toggle"))
	file.AddText("Record Session", nil, emit("cast-toggle"))
	file.AddText("Add Marker to Recording...", acc("j"), emit("cast-marker"))
	file.AddText("Recordings...", nil, emit("recordings"))
	file.AddText("Save Scrollback As...", acc("s"), emit("save-scrollback"))
	file.AddText("Save Scrollback As HTML...", nil, emit("save-scrollback-html"))
	file.AddText("Save Selection As...", nil, emit("save-selection"))
	file.AddSeparator()
	file.AddText("Import Sessions...", nil, emit("import"))
	file.AddText("Discover Devices...", nil, emit("discover"))
	file.AddText("Configuration Backups...", nil, emit("backups"))
	file.AddText("Change Checklists...", nil, emit("checklists"))
	file.AddText("Run and Collect...", nil, emit("collect"))
	file.AddText("Export Sessions...", nil, emit("export"))
	file.AddSeparator()
	file.AddText("Close Tab", acc("w"), emit("close-tab"))
	if !isMac {
		file.AddText("Exit", nil, func(*menu.CallbackData) { wruntime.Quit(app.ctx) })
	}

	edit := m.AddSubmenu("Edit")
	edit.AddText("Copy", acc("c"), emit("copy"))
	edit.AddText("Paste", acc("v"), emit("paste"))
	if isMac {
		edit.AddText("Cut", keys.CmdOrCtrl("x"), emit("cut"))
	}
	edit.AddText("Copy and Paste", nil, emit("copy-paste"))
	edit.AddText("Select All", acc("a"), emit("select-all"))
	edit.AddText("Find...", acc("f"), emit("find"))
	edit.AddSeparator()
	edit.AddText("Clear Scrollback", nil, emit("clear-scrollback"))
	edit.AddText("Clear Screen", nil, emit("clear-screen"))
	edit.AddText("Reset Terminal", nil, emit("reset"))
	edit.AddSeparator()
	edit.AddText("Timestamps On / Off", nil, emit("toggle-timestamps"))
	edit.AddText("Auto Scroll On / Off", nil, emit("toggle-autoscroll"))

	view := m.AddSubmenu("View")
	view.AddText("Quick Open...", acc("g"), emit("quick-open"))
	view.AddSeparator()
	view.AddText("Session Manager", acc("m"), emit("toggle-session-manager"))
	view.AddText("Session Manager Left / Right", nil, emit("toggle-session-manager-side"))
	view.AddText("Command Window", acc("k"), emit("toggle-command-window"))
	view.AddText("Button Bar", acc("b"), emit("toggle-button-bar"))
	view.AddText("Connect Bar", nil, emit("toggle-connect-bar"))
	view.AddText("Menu Bar in Window", nil, emit("toggle-window-menu"))
	view.AddSeparator()
	view.AddText("Tabbed", nil, emit("layout-tabs"))
	view.AddText("Tile Side by Side", nil, emit("layout-tile-v"))
	view.AddText("Tile Stacked", nil, emit("layout-tile-h"))
	view.AddText("Tile Grid", nil, emit("layout-grid"))
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
	xfer.AddText("Open SFTP Tab", acc("p"), emit("sftp-tab"))
	xfer.AddText("Send File with XMODEM...", nil, emit("xmodem"))
	xfer.AddText("Receive File with XMODEM...", nil, emit("xmodem-receive"))
	xfer.AddText("Send Files with YMODEM...", nil, emit("ymodem-send"))
	xfer.AddText("Receive Files with YMODEM", nil, emit("ymodem-receive"))
	xfer.AddText("Send Text File...", nil, emit("send-file"))
	xfer.AddSeparator()
	xfer.AddText("SCP Upload File...", nil, emit("scp-upload"))
	xfer.AddText("SCP Upload Folder...", nil, emit("scp-upload-folder"))
	xfer.AddText("SCP Download...", nil, emit("scp-download"))

	script := m.AddSubmenu("Script")
	script.AddText("Command Library...", acc("e"), emit("command-library"))
	script.AddText("Command History...", acc("y"), emit("history"))
	script.AddSeparator()
	script.AddText("Run Script...", nil, emit("run-script"))
	script.AddText("Cancel Running Scripts", nil, emit("cancel-scripts"))
	script.AddSeparator()
	script.AddText("Start / Stop Script Recording", nil, emit("record-toggle"))

	opts := m.AddSubmenu("Options")
	opts.AddText("Session Options...", nil, emit("session-options"))
	opts.AddText("Global Options...", keys.CmdOrCtrl(","), emit("global-options"))
	opts.AddSeparator()
	opts.AddText("Passwords & Credentials...", nil, emit("passwords"))
	opts.AddText("Set Up Key Login / Generate Key...", nil, emit("key-setup"))

	tabs := m.AddSubmenu("Tab")
	tabs.AddText("Compare Outputs...", nil, emit("compare"))
	tabs.AddSeparator()
	tabs.AddText("Next Tab", keys.Control("tab"), emit("next-tab"))
	tabs.AddText("Send Command to All Tabs...", nil, emit("send-all"))
	tabs.AddText("Send Break", nil, emit("send-break"))
	tabs.AddSeparator()
	tabs.AddText("Lock Session", nil, emit("lock"))
	tabs.AddText("Lock All Sessions", nil, emit("lock-all"))

	help := m.AddSubmenu("Help")
	help.AddText("User Guide", nil, emit("user-guide"))
	help.AddText("Check for Updates...", nil, emit("check-updates"))
	help.AddText("Keyboard Shortcuts", nil, emit("shortcuts"))
	help.AddText("About Termwright", nil, emit("about"))
	return m
}
