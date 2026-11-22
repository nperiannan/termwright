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
	addFacts(factDrv{&fake{}}, v)
	if v["dut.device_type"] != "kept" || v["x.device_type"] != "VOSS" {
		t.Fatal(v)
	}
}
