package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnboardedOnlyForUpgrades(t *testing.T) {
	fresh := t.TempDir()
	m, err := Load(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if m.Get().Onboarded {
		t.Fatal("a new install should show the welcome flow")
	}
	// a second start before the flow was finished still shows it
	if m, _ = Load(fresh); m.Get().Onboarded {
		t.Fatal("welcome flow skipped on the second start")
	}
	old := t.TempDir()
	if err := os.WriteFile(filepath.Join(old, "settings.json"), []byte(`{"fontSize": 15}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if m, _ = Load(old); !m.Get().Onboarded {
		t.Fatal("an upgrade should not show the welcome flow")
	}
}
