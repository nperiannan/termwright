package session

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/termwright/termwright/internal/securecrt"
)

// SuperPuTTY keeps its sessions in Sessions.xml:
//
//	<ArrayOfSessionData>
//	  <SessionData SessionId="Folder/Sub/name" SessionName="name" Host="10.0.0.1" Port="22"
//	    Proto="SSH" PuttySession="Default Settings" Username="admin" ExtraArgs="-pw secret -i key.ppk" … />
//	</ArrayOfSessionData>

type superPuTTYData struct {
	SessionID    string `xml:"SessionId,attr"`
	SessionName  string `xml:"SessionName,attr"`
	Host         string `xml:"Host,attr"`
	Port         string `xml:"Port,attr"`
	Proto        string `xml:"Proto,attr"`
	PuttySession string `xml:"PuttySession,attr"`
	Username     string `xml:"Username,attr"`
	ExtraArgs    string `xml:"ExtraArgs,attr"`
}

// isSuperPuTTY reports whether data is a SuperPuTTY Sessions.xml.
func isSuperPuTTY(data []byte) bool {
	head := data
	if len(head) > 4096 {
		head = head[:4096]
	}
	return bytes.Contains(head, []byte("<ArrayOfSessionData"))
}

// SuperPuTTYSession is a parsed SuperPuTTY session with the password given by
// "-pw" in its extra arguments ("" = none).
type SuperPuTTYSession struct {
	Session  Session
	Password string
}

// ParseSuperPuTTY reads a SuperPuTTY Sessions.xml. Sessions of types that
// Termwright does not have (RLogin, Cygterm, Mintty, VNC…) are listed in skipped.
func ParseSuperPuTTY(data []byte) (list []SuperPuTTYSession, skipped []string, err error) {
	var doc struct {
		Sessions []superPuTTYData `xml:"SessionData"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("SuperPuTTY sessions: %w", err)
	}
	for _, d := range doc.Sessions {
		id := strings.Trim(strings.ReplaceAll(d.SessionID, `\`, "/"), "/")
		name := strings.TrimSpace(d.SessionName)
		folder := ""
		if i := strings.LastIndex(id, "/"); i > 0 {
			folder = id[:i]
			if name == "" {
				name = id[i+1:]
			}
		} else if name == "" {
			name = id
		}
		if name == "" {
			continue
		}
		n := Defaults()
		n.Folder, n.Name = cleanFolder(folder), strings.ReplaceAll(name, "/", "-")
		port, _ := strconv.Atoi(strings.TrimSpace(d.Port))
		switch strings.ToLower(strings.TrimSpace(d.Proto)) {
		case "ssh", "":
			n.Protocol = ProtoSSH2
		case "telnet":
			n.Protocol = ProtoTelnet
		case "raw":
			n.Protocol = ProtoRaw
		case "serial":
			n.Protocol = ProtoSerial
			n.ComPort = strings.TrimSpace(d.Host)
			if port > 0 {
				n.BaudRate = port
			}
		default:
			skipped = append(skipped, JoinPath(n.Folder, n.Name)+" ("+d.Proto+" sessions are not supported)")
			continue
		}
		if n.Protocol != ProtoSerial {
			host := strings.TrimSpace(d.Host)
			if u, h, ok := strings.Cut(host, "@"); ok {
				n.Username, host = u, h
			}
			n.Hostname = host
			if port > 0 {
				n.Port = port
			} else {
				n.Port = defaultPortFor(n.Protocol)
			}
		}
		if u := strings.TrimSpace(d.Username); u != "" {
			n.Username = u
		}
		pw := ""
		args := splitArgs(d.ExtraArgs)
		for i := 0; i < len(args); i++ {
			next := func() string {
				if i+1 < len(args) {
					i++
					return args[i]
				}
				return ""
			}
			switch args[i] {
			case "-pw":
				pw = next()
			case "-i":
				n.IdentityFile = next()
			case "-l":
				if u := next(); u != "" && n.Username == "" {
					n.Username = u
				}
			case "-P":
				if p, err := strconv.Atoi(next()); err == nil && p > 0 && n.Protocol != ProtoSerial {
					n.Port = p
				}
			case "-A":
				n.AgentForward = true
			case "-X":
				n.X11Forward = true
			}
		}
		if ps := strings.TrimSpace(d.PuttySession); ps != "" && !strings.EqualFold(ps, "Default Settings") {
			n.Description = append(n.Description, "SuperPuTTY: uses the PuTTY session \""+ps+"\" for its settings")
		}
		if pw != "" {
			n.SavePassword = true
		}
		list = append(list, SuperPuTTYSession{Session: n, Password: pw})
	}
	return list, skipped, nil
}

// splitArgs splits a command line, keeping "quoted parts" together.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	in, quote, has := false, rune(0), false
	for _, r := range s {
		switch {
		case in && r == quote:
			in = false
		case !in && (r == '"' || r == '\''):
			in, quote, has = true, r, true
		case !in && (r == ' ' || r == '\t'):
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

// importSuperPuTTY imports a SuperPuTTY Sessions.xml; "-pw" passwords go to
// the OS keychain (through res.Passwords), never into the session files.
func (s *Store) importSuperPuTTY(data []byte, opt ImportOptions) (*ImportResult, error) {
	list, skipped, err := ParseSuperPuTTY(data)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{Skipped: skipped}
	for _, it := range list {
		f := securecrt.NewFile()
		it.Session.Apply(f)
		dest := s.importOne(JoinPath(it.Session.Folder, it.Session.Name), f, opt, res)
		if dest != "" && it.Password != "" {
			if res.Passwords == nil {
				res.Passwords = map[string]string{}
			}
			res.Passwords[dest] = it.Password
		}
	}
	return res, nil
}
