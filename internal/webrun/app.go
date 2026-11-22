package webrun

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// App is a web app: a saved session with its address, browser settings and recorded login.
type App struct {
	Name     string // the session ("Apps/CRM staging"): its login is kept under this name
	URL      string
	Browser  string
	Viewport string
	Insecure bool
	User     string
	Password string
	Login    []Action // recorded login ({{username}} / {{password}} in the values)
}

// ParseLogin reads a recorded login saved with a session (JSON actions).
func ParseLogin(s string) []Action {
	var l []Action
	if strings.TrimSpace(s) != "" {
		_ = json.Unmarshal([]byte(s), &l)
	}
	return l
}

var scheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// Join puts a path after the app's address: "" is the address, "/contacts" or "contacts" go after it,
// a full address stays as it is.
func Join(base, p string) string {
	if p == "" {
		return base
	}
	if scheme.MatchString(p) || base == "" || strings.Contains(p, "{{") && !strings.HasPrefix(p, "/") {
		return p
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(p, "/")
}

// ForApp applies a web app to a step: its address goes before the step's paths (and goto actions'), its
// browser settings are the defaults, its login is kept between steps (State, unless the step names one)
// and done by itself when the app asks for it, and {{username}} / {{password}} are its login.
func ForApp(w Step, app App, x Extra) (Step, Extra) {
	w.URL = Join(app.URL, w.URL)
	if w.Browser == "" {
		w.Browser = app.Browser
	}
	if w.Viewport == "" {
		w.Viewport = app.Viewport
	}
	w.Insecure = w.Insecure || app.Insecure
	if w.State == "" && app.Name != "" {
		w.State = "app-" + app.Name
	}
	acts := make([]Action, len(w.Actions))
	for i, a := range w.Actions {
		if strings.EqualFold(a.Do, "goto") && a.Value != "" {
			a.Value = Join(app.URL, a.Value)
		}
		acts[i] = a
	}
	w.Actions = acts
	w = WithLogin(w, app.User, app.Password)
	if len(app.Login) > 0 {
		x.Login = WithLogin(Step{Actions: app.Login}, app.User, app.Password).Actions
	}
	if x.HTTPUser == "" {
		x.HTTPUser, x.HTTPPassword = app.User, app.Password
	}
	return w, x
}

var stateName = regexp.MustCompile(`[^\w.-]+`)

// StatePath is the file a kept login (State) is saved in.
func StatePath(dir, state string) string {
	return filepath.Join(dir, stateName.ReplaceAllString(state, "_")+".json")
}

// Cookies is the Cookie header for an address from a kept login (the browser's cookies of its host).
func Cookies(statePath, address string) string {
	b, err := os.ReadFile(statePath)
	if err != nil {
		return ""
	}
	var st struct {
		Cookies []struct{ Name, Value, Domain, Path string } `json:"cookies"`
	}
	if json.Unmarshal(b, &st) != nil {
		return ""
	}
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	var parts []string
	for _, c := range st.Cookies {
		d := strings.TrimPrefix(c.Domain, ".")
		if host != d && !strings.HasSuffix(host, "."+d) {
			continue
		}
		if c.Path != "" && !strings.HasPrefix(u.Path+"/", strings.TrimSuffix(c.Path, "/")+"/") && c.Path != "/" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}
