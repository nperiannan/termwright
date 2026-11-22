package testrec

import (
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
