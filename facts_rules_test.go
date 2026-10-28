package main

import (
	"testing"

	"github.com/termwright/termwright/internal/session"
)

const vossInfo = "core-sw1:1#show sys-info\r\n\r\nGeneral Info :\r\n\r\n        SysDescr     : VSP-7400-48Y-8C (8.10.0.0)\r\n" +
	"        SysName      : core-sw1\r\n        SysUpTime    : 41 day(s), 03:12:44\r\n        ModelName    : 7400-48Y-8C\r\n" +
	"        Serial#      : 18JP2B0012\r\n        BaseMacAddr  : 00:04:96:a1:b2:11\r\ncore-sw1:1#"

func TestBuiltinFactRules(t *testing.T) {
	rules := session.BuiltinFacts("VOSS")
	got := ApplyFactRules(rules, []string{"show sys-info"}, []string{vossInfo})
	want := map[string]string{"Model": "7400-48Y-8C", "Version": "8.10.0.0", "Serial": "18JP2B0012", "MAC": "00:04:96:a1:b2:11", "Uptime": "41 day(s), 03:12:44"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	for _, name := range []string{"EXOS", "Cisco IOS", "Linux"} {
		if len(session.BuiltinFacts(name)) == 0 {
			t.Errorf("%s has no fact rules", name)
		}
		ApplyFactRules(session.BuiltinFacts(name), []string{"x"}, []string{""}) // compiles
	}
	exos := "System Type:      X440G2-48p-10G4\r\nSystem MAC:       00:04:96:c3:d4:12\r\nSystem UpTime:    12 days 4 hours 3 minutes\r\nPrimary ver:      32.6.1.5\r\n"
	got = ApplyFactRules(session.BuiltinFacts("EXOS"), []string{"show switch"}, []string{exos})
	if got["Model"] != "X440G2-48p-10G4" || got["Version"] != "32.6.1.5" || got["MAC"] != "00:04:96:c3:d4:12" || got["Uptime"] != "12 days 4 hours 3 minutes" {
		t.Errorf("exos: %v", got)
	}
}
