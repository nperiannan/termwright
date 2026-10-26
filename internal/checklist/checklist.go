// Package checklist stores change-checklist runs: the output of the same show
// commands on the same devices before and after a change, and whether each
// device's output changed.
package checklist

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

	"github.com/termwright/termwright/internal/backup"
)

// Phase is "before" or "after".
type Phase string

const (
	Before Phase = "before"
	After  Phase = "after"
)

// Result is one device in one phase.
type Result struct {
	Time  string `json:"time"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	File  string `json:"file,omitempty"`
	Log   string `json:"log,omitempty"` // device output of a failure
}

// Device is one device of a run.
type Device struct {
	Before  *Result `json:"before,omitempty"`
	After   *Result `json:"after,omitempty"`
	Changed *bool   `json:"changed,omitempty"` // set once both phases have output
}

// Run is one change: a before and an after snapshot.
type Run struct {
	ID      string             `json:"id"`
	Created string             `json:"created"`
	Note    string             `json:"note,omitempty"`
	Devices map[string]*Device `json:"devices"`
}

// Store keeps the runs of all checklists under Dir.
type Store struct {
	Dir string
	mu  sync.Mutex
}

var reBad = regexp.MustCompile(`[\\/:*?"<>|]+`)

func safe(name string) string {
	n := strings.TrimSpace(reBad.ReplaceAllString(name, "-"))
	if n == "" || n == "." || n == ".." {
		n = "checklist"
	}
	return n
}

func (s *Store) runDir(list, id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.HasPrefix(id, ".") {
		return "", errors.New("bad run id")
	}
	return filepath.Join(s.Dir, safe(list), id), nil
}

// NewRun starts a run (its id is the date and time).
func (s *Store) NewRun(list, note string, now time.Time) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := now.Format("2006-01-02_150405")
	dir, _ := s.runDir(list, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	r := &Run{ID: id, Created: now.Format(time.RFC3339), Note: note, Devices: map[string]*Device{}}
	return r, s.save(list, r)
}

func (s *Store) save(list string, r *Run) error {
	dir, err := s.runDir(list, r.ID)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	tmp := filepath.Join(dir, "run.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "run.json"))
}

func (s *Store) load(list, id string) (*Run, error) {
	dir, err := s.runDir(list, id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return nil, err
	}
	var r Run
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if r.Devices == nil {
		r.Devices = map[string]*Device{}
	}
	return &r, nil
}

// Runs lists a checklist's runs, newest first.
func (s *Store) Runs(list string) ([]*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ents, err := os.ReadDir(filepath.Join(s.Dir, safe(list)))
	if err != nil {
		if os.IsNotExist(err) {
			return []*Run{}, nil
		}
		return nil, err
	}
	out := []*Run{}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if r, err := s.load(list, e.Name()); err == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// Record saves a device's output (or failure) for a phase and updates the
// run; it returns the device entry.
func (s *Store) Record(list, id, path string, ph Phase, output string, failure error, log string, now time.Time) (*Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.load(list, id)
	if err != nil {
		return nil, err
	}
	dir, _ := s.runDir(list, id)
	p := filepath.Clean(filepath.FromSlash(path))
	if path == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "..") {
		return nil, errors.New("bad session path")
	}
	res := &Result{Time: now.Format(time.RFC3339), OK: failure == nil}
	if failure != nil {
		res.Error = failure.Error()
		if log != "" {
			lf := filepath.Join(dir, string(ph), p+".error.txt")
			if os.MkdirAll(filepath.Dir(lf), 0o700) == nil && os.WriteFile(lf, []byte(log), 0o600) == nil {
				res.Log = lf
			}
		}
	} else {
		f := filepath.Join(dir, string(ph), p+".txt")
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(f, []byte(output), 0o600); err != nil {
			return nil, err
		}
		res.File = f
	}
	d := r.Devices[path]
	if d == nil {
		d = &Device{}
		r.Devices[path] = d
	}
	if ph == Before {
		d.Before = res
	} else {
		d.After = res
	}
	d.Changed = nil
	if d.Before != nil && d.After != nil && d.Before.OK && d.After.OK {
		a, _ := os.ReadFile(d.Before.File)
		b, _ := os.ReadFile(d.After.File)
		ch := Normalize(string(a)) != Normalize(string(b))
		d.Changed = &ch
	}
	return d, s.save(list, r)
}

var (
	reClock = regexp.MustCompile(`\b\d{1,2}:\d{2}:\d{2}(\.\d+)?\b`)
	reDays  = regexp.MustCompile(`\b\d+ days?,?`)
	reDate  = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
)

// Normalize returns the text used to decide whether an output changed:
// configuration-style volatile lines are dropped and times of day, dates and
// day counters (uptime) are ignored.
func Normalize(s string) string {
	s = backup.Normalize(s)
	s = reClock.ReplaceAllString(s, "T")
	s = reDays.ReplaceAllString(s, "D")
	return reDate.ReplaceAllString(s, "DATE")
}

// Format joins the outputs of the commands, each under a "### command" line.
func Format(cmds, outs []string) string {
	var b strings.Builder
	for i, c := range cmds {
		b.WriteString("### " + c + "\n")
		if i < len(outs) {
			b.WriteString(outs[i])
		}
		if !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}
