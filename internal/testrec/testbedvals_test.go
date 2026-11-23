package testrec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTestbedValues(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	os.MkdirAll(filepath.Join(f.Root, "testbeds"), 0o755)
	os.WriteFile(filepath.Join(f.Root, "testbeds", "lab.yaml"), []byte("# lab\ndev_1: Lab/sw1\nvlan: \"200\"\n"), 0o644)
	if err := f.SetTestbedValue("lab.yaml", "dev_1", "port", "1:2"); err != nil {
		t.Fatal(err)
	}
	if err := f.RenameTestbedRole("lab.yaml", "dev_1", "dut"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetTestbedRole("lab.yaml", "stack", "Lab/sw2"); err != nil {
		t.Fatal(err)
	}
	m, err := f.Testbed("lab.yaml")
	if err != nil || m["dut"] != "Lab/sw1" || m["dut.port"] != "1:2" || m["stack"] != "Lab/sw2" || m["vlan"] != "200" {
		t.Fatalf("%v %v", err, m)
	}
	b, _ := os.ReadFile(filepath.Join(f.Root, "testbeds", "lab.yaml"))
	if !strings.HasPrefix(string(b), "# lab\n") {
		t.Fatalf("%s", b)
	}
	f.SetTestbedValue("lab.yaml", "dut", "port", "")
	if m, _ := f.Testbed("lab.yaml"); m["dut"] != "Lab/sw1" || m["dut.port"] != "" {
		t.Fatalf("%v", m)
	}
}

type factDrv struct{ *fake }

func (factDrv) Facts(s string) map[string]string {
	if s == "Lab/sw1" {
		return map[string]string{"device_type": "VOSS"}
	}
	return nil
}

func TestFacts(t *testing.T) {
	v := map[string]string{"dut": "Lab/sw1", "dut.device_type": "kept", "x": "Lab/sw1"}
	addFacts(factDrv{&fake{}}, v, nil)
	if v["dut.device_type"] != "kept" || v["x.device_type"] != "VOSS" || v["x.port1"] != "1/1" || v["port1"] != "1/1" || v["ports"] == "" {
		t.Fatal(v)
	}
	// a testbed with more devices than the test uses: {{port1}} is the test's own device's
	tb := map[string]string{"dut": "Lab/sw1", "other": "Lab/sw2"}
	v = map[string]string{"dut": "Lab/sw1", "other": "Lab/sw2", "dut.port1": "7"}
	addFacts(factDrv{&fake{}}, v, map[string]string{"dut.port1": "7", "dut": "Lab/sw1", "other": "Lab/sw2"}, caseRoles(Case{Vars: map[string]string{"dut": "x"}}))
	if v["port1"] != "7" {
		t.Fatal(v["port1"], tb)
	}
}

func TestUsePortVars(t *testing.T) {
	eq := "1:2"
	c := Case{Vars: map[string]string{"stk": "Lab/stack1", "sa": "Lab/sw2"}, Steps: []Step{
		{Run: &RunStep{Session: "{{stk}}", Command: "configure vlan v1000 add ports 1:2,1:3 tagged"}, Expect: []Expect{{Contains: "Tag: 1:2"}, {Contains: "11:2"}, {Equals: &eq}}},
		{Run: &RunStep{Session: "{{sa}}", Command: "configure vlan v1 add ports 2-4 untagged vlan 2"}, Expect: []Expect{{Contains: "port 2"}}},
	}}
	ports := func(s string) []string { return DefaultPorts("EXOS", s == "Lab/stack1", "", 0, "") }
	c2, n := UsePortVars(c, ports)
	if n != 6 || c2.Steps[0].Run.Command != "configure vlan v1000 add ports {{stk.port1}},{{stk.port2}} tagged" ||
		c2.Steps[0].Expect[0].Contains != "Tag: {{stk.port1}}" || c2.Steps[0].Expect[1].Contains != "11:2" || *c2.Steps[0].Expect[2].Equals != "{{stk.port1}}" ||
		c2.Steps[1].Run.Command != "configure vlan v1 add ports {{sa.port1}}-{{sa.port3}} untagged vlan 2" || c2.Steps[1].Expect[0].Contains != "port 2" {
		t.Fatalf("%d %+v %+v", n, c2.Steps[0], c2.Steps[1].Run)
	}
	one := Case{Vars: map[string]string{"stk": "Lab/stack1"}, Steps: []Step{{Run: &RunStep{Session: "{{stk}}", Command: "configure vlan v1 delete ports 1:2"}, Expect: []Expect{{Contains: "Tag: {port1} {{port2}}"}}}}}
	if c3, n := UsePortVars(one, ports); n != 2 || c3.Steps[0].Run.Command != "configure vlan v1 delete ports {{port1}}" || c3.Steps[0].Expect[0].Contains != "Tag: {{port1}} {{port2}}" {
		t.Fatal(c3.Steps[0].Run.Command)
	}
	if p := DefaultPorts("VOSS", false, "", 0, ""); p[0] != "1/1" || p[7] != "1/8" {
		t.Fatal(p)
	}
	if p := DefaultPorts("EXOS", true, "3", 0, ""); p[0] != "3:2" || p[7] != "3:9" {
		t.Fatal(p)
	}
	if p := DefaultPorts("EXOS", true, "", 30, "1:28,2:28,3:28"); len(p) != 30 || p[26] != "1:28" || p[27] != "2:1" || p[29] != "2:3" {
		t.Fatal(p)
	}
	if p := DefaultPorts("EXOS", true, "", 90, "1:28,2:28"); len(p) != 55 {
		t.Fatal(len(p))
	}
	v := map[string]string{"d.device_type": "EXOS", "d.port_count": "3", "d.port2": "x"}
	addPorts("d", v, map[string]string{"d.port2": "x"})
	if v["d.port1"] != "2" || v["d.port3"] != "4" || v["d.port4"] != "" || v["d.ports"] != "2,x,4" || v["d.port2"] != "x" {
		t.Fatal(v)
	}
}

type undoDrv struct{ *fake }

func (undoDrv) UndoOf(_, cmd string) string {
	if f := strings.Fields(cmd); len(f) > 2 && f[0] == "create" {
		return "delete " + f[1] + " " + f[2]
	}
	return ""
}

func TestCleanupOnlyWhatRan(t *testing.T) {
	c := Case{Name: "x", Steps: []Step{
		{Run: &RunStep{Session: "sw", Command: "show vlan"}, Expect: []Expect{{Contains: "not there"}}},
		{Run: &RunStep{Session: "sw", Command: "create vlan v1000 tag 1000"}},
	}, Cleanup: []Step{{Run: &RunStep{Session: "sw", Command: "delete vlan v1000"}}, {Run: &RunStep{Session: "sw", Command: "show version"}}}}
	f := &fake{}
	r := Run(context.Background(), c, undoDrv{f}, nil)
	if len(r.Steps) != 4 || !r.Steps[2].Skipped || !strings.Contains(r.Steps[2].Error, "not needed") || r.Steps[3].Skipped {
		t.Fatalf("%+v", r.Steps)
	}
	for _, s := range f.sent {
		if strings.Contains(s, "delete") {
			t.Fatal("ran", s)
		}
	}
}

func TestStepDefaults(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	c := Case{Defaults: &StepOptions{Timeout: 20, Retry: &Retry{Timeout: 30}}, Steps: []Step{
		{Run: &RunStep{Session: "s", Command: "a"}},
		{Run: &RunStep{Session: "s", Command: "b"}, Options: "none"},
		{Run: &RunStep{Session: "s", Command: "c"}, Timeout: 5},
		{Run: &RunStep{Session: "s", Command: "d"}, Options: "custom"},
	}}
	pc, _ := f.Prepare(c)
	s := pc.Steps
	if s[0].Timeout != 20 || s[0].Retry == nil || s[1].Timeout != 0 || s[2].Timeout != 5 || s[2].Retry != nil || s[3].Timeout != 0 || c.Steps[0].Timeout != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestFramework(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	os.MkdirAll(filepath.Join(f.Root, "cases", "l2"), 0o755)
	os.MkdirAll(filepath.Join(f.Root, "testbeds"), 0o755)
	os.WriteFile(filepath.Join(f.Root, "defaults.yaml"), []byte("vars: {vlan: \"100\", site: lab}\nstep_options: {timeout: 30}\n"), 0o644)
	os.WriteFile(filepath.Join(f.Root, "cases", "l2", "defaults.yaml"), []byte("vars: {vlan: \"200\"}\nstep_options: {retry: {timeout: 20}}\n"), 0o644)
	os.WriteFile(filepath.Join(f.Root, "cases", "l2", "t.yaml"), []byte("name: t\nvars: {site: own}\nsteps:\n  - run: {session: s, command: \"show vlan {{vlan}}\"}\n"), 0o644)
	if l := f.List(); len(l) != 1 {
		t.Fatalf("defaults.yaml listed as a test: %+v", l)
	}
	c, err := f.Load("cases/l2/t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	pc, _ := f.Prepare(c)
	if pc.Vars["vlan"] != "200" || pc.Vars["site"] != "own" || pc.Steps[0].Timeout != 30 || pc.Steps[0].Retry == nil || pc.Steps[0].Retry.Timeout != 20 {
		t.Fatalf("%+v %+v", pc.Vars, pc.Steps[0])
	}
	os.WriteFile(filepath.Join(f.Root, "testbeds", "tb.yaml"), []byte("tags: [stack, l3]\ndut:\n  session: Lab/a\n  port1: {value: \"1:2\", speed: 10G}\npeer: Lab/b\nlinks:\n  - dut.port1 - peer.port3\n"), 0o644)
	v, err := f.Testbed("tb.yaml")
	if err != nil || v["dut.port1"] != "1:2" || v["dut.port1.speed"] != "10G" || v["testbed.tags"] != "stack,l3" {
		t.Fatalf("%v %v", err, v)
	}
	v["peer.port3"] = "3"
	addLinks(v)
	if v["dut.to.peer"] != "1:2" || v["peer.to.dut"] != "3" || v["dut.links.peer"] != "1:2" {
		t.Fatal(v)
	}
	yes := true
	rc := Case{Requires: map[string]Requirement{"dut": {DeviceType: "EXOS", Stack: &yes, Ports: 2, Tags: []string{"l3"}}}}
	w := map[string]string{"dut": "Lab/a", "dut.device_type": "EXOS", "dut.stack": "true", "dut.port1": "1:2", "dut.port2": "1:3", "testbed.tags": "stack,l3"}
	if why := unmet(rc, w); why != "" {
		t.Fatal(why)
	}
	w["dut.device_type"], w["dut.port2"] = "VOSS", ""
	if why := unmet(rc, w); !strings.Contains(why, "not EXOS") || !strings.Contains(why, "1 test ports") {
		t.Fatal(why)
	}
}
