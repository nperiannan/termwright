package config

import "testing"

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
