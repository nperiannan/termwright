package clirun

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

const showSwitch = "SysName:          lab-5520-1\nSystem MAC:       00:04:96:9F:12:30\nSystem Type:      5520-24T\n"
const showVersion = "Switch      : 800744-00-04 2133N-41234 Rev 4.0 BootROM: 3.5.1.6    IMG: 32.5.1.5\n" +
	"Slot-1      : 800744-00-04 2133N-41234\n"

// cliShell is a switch's command line: it echoes what is typed, shows a prompt, pages long output until
// paging is off, and answers the show commands.
func cliShell(rw io.ReadWriter, crlf bool) {
	nl := "\n"
	if crlf {
		nl = "\r\n"
	}
	put := func(s string) { _, _ = io.WriteString(rw, strings.ReplaceAll(s, "\n", nl)) }
	n, paging := 1, true
	put("\x1b[2J\nExtreme Networks Switch Engine\n")
	prompt := func() { put(fmt.Sprintf("* lab-5520-1.%d # ", n)); n++ }
	prompt()
	br := bufio.NewReader(rw)
	var line strings.Builder
	for {
		b, err := br.ReadByte()
		if err != nil {
			return
		}
		if b == '\n' && line.Len() == 0 {
			continue
		}
		if b != '\r' && b != '\n' {
			line.WriteByte(b)
			_, _ = rw.Write([]byte{b}) // echo
			continue
		}
		put("\n")
		cmd := strings.TrimSpace(line.String())
		line.Reset()
		switch cmd {
		case "disable clipaging":
			paging = false
		case "show switch":
			put(showSwitch)
		case "show version":
			put("Switch      : 800744-00-04 2133N-41234 Rev 4.0 BootROM: 3.5.1.6    IMG: 32.5.1.5\n")
			if paging {
				put("Press <SPACE> to continue or <Q> to quit:")
				if c, _ := br.ReadByte(); c != ' ' {
					put("\n")
					prompt()
					continue
				}
				put("\r                                          \r")
			}
			put("Slot-1      : 800744-00-04 2133N-41234\n")
		case "exit":
			put("logout\n")
			return
		case "":
		default:
			put("%% Invalid input detected at '^' marker.\n")
		}
		prompt()
	}
}

func sshDevice(t *testing.T, user, pw string) (addr string, key ssh.PublicKey) {
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(k)
	cfg := &ssh.ServerConfig{KeyboardInteractiveCallback: func(c ssh.ConnMetadata, ch ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		a, err := ch("", "", []string{"password: "}, []bool{false})
		if err == nil && c.User() == user && len(a) == 1 && a[0] == pw {
			return nil, nil
		}
		return nil, errors.New("no")
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					ch, rq, _ := nc.Accept()
					go func() {
						for r := range rq {
							_ = r.Reply(r.Type == "pty-req" || r.Type == "shell", nil)
							if r.Type == "shell" {
								go func() { cliShell(ch, true); ch.Close() }()
							}
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

func target(addr, proto, user, pw string) Target {
	h, p, _ := net.SplitHostPort(addr)
	var port int
	fmt.Sscan(p, &port)
	return Target{Protocol: proto, Host: h, Port: port, User: user, Password: pw}
}

// SSH: a login with an empty password (keyboard-interactive), a pager answered, the outputs; a wrong
// password and a changed host key.
func TestRunSSH(t *testing.T) {
	Quiet = 150 * time.Millisecond
	addr, key := sshDevice(t, "admin", "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	outs, hk, err := Run(ctx, target(addr, "SSH2", "admin", ""), "", []string{"show switch", "show version"})
	if err != nil {
		t.Fatal(err)
	}
	if hk != ssh.FingerprintSHA256(key) || len(outs) != 2 || !strings.Contains(outs[0], "System Type:      5520-24T") || strings.Contains(outs[0], "show switch") ||
		!strings.Contains(outs[1], "Slot-1") || strings.Contains(outs[1], "lab-5520-1.") {
		t.Fatalf("%s %q", hk, outs)
	}
	if _, _, err := Run(ctx, target(addr, "SSH2", "admin", "x"), "disable clipaging", nil); !errors.Is(err, ErrLogin) {
		t.Fatalf("wrong password: %v", err)
	}
	tg := target(addr, "SSH2", "admin", "")
	tg.HostKey = "SHA256:other"
	var he *HostKeyError
	if _, _, err := Run(ctx, tg, "", nil); !errors.As(err, &he) || he.Got != hk {
		t.Fatalf("host key: %v", err)
	}
}

// Telnet: its options refused, user and password typed, paging turned off; a wrong password.
func TestRunTelnet(t *testing.T) {
	Quiet = 150 * time.Millisecond
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = c.Write([]byte{255, 251, 1, 255, 253, 31, 255, 250, 24, 1, 255, 240})
				br := bufio.NewReader(c)
				ask := func(p string) string {
					_, _ = io.WriteString(c, p)
					l, _ := br.ReadString('\n')
					return strings.Trim(l, "\r\n\x00\xff\xfb\xfc\xfd\xfe\x01\x1f")
				}
				for {
					u := ask("\r\nlogin: ")
					if strings.HasSuffix(u, "rwa") && ask("Password: ") == "rwa" {
						break
					}
					_, _ = io.WriteString(c, "\r\nLogin incorrect")
				}
				cliShell(struct {
					io.Reader
					io.Writer
				}{br, c}, true)
			}()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	outs, _, err := Run(ctx, target(ln.Addr().String(), "Telnet", "rwa", "rwa"), "disable clipaging", []string{"show version"})
	if err != nil || len(outs) != 1 || !strings.Contains(outs[0], "Slot-1") || strings.Contains(outs[0], "SPACE") {
		t.Fatalf("%v %q", err, outs)
	}
	if _, _, err := Run(ctx, target(ln.Addr().String(), "Telnet", "rwa", "bad"), "", nil); !errors.Is(err, ErrLogin) {
		t.Fatalf("wrong password: %v", err)
	}
}
