// Package libsrc holds the source of the packages a standalone Go suite carries with it (the steps engine
// and what it uses), so an exported suite builds without Termwright.
package libsrc

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed standalone/*.go textfsm/*.go netconf/*.go snmp/*.go snmp/mibs webrun/webrun.go webrun/runner.js
var src embed.FS

// Module is Termwright's module path; the copies import each other under the suite's module instead.
const Module = "github.com/termwright/termwright/internal/"

// Files returns the packages' files (no tests) by path below lib/, with their imports moved to module.
func Files(module string) map[string][]byte {
	out := map[string][]byte{}
	_ = fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := src.ReadFile(p)
		if err != nil {
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			b = []byte(strings.ReplaceAll(string(b), `"`+Module, `"`+module+"/lib/"))
		}
		out[p] = b
		return nil
	})
	return out
}
