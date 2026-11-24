package webrun

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Leftover is a test browser (or a web runner) a Termwright web step started that is still running
// though nothing uses it: its runner was killed, or the app quit while it was open.
type Leftover struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`    // Chrome for Testing, Chromium, Google Chrome, Microsoft Edge, Firefox, WebKit, runner
	Started string `json:"started"` // as the system says it, "" when unknown
}

// proc is one line of the process list.
type proc struct {
	pid, ppid int
	started   string
	cmd       string
}

// a browser Playwright started has its profile in a temporary folder with this name
var pwProfile = regexp.MustCompile(`playwright_[a-z]+dev_profile-`)

// Leftovers lists the test browsers and web runners left running. Those of a recording or step-by-step
// browser open in this app now (its runner is a child of this process) are not listed.
func (r Runner) Leftovers() []Leftover {
	return leftovers(listProcs(), os.Getpid(), r.Dir)
}

// CloseLeftovers closes them: the browser's main process (its helpers end with it), killed when it does
// not end in 3 s. It says how many were closed.
func (r Runner) CloseLeftovers() int {
	l := r.Leftovers()
	for _, x := range l {
		stop(x.PID, false)
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, x := range l {
		for alive(x.PID) && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		if alive(x.PID) {
			stop(x.PID, true)
		}
	}
	return len(l)
}

func leftovers(ps []proc, self int, dir string) []Leftover {
	by := map[int]proc{}
	for _, p := range ps {
		by[p.pid] = p
	}
	runner := func(p proc) bool {
		i := strings.Index(p.cmd, "runner.js")
		return i > 0 && strings.Contains(strings.ToLower(p.cmd[:i]), "node") && (dir == "" || strings.Contains(p.cmd, filepath.Join(dir, "runner.js")))
	}
	// in use: under a runner this app started
	inUse := func(p proc) bool {
		for i, q := 0, p; i < 30; i++ {
			if runner(q) && q.ppid == self {
				return true
			}
			n, ok := by[q.ppid]
			if !ok || n.pid == q.pid {
				return false
			}
			q = n
		}
		return false
	}
	var out []Leftover
	for _, p := range ps {
		switch {
		case p.pid == self:
		case runner(p) && p.ppid != self:
			out = append(out, Leftover{PID: p.pid, Name: "web runner", Started: p.started})
		case pwProfile.MatchString(p.cmd) && !strings.Contains(p.cmd, "--type=") && !inUse(p):
			// only the browser's main process: not a helper of one already listed
			if pp, ok := by[p.ppid]; ok && pwProfile.MatchString(pp.cmd) {
				continue
			}
			out = append(out, Leftover{PID: p.pid, Name: browserName(p.cmd), Started: p.started})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

func browserName(cmd string) string {
	c := cmd // the program, not its arguments (the profile folder says chromium too)
	if i := strings.Index(c, " --"); i > 0 {
		c = c[:i]
	}
	c = strings.ToLower(c)
	for _, n := range []struct{ k, v string }{{"chrome for testing", "Chrome for Testing"}, {"headless_shell", "Chromium (hidden)"}, {"headless-shell", "Chromium (hidden)"},
		{"msedge", "Microsoft Edge"}, {"microsoft edge", "Microsoft Edge"}, {"firefox", "Firefox"}, {"webkit", "WebKit"}, {"pw_run", "WebKit"},
		{"google chrome", "Google Chrome"}, {"chromium", "Chromium"}, {"chrome", "Chrome"}} {
		if strings.Contains(c, n.k) {
			return n.v
		}
	}
	return "Test browser"
}

// listProcs: pid, parent, start and command line of every process of this user.
func listProcs() []proc {
	if runtime.GOOS == "windows" {
		ps := `Get-CimInstance Win32_Process | ForEach-Object { "$($_.ProcessId)` + "`t" + `$($_.ParentProcessId)` + "`t" + `$(if ($_.CreationDate) { $_.CreationDate.ToString('yyyy-MM-dd HH:mm') })` + "`t" + `$($_.CommandLine)" }`
		c := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
		hide(c)
		b, err := c.Output()
		if err != nil {
			return nil
		}
		return parseWin(string(b))
	}
	b, err := exec.Command("ps", "-axww", "-o", "pid=,ppid=,lstart=,command=").Output()
	if err != nil {
		return nil
	}
	return parsePS(string(b))
}

// parsePS reads "  pid ppid Tue Nov 24 10:42:01 2026 command…" lines.
func parsePS(s string) []proc {
	var out []proc
	for _, ln := range strings.Split(s, "\n") {
		f := strings.Fields(ln)
		if len(f) < 8 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		if e1 != nil || e2 != nil {
			continue
		}
		started := strings.Join(f[2:7], " ")
		if t, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", started, time.Local); err == nil {
			started = t.Format("2006-01-02 15:04")
		}
		out = append(out, proc{pid, ppid, started, strings.Join(f[7:], " ")})
	}
	return out
}

func parseWin(s string) []proc {
	var out []proc
	for _, ln := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		f := strings.SplitN(ln, "\t", 4)
		if len(f) < 4 {
			continue
		}
		pid, e1 := strconv.Atoi(strings.TrimSpace(f[0]))
		ppid, e2 := strconv.Atoi(strings.TrimSpace(f[1]))
		if e1 != nil || e2 != nil {
			continue
		}
		out = append(out, proc{pid, ppid, strings.TrimSpace(f[2]), f[3]})
	}
	return out
}
