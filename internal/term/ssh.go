package term

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/sshkey"
)

type sshConn struct {
	client  *ssh.Client
	sess    *ssh.Session
	stdin   io.WriteCloser
	out     *io.PipeReader
	info    string
	closeMu sync.Once
	stopKA  chan struct{}
	cleanup func()
	fwd     *forwarder
}

func (c *sshConn) Read(p []byte) (int, error)  { return c.out.Read(p) }
func (c *sshConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }
func (c *sshConn) Info() string                { return c.info }
func (c *sshConn) Resize(cols, rows int) error { return c.sess.WindowChange(rows, cols) }

// SendBreak sends an SSH break request (RFC 4335), e.g. for a console server port.
func (c *sshConn) SendBreak() error {
	ok, err := c.sess.SendRequest("break", true, ssh.Marshal(struct{ Ms uint32 }{500}))
	if err == nil && !ok {
		err = errors.New("server does not support break")
	}
	return err
}
func (c *sshConn) Close() error {
	c.closeMu.Do(func() {
		close(c.stopKA)
		c.fwd.Close()
		_ = c.sess.Close()
		_ = c.client.Close()
		_ = c.out.Close()
		if c.cleanup != nil {
			c.cleanup()
		}
	})
	return nil
}

// termType maps an emulation name to a TERM value.
func termType(s session.Session) string {
	if s.TermType != "" {
		return s.TermType
	}
	switch strings.ToLower(s.Emulation) {
	case "vt100":
		return "vt100"
	case "vt102":
		return "vt102"
	case "vt220":
		return "vt220"
	case "ansi":
		return "ansi"
	case "linux":
		return "linux"
	default:
		return "xterm-256color"
	}
}

func (m *Manager) dialSSH(ctx context.Context, t *Tab, s session.Session, cols, rows int) (Conn, error) {
	client, info, cleanup, err := m.sshClient(ctx, t, s, 0)
	if err != nil {
		return nil, err
	}
	m.step(t, "shell")
	fail := func(e error) (Conn, error) {
		_ = client.Close()
		cleanup()
		return nil, e
	}
	sess, err := client.NewSession()
	if err != nil {
		return fail(err)
	}
	if s.AgentForward {
		if err := forwardAgent(client, sess, m.cardSigners(ctx, t, s)); err != nil {
			m.emit.Emit("toast", map[string]string{"level": "error", "message": "Agent forwarding: " + err.Error()})
		}
	}
	if s.X11Forward {
		if err := setupX11(client, sess); err != nil {
			m.emit.Emit("toast", map[string]string{"level": "error", "message": "X11 forwarding: " + err.Error()})
		}
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200}
	if err := sess.RequestPty(termType(s), rows, cols, modes); err != nil {
		return fail(fmt.Errorf("request pty: %w", err))
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		return fail(err)
	}
	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw
	if cmd := strings.TrimSpace(s.RemoteCommand); s.UseRemoteCommand && cmd != "" {
		if err := sess.Start(cmd); err != nil {
			return fail(fmt.Errorf("remote command: %w", err))
		}
	} else if err := sess.Shell(); err != nil {
		return fail(fmt.Errorf("start shell: %w", err))
	}
	c := &sshConn{client: client, sess: sess, stdin: stdin, out: pr, stopKA: make(chan struct{}), info: info, cleanup: cleanup}
	if len(s.PortForwards) > 0 {
		c.fwd = m.startForwards(client, s.PortForwards)
	}
	go func() {
		err := sess.Wait()
		if err == nil {
			err = io.EOF
		}
		_ = pw.CloseWithError(err)
	}()
	go func() {
		// a dead transport (e.g. network loss) also ends the session
		_ = client.Wait()
		_ = pw.CloseWithError(io.EOF)
	}()
	if s.Keepalive && s.KeepaliveSec > 0 {
		go func() {
			tk := time.NewTicker(time.Duration(s.KeepaliveSec) * time.Second)
			defer tk.Stop()
			for {
				select {
				case <-tk.C:
					if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
						_ = client.Close()
						return
					}
				case <-c.stopKA:
					return
				}
			}
		}()
	}
	return c, nil
}

// sshClient authenticates to the session's host (through its jump host or
// proxy when configured). depth guards against jump-host loops.
func (m *Manager) sshClient(ctx context.Context, t *Tab, s session.Session, depth int) (*ssh.Client, string, func(), error) {
	if s.Hostname == "" {
		return nil, "", nil, errors.New("no hostname")
	}
	port := s.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(s.Hostname, strconv.Itoa(port))
	user := s.Username
	account := ""
	if name, u, _, ok := m.accountLogin(s); ok {
		account = name
		if u != "" {
			user = u
		}
	}
	if k := m.accountKey(s); k != "" {
		s.IdentityFile = k // the account logs in with a private key
	}
	if s.TransportUser != "" {
		user = s.TransportUser // console server login (the device behind the port logs in with s.Username)
	}
	if user == "" {
		r, err := m.prompts.Ask(ctx, Prompt{TabID: t.id, Kind: "credentials", Title: "Login",
			Message: "Enter user name for " + addr, Fields: []PromptField{{Label: "Username"}}})
		if err != nil {
			return nil, "", nil, err
		}
		user = r.Values[0]
	}

	var usedPassword, lastPassword string // lastPassword: the one the login tried last (typed or saved)
	var typedPassword bool
	var rememberPassword bool
	pwPath := s.Path
	saved, haveSaved := m.loginPassword(s)
	if s.TransportPath != "" {
		pwPath = s.TransportPath
		saved, haveSaved = "", false
		if m.SavedPassword != nil {
			saved, haveSaved = m.SavedPassword(s.TransportPath)
		}
	}
	askPassword := func(msg string) (string, error) {
		if haveSaved {
			haveSaved = false // only try once
			lastPassword = saved
			return saved, nil
		}
		pr := Prompt{TabID: t.id, Kind: "credentials", Title: "Enter Secure Shell Password",
			Message: msg, Fields: []PromptField{{Label: "Password", Secret: true}}, AllowRemember: pwPath != ""}
		if account != "" && s.TransportPath == "" && m.OnAccountPasswordTyped != nil {
			pr.AllowRemember, pr.RememberLabel = true, "Save for the account "+account+" (every session that uses it)"
		}
		r, err := m.prompts.Ask(ctx, pr)
		if err != nil {
			return "", err
		}
		usedPassword, rememberPassword = r.Values[0], r.Remember
		lastPassword = r.Values[0]
		typedPassword = true
		return r.Values[0], nil
	}

	var auths []ssh.AuthMethod
	methods := strings.Split(strings.ToLower(s.AuthMethods), ",")
	if strings.TrimSpace(s.AuthMethods) == "" {
		methods = []string{"publickey", "keyboard-interactive", "password"}
	}
	for _, meth := range methods {
		switch strings.TrimSpace(meth) {
		case "publickey":
			auths = append(auths, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
				return m.signers(ctx, t, s)
			}))
		case "keyboard-interactive":
			auths = append(auths, ssh.RetryableAuthMethod(ssh.KeyboardInteractive(
				func(name, instruction string, questions []string, echos []bool) ([]string, error) {
					if len(questions) == 0 {
						return nil, nil
					}
					if len(questions) == 1 && !echos[0] && strings.Contains(strings.ToLower(questions[0]), "password") {
						pw, err := askPassword(fmt.Sprintf("%s@%s\n%s", user, addr, strings.TrimSpace(instruction)))
						return []string{pw}, err
					}
					fields := make([]PromptField, len(questions))
					for i, q := range questions {
						fields[i] = PromptField{Label: strings.TrimSpace(q), Secret: !echos[i]}
					}
					title := name
					if title == "" {
						title = "Keyboard Interactive"
					}
					r, err := m.prompts.Ask(ctx, Prompt{TabID: t.id, Kind: "credentials", Title: title, Message: instruction, Fields: fields})
					if err != nil {
						return nil, err
					}
					return r.Values, nil
				}), 3))
		case "gssapi", "gssapi-with-mic", "kerberos":
			auths = append(auths, m.kerberosAuth(ctx, t, s, user, s.Hostname))
		case "password":
			auths = append(auths, ssh.RetryableAuthMethod(ssh.PasswordCallback(func() (string, error) {
				return askPassword(user + "@" + addr)
			}), 3))
		}
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: m.stepAfterHostKey(t, depth, m.hostKeyCallback(ctx, t)),
		Timeout:         20 * time.Second,
		ClientVersion:   "SSH-2.0-Termwright",
	}
	if err := applyAlgorithms(cfg, s, m.knownHostAlgorithms(addr)); err != nil {
		return nil, "", nil, err
	}
	if depth == 0 && s.JumpSession == "" {
		m.step(t, "tcp")
	}
	raw, via, err := m.dialNet(ctx, t, s, addr, depth)
	if err != nil {
		return nil, "", nil, err
	}
	if depth == 0 {
		m.step(t, "hostkey")
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	cc, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
	stop()
	if err != nil {
		_ = raw.Close()
		return nil, "", nil, legacyHint(err, s)
	}
	client := ssh.NewClient(cc, chans, reqs)
	if depth == 0 && lastPassword != "" && t != nil { // the tab's own login (not a jump host's)
		t.mu.Lock()
		t.loginPass = lastPassword
		t.mu.Unlock()
	}
	if rememberPassword && typedPassword {
		if account != "" && s.TransportPath == "" && m.OnAccountPasswordTyped != nil {
			m.OnAccountPasswordTyped(account, usedPassword)
		} else if m.OnPasswordUsed != nil {
			m.OnPasswordUsed(pwPath, usedPassword)
		}
	}
	info := fmt.Sprintf("SSH2 %s@%s  %s%s", user, addr, string(cc.ServerVersion()), via)
	return client, info, func() { _ = raw.Close() }, nil
}

// signers loads keys from the session identity file, the SSH agent and the
// default ~/.ssh keys.
func (m *Manager) signers(ctx context.Context, t *Tab, s session.Session) ([]ssh.Signer, error) {
	out := m.cardSigners(ctx, t, s) // smart card keys first
	agentKeys := agentSigners()
	out = append(out, agentKeys...)
	certPath := expandHome(strings.TrimSpace(s.CertificateFile))
	// add puts a key first with its certificate (key-cert.pub or the session's certificate file)
	add := func(f string, signer ssh.Signer) {
		if cs, ok, err := sshkey.CertSigner(f, certPath, signer); err != nil {
			m.emit.Emit("toast", map[string]string{"level": "error", "message": err.Error()})
		} else if ok {
			out = append(out, cs)
		}
		out = append(out, signer)
	}
	var files []string
	if s.IdentityFile != "" {
		files = []string{expandHome(s.IdentityFile)}
	} else if h, err := os.UserHomeDir(); err == nil {
		for _, n := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			files = append(files, filepath.Join(h, ".ssh", n))
		}
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		// FIDO2 / U2F security keys (sk-…) sign in the hardware: use them through ssh-agent
		if pub, perr := sshkey.PublicKeyOf(f, data); perr == nil && sshkey.IsSecurityKey(pub) {
			if signer := m.securityKeySigner(ctx, t, f, pub); signer != nil {
				add(f, signer)
			}
			continue
		}
		signer, err := sshkey.ParseKey(data, "")
		var pme *ssh.PassphraseMissingError
		if errors.As(err, &pme) {
			if s.IdentityFile == "" {
				continue // don't prompt for default keys
			}
			if m.KeyPassphrase != nil {
				if pw := m.KeyPassphrase(f); pw != "" {
					if signer, err := sshkey.ParseKey(data, pw); err == nil {
						out = append(out, signer)
						continue
					}
				}
			}
			r, perr := m.prompts.Ask(ctx, Prompt{TabID: t.id, Kind: "credentials", Title: "Key Passphrase",
				Message: "Enter passphrase for " + f, Fields: []PromptField{{Label: "Passphrase", Secret: true}},
				AllowRemember: m.OnKeyPassphraseUsed != nil})
			if perr != nil {
				return out, nil
			}
			signer, err = sshkey.ParseKey(data, r.Values[0])
			if err == nil && r.Remember && m.OnKeyPassphraseUsed != nil {
				m.OnKeyPassphraseUsed(f, r.Values[0])
			}
		}
		if err == nil {
			add(f, signer)
		}
	}
	return out, nil
}

// securityKeySigner finds a FIDO2 key in ssh-agent; when it is not loaded the
// user can load it (ssh-add, touching the key) and retry.
func (m *Manager) securityKeySigner(ctx context.Context, t *Tab, file string, pub ssh.PublicKey) ssh.Signer {
	for {
		for _, sg := range agentSigners() {
			if bytes.Equal(sg.PublicKey().Marshal(), pub.Marshal()) {
				return sg
			}
		}
		r, err := m.prompts.Ask(ctx, Prompt{TabID: t.id, Kind: "confirm", Title: "Security Key",
			Message: "The identity file " + file + " is a FIDO2 security key (" + pub.Type() + ").\n" +
				"Termwright signs with it through ssh-agent. Load it once in a terminal:\n\n    ssh-add " + file +
				"\n\n(touch the key or enter its PIN when asked), then choose Retry. Each login may ask you to touch the key.",
			Choices: []string{"Retry"}})
		if err != nil || !r.OK || r.Choice != "Retry" {
			return nil
		}
	}
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[1:])
		}
	}
	return p
}

func (m *Manager) knownHostsFile() string {
	f := filepath.Join(m.dataDir, "known_hosts")
	if _, err := os.Stat(f); err != nil {
		_ = os.WriteFile(f, nil, 0o600)
	}
	return f
}

func (m *Manager) knownHostsFiles() []string {
	files := []string{m.knownHostsFile()}
	if h, err := os.UserHomeDir(); err == nil {
		uf := filepath.Join(h, ".ssh", "known_hosts")
		if _, err := os.Stat(uf); err == nil {
			files = append(files, uf)
		}
	}
	return files
}

var probeKey ssh.PublicKey

func init() {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	if s, err := ssh.NewSignerFromKey(priv); err == nil {
		probeKey = s.PublicKey()
	}
}

// knownHostAlgorithms returns host key algorithms for which we already know a
// key for addr, so the server presents a key we can verify.
func (m *Manager) knownHostAlgorithms(addr string) []string {
	cb, err := knownhosts.New(m.knownHostsFiles()...)
	if err != nil || probeKey == nil {
		return nil
	}
	err = cb(addr, &net.TCPAddr{}, probeKey)
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) || len(ke.Want) == 0 {
		return nil
	}
	var algos []string
	for _, w := range ke.Want {
		switch w.Key.Type() {
		case ssh.KeyAlgoRSA:
			algos = append(algos, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		default:
			algos = append(algos, w.Key.Type())
		}
	}
	return algos
}

// step reports how far a connection got ("route", "tcp", "hostkey", "auth",
// "shell") for the connecting card.
func (m *Manager) step(t *Tab, name string) {
	m.emit.Emit("term:step:"+t.id, name)
}

// stepAfterHostKey reports the login step once the target's host key is
// accepted (key exchange done, authentication next).
func (m *Manager) stepAfterHostKey(t *Tab, depth int, cb ssh.HostKeyCallback) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil && depth == 0 {
			m.step(t, "auth")
		}
		return err
	}
}

func (m *Manager) hostKeyCallback(ctx context.Context, t *Tab) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		cb, err := knownhosts.New(m.knownHostsFiles()...)
		if err != nil {
			return err
		}
		err = cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) {
			return err
		}
		fp := ssh.FingerprintSHA256(key)
		if len(ke.Want) == 0 {
			r, err := m.prompts.Ask(ctx, Prompt{TabID: t.id, Kind: "hostkey", Title: "New Host Key",
				Message: fmt.Sprintf("The host key for %s is not in your known hosts.\n\nKey type: %s\nFingerprint: %s\n\nAccept and save this key?", hostname, key.Type(), fp),
				Choices: []string{"Accept & Save", "Accept Once"}})
			if err != nil {
				return errors.New("host key rejected")
			}
			if r.Choice == "Accept & Save" {
				return m.saveHostKey(hostname, key, false)
			}
			return nil
		}
		r, err := m.prompts.Ask(ctx, Prompt{TabID: t.id, Kind: "hostkey", Title: "WARNING: Host Key Changed", Danger: true,
			Message: fmt.Sprintf("The host key for %s does NOT match the saved key (%s:%d).\nThis could mean someone is intercepting the connection.\n\nNew key type: %s\nNew fingerprint: %s", hostname, ke.Want[0].Filename, ke.Want[0].Line, key.Type(), fp),
			Choices: []string{"Replace & Save", "Accept Once"}})
		if err != nil {
			return errors.New("host key changed - connection refused")
		}
		if r.Choice == "Replace & Save" {
			return m.saveHostKey(hostname, key, true)
		}
		return nil
	}
}

// saveHostKey appends key to Termwright's known_hosts, optionally removing
// existing entries for the host first.
func (m *Manager) saveHostKey(hostname string, key ssh.PublicKey, replace bool) error {
	f := m.knownHostsFile()
	norm := knownhosts.Normalize(hostname)
	if replace {
		data, _ := os.ReadFile(f)
		var keep []string
		for _, l := range strings.Split(string(data), "\n") {
			fields := strings.Fields(l)
			if len(fields) > 0 && hostListContains(fields[0], norm) {
				continue
			}
			if strings.TrimSpace(l) != "" {
				keep = append(keep, l)
			}
		}
		out := strings.Join(keep, "\n")
		if out != "" {
			out += "\n"
		}
		if err := os.WriteFile(f, []byte(out), 0o600); err != nil {
			return err
		}
	}
	fh, err := os.OpenFile(f, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = fmt.Fprintln(fh, knownhosts.Line([]string{norm}, key))
	return err
}

func hostListContains(list, host string) bool {
	for _, h := range strings.Split(list, ",") {
		if h == host {
			return true
		}
	}
	return false
}
