package testrec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/termwright/termwright/internal/textfsm"
)

func TestBuiltinTemplates(t *testing.T) {
	f := Folder{Root: t.TempDir()}
	if err := f.Init(); err != nil {
		t.Fatal(err)
	}
	ix, err := textfsm.LoadIndex(f.TemplatesDir())
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][2]string{
		"extreme_exos sh vlan": {`Default         1    --------------------------------T---------------  ANY    2 /4   VR-Default
Mgmt            4095 10.10.10.1     /24  ----------------------------  ANY    1 /1   VR-Mgmt
v200            200  ------------------------------------------------  ANY    0 /0   VR-Default`, "v200|200|0"},
		"extreme_vsp show vlan basic": {`VLAN                                    MSTP
ID    NAME             TYPE             INST_ID  PROTOCOLID   SUBNETADDR      SUBNETMASK      VRFNAME
--------------------------------------------------------------------------------
1     Default          byPort           0        none         N/A             N/A             GlobalRouter
200   v200             byPort           0        none         N/A             N/A             GlobalRouter`, "200|v200|byPort"},
		"cisco_ios show int status": {`Port      Name               Status       Vlan       Duplex  Speed Type
Gi0/1     uplink core        connected    trunk      a-full a-1000 10/100/1000BaseTX
Gi0/2                        notconnect   200          auto   auto 10/100/1000BaseTX`, "Gi0/2||notconnect"},
	}
	for k, c := range cases {
		var plat, cmd string
		for i, ch := range k {
			if ch == ' ' {
				plat, cmd = k[:i], k[i+1:]
				break
			}
		}
		files := ix.Find(plat, cmd)
		if len(files) != 1 {
			t.Fatal(k, files)
		}
		src, _ := os.ReadFile(files[0])
		tp, err := textfsm.Parse(string(src))
		if err != nil {
			t.Fatal(filepath.Base(files[0]), err)
		}
		tab, _ := tp.Run(c[0])
		last := tab.Rows[len(tab.Rows)-1]
		got := last[0] + "|" + last[1] + "|" + last[2]
		if k == "extreme_exos sh vlan" {
			got = last[0] + "|" + last[1] + "|" + last[6]
		}
		if got != c[1] {
			t.Fatalf("%s: %q", k, tab.Rows)
		}
	}
}

func TestTableChecks(t *testing.T) {
	tp, _ := textfsm.Parse("Value ID (\\d+)\nValue STATE (\\S+)\n\nStart\n  ^${ID}\\s+${STATE} -> Record\n")
	tab, _ := tp.Run("1 active\n200 active\n300 suspended\n")
	one, two := 1, 3
	eq := "active"
	v := map[string]string{"vlan": "200"}
	for _, c := range []struct {
		e    TableExpect
		pass bool
	}{
		{TableExpect{Where: map[string]string{"ID": "{{vlan}}"}, Column: "STATE", Equals: &eq}, true},
		{TableExpect{Where: map[string]string{"id": "300"}, Column: "state", Equals: &eq}, false},
		{TableExpect{Where: map[string]string{"STATE": "~^act"}, Rows: &two}, false},
		{TableExpect{Where: map[string]string{"STATE": "suspended"}, Rows: &one}, true},
		{TableExpect{Where: map[string]string{"ID": "999"}}, false},
	} {
		if r := checkTable(c.e, &tab, v); r.Passed != c.pass {
			t.Fatal(c.e.Label(), r)
		}
	}
	if r := checkTable(TableExpect{}, nil, v); r.Passed {
		t.Fatal(r)
	}
}
