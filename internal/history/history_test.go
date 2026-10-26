package history

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAddDedupeCap(t *testing.T) {
	f := filepath.Join(t.TempDir(), "history.json")
	s := &Store{File: f}
	t0 := time.Now()
	_ = s.Add(Entry{Cmd: "show vlan basic", Session: "Lab/sw1", DeviceType: "VOSS"}, t0)
	_ = s.Add(Entry{Cmd: "show isis", Session: "Lab/sw1"}, t0)
	_ = s.Add(Entry{Cmd: " show vlan basic ", Session: "Lab/sw1", DeviceType: "VOSS"}, t0)
	_ = s.Add(Entry{Cmd: "   "}, t0)
	_ = s.Add(Entry{Cmd: "username admin password Secret1"}, t0)
	_ = s.Add(Entry{Cmd: "snmp-server community public ro"}, t0)
	l := (&Store{File: f}).List() // reloaded from the file
	if len(l) != 2 || l[0].Cmd != "show vlan basic" || l[0].Count != 2 || l[1].Cmd != "show isis" {
		t.Fatalf("%+v", l)
	}
	for i := 0; i < Max+10; i++ {
		s.entries = append(s.entries, Entry{Cmd: "x"})
	}
	_ = s.Add(Entry{Cmd: "last"}, t0)
	if n := len(s.List()); n != Max {
		t.Fatalf("kept %d", n)
	}
	_ = s.Clear()
	if len((&Store{File: f}).List()) != 0 {
		t.Fatal("not cleared")
	}
}
