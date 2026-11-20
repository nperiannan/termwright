package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Settings saved before 0.42 get the new built-in device types once; a type the user removed earlier
// (Linux here) is not put back, and one they already made (Huawei VRP) is kept.
func TestMoreTypesAddedOnce(t *testing.T) {
	dir := t.TempDir()
	old := `{"factRulesAdded":true,"hostnameFactAdded":true,"deviceTypes":[{"name":"EXOS"},{"name":"huawei vrp","prompt":"mine"}]}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := m.Get()
	got := map[string]string{}
	for _, d := range s.DeviceTypes {
		got[d.Name] = d.Prompt
	}
	if !s.MoreTypesAdded || len(s.DeviceTypes) != 5 || got["huawei vrp"] != "mine" || got["Eltex MES"] == "" || got["Ruckus ICX"] == "" || got["Cisco NX-OS"] == "" {
		t.Fatalf("%v", got)
	}
	if _, ok := got["Linux"]; ok {
		t.Fatal("Linux put back")
	}
}
