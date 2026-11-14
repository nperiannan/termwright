package session

import "strings"

// DeviceType is a reusable login profile for a kind of device (EXOS, VOSS,
// Cisco IOS, Linux …). A session or a Quick Connect picks one by name, so a
// connection needs only the address and the device type.
type DeviceType struct {
	Name string `json:"name"`
	// Icon is the device picture for its sessions ("phone", "iot", "tv"…); "" = from the name.
	Icon     string `json:"icon,omitempty"`
	Protocol string `json:"protocol"` // default protocol for Quick Connect ("" = SSH2)
	Port     int    `json:"port"`     // default port (0 = protocol default)
	Username string `json:"username"` // login name; sessions inherit it unless they set OwnLogin
	// Credential names a saved credential for \s and \w in the steps.
	Credential string `json:"credential"`
	InitialCR  bool   `json:"initialCr"`
	// LoginSteps answer username/password prompts; they run for Telnet,
	// Serial and Raw only (SSH logs in before the shell starts).
	LoginSteps []LogonAction `json:"loginSteps"`
	// AfterLogin steps run for every protocol once logged in, e.g. "enable",
	// "terminal more disable".
	AfterLogin []LogonAction `json:"afterLogin"`
	// Prompt is a regular expression for the device's command prompt (used to
	// pace multi-line sends); optional.
	Prompt string `json:"prompt"`
	// BackupCommands print the configuration for Configuration Backups, one
	// command per line (e.g. "show running-config"); empty = the built-in
	// default for the device type's name, if any.
	BackupCommands string `json:"backupCommands"`
	// The device's web server, for Open Web Interface (each empty = the built-in one for the type's name):
	// WebCheck prints its state, WebHTTP and WebHTTPS match that output while each is on, WebOn turns both
	// on and WebSave keeps it after a restart (commands one per line; {name} is the session's name).
	WebCheck string `json:"webCheck,omitempty"`
	WebHTTP  string `json:"webHttp,omitempty"`
	WebHTTPS string `json:"webHttps,omitempty"`
	WebOn    string `json:"webOn,omitempty"`
	WebSave  string `json:"webSave,omitempty"`
	// AIReadOnly: commands the AI agent may run without asking (beginnings,
	// one per line, e.g. "show"); empty = the built-in list (show, display,
	// ping, traceroute… - or the usual read-only commands for Linux).
	AIReadOnly string `json:"aiReadOnly,omitempty"`
	// Facts are values read from command output after connecting (model,
	// version, serial, MAC, uptime, or any the user adds).
	Facts []FactRule `json:"facts"`
}

// FactRule reads one value (a device fact) from the output of a command:
// the first group of Regex. Every = read at each connect (version, uptime);
// otherwise once per session (model, serial, MAC).
type FactRule struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Regex   string `json:"regex"`
	Every   bool   `json:"every"`
}

func fact(name, cmd, re string, every bool) FactRule {
	return FactRule{Name: name, Command: cmd, Regex: re, Every: every}
}

// BuiltinFacts are the fact rules of the built-in device types (by name).
func BuiltinFacts(name string) []FactRule {
	mac := `([0-9a-fA-F]{2}(?:[:.-]?[0-9a-fA-F]{2}){5})`
	switch strings.ToUpper(name) {
	case "VOSS":
		return []FactRule{
			fact("Hostname", "show sys-info", `SysName\s*:\s*(\S+)`, false),
			fact("Model", "show sys-info", `ModelName\s*:\s*(\S+)`, false),
			fact("Version", "show sys-info", `SysDescr\s*:\s*\S+\s*\(([^)]+)\)`, true),
			fact("Serial", "show sys-info", `Serial#\s*:\s*(\S+)`, false),
			fact("MAC", "show sys-info", `BaseMacAddr\s*:\s*`+mac, false),
			fact("Uptime", "show sys-info", `SysUpTime\s*:\s*(.+?)\s*$`, true),
		}
	case "EXOS":
		return []FactRule{
			fact("Hostname", "show switch", `SysName:\s*(\S+)`, false),
			fact("Model", "show switch", `System Type:\s*(.+?)\s*$`, false),
			fact("Version", "show switch", `Primary ver:\s*(\S+)`, true),
			fact("Serial", "show version", `^Switch\s*:\s*\S+\s+(\S+)`, false),
			fact("MAC", "show switch", `System MAC:\s*`+mac, false),
			fact("Uptime", "show switch", `System UpTime:\s*(.+?)\s*$`, true),
		}
	case "CISCO IOS":
		return []FactRule{
			fact("Hostname", "show version", `^(\S+) uptime is`, false),
			fact("Model", "show version", `^[Cc]isco (\S+) \(`, false),
			fact("Version", "show version", `Cisco IOS.*Version ([^,\s]+)`, true),
			fact("Serial", "show version", `Processor board ID (\S+)`, false),
			fact("MAC", "show version", `Base [Ee]thernet MAC [Aa]ddress\s*:\s*`+mac, false),
			fact("Uptime", "show version", `uptime is (.+?)\s*$`, true),
		}
	case "LINUX":
		return []FactRule{
			fact("Hostname", "hostname -s", `^([A-Za-z0-9][\w.-]*)\s*$`, false),
			fact("Version", "cat /etc/os-release", `PRETTY_NAME="?([^"\n]+?)"?\s*$`, true),
			fact("Kernel", "uname -r", `^(\d+\.\d+\S*)\s*$`, true),
			fact("Uptime", "uptime -p", `^up (.+?)\s*$`, true),
		}
	}
	return nil
}

// Backup returns the commands that print the configuration.
func (d DeviceType) Backup() []string {
	src := d.BackupCommands
	if strings.TrimSpace(src) == "" {
		for _, b := range BuiltinDeviceTypes() {
			if strings.EqualFold(b.Name, d.Name) {
				src = b.BackupCommands
			}
		}
	}
	var out []string
	for _, l := range strings.Split(src, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Web is how to see and turn on the device's web server: the commands that print its state, the patterns that
// match while HTTP and HTTPS are on, the commands that turn both on and the ones that save it. ok is false when
// the type has none.
func (d DeviceType) Web() (check []string, http, https string, on, save []string, ok bool) {
	pick := func(own string, f func(DeviceType) string) string {
		if strings.TrimSpace(own) != "" {
			return own
		}
		for _, b := range BuiltinDeviceTypes() {
			if strings.EqualFold(b.Name, d.Name) {
				return f(b)
			}
		}
		return ""
	}
	lines := func(src string) []string {
		var out []string
		for _, l := range strings.Split(src, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				out = append(out, l)
			}
		}
		return out
	}
	check = lines(pick(d.WebCheck, func(b DeviceType) string { return b.WebCheck }))
	http, https = pick(d.WebHTTP, func(b DeviceType) string { return b.WebHTTP }), pick(d.WebHTTPS, func(b DeviceType) string { return b.WebHTTPS })
	on = lines(pick(d.WebOn, func(b DeviceType) string { return b.WebOn }))
	save = lines(pick(d.WebSave, func(b DeviceType) string { return b.WebSave }))
	return check, http, https, on, save, len(check) > 0 && http != "" && https != "" && len(on) > 0
}

// Steps returns the logon actions for a connection over protocol.
func (d DeviceType) Steps(protocol string) []LogonAction {
	return append(d.LoginOnly(protocol), d.fill(d.AfterLogin)...)
}

// LoginOnly returns the login steps for protocol (none for SSH2), without
// the after-login steps.
func (d DeviceType) LoginOnly(protocol string) []LogonAction {
	if protocol == ProtoSSH2 {
		return []LogonAction{}
	}
	return d.fill(d.LoginSteps)
}

// After returns the after-login steps.
func (d DeviceType) After() []LogonAction { return d.fill(d.AfterLogin) }

// fill sets the device type's credential on steps that send \s or \w.
func (d DeviceType) fill(all []LogonAction) []LogonAction {
	res := make([]LogonAction, 0, len(all))
	for _, a := range all {
		if a.Credential == "" && d.Credential != "" && (strings.Contains(a.Send, `\s`) || strings.Contains(a.Send, `\w`)) {
			a.Credential = d.Credential
		}
		res = append(res, a)
	}
	return res
}

func step(expect, send string, hide bool) LogonAction {
	return LogonAction{Expect: expect, Send: send, Hide: hide, TrailingCR: true}
}

// BuiltinDeviceTypes are the device types a new installation starts with;
// users can edit, remove or add their own in Global Options → Device Types.
func BuiltinDeviceTypes() []DeviceType {
	list := []DeviceType{
		{Name: "EXOS", Protocol: ProtoSSH2, Prompt: `[#>] ?$`, BackupCommands: "show configuration",
			LoginSteps: []LogonAction{step("login:", `\s`, false), step("password:", `\w`, true)},
			AfterLogin: []LogonAction{step("#", "disable clipaging", false)},
			// "Web access : Enabled (tcp port 80)", and "Enabled (SSL port 443)" once HTTPS is on (it needs a certificate)
			WebCheck: "show management", WebHTTP: `(?i)web access\s*:\s*enabled\s*\(tcp`, WebHTTPS: `(?i)enabled\s*\(\s*ssl\s+port`,
			WebOn:   "enable web http\nconfigure ssl certificate privkeylen 2048 country US organization Lab common-name {name}\nenable web https",
			WebSave: "save configuration"},
		{Name: "VOSS", Protocol: ProtoSSH2, Prompt: `:\d+(\([^)]*\))?[>#] ?$`, BackupCommands: "show running-config",
			LoginSteps: []LogonAction{step("Login:", `\s`, false), step("Password:", `\w`, true)},
			AfterLogin: []LogonAction{step(">", "enable", false), step("#", "terminal more disable", false)},
			// the web server serves HTTPS when it is on; "secure-only" turns HTTP off
			WebCheck: "show web-server", WebHTTP: `(?is)status\s*:\s*enabled.*secure[ -]?only\s*:\s*disabled`, WebHTTPS: `(?i)status\s*:\s*enabled`,
			WebOn:   "configure terminal\nweb-server enable\nno web-server secure-only\nend",
			WebSave: "save config"},
		{Name: "Cisco IOS", Protocol: ProtoSSH2, Prompt: `[>#] ?$`, BackupCommands: "show running-config",
			LoginSteps: []LogonAction{step("Username:", `\s`, false), step("Password:", `\w`, true)},
			AfterLogin: []LogonAction{step(">", "enable", false), step("#", "terminal length 0", false)}},
		{Name: "Linux", Protocol: ProtoSSH2, Prompt: `[$#] ?$`,
			LoginSteps: []LogonAction{step("login:", `\s`, false), step("Password:", `\w`, true)}},
	}
	for i := range list {
		list[i].Facts = BuiltinFacts(list[i].Name)
	}
	return list
}
