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

	libsrc "github.com/termwright/termwright/internal"
	"github.com/termwright/termwright/internal/standalone"
	"gopkg.in/yaml.v3"
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
	// Standalone: the suite logs in to the devices itself (inventory.yaml, no Termwright needed); else the
	// steps go through Termwright. Python and Go.
	Standalone bool `json:"standalone,omitempty"`
	// Runners: the twrun programs for a runner suite, by file name (twrun-linux-amd64…); missing ones are
	// downloaded by the run script on its first run.
	Runners map[string][]byte `json:"-"`
	// Version: the Termwright release the run script downloads twrun from ("nightly" for development builds).
	Version string `json:"-"`
	// Device: the app's saved sessions for the inventory (session path → device; false when it is not one).
	Device func(session string) (InventoryDevice, bool) `json:"-"`
}

// InventoryDevice is a device of a standalone suite's inventory.yaml - how to reach it, never a password.
type InventoryDevice = standalone.Device

// Export packs a suite that runs without the Tests tab: the tests (YAML and their Go, Python or TCL code),
// the library, the testbeds, the templates, a plan (plan.txt) and a run script for the OS that writes
// reports/<date-time>/ with JUnit XML and each test's run log. The steps still go through Termwright
// (the desktop app, or `termwright tests serve` on a server), which logs in with the saved sessions.
func (f Folder) Export(o ExportOptions) ([]byte, string, error) {
	switch o.Lang {
	case "go", "python", "tcl":
	case "runner": // the test files and the twrun program
		o.Standalone = true
	default:
		return nil, "", fmt.Errorf("language %q: go, python, tcl or runner", o.Lang)
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
	if o.Standalone && o.Lang == "tcl" {
		return nil, "", fmt.Errorf("a standalone suite is Python, Go or the runner program (TCL goes through Termwright)")
	}
	var plan []string
	sessions := map[string]bool{}
	for _, c := range cases {
		if o.Standalone {
			if cs, err := f.Load(c); err == nil {
				for _, x := range f.sessionsOf(cs) {
					sessions[x] = true
				}
			}
		}
		if o.Lang == "runner" {
			b, err := os.ReadFile(filepath.Join(f.Root, c))
			if err != nil {
				return nil, "", err
			}
			if err := put(c, b, false); err != nil {
				return nil, "", err
			}
			plan = append(plan, c)
			continue
		}
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
			if o.Standalone && rel == "go.mod" {
				continue // written with its requirements below
			}
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
	if o.Lang == "python" && len(f.Procs()) > 0 {
		if err := put("lib/python/twprocs.py", f.GeneratePyProcs(), false); err != nil {
			return nil, "", err
		}
	}
	if o.Lang == "go" && len(f.Procs()) > 0 {
		gp, err := f.GenerateGoProcs()
		if err != nil {
			return nil, "", err
		}
		if err := put("lib/twprocs/twprocs.go", gp, false); err != nil {
			return nil, "", err
		}
	}
	for _, dir := range []string{"testbeds", "templates", "suites", "procs", "data"} {
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
	if o.Standalone {
		_ = filepath.WalkDir(filepath.Join(f.Root, "testbeds"), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if b, err := os.ReadFile(p); err == nil {
					var m map[string]any
					if yaml.Unmarshal(b, &m) == nil {
						walkStrings(m, func(v string) { sessions[v] = true })
					}
				}
			}
			return nil
		})
		inv, missing := inventoryOf(sessions, o.Device)
		if err := put("inventory.yaml", inv, false); err != nil {
			return nil, "", err
		}
		if o.Lang == "python" {
			if err := put("requirements.txt", []byte(standaloneReqs(o.Lang)), false); err != nil {
				return nil, "", err
			}
		}
		if o.Lang == "go" {
			for rel, b := range libsrc.Files(GoModule) {
				if err := put("lib/"+rel, b, false); err != nil {
					return nil, "", err
				}
			}
			if err := put("lib/twtest/local.go", golibFile("twlocal.go.txt"), false); err != nil {
				return nil, "", err
			}
			if err := put("go.mod", []byte(standaloneGoMod), false); err != nil {
				return nil, "", err
			}
			if err := put("go.sum", golibFile("go.sum.txt"), false); err != nil {
				return nil, "", err
			}
		}
		if len(missing) > 0 {
			_ = put("MISSING-DEVICES.txt", []byte("These sessions are used by the tests but are not saved sessions in Termwright; add them to inventory.yaml:\n"+strings.Join(missing, "\n")+"\n"), false)
		}
	}
	if o.Lang == "runner" {
		for name, b := range o.Runners {
			if err := put("bin/"+name, b, true); err != nil {
				return nil, "", err
			}
		}
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
	kind := ""
	if o.Standalone {
		kind = "-standalone"
	}
	return buf.Bytes(), name + "-" + o.Lang + kind + "-" + o.OS + ".zip", nil
}

// sessionsOf lists the session names a test may use: its variables' values and the steps' literal sessions.
func (f Folder) sessionsOf(c Case) []string {
	var out []string
	for _, v := range c.Vars {
		out = append(out, v)
	}
	for _, p := range c.Params {
		for _, v := range p {
			out = append(out, v)
		}
	}
	var walk func([]Step)
	walk = func(l []Step) {
		for _, st := range l {
			for _, x := range []string{sessionOfStep(st)} {
				if x != "" && !strings.Contains(x, "{{") {
					out = append(out, x)
				}
			}
			if st.Loop != nil {
				walk(st.Loop.Steps)
			}
		}
	}
	if pc, err := f.Prepare(c); err == nil {
		c = pc
	}
	walk(c.Setup)
	walk(c.Steps)
	walk(c.Cleanup)
	return out
}

func sessionOfStep(st Step) string {
	switch {
	case st.Run != nil:
		return st.Run.Session
	case st.HTTP != nil:
		return st.HTTP.Session
	case st.SNMP != nil:
		return st.SNMP.Session
	case st.Netconf != nil:
		return st.Netconf.Session
	}
	return ""
}

func walkStrings(x any, f func(string)) {
	switch t := x.(type) {
	case string:
		f(t)
	case map[string]any:
		for _, v := range t {
			walkStrings(v, f)
		}
	case []any:
		for _, v := range t {
			walkStrings(v, f)
		}
	}
}

// inventoryOf writes inventory.yaml for the sessions that are saved sessions; the rest are returned.
func inventoryOf(sessions map[string]bool, dev func(string) (InventoryDevice, bool)) ([]byte, []string) {
	var names []string
	for s := range sessions {
		names = append(names, s)
	}
	sort.Strings(names)
	var devs []InventoryDevice
	var missing []string
	for _, s := range names {
		if dev == nil || strings.TrimSpace(s) == "" {
			continue
		}
		if d, ok := dev(s); ok {
			devs = append(devs, d)
			for j := d.Jump; j != "" && !sessions[j]; { // its jump host too
				sessions[j] = true
				jd, ok := dev(j)
				if !ok {
					missing = append(missing, j)
					break
				}
				devs = append(devs, jd)
				j = jd.Jump
			}
		} else if strings.Contains(s, "/") {
			missing = append(missing, s)
		}
	}
	b, _ := yaml.Marshal(map[string]any{"devices": devs})
	head := `# The devices of this suite, written by Termwright from its saved sessions. No passwords are here:
# set TW_PASSWORD_<NAME> (the session's last part in capitals, other characters as _), or TW_PASSWORD for all,
# or keep them in the system keyring (service "termwright", user = the session). API tokens: TW_TOKEN_<NAME>;
# SNMP communities: TW_COMMUNITY_<NAME> (default public). To run on other devices, change host and the rest.
`
	return append([]byte(head), b...), missing
}

// standaloneGoMod: the module of a standalone Go suite (the engine needs x/crypto/ssh and yaml).
const standaloneGoMod = "module " + GoModule + "\n\ngo 1.24.0\n\nrequire (\n\tgolang.org/x/crypto " + xCryptoVersion + "\n\tgopkg.in/yaml.v3 v3.0.1\n)\n\nrequire golang.org/x/sys " + xSysVersion + " // indirect\n"

// the versions Termwright builds with (TestStandaloneGoModVersions keeps them equal to go.mod)
const (
	xCryptoVersion = "v0.43.0"
	xSysVersion    = "v0.37.0"
)

func golibFile(n string) []byte { b, _ := golib.ReadFile("golib/" + n); return b }

func standaloneReqs(lang string) string {
	if lang == "go" {
		return "# Go modules are in go.mod (go mod download fetches them). For SNMP steps install net-snmp; for web steps Playwright for Go is used.\n"
	}
	return `# pip install -r requirements.txt
paramiko>=3.0
textfsm>=1.1
PyYAML>=6.0
# optional:
# ncclient>=0.6      NETCONF steps
# playwright>=1.40   web steps (then: playwright install chromium)
# keyring>=24        passwords from the system keyring
# SNMP steps use the net-snmp commands (apt install snmp / brew install net-snmp)
`
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
	if o.Lang == "runner" {
		return runnerScripts(o)
	}
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
		needs := ` and Termwright running (the desktop app with its Local API on,
# or "termwright tests serve" on a server)`
		local := ""
		if o.Standalone {
			needs = ` and the packages of requirements.txt (pip install -r requirements.txt); the devices are in
# inventory.yaml and passwords come from TW_PASSWORD_<NAME> / TW_PASSWORD, the keyring or a prompt`
			local = `$env:TW_ENGINE = "local"
& ` + py + ` -c "import paramiko, yaml, textfsm" 2>$null; if ($LASTEXITCODE) { Write-Error "Install the packages first: ` + py + ` -m pip install -r requirements.txt"; exit 2 }
`
			if o.Lang == "go" {
				needs = ` 1.24 or later (the first run downloads its modules); the devices are in inventory.yaml and
# passwords come from TW_PASSWORD_<NAME> / TW_PASSWORD`
				local = "go mod download; if ($LASTEXITCODE) { Write-Error \"go mod download failed: the first run needs the Go module proxy\"; exit 2 }\n"
			}
		}
		ps := `# Runs this Termwright suite. Needs ` + exe + needs + `. Usage: .\run.ps1 [-Testbed testbeds\lab.yaml]
param([string]$Testbed = "` + strings.ReplaceAll(tb, "/", `\`) + `")
Set-Location $PSScriptRoot
if (-not (Get-Command ` + exe + ` -ErrorAction SilentlyContinue)) { Write-Error "` + exe + ` is not installed or not on PATH"; exit 2 }
` + local + `$tbArgs = @(); $tbArgsTcl = @(); $tbArgsGo = @()
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
	needs := ` and Termwright running (the desktop app with its Local API on,
# or "termwright tests serve" on a server)`
	local := ""
	if o.Standalone {
		needs = ` and the packages of requirements.txt (pip install -r requirements.txt); the devices are in
# inventory.yaml and passwords come from TW_PASSWORD_<NAME> / TW_PASSWORD, the keyring or a prompt`
		local = `export TW_ENGINE=local
python3 -c "import paramiko, yaml, textfsm" 2>/dev/null || { echo "Install the packages first: python3 -m pip install -r requirements.txt" >&2; exit 2; }
`
		if o.Lang == "go" {
			needs = ` 1.24 or later (the first run downloads its modules); the devices are in inventory.yaml and
# passwords come from TW_PASSWORD_<NAME> / TW_PASSWORD`
			local = "go mod download || { echo \"go mod download failed: the first run needs the Go module proxy\" >&2; exit 2; }\n"
		}
	}
	return map[string]string{"run.sh": `#!/usr/bin/env bash
# Runs this Termwright suite. Needs ` + need + needs + `. Usage: ./run.sh [testbeds/lab.yaml]
cd "$(dirname "$0")" || exit 2
TESTBED=${1:-` + tb + `}
command -v ` + need + ` >/dev/null 2>&1 || { echo "` + need + ` is not installed or not on PATH" >&2; exit 2; }
` + local + cmd + `
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
	if o.Lang == "runner" {
		return "# " + name + "\n\n" + fmt.Sprintf("%d Termwright tests with the twrun program - Termwright is not needed.\n\n", n) +
			"## Run\n\n1. Check `inventory.yaml`: each session the tests use, with its host, protocol, port, user name and device type.\n" +
			"2. Give the passwords - never written in this folder: `TW_PASSWORD` for all devices or `TW_PASSWORD_<NAME>` per device\n   (the session's last part in capitals, other characters as `_`); API tokens `TW_TOKEN_<NAME>`; else twrun asks.\n" +
			"3. Run `" + run + "` - or `" + run + " testbeds/<other>.yaml`. Options after it go to twrun: `--tags smoke`, `--rerun 2`,\n   `--test-timeout 300`, `--junit out.xml`. `bin/twrun-… check` logs in to each device; `bin/twrun-… setup-web` installs\n   Playwright for web steps (Node.js needed).\n\n" +
			"`bin/` holds twrun for this operating system (the run script downloads it from the Termwright release if it is missing).\n" +
			"Each run writes `reports/<date-time>/` - report.html, report.pdf, junit.xml and each test's run log - the same as Termwright's runs.\n\n" +
			"## What is here\n\n- `cases/` - the tests (YAML); `procs/` - the procedures they call; `data/` - test data\n" +
			"- `inventory.yaml` - the devices; `testbeds/` - which device plays each role; `templates/` - TextFSM templates\n" +
			"- `plan.txt` - the tests to run, in order; `run.*` - the run script; `bin/` - twrun\n\n" +
			"Edit the YAML to change a test, or record more in Termwright and export again. Steps that need Termwright itself\n(software upgrades, lab traffic tests, manual steps, saving and restoring the configuration) fail with a note here.\n"
	}
	if o.Standalone && o.Lang == "go" {
		return "# " + name + "\n\n" + fmt.Sprintf("%d Termwright tests as a Go suite that runs on its own - Termwright is not needed.\n\n", n) +
			"## Run\n\n1. Install Go 1.24 or later. The first run downloads the modules (golang.org/x/crypto, gopkg.in/yaml.v3).\n" +
			"2. Check `inventory.yaml`: each session the tests use, with its host, protocol, port, user name and device type.\n" +
			"3. Give the passwords - never written in this folder: `TW_PASSWORD` for all devices or `TW_PASSWORD_<NAME>` per device;\n   API tokens `TW_TOKEN_<NAME>`; SNMP communities `TW_COMMUNITY_<NAME>`.\n" +
			"4. Run `" + run + "` - or `go test ./cases/...` yourself (`-args -tw.testbed=testbeds/lab.yaml -tw.var name=value`).\n\n" +
			"Web steps need Node.js and Playwright: `npm install --prefix .playwright playwright && npx --prefix .playwright playwright install chromium`.\n\n" +
			"## What is here\n\n- `cases/` - the tests: the YAML and the generated Go tests (`go test`)\n" +
			"- `lib/twtest` - the test library; `lib/twprocs` - the procedures as Go functions your tests can call\n" +
			"- `lib/standalone` - logs in to the devices (SSH, Telnet), REST, SNMP, NETCONF, web pages, TextFSM tables;\n  `lib/snmp`, `lib/netconf`, `lib/textfsm`, `lib/webrun` - what it uses\n" +
			"- `inventory.yaml`, `testbeds/`, `templates/`, `plan.txt`, `run.*`\n"
	}
	if o.Standalone {
		return "# " + name + "\n\n" + fmt.Sprintf("%d Termwright tests as a %s suite that runs on its own - Termwright is not needed.\n\n", n, lang) +
			"## Run\n\n1. Install Python 3.8 or later, then `python3 -m pip install -r requirements.txt` (paramiko, textfsm, PyYAML;\n   ncclient, playwright and keyring are optional; SNMP steps use the net-snmp commands).\n" +
			"2. Check `inventory.yaml`: each session the tests use, with its host, protocol, port, user name and device type.\n" +
			"3. Give the passwords - never written in this folder: `export TW_PASSWORD=...` for all devices, or\n   `TW_PASSWORD_<NAME>` per device (the session's last part in capitals, other characters as `_`), or the system\n   keyring (service `termwright`), or type them when asked.\n" +
			"4. Run `" + run + "` - or `" + run + " testbeds/<other>.yaml` for other devices (default: " + tbs + ").\n\n" +
			"Each run writes `reports/<date-time>/`: `junit.xml` for CI and `logs/` with each test's run log - what was sent to which device, what came back, every check with what was expected and what was found, and the result.\n\n" +
			"## What is here\n\n" +
			"- `cases/` - the tests: the YAML (the source) and the generated Python code (unittest)\n" +
			"- `lib/python/twtest.py` - the test library (run, http, snmp, checks, tables, configure, wait_for, log, testbed roles)\n" +
			"- `lib/python/twlocal.py` - logs in to the devices (SSH, Telnet), sends API requests and SNMP, parses with TextFSM\n" +
			"- `lib/python/run_tests.py` - runs plan.txt and writes the reports\n" +
			"- `inventory.yaml` - the devices; `testbeds/` - which device plays each role; `templates/` - TextFSM templates\n" +
			"- `plan.txt` - the tests to run, in order; `run.*` - the run script\n\n" +
			"Steps that need Termwright itself (software upgrades, lab traffic tests, manual steps, saving and restoring the\nconfiguration) are skipped or reported here.\n"
	}
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

// runnerScripts run the suite with bin/twrun (downloaded from the Termwright release when it is not there).
func runnerScripts(o ExportOptions) map[string]string {
	tag := o.Version
	if tag == "" {
		tag = "nightly"
	}
	base := "https://github.com/nperiannan/termwright/releases/download/" + map[bool]string{true: "nightly", false: "v" + tag}[tag == "nightly"]
	if o.OS == "windows" {
		ps := `# Runs this Termwright suite with twrun - Termwright is not needed. Usage: .\run.ps1 [-Testbed testbeds\lab.yaml] [twrun options]
# The devices are in inventory.yaml; passwords come from TW_PASSWORD_<NAME> / TW_PASSWORD, or are asked.
param([string]$Testbed = "` + strings.ReplaceAll(o.Testbed, "/", `\`) + `")
Set-Location $PSScriptRoot
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$tw = "bin\twrun-windows-$arch.exe"
if (-not (Test-Path $tw)) {
  New-Item -ItemType Directory -Force bin | Out-Null
  Write-Host "Downloading twrun ($arch)…"
  Invoke-WebRequest -UseBasicParsing "` + base + `/twrun-windows-$arch.exe" -OutFile $tw
}
$files = Get-Content plan.txt | Where-Object { $_ -and -not $_.StartsWith("#") }
$tb = @(); if ($Testbed) { $tb = @("--testbed", $Testbed) }
& $tw run @files @tb @args
exit $LASTEXITCODE
`
		return map[string]string{"run.ps1": ps, "run.cmd": "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0run.ps1\" %*\r\n"}
	}
	osName := map[string]string{"linux": "linux", "mac": "darwin"}[o.OS]
	return map[string]string{"run.sh": `#!/usr/bin/env bash
# Runs this Termwright suite with twrun - Termwright is not needed. Usage: ./run.sh [testbeds/lab.yaml] [twrun options]
# The devices are in inventory.yaml; passwords come from TW_PASSWORD_<NAME> / TW_PASSWORD, or are asked.
cd "$(dirname "$0")" || exit 2
TESTBED=` + o.Testbed + `
case "$1" in testbeds/*) TESTBED=$1; shift ;; esac
case "$(uname -m)" in arm64|aarch64) ARCH=arm64 ;; *) ARCH=amd64 ;; esac
TW="bin/twrun-` + osName + `-$ARCH"
if [ ! -x "$TW" ]; then
  mkdir -p bin
  echo "Downloading twrun ($ARCH)…"
  curl -fsSL -o "$TW" "` + base + `/twrun-` + osName + `-$ARCH" || { echo "Download failed: put twrun-` + osName + `-$ARCH from the Termwright release into bin/" >&2; exit 2; }
  chmod +x "$TW"
fi
exec "$TW" run $(grep -v '^#' plan.txt) ${TESTBED:+--testbed "$TESTBED"} "$@"
`}
}
