package term

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/termwright/termwright/internal/session"
)

func TestLogPathFor(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(&recEmitter{}, t.TempDir(), func() string { return dir })
	s := session.Defaults()
	s.Path, s.Hostname = "Lab/Stack", "10.1.1.1"
	ti := m.Create(s)
	p, err := m.LogPathFor(ti.ID)
	if err != nil || filepath.Dir(p) != dir || !regexp.MustCompile(`-\d{8}-\d{6}\.log$`).MatchString(p) {
		t.Fatalf("%q %v", p, err)
	}
	m.LogName = func() string { return "%H-%Y%M%D.txt" }
	if p, _ := m.LogPathFor(ti.ID); !regexp.MustCompile(`10\.1\.1\.1-\d{8}\.txt$`).MatchString(p) {
		t.Fatalf("pattern: %q", p)
	}
}
