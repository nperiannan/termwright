package app

import "os"

// The tests run in app/: what the main package embeds is read from the repository.
func init() {
	read := func(p string) []byte { b, _ := os.ReadFile(p); return b }
	Use(Files{Assets: os.DirFS("../frontend/dist"), Shortcuts: read("../frontend/src/app/core/shortcuts.json"), Docs: os.DirFS(".."), OpenAPI: read("../docs/api/openapi.yaml"), Scripts: os.DirFS("..")})
}
