// Package fastbin holds the fast traffic engine (cmd/twfast) built for the KVM hosts' CPUs. `make fastbin` builds
// them into bin/ before the app is built; without them the app still works, and the hosts send with the built-in
// engine (traffic.py).
package fastbin

import (
	"embed"
	"strings"
)

//go:embed bin
var bins embed.FS

// For is the engine for a host's CPU as `uname -m` names it (x86_64, aarch64, …), or nil when this build has none.
func For(machine string) []byte {
	arch := map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[strings.TrimSpace(machine)]
	if arch == "" {
		return nil
	}
	b, err := bins.ReadFile("bin/tw-fast-" + arch)
	if err != nil {
		return nil
	}
	return b
}
