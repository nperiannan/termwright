package testrec

import (
	"testing"

	"github.com/termwright/termwright/internal/textfsm"
)

func TestMakeTemplate(t *testing.T) {
	out := `Untagged ports auto-move: Inform
------------------------------------------------
Name            VID  Protocol Addr       Flags
------------------------------------------------
Default         1    10.0.0.1/24         -T--
v200            200  ------------------  ----
Mgmt            4095 10.127.34.108 /24   ----
------------------------------------------------
Total number of VLAN(s) : 3`
	src, err := MakeTemplate(TemplateSpec{Kind: "table", Header: "Name            VID  Protocol Addr       Flags",
		Columns: []TemplateColumn{{Name: "Name"}, {Name: "VID"}, {Name: "Protocol Addr", Spaces: true}, {Name: "Flags"}}})
	if err != nil {
		t.Fatal(err)
	}
	tp, err := textfsm.Parse(src)
	if err != nil {
		t.Fatal(err, src)
	}
	tab, err := tp.Run(out)
	if err != nil || len(tab.Rows) != 3 || tab.Maps()[1]["VID"] != "200" || tab.Maps()[2]["PROTOCOL_ADDR"] != "10.127.34.108 /24" {
		t.Fatalf("%v %+v\n%s", err, tab.Maps(), src)
	}
	src, err = MakeTemplate(TemplateSpec{Kind: "fields", Fields: []TemplateField{{Name: "Admin State", Label: "Admin State:"}, {Name: "IP", Label: "Primary IP"}, {Name: "Tagging", Label: "Tagging:"}}})
	tp, _ = textfsm.Parse(src)
	tab, err = tp.Run("VLAN Interface with name v200\n    Admin State:     Enabled      Tagging:   802.1Q Tag 200\n    Primary IP:      10.10.10.1/24\n")
	if err != nil || len(tab.Rows) != 1 || tab.Maps()[0]["ADMIN_STATE"] != "Enabled" || tab.Maps()[0]["IP"] != "10.10.10.1/24" || tab.Maps()[0]["TAGGING"] != "802.1Q Tag 200" {
		t.Fatalf("%v %+v\n%s", err, tab.Maps(), src)
	}
}
