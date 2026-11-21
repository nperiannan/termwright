package app

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/testrec"
)

const testsCLIHelp = `termwright tests - Termwright tests without the window (for CI machines)

  termwright tests run [files or folders…] [--testbed lab.yaml] [--var name=value]… [--folder dir]
                       [--junit junit.xml] [--production] [--on-cleanup-failure stop|restore|factory]
                       [--cleanup steps|off|auto]
      Runs tests (default: all) with Termwright's sessions, saved logins and device types, prints
      pass/fail per step and exits 1 when a test failed. --production lets tests use production
      sessions (refused otherwise). When a test's cleanup fails the run stops and the remaining
      tests are skipped, unless --on-cleanup-failure restore (put back the configuration saved before
      the run; the device may restart) or factory (the device type's factory-default commands).
      --cleanup off runs no cleanups (tests build on what earlier ones configured); auto runs the
      cleanup steps and, for a test without them, the undo of the configuration commands that ran.

  termwright tests serve [--listen 127.0.0.1:8765] [--folder dir]
      Starts only the local API, so the generated Go, Python and TCL tests run on this computer
      (they read its address and key from mcp.json in the data folder). Stops with Ctrl+C.

  termwright tests secrets [files…] [--folder dir]
      Lists the environment variables that can hold the tests' passwords, communities and keys on a
      computer without a credential store (TERMWRIGHT_SECRET_…). Values are never printed.

The tests folder is the one set in Termwright (Tests → Open…) unless --folder is given.
`

// testsCLIMain is "termwright tests …".
func testsCLIMain(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(testsCLIHelp)
		return 0
	}
	fs := flag.NewFlagSet("termwright tests "+args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	folder := fs.String("folder", "", "the tests folder")
	testbed := fs.String("testbed", "", "a testbed file (testbeds/…)")
	junit := fs.String("junit", "", "also write the JUnit XML report here")
	listen := fs.String("listen", "127.0.0.1:0", "serve: address to listen on")
	prod := fs.Bool("production", false, "run: allow production sessions")
	onFail := fs.String("on-cleanup-failure", "stop", "run: stop, restore or factory")
	cleanup := fs.String("cleanup", "steps", "run: steps, off or auto")
	vars := map[string]string{}
	fs.Func("var", "name=value for {{name}} (repeatable)", func(s string) error {
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return errors.New("want name=value")
		}
		vars[k] = v
		return nil
	})
	// flags may come after the files: parse them out wherever they are
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
	a := NewApp()
	if err := a.init(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *folder != "" {
		abs, _ := filepath.Abs(*folder)
		a.testsDir = abs
	}
	f, err := a.testsFolder()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch args[0] {
	case "run":
		return a.testsCLIRun(f, files, *testbed, vars, *junit, *prod, *onFail, strings.TrimPrefix(*cleanup, "steps"))
	case "serve":
		return a.testsCLIServe(*listen)
	case "secrets":
		return a.testsCLISecrets(f, files)
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], testsCLIHelp)
	return 2
}

// relFiles makes the given files and folders paths relative to the tests folder (a folder: every test
// below it); anything else is passed on as given (relative to the tests folder, "cases/" may be left out).
func relFiles(f testrec.Folder, in []string) []string {
	var out []string
	for _, p := range in {
		abs, _ := filepath.Abs(p)
		inside := func(x string) (string, bool) {
			r, err := filepath.Rel(f.Root, x)
			return filepath.ToSlash(r), err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
		}
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			for _, c := range f.List() {
				if r, err := filepath.Rel(abs, filepath.Join(f.Root, c.File)); err == nil && !strings.HasPrefix(r, "..") {
					out = append(out, c.File)
				}
			}
			continue
		}
		if r, ok := inside(abs); ok && fileExists(abs) {
			out = append(out, r)
			continue
		}
		out = append(out, p)
	}
	return out
}

func (a *App) testsCLIRun(f testrec.Folder, files []string, testbed string, vars map[string]string, junit string, prod bool, onFail, cleanup string) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	req := TestRunRequest{Files: relFiles(f, files), Vars: vars, Testbed: testbed, Confirmed: prod, OnCleanupFail: onFail, Cleanup: cleanup}
	rep, err := a.runTests(ctx, req, nil)
	if err != nil && len(rep.Results) == 0 {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, errTestsProduction) {
			fmt.Fprintln(os.Stderr, "Add --production to run them anyway.")
		}
		return 1
	}
	fmt.Print(testsReportText(f, rep))
	if junit != "" {
		src, err := os.Open(filepath.Join(f.Root, rep.Dir, "junit.xml"))
		if err == nil {
			defer src.Close()
			if dst, err := os.Create(junit); err == nil {
				io.Copy(dst, src)
				dst.Close()
			}
		}
	}
	if rep.Failed > 0 || rep.Skipped > 0 {
		return 1
	}
	return 0
}

func (a *App) testsCLIServe(listen string) int {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s := &mcpServer{token: fmt.Sprintf("%x", b), url: "http://" + ln.Addr().String() + "/mcp"}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { a.apiHTTP(s, w, r) })
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go s.srv.Serve(ln)
	file := mcpFile(a.cfg.DataDir())
	old, _ := os.ReadFile(file)
	j := fmt.Sprintf(`{"url":%q,"token":%q,"pid":%d}`, s.url, s.token, os.Getpid())
	if err := os.WriteFile(file, []byte(j), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Termwright tests API on http://%s/api/v1 (address and key in %s). Ctrl+C stops it.\n", ln.Addr(), file)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	<-ctx.Done()
	s.srv.Close()
	if len(old) > 0 {
		os.WriteFile(file, old, 0o600)
	} else {
		os.WriteFile(file, []byte(`{"stopped":true}`), 0o600)
	}
	if a.terms != nil {
		a.terms.CloseAll()
	}
	return 0
}

// testsSecretKeys are the credential store keys a session's steps may read.
func testsSecretKeys(kind, path string) []string {
	switch kind {
	case "run":
		return []string{path, "keypass:" + path}
	case "http":
		return []string{httpSecretKey(path)}
	case "snmp":
		return []string{snmpKey("community", path), snmpKey("wcommunity", path), snmpKey("auth", path), snmpKey("priv", path)}
	}
	return nil
}

func (a *App) testsCLISecrets(f testrec.Folder, files []string) int {
	files = relFiles(f, files)
	if len(files) == 0 {
		for _, c := range f.List() {
			files = append(files, c.File)
		}
	}
	type use struct{ kind, path string }
	seen := map[use]bool{}
	for _, file := range files {
		c, err := f.Load(file)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		for _, st := range c.Steps {
			switch {
			case st.Run != nil:
				seen[use{"run", testrec.Fill(st.Run.Session, c.Vars)}] = true
			case st.HTTP != nil && st.HTTP.Session != "":
				seen[use{"http", testrec.Fill(st.HTTP.Session, c.Vars)}] = true
			case st.SNMP != nil:
				seen[use{"snmp", testrec.Fill(st.SNMP.Session, c.Vars)}] = true
			}
		}
	}
	var lines []string
	for u := range seen {
		for _, k := range testsSecretKeys(u.kind, u.path) {
			_, has := secrets.Lookup(k)
			state := "not set"
			if has {
				state = "set (credential store or environment)"
			}
			lines = append(lines, fmt.Sprintf("%-60s %s  [%s %s]", secrets.EnvName(k), state, u.kind, u.path))
		}
	}
	sort.Strings(lines)
	fmt.Println("Only the ones a session's login really uses are needed; accounts (Passwords & Accounts) use TERMWRIGHT_SECRET_CREDENTIAL_<NAME>.")
	for _, l := range lines {
		fmt.Println(l)
	}
	return 0
}
