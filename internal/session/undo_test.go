package session

import "testing"

func TestUndoOf(t *testing.T) {
	cases := []struct{ typ, cmd, want string }{
		{"EXOS", "create vlan v42 tag 42", "delete vlan v42"},
		{"EXOS", "configure vlan v42 add ports 1:4 untagged", "configure vlan v42 delete ports 1:4"},
		{"EXOS", "enable ports 3", "disable ports 3"},
		{"EXOS", "show vlan", ""},
		{"Cisco IOS", "vlan 42", "no vlan 42"},
		{"Cisco IOS", "no shutdown", "shutdown"},
		{"Cisco IOS", "interface Gi1/0/4", ""},
		{"Cisco IOS", "show run", ""},
		{"VOSS", "vlan create 42 type port-mstprstp 0", "no vlan create 42 type port-mstprstp 0"},
		{"Huawei VRP", "vlan 42", "undo vlan 42"},
		{"Huawei VRP", "undo shutdown", "shutdown"},
		{"Linux", "touch x", ""},
	}
	for _, c := range cases {
		if got := UndoOf(DeviceType{Name: c.typ}.Undo(), c.cmd); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.typ, c.cmd, got, c.want)
		}
	}
	if got := UndoOf("^set (\\w+) (\\S+) => set $1 old", "set x 5"); got != "set x old" {
		t.Error(got)
	}
	if s, r := (DeviceType{Name: "Cisco NX-OS"}).TestState(); len(s) != 1 || len(r) != 2 {
		t.Error(s, r)
	}
	match := func(typ, out string) bool {
		for _, re := range (DeviceType{Name: typ}).Errors() {
			if re.MatchString(out) {
				return true
			}
		}
		return false
	}
	if !match("EXOS", "ok\nError: VLAN v200 already exists\n") || match("EXOS", "VLAN Interface with name v200 created by user") || !match("Cisco IOS", "% Invalid input detected at '^' marker.") || match("Linux", "Error: x") {
		t.Error("error patterns")
	}
	if len((DeviceType{Name: "EXOS"}).Factory()) != 1 || len((DeviceType{Name: "VOSS"}).Factory()) != 0 {
		t.Error("factory")
	}
}

func TestUndoEXOSPort(t *testing.T) {
	r := builtinUndo["exos"]
	for in, want := range map[string]string{"configure vlan v200 add port 1:2 tagged": "configure vlan v200 delete ports 1:2", "config v200 add ports 1:3": "configure vlan v200 delete ports 1:3", "configure vlan v200 ipaddress 10.10.10.1/24": "unconfigure vlan v200 ipaddress", "sh vlan": ""} {
		if got := UndoOf(r, in); got != want {
			t.Fatalf("%q: %q", in, got)
		}
	}
}
