package term

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/sshkey"
)

// runRemote runs one command on a new channel of client, feeding stdin, and
// returns stdout followed by stderr.
func runRemote(client *ssh.Client, cmd, stdin string, timeout time.Duration) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	var stdout, stderr bytes.Buffer
	sess.Stdout, sess.Stderr = &stdout, &stderr
	sess.Stdin = strings.NewReader(stdin)
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case err = <-done:
	case <-time.After(timeout):
		_ = sess.Close()
		<-done
		err = errors.New("timed out")
	}
	return strings.TrimSpace(stdout.String() + "\n" + stderr.String()), err
}

// InstallPublicKey adds a public key line to the authorized keys of the
// account the SSH2 tab is logged in to. Linux/macOS/BSD (~/.ssh/authorized_keys)
// and Windows OpenSSH (user or administrators_authorized_keys) are supported.
// It returns a short description of what was done.
func (m *Manager) InstallPublicKey(tabID, line string) (string, error) {
	key, err := sshkey.Normalize(line)
	if err != nil {
		return "", err
	}
	client, err := m.sshClientOf(tabID)
	if err != nil {
		return "", err
	}
	osName, uerr := runRemote(client, "uname -s", "", 15*time.Second)
	var out string
	switch {
	case uerr == nil && osName != "" && !strings.ContainsAny(osName, "\r\n"):
		out, err = runRemote(client, sshkey.PosixInstallCommand, key+"\n", 30*time.Second)
	default:
		out, err = runRemote(client, sshkey.WindowsInstallCommand(key), "", 60*time.Second)
		osName = "Windows"
	}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if p, ok := strings.CutPrefix(l, "TERMWRIGHT-ADDED "); ok {
			return fmt.Sprintf("Public key added to %s (%s)", p, osName), nil
		}
		if p, ok := strings.CutPrefix(l, "TERMWRIGHT-PRESENT "); ok {
			return fmt.Sprintf("Public key was already in %s (%s)", p, osName), nil
		}
	}
	if err == nil {
		err = errors.New("unexpected reply")
	}
	if len(out) > 400 {
		out = out[:400] + "…"
	}
	return "", fmt.Errorf("could not install the key automatically (%v). This server may not offer a Unix or Windows "+
		"command shell (e.g. a network switch); add the public key with the device's own command instead.\n%s", err, out)
}

// TestKeyLogin opens a separate connection to the tab's host that may only
// use the given key, proving that key login works before the session is
// switched to it.
func (m *Manager) TestKeyLogin(tabID, keyPath, passphrase string) error {
	client, err := m.sshClientOf(tabID)
	if err != nil {
		return err
	}
	t, err := m.get(tabID)
	if err != nil {
		return err
	}
	t.mu.Lock()
	s := t.sess
	t.mu.Unlock()
	signer, err := sshkey.LoadSigner(expandHome(keyPath), passphrase)
	if err != nil {
		return err
	}
	port := s.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(s.Hostname, strconv.Itoa(port))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	probe := &Tab{id: tabID, sess: s}
	cfg := &ssh.ClientConfig{
		User:            client.User(),
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: m.hostKeyCallback(ctx, probe),
		Timeout:         20 * time.Second,
		ClientVersion:   "SSH-2.0-Termwright",
	}
	if err := applyAlgorithms(cfg, s, m.knownHostAlgorithms(addr)); err != nil {
		return err
	}
	raw, _, err := m.dialNet(ctx, probe, s, addr, 0)
	if err != nil {
		return err
	}
	defer raw.Close()
	cc, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
	if err != nil {
		return fmt.Errorf("key login failed: %w", err)
	}
	return ssh.NewClient(cc, chans, reqs).Close()
}
