package term

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
)

// forwarder owns the listeners of a connection's port forwards.
type forwarder struct {
	mu        sync.Mutex
	listeners []net.Listener
}

// Close stops all listeners (safe on nil).
func (f *forwarder) Close() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.listeners {
		_ = l.Close()
	}
	f.listeners = nil
}

func bindAddr(pf session.PortForward) string {
	host := pf.BindAddr
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(pf.BindPort))
}

// startForwards opens local (-L), remote (-R) and dynamic SOCKS (-D) forwards.
func (m *Manager) startForwards(client *ssh.Client, fwds []session.PortForward) *forwarder {
	f := &forwarder{}
	for _, pf := range fwds {
		dest := net.JoinHostPort(pf.DestHost, strconv.Itoa(pf.DestPort))
		var ln net.Listener
		var err error
		var desc string
		switch pf.Type {
		case "remote":
			ln, err = client.Listen("tcp", bindAddr(pf))
			desc = fmt.Sprintf("remote %s -> %s", bindAddr(pf), dest)
			if err == nil {
				go serve(ln, func(c net.Conn) {
					rc, err := net.Dial("tcp", dest)
					if err != nil {
						c.Close()
						return
					}
					pipe(c, rc)
				})
			}
		case "dynamic":
			ln, err = net.Listen("tcp", bindAddr(pf))
			desc = fmt.Sprintf("SOCKS %s", bindAddr(pf))
			if err == nil {
				go serve(ln, func(c net.Conn) { socksServe(c, client) })
			}
		default: // local
			ln, err = net.Listen("tcp", bindAddr(pf))
			desc = fmt.Sprintf("local %s -> %s", bindAddr(pf), dest)
			if err == nil {
				go serve(ln, func(c net.Conn) {
					rc, err := client.Dial("tcp", dest)
					if err != nil {
						c.Close()
						return
					}
					pipe(c, rc)
				})
			}
		}
		if err != nil {
			m.emit.Emit("toast", map[string]string{"level": "error", "message": "Port forward " + desc + ": " + err.Error()})
			continue
		}
		f.mu.Lock()
		f.listeners = append(f.listeners, ln)
		f.mu.Unlock()
		m.emit.Emit("toast", map[string]string{"level": "info", "message": "Forwarding " + desc})
	}
	return f
}

func serve(ln net.Listener, handle func(net.Conn)) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go handle(c)
	}
}

func pipe(a, b net.Conn) {
	var once sync.Once
	closeBoth := func() { a.Close(); b.Close() }
	go func() { _, _ = io.Copy(a, b); once.Do(closeBoth) }()
	_, _ = io.Copy(b, a)
	once.Do(closeBoth)
}

// socksServe handles one SOCKS5 CONNECT request and tunnels it over SSH.
func socksServe(c net.Conn, client *ssh.Client) {
	target, err := socksHandshake(c)
	if err != nil {
		c.Close()
		return
	}
	rc, err := client.Dial("tcp", target)
	if err != nil {
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}) // connection refused
		c.Close()
		return
	}
	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		c.Close()
		rc.Close()
		return
	}
	pipe(c, rc)
}

func socksHandshake(c net.Conn) (string, error) {
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return "", err
	}
	if hdr[0] != 5 {
		return "", errors.New("only SOCKS5 is supported")
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return "", err
	}
	if _, err := c.Write([]byte{5, 0}); err != nil { // no authentication
		return "", err
	}
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil {
		return "", err
	}
	if req[1] != 1 { // CONNECT only
		_, _ = c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return "", errors.New("unsupported SOCKS command")
	}
	var host string
	switch req[3] {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(c, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(c, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return "", err
		}
		b := make([]byte, l[0])
		if _, err := io.ReadFull(c, b); err != nil {
			return "", err
		}
		host = string(b)
	default:
		return "", errors.New("bad address type")
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(c, pb); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb)))), nil
}
