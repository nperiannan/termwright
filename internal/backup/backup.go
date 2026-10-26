// Package backup keeps copies of device configurations: one folder per
// session, one text file per copy, a status file with the last result of
// every device and job.
package backup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status is the last backup result of one session.
type Status struct {
	Time    string `json:"time"` // RFC 3339
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Changed bool   `json:"changed"` // differs from the previous copy
	File    string `json:"file,omitempty"`
	Copies  int    `json:"copies"`
}

// File is one saved copy.
type File struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Time string `json:"time"`
	Size int64  `json:"size"`
}

type statusFile struct {
	Devices map[string]Status `json:"devices"`
	Jobs    map[string]string `json:"jobs"` // job name → last run (RFC 3339)
}

// Store is a backup folder.
type Store struct {
	Dir string
	mu  sync.Mutex
}

const stamp = "2006-01-02_150405"

func (s *Store) folder(path string) (string, error) {
	p := filepath.Clean(filepath.FromSlash(path))
	if path == "" || filepath.IsAbs(p) || p == "." || strings.HasPrefix(p, "..") {
		return "", errors.New("bad session path")
	}
	return filepath.Join(s.Dir, p), nil
}

// Save writes a copy of a session's configuration unless onlyChanged is set
// and it equals the newest copy (volatile lines such as timestamps ignored).
// It keeps the newest keep copies (0 = 30).
func (s *Store) Save(path, content string, onlyChanged bool, keep int, now time.Time) (file string, changed bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.folder(path)
	if err != nil {
		return "", false, err
	}
	files, _ := s.files(dir)
	changed = true
	if len(files) > 0 {
		if prev, err := os.ReadFile(files[0].Path); err == nil {
			changed = Normalize(string(prev)) != Normalize(content)
		}
	}
	if !changed && onlyChanged {
		return files[0].Path, false, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, err
	}
	file = filepath.Join(dir, now.Format(stamp)+".txt")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		return "", false, err
	}
	if keep <= 0 {
		keep = 30
	}
	if files, err := s.files(dir); err == nil && len(files) > keep {
		for _, f := range files[keep:] {
			_ = os.Remove(f.Path)
		}
	}
	return file, changed, nil
}

// Files lists a session's copies, newest first.
func (s *Store) Files(path string) ([]File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.folder(path)
	if err != nil {
		return nil, err
	}
	return s.files(dir)
}

var reStamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_\d{6}\.txt$`)

func (s *Store) files(dir string) ([]File, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []File{}, nil
		}
		return nil, err
	}
	out := []File{}
	for _, e := range ents {
		if e.IsDir() || !reStamp.MatchString(e.Name()) {
			continue
		}
		t, _ := time.ParseInLocation(stamp, strings.TrimSuffix(e.Name(), ".txt"), time.Local)
		var size int64
		if fi, err := e.Info(); err == nil {
			size = fi.Size()
		}
		out = append(out, File{Name: e.Name(), Path: filepath.Join(dir, e.Name()), Time: t.Format(time.RFC3339), Size: size})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

func (s *Store) loadStatus() statusFile {
	st := statusFile{Devices: map[string]Status{}, Jobs: map[string]string{}}
	if b, err := os.ReadFile(filepath.Join(s.Dir, "backup-status.json")); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if st.Devices == nil {
		st.Devices = map[string]Status{}
	}
	if st.Jobs == nil {
		st.Jobs = map[string]string{}
	}
	return st
}

func (s *Store) saveStatus(st statusFile) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	tmp := filepath.Join(s.Dir, "backup-status.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Dir, "backup-status.json"))
}

// Status returns the last result of every session.
func (s *Store) Status() map[string]Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadStatus().Devices
}

// SetStatus records a session's result.
func (s *Store) SetStatus(path string, st Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.loadStatus()
	all.Devices[path] = st
	return s.saveStatus(all)
}

// JobRun returns when a job last ran (zero if never).
func (s *Store) JobRun(name string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, _ := time.Parse(time.RFC3339, s.loadStatus().Jobs[name])
	return t
}

// SetJobRun records when a job ran.
func (s *Store) SetJobRun(name string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.loadStatus()
	all.Jobs[name] = t.Format(time.RFC3339)
	return s.saveStatus(all)
}

// Lines that change on every run without a configuration change: comment
// lines with a time of day, counters some devices print in the header.
var volatile = []*regexp.Regexp{
	regexp.MustCompile(`^\s*[#!;].*\b\d{1,2}:\d{2}:\d{2}\b`),
	regexp.MustCompile(`(?i)^\s*(! )?(last configuration change|nvram config last updated|current configuration\s*:|building configuration)`),
	regexp.MustCompile(`(?i)^\s*ntp clock-period\b`),
	regexp.MustCompile(`(?i)^\s*(uptime|system up ?time)\b`),
}

// Normalize returns the text used to decide whether a configuration changed.
func Normalize(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := lines[:0]
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		skip := l == ""
		for _, re := range volatile {
			skip = skip || re.MatchString(l)
		}
		if !skip {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// Due reports whether a scheduled job should run at now: schedule "daily" or
// "weekdays" at "HH:MM", and it has not run since that time today.
func Due(schedule, at string, last, now time.Time) bool {
	if schedule != "daily" && schedule != "weekdays" {
		return false
	}
	if schedule == "weekdays" && (now.Weekday() == time.Saturday || now.Weekday() == time.Sunday) {
		return false
	}
	hm, err := time.ParseInLocation("15:04", at, now.Location())
	if err != nil {
		return false
	}
	slot := time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, now.Location())
	return !now.Before(slot) && last.Before(slot)
}
