package session

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"gopkg.in/yaml.v3"
)

// Exports for other terminal programs. What a program has no setting for
// (device types, logon actions, triggers, recordings…) stays in Termwright;
// passwords are never exported. The report lists what was left out.

// ExportFormats are the formats ExportOther writes.
var ExportFormats = map[string]string{
	"putty-reg":  "PuTTY sessions (.reg, Windows)",
	"kitty-reg":  "KiTTY sessions (.reg, Windows)",
	"putty-dir":  "PuTTY sessions folder (~/.putty/sessions, Linux/macOS)",
	"moba":       "MobaXterm sessions (.mxtsessions)",
	"tabby":      "Tabby profiles (.yaml)",
	"ssh-config": "OpenSSH client config (~/.ssh/config format)",
	"csv":        "CSV (spreadsheet / inventory)",
}

// ExportReport tells what an export wrote and what it left out.
type ExportReport struct {
	Count   int      `json:"count"`
	Skipped []string `json:"skipped"` // sessions the format cannot hold
	Notes   []string `json:"notes"`   // settings that were not carried over
}

type exportNotes struct {
	counts map[string]int
	order  []string
}

func (n *exportNotes) add(what string) {
	if n.counts == nil {
		n.counts = map[string]int{}
	}
	if n.counts[what] == 0 {
		n.order = append(n.order, what)
	}
	n.counts[what]++
}

func (n *exportNotes) list() []string {
	out := []string{}
	for _, w := range n.order {
		c := n.counts[w]
		out = append(out, fmt.Sprintf("%s (%d session%s)", w, c, map[bool]string{true: "", false: "s"}[c == 1]))
	}
	return out
}

// common notes for settings no other program has
func (n *exportNotes) termwrightOnly(s Session) {
	if s.DeviceType != "" {
		n.add("device type login and after-login steps are Termwright-only")
	}
	if s.AutomateLogon && len(s.LogonActions) > 0 {
		n.add("logon actions (expect/send) are not exported")
	}
	if len(s.Triggers) > 0 {
		n.add("triggers are not exported")
	}
	if s.SmartCard != "" {
		n.add("smart card key choice is not exported")
	}
}

// ExportOther writes sessions for another program. prep can complete each
// session (folder defaults, the device type's username) before it is written.
func (s *Store) ExportOther(paths []string, dest, format string, prep func(Session) Session) (*ExportReport, error) {
	if _, ok := ExportFormats[format]; !ok {
		return nil, fmt.Errorf("unknown export format %q", format)
	}
	list, err := s.AllSessions(paths)
	if err != nil {
		return nil, err
	}
	var sessions []Session
	for _, p := range list {
		ss, err := s.Get(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if prep != nil {
			ss = prep(ss)
		}
		sessions = append(sessions, ss)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Path < sessions[j].Path })
	byPath := map[string]Session{}
	for _, ss := range sessions {
		byPath[ss.Path] = ss
	}
	jump := func(p string) (Session, bool) {
		if j, ok := byPath[p]; ok {
			return j, true
		}
		j, err := s.Get(p)
		if err == nil && prep != nil {
			j = prep(j)
		}
		return j, err == nil
	}
	rep := &ExportReport{Skipped: []string{}}
	var notes exportNotes
	var data []byte
	switch format {
	case "putty-reg", "kitty-reg":
		data, err = exportPuTTYReg(sessions, format == "kitty-reg", jump, rep, &notes)
	case "putty-dir":
		err = exportPuTTYDir(sessions, dest, jump, rep, &notes)
	case "moba":
		data, err = exportMoba(sessions, jump, rep, &notes)
	case "tabby":
		data, err = exportTabby(sessions, rep, &notes)
	case "ssh-config":
		data, err = exportSSHConfig(sessions, rep, &notes)
	case "csv":
		data, err = exportCSV(sessions, rep, &notes)
	}
	if err != nil {
		return nil, err
	}
	if data != nil {
		if err := os.WriteFile(dest, data, 0o600); err != nil {
			return nil, err
		}
	}
	rep.Notes = append(notes.list(), "passwords are never exported (use the password vault export to move them between Termwright installations)")
	return rep, nil
}

// ---------- PuTTY / KiTTY ----------

// puttyEscape escapes a session name the way PuTTY stores it (file names
// on Linux/macOS also escape "/").
func puttyEscape(name string) string { return puttyEscapeFor(name, false) }

func puttyEscapeFor(name string, file bool) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (file && c == '/') || c == ' ' || c == '\\' || c == '*' || c == '?' || c == '%' || c < ' ' || c > '~' || (i == 0 && c == '.') {
			fmt.Fprintf(&b, "%%%02X", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

type puttyVal struct {
	key string
	str string
	num int
	isN bool
}

func puttyValues(s Session, kitty bool, jump func(string) (Session, bool), notes *exportNotes) ([]puttyVal, bool) {
	var v []puttyVal
	str := func(k, x string) { v = append(v, puttyVal{key: k, str: x}) }
	num := func(k string, x int) { v = append(v, puttyVal{key: k, num: x, isN: true}) }
	switch s.Protocol {
	case ProtoSSH2:
		str("Protocol", "ssh")
	case ProtoTelnet:
		str("Protocol", "telnet")
	case ProtoSerial:
		str("Protocol", "serial")
	case ProtoRaw:
		str("Protocol", "raw")
	default:
		return nil, false
	}
	if s.Folder != "" {
		if kitty {
			str("Folder", s.Folder)
		} else {
			str("PsmPath", strings.ReplaceAll(s.Folder, "/", `\`))
		}
	}
	if s.Protocol == ProtoSerial {
		str("SerialLine", s.ComPort)
		num("SerialSpeed", s.BaudRate)
		num("SerialDataBits", s.DataBits)
		num("SerialStopHalfbits", 2+s.StopBits)
		num("SerialParity", s.Parity)
		flow := 0
		switch s.SerialFlow {
		case "xonxoff":
			flow = 1
		case "rtscts":
			flow = 2
		}
		num("SerialFlowControl", flow)
	} else {
		str("HostName", s.Hostname)
		num("PortNumber", portOr(s))
	}
	str("UserName", s.Username)
	if s.TermType != "" {
		str("TerminalType", s.TermType)
	}
	if s.Scrollback > 0 {
		num("ScrollbackLines", s.Scrollback)
	}
	if s.Protocol == ProtoSSH2 {
		if s.IdentityFile != "" {
			str("PublicKeyFile", s.IdentityFile)
			if !strings.EqualFold(filepath.Ext(s.IdentityFile), ".ppk") {
				notes.add("PuTTY needs .ppk keys: convert OpenSSH key files with PuTTYgen")
			}
		}
		if s.Keepalive && s.KeepaliveSec > 0 {
			num("PingIntervalSecs", s.KeepaliveSec)
		}
		num("X11Forward", b2i(s.X11Forward))
		num("AgentFwd", b2i(s.AgentForward))
		if s.UseRemoteCommand && s.RemoteCommand != "" {
			str("RemoteCommand", s.RemoteCommand)
		}
		str("PortForwardings", puttyForwards(s.PortForwards))
		if s.CertificateFile != "" {
			str("DetachedCertificate", s.CertificateFile)
		}
	}
	switch {
	case s.JumpSession != "":
		if j, ok := jump(s.JumpSession); ok && j.Protocol == ProtoSSH2 {
			// PuTTY 0.77+: "SSH to proxy and use port forwarding"
			num("ProxyMethod", 6)
			str("ProxyHost", j.Hostname)
			num("ProxyPort", portOr(j))
			str("ProxyUsername", j.Username)
			if kitty {
				notes.add("jump host written as an SSH proxy (needs a KiTTY based on PuTTY 0.77 or newer)")
			} else {
				notes.add("jump host written as an SSH proxy (needs PuTTY 0.77 or newer)")
			}
		} else {
			notes.add("jump host not found - not exported")
		}
	case s.ProxyType == "socks5" || s.ProxyType == "http":
		num("ProxyMethod", map[string]int{"socks5": 2, "http": 3}[s.ProxyType])
		str("ProxyHost", s.ProxyHost)
		num("ProxyPort", s.ProxyPort)
		str("ProxyUsername", s.ProxyUser)
	}
	if s.LogOnConnect {
		lt := 1
		if s.LogRaw {
			lt = 2
		}
		num("LogType", lt)
		if s.LogFile != "" {
			str("LogFileName", s.LogFile)
		}
		num("LogFileClash", map[bool]int{true: 1, false: 0}[s.LogAppend])
	}
	notes.termwrightOnly(s)
	return v, true
}

func portOr(s Session) int {
	if s.Port > 0 {
		return s.Port
	}
	return defaultPortFor(s.Protocol)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func puttyForwards(pf []PortForward) string {
	var parts []string
	for _, f := range pf {
		if f.BindPort <= 0 {
			continue
		}
		src := strconv.Itoa(f.BindPort)
		if f.BindAddr != "" && f.BindAddr != "127.0.0.1" && f.BindAddr != "localhost" {
			src = f.BindAddr + ":" + src
		}
		switch f.Type {
		case "local":
			parts = append(parts, fmt.Sprintf("L%s=%s:%d", src, f.DestHost, f.DestPort))
		case "remote":
			parts = append(parts, fmt.Sprintf("R%s=%s:%d", src, f.DestHost, f.DestPort))
		case "dynamic":
			parts = append(parts, "D"+src)
		}
	}
	return strings.Join(parts, ",")
}

func skipNote(s Session, rep *ExportReport, why string) {
	rep.Skipped = append(rep.Skipped, s.Path+" ("+why+")")
}

func exportPuTTYReg(list []Session, kitty bool, jump func(string) (Session, bool), rep *ExportReport, notes *exportNotes) ([]byte, error) {
	root := `HKEY_CURRENT_USER\Software\SimonTatham\PuTTY\Sessions`
	if kitty {
		root = `HKEY_CURRENT_USER\Software\9bis.com\KiTTY\Sessions`
	}
	var b strings.Builder
	b.WriteString("Windows Registry Editor Version 5.00\r\n\r\n")
	for _, s := range list {
		vals, ok := puttyValues(s, kitty, jump, notes)
		if !ok {
			skipNote(s, rep, s.Protocol+" sessions are not supported")
			continue
		}
		fmt.Fprintf(&b, "[%s\\%s]\r\n", root, puttyEscape(s.Path))
		for _, v := range vals {
			if v.isN {
				fmt.Fprintf(&b, "\"%s\"=dword:%08x\r\n", v.key, uint32(v.num))
			} else {
				fmt.Fprintf(&b, "\"%s\"=\"%s\"\r\n", v.key, strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v.str))
			}
		}
		b.WriteString("\r\n")
		rep.Count++
	}
	// regedit expects UTF-16 LE with a BOM for "Version 5.00" files
	u := utf16.Encode([]rune(b.String()))
	out := make([]byte, 2, 2+2*len(u))
	out[0], out[1] = 0xFF, 0xFE
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out, nil
}

func exportPuTTYDir(list []Session, dest string, jump func(string) (Session, bool), rep *ExportReport, notes *exportNotes) error {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	for _, s := range list {
		vals, ok := puttyValues(s, false, jump, notes)
		if !ok {
			skipNote(s, rep, s.Protocol+" sessions are not supported")
			continue
		}
		var b strings.Builder
		for _, v := range vals {
			if v.isN {
				fmt.Fprintf(&b, "%s=%d\n", v.key, v.num)
			} else {
				fmt.Fprintf(&b, "%s=%s\n", v.key, v.str)
			}
		}
		if err := os.WriteFile(filepath.Join(dest, puttyEscapeFor(s.Path, true)), []byte(b.String()), 0o600); err != nil {
			return err
		}
		rep.Count++
	}
	return nil
}

// ---------- MobaXterm ----------

var mobaEscape = strings.NewReplacer("#", "__DIEZE__", ";", "__PTVIRG__", `"`, "__DBLQUO__", "|", "__PIPE__", "%", "__PERCENT__", "=", "__EGAL__")

// mobaTerm is MobaXterm's default terminal settings part of a bookmark.
const mobaTerm = "MobaFont%10%0%0%-1%15%236,236,236%30,30,30%180,180,192%0%-1%0%%xterm%-1%0%_Std_Colors_0_%80%24%0%1%-1%<none>%%0%0%-1"

func exportMoba(list []Session, jump func(string) (Session, bool), rep *ExportReport, notes *exportNotes) ([]byte, error) {
	byFolder := map[string][]string{}
	var folders []string
	for _, s := range list {
		e := mobaEscape.Replace
		var line string
		switch s.Protocol {
		case ProtoSSH2:
			f := make([]string, 30)
			f[0], f[1], f[2], f[3] = "0", e(s.Hostname), strconv.Itoa(portOr(s)), e(s.Username)
			f[5] = map[bool]string{true: "-1", false: "0"}[s.X11Forward]
			f[6] = "-1"
			if s.UseRemoteCommand {
				f[7] = e(s.RemoteCommand)
			}
			if s.JumpSession != "" {
				if j, ok := jump(s.JumpSession); ok && j.Protocol == ProtoSSH2 {
					f[8], f[9], f[10] = e(j.Hostname), strconv.Itoa(portOr(j)), e(j.Username)
					notes.add("jump host written as MobaXterm's SSH gateway")
				} else {
					notes.add("jump host not found - not exported")
				}
			}
			f[11], f[12], f[13] = "0", "0", "0"
			f[14] = e(s.IdentityFile)
			f[16] = "-1"
			f[17], f[18], f[19] = "0", "0", "0"
			f[21] = "1080"
			f[23], f[24], f[25] = "0", "0", "1"
			line = "#109#" + strings.Join(f[:26], "%")
			if len(s.PortForwards) > 0 {
				notes.add("port forwards are not exported to MobaXterm (use its MobaSSHTunnel)")
			}
		case ProtoTelnet:
			line = fmt.Sprintf("#98#1%%%s%%%d%%%%%%2%%%%%%%%%%0%%0%%%%1080%%", e(s.Hostname), portOr(s))
		case ProtoSerial:
			line = fmt.Sprintf("#131#8%%%s%%%d%%3%%0%%0%%1%%2%%0%%0%%%%1500%%0%%0", e(s.ComPort), s.BaudRate)
		default:
			skipNote(s, rep, s.Protocol+" sessions are not supported")
			continue
		}
		if s.ProxyType != "" {
			notes.add("proxy settings are not exported to MobaXterm")
		}
		desc := ""
		if len(s.Description) > 0 {
			desc = e(strings.Join(s.Description, " "))
		}
		folder := strings.ReplaceAll(s.Folder, "/", `\`)
		if _, ok := byFolder[folder]; !ok {
			folders = append(folders, folder)
		}
		byFolder[folder] = append(byFolder[folder], fmt.Sprintf("%s= %s#%s#0#%s#-1", e(s.Name), line, mobaTerm, desc))
		notes.termwrightOnly(s)
		rep.Count++
	}
	sort.Strings(folders)
	var b strings.Builder
	// the root section first (MobaXterm's "User sessions"), then one per folder
	b.WriteString("[Bookmarks]\r\nSubRep=\r\nImgNum=42\r\n")
	for _, l := range byFolder[""] {
		b.WriteString(l + "\r\n")
	}
	b.WriteString("\r\n")
	n := 0
	for _, f := range folders {
		if f == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "[Bookmarks_%d]\r\nSubRep=%s\r\nImgNum=41\r\n", n, mobaEscape.Replace(f))
		for _, l := range byFolder[f] {
			b.WriteString(l + "\r\n")
		}
		b.WriteString("\r\n")
	}
	return []byte(b.String()), nil
}

// ---------- Tabby ----------

type tabbyFwd struct {
	Type          string `yaml:"type"`
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	TargetAddress string `yaml:"targetAddress,omitempty"`
	TargetPort    int    `yaml:"targetPort,omitempty"`
	Description   string `yaml:"description,omitempty"`
}

type tabbyProfile struct {
	Type    string         `yaml:"type"`
	Name    string         `yaml:"name"`
	Group   string         `yaml:"group,omitempty"`
	ID      string         `yaml:"id"`
	Options map[string]any `yaml:"options"`
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func exportTabby(list []Session, rep *ExportReport, notes *exportNotes) ([]byte, error) {
	ids := map[string]string{}
	for _, s := range list {
		ids[s.Path] = "termwright-" + strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s.Path), "-"), "-")
	}
	var profiles []tabbyProfile
	for _, s := range list {
		p := tabbyProfile{Name: s.Name, Group: s.Folder, ID: ids[s.Path], Options: map[string]any{}}
		switch s.Protocol {
		case ProtoSSH2:
			p.Type = "ssh"
			p.ID = "ssh:custom:" + p.ID
			p.Options["host"], p.Options["port"], p.Options["user"] = s.Hostname, portOr(s), s.Username
			if s.IdentityFile != "" {
				p.Options["privateKeys"] = []string{s.IdentityFile}
			}
			if s.Keepalive && s.KeepaliveSec > 0 {
				p.Options["keepaliveInterval"] = s.KeepaliveSec * 1000
			}
			if s.X11Forward {
				p.Options["x11"] = true
			}
			if s.AgentForward {
				p.Options["agentForward"] = true
			}
			if s.JumpSession != "" {
				if id, ok := ids[s.JumpSession]; ok {
					p.Options["jumpHost"] = "ssh:custom:" + id
				} else {
					notes.add("jump host not in the export - add it to the export to keep the link")
				}
			}
			if s.ProxyType == "socks5" {
				p.Options["socksProxyHost"], p.Options["socksProxyPort"] = s.ProxyHost, s.ProxyPort
			} else if s.ProxyType == "http" {
				p.Options["httpProxyHost"], p.Options["httpProxyPort"] = s.ProxyHost, s.ProxyPort
			}
			var fw []tabbyFwd
			for _, f := range s.PortForwards {
				t := map[string]string{"local": "Local", "remote": "Remote", "dynamic": "Dynamic"}[f.Type]
				if t == "" || f.BindPort <= 0 {
					continue
				}
				bind := f.BindAddr
				if bind == "" {
					bind = "127.0.0.1"
				}
				fw = append(fw, tabbyFwd{Type: t, Host: bind, Port: f.BindPort, TargetAddress: f.DestHost, TargetPort: f.DestPort, Description: f.Name})
			}
			if len(fw) > 0 {
				p.Options["forwardedPorts"] = fw
			}
			if s.UseRemoteCommand && s.RemoteCommand != "" {
				notes.add("remote commands are not exported to Tabby (use its login scripts)")
			}
		case ProtoTelnet:
			p.Type = "telnet"
			p.ID = "telnet:custom:" + p.ID
			p.Options["host"], p.Options["port"] = s.Hostname, portOr(s)
		case ProtoSerial:
			p.Type = "serial"
			p.ID = "serial:custom:" + p.ID
			p.Options["port"], p.Options["baudrate"] = s.ComPort, s.BaudRate
			p.Options["databits"], p.Options["stopbits"] = s.DataBits, map[int]float64{0: 1, 1: 1.5, 2: 2}[s.StopBits]
			p.Options["parity"] = map[int]string{0: "none", 1: "odd", 2: "even", 3: "mark", 4: "space"}[s.Parity]
			p.Options["rtscts"], p.Options["xon"], p.Options["xoff"] = s.SerialFlow == "rtscts", s.SerialFlow == "xonxoff", s.SerialFlow == "xonxoff"
		default:
			skipNote(s, rep, s.Protocol+" sessions are not supported")
			continue
		}
		notes.termwrightOnly(s)
		profiles = append(profiles, p)
		rep.Count++
	}
	var buf bytes.Buffer
	buf.WriteString("# Tabby profiles exported by Termwright. Add the entries under \"profiles:\" to Tabby's\n" +
		"# config.yaml (Settings → Config file → Show config file), then restart Tabby.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(map[string]any{"profiles": profiles}); err != nil {
		return nil, err
	}
	return buf.Bytes(), enc.Close()
}

// ---------- OpenSSH config ----------

var aliasRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func exportSSHConfig(list []Session, rep *ExportReport, notes *exportNotes) ([]byte, error) {
	// short aliases (the session name), the folder added when names repeat
	count := map[string]int{}
	for _, s := range list {
		count[aliasRe.ReplaceAllString(s.Name, "-")]++
	}
	alias := map[string]string{}
	for _, s := range list {
		a := aliasRe.ReplaceAllString(s.Name, "-")
		if count[a] > 1 {
			a = aliasRe.ReplaceAllString(strings.ReplaceAll(s.Path, "/", "_"), "-")
		}
		alias[s.Path] = strings.Trim(a, "-")
	}
	var b strings.Builder
	b.WriteString("# OpenSSH client config exported by Termwright - add it to ~/.ssh/config\n# (or save it as ~/.ssh/termwright.conf and add \"Include termwright.conf\" at the top of ~/.ssh/config).\n\n")
	quote := func(v string) string {
		if strings.ContainsAny(v, " \t\"") {
			return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
		}
		return v
	}
	for _, s := range list {
		if s.Protocol != ProtoSSH2 {
			skipNote(s, rep, s.Protocol+" sessions are not SSH")
			continue
		}
		fmt.Fprintf(&b, "# %s\nHost %s\n  HostName %s\n", s.Path, alias[s.Path], s.Hostname)
		if p := portOr(s); p != 22 {
			fmt.Fprintf(&b, "  Port %d\n", p)
		}
		if s.Username != "" {
			fmt.Fprintf(&b, "  User %s\n", quote(s.Username))
		}
		if s.IdentityFile != "" {
			fmt.Fprintf(&b, "  IdentityFile %s\n", quote(s.IdentityFile))
			if strings.EqualFold(filepath.Ext(s.IdentityFile), ".ppk") {
				notes.add("OpenSSH cannot read .ppk keys: convert them with PuTTYgen (Export OpenSSH key)")
			}
		}
		if s.CertificateFile != "" {
			fmt.Fprintf(&b, "  CertificateFile %s\n", quote(s.CertificateFile))
		}
		if s.JumpSession != "" {
			if a, ok := alias[s.JumpSession]; ok {
				fmt.Fprintf(&b, "  ProxyJump %s\n", a)
			} else {
				notes.add("jump host not in the export - add it to the export to keep ProxyJump")
			}
		}
		if s.ProxyType != "" {
			notes.add("SOCKS / HTTP proxies are not exported (OpenSSH needs a ProxyCommand)")
		}
		if s.Keepalive && s.KeepaliveSec > 0 {
			fmt.Fprintf(&b, "  ServerAliveInterval %d\n", s.KeepaliveSec)
		}
		if s.AgentForward {
			b.WriteString("  ForwardAgent yes\n")
		}
		if s.X11Forward {
			b.WriteString("  ForwardX11 yes\n")
		}
		for _, f := range s.PortForwards {
			bind := strconv.Itoa(f.BindPort)
			if f.BindAddr != "" {
				bind = f.BindAddr + ":" + bind
			}
			switch f.Type {
			case "local":
				fmt.Fprintf(&b, "  LocalForward %s %s:%d\n", bind, f.DestHost, f.DestPort)
			case "remote":
				fmt.Fprintf(&b, "  RemoteForward %s %s:%d\n", bind, f.DestHost, f.DestPort)
			case "dynamic":
				fmt.Fprintf(&b, "  DynamicForward %s\n", bind)
			}
		}
		if s.UseRemoteCommand && s.RemoteCommand != "" {
			fmt.Fprintf(&b, "  RequestTTY yes\n  RemoteCommand %s\n", s.RemoteCommand)
		}
		if strings.Contains(strings.ToLower(s.AuthMethods), "gssapi") {
			b.WriteString("  GSSAPIAuthentication yes\n")
		}
		if s.SmartCard != "" {
			b.WriteString("  # smart card: add \"PKCS11Provider <your card's PKCS#11 library>\"\n")
		}
		b.WriteString("\n")
		notes.termwrightOnly(s)
		rep.Count++
	}
	return []byte(b.String()), nil
}

// ---------- CSV ----------

func exportCSV(list []Session, rep *ExportReport, notes *exportNotes) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf") // BOM: spreadsheets open it as UTF-8
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"name", "folder", "protocol", "host", "port", "username", "device_type", "environment", "jump_host",
		"identity_file", "com_port", "baud", "mac", "description", "account", "network", "files_panel"})
	for _, s := range list {
		host, port, com, baud := s.Hostname, strconv.Itoa(portOr(s)), "", ""
		if s.Protocol == ProtoSerial {
			host, port, com, baud = "", "", s.ComPort, strconv.Itoa(s.BaudRate)
		}
		if s.Protocol == ProtoLocal || s.Protocol == ProtoConsole || s.Protocol == ProtoWOL {
			port = ""
		}
		if s.Protocol == ProtoConsole {
			host = fmt.Sprintf("%s port %d", s.ConsoleServer, s.ConsolePort)
		}
		_ = w.Write([]string{s.Name, s.Folder, s.Protocol, host, port, s.Username, s.DeviceType, s.Environment, s.JumpSession,
			s.IdentityFile, com, baud, s.MAC, strings.Join(s.Description, " "), s.Account, s.WOLNetwork, map[bool]string{true: "yes"}[s.FilesPanel]})
		rep.Count++
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}
