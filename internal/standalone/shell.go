package standalone

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

var (
	ansiRe  = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b[()][A-Z0-9]|\x1b[=>]|\r`)
	pagerRe = regexp.MustCompile(`(?i)(--\s*More\s*--|Press <SPACE> to continue or <Q> to quit:|---- More ----|<--- More --->|\(q\)uit)`)
	// a line that ends like a prompt
	genericPrompt = regexp.MustCompile(`(?:^|\n)([^\n]{1,80}?[>#$%\]])[ \t]*$`)
)

// pagingOff: the commands that turn paging off, by device type.
var pagingOff = map[string][]string{
	"exos": {"disable clipaging"}, "voss": {"terminal more disable"}, "cisco ios": {"terminal length 0"},
	"cisco ios-xe": {"terminal length 0"}, "cisco nx-os": {"terminal length 0"}, "arista eos": {"terminal length 0"},
	"huawei vrp": {"screen-length 0 temporary"}, "juniper junos": {"set cli screen-length 0"},
	"ruckus icx": {"skip-page-display"}, "eltex mes": {"terminal datadump"},
}

// shell is an interactive login: commands are written and read up to the next prompt.
type shell struct {
	d      Device
	w      io.Writer
	in     chan string
	done   chan struct{}
	buf    strings.Builder
	prompt *regexp.Regexp
	closer func()
}

func newShell(d Device, r io.Reader, w io.Writer, closer func()) *shell {
	s := &shell{d: d, w: w, in: make(chan string, 64), done: make(chan struct{}), closer: closer}
	go func() {
		defer close(s.done)
		b := make([]byte, 32<<10)
		for {
			n, err := r.Read(b)
			if n > 0 {
				s.in <- string(b[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	return s
}

func (s *shell) close() {
	if s.closer != nil {
		s.closer()
	}
}

func (s *shell) write(t string) error {
	_, err := io.WriteString(s.w, t)
	return err
}

// wait reads until match finds something in what came since the last reset.
func (s *shell) wait(ctx context.Context, limit time.Duration, match func(string) []int) ([]int, error) {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	for {
		if m := match(s.buf.String()); m != nil {
			return m, nil
		}
		select {
		case chunk := <-s.in:
			s.buf.WriteString(ansiRe.ReplaceAllString(chunk, ""))
			if t := s.buf.String(); pagerRe.MatchString(tail(t, 120)) {
				clean := pagerRe.ReplaceAllString(t, "")
				s.buf.Reset()
				s.buf.WriteString(clean)
				_ = s.write(" ")
			}
		case <-s.done:
			if m := match(s.buf.String()); m != nil {
				return m, nil
			}
			return nil, fmt.Errorf("%s closed the connection", s.d.Session)
		case <-deadline.C:
			return nil, fmt.Errorf("no prompt from %s within %s; it showed: %q", s.d.Session, limit, tail(s.buf.String(), 300))
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

var digits = regexp.MustCompile(`\d+`)

// promptLike matches a prompt shaped like the one seen: its numbers may change ("Stack.5 #" after
// "Stack.4 #") and EXOS's "* " for unsaved changes may come and go.
func promptLike(p string) *regexp.Regexp {
	p = regexp.MustCompile(`^\*\s+`).ReplaceAllString(strings.TrimSpace(p), "")
	var rx strings.Builder
	last := 0
	for _, m := range digits.FindAllStringIndex(p, -1) {
		rx.WriteString(regexp.QuoteMeta(p[last:m[0]]) + `\d+`)
		last = m[1]
	}
	rx.WriteString(regexp.QuoteMeta(p[last:]))
	return regexp.MustCompile(`(?:^|\n)[ \t]*(?:\*\s+)?` + rx.String() + `[ \t]*$`)
}

// learn finds the prompt and turns paging off.
func (s *shell) learn(ctx context.Context) error {
	if s.d.Prompt != "" {
		re, err := regexp.Compile(s.d.Prompt)
		if err != nil {
			return fmt.Errorf("prompt of %s: %v", s.d.Session, err)
		}
		s.prompt = re
		if _, err := s.wait(ctx, 30*time.Second, re.FindStringIndex); err != nil {
			return err
		}
	} else {
		if _, err := s.wait(ctx, 30*time.Second, genericPrompt.FindStringIndex); err != nil {
			return err
		}
		s.buf.Reset()
		_ = s.write("\n")
		var seen string
		if _, err := s.wait(ctx, 30*time.Second, func(b string) []int {
			m := genericPrompt.FindStringSubmatchIndex(b)
			if m != nil {
				seen = b[m[2]:m[3]]
			}
			return m
		}); err != nil {
			return err
		}
		s.prompt = promptLike(seen)
	}
	s.buf.Reset()
	cmds := s.d.PagingOff
	if len(cmds) == 0 {
		cmds = pagingOff[strings.ToLower(s.d.DeviceType)]
	}
	for _, c := range cmds {
		if _, err := s.command(ctx, c, "", 15*time.Second); err != nil {
			return err
		}
	}
	return nil
}

func (s *shell) command(ctx context.Context, cmd, prompt string, limit time.Duration) (string, error) {
	s.buf.Reset()
	// what came before (a late banner, a log line) is not this command's output
	for len(s.in) > 0 {
		<-s.in
	}
	if err := s.write(cmd + "\n"); err != nil {
		return "", err
	}
	pr := s.prompt
	if prompt != "" {
		re, err := regexp.Compile(prompt)
		if err != nil {
			return "", fmt.Errorf("prompt %q: %v", prompt, err)
		}
		pr = re
	}
	m, err := s.wait(ctx, limit, func(b string) []int {
		if !strings.Contains(b, "\n") && prompt == "" {
			return nil // the echo of the command, not yet its answer
		}
		return pr.FindStringIndex(b)
	})
	if err != nil {
		return "", err
	}
	out := s.buf.String()[:m[0]]
	lines := strings.Split(out, "\n")
	if len(lines) > 0 && strings.TrimSpace(cmd) != "" && strings.Contains(lines[0], strings.TrimSpace(cmd)) {
		lines = lines[1:]
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n"), nil
}

func port(p, def int) string {
	if p == 0 {
		p = def
	}
	return strconv.Itoa(p)
}

// login opens a device's command line over SSH (through its jump host, if any) or Telnet.
func (e *Engine) login(ctx context.Context, d Device) (*shell, error) {
	if strings.HasPrefix(strings.ToLower(d.Protocol), "telnet") {
		return e.telnet(ctx, d)
	}
	client, closeAll, err := e.dial(ctx, d, port(d.Port, 22))
	if err != nil {
		return nil, err
	}
	ss, err := client.NewSession()
	if err != nil {
		closeAll()
		return nil, err
	}
	_ = ss.RequestPty("vt100", 1000, 511, ssh.TerminalModes{ssh.ECHO: 1})
	w, _ := ss.StdinPipe()
	r, _ := ss.StdoutPipe()
	if err := ss.Shell(); err != nil {
		closeAll()
		return nil, fmt.Errorf("shell on %s: %v", d.Session, err)
	}
	sh := newShell(d, r, w, func() { ss.Close(); closeAll() })
	if err := sh.learn(ctx); err != nil {
		sh.close()
		return nil, err
	}
	return sh, nil
}

// dial makes an SSH connection to d on port p (through its jump host); close closes it and the jump.
func (e *Engine) dial(ctx context.Context, d Device, p string) (*ssh.Client, func(), error) {
	cfg := &ssh.ClientConfig{User: d.Username, Timeout: 20 * time.Second, HostKeyCallback: hostKeys()}
	if d.KeyFile != "" {
		kb, err := os.ReadFile(expand(d.KeyFile))
		if err != nil {
			return nil, nil, fmt.Errorf("key of %s: %v", d.Session, err)
		}
		signer, err := ssh.ParsePrivateKey(kb)
		if err != nil {
			return nil, nil, fmt.Errorf("key of %s: %v (a key with a passphrase: use ssh-agent or a key without one)", d.Session, err)
		}
		cfg.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	} else {
		pw, err := e.Secret(d, "password")
		if err != nil {
			return nil, nil, err
		}
		cfg.Auth = []ssh.AuthMethod{ssh.Password(pw), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			a := make([]string, len(qs))
			for i := range a {
				a[i] = pw
			}
			return a, nil
		})}
	}
	addr := net.JoinHostPort(d.Host, p)
	if d.Jump != "" {
		jd, err := e.Device(d.Jump)
		if err != nil {
			return nil, nil, fmt.Errorf("jump host of %s: %v", d.Session, err)
		}
		jc, jclose, err := e.dial(ctx, jd, port(jd.Port, 22))
		if err != nil {
			return nil, nil, fmt.Errorf("jump host %s: %v", jd.Session, err)
		}
		conn, err := jc.Dial("tcp", addr)
		if err != nil {
			jclose()
			return nil, nil, fmt.Errorf("%s through %s: %v", d.Session, jd.Session, err)
		}
		c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
		if err != nil {
			jclose()
			return nil, nil, fmt.Errorf("SSH to %s (%s): %v", d.Session, d.Host, err)
		}
		cl := ssh.NewClient(c, chans, reqs)
		return cl, func() { cl.Close(); jclose() }, nil
	}
	dl := net.Dialer{Timeout: 20 * time.Second}
	conn, err := dl.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("SSH to %s (%s): %v", d.Session, addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("SSH to %s (%s): %v", d.Session, d.Host, err)
	}
	cl := ssh.NewClient(c, chans, reqs)
	return cl, func() { cl.Close() }, nil
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return h + p[1:]
		}
	}
	return p
}

// hostKeys accepts any host key, unless TW_KNOWN_HOSTS names a known_hosts file to check against.
func hostKeys() ssh.HostKeyCallback {
	f := os.Getenv("TW_KNOWN_HOSTS")
	if f == "" {
		return ssh.InsecureIgnoreHostKey() //nolint:gosec // lab devices change keys often; TW_KNOWN_HOSTS turns checking on
	}
	b, err := os.ReadFile(expand(f))
	if err != nil {
		return func(string, net.Addr, ssh.PublicKey) error { return fmt.Errorf("TW_KNOWN_HOSTS: %v", err) }
	}
	known := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		fs := strings.Fields(line)
		if len(fs) >= 3 {
			for _, h := range strings.Split(fs[0], ",") {
				known[h+" "+fs[1]+" "+fs[2]] = true
			}
		}
	}
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		k := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
		h, p, _ := net.SplitHostPort(host)
		for _, name := range []string{h, "[" + h + "]:" + p} {
			if known[name+" "+k] {
				return nil
			}
		}
		return fmt.Errorf("the host key of %s is not in %s", host, f)
	}
}

// telnetConn reads a Telnet connection, refusing every option the far end offers.
type telnetConn struct {
	net.Conn
}

func (t *telnetConn) Read(p []byte) (int, error) {
	buf := make([]byte, len(p))
	n, err := t.Conn.Read(buf)
	out := 0
	for i := 0; i < n; i++ {
		if buf[i] == 255 && i+2 < n && buf[i+1] >= 251 && buf[i+1] <= 254 {
			reply := byte(252) // WONT
			if buf[i+1] == 251 || buf[i+1] == 252 {
				reply = 254 // DONT
			}
			_, _ = t.Conn.Write([]byte{255, reply, buf[i+2]})
			i += 2
			continue
		}
		if buf[i] == 255 && i+1 < n && buf[i+1] == 250 { // subnegotiation: skip to IAC SE
			for i < n-1 && !(buf[i] == 255 && buf[i+1] == 240) {
				i++
			}
			i++
			continue
		}
		p[out] = buf[i]
		out++
	}
	return out, err
}

type crlf struct{ w io.Writer }

func (c crlf) Write(p []byte) (int, error) {
	_, err := c.w.Write([]byte(strings.ReplaceAll(string(p), "\n", "\r\n")))
	return len(p), err
}

func (e *Engine) telnet(ctx context.Context, d Device) (*shell, error) {
	dl := net.Dialer{Timeout: 20 * time.Second}
	conn, err := dl.DialContext(ctx, "tcp", net.JoinHostPort(d.Host, port(d.Port, 23)))
	if err != nil {
		return nil, fmt.Errorf("Telnet to %s (%s): %v", d.Session, d.Host, err)
	}
	tc := &telnetConn{conn}
	sh := newShell(d, tc, crlf{conn}, func() { conn.Close() })
	login := regexp.MustCompile(`(?i)(login|user ?name)\s*:\s*$`)
	pass := regexp.MustCompile(`(?i)password\s*:\s*$`)
	if d.Username != "" {
		if _, err := sh.wait(ctx, 30*time.Second, login.FindStringIndex); err != nil {
			sh.close()
			return nil, err
		}
		sh.buf.Reset()
		_ = sh.write(d.Username + "\n")
	}
	if _, err := sh.wait(ctx, 30*time.Second, func(b string) []int {
		if m := pass.FindStringIndex(b); m != nil {
			return m
		}
		return genericPrompt.FindStringIndex(b)
	}); err != nil {
		sh.close()
		return nil, err
	}
	if pass.MatchString(sh.buf.String()) {
		pw, err := e.Secret(d, "password")
		if err != nil {
			sh.close()
			return nil, err
		}
		sh.buf.Reset()
		_ = sh.write(pw + "\n")
	} else {
		// already at a prompt: learn reads it from here
		_ = sh.write("\n")
	}
	if err := sh.learn(ctx); err != nil {
		sh.close()
		return nil, err
	}
	return sh, nil
}
