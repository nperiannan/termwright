package sim

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
)

// Serve starts every switch: SSH on 127.0.0.1 (a port of 0 gets a free one,
// written back to the spec) until Stop.
func (l *Lab) Serve(_ context.Context, hostKey ssh.Signer) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.key = hostKey
	for _, d := range l.devices {
		if err := l.startLocked(d); err != nil {
			l.stopAllLocked()
			return err
		}
	}
	return nil
}

// StartDevice starts one switch (the lab's host key must be set by Serve or SetKey).
func (l *Lab) StartDevice(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.Device(id)
	if d == nil {
		return errors.New("no such switch")
	}
	return l.startLocked(d)
}

// StopDevice stops one switch and closes its logins; what was not saved is lost.
func (l *Lab) StopDevice(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if d := l.Device(id); d != nil {
		l.stopLocked(d)
	}
}

// SetKey sets the host key without starting anything.
func (l *Lab) SetKey(k ssh.Signer) { l.mu.Lock(); l.key = k; l.mu.Unlock() }

func (l *Lab) startLocked(d *Device) error {
	if l.lns == nil {
		l.lns = map[string]net.Listener{}
	}
	if l.lns[d.ID] != nil {
		return nil
	}
	if l.key == nil {
		return errors.New("no host key")
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", d.Port))
	if err != nil {
		return fmt.Errorf("%s: port %d: %w", d.cur.Hostname, d.Port, err)
	}
	d.Port = ln.Addr().(*net.TCPAddr).Port
	d.reboot()
	l.lns[d.ID] = ln
	go l.accept(ln, d, l.key)
	return nil
}

func (l *Lab) stopLocked(d *Device) {
	if ln := l.lns[d.ID]; ln != nil {
		ln.Close()
		delete(l.lns, d.ID)
	}
	l.conns.Range(func(k, v any) bool {
		if v.(string) == d.ID {
			k.(net.Conn).Close()
		}
		return true
	})
}

func (l *Lab) stopAllLocked() {
	for _, d := range l.devices {
		l.stopLocked(d)
	}
}

// Stop stops every switch and closes every login.
func (l *Lab) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopAllLocked()
}

// Running reports whether any switch runs.
func (l *Lab) Running() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.lns) > 0
}

// IsRunning reports whether one switch runs.
func (l *Lab) IsRunning(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lns[id] != nil
}

// Logins: how many SSH logins each switch has now (by id).
func (l *Lab) Logins() map[string]int {
	out := map[string]int{}
	l.conns.Range(func(_, v any) bool { out[v.(string)]++; return true })
	return out
}

func (l *Lab) accept(ln net.Listener, d *Device, key ssh.Signer) {
	cfg := &ssh.ServerConfig{ServerVersion: "SSH-2.0-OpenSSH_9.6"}
	if d.Password == "" {
		cfg.NoClientAuth = true
		cfg.PasswordCallback = func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }
		cfg.KeyboardInteractiveCallback = func(ssh.ConnMetadata, ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) { return nil, nil }
	} else {
		cfg.PasswordCallback = func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if subtle.ConstantTimeCompare(pw, []byte(d.Password)) == 1 {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		}
	}
	cfg.AddHostKey(key)
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		l.conns.Store(c, d.ID)
		go func() {
			defer func() { l.conns.Delete(c); c.Close() }()
			l.serveConn(c, cfg, d)
		}()
	}
}

func (l *Lab) serveConn(c net.Conn, cfg *ssh.ServerConfig, d *Device) {
	_, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	var wg sync.WaitGroup
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer ch.Close()
			var rows atomic.Int32
			var shp atomic.Pointer[Shell]
			for r := range creqs {
				switch r.Type {
				case "pty-req", "window-change":
					if h := ptyRows(r.Type, r.Payload); h > 0 {
						rows.Store(int32(h))
						if sh := shp.Load(); sh != nil {
							sh.rows = h
						}
					}
					_ = r.Reply(true, nil)
				case "env":
					_ = r.Reply(true, nil)
				case "shell":
					_ = r.Reply(true, nil)
					sh := newShell(d, ch)
					if h := rows.Load(); h > 0 {
						sh.rows = int(h)
					}
					shp.Store(sh)
					go func() {
						sh.Run(ch)
						_, _ = ch.SendRequest("exit-status", false, binary.BigEndian.AppendUint32(nil, 0))
						ch.Close()
					}()
				case "exec":
					_ = r.Reply(true, nil)
					cmd := ""
					if len(r.Payload) > 4 {
						cmd = string(r.Payload[4:])
					}
					go func() {
						s := newShell(d, ch)
						if d.osd.ExecMode != "" {
							s.mode = d.osd.ExecMode
						}
						s.exec(cmd)
						s.flush()
						_, _ = ch.SendRequest("exit-status", false, binary.BigEndian.AppendUint32(nil, 0))
						ch.Close()
					}()
				default:
					_ = r.Reply(false, nil)
				}
			}
		}()
	}
	wg.Wait()
}

// ptyRows reads the terminal height from a pty-req or window-change request.
func ptyRows(typ string, p []byte) int {
	if typ == "pty-req" {
		if len(p) < 4 {
			return 0
		}
		n := int(binary.BigEndian.Uint32(p))
		p = p[4+min(n, len(p)-4):]
	}
	if len(p) < 8 {
		return 0
	}
	return int(binary.BigEndian.Uint32(p[4:8]))
}

// NewHostKey makes an Ed25519 host key (PEM, to keep) and its signer.
func NewHostKey() ([]byte, ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	b, err := ssh.MarshalPrivateKey(priv, "termwright simulator")
	if err != nil {
		return nil, nil, err
	}
	sg, err := ssh.NewSignerFromKey(priv)
	return pem.EncodeToMemory(b), sg, err
}
