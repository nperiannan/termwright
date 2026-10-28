package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDefaultUpdateRepo(t *testing.T) {
	s := Settings{}
	if !defaultUpdateRepo(&s) || s.UpdateRepo != PublicRepo {
		t.Fatalf("empty repo: %q", s.UpdateRepo)
	}
	f := Settings{UpdateRepo: "me/fork"}
	if defaultUpdateRepo(&f) || f.UpdateRepo != "me/fork" {
		t.Fatalf("fork changed: %q", f.UpdateRepo)
	}
}

func TestMovePaths(t *testing.T) {
	m := &Manager{dataDir: t.TempDir()}
	s := m.Get()
	s.Favorites = []string{"Lab/sw1", "Core/r1", "Lab/Old/x"}
	s.RecentSessions = []string{"Lab/sw1"}
	s.LastConnected = map[string]string{"Lab/sw1": "t1", "Lab/Old/x": "t2"}
	if err := m.Save(s); err != nil {
		t.Fatal(err)
	}
	m.MovePaths("Lab/sw1", "Lab/sw1-new", false)
	m.MovePaths("Lab/Old", "Archive/Old", true)
	m.MovePaths("Core/r1", "", false)
	g := m.Get()
	if strings.Join(g.Favorites, ",") != "Lab/sw1-new,Archive/Old/x" || g.RecentSessions[0] != "Lab/sw1-new" ||
		g.LastConnected["Lab/sw1-new"] != "t1" || g.LastConnected["Archive/Old/x"] != "t2" || len(g.LastConnected) != 2 {
		t.Fatalf("%+v %+v %+v", g.Favorites, g.RecentSessions, g.LastConnected)
	}
}

func TestChecklistFor(t *testing.T) {
	c := Checklist{Commands: []ChecklistCommands{{DeviceType: "VOSS", Commands: "show isis adjacency\n\nshow vlan basic"}, {DeviceType: "", Commands: "show version"}}}
	if got := strings.Join(c.For("voss"), "|"); got != "show isis adjacency|show vlan basic" {
		t.Fatalf("voss %q", got)
	}
	if got := strings.Join(c.For("EXOS"), "|"); got != "show version" {
		t.Fatalf("other %q", got)
	}
}

// Keyword highlighting turns on once in 0.21: the Network set becomes the
// default and its built-in words also match enable / disable.
func TestKeywordsOnMigration(t *testing.T) {
	dir := t.TempDir()
	old := `{"defaultKeywordSet":"","keywordSets":[{"name":"Network","keywords":[` +
		`{"text":"\\b(warning|warn|disabled|blocked)\\b","regex":true,"color":"#6b5310"},` +
		`{"text":"\\b(up|ok|success|enabled|active|forwarding)\\b","regex":true,"color":"#1f5a2c"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := m.Get()
	if s.DefaultKeywordSet != "Network" || !s.KeywordsOn {
		t.Fatalf("default set %q, on %v", s.DefaultKeywordSet, s.KeywordsOn)
	}
	re := regexp.MustCompile(s.KeywordSets[0].Keywords[1].Text)
	if !re.MatchString("1/5   up   enable") || !regexp.MustCompile(s.KeywordSets[0].Keywords[0].Text).MatchString("disable") {
		t.Fatalf("keywords not updated: %+v", s.KeywordSets[0].Keywords)
	}
	// a user's choice of "None" afterwards stays
	s.DefaultKeywordSet = "None"
	_ = m.Save(s)
	m2, _ := Load(dir)
	if m2.Get().DefaultKeywordSet != "None" {
		t.Fatal("None was not kept")
	}
}
