package testrec

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// Lock says who runs tests on a testbed (locks/<testbed>.lock in the tests folder - shared with the
// team when the folder is). A lock older than LockStale is taken over.
type Lock struct {
	Testbed string    `json:"testbed"`
	User    string    `json:"user"`
	Host    string    `json:"host"`
	PID     int       `json:"pid"`
	Since   time.Time `json:"since"`
	// Mine: held by this Termwright.
	Mine bool `json:"mine,omitempty"`
}

// LockStale is when a lock is old enough to take over.
const LockStale = 12 * time.Hour

func lockName(testbed string) string {
	b := strings.TrimSuffix(filepath.Base(filepath.ToSlash(testbed)), filepath.Ext(testbed))
	return slug(b) + ".lock"
}

func me() (string, string) {
	h, _ := os.Hostname()
	u := os.Getenv("USER")
	if cu, err := user.Current(); err == nil && cu.Username != "" {
		u = cu.Username
	}
	return u, h
}

func (l *Lock) setMine() {
	u, h := me()
	l.Mine = l.User == u && l.Host == h && l.PID == os.Getpid()
}

// LockOf reads a testbed's lock (nil when free or stale).
func (f Folder) LockOf(testbed string) *Lock {
	b, err := os.ReadFile(filepath.Join(f.Root, "locks", lockName(testbed)))
	if err != nil {
		return nil
	}
	var l Lock
	if json.Unmarshal(b, &l) != nil || time.Since(l.Since) > LockStale {
		return nil
	}
	l.setMine()
	return &l
}

// TakeLock locks a testbed for a run; held by someone else it fails unless force.
func (f Folder) TakeLock(testbed string, force bool) error {
	if l := f.LockOf(testbed); l != nil && !l.Mine && !force {
		return fmt.Errorf("%w: %s is in use by %s on %s since %s", ErrLocked, testbed, l.User, l.Host, l.Since.Local().Format("15:04"))
	}
	u, h := me()
	b, _ := json.MarshalIndent(Lock{Testbed: testbed, User: u, Host: h, PID: os.Getpid(), Since: time.Now()}, "", "  ")
	if err := os.MkdirAll(filepath.Join(f.Root, "locks"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(f.Root, "locks", lockName(testbed)), b, 0o644)
}

// ReleaseLock frees a testbed: the lock file goes to .removed/locks (nothing is deleted).
func (f Folder) ReleaseLock(testbed string) error {
	p := filepath.Join(f.Root, "locks", lockName(testbed))
	if _, err := os.Stat(p); err != nil {
		return nil
	}
	d := filepath.Join(f.Root, ".removed", "locks")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	return os.Rename(p, filepath.Join(d, time.Now().Format("2006-01-02_150405.000")+"_"+lockName(testbed)))
}

// Locks lists the testbeds in use.
func (f Folder) Locks() []Lock {
	out := []Lock{}
	es, _ := os.ReadDir(filepath.Join(f.Root, "locks"))
	for _, e := range es {
		if !strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(f.Root, "locks", e.Name()))
		var l Lock
		if err == nil && json.Unmarshal(b, &l) == nil && time.Since(l.Since) <= LockStale {
			l.setMine()
			out = append(out, l)
		}
	}
	return out
}

// ErrLocked: the testbed is in use.
var ErrLocked = fmt.Errorf("testbed in use")
