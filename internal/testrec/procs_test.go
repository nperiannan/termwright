package testrec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcsDataSample(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	if err := f.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.SaveProc("show-vlan", Proc{Params: []string{"id", "who=user"}, Steps: []Step{
		{Run: &RunStep{Session: "s", Command: "show vlan {{id}}"}, Expect: []Expect{{Contains: "created by {{who}}"}}}}}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(f.Root, "cases", "rows.csv"), []byte("vlan\n42\n\n42\n"), 0o644)
	c := Case{Name: "p", Data: "cases/rows.csv", Steps: []Step{{Call: &CallStep{Proc: "show-vlan", With: map[string]string{"id": "{{vlan}}"}}}}}
	pc, err := f.Prepare(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(pc.Steps) != 1 || pc.Steps[0].Run.Command != "show vlan {{vlan}}" || pc.Steps[0].Expect[0].Contains != "created by user" || len(pc.Params) != 2 || !strings.HasPrefix(pc.Steps[0].Name, "show-vlan › ") {
		t.Fatalf("%+v", pc)
	}
	rs := RunParamsWith(context.Background(), pc, &fake{}, nil, RunOptions{})
	if len(rs) != 2 || !rs[0].Passed {
		t.Fatalf("%+v", rs)
	}
	if _, err := f.Prepare(Case{Steps: []Step{{Call: &CallStep{Proc: "show-vlan"}}}}); err == nil || !strings.Contains(err.Error(), "needs id") {
		t.Fatal("missing param", err)
	}
	// samples: field values over time
	g := 1.0
	l := 100.0
	st := Step{Run: &RunStep{Session: "s", Command: "show version"}, Sample: &Sample{Count: 3},
		Expect: []Expect{{Field: `Uptime: (\d+)`, Agg: "avg", Greater: &g, Less: &l}, {Field: `Uptime: (\d+)`, Agg: "delta", Equals: ptr("0")}, {Contains: "ExtremeXOS"}}}
	rs2 := RunWith(context.Background(), Case{Steps: []Step{st}}, &fake{}, nil, RunOptions{})
	if !rs2.Passed || len(rs2.Steps[0].Checks) != 3 || !strings.Contains(rs2.Steps[0].Name, "3 samples") {
		t.Fatalf("%+v", rs2.Steps[0])
	}
}

func ptr(s string) *string { return &s }

func TestXMLDocAndRPC(t *testing.T) {
	doc := XMLDoc(`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><data><vlans xmlns="x"><vlan><name>v1</name><id>1</id></vlan><vlan><name>v200</name><id>200</id></vlan></vlans></data></rpc-reply>`)
	got, ok := extract("", "data.vlans.vlan[1].id", `<rpc-reply><data><vlans><vlan><name>v1</name><id>1</id></vlan><vlan><name>v200</name><id>200</id></vlan></vlans></data></rpc-reply>`)
	if !ok || got != "200" || doc == nil {
		t.Fatal(got, ok)
	}
	r, err := NetconfRPC(NetconfStep{Op: "edit-config", Target: "candidate", Config: "<x/>"})
	if err != nil || len(r) != 2 || r[1] != "<commit/>" || !strings.Contains(r[0], "<candidate/>") {
		t.Fatal(r, err)
	}
	if _, err := NetconfRPC(NetconfStep{Op: "get-config", Source: "bogus"}); err == nil {
		t.Fatal("bad datastore")
	}
}
