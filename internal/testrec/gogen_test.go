package testrec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The generated Go for the sample case: one subtest per step, checks as calls, variables saved.
func TestGenerateGoGolden(t *testing.T) {
	c, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	src, err := GenerateGo(c, "cases/vlan/vlan-42_up.yaml")
	if err != nil {
		t.Fatal(err, string(src))
	}
	py, _ := GeneratePython(c, "cases/vlan/vlan-42_up.yaml")
	if PyFile("cases/vlan/vlan-42_up.yaml") != "cases/vlan/test_vlan_vlan_42_up.py" {
		t.Fatal(PyFile("cases/vlan/vlan-42_up.yaml"))
	}
	pyGolden := filepath.Join("testdata", "vlan42_test.py.golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.WriteFile(pyGolden, py, 0o644)
	}
	if want, err := os.ReadFile(pyGolden); err != nil || string(want) != string(py) {
		t.Fatalf("generated Python differs from %s:\n%s", pyGolden, py)
	}
	tcl, _ := GenerateTcl(c, "cases/vlan/vlan-42_up.yaml")
	tclGolden := filepath.Join("testdata", "vlan42.test.tcl.golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.WriteFile(tclGolden, tcl, 0o644)
	}
	if want, err := os.ReadFile(tclGolden); err != nil || string(want) != string(tcl) {
		t.Fatalf("generated TCL differs from %s:\n%s", tclGolden, tcl)
	}
	golden := filepath.Join("testdata", "vlan42_test.go.golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.MkdirAll("testdata", 0o755)
		os.WriteFile(golden, src, 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil || string(want) != string(src) {
		t.Fatalf("generated Go differs from %s (UPDATE_GOLDEN=1 go test to update):\n%s", golden, src)
	}
	for _, s := range []string{"package vlan", "func TestVlan42Up(t *testing.T)", `a.Field(` + "`version (\\S+)`" + `).Equals("32.6.1.4")`, `a.JSON("data.items[0].name").Save("first")`, `a.StatusIs(200)`} {
		if !strings.Contains(string(src), s) {
			t.Errorf("missing %s", s)
		}
	}
}

// The generated tests and lib compile and run against a fake Termwright API: the sample passes;
// a failed check fails its step and skips the next one; junit makes XML of go test -json.
var fkMu sync.Mutex

func TestGeneratedGoRuns(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil || testing.Short() {
		t.Skip("no go toolchain")
	}
	fk := &fake{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tools/tests" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "no", 401)
			return
		}
		var in struct {
			Action   string   `json:"action"`
			Step     Step     `json:"step"`
			Sessions []string `json:"sessions"`
			Token    string   `json:"token"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		fkMu.Lock() // the generated tests run in parallel (t.Parallel / several packages)
		defer fkMu.Unlock()
		switch in.Action {
		case "save_state":
			fk.sent = append(fk.sent, "save "+strings.Join(in.Sessions, ","))
			json.NewEncoder(w).Encode(map[string]string{"result": `{"token":"t1","results":[{"name":"save lab","passed":true}]}`})
			return
		case "restore_state":
			fk.sent = append(fk.sent, "restore "+in.Token)
			json.NewEncoder(w).Encode(map[string]string{"result": `{"results":[{"name":"restore lab","passed":true}]}`})
			return
		}
		res := RunOne(context.Background(), in.Step, fk)
		b, _ := json.Marshal(res)
		json.NewEncoder(w).Encode(map[string]string{"result": string(b)})
	}))
	defer srv.Close()
	sent := func() []string { fkMu.Lock(); defer fkMu.Unlock(); return append([]string(nil), fk.sent...) }
	f := Folder{Root: t.TempDir()}
	f.Init()
	c, _ := Parse([]byte(sample))
	f.Save("vlan/vlan42", c)
	bad := Case{Name: "bad", Steps: []Step{
		{Run: &RunStep{Session: "s", Command: "show version"}, Expect: []Expect{{Contains: "NOPE"}}},
		{Run: &RunStep{Session: "s", Command: "show version"}},
	}}
	f.Save("other/bad", bad)
	sn, err := Parse([]byte("steps:\n  - snmp: {session: Lab/sw1, op: walk, oids: [IF-MIB::ifTable]}\n    expect: [{oid: ifOperStatus.5, equals: up}, {oid: ifInOctets, greater: 1000}]\n    save: [{var: n, oid: sysName.0}]\n  - snmp: {session: Lab/sw1, op: set, values: [{oid: sysContact.0, value: \"{{n}}\"}]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	f.Save("snmp/ifs", sn)
	cl, err := Parse([]byte("restore: [lab]\nsetup: [{run: {session: Lab/sw1, command: show version}}]\nsteps:\n  - run: {session: Lab/sw1, command: show version}\n    expect: [{contains: NOPE}]\ncleanup: [{run: {session: Lab/sw1, command: show vlan 42}}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	f.Save("clean/c", cl)
	pc, _ := Parse([]byte("params: [{vlan: \"42\"}]\nsteps:\n  - run: {session: \"{{site.sw}}\", command: \"show vlan {{vlan}}\"}\n    expect: [{contains: created}]\n"))
	f.Save("params/vlans", pc)
	tb, err := Parse([]byte("steps:\n  - run: {session: Lab/sw1, command: show vlan, parse: auto}\n    expect:\n      - table: {where: {ID: \"200\"}, column: STATE, equals: active}\n      - table: {where: {STATE: \"~^act\"}, rows: 2}\n      - table: {where: {ID: \"300\"}, column: STATE, matches: \"^susp\"}\n      - table: {where: {ID: \"999\"}, exists: false}\n    save: [{var: st, where: {ID: \"300\"}, column: STATE}]\n  - run: {session: Lab/sw1, command: show vlan, parse: auto}\n    expect: [{table: {where: {STATE: \"{{st}}\"}, column: ID, equals: \"300\"}}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	f.Save("tables/t", tb)
	lg, err := Parse([]byte(`vars: {cmds: "show version, show vlan 42"}
steps:
  - wait: 0.01
  - loop: {var: c, from: "{{cmds}}", steps: [{run: {session: Lab/sw1, command: "{{c}}"}}]}
  - loop: {var: p, values: ["1", "2"], steps: [{run: {session: Lab/sw1, command: show version}, expect: [{contains: ExtremeXOS}]}]}
  - run: {session: Lab/sw1, command: show vlan 42}
    if: "{{cmds}} contains vlan"
    retry: {timeout: 1, interval: 0.1}
    expect: [{contains: created}]
  - run: {session: Lab/sw1, command: show vlan 42}
    if: "1 == 2"
    expect: [{contains: NOPE}]
`))
	if err != nil {
		t.Fatal(err)
	}
	f.Save("logic/l", lg)
	if r := Run(context.Background(), lg, fk, nil); !r.Passed {
		t.Fatalf("logic run: %+v", r)
	}
	if r := Run(context.Background(), tb, fk, nil); !r.Passed {
		t.Fatalf("table run: %+v", r)
	}
	os.WriteFile(filepath.Join(f.Root, "testbeds", "nested.yaml"), []byte("site:\n  sw: Lab/sw1   # the core\n"), 0o644)
	for _, file := range []string{"cases/vlan/vlan42.yaml", "cases/other/bad.yaml", "cases/snmp/ifs.yaml", "cases/params/vlans.yaml", "cases/clean/c.yaml", "cases/tables/t.yaml", "cases/logic/l.yaml"} {
		if _, err := f.WriteGo(file); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(gobin, args...)
		cmd.Dir = f.Root
		cmd.Env = append(os.Environ(), "TERMWRIGHT_API="+srv.URL+"/api/v1", "TERMWRIGHT_KEY=k", "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run("test", "-count=1", "./cases/vlan/", "./cases/snmp/", "./cases/tables/", "./cases/logic/"); err != nil {
		t.Fatalf("vlan: %v\n%s", err, out)
	}
	if out, err := run("test", "-count=1", "-v", "./cases/params/", "-args", "-tw.testbed=testbeds/nested.yaml"); err != nil || !strings.Contains(out, "[vlan=42]") || !strings.Contains(sent()[len(sent())-1], "Lab/sw1: show vlan 42") {
		t.Fatalf("params: %v\n%s\n%v", err, out, sent())
	}
	checkClean := func(lang, out string) {
		t.Helper()
		n := len(sent())
		if n < 2 || sent()[n-1] != "restore t1" || sent()[n-2] != "Lab/sw1: show vlan 42" {
			t.Fatalf("%s clean: cleanup or restore did not run: %v\n%s", lang, sent(), out)
		}
	}
	if out, err := run("test", "-count=1", "-v", "./cases/clean/"); err == nil || !strings.Contains(out, "cleanup:_show_vlan_42") {
		t.Fatalf("go clean: %v\n%s", err, out)
	} else {
		checkClean("go", out)
	}
	out, err := run("test", "-count=1", "-v", "./cases/other/", "-args", "-tw.var=x=1")
	if err == nil || !strings.Contains(out, `expected the output to contain "NOPE"`) || !strings.Contains(out, "an earlier step failed") {
		t.Fatalf("bad: %v\n%s", err, out)
	}
	if py, err := exec.LookPath("python3"); err == nil {
		prun := func(env []string, args ...string) (string, error) {
			cmd := exec.Command(py, append([]string{"lib/python/run_tests.py"}, args...)...)
			cmd.Dir = f.Root
			cmd.Env = append(append(os.Environ(), "TERMWRIGHT_API="+srv.URL+"/api/v1", "TERMWRIGHT_KEY=k"), env...)
			out, err := cmd.CombinedOutput()
			return string(out), err
		}
		if out, err := prun([]string{"TW_TESTBED=testbeds/nested.yaml"}, "cases/vlan", "cases/snmp", "cases/params", "cases/tables", "cases/logic", "--junit", "py.xml"); err != nil {
			t.Fatalf("python: %v\n%s", err, out)
		}
		if x, _ := os.ReadFile(filepath.Join(f.Root, "py.xml")); !strings.Contains(string(x), "<testsuite") || !strings.Contains(string(x), "params='vlan=42'") {
			t.Fatalf("python junit: %s", x)
		}
		if out, err := prun(nil, "cases/clean"); err == nil {
			t.Fatalf("python clean passed:\n%s", out)
		} else {
			checkClean("python", out)
		}
		out, err := prun(nil, "cases/other")
		if err == nil || !strings.Contains(out, `expected the output to contain 'NOPE'`) || !strings.Contains(out, "an earlier step failed") {
			t.Fatalf("python bad: %v\n%s", err, out)
		}
	}
	if tclsh, err := exec.LookPath("tclsh"); err == nil {
		trun := func(env []string, args ...string) (string, error) {
			cmd := exec.Command(tclsh, append([]string{"lib/tcl/run_tests.tcl"}, args...)...)
			cmd.Dir = f.Root
			cmd.Env = append(append(os.Environ(), "TERMWRIGHT_API="+srv.URL+"/api/v1", "TERMWRIGHT_KEY=k"), env...)
			out, err := cmd.CombinedOutput()
			return string(out), err
		}
		if out, err := trun([]string{"TW_TESTBED=testbeds/nested.yaml"}, "cases/vlan", "cases/snmp", "cases/params", "cases/tables", "cases/logic", "-junit", "tcl.xml"); err != nil || !strings.Contains(out, "ok 1 - show vlan 42") {
			t.Fatalf("tcl: %v\n%s", err, out)
		}
		if x, _ := os.ReadFile(filepath.Join(f.Root, "tcl.xml")); !strings.Contains(string(x), `failures="0"`) {
			t.Fatalf("tcl junit: %s", x)
		}
		if out, err := trun(nil, "cases/clean"); err == nil || !strings.Contains(out, "ok 4 - cleanup: show vlan 42") {
			t.Fatalf("tcl clean: %v\n%s", err, out)
		} else {
			checkClean("tcl", out)
		}
		out, err := trun(nil, "cases/other")
		if err == nil || !strings.Contains(out, `expected the output to contain "NOPE"`) || !strings.Contains(out, "# SKIP an earlier step failed") {
			t.Fatalf("tcl bad: %v\n%s", err, out)
		}
	}
	j, _ := run("test", "-count=1", "-json", "./cases/...")
	cmd := exec.Command(gobin, "run", "./lib/junit")
	cmd.Dir, cmd.Stdin = f.Root, strings.NewReader(j)
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")
	x, _ := cmd.Output()
	if !strings.Contains(string(x), "<testsuite") || !strings.Contains(string(x), `failures="`) {
		t.Fatalf("junit: %s", x)
	}
}
