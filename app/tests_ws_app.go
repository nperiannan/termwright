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
	cmd := exec.CommandContext(ctx, git, "clone", "--", repo, dest)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, git, "-C", f.Root, "status", "--porcelain", "-b").Output()
	if err != nil {
		return i
	}
	i.Repo = true
	for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if b, ok := strings.CutPrefix(l, "## "); ok {
			b, _, _ = strings.Cut(b, "...")
			i.Branch = strings.TrimPrefix(b, "No commits yet on ")
		} else if l != "" {
			i.Changed++
		}
	}
	return i
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
