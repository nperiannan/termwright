package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/termwright/termwright/internal/session"
)

// Console server access methods: how a port (line) of a console server is
// reached from the console server's own session.
var consoleMethods = map[string]struct {
	proto string
	base  int    // TCP port = base + port number (0 = the server session's port)
	user  string // user name template
}{
	"opengear-ssh": {session.ProtoSSH2, 0, "{user}:port{nn}"},   // Opengear: ssh admin:port05@cs
	"avocent-ssh":  {session.ProtoSSH2, 0, "{user}:ttyS{port}"}, // Avocent ACS: ssh admin:ttyS5@acs
	"ssh-3000":     {session.ProtoSSH2, 3000, "{user}"},         // Opengear, Lantronix, Perle: SSH to 3000+N
	"telnet-2000":  {session.ProtoTelnet, 2000, ""},             // Opengear, Lantronix, Cisco reverse Telnet: 2000+N
	"telnet-7000":  {session.ProtoTelnet, 7000, ""},             // Avocent / Cyclades: 7000+N
}

// consoleSession turns a Console session (a port of a console server) into
// the SSH or Telnet session that reaches it: host, jump host, proxy, keys and
// the SSH login come from the console server's session; the device type and
// the session's own username log in to the device behind the port.
func (a *App) consoleSession(s session.Session) (session.Session, error) {
	if s.Protocol != session.ProtoConsole {
		return s, nil
	}
	if s.ConsoleServer == "" {
		return s, errors.New("choose the console server (Session Options → Connection)")
	}
	if s.ConsolePort <= 0 {
		return s, errors.New("set the console server port number (Session Options → Connection)")
	}
	cs, err := a.store.Get(s.ConsoleServer)
	if err != nil {
		return s, fmt.Errorf("console server %q: %w", s.ConsoleServer, err)
	}
	cs = a.withFolderDefaults(cs)
	return consoleOf(s, cs)
}

// consoleOf is consoleSession without the session store (tested on its own).
func consoleOf(s, cs session.Session) (session.Session, error) {
	if cs.Protocol != session.ProtoSSH2 && cs.Protocol != session.ProtoTelnet {
		return s, fmt.Errorf("console server %s must be an SSH2 or Telnet session", cs.Name)
	}
	m, ok := consoleMethods[s.ConsoleMethod]
	if s.ConsoleMethod == "custom" || !ok {
		m.proto, m.base, m.user = s.ConsoleProtocol, s.ConsoleBase, s.ConsoleUser
		if m.proto == "" {
			m.proto = cs.Protocol
		}
		if m.user == "" && m.proto == session.ProtoSSH2 {
			m.user = "{user}"
		}
	}
	n := s.ConsolePort
	out := s
	out.Protocol = m.proto
	out.Hostname = cs.Hostname
	out.Port = cs.Port
	if m.base > 0 {
		out.Port = m.base + n
	} else if out.Protocol != cs.Protocol || out.Port == 0 {
		out.Port = map[string]int{session.ProtoSSH2: 22, session.ProtoTelnet: 23}[out.Protocol]
	}
	out.JumpSession, out.ProxyType, out.ProxyHost, out.ProxyPort, out.ProxyUser = cs.JumpSession, cs.ProxyType, cs.ProxyHost, cs.ProxyPort, cs.ProxyUser
	out.ConsoleServer = cs.Path // marks the session as a console port (device login steps run over SSH too)
	if out.Protocol == session.ProtoSSH2 {
		out.IdentityFile, out.AuthMethods, out.KerberosPrincipal, out.CertificateFile = cs.IdentityFile, cs.AuthMethods, cs.KerberosPrincipal, cs.CertificateFile
		r := strings.NewReplacer("{user}", cs.Username, "{port}", strconv.Itoa(n), "{nn}", fmt.Sprintf("%02d", n))
		out.TransportUser = r.Replace(m.user)
		out.TransportPath = cs.Path
		out.OpenAs, out.UseRemoteCommand, out.RemoteCommand = "", false, ""
	}
	out.InitialCR = true // wake the console
	return out, nil
}
