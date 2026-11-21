package testrec

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportStandalone(t *testing.T) {
	d := t.TempDir()
	f := Folder{Root: d}
	os.MkdirAll(filepath.Join(d, "cases", "vlan"), 0o755)
	os.WriteFile(filepath.Join(d, "cases", "vlan", "tc-1.yaml"), []byte(`name: TC-1
vars:
  sw: Lab/EXOS/sw1
steps:
  - run: {session: '{{sw}}', command: show vlan}
    expect: [{contains: 'Total number of VLAN(s) : 2'}]
  - call: {proc: create-vlan, with: {name: v1000, tag: "1000"}}
  - run: {session: '{{sw}}', command: show vlan v1000}
    expect: [{field: 'Primary\s+IP:\s*(\S+)', equals: 10.1.1.1/24}, {contains: 802.1Q Tag 1000}]
cleanup:
  - run: {session: '{{sw}}', command: delete vlan v1000}
`), 0o644)
	os.MkdirAll(filepath.Join(d, "procs"), 0o755)
	os.WriteFile(filepath.Join(d, "procs", "create-vlan.yaml"), []byte(`name: create-vlan
description: Makes a VLAN
params: [name, tag=1]
steps:
  - run: {session: '{{sw}}', command: 'create vlan {{name}} tag {{tag}}'}
`), 0o644)
	if _, err := f.WriteGo("cases/vlan/tc-1.yaml"); err != nil {
		t.Fatal(err)
	}
	dev := func(s string) (InventoryDevice, bool) {
		if s != "Lab/EXOS/sw1" {
			return InventoryDevice{}, false
		}
		return InventoryDevice{Session: s, Host: "127.0.0.1", Protocol: "ssh", Port: 2222, Username: "admin", DeviceType: "EXOS"}, true
	}
	b, name, err := f.Export(ExportOptions{Lang: "python", OS: "linux", Standalone: true, Device: dev})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(name, "standalone") {
		t.Fatal(name)
	}
	z, _ := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	got := map[string]string{}
	for _, zf := range z.File {
		r, _ := zf.Open()
		var buf bytes.Buffer
		buf.ReadFrom(r)
		got[strings.SplitN(zf.Name, "/", 2)[1]] = buf.String()
	}
	if !strings.Contains(got["lib/python/twprocs.py"], "def create_vlan(tw, name, tag=\"1\"") || !strings.Contains(got["cases/vlan/test_vlan_tc_1.py"], "twprocs.create_vlan(tw, name=tw.fill(\"v1000\"), tag=tw.fill(\"1000\"))") {
		t.Fatal(got["lib/python/twprocs.py"], got["cases/vlan/test_vlan_tc_1.py"])
	}
	for _, want := range []string{"inventory.yaml", "requirements.txt", "lib/python/twlocal.py", "lib/python/twtest.py", "run.sh", "cases/vlan/tc-1.yaml"} {
		if got[want] == "" {
			t.Fatalf("no %s in %v", want, name)
		}
	}
	if !strings.Contains(got["inventory.yaml"], "host: 127.0.0.1") || strings.Contains(strings.ToLower(got["inventory.yaml"]), "password:") {
		t.Fatal(got["inventory.yaml"])
	}
	if !strings.Contains(got["run.sh"], "TW_ENGINE=local") {
		t.Fatal(got["run.sh"])
	}
	if out := os.Getenv("TW_EXPORT_OUT"); out != "" {
		os.WriteFile(out, b, 0o644)
	}
	if _, _, err := f.Export(ExportOptions{Lang: "tcl", OS: "linux", Standalone: true}); err == nil {
		t.Fatal("tcl standalone allowed")
	}
}
