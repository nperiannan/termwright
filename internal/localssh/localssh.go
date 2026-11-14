// Package localssh is an SSH connection to this computer that never leaves the process: the client and a
// small server talk over an in-memory pipe, so no port is opened and nothing else can reach it. Code written
// for a remote host (commands, SFTP, tunnels) then works on this computer unchanged.
package localssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Dial makes the connection. Its sessions run their command with sh in dir (stdin, stdout, stderr and the
// exit status as over SSH); its "sftp" subsystem is this computer's files (relative paths from dir); its
// tunnels (direct-tcpip) connect from this computer. Closing the client stops what its sessions still run.
func Dial(dir string) (*ssh.Client, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	c1, c2, err := pipe()
	if err != nil {
		return nil, err
	}
	go serve(c2, cfg, dir)
	cc, chans, reqs, err := ssh.NewClientConn(c1, "this-computer", &ssh.ClientConfig{User: "local", HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()), Timeout: 10 * time.Second})
	if err != nil {
		c1.Close()
		return nil, err
	}
	return ssh.NewClient(cc, chans, reqs), nil
}

func serve(c net.Conn, cfg *ssh.ServerConfig, dir string) {
	sc, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		c.Close()
		return
	}
	defer sc.Close()
	go func() { // keepalives and the like: answered, nothing else to do
		for r := range reqs {
			if r.WantReply {
				_ = r.Reply(true, nil)
			}
		}
	}()
	for nc := range chans {
		switch nc.ChannelType() {
		case "session":
			go session(nc, dir)
		case "direct-tcpip":
			go tunnel(nc)
		default:
			_ = nc.Reject(ssh.UnknownChannelType, "not supported")
		}
	}
}

// tunnel connects a direct-tcpip channel to the address it asks for, from this computer.
func tunnel(nc ssh.NewChannel) {
	var p struct {
		Host     string
		Port     uint32
		FromHost string
		FromPort uint32
	}
	if err := ssh.Unmarshal(nc.ExtraData(), &p); err != nil {
		_ = nc.Reject(ssh.Prohibited, "bad request")
		return
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port))), 10*time.Second)
	if err != nil {
		_ = nc.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nc.Accept()
	if err != nil {
		conn.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	var once sync.Once
	done := func() { once.Do(func() { conn.Close(); ch.Close() }) }
	go func() { _, _ = io.Copy(conn, ch); done() }()
	_, _ = io.Copy(ch, conn)
	done()
}

// session runs a command (exec) or serves files (the sftp subsystem); env requests set its environment.
func session(nc ssh.NewChannel, dir string) {
	ch, reqs, err := nc.Accept()
	if err != nil {
		return
	}
	var (
		env  []string
		mu   sync.Mutex
		cmd  *exec.Cmd
		done chan struct{}
	)
	for r := range reqs {
		switch r.Type {
		case "env":
			var p struct{ Name, Value string }
			if ssh.Unmarshal(r.Payload, &p) == nil {
				env = append(env, p.Name+"="+p.Value)
			}
			reply(r, true)
		case "exec":
			var p struct{ Command string }
			if ssh.Unmarshal(r.Payload, &p) != nil {
				reply(r, false)
				continue
			}
			c := exec.Command("sh", "-c", p.Command)
			c.Dir, c.Env = dir, append(Environ(), env...)
			c.Stdin, c.Stdout, c.Stderr = ch, ch, ch.Stderr()
			c.WaitDelay = 5 * time.Second // a program it left running (a daemon) may keep its output open
			reply(r, true)
			if err := c.Start(); err != nil {
				_, _ = io.WriteString(ch.Stderr(), err.Error()+"\n")
				exit(ch, 127)
				continue
			}
			mu.Lock()
			cmd, done = c, make(chan struct{})
			mu.Unlock()
			go func(d chan struct{}) {
				code := 0
				if err := c.Wait(); err != nil {
					var ee *exec.ExitError
					if errors.As(err, &ee) && ee.ExitCode() >= 0 {
						code = ee.ExitCode()
					} else if !errors.Is(err, exec.ErrWaitDelay) {
						code = 127
					}
				}
				close(d)
				exit(ch, code)
			}(done)
		case "subsystem":
			var p struct{ Name string }
			if ssh.Unmarshal(r.Payload, &p) != nil || p.Name != "sftp" {
				reply(r, false)
				continue
			}
			reply(r, true)
			go func() {
				if srv, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(dir)); err == nil {
					_ = srv.Serve()
					srv.Close()
				}
				ch.Close()
			}()
		default: // pty-req, shell…: not here
			reply(r, false)
		}
	}
	// the client closed the session: what it ran stops too
	mu.Lock()
	if cmd != nil {
		select {
		case <-done:
		default:
			_ = cmd.Process.Kill()
		}
	}
	mu.Unlock()
}

// exit ends a session with the command's exit status.
func exit(ch ssh.Channel, code int) {
	_ = ch.CloseWrite()
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{uint32(code)}))
	ch.Close()
}

func reply(r *ssh.Request, ok bool) {
	if r.WantReply {
		_ = r.Reply(ok, nil)
	}
}

// Environ is the environment commands get: this process's, with the usual tool folders on PATH (an app opened
// from the Finder has a short one).
func Environ() []string {
	env := os.Environ()
	path := os.Getenv("PATH")
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		if !slices.Contains(filepath.SplitList(path), d) {
			if path != "" {
				path += string(os.PathListSeparator)
			}
			path += d
		}
	}
	return append(env, "PATH="+path)
}

// pipe is a connected pair of in-memory connections. Unlike net.Pipe they are buffered (the kernel's pipes):
// both ends of SSH write their version first, which a pipe without a buffer deadlocks on.
func pipe() (net.Conn, net.Conn, error) {
	r1, w1, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	r2, w2, err := os.Pipe()
	if err != nil {
		r1.Close()
		w1.Close()
		return nil, nil, err
	}
	return &pipeConn{r: r2, w: w1}, &pipeConn{r: r1, w: w2}, nil
}

type pipeConn struct{ r, w *os.File }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "this-computer" }

func (p *pipeConn) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeConn) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeConn) Close() error {
	p.w.Close()
	return p.r.Close()
}
func (p *pipeConn) LocalAddr() net.Addr                { return pipeAddr{} }
func (p *pipeConn) RemoteAddr() net.Addr               { return pipeAddr{} }
func (p *pipeConn) SetDeadline(t time.Time) error      { _ = p.w.SetDeadline(t); return p.r.SetDeadline(t) }
func (p *pipeConn) SetReadDeadline(t time.Time) error  { return p.r.SetDeadline(t) }
func (p *pipeConn) SetWriteDeadline(t time.Time) error { return p.w.SetDeadline(t) }
