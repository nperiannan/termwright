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
	os.WriteFile(filepath.Join(d, "cases", "vlan", "api.yaml"), []byte("name: REST VLANs\nsteps:\n  - http: {session: APIs/rest, url: /vlans}\n    expect: [{status: 200}, {json: 'data.vlans[0].name', equals: v1000}]\n"), 0o644)
	os.WriteFile(filepath.Join(d, "cases", "vlan", "web.yaml"), []byte("name: Web login\nsteps:\n  - web:\n      url: http://127.0.0.1:8088/\n      actions:\n        - {do: fill, target: '#u', value: admin}\n        - {do: click, target: 'text=Sign in'}\n        - {do: expect, value: Welcome admin}\n    expect: [{contains: Welcome admin}]\n"), 0o644)
	os.MkdirAll(filepath.Join(d, "procs"), 0o755)
	os.WriteFile(filepath.Join(d, "procs", "create-vlan.yaml"), []byte(`name: create-vlan
description: Makes a VLAN
params: [name, tag=1]
steps:
  - run: {session: '{{sw}}', command: 'create vlan {{name}} tag {{tag}}'}
`), 0o644)
	for _, c := range []string{"cases/vlan/tc-1.yaml", "cases/vlan/api.yaml", "cases/vlan/web.yaml"} {
		if _, err := f.WriteGo(c); err != nil {
			t.Fatal(err)
		}
	}
	dev := func(s string) (InventoryDevice, bool) {
		if s == "APIs/rest" {
			return InventoryDevice{Session: s, Protocol: "api", BaseURL: "http://127.0.0.1:8088/rest", Auth: "basic", Username: "api"}, true
		}
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
	gb, gname, err := f.Export(ExportOptions{Lang: "go", OS: "linux", Standalone: true, Device: dev})
	if err != nil {
		t.Fatal(err)
	}
	gz, _ := zip.NewReader(bytes.NewReader(gb), int64(len(gb)))
	gfiles := map[string]bool{}
	for _, zf := range gz.File {
		gfiles[strings.SplitN(zf.Name, "/", 2)[1]] = true
	}
	for _, want := range []string{"lib/standalone/execute.go", "lib/snmp/mibs/IF-MIB", "lib/webrun/runner.js", "lib/twtest/local.go", "lib/twprocs/twprocs.go", "go.mod", "go.sum"} {
		if !gfiles[want] {
			t.Fatalf("no %s in %s", want, gname)
		}
	}
	if out := os.Getenv("TW_EXPORT_GO"); out != "" {
		os.WriteFile(out, gb, 0o644)
	}
	rb, rname, err := f.Export(ExportOptions{Lang: "runner", OS: "linux", Device: dev, Runners: map[string][]byte{"twrun-linux-amd64": []byte("x")}, Version: "0.44.0"})
	if err != nil {
		t.Fatal(err)
	}
	rz, _ := zip.NewReader(bytes.NewReader(rb), int64(len(rb)))
	rf := map[string]string{}
	for _, zf := range rz.File {
		r, _ := zf.Open()
		var buf bytes.Buffer
		buf.ReadFrom(r)
		rf[strings.SplitN(zf.Name, "/", 2)[1]] = buf.String()
	}
	if rf["bin/twrun-linux-amd64"] == "" || rf["cases/vlan/tc-1.yaml"] == "" || rf["procs/create-vlan.yaml"] == "" || rf["inventory.yaml"] == "" ||
		!strings.Contains(rf["run.sh"], "releases/download/v0.44.0/twrun-linux-$ARCH") || !strings.Contains(rf["plan.txt"], "cases/vlan/tc-1.yaml") || rf["lib/twtest/twtest.go"] != "" {
		t.Fatalf("%s: %v\n%s", rname, len(rf), rf["run.sh"])
	}
	if out := os.Getenv("TW_EXPORT_RUNNER"); out != "" {
		os.WriteFile(out, rb, 0o644)
	}
	if _, _, err := f.Export(ExportOptions{Lang: "tcl", OS: "linux", Standalone: true}); err == nil {
		t.Fatal("tcl standalone allowed")
	}
}

// The standalone Go suite builds with the same module versions as Termwright (go.sum.txt has their sums).
func TestStandaloneGoModVersions(t *testing.T) {
	b, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Skip(err)
	}
	sum := string(golibFile("go.sum.txt"))
	for mod, v := range map[string]string{"golang.org/x/crypto": xCryptoVersion, "golang.org/x/sys": xSysVersion, "golang.org/x/term": xTermVersion, "gopkg.in/yaml.v3": "v3.0.1"} {
		if !strings.Contains(string(b), mod+" "+v) {
			t.Errorf("go.mod has another %s than %s: update export.go and golib/go.sum.txt", mod, v)
		}
		if !strings.Contains(sum, mod+" "+v+" h1:") {
			t.Errorf("golib/go.sum.txt has no %s %s", mod, v)
		}
	}
}
