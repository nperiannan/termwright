package emu

import (
	"strings"
	"testing"
)

func TestPersona(t *testing.T) {
	d := &PersonaDef{Name: "Persona", File: "/data/persona.yaml", Stack: true, Values: []PersonaValue{{Key: "feature", Default: "basic"}},
		Template: "kind: {{if .Stack}}stack{{else}}single{{end}} {{.Values.feature}}\n{{range .Slots}}- {{.Num}} {{.Model}} {{.Serial}}{{if .Master}} master{{end}}{{with .Module}} +{{.}}{{end}}{{range .Ports}} {{.Port}}>{{.VIF}}{{end}}\n{{end}}"}
	out, err := d.Render(VM{Name: "sw1"}, &Persona{Slots: []Slot{{Model: "M-24", Serial: "S1", Module: "X2"}, {Model: "M-48", Serial: "S2", Ports: []PortMap{{10, 1}}}}})
	if err != nil || out != "kind: stack basic\n- 1 M-24 S1 master +X2\n- 2 M-48 S2 10>1\n" {
		t.Fatalf("%q %v", out, err)
	}
	if out, _ := d.Render(VM{}, &Persona{Slots: []Slot{{Model: "M-24"}}, Values: map[string]string{"feature": "full"}}); !strings.HasPrefix(out, "kind: single full") {
		t.Fatal(out)
	}
	if p, err := ParsePorts("10:1, 3:3"); err != nil || len(p) != 2 || p[1] != (PortMap{3, 3}) {
		t.Fatal(p, err)
	}
	if NextSerial("AR022205Q-40036", 2) != "AR022205Q-40038" || NextSerial("12345-00099", 1) != "12345-00100" || NextSerial("ABC", 2) != "ABC-2" {
		t.Fatal(NextSerial("AR022205Q-40036", 2), NextSerial("12345-00099", 1))
	}
	if NextMAC("00:04:96:aa:00:00", 2) != "00:04:96:aa:00:80" {
		t.Fatal(NextMAC("00:04:96:aa:00:00", 2))
	}
	l, _ := testLab()
	sc := l.WriteFileScript(l.VMs[0], "/data/persona.yaml", "a: 1", "cfg", "log")
	if !strings.Contains(sc, "dir=/data") || !strings.Contains(sc, "-v lab=data") || !strings.Contains(sc, `p="$at/persona.yaml"`) || !strings.Contains(sc, "a: 1\nTW_PERSONA_END") || !strings.Contains(sc, "/etc/fstab") || !strings.Contains(sc, "-v has='cfg log'") {
		t.Fatal(sc)
	}
}
