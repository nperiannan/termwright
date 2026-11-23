// Package standalone runs the steps of Termwright tests without Termwright: it logs in to the devices of
// an inventory file itself (SSH, Telnet), sends REST requests, SNMP and NETCONF, drives a browser with
// Playwright and parses output with TextFSM templates. The runner program (cmd/twrun) and the exported
// standalone Go suites use it; it depends only on the standard library, golang.org/x/crypto/ssh,
// gopkg.in/yaml.v3 and Termwright's textfsm, netconf, snmp and webrun packages (which are copied with it).
package standalone

import (
	"context"
	"errors"
	"fmt"
	"github.com/termwright/termwright/internal/webrun"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// Device is one device of inventory.yaml: how to reach it, never a password.
type Device struct {
	Session    string `yaml:"session" json:"session"`
	Host       string `yaml:"host,omitempty" json:"host,omitempty"`
	Protocol   string `yaml:"protocol,omitempty" json:"protocol,omitempty"` // ssh (default), telnet or api
	Port       int    `yaml:"port,omitempty" json:"port,omitempty"`
	Username   string `yaml:"username,omitempty" json:"username,omitempty"`
	DeviceType string `yaml:"device_type,omitempty" json:"device_type,omitempty"`
	KeyFile    string `yaml:"key_file,omitempty" json:"key_file,omitempty"`
	// Prompt: a regular expression for the prompt (default: learned from the first prompt).
	Prompt string `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	// PagingOff: commands sent after login (default: the device type's, e.g. disable clipaging).
	PagingOff []string `yaml:"paging_off,omitempty" json:"paging_off,omitempty"`
	BaseURL   string   `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	Auth      string   `yaml:"auth,omitempty" json:"auth,omitempty"` // "" none, basic, bearer, apikey (login/oauth2: a token)
	KeyName   string   `yaml:"key_name,omitempty" json:"key_name,omitempty"`
	Insecure  bool     `yaml:"insecure,omitempty" json:"insecure,omitempty"`
	SNMPVer   string   `yaml:"snmp_version,omitempty" json:"snmp_version,omitempty"`
	SNMPHost  string   `yaml:"snmp_host,omitempty" json:"snmp_host,omitempty"`
	SNMPPort  int      `yaml:"snmp_port,omitempty" json:"snmp_port,omitempty"`
	SNMPUser  string   `yaml:"snmp_user,omitempty" json:"snmp_user,omitempty"`
	SNMPAuth  string   `yaml:"snmp_auth,omitempty" json:"snmp_auth,omitempty"`
	SNMPPriv  string   `yaml:"snmp_priv,omitempty" json:"snmp_priv,omitempty"`
	Jump      string   `yaml:"jump_host,omitempty" json:"jump_host,omitempty"`
	// Errors: regular expressions of the device's error messages ("Invalid input"); a command whose
	// output matches fails its step.
	Errors []string `yaml:"errors,omitempty" json:"errors,omitempty"`
	// NoPassword: the device logs in without a password (Termwright's session or device type says so).
	NoPassword bool `yaml:"no_password,omitempty" json:"no_password,omitempty"`
	// PasswordEnv / TokenEnv / CommunityEnv: other environment variables that hold its secrets.
	PasswordEnv  string `yaml:"password_env,omitempty" json:"password_env,omitempty"`
	TokenEnv     string `yaml:"token_env,omitempty" json:"token_env,omitempty"`
	CommunityEnv string `yaml:"community_env,omitempty" json:"community_env,omitempty"`
	// Stack: an EXOS stack (its ports are slot:port); Slots: its operational slots and their ports, "1:28,2:28".
	Stack bool   `yaml:"stack,omitempty" json:"stack,omitempty"`
	Slots string `yaml:"slots,omitempty" json:"slots,omitempty"`
	// A web app (protocol web): its address, browser settings and recorded login ({{username}} and
	// {{password}} in the values).
	WebURL   string          `yaml:"web_url,omitempty" json:"web_url,omitempty"`
	Browser  string          `yaml:"browser,omitempty" json:"browser,omitempty"`
	Viewport string          `yaml:"viewport,omitempty" json:"viewport,omitempty"`
	WebLogin []webrun.Action `yaml:"web_login,omitempty" json:"web_login,omitempty"`
}

// Engine runs steps on the devices of one inventory. It keeps a login per device until Close.
type Engine struct {
	// Root: the suite folder (templates/, reports/).
	Root string
	devs map[string]Device
	// Ask asks for a secret when none is set (nil: no asking; the step fails with what to set).
	Ask func(label string) (string, error)
	// Keychain looks a secret up in the computer's credential store (twrun: Termwright's saved passwords).
	Keychain func(key string) (string, bool)

	mu     sync.Mutex
	shells map[string]*shell
	asked  map[string]string
}

// ErrNoInventory: there is no inventory.yaml.
var ErrNoInventory = errors.New("no inventory.yaml")

// Open reads root/inventory.yaml (or the file TW_INVENTORY names).
func Open(root string) (*Engine, error) {
	p := os.Getenv("TW_INVENTORY")
	if p == "" {
		p = filepath.Join(root, "inventory.yaml")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("%w (%v): export the suite from Termwright again, or write it", ErrNoInventory, err)
	}
	var doc struct {
		Devices []Device `yaml:"devices"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %v", p, err)
	}
	e := &Engine{Root: root, devs: map[string]Device{}, shells: map[string]*shell{}, asked: map[string]string{}}
	for _, d := range doc.Devices {
		e.devs[d.Session] = d
	}
	return e, nil
}

// Devices lists the inventory.
func (e *Engine) Devices() []Device {
	var out []Device
	for _, d := range e.devs {
		out = append(out, d)
	}
	return out
}

// Device is a device of the inventory by session name.
func (e *Engine) Device(session string) (Device, error) {
	d, ok := e.devs[session]
	if !ok {
		return d, fmt.Errorf("%q is not in inventory.yaml - add it (host, protocol, port, username, device_type)", session)
	}
	return d, nil
}

var nonWord = regexp.MustCompile(`[^A-Za-z0-9]+`)

// EnvName is the environment variable of a session's secret: TW_PASSWORD_ + its last part in capitals.
func EnvName(prefix, session string) string {
	parts := strings.Split(session, "/")
	return prefix + strings.ToUpper(strings.Trim(nonWord.ReplaceAllString(parts[len(parts)-1], "_"), "_"))
}

// Secret is a device's password ("password"), API token ("token") or SNMP community ("community"):
// TW_PASSWORD_<NAME>, the device's *_env variable, TW_PASSWORD (all devices), else asked once.
func (e *Engine) Secret(d Device, kind string) (string, error) {
	pre, own := "TW_PASSWORD_", d.PasswordEnv
	switch kind {
	case "token":
		pre, own = "TW_TOKEN_", d.TokenEnv
	case "community":
		pre, own = "TW_COMMUNITY_", d.CommunityEnv
	}
	if kind == "password" && d.NoPassword {
		return "", nil
	}
	for _, k := range []string{EnvName(pre, d.Session), own, strings.TrimSuffix(pre, "_")} {
		if v, ok := os.LookupEnv(k); k != "" && ok { // set, even empty (a device without a password)
			return v, nil
		}
	}
	if e.Keychain != nil { // Termwright's saved passwords on this computer (service "Termwright")
		keys := map[string][]string{"password": {d.Session}, "token": {"api-secret:" + d.Session}, "community": {"snmp-community:" + d.Session}}[kind]
		if kind == "password" && d.DeviceType != "" { // the device type's password ("no password" counts)
			keys = append(keys, "devicetype:"+strings.ToLower(d.DeviceType))
		}
		for _, key := range keys {
			if v, ok := e.Keychain(key); ok {
				return v, nil
			}
		}
	}
	if kind == "community" {
		return "public", nil
	}
	e.mu.Lock()
	v, ok := e.asked[kind+"\x00"+d.Session]
	e.mu.Unlock()
	if ok {
		return v, nil
	}
	if e.Ask != nil {
		v, err := e.Ask(fmt.Sprintf("%s for %s (%s@%s) - Enter for none", kind, d.Session, d.Username, d.Host))
		if err == nil {
			e.mu.Lock()
			e.asked[kind+"\x00"+d.Session] = v
			e.mu.Unlock()
			return v, nil
		}
	}
	return "", fmt.Errorf("no %s for %s: set %s (or %s for every device)", kind, d.Session, EnvName(pre, d.Session), strings.TrimSuffix(pre, "_"))
}

// AskTerminal asks on the terminal without showing what is typed.
func AskTerminal(label string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("not a terminal")
	}
	fmt.Fprintf(os.Stderr, "%s: ", label)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	return strings.TrimRight(string(b), "\r\n"), err
}

// Close logs out of every device.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, s := range e.shells {
		s.close()
		delete(e.shells, k)
	}
}

// Command sends a command on a device's login and returns its output (no echo, no prompt).
func (e *Engine) Command(ctx context.Context, session, cmd, prompt string, timeout time.Duration) (string, error) {
	sh, err := e.shell(ctx, session)
	if err != nil {
		return "", err
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	out, err := sh.command(ctx, cmd, prompt, timeout)
	if err != nil { // a broken login is made again for the next step
		e.mu.Lock()
		if e.shells[session] == sh {
			sh.close()
			delete(e.shells, session)
		}
		e.mu.Unlock()
	}
	return out, err
}

func (e *Engine) shell(ctx context.Context, session string) (*shell, error) {
	e.mu.Lock()
	sh := e.shells[session]
	e.mu.Unlock()
	if sh != nil {
		return sh, nil
	}
	d, err := e.Device(session)
	if err != nil {
		return nil, err
	}
	sh, err = e.login(ctx, d)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.shells[session] = sh
	e.mu.Unlock()
	return sh, nil
}

// DeviceError is the line of out where the device refused a command ("" = none).
func (e *Engine) DeviceError(session, out string) string {
	d, ok := e.devs[session]
	if !ok {
		return ""
	}
	for _, p := range d.Errors {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		if loc := re.FindStringIndex(out); loc != nil {
			start := strings.LastIndex(out[:loc[0]], "\n") + 1
			end := strings.Index(out[loc[0]:], "\n")
			if end < 0 {
				end = len(out) - loc[0]
			}
			return strings.TrimSpace(out[start : loc[0]+end])
		}
	}
	return ""
}
