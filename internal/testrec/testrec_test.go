package testrec

import (
	"context"
	"errors"
	"github.com/termwright/termwright/internal/textfsm"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fake struct {
	sent  []string
	ended int
}

func (f *fake) EndCase() { f.ended++ }

func (f *fake) Command(_ context.Context, session, cmd, _ string) (string, error) {
	f.sent = append(f.sent, session+": "+cmd)
	switch cmd {
	case "show version":
		return "ExtremeXOS version 32.6.1.4\nUptime: 3 days\n", nil
	case "show vlan 42":
		return "VLAN Interface with name v42 created by user\n", nil
	case "show vlan":
		return "1 active\n200 active\n300 suspended\n", nil
	}
	return "", errors.New("no answer")
}

func (f *fake) SNMP(_ context.Context, r SNMPStep) ([]SNMPValue, error) {
	f.sent = append(f.sent, r.Session+": snmp "+r.Op)
	return []SNMPValue{
		{OID: "1.3.6.1.2.1.1.5.0", Name: "SNMPv2-MIB::sysName.0", Value: "core-sw1", Display: "core-sw1"},
		{OID: "1.3.6.1.2.1.2.2.1.8.5", Name: "IF-MIB::ifOperStatus.5", Value: "1", Display: "up"},
		{OID: "1.3.6.1.2.1.2.2.1.10.5", Name: "IF-MIB::ifInOctets.5", Value: "123456", Display: "123456"},
	}, nil
}

func (f *fake) HTTP(_ context.Context, r HTTPStep) (HTTPAnswer, error) {
	f.sent = append(f.sent, r.Method+" "+r.URL)
	return HTTPAnswer{Status: 200, Body: `{"data":{"items":[{"name":"v42","tag":42},{"name":"v7","tag":7}]},"ok":true}`}, nil
}

const sample = `
name: VLAN 42 is up
vars: {vlan: "42"}
steps:
  - run: {session: Lab/sw1, command: show version}
    expect:
      - contains: ExtremeXOS
      - field: 'version (\S+)'
        equals: "32.6.1.4"
    save:
      - {var: ver, field: 'version (\S+)'}
  - run: {session: Lab/sw1, command: "show vlan {{vlan}}"}
    expect: [{matches: 'v{{vlan}} created'}]
  - http: {session: APIs/sw1, url: /vlans}
    expect:
      - status: 200
      - {json: "data.items[0].tag", greater: 40}
      - {json: data.items.length, equals: "2"}
      - {json: "data.items[-1].name", equals: v7}
      - {json: missing, exists: false}
      - {maxMs: 5000}
    save:
      - {var: first, json: "data.items[0].name"}
  - run: {session: Lab/sw1, command: "show vlan {{vlan}}"}
    name: uses saved {{first}} and {{ver}}
    expect: [{contains: "{{first}}"}]
`

func eq(s string) *string { return &s }

func TestRunPasses(t *testing.T) {
	c, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{}
	r := Run(context.Background(), c, f, nil)
	if !r.Passed {
		t.Fatalf("failed: %+v", r.Steps)
	}
	if f.sent[1] != "Lab/sw1: show vlan 42" || r.Steps[3].Name != "uses saved v42 and 32.6.1.4" {
		t.Fatalf("vars: %v / %q", f.sent, r.Steps[3].Name)
	}
}

func TestRunFailsAndSkips(t *testing.T) {
	c := Case{Name: "x", Steps: []Step{
		{Run: &RunStep{Session: "s", Command: "show version"}, Expect: []Expect{{Field: `version (\S+)`, Equals: eq("33.1")}}},
		{Run: &RunStep{Session: "s", Command: "show version"}},
	}}
	r := Run(context.Background(), c, &fake{}, nil)
	if r.Passed || !r.Steps[1].Skipped || r.Steps[0].Checks[0].Got != "32.6.1.4" {
		t.Fatalf("%+v", r)
	}
	c.Steps[0].ContinueOnFail = true
	if r = Run(context.Background(), c, &fake{}, nil); r.Steps[1].Skipped || r.Passed {
		t.Fatalf("continue: %+v", r)
	}
	c.Steps = []Step{{Run: &RunStep{Session: "s", Command: "reload"}}}
	if r = Run(context.Background(), c, &fake{}, nil); r.Passed || r.Steps[0].Error != "no answer" {
		t.Fatalf("error: %+v", r)
	}
}

func TestParseRefusesBadSteps(t *testing.T) {
	for _, s := range []string{"steps: [{}]", "steps: [{run: {session: a}}]", "steps: [{http: {}}]", "steps: [{run: {session: a, command: b}, http: {url: /}}]"} {
		if _, err := Parse([]byte(s)); err == nil {
			t.Errorf("%s: no error", s)
		}
	}
}

func TestFolderRunWritesReports(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	if err := f.Init(); err != nil {
		t.Fatal(err)
	}
	c, _ := Parse([]byte(sample))
	rel, err := f.Save("vlan/vlan42", c)
	if err != nil || rel != "cases/vlan/vlan42.yaml" {
		t.Fatal(rel, err)
	}
	os.WriteFile(filepath.Join(f.Root, "cases", "bad.yaml"), []byte("steps: [{}]"), 0o644)
	if _, err := f.Save("../../evil", c); err == nil {
		t.Fatal("saved outside the folder")
	}
	l := f.List()
	if len(l) != 2 || l[0].Error == "" || l[1].Name != "VLAN 42 is up" || l[1].Steps != 4 {
		t.Fatalf("list %+v", l)
	}
	if _, err := f.Load("vlan/vlan42"); err != nil {
		t.Fatal("short name:", err)
	}
	rep, err := f.RunFiles(context.Background(), []string{"all"}, &fake{}, nil, nil)
	if err != nil || rep.Passed != 1 || rep.Failed != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
	for _, n := range []string{"result.json", "junit.xml", "report.html"} {
		b, err := os.ReadFile(filepath.Join(f.Root, rep.Dir, n))
		if err != nil || len(b) == 0 {
			t.Fatal(n, err)
		}
		if n == "junit.xml" && (!strings.Contains(string(b), `tests="2"`) || !strings.Contains(string(b), `errors="1"`)) {
			t.Fatalf("junit: %s", b)
		}
	}
}

func TestSNMPStep(t *testing.T) {
	c, err := Parse([]byte(`
steps:
  - snmp: {session: Lab/sw1, op: walk, oids: [IF-MIB::ifTable]}
    expect:
      - {oid: ifOperStatus.5, equals: up}
      - {oid: IF-MIB::ifOperStatus.5, equals: "1"}
      - {oid: ifInOctets, greater: 1000}
      - {oid: 1.3.6.1.2.1.1.5.0, equals: core-sw1}
      - contains: "ifOperStatus.5 = up"
      - {oid: ifSpeed.5, exists: false}
    save: [{var: name, oid: sysName.0}]
  - snmp: {session: Lab/sw1, op: set, values: [{oid: sysContact.0, value: "{{name}}"}]}
    expect: [{oid: ifOperStatus.5, notEquals: down}]
`))
	if err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), c, &fake{}, nil)
	if !r.Passed {
		t.Fatalf("%+v", r.Steps)
	}
	if r.Steps[1].Sent != "set sysContact.0 = core-sw1" {
		t.Fatal(r.Steps[1].Sent)
	}
	if _, err := Parse([]byte("steps: [{snmp: {session: a, op: set}}]")); err == nil {
		t.Fatal("set without values")
	}
}

func TestJSONPath(t *testing.T) {
	v, ok := extract("", `a["b.c"][1]`, `{"a":{"b.c":[1,"x"]}}`)
	if !ok || v != "x" {
		t.Fatal(v, ok)
	}
	if _, ok := extract("", "a.z", `{"a":{}}`); ok {
		t.Fatal("found missing key")
	}
}

func TestTreeTestbedSignature(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	f.Init()
	c, _ := Parse([]byte(sample))
	f.Save("a", c)
	os.WriteFile(filepath.Join(f.Root, "testbeds", "lab.yaml"), []byte("vlan: 7\nlab:\n  sw1: Lab/core-sw1\n"), 0o644)
	sig := f.Signature()
	tb, err := f.Testbed("lab.yaml")
	if err != nil || tb["vlan"] != "7" || tb["lab.sw1"] != "Lab/core-sw1" {
		t.Fatal(tb, err)
	}
	f.RunFiles(context.Background(), nil, &fake{}, tb, nil)
	tr := f.Tree()
	if len(tr.Cases) != 1 || len(tr.Testbeds) != 1 || len(tr.Reports) != 1 || tr.Reports[0].Failed != 1 {
		t.Fatalf("%+v", tr) // vlan 7 is not in the fake's output, so the run fails
	}
	if f.Signature() == sig {
		t.Fatal("signature did not change")
	}
	if _, err := f.ReadText("../x"); err == nil {
		t.Fatal("read outside")
	}
}

func TestParamsAndRoles(t *testing.T) {
	c, err := Parse([]byte("name: v\nparams: [{vlan: \"42\"}, {vlan: \"7\"}]\nsteps:\n  - run: {session: s, command: \"show vlan {{vlan}}\"}\n    expect: [{contains: \"v{{vlan}}\"}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	fk := &fake{}
	rs := RunParams(context.Background(), c, fk, nil)
	if fk.ended != 2 {
		t.Fatal("EndCase per run:", fk.ended)
	}
	if len(rs) != 2 || rs[0].Case != "v [vlan=42]" || !rs[0].Passed || rs[1].Passed {
		t.Fatalf("%+v", rs) // the fake only knows vlan 42
	}
	if RoleName("Lab/core-sw1") != "core_sw1" || RoleName("Lab/7750 SR") != "dev_7750_sr" {
		t.Fatal(RoleName("Lab/core-sw1"), RoleName("Lab/7750 SR"))
	}
	f := Folder{Root: t.TempDir()}
	f.Init()
	rel, err := f.WriteTestbed("Lab A", []string{"Lab/core-sw1", "Lab/edge-sw3", "Other/core-sw1"}, "from Lab")
	if err != nil || rel != "testbeds/lab-a.yaml" {
		t.Fatal(rel, err)
	}
	tb, _ := f.Testbed("lab-a.yaml")
	if tb["core_sw1"] != "Lab/core-sw1" || tb["core_sw1_2"] != "Other/core-sw1" || tb["edge_sw3"] != "Lab/edge-sw3" {
		t.Fatal(tb)
	}
	if _, err := f.WriteTestbed("lab-a", nil, ""); err == nil {
		t.Fatal("replaced a testbed")
	}
}

type restorer struct {
	*fake
	saved, restored []string
}

func (r *restorer) SaveState(_ context.Context, kinds, sessions []string) ([]StepResult, func(context.Context) []StepResult) {
	r.saved = append(r.saved, sessions...)
	return []StepResult{{Name: "save " + kinds[0], Passed: true}}, func(context.Context) []StepResult {
		r.restored = append(r.restored, sessions...)
		return []StepResult{{Name: "restore " + kinds[0], Passed: true}}
	}
}

func TestSetupCleanupRestore(t *testing.T) {
	c, err := Parse([]byte(`
restore: [lab]
setup: [{run: {session: s1, command: show vlan 42}}]
steps:
  - run: {session: s1, command: show version}
    expect: [{contains: NOPE}]
  - run: {session: s2, command: show version}
cleanup:
  - run: {session: s1, command: reload}
  - snmp: {session: s3, op: get, oids: [sysName.0]}
`))
	if err != nil {
		t.Fatal(err)
	}
	rs := &restorer{fake: &fake{}}
	r := Run(context.Background(), c, rs, nil)
	ph := ""
	for _, s := range r.Steps {
		ph += s.Phase + ":" + map[bool]string{true: "ok", false: "x"}[s.Passed] + map[bool]string{true: "-", false: ""}[s.Skipped] + " "
	}
	// save, setup ok, step 1 fails, step 2 skipped, both cleanup steps run (the first fails), restore
	if ph != "restore:ok setup:ok :x :x- cleanup:x cleanup:ok restore:ok " || r.Passed || !r.CleanupFailed {
		t.Fatalf("%q %+v", ph, r)
	}
	if strings.Join(rs.saved, ",") != "s1,s2,s3" || len(rs.restored) != 3 {
		t.Fatal(rs.saved, rs.restored)
	}
	// without a restorer a restore test cannot run; a bad restore kind is refused
	if r := Run(context.Background(), c, &fake{}, nil); r.Passed || !strings.Contains(r.Error, "restore") {
		t.Fatal(r.Error)
	}
	if _, err := Parse([]byte("restore: [disk]\nsteps: [{run: {session: a, command: b}}]")); err == nil {
		t.Fatal("bad restore kind")
	}
	// a failed setup skips the steps but not the cleanup
	c2, _ := Parse([]byte("setup: [{run: {session: s, command: nope}}]\nsteps: [{run: {session: s, command: show version}}]\ncleanup: [{run: {session: s, command: show version}}]"))
	r2 := Run(context.Background(), c2, &fake{}, nil)
	if !r2.Steps[1].Skipped || r2.Steps[2].Skipped || r2.Steps[2].Phase != "cleanup" || r2.CleanupFailed {
		t.Fatalf("%+v", r2.Steps)
	}
}

type recov struct {
	*fake
	base, rec   []string
	failRecover bool
}

func (r *recov) Baseline(_ context.Context, s []string) []StepResult {
	r.base = append(r.base, s...)
	return []StepResult{{Name: "save configuration", Target: strings.Join(s, ","), Passed: true}}
}
func (r *recov) Recover(_ context.Context, mode string, s []string) []StepResult {
	r.rec = append(r.rec, mode+":"+strings.Join(s, ","))
	return []StepResult{{Name: mode, Target: strings.Join(s, ","), Passed: !r.failRecover}}
}
func (r *recov) DeviceError(_, out string) string {
	if strings.HasPrefix(out, "Error:") {
		return strings.TrimSpace(out)
	}
	return ""
}

func TestCleanupFailurePolicy(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	f.Init()
	bad, _ := Parse([]byte("steps: [{run: {session: s1, command: show version}}]\ncleanup: [{run: {session: s1, command: nope}}]"))
	good, _ := Parse([]byte("steps: [{run: {session: s2, command: show version}}]"))
	f.Save("a-bad", bad)
	f.Save("b-good", good)
	f.Save("c-good", good)
	// stop (default): the cases after the failed cleanup are skipped with the reason
	rep, err := f.RunFilesWith(context.Background(), nil, &fake{}, nil, RunOptions{}, nil)
	if err != nil || rep.Failed != 1 || rep.Skipped != 2 || !rep.Results[1].Skipped || !strings.Contains(rep.Results[1].Error, "cleanup of") {
		t.Fatalf("%+v %v", rep, err)
	}
	if j := string(JUnit(rep)); !strings.Contains(j, `skipped="2"`) || !strings.Contains(j, "the cleanup failed") {
		t.Fatal(j)
	}
	// restore: saved before the run, put back after the failed cleanup, the run goes on
	r := &recov{fake: &fake{}}
	rep, err = f.RunFilesWith(context.Background(), nil, r, nil, RunOptions{OnCleanupFail: "restore"}, nil)
	if err != nil || rep.Passed != 2 || rep.Skipped != 0 || strings.Join(r.base, ",") != "s1,s2" || len(r.rec) != 1 || r.rec[0] != "restore:s1" || len(rep.Recovery) != 2 {
		t.Fatalf("%+v %v %v %v", rep, err, r.base, r.rec)
	}
	// factory that fails: the rest is skipped
	r = &recov{fake: &fake{}, failRecover: true}
	rep, _ = f.RunFilesWith(context.Background(), nil, r, nil, RunOptions{OnCleanupFail: "factory"}, nil)
	if rep.Skipped != 2 || !strings.Contains(rep.Results[2].Error, "factory default") || len(r.base) != 0 {
		t.Fatalf("%+v", rep)
	}
	// a driver that cannot recover refuses restore and factory
	if _, err := f.RunFilesWith(context.Background(), nil, &fake{}, nil, RunOptions{OnCleanupFail: "factory"}, nil); err == nil {
		t.Fatal("factory without a recoverer")
	}
	if h := string(HTML(rep)); !strings.Contains(h, "skipped") || !strings.Contains(h, "failed (cleanup)") {
		t.Fatal("html")
	}
}

type errFake struct{ *recov }

func (e errFake) Command(_ context.Context, s, cmd, _ string) (string, error) {
	if cmd == "create vlan v1" {
		return "Error: VLAN v1 already exists", nil
	}
	return "ok", nil
}

func TestDeviceErrorsAndUnchecked(t *testing.T) {
	c, _ := Parse([]byte("steps:\n  - run: {session: s, command: create vlan v1, ignoreErrors: true}\n  - run: {session: s, command: show x}\n  - run: {session: s, command: create vlan v1}\n"))
	r := Run(context.Background(), c, errFake{&recov{fake: &fake{}}}, nil)
	if !r.Steps[0].Passed || !r.Steps[1].Passed || !r.Steps[1].Unchecked || r.Steps[2].Passed || !strings.Contains(r.Steps[2].Error, "refused the command: Error: VLAN v1 already exists") {
		t.Fatalf("%+v", r.Steps)
	}
}

func TestRemoveRename(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	f.Init()
	c, _ := Parse([]byte("name: a\nsteps: [{run: {session: s, command: show version}}]"))
	rel, _ := f.Save("x/a", c)
	f.WriteGo(rel)
	dup, err := f.Rename(rel, "x/b", true)
	if err != nil || dup != "cases/x/b.yaml" {
		t.Fatal(dup, err)
	}
	if b, _ := f.Load(dup); b.Name != "a (copy)" {
		t.Fatal(b.Name)
	}
	mv, err := f.Rename(rel, "y/c", false)
	if err != nil || mv != "cases/y/c.yaml" || fileThere(f, "cases/x/a.yaml") || !fileThere(f, "cases/y/c_test.go") {
		t.Fatal(mv, err)
	}
	where, err := f.Remove(dup)
	if err != nil || fileThere(f, "cases/x/b.yaml") || !fileThere(f, where+"/cases/x/b.yaml") || !fileThere(f, where+"/cases/x/b_test.go") {
		t.Fatal(where, err)
	}
	if len(f.List()) != 1 {
		t.Fatal("removed tests are not listed", f.List())
	}
	if l := f.LibFiles(); !slices.Contains(l, "lib/twtest/twtest.go") {
		t.Fatal(l)
	}
	if _, err := f.Remove("../../etc/passwd"); err == nil {
		t.Fatal("removed outside")
	}
}

func fileThere(f Folder, rel string) bool {
	_, err := os.Stat(filepath.Join(f.Root, rel))
	return err == nil
}

func (f *fake) ParseOutput(_, _, tpl, out string) (*textfsm.Table, error) {
	t, err := textfsm.Parse("Value ID (\\d+)\nValue STATE (\\S+)\n\nStart\n  ^${ID}\\s+${STATE} -> Record\n")
	if err != nil {
		return nil, err
	}
	tab, err := t.Run(out)
	return &tab, err
}

func TestHasTextSpacing(t *testing.T) {
	out := "  IP Anycast:         Disabled\n  IPv4 Forwarding:    Disabled\n  Primary IP:          200.1.1.1/24\n"
	if !HasText(out, "Primary IP: 200.1.1.1/24") || !HasText(out, "IP Anycast: Disabled IPv4 Forwarding: Disabled") || HasText(out, "Primary IP: 200.1.1.2") {
		t.Fatal("spacing")
	}
}

func (f *fake) UndoOf(_, cmd string) string {
	if cmd == "show vlan 42" {
		return "show version"
	}
	return ""
}

func TestCleanupModes(t *testing.T) {
	c, _ := Parse([]byte("steps:\n  - run: {session: s, command: show vlan 42}\n  - run: {session: s, command: show version}\ncleanup: [{run: {session: s, command: show vlan}}]\n"))
	f := &fake{}
	r := RunWith(context.Background(), c, f, nil, RunOptions{Cleanup: CleanupOff})
	if !r.Passed || len(f.sent) != 2 || !r.Steps[2].Skipped || r.Steps[2].Error == "" {
		t.Fatal(f.sent, r.Steps)
	}
	f = &fake{}
	c.Cleanup = nil
	r = RunWith(context.Background(), c, f, nil, RunOptions{Cleanup: CleanupAuto})
	if !r.Passed || len(f.sent) != 3 || f.sent[2] != "s: show version" || r.Steps[2].Name != "undo: show vlan 42" {
		t.Fatal(f.sent, r.Steps)
	}
}

func TestSuitesTagsDeps(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	f.Init()
	mk := func(rel, y string) {
		c, err := Parse([]byte(y))
		if err != nil {
			t.Fatal(err)
		}
		f.Save(rel, c)
	}
	mk("vlan/create", "name: create\ntags: [l2, smoke]\nsteps: [{run: {session: s, command: show vlan 42}, expect: [{contains: NOPE}]}]")
	mk("vlan/ports", "name: ports\ntags: [l2]\ndependsOn: [vlan/create]\nsteps: [{run: {session: s, command: show version}}]")
	mk("sys/ver", "name: ver\ntags: [smoke]\nsteps: [{run: {session: s, command: show version}}]")
	f.SaveSuite("l2", Suite{Tests: []string{"cases/vlan/ports.yaml", "cases/vlan/create.yaml"}})
	f.SaveSuite("nightly", Suite{Tests: []string{"suites/l2.yaml", "cases/sys"}, Tags: []string{"!skipme"}})
	got, err := f.ExpandFiles([]string{"suites/nightly.yaml"}, nil)
	if err != nil || strings.Join(got, ",") != "cases/vlan/create.yaml,cases/vlan/ports.yaml,cases/sys/ver.yaml" {
		t.Fatal(got, err)
	}
	if got, _ := f.ExpandFiles(nil, []string{"smoke", "!l2"}); strings.Join(got, ",") != "cases/sys/ver.yaml" {
		t.Fatal(got)
	}
	if got, _ := f.ExpandFiles([]string{"cases/vlan"}, nil); len(got) != 2 {
		t.Fatal(got)
	}
	rep, err := f.RunFilesWith(context.Background(), []string{"suites/l2.yaml"}, &fake{}, nil, RunOptions{}, nil)
	if err != nil || rep.Failed != 1 || rep.Skipped != 1 || !strings.Contains(rep.Results[1].Error, "depends on") {
		t.Fatal(rep, err)
	}
	if s := f.Suites(); len(s) != 2 || s[1].Tests != 3 {
		t.Fatal(s)
	}
}

func TestLogic(t *testing.T) {
	for in, want := range map[string]bool{"42 == 42": true, "v42 != v42": false, "abc contains b": true, "10 > 9": true, "x ~ ^x$": true, "": false, "{{y}}": false, "yes": true, "abc !contains z": true} {
		if got, err := Cond(in); err != nil || got != want {
			t.Fatal(in, got, err)
		}
	}
	if v, _ := rangeValues("1:1-1:3"); strings.Join(v, ",") != "1:1,1:2,1:3" {
		t.Fatal(v)
	}
	c, err := Parse([]byte(`vars: {list: "show version, show vlan 42"}
steps:
  - wait: 0.01
  - loop: {var: cmd, from: "{{list}}", steps: [{run: {session: s, command: "{{cmd}}"}}]}
  - run: {session: s, command: show version}
    if: "{{list}} contains vlan"
  - run: {session: s, command: show vlan 42}
    if: "1 == 2"
  - run: {session: s, command: show version}
    expect: [{contains: NOPE}]
    retry: {timeout: 0.05, interval: 0.02}
`))
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{}
	r := Run(context.Background(), c, f, nil)
	names := []string{}
	for _, s := range r.Steps {
		names = append(names, s.Name)
	}
	if r.Passed || len(r.Steps) != 6 || r.Steps[1].Name != "show version [cmd=show version]" || !r.Steps[4].Skipped || !strings.Contains(r.Steps[5].Error, "did not pass") || len(f.sent) < 5 {
		t.Fatal(names, r.Steps[5], f.sent)
	}
	if _, err := Parse([]byte("steps: [{loop: {var: x, steps: [{run: {session: s, command: a}}]}}]")); err == nil {
		t.Fatal("loop without values parsed")
	}
}
