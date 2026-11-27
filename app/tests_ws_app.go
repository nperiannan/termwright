package app

import (
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"

	"context"
	"errors"
	"fmt"
	"github.com/termwright/termwright/internal/textfsm"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/testrec"
	"github.com/termwright/termwright/internal/webrun"
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
	if err := mayEdit(f); err != nil {
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
	for _, p := range []string{"cases", "testbeds", "lib", "templates", "suites", "procs", "data", "roles.yaml", "approvals", "go.mod", ".gitignore", "README.md"} {
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
func (a *App) TestsNewTestbed(name, folder string, sessions []string) (string, error) {
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
	if len(sessions) > 0 { // only the devices picked
		in := map[string]bool{}
		for _, p := range paths {
			in[p] = true
		}
		var picked []string
		for _, s := range sessions {
			if !in[s] {
				return "", fmt.Errorf("%s is not a session of %s", s, folder)
			}
			picked = append(picked, s)
		}
		paths = picked
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no saved sessions in %s", folder)
	}
	return f.WriteTestbed(name, paths, "devices of "+folder)
}

// TestbedDevice is one value of a testbed: a role and its saved session (with how to reach it), or another value.
type TestbedDevice struct {
	Role       string `json:"role"`
	Session    string `json:"session,omitempty"`
	Folder     string `json:"folder,omitempty"`
	Name       string `json:"name,omitempty"`
	Host       string `json:"host,omitempty"`
	DeviceType string `json:"deviceType,omitempty"`
	Model      string `json:"model,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	// Value: a value that is not a saved session (an address, a VLAN number…).
	Value string `json:"value,omitempty"`
	// Values: the role's own values ({{role.port}}); Wanted: the ones the tests use that it does not set.
	Values map[string]string `json:"values,omitempty"`
	Wanted []string          `json:"wanted,omitempty"`
	// Ports: port1…port8 - the testbed's when it sets them, else the device type's (Set false).
	Ports []TestbedPort `json:"ports,omitempty"`
}

// TestbedPort is one of a device's port variables.
type TestbedPort struct {
	Name  string            `json:"name"`
	Value string            `json:"value"`
	Set   bool              `json:"set,omitempty"`
	Props map[string]string `json:"props,omitempty"` // speed, to …
}

// portsOf: a device's port1…portN (N: role.port_count, else 8), the role's own values first, else the defaults of its device type.
func (a *App) portsOf(path, deviceType string, values map[string]string) []TestbedPort {
	stack := values["stack"] == "true" || values["slot"] != "" || a.isStack(path)
	count, _ := strconv.Atoi(values["port_count"])
	if count <= 0 {
		count = testrec.PortCount
	}
	def := testrec.DefaultPorts(deviceType, stack, values["slot"], count, a.slotsOf(path))
	var out []TestbedPort
	for i := 1; i <= count; i++ {
		n := "port" + strconv.Itoa(i)
		p := TestbedPort{Name: n}
		if v := values[n]; v != "" {
			p.Value, p.Set = v, true
		} else if i <= len(def) {
			p.Value = def[i-1]
		}
		out = append(out, p)
	}
	return out
}

// TestbedDevices lists a testbed's roles with their sessions (folder, name, address, device type, model).
func (a *App) TestbedDevices(file string) ([]TestbedDevice, error) {
	f, err := a.testsFolder()
	if err != nil {
		return nil, err
	}
	m, err := f.Testbed(file)
	if err != nil {
		return nil, err
	}
	facts := a.cfg.Get().DeviceFacts
	used := testsRoleValues(f)
	out := []TestbedDevice{}
	for role, v := range m {
		if r, _, ok := strings.Cut(role, "."); ok && (m[r] != "" || r == "links" || r == "testbed") {
			continue // a role's own value (listed with the role), a link or the testbed's tags
		}
		d := TestbedDevice{Role: role}
		if s, err := a.store.Get(v); err == nil && v != "" {
			d.Session, d.Folder, d.Name, d.Host, d.DeviceType, d.Protocol = v, s.Folder, s.Name, s.Hostname, s.DeviceType, s.Protocol
			if s.Protocol == session.ProtoWeb {
				d.Host = s.WebURL
			}
			if st := facts[v]; st != nil {
				d.Model = st["Model"]
			}
			for k, x := range m {
				if r, key, ok := strings.Cut(k, "."); ok && r == role && key != "session" && !strings.Contains(key, ".") {
					if d.Values == nil {
						d.Values = map[string]string{}
					}
					d.Values[key] = x
				}
			}
		} else {
			d.Value = v
		}
		if d.Session != "" {
			d.Ports = a.portsOf(d.Session, d.DeviceType, d.Values)
			for i := range d.Ports { // a port's own properties: role.port1.speed …
				for k, x := range m {
					if p, ok := strings.CutPrefix(k, role+"."+d.Ports[i].Name+"."); ok && p != "value" {
						if d.Ports[i].Props == nil {
							d.Ports[i].Props = map[string]string{}
						}
						d.Ports[i].Props[p] = x
					}
				}
			}
			for k := range d.Values {
				if portVar.MatchString(k) || k == "port_count" || k == "ports" {
					delete(d.Values, k)
				}
			}
		}
		for _, k := range used {
			if r, key, _ := strings.Cut(k, "."); d.Session != "" && r == role && d.Values[key] == "" && !portVar.MatchString(key) && !slices.Contains(d.Wanted, key) {
				d.Wanted = append(d.Wanted, key)
			}
		}
		sort.Strings(d.Wanted)
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder < out[j].Folder
		}
		return out[i].Name+out[i].Role < out[j].Name+out[j].Role
	})
	return out, nil
}

// TestbedChange adds saved sessions to a testbed (a role each) and takes roles out.
func (a *App) TestbedChange(file string, add, removeRoles []string) error {
	f, err := a.testsFolder()
	if err != nil {
		return err
	}
	if err := mayEdit(f); err != nil {
		return err
	}
	for _, s := range add {
		if _, err := a.store.Get(s); err != nil {
			return fmt.Errorf("no saved session %s", s)
		}
	}
	return f.ChangeTestbed(file, add, removeRoles)
}

// TestsDeviceModels: the models known for sessions (from their device facts).
func (a *App) TestsDeviceModels(paths []string) map[string]string {
	out := map[string]string{}
	facts := a.cfg.Get().DeviceFacts
	for _, p := range paths {
		if st := facts[p]; st != nil && st["Model"] != "" {
			out[p] = st["Model"]
		}
	}
	return out
}

// TestRemove moves a test and its generated code to .removed/ in the tests folder (nothing is deleted).
func (a *App) TestRemove(file string) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	if err := mayEdit(f); err != nil {
		return "", err
	}
	f.Audit("delete", file)
	return f.Remove(file)
}

// TestRename moves a test to another file (copy: Duplicate keeps the original); it returns the new file.
func (a *App) TestRename(file, to string, copy bool) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	if err := mayEdit(f); err != nil {
		return "", err
	}
	f.Audit(map[bool]string{true: "duplicate", false: "rename"}[copy], file+" → "+to)
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

// TestsExport packs a standalone suite (tests, generated code, library, testbeds, plan, run script for the
// OS) as a zip the person saves; it returns the file.
func (a *App) TestsExport(o testrec.ExportOptions) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	o.Device = a.inventoryDevice
	if o.Lang == "runner" {
		o.Version = teamImageTag()
		ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
		o.Runners = a.twrunBinaries(ctx, o.OS)
		cancel()
	}
	b, name, err := f.Export(o)
	if err != nil {
		return "", err
	}
	dir := a.ExportDir()
	_ = os.MkdirAll(dir, 0o755)
	var p string
	if a.cfg.Get().ExportNoAsk { // Settings → Files & Folders: saved at once (a name already there gets a number)
		p = filepath.Join(dir, name)
		ext := filepath.Ext(name)
		for i := 2; ; i++ {
			if _, err := os.Stat(p); err != nil {
				break
			}
			p = filepath.Join(dir, strings.TrimSuffix(name, ext)+" ("+strconv.Itoa(i)+")"+ext)
		}
	} else {
		p, err = wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "Export Automation Suite", DefaultDirectory: dir, DefaultFilename: name, CanCreateDirectories: true})
		if err != nil || p == "" {
			return "", err
		}
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return "", err
	}
	f.Audit("export", fmt.Sprintf("%s (%s, %s, %d tests or plans)", filepath.Base(p), o.Lang, o.OS, len(o.Files)))
	return p, nil
}

// TestProcs lists the procedures (procs/*.yaml): shared steps with parameters that tests call.
func (a *App) TestProcs() ([]testrec.ProcInfo, error) {
	f, err := a.testsFolder()
	if err != nil {
		return nil, err
	}
	return f.Procs(), nil
}

// TestProc reads one procedure.
func (a *App) TestProc(name string) (testrec.Proc, error) {
	f, err := a.testsFolder()
	if err != nil {
		return testrec.Proc{}, err
	}
	return f.LoadProc(name)
}

// TestSaveProc writes a procedure; it returns its file.
func (a *App) TestSaveProc(name string, p testrec.Proc) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	if err := mayEdit(f); err != nil {
		return "", err
	}
	f.Audit("save procedure", name)
	return f.SaveProc(name, p)
}

// TestMakeTemplate writes a TextFSM template made with the table maker (templates/<name>.textfsm) and, when
// the session has a device type, its index line so "parse: auto" finds it for that command; it returns
// the file name to put in the step's parse.
func (a *App) TestMakeTemplate(name, session, command string, spec testrec.TemplateSpec) (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	src, err := testrec.MakeTemplate(spec)
	if err != nil {
		return "", err
	}
	file := strings.Trim(regexp.MustCompile(`[^a-z0-9_]+`).ReplaceAllString(strings.ToLower(name), "_"), "_")
	if file == "" {
		return "", errors.New("give the table a name")
	}
	file += ".textfsm"
	dir := f.TemplatesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o644); err != nil {
		return "", err
	}
	if s, err := a.store.Get(session); err == nil && strings.TrimSpace(command) != "" {
		if platform := textfsm.Platform(s.DeviceType); platform != "" {
			idx := filepath.Join(dir, "index")
			b, _ := os.ReadFile(idx)
			line := fmt.Sprintf("%s, .*, %s, %s", file, platform, regexp.QuoteMeta(strings.Join(strings.Fields(command), " ")))
			if !strings.Contains(string(b), file+",") {
				// own templates go before the community ones
				txt := string(b)
				if i := strings.Index(txt, "# --- ntc-templates"); i >= 0 {
					txt = txt[:i] + line + "\n" + txt[i:]
				} else {
					txt = strings.TrimRight(txt, "\n") + "\n" + line + "\n"
				}
				_ = os.WriteFile(idx, []byte(txt), 0o644)
			}
		}
	}
	f.Audit("make table", file)
	return file, nil
}

// TestPreviewTemplate parses output with the template a table spec makes (the table maker's preview).
func (a *App) TestPreviewTemplate(spec testrec.TemplateSpec, output string) (*textfsm.Table, error) {
	src, err := testrec.MakeTemplate(spec)
	if err != nil {
		return nil, err
	}
	t, err := textfsm.Parse(src)
	if err != nil {
		return nil, err
	}
	tab, err := t.Run(output)
	return &tab, err
}

// TestPickData lets the person choose a CSV or Excel file for a test's data; a file outside the tests
// folder is copied into data/. It returns the path to put in the test's data.
func (a *App) TestPickData() (string, error) {
	f, err := a.testsFolder()
	if err != nil {
		return "", err
	}
	p, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Test Data (one run per row)",
		Filters: []wruntime.FileFilter{{DisplayName: "CSV or Excel (*.csv, *.xlsx)", Pattern: "*.csv;*.xlsx"}}})
	if err != nil || p == "" {
		return "", err
	}
	if rel, err := filepath.Rel(f.Root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel), nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	rel := "data/" + filepath.Base(p)
	if err := os.MkdirAll(filepath.Join(f.Root, "data"), 0o755); err != nil {
		return "", err
	}
	return rel, os.WriteFile(filepath.Join(f.Root, filepath.FromSlash(rel)), b, 0o644)
}

// TestTrends is the suite over its runs: counts per run, the tests that fail most and pass rates per
// software version (files: only those tests or plans; testbed: only runs on it).
func (a *App) TestTrends(files []string, testbed string, limit int) (testrec.Trends, error) {
	f, err := a.testsFolder()
	if err != nil {
		return testrec.Trends{}, err
	}
	var exp []string
	if len(files) > 0 {
		if exp, err = f.ExpandFiles(files, nil); err != nil {
			return testrec.Trends{}, err
		}
	}
	if limit <= 0 {
		limit = 30
	}
	return f.SuiteTrends(exp, testbed, limit), nil
}

// inventoryDevice is a saved session as a standalone suite's device: how to reach it, never a secret.
func (a *App) inventoryDevice(path string) (testrec.InventoryDevice, bool) {
	s, err := a.store.Get(path)
	if err != nil {
		return testrec.InventoryDevice{}, false
	}
	d := testrec.InventoryDevice{Session: path, Host: s.Hostname, Port: s.Port, Username: s.Username, DeviceType: s.DeviceType,
		KeyFile: s.IdentityFile, Jump: s.JumpSession}
	switch strings.ToLower(s.Protocol) {
	case "api":
		d.Protocol, d.BaseURL, d.Auth, d.KeyName, d.Insecure = "api", s.APIBaseURL, s.APIAuth, s.APIKeyName, s.APIInsecure
	case "telnet", "telnet/tls":
		d.Protocol = "telnet"
	case "web":
		d.Protocol, d.WebURL, d.Browser, d.Viewport, d.Insecure, d.WebLogin = "web", s.WebURL, s.WebBrowser, s.WebViewport, s.WebInsecure, webrun.ParseLogin(s.WebLogin)
	default:
		d.Protocol = "ssh"
	}
	// "no password" saved for the session or its device type: the suite then never asks for one
	if pw, ok := secrets.Lookup(path); ok && pw == "" {
		d.NoPassword = true
	} else if !ok && s.DeviceType != "" {
		if pw, ok := secrets.Lookup("devicetype:" + strings.ToLower(s.DeviceType)); ok && pw == "" {
			d.NoPassword = true
		}
	}
	if s.DeviceType != "" {
		if dt, ok := a.deviceTypeNamed(s.DeviceType); ok {
			for _, re := range dt.Errors() {
				d.Errors = append(d.Errors, re.String())
			}
		}
	}
	d.Stack, d.Slots = a.isStack(path), a.slotsOf(path)
	if s.SNMPVersion != "" || s.SNMPUser != "" {
		d.SNMPVer, d.SNMPHost, d.SNMPPort, d.SNMPUser, d.SNMPAuth, d.SNMPPriv = s.SNMPVersion, s.SNMPHost, s.SNMPPort, s.SNMPUser, s.SNMPAuth, s.SNMPPriv
	}
	return d, true
}

// twrunBinaries gets the twrun programs of an operating system (both architectures): next to Termwright,
// in the data folder's runners/, else from the GitHub release of this version (nightly for development
// builds). The ones it cannot get are left out - the suite's run script downloads them on its first run.
func (a *App) twrunBinaries(ctx context.Context, goos string) map[string][]byte {
	if goos == "mac" {
		goos = "darwin"
	}
	out := map[string][]byte{}
	exe, _ := os.Executable()
	repo := a.cfg.Get().UpdateRepo
	if repo == "" {
		repo = "nperiannan/termwright"
	}
	tag := teamImageTag()
	for _, arch := range []string{"amd64", "arm64"} {
		name := "twrun-" + goos + "-" + arch
		if goos == "windows" {
			name += ".exe"
		}
		var found []byte
		for _, dir := range []string{filepath.Dir(exe), filepath.Join(a.cfg.DataDir(), "runners")} {
			if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil && len(b) > 1<<20 {
				found = b
				break
			}
		}
		if found == nil {
			u := "https://github.com/" + repo + "/releases/download/v" + tag + "/" + name
			if tag == "nightly" {
				u = "https://github.com/" + repo + "/releases/download/nightly/" + name
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				continue
			}
			res, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
			if err != nil {
				continue
			}
			if res.StatusCode == http.StatusOK {
				found, _ = io.ReadAll(io.LimitReader(res.Body, 64<<20))
			}
			res.Body.Close()
		}
		if len(found) > 1<<20 {
			out[name] = found
		}
	}
	return out
}

var portVar = regexp.MustCompile(`^port[0-9]+$`)

var roleValueRe = regexp.MustCompile(`\{\{\s*([A-Za-z_][\w-]*)\.([A-Za-z_][\w-]*)\s*\}\}`)

// factNames are the values every saved-session role has by itself.
var factNames = map[string]bool{"device_type": true, "host": true, "model": true, "version": true, "name": true, "session": true}

// testsRoleValues: the role values ({{role.key}}) the tests use, as "role.key".
func testsRoleValues(f testrec.Folder) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range f.List() {
		b, err := os.ReadFile(filepath.Join(f.Root, filepath.FromSlash(c.File)))
		if err != nil {
			continue
		}
		for _, m := range roleValueRe.FindAllStringSubmatch(string(b), -1) {
			if k := m[1] + "." + m[2]; !factNames[m[2]] && !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	return out
}

// TestbedSetRole makes a role of a testbed a saved session (a test's role mapped to a device).
func (a *App) TestbedSetRole(file, role, sess string) error {
	f, err := a.testsFolder()
	if err != nil {
		return err
	}
	if err := mayEdit(f); err != nil {
		return err
	}
	if _, err := a.store.Get(sess); err != nil {
		return fmt.Errorf("no saved session %s", sess)
	}
	return f.SetTestbedRole(file, role, sess)
}

// TestbedSetValue sets one of a role's values in a testbed ({{role.key}}, e.g. its port); "" removes it.
func (a *App) TestbedSetValue(file, role, key, value string) error {
	f, err := a.testsFolder()
	if err != nil {
		return err
	}
	if err := mayEdit(f); err != nil {
		return err
	}
	return f.SetTestbedValue(file, role, key, value)
}

// TestbedRenameRole renames a role of a testbed.
func (a *App) TestbedRenameRole(file, from, to string) error {
	f, err := a.testsFolder()
	if err != nil {
		return err
	}
	if err := mayEdit(f); err != nil {
		return err
	}
	return f.RenameTestbedRole(file, from, strings.TrimSpace(to))
}

// TestRole is a device a test uses: its role and the session it uses by default.
type TestRole struct {
	Role       string `json:"role"`
	Session    string `json:"session"`
	DeviceType string `json:"deviceType,omitempty"`
	// Protocol of the session ("Web" for a web app the test logs in to).
	Protocol string `json:"protocol,omitempty"`
}

// TestsRoles lists the roles of tests: their variables that are saved sessions.
func (a *App) TestsRoles(files []string) []TestRole {
	f, err := a.testsFolder()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := []TestRole{}
	for _, file := range files {
		c, err := f.Load(file)
		if err != nil {
			continue
		}
		keys := make([]string, 0, len(c.Vars))
		for k := range c.Vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := c.Vars[k]
			if seen[k] || strings.Contains(k, ".") {
				continue
			}
			if s, err := a.store.Get(v); err == nil {
				seen[k] = true
				out = append(out, TestRole{Role: k, Session: v, DeviceType: s.DeviceType, Protocol: s.Protocol})
			}
		}
	}
	return out
}

// TestbedMeta is what a testbed says about itself: its tags (tests can require them), its description and
// the links between its devices ("dut.port1 - peer.port3").
type TestbedMeta struct {
	Tags        []string `json:"tags"`
	Description string   `json:"description,omitempty"`
	Links       []string `json:"links"`
}

// TestbedInfo reads a testbed's tags, description and links.
func (a *App) TestbedInfo(file string) (TestbedMeta, error) {
	out := TestbedMeta{Tags: []string{}, Links: []string{}}
	f, err := a.testsFolder()
	if err != nil {
		return out, err
	}
	err = f.EditTestbed(file, func(m map[string]any) error {
		for _, x := range asList(m["tags"]) {
			out.Tags = append(out.Tags, x)
		}
		out.Links = append(out.Links, asList(m["links"])...)
		if d, ok := m["description"].(string); ok {
			out.Description = d
		}
		return errNoChange
	})
	if errors.Is(err, errNoChange) {
		err = nil
	}
	return out, err
}

var errNoChange = errors.New("no change")

func asList(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			out = append(out, fmt.Sprint(x))
		}
	}
	return out
}

// TestbedSetInfo writes a testbed's tags, description and links (links as "dut.port1 - peer.port3").
func (a *App) TestbedSetInfo(file string, meta TestbedMeta) error {
	f, err := a.testsFolder()
	if err != nil {
		return err
	}
	if err := mayEdit(f); err != nil {
		return err
	}
	for _, l := range meta.Links {
		if p := strings.Fields(strings.ReplaceAll(l, " - ", " ")); len(p) != 2 || !strings.Contains(p[0], ".") || !strings.Contains(p[1], ".") {
			return fmt.Errorf("link %q: write it as role.port - role.port, e.g. dut.port1 - peer.port1", l)
		}
	}
	return f.EditTestbed(file, func(m map[string]any) error {
		set := func(k string, l []string) {
			if len(l) == 0 {
				delete(m, k)
				return
			}
			x := make([]any, len(l))
			for i, s := range l {
				x[i] = s
			}
			m[k] = x
		}
		set("tags", meta.Tags)
		set("links", meta.Links)
		if strings.TrimSpace(meta.Description) == "" {
			delete(m, "description")
		} else {
			m["description"] = strings.TrimSpace(meta.Description)
		}
		return nil
	})
}

// TestbedSetPortProp sets a property of a device's test port (speed, to…); "" removes it.
func (a *App) TestbedSetPortProp(file, role, port, key, value string) error {
	f, err := a.testsFolder()
	if err != nil {
		return err
	}
	if err := mayEdit(f); err != nil {
		return err
	}
	return f.EditTestbed(file, func(m map[string]any) error {
		g, ok := m[role].(map[string]any)
		if !ok {
			if m[role] == nil {
				return fmt.Errorf("no role %s", role)
			}
			g = map[string]any{"session": fmt.Sprint(m[role])}
			m[role] = g
		}
		pm, ok := g[port].(map[string]any)
		if !ok {
			pm = map[string]any{}
			if g[port] != nil {
				pm["value"] = fmt.Sprint(g[port])
			}
		}
		if value == "" {
			delete(pm, key)
		} else {
			pm[key] = value
		}
		switch {
		case len(pm) == 0:
			delete(g, port)
		case len(pm) == 1 && pm["value"] != nil:
			g[port] = pm["value"]
		default:
			g[port] = pm
		}
		return nil
	})
}

// TestsSuiteDefaults reads a folder's defaults.yaml ("" = the tests folder): values and step options for
// every test in it and below.
func (a *App) TestsSuiteDefaults(dir string) (testrec.Defaults, error) {
	f, err := a.testsFolder()
	if err != nil {
		return testrec.Defaults{}, err
	}
	return f.LoadSuiteDefaults(dir), nil
}

// TestsSaveSuiteDefaults writes a folder's defaults.yaml.
func (a *App) TestsSaveSuiteDefaults(dir string, d testrec.Defaults) error {
	f, err := a.testsFolder()
	if err != nil {
		return err
	}
	if err := mayEdit(f); err != nil {
		return err
	}
	return f.SaveSuiteDefaults(dir, d)
}

// SuiteDefaultsView is a folder's own defaults and what it gets from the folders above.
type SuiteDefaultsView struct {
	Own       testrec.Defaults `json:"own"`
	Inherited testrec.Defaults `json:"inherited"`
}

// TestsDefaultsView reads a folder's defaults.yaml with what it inherits ("" = all tests).
func (a *App) TestsDefaultsView(dir string) (SuiteDefaultsView, error) {
	f, err := a.testsFolder()
	if err != nil {
		return SuiteDefaultsView{}, err
	}
	return SuiteDefaultsView{Own: f.LoadSuiteDefaults(dir), Inherited: f.InheritedDefaults(dir)}, nil
}

// SkipPreview is a test the chosen testbed will skip, and why.
type SkipPreview struct {
	File string `json:"file"`
	Case string `json:"case"`
	Why  string `json:"why"`
}

// TestsWillSkip lists the tests (all when files is empty) a testbed does not have what they need.
func (a *App) TestsWillSkip(files []string, testbed string) []SkipPreview {
	out := []SkipPreview{}
	f, err := a.testsFolder()
	if err != nil {
		return out
	}
	vars := map[string]string{}
	if testbed != "" {
		if tb, err := f.Testbed(testbed); err == nil {
			vars = tb
		}
	}
	if len(files) == 0 {
		for _, c := range f.List() {
			files = append(files, c.File)
		}
	}
	d := &testDriver{a: a}
	for _, file := range files {
		c, err := f.Load(file)
		if err != nil {
			continue
		}
		if why := f.Unmet(c, vars, d); why != "" {
			out = append(out, SkipPreview{File: file, Case: c.Name, Why: why})
		}
	}
	return out
}
