package fileserve

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TFTP (RFC 1350) with the options blksize, tsize and timeout (RFC 2347-2349).
// Each transfer gets its own UDP port, as the protocol wants.

const (
	opRRQ   = 1
	opWRQ   = 2
	opDATA  = 3
	opACK   = 4
	opERROR = 5
	opOACK  = 6

	tftpNotFound = 1
	tftpAccess   = 2
	tftpIllegal  = 4
	tftpBadTID   = 5
	tftpOptions  = 8

	tftpRetries = 5
)

type tftpServer struct {
	m    *Manager
	conn net.PacketConn
	ip   net.IP
	wg   sync.WaitGroup
	done chan struct{}
}

func (s *tftpServer) addr() net.Addr { return s.conn.LocalAddr() }

func (s *tftpServer) stop() {
	close(s.done)
	s.conn.Close()
}

func (m *Manager) startTFTP(addr string) (runner, error) {
	c, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	s := &tftpServer{m: m, conn: c, done: make(chan struct{})}
	if ua, ok := c.LocalAddr().(*net.UDPAddr); ok && !ua.IP.IsUnspecified() {
		s.ip = ua.IP
	}
	go s.serve()
	return s, nil
}

func (s *tftpServer) serve() {
	buf := make([]byte, 2048)
	for {
		n, peer, err := s.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		if n < 4 {
			continue
		}
		p := append([]byte(nil), buf[:n]...)
		go s.request(peer, p)
	}
}

type tftpReq struct {
	op      uint16
	file    string
	mode    string
	opts    map[string]string
	optKeys []string
}

func parseTFTP(p []byte) (tftpReq, error) {
	r := tftpReq{op: binary.BigEndian.Uint16(p), opts: map[string]string{}}
	parts := bytes.Split(p[2:], []byte{0})
	if len(parts) < 3 || len(parts[0]) == 0 {
		return r, errors.New("malformed request")
	}
	r.file, r.mode = string(parts[0]), strings.ToLower(string(parts[1]))
	for i := 2; i+1 < len(parts); i += 2 {
		k := strings.ToLower(string(parts[i]))
		if k == "" {
			break
		}
		r.opts[k] = string(parts[i+1])
		r.optKeys = append(r.optKeys, k)
	}
	return r, nil
}

func tftpError(code uint16, msg string) []byte {
	b := make([]byte, 4, 5+len(msg))
	binary.BigEndian.PutUint16(b, opERROR)
	binary.BigEndian.PutUint16(b[2:], code)
	b = append(b, msg...)
	return append(b, 0)
}

func (s *tftpServer) request(peer net.Addr, p []byte) {
	r, err := parseTFTP(p)
	if err != nil || (r.op != opRRQ && r.op != opWRQ) {
		s.conn.WriteTo(tftpError(tftpIllegal, "illegal TFTP operation"), peer)
		return
	}
	put := r.op == opWRQ
	if r.mode != "octet" && r.mode != "netascii" {
		s.conn.WriteTo(tftpError(tftpIllegal, "only octet and netascii modes"), peer)
		return
	}
	name, err := CleanName(r.file)
	if err != nil || name == "" {
		s.m.refuse("TFTP", peer, r.file, put, "Refused - outside the folder")
		s.conn.WriteTo(tftpError(tftpAccess, "access violation"), peer)
		return
	}
	if put && !s.m.canWrite(TFTP) {
		s.m.refuse("TFTP", peer, r.file, put, "Refused - uploads are turned off")
		s.conn.WriteTo(tftpError(tftpAccess, "uploads are turned off"), peer)
		return
	}
	root := s.m.folder()
	if root == nil {
		return
	}
	// a new port for this transfer (its "transfer ID")
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: s.ip})
	if err != nil {
		s.conn.WriteTo(tftpError(0, "server busy"), peer)
		return
	}
	defer c.Close()
	t := &tftpXfer{c: c, peer: peer.(*net.UDPAddr), blk: 512, timeout: 3 * time.Second}
	// options
	oack := map[string]string{}
	var keys []string
	for _, k := range r.optKeys {
		v := r.opts[k]
		switch k {
		case "blksize":
			n, err := strconv.Atoi(v)
			if err != nil || n < 8 {
				continue
			}
			if n > 65464 {
				n = 65464
			}
			t.blk = n
			oack[k], keys = strconv.Itoa(n), append(keys, k)
		case "timeout":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 255 {
				continue
			}
			t.timeout = time.Duration(n) * time.Second
			oack[k], keys = v, append(keys, k)
		case "tsize":
			oack[k], keys = v, append(keys, k)
		}
	}
	if put {
		size := int64(-1)
		if v, ok := oack["tsize"]; ok {
			size, _ = strconv.ParseInt(v, 10, 64)
		}
		s.receive(root, t, peer, r.file, name, size, oack, keys)
	} else {
		s.send(root, t, peer, r.file, name, oack, keys)
	}
}

type tftpXfer struct {
	c       *net.UDPConn
	peer    *net.UDPAddr
	blk     int
	timeout time.Duration
}

func oackPacket(oack map[string]string, keys []string) []byte {
	b := []byte{0, opOACK}
	for _, k := range keys {
		b = append(append(append(append(b, k...), 0), oack[k]...), 0)
	}
	return b
}

// exchange sends pkt and waits for a packet that ok accepts, sending again on
// timeouts. Packets from other ports get an error, as the RFC asks.
func (t *tftpXfer) exchange(pkt []byte, ok func([]byte) (bool, error)) ([]byte, error) {
	buf := make([]byte, t.blk+4+64)
	for try := 0; try < tftpRetries; try++ {
		if pkt != nil {
			if _, err := t.c.WriteToUDP(pkt, t.peer); err != nil {
				return nil, err
			}
		}
		deadline := time.Now().Add(t.timeout)
		for {
			t.c.SetReadDeadline(deadline)
			n, from, err := t.c.ReadFromUDP(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					break
				}
				return nil, err
			}
			if !from.IP.Equal(t.peer.IP) || from.Port != t.peer.Port {
				t.c.WriteToUDP(tftpError(tftpBadTID, "unknown transfer ID"), from)
				continue
			}
			if n >= 4 && binary.BigEndian.Uint16(buf) == opERROR {
				msg := strings.TrimRight(string(buf[4:n]), "\x00")
				if msg == "" {
					msg = "the device ended the transfer"
				}
				return nil, errors.New(msg)
			}
			if done, err := ok(buf[:n]); err != nil {
				return nil, err
			} else if done {
				return buf[:n], nil
			}
		}
	}
	return nil, errors.New("the device stopped answering")
}

func (s *tftpServer) send(root *os.Root, t *tftpXfer, peer net.Addr, asked, name string, oack map[string]string, keys []string) {
	f, err := root.Open(name)
	var fi os.FileInfo
	if err == nil {
		fi, err = f.Stat()
		if err == nil && !fi.Mode().IsRegular() {
			err = os.ErrNotExist
		}
	}
	if err != nil {
		if f != nil {
			f.Close()
		}
		why := "Not found"
		if !errors.Is(err, os.ErrNotExist) {
			why = "Refused - " + err.Error()
		}
		s.m.refuse("TFTP", peer, asked, false, why)
		t.c.WriteToUDP(tftpError(tftpNotFound, "file not found"), t.peer)
		return
	}
	defer f.Close()
	xf := s.m.begin("TFTP", peer, filepathSlash(name), false, fi.Size())
	if _, ok := oack["tsize"]; ok {
		oack["tsize"] = strconv.FormatInt(fi.Size(), 10)
	}
	if len(keys) > 0 {
		_, err := t.exchange(oackPacket(oack, keys), func(p []byte) (bool, error) {
			return len(p) >= 4 && binary.BigEndian.Uint16(p) == opACK && binary.BigEndian.Uint16(p[2:]) == 0, nil
		})
		if err != nil {
			s.m.finish(xf, "error", err)
			return
		}
	}
	data := make([]byte, 4+t.blk)
	binary.BigEndian.PutUint16(data, opDATA)
	var block uint16 = 1
	for {
		n, rerr := io.ReadFull(f, data[4:])
		if rerr != nil && rerr != io.ErrUnexpectedEOF && rerr != io.EOF {
			t.c.WriteToUDP(tftpError(0, "read error"), t.peer)
			s.m.finish(xf, "error", rerr)
			return
		}
		binary.BigEndian.PutUint16(data[2:], block)
		b := block
		_, err := t.exchange(data[:4+n], func(p []byte) (bool, error) {
			return len(p) >= 4 && binary.BigEndian.Uint16(p) == opACK && binary.BigEndian.Uint16(p[2:]) == b, nil
		})
		if err != nil {
			s.m.finish(xf, "error", err)
			return
		}
		s.m.progress(xf, int64(n))
		if n < t.blk {
			s.m.finish(xf, "done", nil)
			return
		}
		block++ // wraps to 0 after 65535, as most devices expect
	}
}

func (s *tftpServer) receive(root *os.Root, t *tftpXfer, peer net.Addr, asked, name string, size int64, oack map[string]string, keys []string) {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		s.m.refuse("TFTP", peer, asked, true, "Refused - "+err.Error())
		t.c.WriteToUDP(tftpError(tftpAccess, "cannot write the file"), t.peer)
		return
	}
	xf := s.m.begin("TFTP", peer, filepathSlash(name), true, size)
	fail := func(err error) {
		f.Close()
		root.Remove(name)
		s.m.finish(xf, "error", err)
	}
	var first []byte
	if len(keys) > 0 {
		first = oackPacket(oack, keys)
	} else {
		first = []byte{0, opACK, 0, 0}
	}
	ack := first
	var want uint16 = 1
	for {
		w := want
		p, err := t.exchange(ack, func(p []byte) (bool, error) {
			if len(p) < 4 || binary.BigEndian.Uint16(p) != opDATA {
				return false, nil
			}
			got := binary.BigEndian.Uint16(p[2:])
			if got == w {
				return true, nil
			}
			if got == w-1 { // our ACK was lost: say it again
				t.c.WriteToUDP([]byte{0, opACK, byte(got >> 8), byte(got)}, t.peer)
			}
			return false, nil
		})
		if err != nil {
			fail(err)
			return
		}
		n := len(p) - 4
		if n > t.blk {
			t.c.WriteToUDP(tftpError(tftpIllegal, "block too large"), t.peer)
			fail(errors.New("the device sent a block larger than agreed"))
			return
		}
		if _, err := f.Write(p[4:]); err != nil {
			t.c.WriteToUDP(tftpError(3, "disk full or write error"), t.peer)
			fail(err)
			return
		}
		s.m.progress(xf, int64(n))
		ack = []byte{0, opACK, byte(w >> 8), byte(w)}
		if n < t.blk {
			t.c.WriteToUDP(ack, t.peer)
			if err := f.Close(); err != nil {
				root.Remove(name)
				s.m.finish(xf, "error", err)
				return
			}
			s.m.finish(xf, "done", nil)
			return
		}
		want++
	}
}

func filepathSlash(p string) string { return strings.ReplaceAll(p, `\`, "/") }
