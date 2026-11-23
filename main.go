// Termwright - an open-source, cross-platform terminal emulator and session manager.
//
// The app is in app/; this file embeds what it takes from the repository and starts it.
//
// Copyright 2026 The Termwright Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"embed"

	"github.com/termwright/termwright/app"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed frontend/src/app/core/shortcuts.json
var shortcuts []byte

//go:embed docs/USER_GUIDE.md docs/EMULATOR_GUIDE.md docs/FEATURES.md docs/TEAM_SERVER.md CHANGELOG.md
var docs embed.FS

//go:embed docs/api/openapi.yaml
var openapi []byte

//go:embed scripts/remote-desktop/*.sh
var scripts embed.FS

// Version is set at build time with -ldflags "-X main.Version=x.y.z".
var Version = "0.44.1-dev"

// App is the app as the window calls it (window.go.main.App).
type App struct{ *app.App }

func main() {
	app.Version = Version
	app.Use(app.Files{Assets: assets, Shortcuts: shortcuts, Docs: docs, OpenAPI: openapi, Scripts: scripts})
	app.Run(func(a *app.App) any { return &App{a} })
}
