package topo

import (
	"os"
	"reflect"
	"testing"
)

func TestVLANParsers(t *testing.T) {
	b, _ := os.ReadFile("testdata/voss/vlan_members.txt")
	m := ParseVOSSVLANMembers(string(b))
	if !reflect.DeepEqual(m["1/49"], []int{200}) || !reflect.DeepEqual(m["1/50"], []int{200, 4048}) || !reflect.DeepEqual(m["1/24"], []int{1}) || !reflect.DeepEqual(m["1/1"], []int{1, 200}) {
		t.Fatalf("voss members: %v", m)
	}
	b, _ = os.ReadFile("testdata/voss/vlan_basic.txt")
	n := ParseVOSSVLANBasic(string(b))
	if n[200] != "Users" || n[4048] != "onboarding-vlan" || n[1] != "Default" {
		t.Fatalf("voss names: %v", n)
	}
	b, _ = os.ReadFile("testdata/exos/vlan_detail.txt")
	p, nm := ParseEXOSVLANDetail(string(b))
	if !reflect.DeepEqual(p["4"], []int{1}) || !reflect.DeepEqual(p["49"], []int{200}) || !reflect.DeepEqual(p["1:5"], []int{200}) || nm[200] != "Users" {
		t.Fatalf("exos: %v %v", p, nm)
	}
	if got := vlansOf(&Port{VLANs: []int{1, 200}}, &Port{VLANs: []int{200, 300}}); !reflect.DeepEqual(got, []int{200}) {
		t.Fatalf("vlansOf: %v", got)
	}
	if got := expandPortList("1/1-1/3,1/9"); !reflect.DeepEqual(got, []string{"1/1", "1/2", "1/3", "1/9"}) {
		t.Fatalf("expand: %v", got)
	}
}
