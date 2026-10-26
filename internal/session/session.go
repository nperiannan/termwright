// Package session holds the session model and the on-disk session library.
package session

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/termwright/termwright/internal/securecrt"
)

// Protocol names as stored in session files.
const (
	ProtoSSH2   = "SSH2"
	ProtoTelnet = "Telnet"
	ProtoSerial = "Serial"
	ProtoLocal  = "Local Shell"
	ProtoRaw    = "Raw"
)

// Keys used in the interoperable session file format. Keys prefixed with
// "Termwright " are Termwright extensions and are ignored by other applications.
const (
	kProtocol     = "Protocol Name"
	kHostname     = "Hostname"
	kUsername     = "Username"
	kSSH2Port     = "[SSH2] Port"
	kPort         = "Port"
	kEmulation    = "Emulation"
	kDescription  = "Description"
	kRows         = "Rows"
	kCols         = "Cols"
	kScrollback   = "Scrollback"
	kIdentity     = "Identity Filename V2"
	kAuths        = "SSH2 Authentications V2"
	kKeepalive    = "Idle NO-OP Check"
	kKeepaliveSec = "Idle NO-OP Timeout"
	kComPort      = "[Serial] Com Port"
	kBaud         = "[Serial] Baud Rate"
	kDataBits     = "[Serial] Data Bits"
	kStopBits     = "[Serial] Stop Bits"
	kParity       = "[Serial] Parity"
	kLogFile      = "Log Filename V2"
	kLogOnConnect = "Start Log Upon Connect"
	kLogAppend    = "Append to Log File"
	kLogEachLine  = "Custom Log Message Each Line"
	kLogTimestamp = "Log Timestamp Each Line"
	kLogRaw       = "Termwright Raw Log"
	kSavePassword = "Termwright Save Password"
	kLocalCommand = "Termwright Local Command"
	kLocalDir     = "Initial Local Directory V2"
	kLogon        = "Termwright Logon Actions"
	kTermType     = "Termwright Terminal Type"
	kColorScheme  = "Color Scheme"
	kFirewall     = "Firewall Name"
	kAutoReconn   = "Auto Reconnect"
	kX11          = "Forward X11"
	kAgentFwd     = "Enable Agent Forwarding"
	kKeywordSet   = "Keyword Set"
	kForwards     = "Termwright Port Forwards"
	kProxyType    = "Termwright Proxy Type"
	kProxyHost    = "Termwright Proxy Host"
	kProxyPort    = "Termwright Proxy Port"
	kProxyUser    = "Termwright Proxy User"
	kFontFamily   = "Termwright Font"
	kFontSize     = "Termwright Font Size"
	kBackspace    = "Termwright Backspace"
	kTriggers     = "Termwright Triggers"
	kLogonScript  = "Termwright Logon Script"
	// SecureCRT 9.x logon keys (verified against a real 9.x session file).
	// "Login Script V4" (the Expect/Send table) is stored encrypted by SecureCRT
	// and is kept untouched; Termwright keeps its own table in kLogon.
	kUseLogin    = "Use Login Script"
	kInitialCR   = "Send Initial Carriage Return"
	kUseScript   = "Use Script File"
	kScriptFile  = "Script Filename V2"
	kLogonArgs   = "Script Arguments"
	kUseShellCmd = "Use Shell Command"
	kShellCmd    = "Shell Command"
	kOpenAs      = "Termwright Open As"
	kDevice      = "Termwright Device Type"
	kOwnLogin    = "Termwright Own Login"
	kEnv         = "Termwright Environment"
	kSFTPTarget  = "Termwright SFTP To Command Target"
	kKrbPrinc    = "Termwright Kerberos Principal"
	kKrbSPN      = "Termwright Kerberos SPN"
	kCertFile    = "Termwright Certificate File"
	kSerialNoDTR = "Termwright Serial No DTR"
	kSerialNoRTS = "Termwright Serial No RTS"
	kSerialFlow  = "Termwright Serial Flow"
)

// PortForward is an SSH tunnel definition.
type PortForward struct {
	Type     string `json:"type"` // local | remote | dynamic
	BindAddr string `json:"bindAddr"`
	BindPort int    `json:"bindPort"`
	DestHost string `json:"destHost"`
	DestPort int    `json:"destPort"`
	Name     string `json:"name"`
}

// Trigger sends text whenever matching output is received.
type Trigger struct {
	Pattern string `json:"pattern"`
	Regex   bool   `json:"regex"`
	Send    string `json:"send"`
}

// LogonAction is an expect/send pair run after connecting.
// Send supports \r \n \b \e \t \\ \xHH, \v (clipboard), \p (1 s pause),
// \s (credential user name) and \w (credential password).
type LogonAction struct {
	Expect     string `json:"expect"`
	Send       string `json:"send"`
	Hide       bool   `json:"hide"`       // mask Send in the UI (passwords)
	TrailingCR bool   `json:"trailingCr"` // append Enter after Send
	Credential string `json:"credential"` // credential name used by \s and \w
}

// Session is the editable view of a session file exposed to the UI.
type Session struct {
	Path              string        `json:"path"`   // "Folder/Sub/Name" (no extension)
	Name              string        `json:"name"`   // last path element
	Folder            string        `json:"folder"` // parent folder path, "" for root
	Protocol          string        `json:"protocol"`
	Hostname          string        `json:"hostname"`
	Port              int           `json:"port"`
	Username          string        `json:"username"`
	IdentityFile      string        `json:"identityFile"`
	AuthMethods       string        `json:"authMethods"`
	Emulation         string        `json:"emulation"`
	TermType          string        `json:"termType"`
	Description       []string      `json:"description"`
	Rows              int           `json:"rows"`
	Cols              int           `json:"cols"`
	Scrollback        int           `json:"scrollback"`
	Keepalive         bool          `json:"keepalive"`
	KeepaliveSec      int           `json:"keepaliveSec"`
	ComPort           string        `json:"comPort"`
	BaudRate          int           `json:"baudRate"`
	DataBits          int           `json:"dataBits"`
	StopBits          int           `json:"stopBits"` // 0 = 1, 1 = 1.5, 2 = 2
	Parity            int           `json:"parity"`   // 0 none, 1 odd, 2 even, 3 mark, 4 space
	LogFile           string        `json:"logFile"`
	LogOnConnect      bool          `json:"logOnConnect"`
	LogAppend         bool          `json:"logAppend"`
	LogEachLine       string        `json:"logEachLine"`
	LogRaw            bool          `json:"logRaw"`
	SavePassword      bool          `json:"savePassword"`
	LocalCommand      string        `json:"localCommand"`
	LocalDir          string        `json:"localDir"`
	ColorScheme       string        `json:"colorScheme"`
	LogonActions      []LogonAction `json:"logonActions"`
	Triggers          []Trigger     `json:"triggers"`
	PortForwards      []PortForward `json:"portForwards"`
	JumpSession       string        `json:"jumpSession"` // path of a session used as jump host
	ProxyType         string        `json:"proxyType"`   // "" | socks5 | http
	ProxyHost         string        `json:"proxyHost"`
	ProxyPort         int           `json:"proxyPort"`
	ProxyUser         string        `json:"proxyUser"`
	AutoReconnect     bool          `json:"autoReconnect"`
	X11Forward        bool          `json:"x11Forward"`
	AgentForward      bool          `json:"agentForward"`
	KeywordSet        string        `json:"keywordSet"`
	FontFamily        string        `json:"fontFamily"`
	FontSize          int           `json:"fontSize"`
	Backspace         string        `json:"backspace"`   // "" (DEL, default) | "bs" (Ctrl+H)
	LogonScript       string        `json:"logonScript"` // JavaScript file run after connecting
	LogonArgs         string        `json:"logonArgs"`   // arguments for the logon script (scriptArgs)
	AutomateLogon     bool          `json:"automateLogon"`
	InitialCR         bool          `json:"initialCr"` // send Enter right after connecting
	UseLogonScript    bool          `json:"useLogonScript"`
	RemoteCommand     string        `json:"remoteCommand"` // SSH2: run this instead of a login shell
	UseRemoteCommand  bool          `json:"useRemoteCommand"`
	OpenAs            string        `json:"openAs"`            // SSH2: "" = terminal, "sftp" = SFTP browser only
	DeviceType        string        `json:"deviceType"`        // name of a device type (login profile)
	OwnLogin          bool          `json:"ownLogin"`          // with a device type: use this session's username/password instead of the device type's
	Environment       string        `json:"environment"`       // "", production, staging, test, development: colours the tab, production asks before sending to many tabs
	KerberosPrincipal string        `json:"kerberosPrincipal"` // GSSAPI: user@REALM when no kinit ticket ("" = login user)
	KerberosSPN       string        `json:"kerberosSpn"`       // GSSAPI target, "" = host@<hostname>; %H = hostname
	CertificateFile   string        `json:"certificateFile"`   // OpenSSH user certificate, "" = <identity>-cert.pub
	SFTPToTarget      bool          `json:"sftpToTarget"`      // remote command "ssh … host": SFTP opens on that host, through this connection
	SerialDTR         bool          `json:"serialDtr"`         // assert DTR when opening the port
	SerialRTS         bool          `json:"serialRts"`         // assert RTS when opening the port
	SerialFlow        string        `json:"serialFlow"`        // "" none | rtscts | xonxoff
}

// Defaults returns a new session with sensible default values.
func Defaults() Session {
	return Session{
		Protocol: ProtoSSH2, Port: 22, Emulation: "Xterm", Rows: 24, Cols: 80,
		Scrollback: 10000, KeepaliveSec: 60, BaudRate: 9600, DataBits: 8,
		AuthMethods: "publickey,keyboard-interactive,password",
		Description: []string{}, LogonActions: []LogonAction{},
		Triggers: []Trigger{}, PortForwards: []PortForward{}, SerialDTR: true, SerialRTS: true, AutomateLogon: true,
	}
}

// FromFile maps a parsed session file into a Session.
func FromFile(path string, f *securecrt.File) Session {
	s := Defaults()
	s.Path = path
	s.Name, s.Folder = splitPath(path)
	s.Protocol = f.Str(kProtocol, s.Protocol)
	s.Hostname = f.Str(kHostname, "")
	s.Username = f.Str(kUsername, "")
	switch s.Protocol {
	case ProtoTelnet:
		s.Port = int(f.DWord(kPort, 23))
	case ProtoRaw:
		s.Port = int(f.DWord(kPort, 0))
	default:
		s.Port = int(f.DWord(kSSH2Port, 22))
	}
	s.IdentityFile = stripIdentity(f.Str(kIdentity, ""))
	s.AuthMethods = f.Str(kAuths, s.AuthMethods)
	s.Emulation = f.Str(kEmulation, s.Emulation)
	s.TermType = f.Str(kTermType, "")
	if d := f.List(kDescription); d != nil {
		s.Description = d
	}
	s.Rows = int(f.DWord(kRows, uint32(s.Rows)))
	s.Cols = int(f.DWord(kCols, uint32(s.Cols)))
	s.Scrollback = int(f.DWord(kScrollback, uint32(s.Scrollback)))
	s.Keepalive = f.Bool(kKeepalive, false)
	s.KeepaliveSec = int(f.DWord(kKeepaliveSec, uint32(s.KeepaliveSec)))
	s.ComPort = f.Str(kComPort, "")
	s.BaudRate = int(f.DWord(kBaud, uint32(s.BaudRate)))
	s.DataBits = int(f.DWord(kDataBits, uint32(s.DataBits)))
	s.StopBits = int(f.DWord(kStopBits, 0))
	s.Parity = int(f.DWord(kParity, 0))
	s.LogFile = f.Str(kLogFile, "")
	s.LogOnConnect = f.Bool(kLogOnConnect, false)
	s.LogAppend = f.Bool(kLogAppend, false)
	s.LogEachLine = f.Str(kLogEachLine, "")
	if ts := f.Get(kLogTimestamp); ts != nil {
		// the original application only prefixes lines when this flag is on
		if ts.DWord == 0 {
			s.LogEachLine = ""
		} else if s.LogEachLine == "" {
			s.LogEachLine = "[%Y-%M-%D %h:%m:%s] "
		}
	}
	s.LogRaw = f.Bool(kLogRaw, false)
	s.SavePassword = f.Bool(kSavePassword, false)
	s.LocalCommand = f.Str(kLocalCommand, "")
	s.LocalDir = f.Str(kLocalDir, "")
	s.ColorScheme = f.Str(kColorScheme, "")
	for _, l := range f.List(kLogon) {
		// "expect<TAB>send" (0.1-0.5) or "expect<TAB>send<TAB>flags<TAB>credential"
		p := strings.SplitN(l, "\t", 4)
		a := LogonAction{Expect: unescapeTab(p[0])}
		if len(p) > 1 {
			a.Send = unescapeTab(p[1])
		}
		if len(p) > 2 {
			a.Hide = strings.Contains(p[2], "h")
			a.TrailingCR = strings.Contains(p[2], "c")
		}
		if len(p) > 3 {
			a.Credential = unescapeTab(p[3])
		}
		s.LogonActions = append(s.LogonActions, a)
	}
	s.AutomateLogon = f.Bool(kUseLogin, true)
	s.InitialCR = f.Bool(kInitialCR, false)
	s.LogonArgs = f.Str(kLogonArgs, "")
	s.RemoteCommand = f.Str(kShellCmd, "")
	s.UseRemoteCommand = f.Bool(kUseShellCmd, false)
	s.OpenAs = f.Str(kOpenAs, "")
	s.DeviceType = f.Str(kDevice, "")
	s.OwnLogin = f.Bool(kOwnLogin, false)
	s.Environment = f.Str(kEnv, "")
	s.SFTPToTarget = f.Bool(kSFTPTarget, false)
	s.KerberosPrincipal = f.Str(kKrbPrinc, "")
	s.KerberosSPN = f.Str(kKrbSPN, "")
	s.CertificateFile = f.Str(kCertFile, "")
	for _, l := range f.List(kTriggers) {
		parts := strings.SplitN(l, "\t", 3)
		if len(parts) == 3 {
			s.Triggers = append(s.Triggers, Trigger{Regex: parts[0] == "re", Pattern: unescapeTab(parts[1]), Send: unescapeTab(parts[2])})
		}
	}
	for _, l := range f.List(kForwards) {
		if pf, ok := parseForward(l); ok {
			s.PortForwards = append(s.PortForwards, pf)
		}
	}
	if f.Get(kForwards) == nil {
		// No Termwright forwards yet: read the original application's table
		// (read-only; it is preserved unchanged when the session is saved).
		for _, key := range []string{"Port Forward Table V3", "Port Forward Table V2", "Port Forward TableV2", "Port Forward Table"} {
			for _, l := range f.List(key) {
				if pf, ok := parseSecureCRTForward(l); ok {
					s.PortForwards = append(s.PortForwards, pf)
				}
			}
			if f.Get(key) != nil {
				break
			}
		}
		for _, key := range []string{"Reverse Forward Table V3", "Reverse Forward Table V2", "Reverse Forward TableV2"} {
			for _, l := range f.List(key) {
				if pf, ok := parseSecureCRTForward(l); ok && pf.Type != "dynamic" {
					pf.Type = "remote"
					s.PortForwards = append(s.PortForwards, pf)
				}
			}
		}
	}
	if fw := f.Str(kFirewall, ""); strings.HasPrefix(fw, "Session:") {
		s.JumpSession = strings.TrimPrefix(fw, "Session:")
	}
	s.ProxyType = f.Str(kProxyType, "")
	s.ProxyHost = f.Str(kProxyHost, "")
	s.ProxyPort = int(f.DWord(kProxyPort, 0))
	s.ProxyUser = f.Str(kProxyUser, "")
	s.AutoReconnect = f.Bool(kAutoReconn, false)
	s.X11Forward = f.Bool(kX11, false)
	s.AgentForward = f.Bool(kAgentFwd, false)
	s.KeywordSet = f.Str(kKeywordSet, "")
	s.FontFamily = f.Str(kFontFamily, "")
	s.FontSize = int(f.DWord(kFontSize, 0))
	s.Backspace = f.Str(kBackspace, "")
	s.LogonScript = f.Str(kScriptFile, f.Str(kLogonScript, ""))
	s.UseLogonScript = f.Bool(kUseScript, f.Get(kLogonScript) != nil)
	s.SerialDTR = !f.Bool(kSerialNoDTR, false)
	s.SerialRTS = !f.Bool(kSerialNoRTS, false)
	s.SerialFlow = f.Str(kSerialFlow, "")
	return s
}

// parseSecureCRTForward decodes one line of a SecureCRT port forward table:
//
//	localhost:8080:webserver.internal.net:80::0:0::   local forward
//	D1080::::0:0::                                     dynamic (SOCKS) forward
//
// Fields after the destination port are application flags and ignored.
func parseSecureCRTForward(l string) (PortForward, bool) {
	l = strings.TrimSpace(l)
	if l == "" {
		return PortForward{}, false
	}
	if strings.Contains(l, "|") {
		return parseForwardV3(l)
	}
	if l[0] == 'D' || l[0] == 'd' {
		p := strings.SplitN(l[1:], ":", 2)
		if port, err := strconv.Atoi(p[0]); err == nil && port > 0 {
			return PortForward{Type: "dynamic", BindAddr: "127.0.0.1", BindPort: port, Name: "SOCKS " + p[0]}, true
		}
		return PortForward{}, false
	}
	p := strings.Split(l, ":")
	if len(p) < 4 {
		return PortForward{}, false
	}
	lport, err1 := strconv.Atoi(p[1])
	dport, err2 := strconv.Atoi(p[3])
	if err1 != nil || err2 != nil || lport <= 0 || p[2] == "" {
		return PortForward{}, false
	}
	bind := p[0]
	if bind == "" || strings.EqualFold(bind, "localhost") {
		bind = "127.0.0.1"
	}
	return PortForward{Type: "local", BindAddr: bind, BindPort: lport, DestHost: p[2], DestPort: dport,
		Name: fmt.Sprintf("%d -> %s:%d", lport, p[2], dport)}, true
}

// parseForwardV3 decodes the 9.x "Port Forward Table V3" / "Reverse Forward
// Table V3" line format (verified against SecureCRT 9.x files):
//
//	enabled|name|bindIP,port|destDifferent|destHost|destPort|application|arguments
//	1|web|10.0.0.5,8080|0||80||          -> port 80 on the SSH server itself
//	1|db|,3306|1|db.lab|3306||           -> db.lab:3306 as seen from the server
//	1|proxy|,1080|0|socks,|||            -> dynamic SOCKS 4/5 forward
//
// Disabled entries (enabled = 0) are skipped.
func parseForwardV3(l string) (PortForward, bool) {
	p := strings.Split(l, "|")
	for len(p) < 6 {
		p = append(p, "")
	}
	if strings.TrimSpace(p[0]) == "0" {
		return PortForward{}, false
	}
	bind, portStr, found := strings.Cut(p[2], ",")
	if !found {
		bind, portStr = "", p[2]
	}
	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || port <= 0 {
		return PortForward{}, false
	}
	bind = strings.TrimSpace(bind)
	if bind == "" || strings.EqualFold(bind, "localhost") {
		bind = "127.0.0.1"
	}
	pf := PortForward{BindAddr: bind, BindPort: port, Name: p[1]}
	host := strings.TrimSpace(p[4])
	if strings.HasPrefix(strings.ToLower(host), "socks") {
		pf.Type = "dynamic"
		return pf, true
	}
	pf.Type = "local" // the caller turns entries of the reverse table into remote forwards
	if strings.TrimSpace(p[3]) == "0" || host == "" {
		host = "localhost" // the SSH server itself
	}
	pf.DestHost = host
	if pf.DestPort, err = strconv.Atoi(strings.TrimSpace(p[5])); err != nil {
		return PortForward{}, false
	}
	return pf, true
}

// parseForward decodes "type|bindAddr|bindPort|destHost|destPort|name".
func parseForward(l string) (PortForward, bool) {
	p := strings.Split(l, "|")
	if len(p) < 5 {
		return PortForward{}, false
	}
	bp, err1 := strconv.Atoi(p[2])
	dp, _ := strconv.Atoi(p[4])
	if err1 != nil {
		return PortForward{}, false
	}
	pf := PortForward{Type: p[0], BindAddr: p[1], BindPort: bp, DestHost: p[3], DestPort: dp}
	if len(p) > 5 {
		pf.Name = strings.Join(p[5:], "|")
	}
	return pf, true
}

func (pf PortForward) encode() string {
	return fmt.Sprintf("%s|%s|%d|%s|%d|%s", pf.Type, pf.BindAddr, pf.BindPort, pf.DestHost, pf.DestPort, pf.Name)
}

// Apply writes the Session fields into f, keeping every other key untouched.
func (s Session) Apply(f *securecrt.File) {
	f.SetStr(kProtocol, s.Protocol)
	f.SetStr(kHostname, s.Hostname)
	f.SetStr(kUsername, s.Username)
	switch s.Protocol {
	case ProtoTelnet, ProtoRaw:
		f.SetDWord(kPort, uint32(s.Port))
	case ProtoSSH2:
		f.SetDWord(kSSH2Port, uint32(s.Port))
	}
	if s.IdentityFile != "" || f.Get(kIdentity) != nil {
		f.SetStr(kIdentity, s.IdentityFile)
	}
	f.SetStr(kAuths, s.AuthMethods)
	f.SetStr(kEmulation, s.Emulation)
	setOptStr(f, kTermType, s.TermType)
	f.SetList(kDescription, s.Description)
	f.SetDWord(kRows, uint32(s.Rows))
	f.SetDWord(kCols, uint32(s.Cols))
	f.SetDWord(kScrollback, uint32(s.Scrollback))
	f.SetBool(kKeepalive, s.Keepalive)
	f.SetDWord(kKeepaliveSec, uint32(s.KeepaliveSec))
	if s.Protocol == ProtoSerial || f.Get(kComPort) != nil {
		f.SetStr(kComPort, s.ComPort)
		f.SetDWord(kBaud, uint32(s.BaudRate))
		f.SetDWord(kDataBits, uint32(s.DataBits))
		f.SetDWord(kStopBits, uint32(s.StopBits))
		f.SetDWord(kParity, uint32(s.Parity))
	}
	f.SetStr(kLogFile, s.LogFile)
	f.SetBool(kLogOnConnect, s.LogOnConnect)
	f.SetBool(kLogAppend, s.LogAppend)
	if s.LogEachLine != "" || f.Get(kLogTimestamp) == nil || f.Bool(kLogTimestamp, false) {
		f.SetStr(kLogEachLine, s.LogEachLine) // keep a custom message when only switching it off
	}
	if f.Get(kLogTimestamp) != nil || s.LogEachLine != "" {
		f.SetBool(kLogTimestamp, s.LogEachLine != "")
	}
	setOptBool(f, kLogRaw, s.LogRaw)
	setOptBool(f, kSavePassword, s.SavePassword)
	setOptStr(f, kLocalCommand, s.LocalCommand)
	if s.LocalDir != "" || f.Get(kLocalDir) != nil {
		f.SetStr(kLocalDir, s.LocalDir)
	}
	if s.ColorScheme != "" || f.Get(kColorScheme) != nil {
		f.SetStr(kColorScheme, s.ColorScheme)
	}
	if len(s.LogonActions) > 0 {
		var l []string
		for _, a := range s.LogonActions {
			flags := "-"
			if a.Hide {
				flags += "h"
			}
			if a.TrailingCR {
				flags += "c"
			}
			l = append(l, escapeTab(a.Expect)+"\t"+escapeTab(a.Send)+"\t"+flags+"\t"+escapeTab(a.Credential))
		}
		f.SetList(kLogon, l)
	} else {
		f.Delete(kLogon)
	}
	if len(s.Triggers) > 0 {
		var l []string
		for _, t := range s.Triggers {
			kind := "txt"
			if t.Regex {
				kind = "re"
			}
			l = append(l, kind+"\t"+escapeTab(t.Pattern)+"\t"+escapeTab(t.Send))
		}
		f.SetList(kTriggers, l)
	} else {
		f.Delete(kTriggers)
	}
	if len(s.PortForwards) > 0 {
		var l []string
		for _, pf := range s.PortForwards {
			l = append(l, pf.encode())
		}
		f.SetList(kForwards, l)
	} else {
		f.Delete(kForwards)
	}
	if s.JumpSession != "" {
		f.SetStr(kFirewall, "Session:"+s.JumpSession)
	} else if strings.HasPrefix(f.Str(kFirewall, ""), "Session:") {
		f.SetStr(kFirewall, "None")
	}
	setOptStr(f, kProxyType, s.ProxyType)
	setOptStr(f, kProxyHost, s.ProxyHost)
	if s.ProxyPort > 0 {
		f.SetDWord(kProxyPort, uint32(s.ProxyPort))
	} else {
		f.Delete(kProxyPort)
	}
	setOptStr(f, kProxyUser, s.ProxyUser)
	if s.AutoReconnect || f.Get(kAutoReconn) != nil {
		f.SetBool(kAutoReconn, s.AutoReconnect)
	}
	if s.X11Forward || f.Get(kX11) != nil {
		f.SetBool(kX11, s.X11Forward)
	}
	if s.AgentForward || f.Get(kAgentFwd) != nil {
		f.SetBool(kAgentFwd, s.AgentForward)
	}
	if s.KeywordSet != "" || f.Get(kKeywordSet) != nil {
		f.SetStr(kKeywordSet, s.KeywordSet)
	}
	setOptStr(f, kFontFamily, s.FontFamily)
	if s.FontSize > 0 {
		f.SetDWord(kFontSize, uint32(s.FontSize))
	} else {
		f.Delete(kFontSize)
	}
	setOptStr(f, kBackspace, s.Backspace)
	f.Delete(kLogonScript) // pre-0.6 Termwright key
	f.SetStr(kScriptFile, s.LogonScript)
	f.SetBool(kUseScript, s.UseLogonScript && s.LogonScript != "")
	f.SetStr(kLogonArgs, s.LogonArgs)
	f.SetBool(kUseLogin, s.AutomateLogon)
	f.SetBool(kInitialCR, s.InitialCR)
	f.SetStr(kShellCmd, s.RemoteCommand)
	f.SetBool(kUseShellCmd, s.UseRemoteCommand && strings.TrimSpace(s.RemoteCommand) != "")
	setOptStr(f, kOpenAs, s.OpenAs)
	setOptStr(f, kDevice, s.DeviceType)
	setOptBool(f, kOwnLogin, s.OwnLogin && s.DeviceType != "")
	setOptStr(f, kEnv, s.Environment)
	setOptBool(f, kSFTPTarget, s.SFTPToTarget && s.UseRemoteCommand)
	setOptStr(f, kKrbPrinc, s.KerberosPrincipal)
	setOptStr(f, kKrbSPN, s.KerberosSPN)
	setOptStr(f, kCertFile, s.CertificateFile)
	setOptBool(f, kSerialNoDTR, !s.SerialDTR)
	setOptBool(f, kSerialNoRTS, !s.SerialRTS)
	setOptStr(f, kSerialFlow, s.SerialFlow)
}

func setOptStr(f *securecrt.File, k, v string) {
	if v == "" {
		f.Delete(k)
	} else {
		f.SetStr(k, v)
	}
}

func setOptBool(f *securecrt.File, k string, v bool) {
	if !v {
		f.Delete(k)
	} else {
		f.SetBool(k, true)
	}
}

func escapeTab(s string) string   { return strings.ReplaceAll(s, "\t", `\t`) }
func unescapeTab(s string) string { return strings.ReplaceAll(s, `\t`, "\t") }

// stripIdentity removes option suffixes some versions append to the identity
// path (e.g. "C:\keys\id_rsa:raw").
func stripIdentity(p string) string {
	if i := strings.LastIndex(p, ":"); i > 1 {
		suffix := p[i+1:]
		if suffix == "raw" || suffix == "pubkey" {
			return p[:i]
		}
	}
	return p
}

func splitPath(p string) (name, folder string) {
	p = strings.Trim(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:], p[:i]
	}
	return p, ""
}

// JoinPath joins a folder and a name into a session path.
func JoinPath(folder, name string) string {
	folder = strings.Trim(folder, "/")
	if folder == "" {
		return name
	}
	return folder + "/" + name
}
