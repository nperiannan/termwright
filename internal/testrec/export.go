package testrec

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ExportOptions: what goes into a standalone suite.
type ExportOptions struct {
	// Files: tests, folders or suites/… plans (none: every test).
	Files []string `json:"files,omitempty"`
	// Lang: go, python or tcl.
	Lang string `json:"lang"`
	// OS: linux, mac or windows - which run scripts to write.
	OS string `json:"os"`
	// Testbed: the default testbed of the run script (testbeds/…); "" = the tests' own sessions.
	Testbed string `json:"testbed,omitempty"`
	// Name of the suite (the zip's folder).
	Name string `json:"name,omitempty"`
}

// Export packs a suite that runs without the Tests tab: the tests (YAML and their Go, Python or TCL code),
// the library, the testbeds, the templates, a plan (plan.txt) and a run script for the OS that writes
// reports/<date-time>/ with JUnit XML and each test's run log. The steps still go through Termwright
// (the desktop app, or `termwright tests serve` on a server), which logs in with the saved sessions.
func (f Folder) Export(o ExportOptions) ([]byte, string, error) {
	switch o.Lang {
	case "go", "python", "tcl":
	default:
		return nil, "", fmt.Errorf("language %q: go, python or tcl", o.Lang)
	}
	switch o.OS {
	case "linux", "mac", "windows":
	default:
		return nil, "", fmt.Errorf("operating system %q: linux, mac or windows", o.OS)
	}
	cases, err := f.ExpandFiles(o.Files, nil)
	if err != nil {
		return nil, "", err
	}
	if len(cases) == 0 {
		return nil, "", fmt.Errorf("no tests to export")
	}
	name := slug(o.Name)
	if name == "" || name == "test" {
		name = "termwright-suite"
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	put := func(rel string, b []byte, exec bool) error {
		h := &zip.FileHeader{Name: name + "/" + rel, Method: zip.Deflate, Modified: time.Now()}
		mode := fs.FileMode(0o644)
		if exec {
			mode = 0o755
		}
		h.SetMode(mode)
		w, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
	var plan []string
	for _, c := range cases {
		if _, err := f.WriteGo(c); err != nil {
			return nil, "", fmt.Errorf("%s: %v", c, err)
		}
		gen := map[string]string{"go": GoFile(c), "python": PyFile(c), "tcl": TclFile(c)}[o.Lang]
		for _, rel := range []string{c, gen} {
			b, err := os.ReadFile(filepath.Join(f.Root, rel))
			if err != nil {
				return nil, "", err
			}
			if err := put(rel, b, false); err != nil {
				return nil, "", err
			}
		}
		if o.Lang == "go" {
			gen = "./" + filepath.ToSlash(filepath.Dir(c))
		}
		plan = append(plan, gen)
	}
	plan = uniq(plan)
	for rel, b := range GoLibFiles() {
		keep := map[string]bool{"go": rel == "go.mod" || strings.HasPrefix(rel, "lib/twtest/") || strings.HasPrefix(rel, "lib/junit/"),
			"python": strings.HasPrefix(rel, "lib/python/"), "tcl": strings.HasPrefix(rel, "lib/tcl/")}[o.Lang]
		if keep {
			if p := filepath.Join(f.Root, rel); rel == "go.mod" {
				if own, err := os.ReadFile(p); err == nil {
					b = own
				}
			}
			if err := put(rel, b, false); err != nil {
				return nil, "", err
			}
		}
	}
	for _, dir := range []string{"testbeds", "templates", "suites"} {
		_ = filepath.WalkDir(filepath.Join(f.Root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(f.Root, p)
			rel = filepath.ToSlash(rel)
			if dir == "testbeds" && o.Testbed != "" && rel != o.Testbed {
				return nil
			}
			if b, err := os.ReadFile(p); err == nil {
				_ = put(rel, b, false)
			}
			return nil
		})
	}
	if err := put("plan.txt", []byte("# the tests this suite runs, in order (one per line)\n"+strings.Join(plan, "\n")+"\n"), false); err != nil {
		return nil, "", err
	}
	for rel, s := range runScripts(o) {
		if err := put(rel, []byte(s), !strings.HasSuffix(rel, ".ps1") && !strings.HasSuffix(rel, ".cmd")); err != nil {
			return nil, "", err
		}
	}
	if err := put("README.md", []byte(exportReadme(o, name, len(cases))), false); err != nil {
		return nil, "", err
	}
	if err := z.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), name + "-" + o.Lang + "-" + o.OS + ".zip", nil
}

func uniq(l []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range l {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func runScripts(o ExportOptions) map[string]string {
	tb := o.Testbed
	need := map[string]string{"go": "go", "python": "python3", "tcl": "tclsh"}[o.Lang]
	if o.OS == "windows" {
		py := "python"
		cmd := map[string]string{
			"python": py + ` lib\python\run_tests.py --list plan.txt --reports reports $tbArgs`,
			"tcl":    `tclsh lib\tcl\run_tests.tcl -list plan.txt -reports reports $tbArgsTcl`,
			"go": `$out = "reports\" + (Get-Date -Format "yyyyMMdd-HHmmss"); New-Item -ItemType Directory -Force "$out\logs" | Out-Null
$env:TW_LOG_DIR = (Resolve-Path "$out\logs").Path
$pkgs = Get-Content plan.txt | Where-Object { $_ -and -not $_.StartsWith("#") }
go test -count=1 -json @pkgs -args $tbArgsGo | Tee-Object "$out\go-test.json" | go run ./lib/junit | Set-Content "$out\junit.xml"
Write-Host "reports: $out"`,
		}[o.Lang]
		exe := map[string]string{"go": "go", "python": py, "tcl": "tclsh"}[o.Lang]
		ps := `# Runs this Termwright suite. Needs ` + exe + ` and Termwright running (the desktop app with its Local API on,
# or "termwright tests serve" on a server). Usage: .\run.ps1 [-Testbed testbeds\lab.yaml]
param([string]$Testbed = "` + strings.ReplaceAll(tb, "/", `\`) + `")
Set-Location $PSScriptRoot
if (-not (Get-Command ` + exe + ` -ErrorAction SilentlyContinue)) { Write-Error "` + exe + ` is not installed or not on PATH"; exit 2 }
$tbArgs = @(); $tbArgsTcl = @(); $tbArgsGo = @()
if ($Testbed) { $tbArgs = @("--testbed", $Testbed); $tbArgsTcl = @("-testbed", $Testbed); $tbArgsGo = @("-tw.testbed=$Testbed") }
` + cmd + `
exit $LASTEXITCODE
`
		return map[string]string{"run.ps1": ps, "run.cmd": "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0run.ps1\" %*\r\n"}
	}
	cmd := map[string]string{
		"python": `python3 lib/python/run_tests.py --list plan.txt --reports reports ${TESTBED:+--testbed "$TESTBED"}`,
		"tcl":    `tclsh lib/tcl/run_tests.tcl -list plan.txt -reports reports ${TESTBED:+-testbed "$TESTBED"}`,
		"go": `out="reports/$(date +%Y%m%d-%H%M%S)"; mkdir -p "$out/logs"
export TW_LOG_DIR="$PWD/$out/logs"
set -o pipefail 2>/dev/null
go test -count=1 -json $(grep -v '^#' plan.txt) -args ${TESTBED:+-tw.testbed="$TESTBED"} | tee "$out/go-test.json" | go run ./lib/junit > "$out/junit.xml"
rc=$?
echo "reports: $out"
exit $rc`,
	}[o.Lang]
	return map[string]string{"run.sh": `#!/usr/bin/env bash
# Runs this Termwright suite. Needs ` + need + ` and Termwright running (the desktop app with its Local API on,
# or "termwright tests serve" on a server). Usage: ./run.sh [testbeds/lab.yaml]
cd "$(dirname "$0")" || exit 2
TESTBED=${1:-` + tb + `}
command -v ` + need + ` >/dev/null 2>&1 || { echo "` + need + ` is not installed or not on PATH" >&2; exit 2; }
` + cmd + `
`}
}

func exportReadme(o ExportOptions, name string, n int) string {
	lang := map[string]string{"go": "Go", "python": "Python", "tcl": "TCL"}[o.Lang]
	run := "./run.sh"
	if o.OS == "windows" {
		run = `.\run.cmd`
	}
	need := map[string]string{"go": "Go 1.22 or later", "python": "Python 3.8 or later (standard library only)", "tcl": "Tcl 8.6 (tclsh) with the http package"}[o.Lang]
	tbs := "the tests' own sessions"
	if o.Testbed != "" {
		tbs = o.Testbed
	}
	var files []string
	for _, d := range []string{"cases/", "lib/", "testbeds/", "templates/", "suites/", "plan.txt", "run.*", "reports/"} {
		files = append(files, d)
	}
	sort.Strings(files)
	return "# " + name + "\n\n" + fmt.Sprintf("%d Termwright tests as a standalone %s suite.\n\n", n, lang) +
		"## Run\n\n1. Install " + need + ".\n2. Start Termwright with its Local API on (Settings → AI → Local API), or on a server without a desktop run\n   `termwright tests serve`. The steps go through it: it logs in with the saved sessions, so no password is in this folder.\n" +
		"3. Run `" + run + "` - or `" + run + " testbeds/<other>.yaml` for other devices (default: " + tbs + ").\n\n" +
		"Each run writes `reports/<date-time>/`: `junit.xml` for CI and `logs/` with each test's run log - what was sent to which device, what came back, every check with what was expected and what was found, and the result.\n\n" +
		"## What is here\n\n" +
		"- `cases/` - the tests: the YAML (the source) and the generated " + lang + " code\n" +
		"- `lib/` - the helper library (run, http, snmp, checks, tables, configure, wait_for, log, testbed roles)\n" +
		"- `testbeds/` - which saved session plays each role\n- `templates/` - TextFSM templates for parsed output\n" +
		"- `plan.txt` - the tests the run script runs, in order; edit it to run fewer or more\n" +
		"- `run.*` - the run script for " + map[string]string{"linux": "Linux", "mac": "macOS", "windows": "Windows"}[o.OS] + "\n\n" +
		"## Write more\n\nCopy a test and change it, or record new ones in Termwright and export again. The library's own\nhelpers (`configure`, `wait_for`, `log`, `session(role)`) are documented at the top of the library file.\n"
}
