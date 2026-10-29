package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/session"
)

// Web pages through a session: a browser window with a temporary profile
// whose traffic goes through a local SOCKS5 proxy that leaves from an SSH
// server (internal/term WebProxy). Closing the window ends the proxy and
// deletes the profile.

// WebBrowser is a browser found on this computer.
type WebBrowser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	path string
	kind string // "chromium" or "firefox"
}

// WebOpened tells what was opened.
type WebOpened struct {
	Browser string `json:"browser"`
	Via     string `json:"via"` // "" = directly (no proxy needed)
}

var (
	webMu   sync.Mutex
	webRuns = map[string]func(){} // id → stop
)

// WebBrowsers lists the browsers a page can be opened in (Safari cannot use
// a proxy per window, so it is not listed).
func (a *App) WebBrowsers() []WebBrowser {
	type cand struct{ id, name, kind string }
	cands := []cand{{"chrome", "Google Chrome", "chromium"}, {"edge", "Microsoft Edge", "chromium"}, {"chromium", "Chromium", "chromium"},
		{"brave", "Brave", "chromium"}, {"firefox", "Firefox", "firefox"}}
	var out []WebBrowser
	for _, c := range cands {
		for _, p := range browserPaths(c.id) {
			if p == "" {
				continue
			}
			if !filepath.IsAbs(p) {
				lp, err := exec.LookPath(p)
				if err != nil {
					continue
				}
				p = lp
			}
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				out = append(out, WebBrowser{ID: c.id, Name: c.name, path: p, kind: c.kind})
				break
			}
		}
	}
	return out
}

func browserPaths(id string) []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		app := map[string]string{"chrome": "Google Chrome.app/Contents/MacOS/Google Chrome", "edge": "Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"chromium": "Chromium.app/Contents/MacOS/Chromium", "brave": "Brave Browser.app/Contents/MacOS/Brave Browser", "firefox": "Firefox.app/Contents/MacOS/firefox"}[id]
		return []string{"/Applications/" + app, filepath.Join(home, "Applications", app)}
	case "windows":
		pf, pf86, local := os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")
		rel := map[string]string{"chrome": `Google\Chrome\Application\chrome.exe`, "edge": `Microsoft\Edge\Application\msedge.exe`,
			"chromium": `Chromium\Application\chrome.exe`, "brave": `BraveSoftware\Brave-Browser\Application\brave.exe`, "firefox": `Mozilla Firefox\firefox.exe`}[id]
		var out []string
		for _, b := range []string{pf, pf86, local} {
			if b != "" {
				out = append(out, filepath.Join(b, rel))
			}
		}
		return out
	default:
		return map[string][]string{"chrome": {"google-chrome", "google-chrome-stable"}, "edge": {"microsoft-edge", "microsoft-edge-stable"},
			"chromium": {"chromium", "chromium-browser"}, "brave": {"brave-browser", "brave"}, "firefox": {"firefox"}}[id]
	}
}

// OpenWebPage opens rawURL in a browser window of its own.
//
// through "session": out through the SSH server of session s (tab tabID's
// connection when it is connected). through "device": the device's web
// interface, through its jump host / jump station / proxy - or directly in
// the default browser when it needs none.
func (a *App) OpenWebPage(tabID string, s session.Session, through, rawURL, browserID string) (WebOpened, error) {
	if err := a.ready(); err != nil {
		return WebOpened{}, err
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return WebOpened{}, errors.New("enter a web address that starts with http:// or https://")
	}
	s = a.withFolderDefaults(s)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	wp, err := a.terms.OpenWebProxy(ctx, tabID, s, tabID, through == "device")
	if err != nil {
		return WebOpened{}, err
	}
	if wp == nil { // reached directly: the default browser
		if err := openWithSystem(u.String()); err != nil {
			return WebOpened{}, err
		}
		return WebOpened{Browser: "the default browser"}, nil
	}
	var br *WebBrowser
	all := a.WebBrowsers()
	for i := range all {
		if all[i].ID == browserID || br == nil {
			br = &all[i]
			if all[i].ID == browserID {
				break
			}
		}
	}
	if br == nil {
		wp.Close()
		return WebOpened{}, errors.New("no Chrome, Edge, Chromium, Brave or Firefox found - install one of them (Safari cannot send one window through a tunnel)")
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	profile := filepath.Join(os.TempDir(), "termwright-web", id)
	if err := os.MkdirAll(profile, 0o700); err != nil {
		wp.Close()
		return WebOpened{}, err
	}
	_, port, _ := net.SplitHostPort(wp.Addr)
	var args []string
	if br.kind == "firefox" {
		prefs := strings.Join([]string{
			`user_pref("network.proxy.type", 1);`,
			`user_pref("network.proxy.socks", "127.0.0.1");`,
			fmt.Sprintf(`user_pref("network.proxy.socks_port", %s);`, port),
			`user_pref("network.proxy.socks_version", 5);`,
			`user_pref("network.proxy.socks_remote_dns", true);`,
			`user_pref("network.proxy.no_proxies_on", "");`,
			`user_pref("network.proxy.allow_hijacking_localhost", true);`,
			`user_pref("network.trr.mode", 5);`,
			`user_pref("browser.shell.checkDefaultBrowser", false);`,
			`user_pref("browser.startup.homepage_override.mstone", "ignore");`,
			`user_pref("datareporting.policy.dataSubmissionEnabled", false);`,
			`user_pref("toolkit.telemetry.reportingpolicy.firstRun", false);`,
		}, "\n")
		if err := os.WriteFile(filepath.Join(profile, "user.js"), []byte(prefs+"\n"), 0o600); err != nil {
			wp.Close()
			return WebOpened{}, err
		}
		args = []string{"-no-remote", "-new-instance", "-profile", profile, u.String()}
	} else {
		args = []string{
			"--user-data-dir=" + profile,
			"--proxy-server=socks5://" + wp.Addr,
			"--proxy-bypass-list=<-loopback>",
			"--host-resolver-rules=MAP * ~NOTFOUND , EXCLUDE 127.0.0.1",
			"--no-first-run", "--no-default-browser-check", "--disable-sync", "--disable-background-networking",
			"--new-window", u.String(),
		}
	}
	cmd := exec.Command(br.path, args...)
	if err := cmd.Start(); err != nil {
		wp.Close()
		_ = os.RemoveAll(profile)
		return WebOpened{}, fmt.Errorf("start %s: %w", br.Name, err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			wp.Close()
			webMu.Lock()
			delete(webRuns, id)
			webMu.Unlock()
		})
	}
	webMu.Lock()
	webRuns[id] = func() { stop(); _ = cmd.Process.Kill() }
	webMu.Unlock()
	go func() {
		_ = cmd.Wait()
		stop()
		time.Sleep(time.Second) // let the browser release its files
		_ = os.RemoveAll(profile)
	}()
	// A browser whose proxy is set by an organisation's policy ignores ours
	// and goes straight out: say so when nothing has come through the tunnel.
	go func(name string) {
		time.Sleep(12 * time.Second)
		if wp.Used() == 0 && !webStopped(id) {
			emitter{a}.Emit("web:bypassed", map[string]string{"browser": name, "url": u.String()})
		}
	}(br.Name)
	return WebOpened{Browser: br.Name, Via: wp.Via}, nil
}

// closeWebPages ends every browser window opened through a session (at quit).
func closeWebPages() {
	webMu.Lock()
	var stops []func()
	for _, f := range webRuns {
		stops = append(stops, f)
	}
	webMu.Unlock()
	for _, f := range stops {
		f()
	}
}

// openWithSystem opens a URL or file with the system's default program.
func openWithSystem(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func webStopped(id string) bool {
	webMu.Lock()
	defer webMu.Unlock()
	_, ok := webRuns[id]
	return !ok
}
