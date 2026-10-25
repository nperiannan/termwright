package term

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Browsing as root: the SFTP server program is started through sudo on an
// exec channel (like WinSCP's "sudo sftp-server"). sudo's password prompt is
// recognised on stderr, so the password is only sent when sudo asks for it.

const (
	sudoPrompt = "TWSUDO:"
	sudoReady  = "TWREADY"
)

// sftpServerPaths are where distributions install the OpenSSH SFTP server.
var sftpServerPaths = []string{
	"/usr/libexec/openssh/sftp-server", "/usr/lib/openssh/sftp-server", "/usr/lib/ssh/sftp-server",
	"/usr/libexec/sftp-server", "/usr/lib/sftp-server", "/usr/local/libexec/sftp-server",
}

// sudoSFTPCommand finds sftp-server and runs it as root with sudo.
func sudoSFTPCommand() string {
	return "P=; for f in " + strings.Join(sftpServerPaths, " ") + `; do [ -x "$f" ] && P=$f && break; done; ` +
		`[ -n "$P" ] || { echo "TWERR:sftp-server was not found on this host" >&2; exit 1; }; ` +
		"exec sudo -S -p " + sudoPrompt + ` sh -c 'echo ` + sudoReady + ` >&2; exec "$0"' "$P"`
}

// sftpAsRoot starts sftp-server as root on client (or on the hop target
// through it). askPass is called when sudo wants a password.
func sftpAsRoot(ctx context.Context, client *ssh.Client, hop *Hop, askPass func(retry bool) (string, error)) (*sftp.Client, func(), error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	w, err1 := sess.StdinPipe()
	r, err2 := sess.StdoutPipe()
	er, err3 := sess.StderrPipe()
	if err = errors.Join(err1, err2, err3); err != nil {
		_ = sess.Close()
		return nil, nil, err
	}
	cmd := sudoSFTPCommand()
	if hop != nil {
		cmd = hop.ExecCommand(cmd)
	}
	if err := sess.Start(cmd); err != nil {
		_ = sess.Close()
		return nil, nil, err
	}
	fail := func(err error) (*sftp.Client, func(), error) {
		_ = sess.Close()
		return nil, nil, err
	}
	// read stderr until sudo either asks for a password or starts sftp-server
	events := make(chan string, 4)
	var tail limitedBuf
	go func() {
		br := bufio.NewReader(er)
		var cur strings.Builder
		for {
			b, err := br.ReadByte()
			if err != nil {
				_, _ = tail.Write([]byte(cur.String()))
				events <- "eof"
				return
			}
			cur.WriteByte(b)
			s := cur.String()
			switch {
			case strings.HasSuffix(s, sudoPrompt):
				events <- "pass"
				cur.Reset()
			case strings.HasSuffix(s, sudoReady+"\n"):
				events <- "ready"
				_, _ = io.Copy(&tail, br) // keep draining
				return
			case b == '\n':
				_, _ = tail.Write([]byte(s))
				cur.Reset()
			}
		}
	}()
	tries := 0
	for {
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-time.After(30 * time.Second):
			return fail(errors.New("sudo did not answer: " + strings.TrimSpace(tail.String())))
		case ev := <-events:
			switch ev {
			case "pass":
				pw, err := askPass(tries > 0)
				if err != nil {
					return fail(err)
				}
				tries++
				if _, err := io.WriteString(w, pw+"\n"); err != nil {
					return fail(err)
				}
				continue
			case "eof":
				msg := strings.TrimSpace(strings.ReplaceAll(tail.String(), "TWERR:", ""))
				if msg == "" {
					msg = "sudo refused to start the SFTP server as root"
				}
				return fail(fmt.Errorf("root access: %s", msg))
			}
		}
		break // ready
	}
	c, err := sftp.NewClientPipe(r, w, sftp.UseConcurrentWrites(true), sftp.UseConcurrentReads(true))
	if err != nil {
		return fail(fmt.Errorf("root access: %v %s", err, strings.TrimSpace(tail.String())))
	}
	return c, func() { _ = sess.Close() }, nil
}

// SFTPSetRoot switches an SFTP tab between the login user and root (via
// sudo). It returns the folder to show: /root when it can be read, else the
// current home.
func (m *Manager) SFTPSetRoot(id string, root bool) (string, error) {
	s, err := m.sftpGet(id)
	if err != nil {
		return "", err
	}
	client := s.client
	if client == nil {
		if client, err = m.sshClientOf(s.tabID); err != nil {
			return "", err
		}
	}
	var c *sftp.Client
	var stop func()
	if root {
		host := "this server"
		if s.hop != nil {
			host = s.hop.Target
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		ask := func(retry bool) (string, error) {
			if s.sudoPass != "" && !retry {
				return s.sudoPass, nil
			}
			msg := "sudo password on " + host + " (your login password there)"
			if retry {
				msg = "Sorry, try again. " + msg
			}
			r, err := m.prompts.Ask(ctx, Prompt{TabID: id, Kind: "credentials", Title: "Browse as root",
				Message: msg, Fields: []PromptField{{Label: "Password", Secret: true}}})
			if err != nil {
				return "", err
			}
			s.sudoPass = r.Values[0]
			return r.Values[0], nil
		}
		c, stop, err = sftpAsRoot(ctx, client, s.hop, ask)
	} else {
		c, stop, err = startSFTP(client, s.hop)
	}
	if err != nil {
		return "", err
	}
	ns := *s
	ns.c, ns.stop, ns.root = c, stop, root
	m.mu.Lock()
	if _, open := m.sftps[id]; !open {
		m.mu.Unlock()
		_ = c.Close()
		stop()
		return "", errors.New("SFTP session is closed")
	}
	m.sftps[id] = &ns
	m.mu.Unlock()
	_ = s.c.Close() // the previous user's channel; the connection stays
	if s.stop != nil {
		s.stop()
	}
	home, err := c.Getwd()
	if err != nil || home == "" {
		home = "/"
	}
	if root {
		if _, err := c.ReadDir("/root"); err == nil {
			home = "/root"
		}
	}
	return home, nil
}
