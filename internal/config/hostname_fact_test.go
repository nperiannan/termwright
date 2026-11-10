package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Settings saved before 0.34 get the Hostname fact once; a type that has one keeps its own.
func TestHostnameFactAddedOnce(t *testing.T) {
	dir := t.TempDir()
	old := `{"factRulesAdded":true,"deviceTypes":[{"name":"EXOS","facts":[{"name":"Model","command":"show switch","regex":"System Type:\\s*(.+?)\\s*$"}]},{"name":"VOSS","facts":[{"name":"Hostname","command":"show sys-info","regex":"mine"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := m.Get()
	if !s.HostnameFactAdded {
		t.Fatal("not marked")
	}
	for _, d := range s.DeviceTypes {
		switch d.Name {
		case "EXOS":
			if len(d.Facts) != 2 || d.Facts[0].Name != "Hostname" || d.Facts[1].Name != "Model" {
				t.Fatalf("EXOS facts %+v", d.Facts)
			}
		case "VOSS":
			if len(d.Facts) != 1 || d.Facts[0].Regex != "mine" {
				t.Fatalf("VOSS kept its own %+v", d.Facts)
			}
		}
	}
}
