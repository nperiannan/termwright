package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/term"
)

// The read-only script "Diagnose Remote Desktop" runs on the Linux machine.
//
//go:embed scripts/remote-desktop/diagnose-remote-desktop.sh
var diagScript []byte

// The script "Restart service" runs on the Linux machine.
//
//go:embed scripts/remote-desktop/restart-remote-desktop.sh
var restartScript []byte

// DiagFinding is one line of the result of the diagnosis.
type DiagFinding struct {
	Level  string `json:"level"`  // "error", "warn", "info" or "ok"
	Text   string `json:"text"`   // what is wrong, in one line
	Detail string `json:"detail"` // what to do about it
}

// DiagResult is what Diagnose returns: the findings (the problems, the most
// important first), the checks (everything looked at, good or bad), the service whose restart is the fix ("" when none), and the full
// report with the findings at its top.
type DiagResult struct {
	Findings []DiagFinding `json:"findings"`
	Checks   []DiagFinding `json:"checks"` // everything that was checked, good or bad, for the details
	Fix      string        `json:"fix"`
	Report   string        `json:"report"`
}

// DiagnoseRemoteDesktop finds out why an RDP or VNC session does not connect,
// from the Session Manager, and says so in a few lines; the report behind it
// is the evidence:
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
func (a *App) DiagnoseRemoteDesktop(path, sudo string) (*DiagResult, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	s, err := a.store.Get(path)
	if err != nil {
		return nil, err
	}
	s = a.withFolderDefaults(s)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	id := randHex(8)
	rep := &diagReport{secrets: []string{sudo}}
	rep.addf("Termwright remote desktop diagnosis")
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
		return nil, fmt.Errorf("%s is not an RDP, VNC or SSH2 session", s.Protocol)
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
		err = errors.New("no SSH session is known for this machine, so it cannot be examined from here. Open the SSH session of the machine and choose Diagnose Remote Desktop on it")
	}
	if err != nil {
		rep.addf("could not read the machine: %v", err)
	} else if code != 0 {
		rep.addf("(the script ended with status %d)", code)
	}
	body := rep.String()
	finds, fix := analyze(body, kind)
	return &DiagResult{Findings: finds, Checks: checksOf(body), Fix: fix, Report: withResult(body, finds)}, nil
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

// vncRefusal reads the reason a VNC server gives when it refuses a connection:
// after a "0 security types" byte (RFB 3.7+), or after a 4-byte "0 = failed" (RFB 3.3).
func vncRefusal(c net.Conn, old bool) string {
	if old { // the first byte of the 4-byte security type was read already
		if _, err := readFull(c, make([]byte, 3)); err != nil {
			return ""
		}
	}
	l := make([]byte, 4)
	if _, err := readFull(c, l); err != nil {
		return ""
	}
	n := int(l[0])<<24 | int(l[1])<<16 | int(l[2])<<8 | int(l[3])
	if n <= 0 || n > 200 {
		return ""
	}
	b := make([]byte, n)
	if _, err := readFull(c, b); err != nil {
		return ""
	}
	return string(b)
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
		reason := vncRefusal(c, strings.HasPrefix(string(banner), "RFB 003.003"))
		out = append(out, "security:  the server refuses the connection"+map[bool]string{true: ": " + reason, false: ""}[reason != ""])
		if strings.Contains(strings.ToLower(reason), "too many") {
			out = append(out,
				"=> TigerVNC has blocked this address after several wrong passwords (it answers RFB 003.003 only to a blocked address).",
				"   Through the SSH session every login comes from 127.0.0.1, so this blocks every login until the time runs out",
				"   (10 s after the first block, doubling with every further wrong attempt) or the VNC service is restarted:",
				"   sudo systemctl restart tigervnc@<display>. Then connect once with the right password: the saved password probably differs",
				"   from the one set on the machine; run Set Up Remote Desktop with \"Reconfigure services that already run\" to set it again.")
		}
		return out
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

// ---- restart a service ----

// restartUnits are the systemd units of a remote desktop service: xrdp, or the TigerVNC display of the port.
func restartUnits(kind string, port int) ([]string, error) {
	switch kind {
	case "RDP":
		return []string{"xrdp.service", "xrdp-sesman.service"}, nil
	case "VNC":
		if port < 5901 || port > 5999 {
			return nil, fmt.Errorf("port %d is not a VNC display port (5901 to 5999), so the service cannot be named", port)
		}
		return []string{fmt.Sprintf("tigervnc@%d.service", port-5900)}, nil
	}
	return nil, fmt.Errorf("%q is not RDP or VNC", kind)
}

// RestartRemoteDesktopService restarts the xrdp or TigerVNC service of an RDP,
// VNC or SSH2 session's machine (which = "RDP" or "VNC"; for an RDP or VNC
// session its own protocol), over the SSH session the desktop is reached
// through. It repairs a service that hangs and clears the address TigerVNC
// blocks after wrong passwords. Anyone connected is disconnected, so the
// caller asks first. sudo is the password sudo may ask for (empty when it needs
// none); it goes to the command's standard input and is never shown.
func (a *App) RestartRemoteDesktopService(path, which, sudo string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	s, err := a.store.Get(path)
	if err != nil {
		return "", err
	}
	s = a.withFolderDefaults(s)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id := randHex(8)

	kind := strings.ToUpper(s.Protocol)
	var port int
	var hop *session.Session
	var tu *term.Tunnel
	switch kind {
	case "RDP", "VNC":
		which = kind
		port = firstPort(s.Port, map[string]int{"RDP": 3389, "VNC": 5900}[kind])
		if s.JumpSession != "" {
			tu, err = a.terms.OpenTunnel(ctx, s, id, net.JoinHostPort(s.Hostname, strconv.Itoa(port)))
			if err != nil {
				return "", fmt.Errorf("could not reach the machine through its jump host: %w", err)
			}
			defer tu.Close()
		} else {
			hop = a.sshOfMachine(s.Hostname)
		}
	case session.ProtoSSH2:
		which = strings.ToUpper(which)
		port = map[string]int{"RDP": 3389, "VNC": 5902}[which]
		for _, n := range a.remoteDesktopsOf(s) {
			if strings.ToUpper(n.Protocol) == which {
				port = firstPort(n.Port, port)
			}
		}
		hop = &s
	default:
		return "", fmt.Errorf("%s is not an RDP, VNC or SSH2 session", s.Protocol)
	}
	units, err := restartUnits(which, port)
	if err != nil {
		return "", err
	}
	q := make([]string, len(units))
	for i, u := range units {
		q[i] = term.ShellQuote(u)
	}
	cmd := "sh -c " + term.ShellQuote(`IFS= read -r TW_SUDO; export TW_SUDO; exec bash -s -- "$@"`) + " sh " + strings.Join(q, " ")
	stdin := []byte(sudo + "\n" + string(restartScript))
	rep := &diagReport{secrets: []string{sudo}}
	on := func(line string) { rep.addRaw(line) }
	var code int
	switch {
	case tu != nil && tu.CanExec():
		code, err = tu.Exec(ctx, cmd, stdin, on)
	case hop != nil:
		code, err = a.terms.ExecOn(ctx, id, *hop, cmd, stdin, on)
	default:
		return "", errors.New("no SSH session is known for this machine. Open the SSH session of the machine and restart the service there")
	}
	if err != nil {
		return rep.String(), fmt.Errorf("could not run the restart on the machine: %w", err)
	}
	if code != 0 {
		rep.addf("(the restart ended with status %d)", code)
	}
	return rep.String(), nil
}

// ---- the findings ----

// withResult puts the findings at the top of the report, under its title lines.
func withResult(body string, finds []DiagFinding) string {
	var b strings.Builder
	b.WriteString("== Result ==\n")
	tag := map[string]string{"error": "PROBLEM", "warn": "CHECK", "info": "NOTE", "ok": "OK"}
	for _, f := range finds {
		b.WriteString(tag[f.Level] + ": " + f.Text + "\n")
		if f.Detail != "" {
			b.WriteString("    " + f.Detail + "\n")
		}
	}
	res := b.String()
	if i := strings.Index(body, "\n\n== "); i >= 0 {
		return body[:i] + "\n\n" + res + body[i:]
	}
	return res + "\n" + body
}

// analyze reads the report and names what is wrong in a few lines, the most
// important first, and the service (RDP or VNC) whose restart may fix it. With
// nothing wrong it says what was checked.
func analyze(report, kind string) (finds []DiagFinding, fix string) {
	lines := strings.Split(report, "\n")
	add := func(level, text, detail string) { finds = append(finds, DiagFinding{level, text, detail}) }
	has := func(sub string) bool { return strings.Contains(report, sub) }
	// the lines of the section whose title starts with prefix
	section := func(prefix string) []string {
		var out []string
		in := false
		for _, l := range lines {
			if strings.HasPrefix(l, "== ") {
				in = strings.HasPrefix(l, "== "+prefix)
				continue
			}
			if in {
				out = append(out, l)
			}
		}
		return out
	}
	find := func(ls []string, sub string) string {
		for _, l := range ls {
			if strings.Contains(l, sub) {
				return l
			}
		}
		return ""
	}
	arrow := func(ls []string) string {
		for _, l := range ls {
			if strings.HasPrefix(l, "=> ") {
				return strings.TrimPrefix(l, "=> ")
			}
		}
		return ""
	}

	conn := section("From this computer")
	switch {
	case find(conn, "could not prepare the connection") != "":
		add("error", "Termwright cannot set up the route to the machine.", firstNonEmpty(arrow(conn), strings.TrimSpace(find(conn, "could not prepare"))))
	case find(conn, "connect:  FAILED") != "":
		add("error", "The machine's "+kind+" port cannot be reached.", firstNonEmpty(arrow(conn), strings.TrimSpace(find(conn, "FAILED"))))
	case has("Too many security failures") || has("says first: RFB 003.003 "):
		add("error", "The VNC server has blocked this address after wrong passwords.",
			"Restart the VNC service (the button below) to clear the block, then connect once with the right password. If unsure, set it again with Set Up Remote Desktop > Reconfigure.")
		fix = "VNC"
	case find(conn, "the server refuses the connection") != "":
		add("error", "The VNC server refuses the connection.", strings.TrimSpace(find(conn, "the server refuses the connection")))
	case find(conn, "not a VNC server") != "" || find(conn, "sent nothing") != "":
		add("error", "The port is open but it is not a VNC server.", "Something else listens on that port, or it closes the connection at once. Check the port of the session.")
	case find(conn, "cannot use any of these") != "":
		add("error", "The VNC server offers only security types Termwright's viewer cannot use.", strings.TrimSpace(find(conn, "the server offers")))
	case find(conn, "no RDP answer") != "" || find(conn, "answer is not RDP") != "" || find(conn, "unexpected RDP answer") != "":
		add("error", "The port is open but it does not answer as an RDP server.", "Something else listens on that port, or it closes the connection at once. Check the port of the session.")
	case find(conn, "refused the security methods") != "":
		add("error", "The RDP server refused the security methods Termwright offered.", strings.TrimSpace(find(conn, "refused the security")))
	}

	// the machine: one block per service
	for _, svc := range []struct{ kind, prefix string }{{"VNC", "VNC (TigerVNC)"}, {"RDP", "RDP (xrdp)"}} {
		sec := section(svc.prefix)
		if len(sec) == 0 {
			continue
		}
		if l := find(sec, "refuses connections"); l != "" {
			add("error", "Nothing listens on the "+svc.kind+" port of the machine.", "The "+svc.kind+" service is stopped or uses another port. Restart it (the button below) or run Set Up Remote Desktop again.")
			if fix == "" {
				fix = svc.kind
			}
		}
		for _, l := range sec {
			if m := serviceState.FindStringSubmatch(l); m != nil && m[2] != "active" {
				add("error", "The service "+m[1]+" is "+m[2]+".", "Restart it (the button below); if it fails again, the journal of the service in the report says why.")
				if fix == "" {
					fix = svc.kind
				}
			}
		}
		if find(sec, "no packages found") != "" || find(sec, "is not installed") != "" {
			add("error", svc.kind+" is not installed on the machine.", "Run Set Up Remote Desktop on its SSH session.")
		}
		if l := find(sec, "does not exist on this machine"); l != "" {
			add("error", strings.TrimSpace(strings.TrimPrefix(l, "the "))+".", "The session's user name is wrong, or the user was removed. Edit the session or run Set Up Remote Desktop again.")
		}
		if find(sec, "(MISSING)") != "" {
			add("error", "The home folder of the "+svc.kind+" user is missing.", "The desktop cannot start without it. Run Set Up Remote Desktop with Reconfigure, or create the folder.")
		}
		if l := find(sec, "locked: L"); l != "" {
			add("warn", "The account of the "+svc.kind+" user is locked.", "Unlock it (sudo passwd -u <user>) or set a password for it.")
		}
		if svc.kind == "VNC" && lastFailed(sec) && !has("Too many security failures") {
			add("warn", "The VNC log shows failed logins.", "The password typed differs from the one on the machine. Set it again (sudo -u <vnc user> vncpasswd, then restart the VNC service) and update the saved password of the session.")
			if fix == "" {
				fix = "VNC"
			}
		}
		if svc.kind == "RDP" && (find(sec, "login failed") != "" || find(sec, "authentication failure") != "") {
			add("warn", "The xrdp log shows failed logins.", "The user name or password is wrong, or the user is not allowed to log in. Check the account and the saved password of the session.")
		}
	}
	if l := strings.ToLower(report); strings.Contains(l, "allowtcpforwarding no") || strings.Contains(l, "disableforwarding yes") {
		add("error", "The SSH server forbids port forwarding, so this session cannot be reached through it.", "Set AllowTcpForwarding yes in its sshd_config, or choose \"Directly from the network\" in Set Up Remote Desktop.")
	}
	if has("(needs sudo") {
		add("info", "Some checks need sudo and were skipped.", "Type the sudo password in the box below and choose Diagnose Again to include the service logs.")
	}

	hasProblem := false
	for _, f := range finds {
		if f.Level == "error" || f.Level == "warn" {
			hasProblem = true
		}
	}
	if !hasProblem {
		switch {
		case find(conn, "server says RFB 003.008") != "" && find(conn, "VNC Authentication") != "":
			add("ok", "No problem found: the VNC server is reachable and answers correctly.", "If the login still fails, the cause is the password or the desktop session of the VNC user. Use Restart VNC service if the desktop looks frozen.")
		case find(conn, "RDP connection confirmed") != "":
			add("ok", "No problem found: the RDP server is reachable and answers correctly.", "If the login still fails, the cause is the user name, the password or the desktop session of the RDP user.")
		case kind == "SSH2" && (has("accepts connections")):
			add("ok", "No problem found: the services of this machine run and accept connections.", "")
		default:
			add("warn", "The diagnosis could not find the cause.", "The details below show what was checked.")
		}
	}
	order := map[string]int{"error": 0, "warn": 1, "info": 2, "ok": 0}
	sort.SliceStable(finds, func(i, j int) bool { return order[finds[i].Level] < order[finds[j].Level] })
	return finds, fix
}

var serviceState = regexp.MustCompile(`^(tigervnc@\d+\.service|xrdp|xrdp-sesman): (\w+)`)

// lastFailed: the VNC log's last login event is a failed one.
func lastFailed(sec []string) bool {
	failed := false
	for _, l := range sec {
		ll := strings.ToLower(l)
		switch {
		case strings.Contains(ll, "authentication failed") || strings.Contains(ll, "authentication rejected"):
			failed = true
		case strings.Contains(ll, "authentication successful") || strings.Contains(ll, "authenticated"):
			failed = false
		}
	}
	return failed
}

// checksOf lists what the report shows was checked, good or bad, one line each.
func checksOf(report string) []DiagFinding {
	var out []DiagFinding
	add := func(level, text string) { out = append(out, DiagFinding{Level: level, Text: text}) }
	var sec string
	for _, l := range strings.Split(report, "\n") {
		if strings.HasPrefix(l, "== ") {
			sec = l
			continue
		}
		inConn := strings.HasPrefix(sec, "== From this computer")
		svc := ""
		switch {
		case strings.HasPrefix(sec, "== VNC"):
			svc = "VNC"
		case strings.HasPrefix(sec, "== RDP"):
			svc = "RDP"
		}
		switch {
		case inConn && strings.HasPrefix(l, "connect:  OK"):
			add("ok", "The port can be reached from this computer ("+strings.TrimSpace(strings.TrimPrefix(l, "connect:  OK"))+")")
		case inConn && strings.HasPrefix(l, "connect:  FAILED"):
			add("error", "The port cannot be reached from this computer: "+strings.TrimSpace(strings.TrimPrefix(l, "connect:  FAILED")))
		case inConn && strings.HasPrefix(l, "handshake: server says RFB 003.008"):
			add("ok", "The server speaks VNC (RFB 003.008)")
		case inConn && strings.HasPrefix(l, "handshake: server says RFB 003.003"):
			add("error", "The server answers RFB 003.003: it has blocked this address")
		case inConn && strings.Contains(l, "RDP connection confirmed"):
			add("ok", "The server answers as an RDP server")
		case inConn && strings.HasPrefix(l, "handshake:"):
			add("error", strings.TrimSpace(strings.TrimPrefix(l, "handshake:")))
		case inConn && strings.HasPrefix(l, "security:  the server offers"):
			add("ok", "Security: "+strings.TrimSpace(strings.TrimPrefix(l, "security:  the server offers")))
		case inConn && strings.HasPrefix(l, "security:  the server chose"):
			add("ok", "Security: "+strings.TrimSpace(strings.TrimPrefix(l, "security:  the server chose")))
		case inConn && strings.HasPrefix(l, "security:"):
			add("error", "Security: "+strings.TrimSpace(strings.TrimPrefix(l, "security:")))
		case svc != "" && strings.Contains(l, "this machine to itself:") && strings.Contains(l, "accepts connections"):
			add("ok", "The "+svc+" service listens on the machine")
		case svc != "" && strings.Contains(l, "this machine to itself:") && strings.Contains(l, "refuses connections"):
			add("error", "Nothing listens on the "+svc+" port of the machine")
		case svc != "" && serviceState.MatchString(l):
			m := serviceState.FindStringSubmatch(l)
			lv := "ok"
			if m[2] != "active" {
				lv = "error"
			}
			add(lv, "Service "+m[1]+" is "+m[2])
		case svc != "" && strings.HasPrefix(l, "home: ") && strings.Contains(l, "(exists)"):
			add("ok", "The user's home folder exists")
		case svc != "" && strings.HasPrefix(l, "home: ") && strings.Contains(l, "(MISSING)"):
			add("error", "The user's home folder is missing")
		case strings.HasPrefix(l, "sudo:   "):
			if strings.Contains(l, "not available") {
				add("info", "sudo is not available: the logs that need root were skipped")
			} else {
				add("ok", "sudo works: the service logs were read")
			}
		}
	}
	low := strings.ToLower(report)
	if strings.Contains(low, "--- sshd forwarding") {
		if strings.Contains(low, "allowtcpforwarding no") || strings.Contains(low, "disableforwarding yes") {
			add("error", "The SSH server forbids port forwarding")
		} else {
			add("ok", "The SSH server allows port forwarding")
		}
	}
	return out
}
