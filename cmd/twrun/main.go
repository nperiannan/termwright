// Command twrun runs Termwright tests (the YAML files of a tests folder or an exported suite) without
// Termwright: it logs in to the devices of inventory.yaml itself. It writes the same reports as
// Termwright's runs (reports/<date-time>/: report.html, report.pdf, junit.xml, the run logs).
//
//	twrun run [tests, folders or suites/… plans] [--testbed testbeds/lab.yaml] [--var name=value]…
//	          [--tags smoke,!slow] [--rerun N] [--test-timeout s] [--run-timeout s]
//	          [--cleanup steps|off] [--on-cleanup-fail stop] [--folder dir] [--junit file]
//	twrun check [--folder dir]       log in to each device of inventory.yaml
//	twrun setup-web [--folder dir]   install Playwright (Node.js needed) for web steps
//	twrun version
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/termwright/termwright/internal/secretref"
	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/standalone"
	"github.com/termwright/termwright/internal/testrec"
	"github.com/termwright/termwright/internal/textfsm"
)

// Version is set by the build.
var Version = "dev"

const help = `twrun - runs Termwright tests without Termwright

  twrun run [tests, folders or suites/… plans] [options]
      --testbed testbeds/lab.yaml   the devices each role is (default: the tests' own sessions)
      --var name=value              a value for {{name}} (repeat it)
      --tags smoke,!slow            only tests with one of these tags (!tag leaves them out)
      --rerun N                     run a failed test again up to N times (a pass then is "flaky")
      --test-timeout S --run-timeout S   seconds a test / the whole run may take
      --cleanup steps|off|auto      after each test (default: its cleanup steps)
      --junit file                  also copy the JUnit XML there
      --folder dir                  the suite (default: this folder)
  twrun check      log in to each device of inventory.yaml and show its prompt
  twrun setup-web  install Playwright and Chromium for web steps (Node.js needed)
  twrun version

Passwords are never in the suite: TW_PASSWORD_<NAME> (the session's last part in capitals, other
characters as _) or TW_PASSWORD for every device (set but empty: no password), else the passwords
Termwright saved in this computer's keychain (TW_NO_KEYCHAIN=1 turns that off), else typed when asked. API tokens: TW_TOKEN_<NAME>;
SNMP communities: TW_COMMUNITY_<NAME>. TW_KNOWN_HOSTS=file checks SSH host keys.
`

func main() { os.Exit(run(os.Args[1:])) }

type varFlags map[string]string

func (v varFlags) String() string { return "" }
func (v varFlags) Set(s string) error {
	k, val, ok := strings.Cut(s, "=")
	if !ok {
		return fmt.Errorf("--var %q: name=value", s)
	}
	v[k] = val
	return nil
}

func run(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(help)
		return 2
	}
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	folder := fs.String("folder", ".", "")
	testbed := fs.String("testbed", "", "")
	tags := fs.String("tags", "", "")
	rerun := fs.Int("rerun", 0, "")
	testTO := fs.Int("test-timeout", 0, "")
	runTO := fs.Int("run-timeout", 0, "")
	cleanup := fs.String("cleanup", "", "")
	onFail := fs.String("on-cleanup-fail", "", "")
	junit := fs.String("junit", "", "")
	vars := varFlags{}
	fs.Var(vars, "var", "")
	// flags may come after the files
	var files []string
	rest := args[1:]
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		rest = fs.Args()
		if len(rest) > 0 {
			files = append(files, rest[0])
			rest = rest[1:]
		}
	}
	root, _ := filepath.Abs(*folder)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch cmd {
	case "version":
		fmt.Println("twrun", Version)
		return 0
	case "setup-web":
		e := &standalone.Engine{Root: root}
		r := e.WebRunner()
		if err := r.Setup(ctx, func(s string) { fmt.Println(s) }); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("Playwright is ready in", r.Dir)
		return 0
	}
	e, err := standalone.Open(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	e.Ask = standalone.AskTerminal
	if os.Getenv("TW_NO_KEYCHAIN") == "" {
		res := &secretref.Resolver{}
		e.Keychain = func(key string) (string, bool) {
			v, ok := secrets.Lookup(key)
			if !ok || !secretref.IsRef(v) {
				return v, ok
			}
			pw, err := res.Resolve(ctx, v, func(title, msg string) (string, error) { return standalone.AskTerminal(title + " - " + msg) })
			if err != nil {
				fmt.Fprintln(os.Stderr, "password manager:", err)
				return "", false
			}
			return pw, true
		}
	}
	defer e.Close()
	switch cmd {
	case "check":
		code := 0
		for _, d := range e.Devices() {
			if strings.EqualFold(d.Protocol, "api") {
				fmt.Printf("  -  %-40s API %s (not logged in to)\n", d.Session, d.BaseURL)
				continue
			}
			if strings.EqualFold(d.Protocol, "web") {
				fmt.Printf("  -  %-40s web app %s (logged in to by its web steps)\n", d.Session, d.WebURL)
				continue
			}
			c, cancel := context.WithTimeout(ctx, 45*time.Second)
			_, err := e.Command(c, d.Session, "", "", 15*time.Second)
			cancel()
			if err != nil {
				code = 1
				fmt.Printf("  ✗  %-40s %s\n", d.Session, err)
			} else {
				fmt.Printf("  ✓  %-40s %s@%s\n", d.Session, d.Username, d.Host)
			}
		}
		return code
	case "run":
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, help)
		return 2
	}
	f := testrec.Folder{Root: root}
	v := map[string]string(vars)
	if *testbed != "" {
		tb, err := f.Testbed(*testbed)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for k, x := range vars {
			tb[k] = x
		}
		v = tb
	}
	var tagList []string
	for _, t := range strings.Split(*tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tagList = append(tagList, t)
		}
	}
	opt := testrec.RunOptions{Testbed: *testbed, Tags: tagList, Cleanup: strings.TrimPrefix(*cleanup, "steps"), OnCleanupFail: *onFail,
		Rerun: min(max(*rerun, 0), 5), CaseTimeout: *testTO, RunTimeout: *runTO,
		OnStepStart: func(file, name, step string) { fmt.Printf("  … %s: %s\n", name, step) }}
	rep, err := f.RunFilesWith(ctx, rel(f, files), &driver{e}, v, opt, func(r testrec.Result) {
		word := map[bool]string{true: "PASS", false: "FAIL"}[r.Passed]
		if r.Skipped {
			word = "SKIP"
		}
		fmt.Printf("%s %s (%s, %.1f s)\n", word, r.Case, r.File, float64(r.Ms)/1000)
		if !r.Passed && r.Error != "" {
			fmt.Printf("     %s\n", r.Error)
		}
	})
	if err != nil && len(rep.Results) == 0 {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("\n%d passed, %d failed, %d skipped in %.1f s. Report: %s\n", rep.Passed, rep.Failed, rep.Skipped, float64(rep.Ms)/1000, filepath.Join(root, rep.Dir, "report.html"))
	if *junit != "" {
		if src, err := os.Open(filepath.Join(root, rep.Dir, "junit.xml")); err == nil {
			if dst, err := os.Create(*junit); err == nil {
				_, _ = io.Copy(dst, src)
				dst.Close()
			}
			src.Close()
		}
	}
	if rep.Failed > 0 || rep.Skipped > 0 {
		return 1
	}
	return 0
}

// rel makes paths relative to the suite folder (a folder: every test below it).
func rel(f testrec.Folder, in []string) []string {
	var out []string
	for _, p := range in {
		abs, _ := filepath.Abs(p)
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			for _, c := range f.List() {
				if r, err := filepath.Rel(abs, filepath.Join(f.Root, c.File)); err == nil && !strings.HasPrefix(r, "..") {
					out = append(out, c.File)
				}
			}
			continue
		}
		if r, err := filepath.Rel(f.Root, abs); err == nil && !strings.HasPrefix(r, "..") {
			if _, err := os.Stat(abs); err == nil {
				out = append(out, filepath.ToSlash(r))
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

// driver runs the steps with the standalone engine.
type driver struct{ e *standalone.Engine }

func (d *driver) Command(ctx context.Context, session, cmd, prompt string) (string, error) {
	var to time.Duration
	if dl, ok := ctx.Deadline(); ok {
		to = time.Until(dl)
	}
	return d.e.Command(ctx, session, cmd, prompt, to)
}

func (d *driver) HTTP(ctx context.Context, r testrec.HTTPStep) (testrec.HTTPAnswer, error) {
	if r.Environment != "" || r.Collection != "" {
		return testrec.HTTPAnswer{}, fmt.Errorf("API environments and collections are Termwright's: use an API session of inventory.yaml or a full URL")
	}
	st, body, err := d.e.HTTP(ctx, standalone.HTTPRequest{Session: r.Session, Method: r.Method, URL: r.URL, Headers: r.Headers, Body: r.Body})
	return testrec.HTTPAnswer{Status: st, Body: body}, err
}

func (d *driver) SNMP(ctx context.Context, s testrec.SNMPStep) ([]testrec.SNMPValue, error) {
	var vals []standalone.SNMPValue
	for _, v := range s.Values {
		vals = append(vals, standalone.SNMPValue{OID: v.OID, Type: v.Type, Value: v.Value})
	}
	rows, err := d.e.SNMP(ctx, s.Session, s.Op, s.OIDs, vals)
	out := make([]testrec.SNMPValue, 0, len(rows))
	for _, r := range rows {
		out = append(out, testrec.SNMPValue{OID: r.OID, Name: r.Name, Value: r.Value, Display: r.Display})
	}
	return out, err
}

func (d *driver) Netconf(ctx context.Context, n testrec.NetconfStep) (string, error) {
	return d.e.Netconf(ctx, standalone.Netconf{Session: n.Session, Op: n.Op, Source: n.Source, Target: n.Target, Filter: n.Filter, Config: n.Config, RPC: n.RPC, Port: n.Port})
}

func (d *driver) Web(ctx context.Context, w testrec.WebStep) (string, error) {
	to := 30
	if dl, ok := ctx.Deadline(); ok {
		to = max(2, min(to, int(time.Until(dl).Seconds())))
	}
	return d.e.Web(ctx, w, to)
}

func (d *driver) ParseOutput(session, command, template, output string) (*textfsm.Table, error) {
	h, rows, err := d.e.Parse(session, command, output, template)
	if err != nil {
		return nil, err
	}
	return &textfsm.Table{Header: h, Rows: rows}, nil
}

func (d *driver) DeviceError(session, output string) string { return d.e.DeviceError(session, output) }

func (d *driver) TGen(ctx context.Context, t testrec.TGenStep) (string, error) {
	return d.e.TGen(ctx, standalone.TGenStep{Kind: t.Kind, Server: t.Server, Config: t.Config, Ports: t.Ports, Duration: t.Duration, Protocols: t.Protocols, Settle: t.Settle, Session: t.Session})
}
