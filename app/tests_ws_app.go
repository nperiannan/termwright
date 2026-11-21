package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/testrec"
)

// The Tests view: the tests folder's tree, opening or cloning a folder, reading and saving the YAML,
// the Git state, and opening reports or the folder in an editor.

// TestsTree lists the tests folder.
func (a *App) TestsTree() (testrec.Tree, error) {
	f, err := a.testsFolder()
	if err != nil {
		return testrec.Tree{}, err
	}
	return f.Tree(), nil
}

// TestsSignature changes when files in the tests folder change (the view polls it).
func (a *App) TestsSignature() string {
	f, err := a.testsFolder()
	if err != nil {
		return ""
	}
	return f.Signature()
}

// TestFile reads a text file of the tests folder (a test's YAML, its Go file, a testbed).
func (a *App) TestFile(rel string) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	return f.ReadText(rel)
}

// TestSaveFile writes a testbed, template, suite or library file of the tests folder.
func (a *App) TestSaveFile(rel, text string) error {
	f, err := a.testsFolder()
	if err != nil {
		return err
	}
	return f.SaveText(rel, text)
}

// TestSaveYAML checks and saves a test from its YAML text (and writes its Go test).
func (a *App) TestSaveYAML(rel, text string) (string, error) {
	c, err := testrec.Parse([]byte(text))
	if err != nil {
		return "", fmt.Errorf("not saved: %w", err)
	}
	return a.TestSave(rel, c)
}

// TestsSetFolder makes dir the tests folder (cases/, lib/, testbeds/, reports/ are added).
func (a *App) TestsSetFolder(dir string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	if dir == "" {
		return "", errors.New("no folder")
	}
	s := a.cfg.Get()
	if dir == filepath.Join(a.cfg.DataDir(), "tests") {
		dir = ""
	}
	s.TestsDir = dir
	if err := a.cfg.Save(s); err != nil {
		return "", err
	}
	return a.TestsFolder()
}

// TestsOpenFolder asks for a folder and makes it the tests folder.
func (a *App) TestsOpenFolder() (string, error) {
	cur, _ := a.TestsFolder()
	d, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Open Tests Folder", DefaultDirectory: filepath.Dir(cur), CanCreateDirectories: true})
	if err != nil || d == "" {
		return "", err
	}
	return a.TestsSetFolder(d)
}

// TestsClone clones a Git repository (with the computer's git and its saved logins) into a folder
// chosen by the person and makes it the tests folder.
func (a *App) TestsClone(repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" || strings.HasPrefix(repo, "-") {
		return "", errors.New("give the repository's address, e.g. https://git.example.com/team/tests.git")
	}
	if u, err := url.Parse(repo); err == nil && u.User != nil {
		if _, has := u.User.Password(); has {
			return "", errors.New("the address holds a password: leave it out - git asks for it or uses its saved login")
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return "", errors.New("git is not installed on this computer")
	}
	parent, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Clone Into Which Folder?", CanCreateDirectories: true})
	if err != nil || parent == "" {
		return "", err
	}
	name := strings.TrimSuffix(filepath.Base(strings.TrimRight(repo, "/")), ".git")
	if name == "" || name == "." {
		name = "tests"
	}
	dest := filepath.Join(parent, name)
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("%s is already there - use Open… for it", dest)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	ga, ge := a.gitCreds()
	cmd := exec.CommandContext(ctx, git, append(ga, "clone", "--", repo, dest)...)
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), ge...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone: %s", strings.TrimSpace(string(out)))
	}
	return a.TestsSetFolder(dest)
}

// TestsGitInfo is the tests folder's Git state.
type TestsGitInfo struct {
	// Repo: the folder is a Git repository.
	Repo bool `json:"repo"`
	// Branch checked out.
	Branch string `json:"branch,omitempty"`
	// Changed: files changed or new.
	Changed int `json:"changed"`
	// Upstream: the branch tracks a remote branch.
	Upstream bool `json:"upstream,omitempty"`
	// Ahead: commits not pushed yet.
	Ahead int `json:"ahead,omitempty"`
	// Behind: commits on the remote not pulled yet.
	Behind int `json:"behind,omitempty"`
	// HasGit: the git command is installed.
	HasGit bool `json:"hasGit"`
	// HasGo: the go command is installed (to run the Go tests).
	HasGo bool `json:"hasGo"`
}

// TestsGit returns the tests folder's Git state.
func (a *App) TestsGit() TestsGitInfo {
	var i TestsGitInfo
	_, err := exec.LookPath("go")
	i.HasGo = err == nil
	f, err := a.testsFolder()
	if err != nil {
		return i
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return i
	}
	i.HasGit = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, git, "-C", f.Root, "rev-parse", "--show-toplevel").Output()
	if err != nil || !samePath(strings.TrimSpace(string(out)), f.Root) && !strings.HasPrefix(f.Root, strings.TrimSpace(string(out))) {
		return i
	}
	out, err = exec.CommandContext(ctx, git, "-C", f.Root, "status", "--porcelain", "-b").Output()
	if err != nil {
		return i
	}
	parseGitStatus(string(out), &i)
	return i
}

func samePath(a, b string) bool {
	ea, _ := filepath.EvalSymlinks(a)
	eb, _ := filepath.EvalSymlinks(b)
	return filepath.Clean(ea) == filepath.Clean(eb)
}

// parseGitStatus reads `git status --porcelain -b`: "## main...origin/main [ahead 1, behind 2]" and
// one line per changed file.
func parseGitStatus(out string, i *TestsGitInfo) {
	i.Repo = true
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		b, ok := strings.CutPrefix(l, "## ")
		if !ok {
			if l != "" {
				i.Changed++
			}
			continue
		}
		b = strings.TrimPrefix(b, "No commits yet on ")
		if br, rest, ok := strings.Cut(b, "..."); ok {
			i.Branch, i.Upstream = br, true
			if _, ab, ok := strings.Cut(rest, " ["); ok {
				for _, p := range strings.Split(strings.TrimSuffix(ab, "]"), ", ") {
					var n int
					if _, err := fmt.Sscanf(p, "ahead %d", &n); err == nil {
						i.Ahead = n
					} else if _, err := fmt.Sscanf(p, "behind %d", &n); err == nil {
						i.Behind = n
					}
				}
			}
		} else {
			i.Branch = b
		}
	}
}

// testsGitRun runs git in the tests folder (no prompts: git uses its saved login) and returns its output.
func (a *App) testsGitRun(timeout time.Duration, args ...string) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return "", errors.New("git is not installed on this computer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ga, ge := a.gitCreds()
	cmd := exec.CommandContext(ctx, git, append(append(ga, "-C", f.Root), args...)...)
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), ge...)
	out, err := cmd.CombinedOutput()
	o := strings.TrimSpace(string(out))
	if ctx.Err() != nil {
		return o, fmt.Errorf("git %s: no answer in %s", args[0], timeout)
	}
	if err != nil {
		if o == "" {
			o = err.Error()
		}
		return o, fmt.Errorf("git %s: %s", args[0], o)
	}
	return o, nil
}

// TestsGitInit makes the tests folder a Git repository (reports/ is left out by .gitignore).
func (a *App) TestsGitInit() (string, error) {
	return a.testsGitRun(30*time.Second, "init")
}

// TestsGitPull brings the remote's commits (fast-forward only: it never merges or overwrites).
func (a *App) TestsGitPull() (string, error) {
	return a.testsGitRun(5*time.Minute, "pull", "--ff-only")
}

// TestsGitCommit commits the tests, testbeds and library (not the reports) with the person's own
// git name and e-mail.
func (a *App) TestsGitCommit(message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", errors.New("give a commit message")
	}
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	var paths []string
	for _, p := range []string{"cases", "testbeds", "lib", "templates", "suites", "go.mod", ".gitignore", "README.md"} {
		if _, err := os.Stat(filepath.Join(f.Root, p)); err == nil {
			paths = append(paths, p)
		}
	}
	if out, err := a.testsGitRun(time.Minute, append([]string{"add", "-A", "--"}, paths...)...); err != nil {
		return out, err
	}
	return a.testsGitRun(time.Minute, "commit", "-m", message)
}

// TestsGitPush pushes the branch (setting its upstream on origin the first time); never forced.
func (a *App) TestsGitPush() (string, error) {
	i := a.TestsGit()
	if !i.Repo {
		return "", errors.New("the tests folder is not a Git repository")
	}
	if i.Upstream {
		return a.testsGitRun(5*time.Minute, "push")
	}
	return a.testsGitRun(5*time.Minute, "push", "-u", "origin", "HEAD")
}

// TestsOpen opens a file or folder of the tests folder with the system ("" = the folder; a report
// folder opens its report.html).
func (a *App) TestsOpen(rel string) error {
	f, err := a.testsFolder()
	if err != nil {
		return err
	}
	p := f.Root
	if rel != "" {
		if p, err = f.Abs(rel); err != nil {
			return err
		}
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			if _, err := os.Stat(filepath.Join(p, "report.html")); err == nil {
				p = filepath.Join(p, "report.html")
			}
		}
	}
	return openWithSystem(p)
}

// TestsOpenInEditor opens the tests folder in VS Code (the code command), else in the file manager.
func (a *App) TestsOpenInEditor(rel string) error {
	f, err := a.testsFolder()
	if err != nil {
		return err
	}
	args := []string{f.Root}
	if rel != "" {
		p, err := f.Abs(rel)
		if err != nil {
			return err
		}
		args = append(args, p)
	}
	if code, err := exec.LookPath("code"); err == nil {
		cmd := exec.Command(code, args...)
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
			return nil
		}
	}
	return openWithSystem(f.Root)
}

// TestsNewTestbed writes testbeds/<name>.yaml with a role for each saved session in a session folder
// (the emulated lab's folder, a site): core-sw1 → {{core_sw1}}. It returns the file.
func (a *App) TestsNewTestbed(name, folder string) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	folder = strings.Trim(strings.TrimSpace(folder), "/")
	if folder == "" {
		return "", errors.New("give a session folder, e.g. Lab")
	}
	paths, err := a.store.AllSessions([]string{folder})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no saved sessions in %s", folder)
	}
	return f.WriteTestbed(name, paths, "the sessions of "+folder)
}

// TestRemove moves a test and its generated code to .removed/ in the tests folder (nothing is deleted).
func (a *App) TestRemove(file string) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	return f.Remove(file)
}

// TestRename moves a test to another file (copy: Duplicate keeps the original); it returns the new file.
func (a *App) TestRename(file, to string, copy bool) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	return f.Rename(file, to, copy)
}

// TestSaveSuite writes a suite (suites/<name>.yaml) and returns its file.
func (a *App) TestSaveSuite(rel string, s testrec.Suite) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	return f.SaveSuite(rel, s)
}

// TestHistory: the last runs of a test (file "": every test) with pass rate, flips and versions.
func (a *App) TestHistory(file string, limit int) ([]testrec.TestHistory, error) {
	f, err := a.testsFolder()
	if err != nil {
		return nil, err
	}
	return f.History(file, limit), nil
}
