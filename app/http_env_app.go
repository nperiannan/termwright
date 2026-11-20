package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/termwright/termwright/internal/apiclient"
	"github.com/termwright/termwright/internal/secrets"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) httpEnvStore() apiclient.EnvStore {
	return apiclient.EnvStore{Dir: filepath.Join(a.cfg.DataDir(), "api", "environments")}
}

func httpEnvKey(id string) string { return "api-env:" + id }

func httpEnvSecrets(id string) map[string]string {
	m := map[string]string{}
	if v, ok := secrets.Lookup(httpEnvKey(id)); ok {
		_ = json.Unmarshal([]byte(v), &m)
	}
	return m
}

// HTTPEnvVar is an environment's variable as the app shows it: a secret's value is never sent, only
// whether it has one.
type HTTPEnvVar struct {
	apiclient.EnvVar
	// HasValue: a secret variable has a value in the credential store.
	HasValue bool `json:"hasValue,omitempty"`
}

// HTTPEnv is an environment as the app shows it.
type HTTPEnv struct {
	// ID: its id ("globals" for the global variables).
	ID string `json:"id"`
	// Name shown.
	Name string `json:"name"`
	// Vars: its variables (secret values left out).
	Vars []HTTPEnvVar `json:"vars"`
}

func httpEnvView(e apiclient.Environment) HTTPEnv {
	sec := httpEnvSecrets(e.ID)
	v := HTTPEnv{ID: e.ID, Name: e.Name, Vars: []HTTPEnvVar{}}
	for _, x := range e.Vars {
		hv := HTTPEnvVar{EnvVar: x}
		if x.Secret {
			hv.Value, hv.HasValue = "", sec[x.Key] != ""
		}
		v.Vars = append(v.Vars, hv)
	}
	return v
}

// HTTPEnvironments lists the API environments (Postman's environments) by name, the globals first.
func (a *App) HTTPEnvironments() []HTTPEnv {
	st := a.httpEnvStore()
	g, _ := st.Get(apiclient.GlobalsID)
	out := []HTTPEnv{httpEnvView(g)}
	for _, e := range st.List() {
		out = append(out, httpEnvView(e))
	}
	return out
}

// HTTPSaveEnvironment saves an environment; secret values go to the credential store (an empty secret
// value keeps the saved one).
func (a *App) HTTPSaveEnvironment(e HTTPEnv) (HTTPEnv, error) {
	env := apiclient.Environment{ID: e.ID, Name: e.Name}
	typed := map[string]string{}
	for _, v := range e.Vars {
		if strings.TrimSpace(v.Key) == "" {
			continue
		}
		if v.Secret && v.Value != "" {
			typed[v.Key] = v.Value
		}
		env.Vars = append(env.Vars, v.EnvVar)
	}
	saved, err := a.httpEnvStore().Save(env)
	if err != nil {
		return HTTPEnv{}, err
	}
	old := httpEnvSecrets(saved.ID)
	keep := map[string]string{}
	for _, v := range saved.Vars {
		if !v.Secret {
			continue
		}
		if t, ok := typed[v.Key]; ok {
			keep[v.Key] = t
		} else if o := old[v.Key]; o != "" {
			keep[v.Key] = o
		}
	}
	if len(keep) == 0 {
		_ = secrets.Delete(httpEnvKey(saved.ID))
	} else {
		b, _ := json.Marshal(keep)
		if err := secrets.Set(httpEnvKey(saved.ID), string(b)); err != nil {
			return HTTPEnv{}, fmt.Errorf("credential store: %w", err)
		}
	}
	return httpEnvView(saved), nil
}

// HTTPDeleteEnvironment deletes an environment (the globals are emptied).
func (a *App) HTTPDeleteEnvironment(id string) error {
	if err := a.httpEnvStore().Delete(id); err != nil {
		return err
	}
	_ = secrets.Delete(httpEnvKey(id))
	return nil
}

// HTTPImportEnvironment imports a Postman environment (or globals) export; secret values go to the
// credential store.
func (a *App) HTTPImportEnvironment(text, name string) (HTTPEnv, error) {
	e, err := apiclient.ImportEnvironment(text, name)
	if err != nil {
		return HTTPEnv{}, err
	}
	in := HTTPEnv{ID: e.ID, Name: e.Name}
	for _, v := range e.Vars {
		in.Vars = append(in.Vars, HTTPEnvVar{EnvVar: v})
	}
	return a.HTTPSaveEnvironment(in)
}

// HTTPImportEnvironmentFile asks for a Postman environment file and imports it.
func (a *App) HTTPImportEnvironmentFile() (HTTPEnv, error) {
	p, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Import Postman Environment",
		Filters: []wruntime.FileFilter{{DisplayName: "Postman environment (*.json)", Pattern: "*.json"}}})
	if err != nil || p == "" {
		return HTTPEnv{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return HTTPEnv{}, err
	}
	return a.HTTPImportEnvironment(string(b), "")
}

// HTTPExportEnvironmentFile writes an environment as a Postman environment (secret values left empty).
func (a *App) HTTPExportEnvironmentFile(id string) (string, error) {
	e, err := a.httpEnvStore().Get(id)
	if err != nil {
		return "", err
	}
	p, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "Export Environment", DefaultFilename: e.Name + ".postman_environment.json", CanCreateDirectories: true})
	if err != nil || p == "" {
		return "", err
	}
	return p, os.WriteFile(p, apiclient.ExportEnvironment(e), 0o600)
}

// httpEnvVars adds an environment's variables (secrets from the credential store) to a target.
func (a *App) httpEnvVars(idOrName string, t *apiclient.Target) error {
	if a.cfg == nil {
		return nil
	}
	e, err := a.httpEnvStore().Find(idOrName)
	if err != nil {
		return err
	}
	sec := httpEnvSecrets(e.ID)
	for _, v := range e.Vars {
		if v.Off {
			continue
		}
		val := v.Value
		if v.Secret {
			val = sec[v.Key]
			if val != "" {
				t.Secrets = append(t.Secrets, val)
			}
		}
		t.Vars[v.Key] = val
	}
	return nil
}

func (a *App) httpEnvName(idOrName string) string {
	if idOrName == "" || a.cfg == nil {
		return ""
	}
	if e, err := a.httpEnvStore().Find(idOrName); err == nil {
		return e.Name
	}
	return ""
}

// httpKeepSet keeps what scripts set with pm.environment.set (in the chosen environment) and
// pm.globals.set, as Postman does.
func (a *App) httpKeepSet(env string, res apiclient.Response) {
	if a.cfg == nil {
		return
	}
	save := func(idOrName string, set map[string]string) {
		if len(set) == 0 || idOrName == "" {
			return
		}
		st := a.httpEnvStore()
		e, err := st.Find(idOrName)
		if err != nil {
			return
		}
		view := httpEnvView(e)
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			found := false
			for i := range view.Vars {
				if view.Vars[i].Key == k {
					found = true
					view.Vars[i].Value = set[k]
				}
			}
			if !found {
				view.Vars = append(view.Vars, HTTPEnvVar{EnvVar: apiclient.EnvVar{Key: k, Value: set[k]}})
			}
		}
		_, _ = a.HTTPSaveEnvironment(view)
	}
	save(env, res.EnvSet)
	save(apiclient.GlobalsID, res.GlobalSet)
}

// httpScriptGuard stops pm.sendRequest from changing a production API without a confirmation.
func httpScriptGuard(prod, confirmed bool) func(string) error {
	return func(m string) error {
		if prod && !confirmed && apiclient.Changes(strings.ToUpper(m)) {
			return errors.New("pm.sendRequest: a change (" + m + ") on a Production session needs a confirmation; send the request again and confirm")
		}
		return nil
	}
}

// HTTPVar is a variable a request can use, and where its value comes from.
type HTTPVar struct {
	Name string `json:"name"`
	// Value: "" for a secret (see Secret).
	Value string `json:"value"`
	// Source: globals, collection, session, environment or local (set by a script in this tab).
	Source string `json:"source"`
	// Secret: its value is hidden.
	Secret bool `json:"secret,omitempty"`
}

// HTTPVariables lists every variable a request can use (the winning value of each name), for the
// Variables panel and the URL's colours. Secret values are not sent.
func (a *App) HTTPVariables(r HTTPSendRequest) ([]HTTPVar, error) {
	src := map[string]string{}
	add := func(source string, vars map[string]string) {
		for k := range vars {
			src[k] = source
		}
	}
	step := func(fill func(t *apiclient.Target) error) (*apiclient.Target, error) {
		t := &apiclient.Target{Vars: map[string]string{}}
		return t, fill(t)
	}
	if g, err := step(func(t *apiclient.Target) error { return a.httpEnvVars(apiclient.GlobalsID, t) }); err == nil {
		add("globals", g.Vars)
	}
	if r.Collection != "" {
		if c, err := a.httpStore().Find(r.Collection); err == nil {
			m := map[string]string{}
			for _, v := range c.Vars {
				if !v.Off {
					m[v.Key] = v.Value
				}
			}
			add("collection", m)
		}
	}
	if r.Session != "" {
		if a.store != nil {
			if s, err := a.store.Get(r.Session); err == nil {
				m := map[string]string{}
				if s.APIBaseURL != "" {
					m["baseUrl"] = ""
				}
				for _, v := range s.APIVars {
					m[v.Name] = ""
				}
				add("session", m)
			}
		}
	}
	if r.Environment != "" {
		if e, err := step(func(t *apiclient.Target) error { return a.httpEnvVars(r.Environment, t) }); err == nil {
			add("environment", e.Vars)
		}
	}
	add("local", r.Vars)
	t, _, _, err := a.httpTarget(r)
	if err != nil {
		return nil, err
	}
	secret := map[string]bool{}
	for k, v := range t.Vars {
		for _, s := range t.Secrets {
			if v != "" && v == s {
				secret[k] = true
			}
		}
	}
	out := make([]HTTPVar, 0, len(t.Vars))
	for k, v := range t.Vars {
		hv := HTTPVar{Name: k, Value: v, Source: src[k], Secret: secret[k]}
		if hv.Source == "" {
			hv.Source = "session"
		}
		if hv.Secret {
			hv.Value = ""
		}
		out = append(out, hv)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// HTTPCookies lists the cookies kept for an API session ("" for requests sent without one), by host.
func (a *App) HTTPCookies(session string) []apiclient.HostCookies {
	httpJarsMu.Lock()
	j, _ := httpJars[session].(*apiclient.Jar)
	httpJarsMu.Unlock()
	if j == nil {
		return []apiclient.HostCookies{}
	}
	l := j.List()
	if l == nil {
		l = []apiclient.HostCookies{}
	}
	return l
}

// HTTPDeleteCookie removes a cookie of a host ("" name: all of the host's; "" host: every cookie).
func (a *App) HTTPDeleteCookie(session, host, name string) {
	httpJarsMu.Lock()
	j, _ := httpJars[session].(*apiclient.Jar)
	httpJarsMu.Unlock()
	if j == nil {
		return
	}
	if host == "" {
		j.Clear()
		return
	}
	j.Remove(host, name)
}

// HTTPSaveResponse asks where to save an answer's body and writes it (base64 bodies are decoded).
func (a *App) HTTPSaveResponse(body string, isBase64 bool, name string) (string, error) {
	data := []byte(body)
	if isBase64 {
		b, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return "", err
		}
		data = b
	}
	p, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "Save Response", DefaultFilename: firstNonEmptyStr(name, "response"), CanCreateDirectories: true})
	if err != nil || p == "" {
		return "", err
	}
	return p, os.WriteFile(p, data, 0o600)
}

func firstNonEmptyStr(l ...string) string {
	for _, s := range l {
		if s != "" {
			return s
		}
	}
	return ""
}
