// Package history remembers the commands typed in terminals and the Command
// Window, for history search and autocomplete.
package history

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Entry is one remembered command.
type Entry struct {
	Cmd        string `json:"cmd"`
	Session    string `json:"session,omitempty"` // session path ("" = unsaved connection)
	Title      string `json:"title,omitempty"`
	DeviceType string `json:"deviceType,omitempty"`
	Time       string `json:"time"`
	Count      int    `json:"count"`
}

// Secret matches commands that may carry a password or key; they are never kept.
var Secret = regexp.MustCompile(`(?i)pass(word|wd|phrase)|secret|communit(y|ies)|key-string|pre-?shared|\bpsk\b|\btoken\b|api[-_]?key`)

// Max is how many commands are kept.
const Max = 5000

// Store is the history file.
type Store struct {
	File    string
	mu      sync.Mutex
	entries []Entry // oldest first
	loaded  bool
}

func (s *Store) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	if b, err := os.ReadFile(s.File); err == nil {
		_ = json.Unmarshal(b, &s.entries)
	}
}

func (s *Store) save() error {
	b, _ := json.Marshal(s.entries)
	tmp := s.File + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.File)
}

// Add remembers a command; the same command on the same session moves to
// the end with its count raised.
func (s *Store) Add(e Entry, now time.Time) error {
	e.Cmd = strings.TrimSpace(e.Cmd)
	if e.Cmd == "" || len(e.Cmd) > 1000 || Secret.MatchString(e.Cmd) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	e.Time, e.Count = now.UTC().Format(time.RFC3339), 1
	for i, x := range s.entries {
		if x.Cmd == e.Cmd && x.Session == e.Session && (e.Session != "" || x.Title == e.Title) {
			e.Count = x.Count + 1
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			break
		}
	}
	s.entries = append(s.entries, e)
	if len(s.entries) > Max {
		s.entries = s.entries[len(s.entries)-Max:]
	}
	return s.save()
}

// List returns the commands, newest first.
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	out := make([]Entry, len(s.entries))
	for i, e := range s.entries {
		out[len(out)-1-i] = e
	}
	return out
}

// Clear forgets every command.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaded, s.entries = true, nil
	return s.save()
}

// Remove forgets the commands for which drop returns true and tells how many
// were removed.
func (s *Store) Remove(drop func(Entry) bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	kept := s.entries[:0:0]
	for _, e := range s.entries {
		if !drop(e) {
			kept = append(kept, e)
		}
	}
	n := len(s.entries) - len(kept)
	if n == 0 {
		return 0, nil
	}
	s.entries = kept
	return n, s.save()
}
