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
			Action string `json:"action"`
			Step   Step   `json:"step"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		res := RunOne(context.Background(), in.Step, fk)
		b, _ := json.Marshal(res)
		json.NewEncoder(w).Encode(map[string]string{"result": string(b)})
	}))
	defer srv.Close()
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
	pc, _ := Parse([]byte("params: [{vlan: \"42\"}]\nsteps:\n  - run: {session: \"{{site.sw}}\", command: \"show vlan {{vlan}}\"}\n    expect: [{contains: created}]\n"))
	f.Save("params/vlans", pc)
	os.WriteFile(filepath.Join(f.Root, "testbeds", "nested.yaml"), []byte("site:\n  sw: Lab/sw1   # the core\n"), 0o644)
	for _, file := range []string{"cases/vlan/vlan42.yaml", "cases/other/bad.yaml", "cases/snmp/ifs.yaml", "cases/params/vlans.yaml"} {
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
	if out, err := run("test", "-count=1", "./cases/vlan/", "./cases/snmp/"); err != nil {
		t.Fatalf("vlan: %v\n%s", err, out)
	}
	if out, err := run("test", "-count=1", "-v", "./cases/params/", "-args", "-tw.testbed=testbeds/nested.yaml"); err != nil || !strings.Contains(out, "[vlan=42]") || !strings.Contains(fk.sent[len(fk.sent)-1], "Lab/sw1: show vlan 42") {
		t.Fatalf("params: %v\n%s\n%v", err, out, fk.sent)
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
		if out, err := prun([]string{"TW_TESTBED=testbeds/nested.yaml"}, "cases/vlan", "cases/snmp", "cases/params", "--junit", "py.xml"); err != nil {
			t.Fatalf("python: %v\n%s", err, out)
		}
		if x, _ := os.ReadFile(filepath.Join(f.Root, "py.xml")); !strings.Contains(string(x), "<testsuite") || !strings.Contains(string(x), "params='vlan=42'") {
			t.Fatalf("python junit: %s", x)
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
		if out, err := trun([]string{"TW_TESTBED=testbeds/nested.yaml"}, "cases/vlan", "cases/snmp", "cases/params", "-junit", "tcl.xml"); err != nil || !strings.Contains(out, "ok 1 - show vlan 42") {
			t.Fatalf("tcl: %v\n%s", err, out)
		}
		if x, _ := os.ReadFile(filepath.Join(f.Root, "tcl.xml")); !strings.Contains(string(x), `failures="0"`) {
			t.Fatalf("tcl junit: %s", x)
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
