package term

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// "Run as" for SFTP: any command that gives a shell as another user
// ("sudo su - oracle", "su - admin", "sudo -iu postgres", "pbrun …") is run on
// a terminal like in a terminal tab. Password prompts are answered by the
// user; once the new shell is quiet, Termwright types one line that switches
// the terminal to raw mode and replaces the shell with sftp-server, whose
// binary protocol then flows over the same channel.

var passwordPrompt = regexp.MustCompile(`(?i)(pass(word|phrase|code)|pin|token|otp)[^\n]{0,60}[:?]\s*$`)

// runAsLine is typed into the switched shell. The marker is written as
// \124 (= "T") so the echo of this line never matches it.
func runAsLine() string {
	script := `stty raw -echo -iexten cs8 2>/dev/null; P=; for f in ` + strings.Join(sftpServerPaths, " ") +
		`; do [ -x "$f" ] && P=$f && break; done; [ -n "$P" ] || { printf "\124WERR:sftp-server was not found\n"; exit 1; }; ` +
		`printf "\124WREADY:%s\n" "$(id -un)"; exec "$P"`
	return "exec /bin/sh -c '" + script + "'\n"
}

var (
	runAsReady = regexp.MustCompile(`TWREADY:([^\r\n]*)\r?\n`)
	runAsErr   = regexp.MustCompile(`TWERR:([^\r\n]*)`)
)

// sftpRunAs runs command on a terminal and starts sftp-server as the user it
// switches to. It returns the client, a stop function and the user name.
func sftpRunAs(ctx context.Context, client *ssh.Client, hop *Hop, command string, askPass func(prompt string, retry bool) (string, error)) (*sftp.Client, func(), string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, nil, "", err
	}
	done := make(chan struct{}) // stops the reader goroutine
	fail := func(err error) (*sftp.Client, func(), string, error) {
		close(done)
		_ = sess.Close()
		return nil, nil, "", err
	}
	w, err1 := sess.StdinPipe()
	r, err2 := sess.StdoutPipe()
	if err = errors.Join(err1, err2); err != nil {
		return fail(err)
	}
	sess.Stderr = io.Discard
	cmd := command
	if hop != nil {
		cmd = hop.TTYCommand(command) // the target gets the terminal; the jump station just relays bytes
	} else if err := sess.RequestPty("xterm", 40, 200, ssh.TerminalModes{ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}); err != nil {
		return fail(err)
	}
	if err := sess.Start(cmd); err != nil {
		return fail(err)
	}

	// The reader hands over chunks one by one, so it stops exactly when the
	// marker is seen and the rest of the stream belongs to SFTP.
	type chunk struct {
		b   []byte
		err error
	}
	chunks := make(chan chunk)
	next := make(chan bool, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			select {
			case chunks <- chunk{append([]byte(nil), buf[:n]...), err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
			select {
			case ok := <-next:
				if !ok {
					return
				}
			case <-done:
				return
			}
		}
	}()

	var out bytes.Buffer // recent output (for prompts and errors)
	const quiet = 1200 * time.Millisecond
	sentLine, tries := false, 0
	deadline := time.After(2 * time.Minute)
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-deadline:
			return fail(fmt.Errorf("%q did not give a shell: %s", command, lastLines(out.String())))
		case c := <-chunks:
			out.Write(c.b)
			if out.Len() > 8192 {
				out.Next(out.Len() - 8192)
			}
			text := out.String()
			if m := runAsReady.FindStringSubmatchIndex(text); m != nil && sentLine {
				user := strings.TrimSpace(text[m[2]:m[3]])
				rest := append([]byte(nil), out.Bytes()[m[1]:]...)
				if c.err != nil {
					return fail(c.err)
				}
				next <- false // stop the reader: the remaining stream is SFTP
				sc, err := sftp.NewClientPipe(io.MultiReader(bytes.NewReader(rest), r), w,
					sftp.UseConcurrentWrites(true), sftp.UseConcurrentReads(true))
				if err != nil {
					return fail(fmt.Errorf("start SFTP as %s: %w", user, err))
				}
				return sc, func() { _ = sess.Close() }, user, nil
			}
			if m := runAsErr.FindStringSubmatch(text); m != nil && sentLine {
				return fail(errors.New(m[1]))
			}
			if c.err != nil {
				return fail(fmt.Errorf("%q ended: %s", command, lastLines(text)))
			}
			next <- true
			if !sentLine && passwordPrompt.MatchString(strings.TrimRight(text, " ")) {
				pw, err := askPass(lastLines(text), tries > 0)
				if err != nil {
					return fail(err)
				}
				tries++
				out.Reset()
				if _, err := io.WriteString(w, pw+"\n"); err != nil {
					return fail(err)
				}
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(quiet)
		case <-timer.C:
			// output stopped and no password is wanted: the new shell is waiting
			if !sentLine {
				if passwordPrompt.MatchString(strings.TrimRight(out.String(), " ")) {
					timer.Reset(quiet)
					continue
				}
				sentLine = true
				if _, err := io.WriteString(w, runAsLine()); err != nil {
					return fail(err)
				}
			}
			timer.Reset(quiet)
		}
	}
}

// lastLines returns the last two non-empty lines of terminal output, without escape codes.
func lastLines(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 2 {
		lines = lines[len(lines)-2:]
	}
	return strings.Join(lines, " / ")
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07]*\x07`)

// SFTPRunAs switches an SFTP tab to the user that command logs in to (e.g.
// "sudo su - oracle"); "" goes back to the login user. It returns the folder
// to show and the user name.
func (m *Manager) SFTPRunAs(id, command string) (string, string, error) {
	command = strings.TrimSpace(command)
	s, err := m.sftpGet(id)
	if err != nil {
		return "", "", err
	}
	client := s.client
	if client == nil {
		if client, err = m.sshClientOf(s.tabID); err != nil {
			return "", "", err
		}
	}
	var c *sftp.Client
	var stop func()
	user := ""
	if command == "" {
		c, stop, err = startSFTP(client, s.hop)
	} else {
		host := "the server"
		if s.hop != nil {
			host = s.hop.Target
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		ask := func(prompt string, retry bool) (string, error) {
			msg := command + " on " + host + " asks: " + prompt
			if retry {
				msg = "Not accepted - try again. " + msg
			}
			r, err := m.prompts.Ask(ctx, Prompt{TabID: id, Kind: "credentials", Title: "Run As",
				Message: msg, Fields: []PromptField{{Label: "Password", Secret: true}}})
			if err != nil {
				return "", err
			}
			return r.Values[0], nil
		}
		c, stop, user, err = sftpRunAs(ctx, client, s.hop, command, ask)
	}
	if err != nil {
		return "", "", err
	}
	ns := *s
	ns.c, ns.stop, ns.root = c, stop, command != ""
	m.mu.Lock()
	if _, open := m.sftps[id]; !open {
		m.mu.Unlock()
		_ = c.Close()
		stop()
		return "", "", errors.New("SFTP session is closed")
	}
	m.sftps[id] = &ns
	m.mu.Unlock()
	_ = s.c.Close()
	if s.stop != nil {
		s.stop()
	}
	home, err := c.Getwd()
	if err != nil || home == "" {
		home = "/"
	}
	return home, user, nil
}

// SFTPLoginShell returns the name of the login shell ("bash", "zsh", …) on the
// host an SFTP tab browses (the jump station's target when it uses one), or ""
// when it cannot be told (network devices, restricted accounts). It decides
// whether folder reporting may be turned on in the terminal automatically.
func (m *Manager) SFTPLoginShell(id string) string {
	s, err := m.sftpGet(id)
	if err != nil {
		return ""
	}
	client := s.client
	if client == nil {
		if client, err = m.sshClientOf(s.tabID); err != nil {
			return ""
		}
	}
	sess, err := client.NewSession()
	if err != nil {
		return ""
	}
	defer sess.Close()
	cmd := `printf 'TWSHELL:%s\n' "${SHELL##*/}"`
	if s.hop != nil {
		cmd = s.hop.ExecCommand(cmd)
	}
	done := make(chan []byte, 1)
	go func() { out, _ := sess.Output(cmd); done <- out }()
	select {
	case out := <-done:
		if m := regexp.MustCompile(`TWSHELL:([\w.-]*)`).FindSubmatch(out); m != nil {
			return string(m[1])
		}
	case <-time.After(15 * time.Second):
	}
	return ""
}
