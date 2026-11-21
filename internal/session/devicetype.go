package session

import (
	"regexp"
	"strings"

	"github.com/termwright/termwright/internal/topo"
)

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
	// UndoRules: how the test recorder suggests the cleanup of a recorded command, one rule per line
	// "regular expression => undo" ($1… are the groups, $0 the whole command; an empty undo means
	// the command needs none); the first rule that matches wins. Empty = the built-in rules for the
	// type's name.
	UndoRules string `json:"undoRules,omitempty"`
	// TestSave and TestRestore: commands that save the configuration before a test and put it back
	// after it (a test with restore: [config]), one per line; {file} is a name for the saved copy.
	// Empty = the built-in ones for the type's name, if any.
	TestSave    string `json:"testSave,omitempty"`
	TestRestore string `json:"testRestore,omitempty"`
	// ErrorPatterns: regular expressions (one per line) for the device's error messages; a test step
	// whose output has a line that matches one fails (the device refused the command). Empty = the
	// built-in ones for the type's name.
	ErrorPatterns string `json:"errorPatterns,omitempty"`
	// FactoryReset: commands that reset the device to factory default (a run's "on a failed cleanup:
	// factory default"), one per line; the device may restart. Empty = the built-in ones, if any.
	FactoryReset string `json:"factoryReset,omitempty"`
	// Upgrade: commands that install a software image for a test's upgrade step, one per line;
	// {image} is the image (a file name or URL). The device may restart; Termwright waits for it.
	Upgrade string `json:"upgrade,omitempty"`
	// Map tells the network map how to read a device of this type with TextFSM templates (any vendor);
	// nil or without a neighbour command = the built-in reading for the type's name, if any.
	Map *topo.Custom `json:"map,omitempty"`
	// The device's web server, for Open Web Interface (each empty = the built-in one for the type's name):
	// WebCheck prints its state, WebHTTP and WebHTTPS match that output while each is on, WebOn turns both
	// on and WebSave keeps it after a restart (commands one per line; {name} is the session's name).
	WebCheck string `json:"webCheck,omitempty"`
	WebHTTP  string `json:"webHttp,omitempty"`
	WebHTTPS string `json:"webHttps,omitempty"`
	WebOn    string `json:"webOn,omitempty"`
	WebSave  string `json:"webSave,omitempty"`
	// Tunnel: whether its SSH server passes connections on (TCP forwarding), which Open Web Page Through This
	// Session needs: "off", "on", or "" = as the built-in type of that name says (EXOS, VOSS and Cisco IOS: off).
	Tunnel string `json:"tunnel,omitempty"`
	// AIReadOnly: commands the AI agent may run without asking (beginnings,
	// one per line, e.g. "show"); empty = the built-in list (show, display,
	// ping, traceroute… - or the usual read-only commands for Linux).
	AIReadOnly string `json:"aiReadOnly,omitempty"`
	// Detect: a regular expression that recognises this kind of device in what it shows of itself:
	// its SSH or Telnet banner, SNMP system description and web page when Discover Devices scans,
	// and the output of DetectCommand after logging in (Identify Device Type). Empty = never chosen
	// by itself.
	Detect string `json:"detect,omitempty"`
	// DetectCommand: the command whose output Detect is matched against after logging in, e.g.
	// "show version" (empty: show version).
	DetectCommand string `json:"detectCommand,omitempty"`
	// AIInstructions the AI follows on sessions of this type (added to the global ones), e.g.
	// "save configuration after every change".
	AIInstructions string `json:"aiInstructions,omitempty"`
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
			fact("Model", "show version", `^[Cc]isco (\S+) \(|^Model [Nn]umber\s*:\s*(\S+)`, false),
			fact("Version", "show version", `Cisco IOS.*Version ([^,\s]+)`, true),
			fact("Serial", "show version", `System [Ss]erial [Nn]umber\s*:\s*(\S+)|Processor board ID (\S+)`, false),
			fact("MAC", "show version", `Base [Ee]thernet MAC [Aa]ddress\s*:\s*`+mac, false),
			fact("Uptime", "show version", `uptime is (.+?)\s*$`, true),
		}
	case "CISCO NX-OS":
		return []FactRule{
			fact("Hostname", "show version", `Device name:\s*(\S+)`, false),
			fact("Model", "show version", `cisco Nexus\S*\s+(\S+)\s+[Cc]hassis`, false),
			fact("Version", "show version", `(?:NXOS|system):\s+version\s+(\S+)`, true),
			fact("Serial", "show version", `Processor [Bb]oard ID (\S+)`, false),
			fact("Uptime", "show version", `Kernel uptime is (.+?)\s*$`, true),
		}
	case "HUAWEI VRP":
		return []FactRule{
			fact("Hostname", "display current-configuration | include sysname", `^\s*sysname (\S+)`, false),
			fact("Model", "display version", `^(?:HUAWEI|Huawei) (\S+).*\buptime is`, false),
			fact("Version", "display version", `VRP \(R\) software,\s*Version (\S+(?: \([^)]+\))?)`, true),
			fact("Serial", "display esn", `ESN of [^:]*:\s*(\S+)`, false),
			fact("MAC", "display bridge mac-address", `(?i)bridge MAC address\s*:\s*`+mac, false),
			fact("Uptime", "display version", `uptime is (.+?)\s*$`, true),
		}
	case "ELTEX MES":
		return []FactRule{
			fact("Hostname", "show system", `System Name:\s*(\S+)`, false),
			fact("Model", "show system", `System Description:\s*(\S+)`, false),
			fact("Version", "show version", `(?:^\s*Version:\s*|SW version\s+)(\S+)`, true),
			fact("Serial", "show system id", `(?i)^\s*(?:1\s+|serial number\s*:\s*)([A-Z0-9]{6,})\s*$`, false),
			fact("MAC", "show system", `System MAC Address:\s*`+mac, false),
			fact("Uptime", "show system", `System Up Time(?: \([^)]*\))?:\s*(.+?)\s*$`, true),
		}
	case "RUCKUS ICX":
		return []FactRule{
			fact("Hostname", "show running-config | include hostname", `^hostname (\S+)`, false),
			fact("Model", "show version", `HW:\s*(?:Stackable\s+)?(\S+)`, false),
			fact("Version", "show version", `SW: Version (\S+)`, true),
			fact("Serial", "show version", `Serial\s*#:\s*(\S+)`, false),
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
// Tunnels says whether its SSH server passes connections on (its own setting, else its built-in type's).
func (d DeviceType) Tunnels() bool {
	t := strings.ToLower(strings.TrimSpace(d.Tunnel))
	if t == "" {
		for _, b := range BuiltinDeviceTypes() {
			if strings.EqualFold(b.Name, d.Name) {
				t = b.Tunnel
			}
		}
	}
	return t != "off"
}

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
			WebSave: "save configuration", Tunnel: "off"},
		{Name: "VOSS", Protocol: ProtoSSH2, Prompt: `:\d+(\([^)]*\))?[>#] ?$`, BackupCommands: "show running-config",
			LoginSteps: []LogonAction{step("Login:", `\s`, false), step("Password:", `\w`, true)},
			AfterLogin: []LogonAction{step(">", "enable", false), step("#", "terminal more disable", false)},
			// the web server serves HTTPS when it is on; "secure-only" turns HTTP off
			WebCheck: "show web-server", WebHTTP: `(?is)status\s*:\s*enabled.*secure[ -]?only\s*:\s*disabled`, WebHTTPS: `(?i)status\s*:\s*enabled`,
			WebOn:   "configure terminal\nweb-server enable\nno web-server secure-only\nend",
			WebSave: "save config", Tunnel: "off"},
		{Name: "Cisco IOS", Protocol: ProtoSSH2, Prompt: `[>#] ?$`, BackupCommands: "show running-config",
			LoginSteps: []LogonAction{step("Username:", `\s`, false), step("Password:", `\w`, true)},
			AfterLogin: []LogonAction{step(">", "enable", false), step("#", "terminal length 0", false)}, Tunnel: "off"},
		{Name: "Cisco NX-OS", Protocol: ProtoSSH2, Prompt: `# ?$`, BackupCommands: "show running-config",
			LoginSteps: []LogonAction{step("login:", `\s`, false), step("Password:", `\w`, true)},
			AfterLogin: []LogonAction{step("#", "terminal length 0", false)}, Tunnel: "off"},
		// Huawei VRP: <name> in user view, [name] in system view; "display" instead of "show"
		{Name: "Huawei VRP", Protocol: ProtoSSH2, Prompt: `[>\]] ?$`, BackupCommands: "display current-configuration",
			LoginSteps: []LogonAction{step("Username:", `\s`, false), step("Password:", `\w`, true)},
			AfterLogin: []LogonAction{step(">", "screen-length 0 temporary", false)}, Tunnel: "off"},
		{Name: "Eltex MES", Protocol: ProtoSSH2, Prompt: `[>#] ?$`, BackupCommands: "show running-config",
			LoginSteps: []LogonAction{step("User Name:", `\s`, false), step("Password:", `\w`, true)},
			AfterLogin: []LogonAction{step(">", "enable", false), step("#", "terminal datadump", false)}, Tunnel: "off"},
		// Ruckus (formerly Brocade) ICX, FastIron
		{Name: "Ruckus ICX", Protocol: ProtoSSH2, Prompt: `[>#] ?$`, BackupCommands: "show running-config",
			LoginSteps: []LogonAction{step("Name:", `\s`, false), step("Password:", `\w`, true)},
			AfterLogin: []LogonAction{step(">", "enable", false), step("#", "skip-page-display", false)}, Tunnel: "off"},
		{Name: "Linux", Protocol: ProtoSSH2, Prompt: `[$#] ?$`,
			LoginSteps: []LogonAction{step("login:", `\s`, false), step("Password:", `\w`, true)}},
	}
	for i := range list {
		list[i].Facts = BuiltinFacts(list[i].Name)
		list[i].Detect, list[i].DetectCommand = BuiltinDetect(list[i].Name)
	}
	return list
}

// BuiltinDetect is the detection pattern and command of a built-in device type ("" when none).
func BuiltinDetect(name string) (pattern, command string) {
	switch strings.ToUpper(name) {
	case "EXOS":
		return `(?i)extremexos|\bexos\b|switch ?engine|summit ?x?\d`, "show version"
	case "VOSS":
		return `(?i)\bvoss\b|vsp-?\d{3,4}|fabric ?engine|virtual services platform`, "show sys-info"
	case "CISCO IOS":
		return `(?i)cisco ios|ios[- ]xe`, "show version"
	case "CISCO NX-OS":
		return `(?i)nx-os|cisco nexus`, "show version"
	case "HUAWEI VRP":
		return `(?i)huawei versatile routing platform|\bvrp\b|huawei`, "display version"
	case "ELTEX MES":
		return `(?i)eltex|\bmes ?\d{4}`, "show system"
	case "RUCKUS ICX":
		return `(?i)ruckus|brocade|fastiron|\bicx ?\d{4}`, "show version"
	case "LINUX":
		return `(?i)\blinux\b|ubuntu|debian|centos|red ?hat|rocky|almalinux|fedora|suse`, "uname -a"
	}
	return "", ""
}

// DetectCmd is the command whose output identifies this type after logging in.
func (d DeviceType) DetectCmd() string {
	if c := strings.TrimSpace(d.DetectCommand); c != "" {
		return c
	}
	return "show version"
}

// MatchDeviceType returns the device type whose Detect pattern matches text, and the text it matched.
// A type that looks for a general-purpose OS (Linux) wins only when no network OS matches, because a
// switch can run OpenSSH or mention Linux; otherwise the first in the list wins. Bad patterns are
// skipped.
func MatchDeviceType(types []DeviceType, text string) (name, matched string) {
	var later, laterM string
	for _, d := range types {
		p := strings.TrimSpace(d.Detect)
		if p == "" || strings.TrimSpace(text) == "" {
			continue
		}
		re, err := regexp.Compile(`(?m)` + p)
		if err != nil {
			continue
		}
		m := re.FindString(text)
		if m == "" {
			continue
		}
		if generalOS.MatchString(d.Name) || generalOS.MatchString(p) && !networkOS.MatchString(p) {
			if later == "" {
				later, laterM = d.Name, m
			}
			continue
		}
		return d.Name, m
	}
	return later, laterM
}

var (
	generalOS = regexp.MustCompile(`(?i)\b(linux|ubuntu|debian|centos|windows)\b`)
	networkOS = regexp.MustCompile(`(?i)exos|voss|cisco|nx-os|junos|eos|huawei|vrp|eltex|icx|ruckus|brocade`)
)

// builtinUndo are the built-in undo rules by device type name (lower case).
var builtinUndo = map[string]string{
	"exos": `^(show|sh|ping|traceroute|save|top|clear|disable clipaging|enable clipaging) =>
^create vlan (\S+).* => delete vlan $1
^config(?:ure)? (?:vlan )?(\S+) add ports? (\S+).* => configure vlan $1 delete ports $2
^config(?:ure)? (?:vlan )?(\S+) ipaddress .* => unconfigure vlan $1 ipaddress
^config(?:ure)? (?:vlan )?(\S+) tag .* =>
^create (\S+) (\S+).* => delete $1 $2
^enable (.+) => disable $1
^disable (.+) => enable $1`,
	"voss": ciscoUndo, "cisco ios": ciscoUndo, "cisco nx-os": ciscoUndo, "eltex mes": ciscoUndo, "ruckus icx": ciscoUndo,
	"huawei vrp": `^(display|ping|tracert|save|quit|return|system-view|screen-length) =>
^interface .* =>
^undo (.+) => $1
^(.+) => undo $1`,
}

// ciscoUndo: "no" in front, or taken away; mode changes and reading commands need no undo.
const ciscoUndo = `^(show|ping|traceroute|exit|end|copy|write|save|terminal|enable|configure|conf t|do) =>
^interface .* =>
^router .* =>
^no (.+) => $1
^(.+) => no $1`

// builtinTestState are the built-in save and restore commands for a test's restore: [config].
var builtinTestState = map[string][2]string{
	"cisco nx-os": {"checkpoint {file}", "rollback running-config checkpoint {file}\nno checkpoint {file}"},
	"cisco ios":   {"copy running-config flash:{file}.cfg", "configure replace flash:{file}.cfg force"},
	"exos":        {"save configuration {file}", "use configuration {file}\nreboot"},
}

// builtinErrors are the built-in error patterns by device type name.
var builtinErrors = map[string]string{
	"exos":        "^\\s*Error:\n%% (Invalid|Incomplete|Ambiguous|Unrecognized)",
	"voss":        "^\\s*% *(Invalid|Unrecognized|Incomplete|Ambiguous|Error|Cannot)",
	"cisco ios":   "^\\s*% *(Invalid|Incomplete|Ambiguous|Unknown|Error|Bad)",
	"cisco nx-os": "^\\s*% *(Invalid|Incomplete|Ambiguous)\n^\\s*ERROR:",
	"huawei vrp":  "^\\s*Error:",
	"eltex mes":   "^\\s*% *(Unrecognized|Invalid|Incomplete|Ambiguous|Error)",
	"ruckus icx":  "Invalid input\n^\\s*Error",
}

// builtinFactory are the built-in factory-default commands by device type name.
var builtinFactory = map[string]string{
	"exos":        "unconfigure switch all",
	"cisco ios":   "write erase\nreload",
	"cisco nx-os": "write erase\nreload",
}

// Errors returns the device type's error patterns (its own, else the built-in ones).
func (d DeviceType) Errors() []*regexp.Regexp {
	src := d.ErrorPatterns
	if strings.TrimSpace(src) == "" {
		src = builtinErrors[strings.ToLower(d.Name)]
	}
	var out []*regexp.Regexp
	for _, l := range strings.Split(src, "\n") {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		if re, err := regexp.Compile("(?m)" + l); err == nil {
			out = append(out, re)
		}
	}
	return out
}

// Factory returns the commands that reset the device to factory default (none: nil).
func (d DeviceType) Factory() []string {
	src := d.FactoryReset
	if strings.TrimSpace(src) == "" {
		src = builtinFactory[strings.ToLower(d.Name)]
	}
	var out []string
	for _, l := range strings.Split(src, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// BuiltinErrors and BuiltinFactory return the built-in error patterns and factory commands of a name.
func BuiltinErrors(name string) string  { return builtinErrors[strings.ToLower(name)] }
func BuiltinFactory(name string) string { return builtinFactory[strings.ToLower(name)] }

// Undo returns the device type's undo rules (its own, else the built-in ones).
func (d DeviceType) Undo() string {
	if strings.TrimSpace(d.UndoRules) != "" {
		return d.UndoRules
	}
	return builtinUndo[strings.ToLower(d.Name)]
}

// BuiltinUndo returns the built-in undo rules for a device type name ("" when none).
func BuiltinUndo(name string) string { return builtinUndo[strings.ToLower(name)] }

// UndoOf suggests the cleanup of a command with rules ("" when none applies or it needs none).
func UndoOf(rules, cmd string) string {
	cmd = strings.TrimSpace(cmd)
	for _, l := range strings.Split(rules, "\n") {
		pat, undo, ok := strings.Cut(l, "=>")
		pat, undo = strings.TrimSpace(pat), strings.TrimSpace(undo)
		if !ok || pat == "" {
			continue
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			continue
		}
		m := re.FindStringSubmatchIndex(cmd)
		if m == nil {
			continue
		}
		if undo == "" {
			return ""
		}
		return string(re.ExpandString(nil, strings.ReplaceAll(undo, "$0", cmd), cmd, m))
	}
	return ""
}

// TestState returns the commands that save and restore the configuration for a test (empty when the
// device type has none).
func (d DeviceType) TestState() (save, restore []string) {
	s, r := d.TestSave, d.TestRestore
	if strings.TrimSpace(s) == "" && strings.TrimSpace(r) == "" {
		b := builtinTestState[strings.ToLower(d.Name)]
		s, r = b[0], b[1]
	}
	lines := func(t string) []string {
		var out []string
		for _, l := range strings.Split(t, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				out = append(out, l)
			}
		}
		return out
	}
	return lines(s), lines(r)
}

// BuiltinTestState returns the built-in save and restore commands for a device type name.
func BuiltinTestState(name string) (save, restore string) {
	b := builtinTestState[strings.ToLower(name)]
	return b[0], b[1]
}
