package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/term"
)

// The read-only script "Collect Remote Desktop Logs" runs on the Linux machine.
//
//go:embed scripts/remote-desktop/diagnose-remote-desktop.sh
var diagScript []byte

// CollectRemoteDesktopLogs builds a report for finding out why an RDP or VNC
// session does not connect, from the Session Manager:
//
//   - an RDP or VNC session: a connection test from this computer (through the
//     jump host, as when the session is opened) with the first bytes of the
//     RDP / VNC handshake, and the state and logs of that one service on the
//     machine, read over the SSH session the desktop is reached through;
//   - an SSH session: the state and logs of both services on that machine.
//
// Nothing is changed on the machine and no password is in the report. sudo is
// the password sudo may ask for (empty when it needs none); it is sent on the
// command's standard input and never shown.
func (a *App) CollectRemoteDesktopLogs(path, sudo string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	s, err := a.store.Get(path)
	if err != nil {
		return "", err
	}
	s = a.withFolderDefaults(s)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	id := randHex(8)
	rep := &diagReport{secrets: []string{sudo}}
	rep.addf("Termwright remote desktop log report")
	rep.addf("Created:    %s", time.Now().Format("2006-01-02 15:04:05 -0700"))
	rep.addf("Termwright: %s (%s/%s)", Version, runtime.GOOS, runtime.GOARCH)
	rep.addf("Session:    %s (%s)", s.Name, s.Protocol)

	kind := strings.ToUpper(s.Protocol)
	var rdpPort, vncPort int
	var rdpUser, vncUser string
	var hop *session.Session // the SSH session commands run on
	var tu *term.Tunnel

	switch kind {
	case "RDP", "VNC":
		port := s.Port
		if port == 0 {
			port = map[string]int{"RDP": 3389, "VNC": 5900}[kind]
		}
		if kind == "RDP" {
			rdpPort, rdpUser = port, s.Username
		} else {
			vncPort, vncUser = port, s.Username
		}
		target := net.JoinHostPort(s.Hostname, strconv.Itoa(port))
		rep.addf("Address:    %s", target)
		rep.addf("User:       %s", s.Username)
		tu, err = a.terms.OpenTunnel(ctx, s, id, target)
		if err != nil {
			rep.section("From this computer")
			rep.addf("could not prepare the connection: %v", err)
			rep.hint(hintTunnel(err))
		} else {
			defer tu.Close()
			rep.addf("Route:      %s", firstNonEmpty(tu.Via, "direct"))
			rep.section("From this computer: connection test")
			rep.lines(probeRemote(ctx, tu, kind))
		}
		if s.JumpSession == "" {
			if h := a.sshOfMachine(s.Hostname); h != nil {
				hop = h
			}
		}
	case session.ProtoSSH2:
		hop = &s
		rdpPort, vncPort = 3389, 5902
		for _, n := range a.remoteDesktopsOf(s) {
			switch strings.ToUpper(n.Protocol) {
			case "RDP":
				rdpPort, rdpUser = firstPort(n.Port, 3389), n.Username
			case "VNC":
				vncPort, vncUser = firstPort(n.Port, 5902), n.Username
			}
		}
		rep.addf("Address:    %s:%d", s.Hostname, s.Port)
	default:
		return "", fmt.Errorf("%s is not an RDP, VNC or SSH2 session", s.Protocol)
	}

	rep.section("On the machine")
	args := diagArgs(rdpPort, vncPort, rdpUser, vncUser)
	cmd := "sh -c " + term.ShellQuote(`IFS= read -r TW_SUDO; export TW_SUDO; exec bash -s -- "$@"`) + " sh " + args
	stdin := []byte(sudo + "\n" + string(diagScript))
	on := func(line string) { rep.addRaw(line) }
	var code int
	switch {
	case tu != nil && tu.CanExec():
		rep.addf("(read over the SSH session %s, the jump host of this session)", s.JumpSession)
		code, err = tu.Exec(ctx, cmd, stdin, on)
	case hop != nil:
		rep.addf("(read over the SSH session %s)", hop.Path)
		code, err = a.terms.ExecOn(ctx, id, *hop, cmd, stdin, on)
	default:
		err = errors.New("no SSH session is known for this machine, so its logs cannot be read from here. Open the SSH session of the machine and choose Collect Remote Desktop Logs on it")
	}
	if err != nil {
		rep.addf("could not read the machine: %v", err)
	} else if code != 0 {
		rep.addf("(the script ended with status %d)", code)
	}
	return rep.String(), nil
}

// ---- report ----

type diagReport struct {
	b       strings.Builder
	secrets []string
}

func (r *diagReport) mask(s string) string {
	for _, x := range r.secrets {
		if len(x) >= 3 {
			s = strings.ReplaceAll(s, x, "••••")
		}
	}
	return s
}
func (r *diagReport) addRaw(s string)         { r.b.WriteString(r.mask(s) + "\n") }
func (r *diagReport) addf(f string, a ...any) { r.addRaw(fmt.Sprintf(f, a...)) }
func (r *diagReport) lines(l []string) {
	for _, s := range l {
		r.addRaw(s)
	}
}
func (r *diagReport) section(t string) { r.b.WriteString("\n== " + t + " ==\n") }
func (r *diagReport) hint(h string) {
	if h != "" {
		r.addRaw("=> " + h)
	}
}
func (r *diagReport) String() string { return r.b.String() }
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func firstPort(p, def int) int {
	if p > 0 {
		return p
	}
	return def
}

func diagArgs(rdpPort, vncPort int, rdpUser, vncUser string) string {
	var q []string
	if rdpPort > 0 {
		q = append(q, "--rdp", strconv.Itoa(rdpPort))
	}
	if vncPort > 0 {
		q = append(q, "--vnc", strconv.Itoa(vncPort))
	}
	if rdpUser != "" && rdpPort > 0 {
		q = append(q, "--rdp-user", rdpUser)
	}
	if vncUser != "" && vncPort > 0 {
		q = append(q, "--vnc-user", vncUser)
	}
	for i := range q {
		q[i] = term.ShellQuote(q[i])
	}
	return strings.Join(q, " ")
}

// hintTunnel says what an error of the jump host connection usually means.
func hintTunnel(err error) string {
	m := strings.ToLower(err.Error())
	switch {
	case strings.Contains(m, "administratively prohibited"), strings.Contains(m, "open failed"):
		return "the SSH server refuses port forwarding: set AllowTcpForwarding yes in its sshd_config (or choose \"From the network\" in Set Up Remote Desktop)"
	case strings.Contains(m, "connection refused"):
		return "nothing listens on that port of the machine: the service is not running or uses another port"
	case strings.Contains(m, "timeout"), strings.Contains(m, "timed out"):
		return "no answer: a firewall drops the connection, or the address is not reachable"
	case strings.Contains(m, "unable to authenticate"), strings.Contains(m, "no supported methods"):
		return "the SSH login to the jump host failed"
	}
	return ""
}

// ---- finding the SSH session of a machine, and the desktops of an SSH session ----

func walkNodes(n *session.Node, fn func(*session.Node)) {
	if n == nil {
		return
	}
	if !n.IsFolder {
		fn(n)
	}
	for _, c := range n.Children {
		walkNodes(c, fn)
	}
}

// sshOfMachine is an SSH2 session to the host, for a session without a jump host.
func (a *App) sshOfMachine(host string) *session.Session {
	root, err := a.store.Tree()
	if err != nil || host == "" || host == "127.0.0.1" || host == "localhost" {
		return nil
	}
	var found string
	walkNodes(root, func(n *session.Node) {
		if found == "" && n.Protocol == session.ProtoSSH2 && strings.EqualFold(n.Hostname, host) && n.OpenAs == "" {
			found = n.Path
		}
	})
	if found == "" {
		return nil
	}
	s, err := a.store.Get(found)
	if err != nil {
		return nil
	}
	s = a.withFolderDefaults(s)
	return &s
}

// remoteDesktopsOf lists the RDP and VNC sessions that use ssh as jump host or sit on its address.
func (a *App) remoteDesktopsOf(ssh session.Session) []*session.Node {
	root, err := a.store.Tree()
	if err != nil {
		return nil
	}
	var out []*session.Node
	walkNodes(root, func(n *session.Node) {
		p := strings.ToUpper(n.Protocol)
		if p != "RDP" && p != "VNC" {
			return
		}
		if n.Jump == ssh.Path || n.Jump == ssh.Name || (n.Jump == "" && strings.EqualFold(n.Hostname, ssh.Hostname)) {
			out = append(out, n)
		}
	})
	return out
}

// ---- connection test from this computer ----

var vncSecurity = map[byte]string{1: "None", 2: "VNC Authentication", 5: "RA2", 6: "RA2ne", 16: "Tight", 17: "Ultra", 18: "TLS", 19: "VeNCrypt", 22: "XVP", 30: "Apple Remote Desktop"}

// the types Termwright's VNC viewer (noVNC) can log in with
var vncViewerKnows = map[byte]bool{1: true, 2: true, 16: true, 19: true, 22: true, 30: true}

func probeRemote(ctx context.Context, tu *term.Tunnel, kind string) []string {
	start := time.Now()
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c, err := tu.Dial(dctx)
	if err != nil {
		return []string{fmt.Sprintf("connect:  FAILED after %d ms: %v", time.Since(start).Milliseconds(), err), "=> " + firstNonEmpty(hintTunnel(err), "the connection to the machine's port could not be opened")}
	}
	defer c.Close()
	out := []string{fmt.Sprintf("connect:  OK in %d ms", time.Since(start).Milliseconds())}
	if kind == "VNC" {
		return append(out, probeVNC(c)...)
	}
	return append(out, probeRDP(c)...)
}

func probeVNC(c net.Conn) []string {
	_ = c.SetDeadline(time.Now().Add(6 * time.Second))
	banner := make([]byte, 12)
	if _, err := readFull(c, banner); err != nil {
		return []string{"handshake: the server sent nothing (" + err.Error() + ")", "=> something accepts the connection but is not a VNC server, or it closed at once"}
	}
	out := []string{"handshake: server says " + strings.TrimSpace(string(banner))}
	if !strings.HasPrefix(string(banner), "RFB ") {
		return append(out, "=> this is not a VNC server (it should say RFB 003.008)")
	}
	if _, err := c.Write([]byte("RFB 003.008\n")); err != nil {
		return append(out, "handshake: could not answer ("+err.Error()+")")
	}
	n := make([]byte, 1)
	if _, err := readFull(c, n); err != nil {
		return append(out, "security:  the server closed the connection after the version ("+err.Error()+")")
	}
	if n[0] == 0 {
		return append(out, "security:  the server refuses the connection")
	}
	types := make([]byte, n[0])
	if _, err := readFull(c, types); err != nil {
		return append(out, "security:  incomplete list ("+err.Error()+")")
	}
	var names []string
	ok := false
	for _, t := range types {
		nm := vncSecurity[t]
		if nm == "" {
			nm = "type " + strconv.Itoa(int(t))
		}
		names = append(names, fmt.Sprintf("%s (%d)", nm, t))
		ok = ok || vncViewerKnows[t]
	}
	out = append(out, "security:  the server offers "+strings.Join(names, ", "))
	if ok {
		return append(out, "=> Termwright's viewer can use one of these: the server is reachable and answers correctly, so a failure after this is the password or the desktop session")
	}
	return append(out, "=> Termwright's viewer cannot use any of these security types")
}

// the first message of an RDP connection (X.224 connection request asking for TLS or NLA)
var rdpRequest = []byte{0x03, 0x00, 0x00, 0x13, 0x0e, 0xe0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x08, 0x00, 0x03, 0x00, 0x00, 0x00}

func probeRDP(c net.Conn) []string {
	_ = c.SetDeadline(time.Now().Add(6 * time.Second))
	if _, err := c.Write(rdpRequest); err != nil {
		return []string{"handshake: could not send the RDP request (" + err.Error() + ")"}
	}
	buf := make([]byte, 19)
	n, err := readFull(c, buf)
	if err != nil && n < 11 {
		return []string{"handshake: no RDP answer (" + err.Error() + ")", "=> the port accepts connections but does not speak RDP, or closed at once"}
	}
	return parseRDPAnswer(buf[:n])
}

func parseRDPAnswer(b []byte) []string {
	if len(b) < 6 || b[0] != 3 {
		return []string{"handshake: the answer is not RDP"}
	}
	if b[5] != 0xd0 {
		return []string{fmt.Sprintf("handshake: unexpected RDP answer (type 0x%02x)", b[5])}
	}
	out := []string{"handshake: RDP connection confirmed"}
	if len(b) >= 19 {
		v := uint32(b[15]) | uint32(b[16])<<8 | uint32(b[17])<<16 | uint32(b[18])<<24
		switch b[11] {
		case 0x02:
			names := map[uint32]string{0: "standard RDP security", 1: "TLS", 2: "NLA (CredSSP)", 8: "RDSTLS"}
			out = append(out, "security:  the server chose "+firstNonEmpty(names[v], fmt.Sprintf("0x%x", v)))
		case 0x03:
			out = append(out, fmt.Sprintf("security:  the server refused the security methods offered (code 0x%x)", v))
		}
	}
	return append(out, "=> the RDP service answers; a failure after this is the login or the desktop session")
}

func readFull(c net.Conn, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := c.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
