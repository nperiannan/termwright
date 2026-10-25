// Package secretref reads passwords from password managers at connect time,
// so they never have to be copied into Termwright:
//
//	op://Vault/Item/field          1Password (CLI "op", desktop-app or service-account sign-in)
//	bw://<item name or id>         Bitwarden (CLI "bw"; asks for the master password once to unlock)
//	bw://<item>#username           a Bitwarden field other than the password
//	vault://<mount>/<path>#field   HashiCorp Vault KV v2 or v1 (HTTP API, VAULT_ADDR + token)
//	cmd://<shell command>          any command that prints the password
package secretref

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Asker asks the user for a secret (e.g. the Bitwarden master password).
type Asker func(title, message string) (string, error)

// Options configure a resolver.
type Options struct {
	VaultAddr  string // "" = $VAULT_ADDR
	VaultToken func() string
	CacheFor   time.Duration // how long a fetched secret is reused (default 10 minutes)
}

// Resolver fetches and caches secrets.
type Resolver struct {
	Opt   Options
	mu    sync.Mutex
	cache map[string]cached
	bwKey string // BW_SESSION after an unlock
}

type cached struct {
	v  string
	at time.Time
}

// Kinds lists the supported prefixes.
var Kinds = []string{"op://", "bw://", "vault://", "cmd://"}

// IsRef reports whether s is a password-manager reference.
func IsRef(s string) bool {
	for _, k := range Kinds {
		if strings.HasPrefix(s, k) {
			return true
		}
	}
	return false
}

// Forget clears the cache (and the Bitwarden unlock).
func (r *Resolver) Forget() {
	r.mu.Lock()
	r.cache, r.bwKey = nil, ""
	r.mu.Unlock()
}

// Resolve returns the secret a reference points to.
func (r *Resolver) Resolve(ctx context.Context, ref string, ask Asker) (string, error) {
	ref = strings.TrimSpace(ref)
	ttl := r.Opt.CacheFor
	if ttl == 0 {
		ttl = 10 * time.Minute
	}
	r.mu.Lock()
	if c, ok := r.cache[ref]; ok && time.Since(c.at) < ttl {
		r.mu.Unlock()
		return c.v, nil
	}
	r.mu.Unlock()
	var v string
	var err error
	switch {
	case strings.HasPrefix(ref, "op://"):
		v, err = run(ctx, nil, "", "op", "read", "--no-newline", ref)
	case strings.HasPrefix(ref, "bw://"):
		v, err = r.bitwarden(ctx, strings.TrimPrefix(ref, "bw://"), ask)
	case strings.HasPrefix(ref, "vault://"):
		v, err = r.vault(ctx, strings.TrimPrefix(ref, "vault://"))
	case strings.HasPrefix(ref, "cmd://"):
		v, err = shellCmd(ctx, strings.TrimPrefix(ref, "cmd://"))
	default:
		return "", fmt.Errorf("not a password-manager reference: %q", ref)
	}
	if err != nil {
		return "", err
	}
	v = strings.TrimRight(v, "\r\n")
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[string]cached{}
	}
	r.cache[ref] = cached{v, time.Now()}
	r.mu.Unlock()
	return v, nil
}

// --- Bitwarden ---

func (r *Resolver) bitwarden(ctx context.Context, spec string, ask Asker) (string, error) {
	item, field, _ := strings.Cut(spec, "#")
	if field == "" {
		field = "password"
	}
	get := func() (string, error) {
		r.mu.Lock()
		key := r.bwKey
		r.mu.Unlock()
		var env []string
		if key != "" {
			env = []string{"BW_SESSION=" + key}
		}
		return run(ctx, env, "", "bw", "get", field, item, "--nointeraction")
	}
	v, err := get()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "locked") || ask == nil {
		return v, err
	}
	pw, aerr := ask("Bitwarden", "Bitwarden is locked. Master password to unlock it for this Termwright session:")
	if aerr != nil {
		return "", aerr
	}
	key, err := run(ctx, []string{"TW_BW_PASSWORD=" + pw}, "", "bw", "unlock", "--raw", "--passwordenv", "TW_BW_PASSWORD", "--nointeraction")
	if err != nil {
		return "", fmt.Errorf("Bitwarden unlock: %w", err)
	}
	r.mu.Lock()
	r.bwKey = strings.TrimSpace(key)
	r.mu.Unlock()
	return get()
}

// --- HashiCorp Vault (HTTP API) ---

func (r *Resolver) vault(ctx context.Context, spec string) (string, error) {
	path, field, _ := strings.Cut(spec, "#")
	if field == "" {
		field = "password"
	}
	path = strings.Trim(path, "/")
	mount, rest, ok := strings.Cut(path, "/")
	if !ok || rest == "" {
		return "", errors.New("vault reference must look like vault://<mount>/<path>#field")
	}
	addr := strings.TrimRight(firstNonEmpty(r.Opt.VaultAddr, os.Getenv("VAULT_ADDR")), "/")
	if addr == "" {
		return "", errors.New("Vault address unknown: set VAULT_ADDR or Global Options → Password Managers")
	}
	token := os.Getenv("VAULT_TOKEN")
	if token == "" && r.Opt.VaultToken != nil {
		token = r.Opt.VaultToken()
	}
	if token == "" {
		if h, err := os.UserHomeDir(); err == nil {
			if b, err := os.ReadFile(filepath.Join(h, ".vault-token")); err == nil {
				token = strings.TrimSpace(string(b))
			}
		}
	}
	if token == "" {
		return "", errors.New("no Vault token: run 'vault login', set VAULT_TOKEN, or save a token in Global Options → Password Managers")
	}
	fetch := func(url string, v2 bool) (string, int, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		req.Header.Set("X-Vault-Token", token)
		if ns := os.Getenv("VAULT_NAMESPACE"); ns != "" {
			req.Header.Set("X-Vault-Namespace", ns)
		}
		resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
		if err != nil {
			return "", 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", resp.StatusCode, fmt.Errorf("Vault %s: HTTP %d", url, resp.StatusCode)
		}
		var body struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return "", resp.StatusCode, err
		}
		data := body.Data
		if v2 {
			inner, _ := data["data"].(map[string]any)
			data = inner
		}
		val, ok := data[field]
		if !ok {
			return "", resp.StatusCode, fmt.Errorf("Vault secret %s has no field %q", path, field)
		}
		return fmt.Sprint(val), resp.StatusCode, nil
	}
	v, code, err := fetch(fmt.Sprintf("%s/v1/%s/data/%s", addr, mount, rest), true)
	if err == nil {
		return v, nil
	}
	if code == http.StatusNotFound {
		if v, _, err2 := fetch(fmt.Sprintf("%s/v1/%s/%s", addr, mount, rest), false); err2 == nil {
			return v, nil
		}
	}
	return "", err
}

// --- helpers ---

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

// LookPath finds a CLI also in the usual install folders, which are often
// missing from the PATH of apps started from the Dock / Start menu.
func LookPath(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	var dirs []string
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		dirs = []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links"),
			filepath.Join(os.Getenv("ProgramFiles"), "1Password CLI"), filepath.Join(os.Getenv("APPDATA"), "npm"),
			filepath.Join(home, "scoop", "shims")}
		name += ".exe"
	default:
		dirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/snap/bin", filepath.Join(home, ".local", "bin"),
			filepath.Join(home, "bin")}
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found - install its command-line tool", strings.TrimSuffix(name, ".exe"))
}

func run(ctx context.Context, env []string, stdin string, name string, args ...string) (string, error) {
	p, err := LookPath(name)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute) // biometric / touch prompts may take a while
	defer cancel()
	cmd := exec.CommandContext(ctx, p, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s: %s", name, firstLine(msg))
	}
	return out.String(), nil
}

func shellCmd(ctx context.Context, command string) (string, error) {
	if runtime.GOOS == "windows" {
		return run(ctx, nil, "", "cmd", "/C", command)
	}
	return run(ctx, nil, "", "sh", "-c", command)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
