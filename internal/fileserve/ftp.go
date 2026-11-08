package fileserve

import (
	"bufio"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A small FTP server (RFC 959 with EPSV/EPRT, SIZE, MDTM, REST): one user
// name and password, one folder, passive and active data connections. Data
// connections must come from (or go to) the same address as the control
// connection, so the server cannot be used to reach other machines.

const (
	ftpMaxSessions = 16
	ftpIdle        = 5 * time.Minute
	ftpDataWait    = 15 * time.Second
)

type ftpServer struct {
	m     *Manager
	ln    net.Listener
	mu    sync.Mutex
	conns map[net.Conn]bool
}

func (s *ftpServer) addr() net.Addr { return s.ln.Addr() }

func (s *ftpServer) stop() {
	s.ln.Close()
	s.mu.Lock()
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
}

func (m *Manager) startFTP(addr string) (runner, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &ftpServer{m: m, ln: ln, conns: map[net.Conn]bool{}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return
			}
			s.mu.Lock()
			if len(s.conns) >= ftpMaxSessions {
				s.mu.Unlock()
				fmt.Fprint(c, "421 Too many connections\r\n")
				c.Close()
				continue
			}
			s.conns[c] = true
			s.mu.Unlock()
			go func() {
				(&ftpSession{s: s, c: c, r: bufio.NewReader(c), cwd: "/"}).run()
				s.mu.Lock()
				delete(s.conns, c)
				s.mu.Unlock()
				c.Close()
			}()
		}
	}()
	return s, nil
}

type ftpSession struct {
	s      *ftpServer
	c      net.Conn
	r      *bufio.Reader
	user   string
	authed bool
	fails  int
	cwd    string
	pasv   net.Listener
	port   string // active mode address
	rest   int64
}

func (f *ftpSession) reply(code int, msg string) {
	f.c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	fmt.Fprintf(f.c, "%d %s\r\n", code, msg)
}

func (f *ftpSession) peerIP() net.IP {
	if a, ok := f.c.RemoteAddr().(*net.TCPAddr); ok {
		return a.IP
	}
	return nil
}

func (f *ftpSession) closeData() {
	if f.pasv != nil {
		f.pasv.Close()
		f.pasv = nil
	}
	f.port = ""
}

func (f *ftpSession) run() {
	defer f.closeData()
	f.reply(220, "Termwright FTP server ready")
	for {
		f.c.SetReadDeadline(time.Now().Add(ftpIdle))
		line, err := f.r.ReadString('\n')
		if err != nil {
			return
		}
		if len(line) > 4096 {
			f.reply(500, "Line too long")
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd, arg, _ := strings.Cut(line, " ")
		cmd = strings.ToUpper(cmd)
		if !f.authed {
			switch cmd {
			case "USER", "PASS", "QUIT", "FEAT", "SYST", "AUTH", "OPTS", "NOOP":
			default:
				f.reply(530, "Please log in with USER and PASS")
				continue
			}
		}
		if !f.handle(cmd, arg) {
			return
		}
	}
}

// abs is the folder path for an argument (relative to the current directory).
func (f *ftpSession) abs(arg string) (string, error) {
	p := arg
	if !strings.HasPrefix(p, "/") {
		p = path.Join(f.cwd, p)
	}
	n, err := CleanName(p)
	if err != nil {
		return "", err
	}
	return n, nil
}

func (f *ftpSession) handle(cmd, arg string) bool {
	m := f.s.m
	switch cmd {
	case "CWD", "XCWD", "CDUP", "XCUP", "SIZE", "MDTM":
		if f.root() == nil {
			f.reply(451, "The server is stopping")
			return true
		}
	}
	switch cmd {
	case "USER":
		f.user, f.authed = arg, false
		f.reply(331, "Password required")
	case "PASS":
		c := m.config()
		okU := subtle.ConstantTimeCompare([]byte(f.user), []byte(c.FTPUser)) == 1
		okP := subtle.ConstantTimeCompare([]byte(arg), []byte(c.FTPPassword)) == 1
		if okU && okP && c.FTPUser != "" {
			f.authed = true
			f.reply(230, "Logged in")
			return true
		}
		f.fails++
		time.Sleep(time.Second)
		if f.fails >= 3 {
			m.refuse("FTP", f.c.RemoteAddr(), "(login as "+f.user+")", false, "Refused - wrong user name or password")
			f.reply(421, "Too many failed logins")
			return false
		}
		f.reply(530, "Wrong user name or password")
	case "QUIT":
		f.reply(221, "Bye")
		return false
	case "NOOP":
		f.reply(200, "OK")
	case "SYST":
		f.reply(215, "UNIX Type: L8")
	case "FEAT":
		f.c.SetWriteDeadline(time.Now().Add(30 * time.Second))
		fmt.Fprint(f.c, "211-Features:\r\n SIZE\r\n MDTM\r\n REST STREAM\r\n EPSV\r\n EPRT\r\n PASV\r\n UTF8\r\n211 End\r\n")
	case "OPTS":
		f.reply(200, "OK")
	case "AUTH":
		f.reply(502, "TLS is not offered by this server")
	case "TYPE", "MODE", "STRU", "ALLO":
		f.reply(200, "OK")
	case "PWD", "XPWD":
		f.reply(257, strconv.Quote(f.cwd)+" is the current directory")
	case "CWD", "XCWD", "CDUP", "XCUP":
		if cmd == "CDUP" || cmd == "XCUP" {
			arg = ".."
		}
		target := arg
		if !strings.HasPrefix(target, "/") {
			target = path.Join(f.cwd, target)
		}
		target = path.Clean("/" + target)
		n, err := CleanName(target)
		if err != nil {
			f.reply(550, "No such directory")
			return true
		}
		if n != "" {
			fi, err := f.root().Stat(n)
			if err != nil || !fi.IsDir() {
				f.reply(550, "No such directory")
				return true
			}
		}
		f.cwd = target
		f.reply(250, "Directory changed to "+f.cwd)
	case "PASV", "EPSV":
		f.closeData()
		ip := net.IP(nil)
		if a, ok := f.c.LocalAddr().(*net.TCPAddr); ok {
			ip = a.IP
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
		if err != nil {
			f.reply(425, "Cannot open a data connection")
			return true
		}
		f.pasv = ln
		port := ln.Addr().(*net.TCPAddr).Port
		if cmd == "EPSV" {
			f.reply(229, fmt.Sprintf("Entering Extended Passive Mode (|||%d|)", port))
		} else if ip4 := ip.To4(); ip4 != nil {
			f.reply(227, fmt.Sprintf("Entering Passive Mode (%d,%d,%d,%d,%d,%d)", ip4[0], ip4[1], ip4[2], ip4[3], port>>8, port&255))
		} else {
			f.closeData()
			f.reply(522, "Use EPSV over IPv6")
		}
	case "PORT", "EPRT":
		f.closeData()
		addr, err := parsePort(cmd, arg)
		if err != nil {
			f.reply(501, "Bad address")
			return true
		}
		h, _, _ := net.SplitHostPort(addr)
		if ip := net.ParseIP(h); ip == nil || !ip.Equal(f.peerIP()) {
			f.reply(504, "Data connections go only to your own address")
			return true
		}
		f.port = addr
		f.reply(200, "OK")
	case "REST":
		n, err := strconv.ParseInt(arg, 10, 64)
		if err != nil || n < 0 {
			f.reply(501, "Bad offset")
			return true
		}
		f.rest = n
		f.reply(350, "Restarting at "+arg)
	case "SIZE", "MDTM":
		n, err := f.abs(arg)
		var fi os.FileInfo
		if err == nil && n != "" {
			fi, err = f.root().Stat(n)
		}
		if err != nil || n == "" || !fi.Mode().IsRegular() {
			f.reply(550, "No such file")
			return true
		}
		if cmd == "SIZE" {
			f.reply(213, strconv.FormatInt(fi.Size(), 10))
		} else {
			f.reply(213, fi.ModTime().UTC().Format("20060102150405"))
		}
	case "LIST", "NLST", "MLSD":
		f.list(cmd, arg)
	case "RETR":
		f.retr(arg)
	case "STOR":
		f.stor(arg)
	case "ABOR":
		f.reply(226, "Nothing to abort")
	case "DELE", "RMD", "XRMD", "MKD", "XMKD", "RNFR", "RNTO", "APPE", "STOU", "SITE", "CHMOD":
		f.reply(550, "Not allowed on this server")
	default:
		f.reply(502, "Command not implemented")
	}
	return true
}

func (f *ftpSession) root() *os.Root { return f.s.m.folder() }

func parsePort(cmd, arg string) (string, error) {
	if cmd == "PORT" {
		p := strings.Split(arg, ",")
		if len(p) != 6 {
			return "", errors.New("bad PORT")
		}
		var n [6]int
		for i, s := range p {
			v, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil || v < 0 || v > 255 {
				return "", errors.New("bad PORT")
			}
			n[i] = v
		}
		return net.JoinHostPort(fmt.Sprintf("%d.%d.%d.%d", n[0], n[1], n[2], n[3]), strconv.Itoa(n[4]<<8|n[5])), nil
	}
	// EPRT |1|132.235.1.2|6275|
	if len(arg) < 2 {
		return "", errors.New("bad EPRT")
	}
	d := arg[:1]
	p := strings.Split(strings.Trim(arg, d), d)
	if len(p) != 3 {
		return "", errors.New("bad EPRT")
	}
	port, err := strconv.Atoi(p[2])
	if err != nil || port <= 0 || port > 65535 {
		return "", errors.New("bad EPRT")
	}
	return net.JoinHostPort(p[1], p[2]), nil
}

// data opens the data connection (after the 150 reply).
func (f *ftpSession) data() (net.Conn, error) {
	defer f.closeData()
	if f.pasv != nil {
		ln := f.pasv.(*net.TCPListener)
		deadline := time.Now().Add(ftpDataWait)
		for {
			ln.SetDeadline(deadline)
			c, err := ln.Accept()
			if err != nil {
				return nil, err
			}
			if a, ok := c.RemoteAddr().(*net.TCPAddr); ok && a.IP.Equal(f.peerIP()) {
				return c, nil
			}
			c.Close() // someone else: not this session's data
		}
	}
	if f.port != "" {
		return net.DialTimeout("tcp", f.port, ftpDataWait)
	}
	return nil, errors.New("use PASV or PORT first")
}

func (f *ftpSession) list(cmd, arg string) {
	fields := strings.Fields(arg)
	target := ""
	for _, x := range fields {
		if !strings.HasPrefix(x, "-") {
			target = x
		}
	}
	n, err := f.abs(target)
	if err != nil {
		f.reply(550, "No such directory")
		return
	}
	var entries []os.FileInfo
	root := f.root()
	if root == nil {
		f.reply(451, "The server is stopping")
		return
	}
	dir := n
	if dir == "" {
		dir = "."
	}
	fi, err := root.Stat(dir)
	if err != nil {
		f.reply(550, "No such file or directory")
		return
	}
	if fi.IsDir() {
		d, err := root.Open(dir)
		if err != nil {
			f.reply(550, "Cannot read the directory")
			return
		}
		des, _ := d.ReadDir(-1)
		d.Close()
		for _, de := range des {
			if strings.HasPrefix(de.Name(), ".") {
				continue
			}
			if i, err := de.Info(); err == nil {
				entries = append(entries, i)
			}
		}
	} else {
		entries = append(entries, fi)
	}
	f.reply(150, "Here comes the listing")
	c, err := f.data()
	if err != nil {
		f.reply(425, "Cannot open the data connection")
		return
	}
	w := bufio.NewWriter(c)
	now := time.Now()
	for _, e := range entries {
		switch cmd {
		case "NLST":
			fmt.Fprintf(w, "%s\r\n", e.Name())
		case "MLSD":
			t := "file"
			if e.IsDir() {
				t = "dir"
			}
			fmt.Fprintf(w, "type=%s;size=%d;modify=%s; %s\r\n", t, e.Size(), e.ModTime().UTC().Format("20060102150405"), e.Name())
		default:
			perm, when := "-rw-r--r--", e.ModTime().Format("Jan _2 15:04")
			if e.IsDir() {
				perm = "drwxr-xr-x"
			}
			if now.Sub(e.ModTime()) > 180*24*time.Hour || e.ModTime().After(now) {
				when = e.ModTime().Format("Jan _2  2006")
			}
			fmt.Fprintf(w, "%s 1 ftp ftp %12d %s %s\r\n", perm, e.Size(), when, e.Name())
		}
	}
	w.Flush()
	c.Close()
	f.reply(226, "Done")
}

type countWriter struct {
	w  io.Writer
	fn func(int64)
}

func (c countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.fn(int64(n))
	return n, err
}

func (f *ftpSession) retr(arg string) {
	m := f.s.m
	rest := f.rest
	f.rest = 0
	n, err := f.abs(arg)
	if err != nil || n == "" {
		m.refuse("FTP", f.c.RemoteAddr(), arg, false, "Refused - outside the folder")
		f.reply(550, "Not allowed")
		return
	}
	root := f.root()
	if root == nil {
		f.reply(451, "The server is stopping")
		return
	}
	file, err := root.Open(n)
	var fi os.FileInfo
	if err == nil {
		if fi, err = file.Stat(); err == nil && !fi.Mode().IsRegular() {
			err = os.ErrNotExist
		}
	}
	if err != nil {
		if file != nil {
			file.Close()
		}
		m.refuse("FTP", f.c.RemoteAddr(), filepathSlash(n), false, "Not found")
		f.reply(550, "No such file")
		return
	}
	defer file.Close()
	if rest > 0 {
		if _, err := file.Seek(rest, io.SeekStart); err != nil {
			f.reply(554, "Cannot restart there")
			return
		}
	}
	f.reply(150, fmt.Sprintf("Opening data connection for %s (%d bytes)", path.Base(filepathSlash(n)), fi.Size()-rest))
	c, err := f.data()
	if err != nil {
		f.reply(425, "Cannot open the data connection")
		return
	}
	xf := m.begin("FTP", f.c.RemoteAddr(), filepathSlash(n), false, fi.Size()-rest)
	_, err = io.Copy(countWriter{c, func(k int64) { m.progress(xf, k) }}, file)
	cerr := c.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		m.finish(xf, "error", err)
		f.reply(426, "Transfer aborted")
		return
	}
	m.finish(xf, "done", nil)
	f.reply(226, "Transfer complete")
}

func (f *ftpSession) stor(arg string) {
	m := f.s.m
	f.rest = 0
	n, err := f.abs(arg)
	if err != nil || n == "" {
		m.refuse("FTP", f.c.RemoteAddr(), arg, true, "Refused - outside the folder")
		f.reply(553, "Not allowed")
		return
	}
	if !m.canWrite(FTP) {
		m.refuse("FTP", f.c.RemoteAddr(), filepathSlash(n), true, "Refused - uploads are turned off")
		f.reply(553, "Uploads are turned off")
		return
	}
	root := f.root()
	if root == nil {
		f.reply(451, "The server is stopping")
		return
	}
	file, err := root.OpenFile(n, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		m.refuse("FTP", f.c.RemoteAddr(), filepathSlash(n), true, "Refused - "+err.Error())
		f.reply(553, "Cannot write the file")
		return
	}
	f.reply(150, "Ready to receive")
	c, err := f.data()
	if err != nil {
		file.Close()
		root.Remove(n)
		f.reply(425, "Cannot open the data connection")
		return
	}
	xf := m.begin("FTP", f.c.RemoteAddr(), filepathSlash(n), true, -1)
	_, err = io.Copy(countWriter{file, func(k int64) { m.progress(xf, k) }}, c)
	c.Close()
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		root.Remove(n)
		m.finish(xf, "error", err)
		f.reply(426, "Transfer aborted")
		return
	}
	m.finish(xf, "done", nil)
	f.reply(226, "Transfer complete")
}
