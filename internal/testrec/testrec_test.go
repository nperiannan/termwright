package testrec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fake struct{ sent []string }

func (f *fake) Command(_ context.Context, session, cmd, _ string) (string, error) {
	f.sent = append(f.sent, session+": "+cmd)
	switch cmd {
	case "show version":
		return "ExtremeXOS version 32.6.1.4\nUptime: 3 days\n", nil
	case "show vlan 42":
		return "VLAN Interface with name v42 created by user\n", nil
	}
	return "", errors.New("no answer")
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
