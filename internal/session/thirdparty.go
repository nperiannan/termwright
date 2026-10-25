package session

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/termwright/termwright/internal/securecrt"
)

// Importers for other terminal programs:
//   - PuTTY / KiTTY: registry export (.reg, UTF-16 or ANSI) of
//     HKCU\Software\SimonTatham\PuTTY\Sessions (PuTTY Session Manager's
//     "PsmPath" and KiTTY's "Folder" values become folders), or the
//     ~/.putty/sessions files used on Linux/macOS.
//   - MobaXterm: exported .mxtsessions files (also MobaXterm.ini [Bookmarks]).

// decodeText converts UTF-16 (with BOM) or Windows-1252/Latin-1 bytes to UTF-8.
func decodeText(b []byte) string {
	switch {
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		u := make([]uint16, (len(b)-2)/2)
		for i := range u {
			u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
		}
		return string(utf16.Decode(u))
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		u := make([]uint16, (len(b)-2)/2)
		for i := range u {
			u[i] = uint16(b[2+2*i])<<8 | uint16(b[3+2*i])
		}
		return string(utf16.Decode(u))
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return string(b[3:])
	case utf8.Valid(b):
		return string(b)
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c) // Latin-1 approximation of Windows-1252
	}
	return string(r)
}

// sniffFormat recognises PuTTY and MobaXterm files by content.
func sniffFormat(text string) string {
	switch {
	case strings.Contains(text, "Windows Registry Editor") || strings.Contains(text, "REGEDIT4"):
		return "putty-reg"
	case strings.Contains(text, "[Bookmarks"):
		return "mobaxterm"
	case regexp.MustCompile(`(?m)^HostName=`).MatchString(text) && regexp.MustCompile(`(?m)^Protocol=`).MatchString(text):
		return "putty-file"
	}
	return ""
}

func (s *Store) importSessions(list []Session, opt ImportOptions, res *ImportResult) {
	for _, n := range list {
		f := securecrt.NewFile()
		n.Apply(f)
		s.importOne(JoinPath(n.Folder, n.Name), f, opt, res)
	}
}

// ---------- PuTTY ----------

// puttyUnescape decodes PuTTY's %XX escaping of session names.
func puttyUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// regValue parses `"text"` (with \\ and \" escapes) or `dword:0000001f`.
func regValue(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "dword:") {
		n, _ := strconv.ParseUint(strings.TrimPrefix(v, "dword:"), 16, 32)
		return strconv.FormatUint(n, 10)
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
		return strings.NewReplacer(`\\`, `\`, `\"`, `"`).Replace(v)
	}
	return v
}

var regKeyRe = regexp.MustCompile(`(?i)^\[HKEY_[A-Z_]+\\.*\\Sessions\\([^\\\]]+)\]$`)
var regValRe = regexp.MustCompile(`^"((?:[^"\\]|\\.)*)"=(.*)$`)

// ParsePuTTYReg reads a registry export of PuTTY (or KiTTY) sessions.
func ParsePuTTYReg(text string) []Session {
	var out []Session
	var name string
	var vals map[string]string
	flush := func() {
		if vals != nil {
			if s, ok := puttySession(name, vals); ok {
				out = append(out, s)
			}
		}
		vals = nil
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			flush()
			if m := regKeyRe.FindStringSubmatch(line); m != nil {
				name, vals = puttyUnescape(m[1]), map[string]string{}
			}
			continue
		}
		if vals == nil {
			continue
		}
		if m := regValRe.FindStringSubmatch(line); m != nil {
			vals[m[1]] = regValue(m[2])
		}
	}
	flush()
	return out
}

// ParsePuTTYFile reads one ~/.putty/sessions file (Key=Value lines).
func ParsePuTTYFile(name, text string) (Session, bool) {
	vals := map[string]string{}
	for _, l := range strings.Split(text, "\n") {
		if k, v, ok := strings.Cut(strings.TrimRight(l, "\r"), "="); ok {
			vals[k] = v
		}
	}
	return puttySession(puttyUnescape(name), vals)
}

func puttySession(name string, v map[string]string) (Session, bool) {
	if name == "" || strings.EqualFold(name, "Default Settings") {
		return Session{}, false
	}
	n := Defaults()
	num := func(k string) int { i, _ := strconv.Atoi(v[k]); return i }
	switch strings.ToLower(v["Protocol"]) {
	case "", "ssh":
		n.Protocol = ProtoSSH2
	case "telnet":
		n.Protocol = ProtoTelnet
	case "serial":
		n.Protocol = ProtoSerial
	case "raw":
		n.Protocol = ProtoRaw
	default: // rlogin, supdup, bare ssh connection...
		return Session{}, false
	}
	folder := v["PsmPath"]
	if folder == "" {
		folder = v["Folder"] // KiTTY
	}
	n.Folder = cleanFolder(folder)
	n.Name = strings.ReplaceAll(name, "/", "-")
	if i := strings.LastIndexAny(name, `\/`); i > 0 && n.Folder == "" {
		n.Folder, n.Name = cleanFolder(name[:i]), name[i+1:]
	}
	host := v["HostName"]
	if u, h, ok := strings.Cut(host, "@"); ok {
		n.Username, host = u, h
	}
	n.Hostname = host
	if u := v["UserName"]; u != "" {
		n.Username = u
	}
	if p := num("PortNumber"); p > 0 {
		n.Port = p
	} else {
		n.Port = defaultPortFor(n.Protocol)
	}
	n.IdentityFile = v["PublicKeyFile"]
	if t := v["TerminalType"]; t != "" && t != "xterm" {
		n.TermType = t
	}
	if c, r := num("TermWidth"), num("TermHeight"); c > 0 && r > 0 {
		n.Cols, n.Rows = c, r
	}
	if sb := num("ScrollbackLines"); sb > 0 {
		n.Scrollback = sb
	}
	if k := num("PingIntervalSecs"); k > 0 {
		n.Keepalive, n.KeepaliveSec = true, k
	}
	n.X11Forward = num("X11Forward") == 1
	n.AgentForward = num("AgentFwd") == 1
	if cmd := strings.TrimSpace(v["RemoteCommand"]); cmd != "" {
		n.RemoteCommand, n.UseRemoteCommand = cmd, true
	}
	if lf := v["LogFileName"]; lf != "" && lf != "putty.log" {
		n.LogFile = lf
	}
	if lt := num("LogType"); lt == 1 || lt == 2 {
		n.LogOnConnect, n.LogRaw = true, lt == 2
		n.LogAppend = num("LogFileClash") == 1
	}
	switch num("ProxyMethod") {
	case 2:
		n.ProxyType = "socks5"
	case 3:
		n.ProxyType = "http"
	}
	if n.ProxyType != "" {
		n.ProxyHost, n.ProxyPort, n.ProxyUser = v["ProxyHost"], num("ProxyPort"), v["ProxyUsername"]
	}
	n.PortForwards = parsePuTTYForwards(v["PortForwardings"])
	if n.Protocol == ProtoSerial {
		n.ComPort = v["SerialLine"]
		if b := num("SerialSpeed"); b > 0 {
			n.BaudRate = b
		}
		if d := num("SerialDataBits"); d > 0 {
			n.DataBits = d
		}
		switch num("SerialStopHalfbits") {
		case 3:
			n.StopBits = 1
		case 4:
			n.StopBits = 2
		}
		n.Parity = num("SerialParity") // same order: none, odd, even, mark, space
		switch num("SerialFlowControl") {
		case 1:
			n.SerialFlow = "xonxoff"
		case 2:
			n.SerialFlow = "rtscts"
		}
		n.Hostname, n.Port = "", 0
	}
	return n, n.Hostname != "" || n.ComPort != ""
}

// parsePuTTYForwards parses "L8080=localhost:80,R2222=10.0.0.5:22,D1080"
// (optionally prefixed with 4/6 and with a bind address, e.g. L127.0.0.1:8080=...).
func parsePuTTYForwards(s string) []PortForward {
	out := []PortForward{}
	for _, e := range strings.Split(s, ",") {
		e = strings.TrimSpace(e)
		e = strings.TrimLeft(e, "46")
		if len(e) < 2 {
			continue
		}
		var pf PortForward
		switch e[0] {
		case 'L':
			pf.Type = "local"
		case 'R':
			pf.Type = "remote"
		case 'D':
			pf.Type = "dynamic"
		default:
			continue
		}
		src, dst, _ := strings.Cut(e[1:], "=")
		if i := strings.LastIndex(src, ":"); i >= 0 {
			pf.BindAddr, src = src[:i], src[i+1:]
		}
		pf.BindPort, _ = strconv.Atoi(src)
		if dst != "" {
			if i := strings.LastIndex(dst, ":"); i >= 0 {
				pf.DestHost = strings.Trim(dst[:i], "[]")
				pf.DestPort, _ = strconv.Atoi(dst[i+1:])
			}
		}
		if pf.BindPort > 0 {
			pf.Name = e
			out = append(out, pf)
		}
	}
	return out
}

func defaultPortFor(proto string) int {
	switch proto {
	case ProtoTelnet:
		return 23
	case ProtoSSH2:
		return 22
	}
	return 0
}

func cleanFolder(f string) string {
	f = strings.ReplaceAll(f, `\`, "/")
	var parts []string
	for _, p := range strings.Split(f, "/") {
		if p = strings.TrimSpace(p); p != "" && p != "." && p != ".." {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}

// ---------- MobaXterm ----------

var mobaEscapes = strings.NewReplacer("__DIEZE__", "#", "__PTVIRG__", ";", "__DBLQUO__", `"`, "__PIPE__", "|", "__PERCENT__", "%", "__EGAL__", "=")

func mobaPath(p string) string {
	p = mobaEscapes.Replace(p)
	if strings.Contains(p, "_ProfileDir_") {
		h, _ := os.UserHomeDir()
		p = strings.ReplaceAll(p, "_ProfileDir_", h)
	}
	p = strings.ReplaceAll(p, "_CurrentDrive_:", "C:")
	if filepath.Separator == '/' {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	return p
}

var comRe = regexp.MustCompile(`(?i)\b(COM\d+|/dev/[\w./-]+)`)

// ParseMobaXterm reads a MobaXterm .mxtsessions export (or MobaXterm.ini).
// SSH (#109#), Telnet (#98#) and Serial (#131#) bookmarks are converted;
// other types (RDP, VNC, FTP, ...) are skipped.
func ParseMobaXterm(text string) (sessions []Session, skipped []string) {
	inBookmarks := false
	folder := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if strings.HasPrefix(line, "[") {
			inBookmarks = strings.HasPrefix(strings.ToLower(line), "[bookmarks")
			folder = ""
			continue
		}
		if !inBookmarks || line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "SubRep":
			folder = cleanFolder(mobaEscapes.Replace(val))
			continue
		case "ImgNum":
			continue
		}
		parts := strings.Split(val, "#")
		if len(parts) < 3 || parts[0] != "" {
			continue
		}
		name := mobaEscapes.Replace(strings.TrimSpace(key))
		f := strings.Split(parts[2], "%")
		get := func(i int) string {
			if i < len(f) {
				return mobaEscapes.Replace(strings.TrimSpace(f[i]))
			}
			return ""
		}
		n := Defaults()
		n.Name, n.Folder = strings.ReplaceAll(name, "/", "-"), folder
		if len(parts) > 4 {
			if c := strings.TrimSpace(mobaEscapes.Replace(parts[4])); c != "" {
				n.Description = []string{c}
			}
		}
		switch parts[1] {
		case "109": // SSH: 0%host%port%user%%x11%compr%cmd%gwhost%gwport%gwuser%...%privatekey%...
			n.Protocol = ProtoSSH2
			n.Hostname, n.Username = get(1), get(3)
			n.Port, _ = strconv.Atoi(get(2))
			if n.Port == 0 {
				n.Port = 22
			}
			if cmd := get(7); cmd != "" {
				n.RemoteCommand, n.UseRemoteCommand = cmd, true
			}
			if k := get(14); k != "" {
				n.IdentityFile = mobaPath(k)
			}
			n.X11Forward = get(5) == "-1"
			if gw := get(8); gw != "" {
				n.Description = append(n.Description, "MobaXterm SSH gateway: "+get(10)+"@"+gw+":"+get(9)+" (set a jump session)")
			}
		case "98": // Telnet: x%host%port%...
			n.Protocol = ProtoTelnet
			n.Hostname = get(1)
			n.Port, _ = strconv.Atoi(get(2))
			if n.Port == 0 {
				n.Port = 23
			}
		case "131": // Serial: x%COMn (description)%baud%...
			n.Protocol = ProtoSerial
			if m := comRe.FindString(parts[2]); m != "" {
				n.ComPort = m
			}
			for _, fv := range f {
				if b, err := strconv.Atoi(fv); err == nil && isBaud(b) {
					n.BaudRate = b
					break
				}
			}
			n.Port = 0
		default:
			skipped = append(skipped, JoinPath(folder, name))
			continue
		}
		if n.Hostname == "" && n.ComPort == "" {
			skipped = append(skipped, JoinPath(folder, name))
			continue
		}
		sessions = append(sessions, n)
	}
	return sessions, skipped
}

func isBaud(b int) bool {
	switch b {
	case 300, 1200, 2400, 4800, 9600, 14400, 19200, 38400, 57600, 115200, 230400, 460800, 921600:
		return true
	}
	return false
}

// importThirdParty imports a PuTTY or MobaXterm file; ok is false when the
// content is not one of those formats.
func (s *Store) importThirdParty(src string, data []byte, opt ImportOptions) (*ImportResult, bool) {
	text := decodeText(data)
	res := &ImportResult{}
	switch sniffFormat(text) {
	case "putty-reg":
		s.importSessions(ParsePuTTYReg(text), opt, res)
	case "mobaxterm":
		list, skipped := ParseMobaXterm(text)
		s.importSessions(list, opt, res)
		for _, k := range skipped {
			res.Skipped = append(res.Skipped, k+" (unsupported MobaXterm session type)")
		}
	case "putty-file":
		if n, ok := ParsePuTTYFile(filepath.Base(src), text); ok {
			s.importSessions([]Session{n}, opt, res)
		}
	default:
		var list []Session
		var err error
		switch sniffBulk(src, text) {
		case "csv":
			list, err = ParseCSV([]byte(text))
		case "ansible-yaml":
			list, err = ParseAnsibleYAML([]byte(text))
		case "ansible-ini":
			list = ParseAnsibleINI(text)
		default:
			return nil, false
		}
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			return res, true
		}
		s.importSessions(list, opt, res)
	}
	return res, true
}

// importPuTTYDir imports a ~/.putty/sessions directory; ok is false when the
// directory does not look like one.
func (s *Store) importPuTTYDir(dir string, opt ImportOptions) (*ImportResult, bool) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	var list []Session
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || !bytes.Contains(data, []byte("HostName=")) {
			continue
		}
		if n, ok := ParsePuTTYFile(e.Name(), decodeText(data)); ok {
			list = append(list, n)
		}
	}
	if len(list) == 0 {
		return nil, false
	}
	res := &ImportResult{}
	s.importSessions(list, opt, res)
	return res, true
}
