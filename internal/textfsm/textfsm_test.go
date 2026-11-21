package textfsm

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const vlanBrief = `Value Required VLAN_ID (\d+)
Value NAME (\S+)
Value STATUS (\S+)
Value List INTERFACES ([\w./]+)

Start
  ^VLAN\s+Name -> Vlans

Vlans
  ^\d+ -> Continue.Record
  ^${VLAN_ID}\s+${NAME}\s+${STATUS}\s*$$
  ^${VLAN_ID}\s+${NAME}\s+${STATUS}\s+${INTERFACES},? -> Continue
  ^\d+\s+(?:\S+\s+){3}${INTERFACES},? -> Continue
  ^\d+\s+(?:\S+\s+){4}${INTERFACES},? -> Continue
  ^\s+${INTERFACES},? -> Continue
  ^\s+\S+,\s+${INTERFACES},? -> Continue
  ^-+
`

const out = `VLAN Name                             Status    Ports
---- -------------------------------- --------- -------------------------------
1    default                          active    Gi0/1, Gi0/2
                                                Gi0/3
200  v200                             active
300  lab                              act/lshut Gi0/9
`

func TestParseRun(t *testing.T) {
	tp, err := Parse(vlanBrief)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := tp.Run(out)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"1", "default", "active", "Gi0/1, Gi0/2, Gi0/3"}, {"200", "v200", "active", ""}, {"300", "lab", "act/lshut", "Gi0/9"}}
	if !reflect.DeepEqual(tab.Rows, want) {
		t.Fatalf("%q", tab.Rows)
	}
	if tab.Maps()[1]["NAME"] != "v200" {
		t.Fatal(tab.Maps())
	}
}

func TestFilldownErrorEOF(t *testing.T) {
	tp, err := Parse("Value Filldown SLOT (\\d+)\nValue PORT (\\d+)\n\nStart\n  ^Slot ${SLOT}\n  ^Port ${PORT} -> Record\n  ^BAD -> Error \"bad line\"\n\nEOF\n")
	if err != nil {
		t.Fatal(err)
	}
	tab, _ := tp.Run("Slot 1\nPort 1\nPort 2\nSlot 2\nPort 1\n")
	if len(tab.Rows) != 3 || tab.Rows[2][0] != "2" {
		t.Fatal(tab.Rows)
	}
	if _, err := tp.Run("BAD"); err == nil {
		t.Fatal("no error")
	}
	for _, bad := range []string{"Value X (a)\n\nFoo\n  ^a\n", "Value X (a)\n\nStart\n  ^${Y}\n", "Value X (a)\n\nStart\n  ^a -> Nowhere\n"} {
		if _, err := Parse(bad); err == nil {
			t.Fatal("parsed", bad)
		}
	}
}

func TestIndex(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "index"), []byte("Template, Hostname, Platform, Command\n\ncisco_ios_show_vlan.textfsm, .*, cisco_ios, sh[[ow]] vl[[an]] br[[ief]]\n"), 0o644)
	ix, err := LoadIndex(d)
	if err != nil {
		t.Fatal(err)
	}
	if f := ix.Find("cisco_ios", "sh  vlan brief"); len(f) != 1 || filepath.Base(f[0]) != "cisco_ios_show_vlan.textfsm" {
		t.Fatal(f)
	}
	if f := ix.Find("cisco_ios", "show vlan"); f != nil {
		t.Fatal(f)
	}
	if Platform("EXOS") != "extreme_exos" {
		t.Fatal()
	}
}
