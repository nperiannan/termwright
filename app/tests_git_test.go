package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
)

func TestParseGitStatus(t *testing.T) {
	var i TestsGitInfo
	parseGitStatus("## main...origin/main [ahead 1, behind 2]\n M cases/a.yaml\n?? cases/b.yaml\n", &i)
	if !i.Repo || i.Branch != "main" || !i.Upstream || i.Ahead != 1 || i.Behind != 2 || i.Changed != 2 {
		t.Fatalf("%+v", i)
	}
	i = TestsGitInfo{}
	parseGitStatus("## No commits yet on main\n", &i)
	if i.Branch != "main" || i.Upstream || i.Changed != 0 {
		t.Fatalf("%+v", i)
	}
}

// Init, commit (reports left out) and the state, with a real git.
func TestTestsGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	st, _ := session.NewStore(dir + "/Sessions")
	a := &App{cfg: cfg, store: st}
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	if i := a.TestsGit(); i.Repo || !i.HasGit {
		t.Fatalf("%+v", i)
	}
	if _, err := a.TestsGitInit(); err != nil {
		t.Fatal(err)
	}
	f, _ := a.testsFolder()
	os.WriteFile(filepath.Join(f.Root, "cases", "a.yaml"), []byte("steps: []\n"), 0o644)
	os.MkdirAll(filepath.Join(f.Root, "reports", "r1"), 0o755)
	os.WriteFile(filepath.Join(f.Root, "reports", "r1", "x.html"), []byte("x"), 0o644)
	if i := a.TestsGit(); !i.Repo || i.Changed == 0 {
		t.Fatalf("%+v", i)
	}
	if _, err := a.TestsGitCommit(""); err == nil {
		t.Fatal("empty message")
	}
	if out, err := a.TestsGitCommit("first tests"); err != nil {
		t.Fatal(out, err)
	}
	if i := a.TestsGit(); i.Changed != 0 {
		t.Fatalf("left after commit: %+v", i)
	}
	out, _ := a.testsGitRun(5e9, "ls-files")
	if strings.Contains(out, "reports/") || !strings.Contains(out, "cases/a.yaml") {
		t.Fatal(out)
	}
	if _, err := a.TestsGitPush(); err == nil {
		t.Fatal("pushed without a remote")
	}
}
