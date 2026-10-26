package term

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/session"
)

type nopEmitter struct{}

func (nopEmitter) Emit(string, any) {}

// fakeSwitch answers like a VOSS switch over raw TCP.
func fakeSwitch(t *testing.T, password string) string { return fakeSwitchOpt(t, password, false, nil) }

// fakeSwitchOpt: privileged accounts start at "#"; got receives every command.
func fakeSwitchOpt(t *testing.T, password string, privileged bool, got chan<- string) string {
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
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				line := func() string { s, _ := r.ReadString('\r'); return strings.TrimSpace(s) }
				_, _ = c.Write([]byte("Login: "))
				if line() != "rwa" {
					return
				}
				_, _ = c.Write([]byte("\r\nPassword: "))
				if line() != password {
					_, _ = c.Write([]byte("\r\nLogin incorrect\r\n"))
					return
				}
				if privileged {
					_, _ = c.Write([]byte("\r\nsw1:1#"))
				} else {
					_, _ = c.Write([]byte("\r\nsw1:1>"))
				}
				for {
					cmd := line()
					if cmd == "" && r.Buffered() == 0 {
						if _, err := r.Peek(1); err != nil {
							return
						}
					}
					if got != nil && cmd != "" {
						got <- cmd
					}
					_, _ = c.Write([]byte(cmd + "\r\n"))
					switch cmd {
					case "enable":
						_, _ = c.Write([]byte("sw1:1#"))
					case "terminal more disable":
						_, _ = c.Write([]byte("sw1:1#"))
					case "show running-config":
						_, _ = c.Write([]byte("#\r\n# Mon Oct 26 10:22:01 2026 UTC\r\n#\r\nvlan create 10 type port-mstprstp 0\r\n"))
						time.Sleep(300 * time.Millisecond)
						_, _ = c.Write([]byte("vlan members add 10 1/1\r\nend\r\nsw1:1#"))
					default:
						_, _ = c.Write([]byte("sw1:1#"))
					}
				}
			}(c)
		}
	}()
	return ln.Addr().String()
}

func captureManager() *Manager {
	m := NewManager(nopEmitter{}, "", func() string { return "" })
	dt := session.BuiltinDeviceTypes()[1] // VOSS
	dt.Username = "rwa"
	m.DeviceType = func(name string) (session.DeviceType, bool) { return dt, strings.EqualFold(name, "VOSS") }
	return m
}

func TestCaptureRunningConfig(t *testing.T) {
	addr := fakeSwitch(t, "secret")
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	m := captureManager()
	m.DevicePassword = func(string) (string, bool) { return "secret", true }
	s := session.Defaults()
	s.Name, s.Protocol, s.Hostname, s.Port, s.DeviceType = "sw1", session.ProtoRaw, host, p, "VOSS"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := m.Capture(ctx, s, []string{"show running-config"}, `:\d+[>#] ?$`)
	if err != nil {
		t.Fatal(err)
	}
	want := "#\n# Mon Oct 26 10:22:01 2026 UTC\n#\nvlan create 10 type port-mstprstp 0\nvlan members add 10 1/1\nend\n"
	if out != want {
		t.Fatalf("got %q", out)
	}
}

// Without a saved password nothing is asked: the capture fails.
func TestCaptureNeedsPassword(t *testing.T) {
	addr := fakeSwitch(t, "secret")
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	m := captureManager()
	asked := false
	m.prompts.emit = emitFunc(func(name string, _ any) {
		if name == "prompt" {
			asked = true
		}
	})
	s := session.Defaults()
	s.Name, s.Protocol, s.Hostname, s.Port, s.DeviceType = "sw1", session.ProtoRaw, host, p, "VOSS"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := m.Capture(ctx, s, []string{"show running-config"}, "")
	if err == nil || !strings.Contains(err.Error(), "no saved password") || asked {
		t.Fatalf("err %v asked %v", err, asked)
	}
}

type emitFunc func(string, any)

func (f emitFunc) Emit(n string, d any) { f(n, d) }

func TestCleanOutput(t *testing.T) {
	got := cleanOutput("show version\r\nVersion 9.1\r\n\r\nsw1:1#", "show version", "sw1:1#")
	if got != "Version 9.1\n" {
		t.Fatalf("%q", got)
	}
}

// An account that starts privileged ("#"): "enable" is not sent.
func TestCapturePrivilegedSkipsEnable(t *testing.T) {
	got := make(chan string, 20)
	addr := fakeSwitchOpt(t, "secret", true, got)
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	m := captureManager()
	m.DevicePassword = func(string) (string, bool) { return "secret", true }
	s := session.Defaults()
	s.Name, s.Protocol, s.Hostname, s.Port, s.DeviceType = "sw1", session.ProtoRaw, host, p, "VOSS"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := m.Capture(ctx, s, []string{"show running-config"}, `:\d+[>#] ?$`)
	if err != nil || !strings.Contains(out, "vlan members add 10 1/1") {
		t.Fatalf("%q %v", out, err)
	}
	close(got)
	var cmds []string
	for c := range got {
		if c != "rwa" && c != "secret" {
			cmds = append(cmds, c)
		}
	}
	if strings.Join(cmds, "|") != "terminal more disable|show running-config" {
		t.Fatalf("commands %v", cmds)
	}
}

// A failed capture carries what the device sent.
func TestCaptureErrorTranscript(t *testing.T) {
	addr := fakeSwitch(t, "secret")
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	m := captureManager()
	m.DevicePassword = func(string) (string, bool) { return "secret", true }
	s := session.Defaults()
	s.Name, s.Protocol, s.Hostname, s.Port, s.DeviceType = "sw1", session.ProtoRaw, host, p, "VOSS"
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, err := m.Capture(ctx, s, []string{"show running-config"}, `NEVER-MATCHES$`)
	var ce *CaptureError
	if err == nil || !errors.As(err, &ce) || !strings.Contains(ce.Transcript, "Login:") {
		t.Fatalf("%v", err)
	}
}
