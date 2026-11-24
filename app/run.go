// Package app is the Termwright desktop app: its window's backend (bound to the window as App), the menus, the
// emulated lab, the traffic generator, the AI assistant, teams and the local API. The main package at the root of
// the repository embeds what it needs from the repository (Use) and starts it (Run).
//
// Copyright 2026 The Termwright Authors
// SPDX-License-Identifier: Apache-2.0
package app

import (
	"encoding/json"
	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/discover"
	"io/fs"
	"log"
	"os"
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

// Files are what the app takes from the repository when it is built (the main package embeds them).
type Files struct {
	Assets    fs.FS  // the window: frontend/dist
	Shortcuts []byte // the menus' keys: frontend/src/app/core/shortcuts.json
	Docs      fs.FS  // what the AI assistant knows of Termwright: docs/USER_GUIDE.md, docs/EMULATOR_GUIDE.md, docs/FEATURES.md, docs/TEAM_SERVER.md, CHANGELOG.md
	OpenAPI   []byte // the local API's description: docs/api/openapi.yaml
	Scripts   fs.FS  // scripts/remote-desktop/*.sh, run on Linux machines
}

// assets is the window (frontend/dist).
var assets fs.FS

// shortcuts maps menu actions to keys (see shortcuts.json).
var shortcuts = map[string]string{}

// Use takes the files the app needs from the repository.
func Use(f Files) {
	assets, aiDocs, apiOpenAPIYAML = f.Assets, f.Docs, f.OpenAPI
	m := map[string]string{}
	_ = json.Unmarshal(f.Shortcuts, &m)
	shortcuts = m
	if f.Scripts != nil {
		diagScript, _ = fs.ReadFile(f.Scripts, "scripts/remote-desktop/diagnose-remote-desktop.sh")
		restartScript, _ = fs.ReadFile(f.Scripts, "scripts/remote-desktop/restart-remote-desktop.sh")
		remoteDesktopScript, _ = fs.ReadFile(f.Scripts, "scripts/remote-desktop/setup-remote-desktop.sh")
	}
}

// Run starts the app: the window, or "termwright mcp" (the AI apps' connector) or "termwright lab …" (the command
// line). bind is the app as the window calls it (window.go.main.App: the main package's App).
func Run(bind func(*App) any) {
	utf8Locale()
	// "Termwright mcp": the connector Claude Desktop / Claude Code start (stdin/stdout, no window)
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		os.Exit(mcpBridgeMain())
	}
	// "Termwright lab …": the emulated lab from the command line (through the running app's local API)
	if len(os.Args) > 1 && os.Args[1] == "lab" {
		attachConsole()
		os.Exit(labCLIMain(os.Args[2:]))
	}
	// "Termwright tests …": run tests, or serve the local API for generated tests, without the window
	if len(os.Args) > 1 && os.Args[1] == "tests" {
		attachConsole()
		os.Exit(testsCLIMain(os.Args[2:]))
	}
	discover.RaiseFileLimit()
	app := NewApp()
	width, height, start := 1280, 820, options.Normal
	lang := ""
	if dir, err := config.DataDir(); err == nil {
		_ = config.ApplyPendingRestore(dir) // Settings → Restore from Backup…, finished before anything reads the settings
		if c, err := config.Load(dir); err == nil {
			if w := c.Get().Window; w.Width >= 760 && w.Height >= 480 && w.Width < 20000 && w.Height < 20000 {
				width, height = w.Width, w.Height
			}
			lang = c.Get().Language
			if c.Get().Window.Maximised {
				start = options.Maximised
			}
		}
	}
	// macOS: the tabs share the title bar with the window buttons.
	var titleBar *mac.TitleBar
	if runtime.GOOS == "darwin" {
		titleBar = mac.TitleBarHiddenInset()
		insetTitle = true
	}
	// Linux: no window manager title bar and no GTK menu bar (they follow the desktop's theme, not Termwright's):
	// the app's own title bar has the menus and the window buttons, and the window still resizes at its edges.
	ownTitle = runtime.GOOS == "linux"
	appMenu := buildMenu(app, lang)
	if ownTitle {
		appMenu = nil
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
		Menu:                     appMenu,
		Frameless:                ownTitle,
		Bind:                     []interface{}{bind(app)},
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

// ownTitle is true when the app draws the whole title bar - the menus and the window buttons (Linux).
var ownTitle bool

// buildMenu creates the native application menu. Items emit "menu" events
// that the Angular UI handles, so the same actions work from the toolbar,
// keyboard shortcuts and the menu.
func buildMenu(app *App, lang string) *menu.Menu {
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
	t := func(s string) string { return menuText(lang, s) }
	add := func(m *menu.Menu, label, action string) { m.AddText(t(label), sc(action), emit(action)) }

	m := menu.NewMenu()
	if isMac {
		m.Append(menu.AppMenu())
	}

	file := m.AddSubmenu(t("File"))
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
	add(file, "Copy Scrollback", "copy-scrollback")
	add(file, "Copy Scrollback as HTML Report", "copy-scrollback-html")
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
		file.AddText(t("Exit"), nil, func(*menu.CallbackData) { wruntime.Quit(app.ctx) })
	}

	edit := m.AddSubmenu(t("Edit"))
	add(edit, "Copy", "copy")
	add(edit, "Paste", "paste")
	if isMac {
		edit.AddText(t("Cut"), keys.CmdOrCtrl("x"), emit("cut"))
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

	view := m.AddSubmenu(t("View"))
	add(view, "Quick Open...", "quick-open")
	view.AddSeparator()
	add(view, "Session Manager", "toggle-session-manager")
	add(view, "Session Manager Left / Right", "toggle-session-manager-side")
	add(view, "Command Window", "toggle-command-window")
	add(view, "Files Panel", "files-panel")
	add(view, "Button Bar", "toggle-button-bar")
	add(view, "Connect Bar", "toggle-connect-bar")
	add(view, "Tile / Glyph Icons", "toggle-icon-style")
	view.AddSeparator()
	add(view, "Tabbed", "layout-tabs")
	add(view, "Tile Side by Side", "layout-tile-v")
	add(view, "Tile Stacked", "layout-tile-h")
	add(view, "Tile Grid", "layout-grid")
	view.AddSeparator()
	view.AddText(t("Zoom In"), keys.CmdOrCtrl("="), emit("zoom-in"))
	view.AddText(t("Zoom Out"), keys.CmdOrCtrl("-"), emit("zoom-out"))
	view.AddText(t("Reset Zoom"), keys.CmdOrCtrl("0"), emit("zoom-reset"))
	view.AddSeparator()
	view.AddText(t("Full Screen"), keys.Key("f11"), func(*menu.CallbackData) {
		if wruntime.WindowIsFullscreen(app.ctx) {
			wruntime.WindowUnfullscreen(app.ctx)
		} else {
			wruntime.WindowFullscreen(app.ctx)
		}
	})

	xfer := m.AddSubmenu(t("Transfer"))
	add(xfer, "Open SFTP Tab", "sftp-tab")
	add(xfer, "Send File with XMODEM...", "xmodem")
	add(xfer, "Receive File with XMODEM...", "xmodem-receive")
	add(xfer, "Send Files with YMODEM...", "ymodem-send")
	add(xfer, "Receive Files with YMODEM", "ymodem-receive")
	add(xfer, "Send Text File...", "send-file")
	xfer.AddSeparator()
	add(xfer, "Open Web Page Through This Session...", "web-page")
	add(xfer, "Wake Up (Wake-on-LAN)", "wake")
	xfer.AddSeparator()
	add(xfer, "SCP Upload File...", "scp-upload")
	add(xfer, "SCP Upload Folder...", "scp-upload-folder")
	add(xfer, "SCP Download...", "scp-download")
	xfer.AddSeparator()
	add(xfer, "File Servers (TFTP, FTP, HTTP)...", "file-servers")
	add(xfer, "Network Check (Reachability, iPerf3)...", "net-check")

	lab := m.AddSubmenu(t("Lab"))
	add(lab, "Emulated Lab", "lab-emu")
	add(lab, "Simulated Lab", "lab-sim")
	lab.AddSeparator()
	add(lab, "Overview", "lab-overview")
	add(lab, "Traffic Generator", "lab-traffic")
	add(lab, "Appliances", "lab-appliances")
	add(lab, "KVM Hosts", "lab-hosts")

	snmpM := m.AddSubmenu(t("SNMP"))
	add(snmpM, "SNMP Browser", "snmp-browser")
	add(snmpM, "Trap Receiver", "snmp-traps")
	snmpM.AddSeparator()
	add(snmpM, "Load MIB Files...", "snmp-load-mib")

	apiM := m.AddSubmenu(t("API"))
	add(apiM, "API Client", "api-client")
	add(apiM, "New API Session...", "api-new-session")
	apiM.AddSeparator()
	add(apiM, "Import Postman, OpenAPI or cURL...", "api-import")

	tsM := m.AddSubmenu(t("Tests"))
	add(tsM, "Tests", "tests-open")
	add(tsM, "Record a Device Test", "test-record")
	add(tsM, "Record a Web Test (Playwright)", "tests-web-rec")
	add(tsM, "Close Test Browsers...", "tests-close-browsers")

	slM := m.AddSubmenu(t("Syslog"))
	add(slM, "Syslog Server", "syslog-server")
	add(slM, "Start / Stop Receiving", "syslog-toggle")
	slM.AddSeparator()
	add(slM, "Send Test Message...", "syslog-send")

	script := m.AddSubmenu(t("Script"))
	add(script, "Command Library...", "command-library")
	add(script, "Command History...", "history")
	script.AddSeparator()
	add(script, "Run Script...", "run-script")
	add(script, "Cancel Running Scripts", "cancel-scripts")
	script.AddSeparator()
	add(script, "Start / Stop Script Recording", "record-toggle")

	opts := m.AddSubmenu(t("Options"))
	add(opts, "Session Options...", "session-options")
	opts.AddText(t("Global Options..."), keys.CmdOrCtrl(","), emit("global-options"))
	opts.AddSeparator()
	add(opts, "Passwords & Accounts...", "passwords")
	add(opts, "Set Up Key Login / Generate Key...", "key-setup")
	add(opts, "Teams...", "teams")
	add(opts, "AI Assistant...", "ai-assistant")

	tabs := m.AddSubmenu(t("Tab"))
	add(tabs, "Compare Outputs...", "compare")
	tabs.AddSeparator()
	tabs.AddText(t("Next Tab"), keys.Control("tab"), emit("next-tab"))
	tabs.AddText(t("Previous Tab"), keys.Combo("tab", keys.ControlKey, keys.ShiftKey), emit("prev-tab"))
	add(tabs, "Send Command to All Tabs...", "send-all")
	add(tabs, "Send Break", "send-break")
	tabs.AddSeparator()
	add(tabs, "Lock Session", "lock")
	add(tabs, "Lock All Sessions", "lock-all")

	help := m.AddSubmenu(t("Help"))
	add(help, "Welcome...", "welcome")
	add(help, "Emulated Lab Workflow...", "lab-workflow")
	add(help, "User Guide", "user-guide")
	add(help, "Connect an AI App...", "connect-ai")
	add(help, "Check for Updates...", "check-updates")
	add(help, "Keyboard Shortcuts", "shortcuts")
	add(help, "About Termwright", "about")
	return m
}

// utf8Locale: an app started from the Finder or the Dock has no LANG, and macOS's pbcopy (what copying
// to the clipboard runs) then reads the text as Mac Roman - "✅" pasted as ",úÖ". Without a locale of
// its own, the app and what it starts use UTF-8.
func utf8Locale() {
	if runtime.GOOS != "darwin" || os.Getenv("LANG") != "" || os.Getenv("LC_ALL") != "" || os.Getenv("LC_CTYPE") != "" {
		return
	}
	_ = os.Setenv("LANG", "en_US.UTF-8")
}
