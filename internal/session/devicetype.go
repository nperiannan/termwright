package session

import "strings"

// DeviceType is a reusable login profile for a kind of device (EXOS, VOSS,
// Cisco IOS, Linux …). A session or a Quick Connect picks one by name, so a
// connection needs only the address and the device type.
type DeviceType struct {
	Name     string `json:"name"`
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

// Steps returns the logon actions for a connection over protocol.
func (d DeviceType) Steps(protocol string) []LogonAction {
	res := make([]LogonAction, 0, len(d.LoginSteps)+len(d.AfterLogin))
	all := d.AfterLogin
	if protocol != ProtoSSH2 {
		all = append(append([]LogonAction{}, d.LoginSteps...), d.AfterLogin...)
	}
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
	return []DeviceType{
		{Name: "EXOS", Protocol: ProtoSSH2, Prompt: `[#>] ?$`, BackupCommands: "show configuration",
			LoginSteps: []LogonAction{step("login:", `\s`, false), step("password:", `\w`, true)},
			AfterLogin: []LogonAction{step("#", "disable clipaging", false)}},
		{Name: "VOSS", Protocol: ProtoSSH2, Prompt: `:\d+[>#] ?$`, BackupCommands: "show running-config",
			LoginSteps: []LogonAction{step("Login:", `\s`, false), step("Password:", `\w`, true)},
			AfterLogin: []LogonAction{step(">", "enable", false), step("#", "terminal more disable", false)}},
		{Name: "Cisco IOS", Protocol: ProtoSSH2, Prompt: `[>#] ?$`, BackupCommands: "show running-config",
			LoginSteps: []LogonAction{step("Username:", `\s`, false), step("Password:", `\w`, true)},
			AfterLogin: []LogonAction{step(">", "enable", false), step("#", "terminal length 0", false)}},
		{Name: "Linux", Protocol: ProtoSSH2, Prompt: `[$#] ?$`,
			LoginSteps: []LogonAction{step("login:", `\s`, false), step("Password:", `\w`, true)}},
	}
}
