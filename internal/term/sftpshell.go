package term

import (
	"bytes"
	"context"
	"encoding/base64"
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

	"github.com/pkg/sftp"

	"github.com/termwright/termwright/internal/session"
)

// Files through a shell: a switch's SFTP shows only its configuration folder, while its Linux shell ("!sh")
// sees everything. The Files panel's "Switch Shell" view logs in once more in the background, starts that
// shell and answers SFTP requests with shell commands (stat, base64, mkdir, rm, mv) - served to the usual
// SFTP client over an in-memory pipe, so listing, the viewer, downloads and small uploads work as for SFTP.

// shellMaxFile: the largest file read or written through the shell (it goes as base64 text over the terminal).
const shellMaxFile = 4 << 20

type shellFS struct {
	mu   sync.Mutex
	c    *CaptureConn
	exec func(string) (string, error) // tests: instead of c
}

func shq(p string) string { return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'" }

func (f *shellFS) run(cmd string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.exec != nil {
		return f.exec(cmd)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := f.c.Run(ctx, cmd)
	return strings.ReplaceAll(out, "\r", ""), err
}

// shellFile is a file as the shell's stat (or ls -l) describes it.
type shellFile struct {
	name string
	size int64
	mode os.FileMode
	mod  time.Time
}

func (s shellFile) Name() string       { return s.name }
func (s shellFile) Size() int64        { return s.size }
func (s shellFile) Mode() os.FileMode  { return s.mode }
func (s shellFile) ModTime() time.Time { return s.mod }
func (s shellFile) IsDir() bool        { return s.mode.IsDir() }
func (s shellFile) Sys() any           { return nil }

// modeOf turns a raw st_mode (hex, from stat's %f) into an os.FileMode.
func modeOf(raw uint32) os.FileMode {
	m := os.FileMode(raw & 0o777)
	switch raw & 0o170000 {
	case 0o040000:
		m |= os.ModeDir
	case 0o120000:
		m |= os.ModeSymlink
	case 0o010000:
		m |= os.ModeNamedPipe
	case 0o140000:
		m |= os.ModeSocket
	case 0o020000:
		m |= os.ModeDevice | os.ModeCharDevice
	case 0o060000:
		m |= os.ModeDevice
	}
	return m
}

// parseStat reads "mode-hex|size|mtime|name" lines.
func parseStat(out string) []shellFile {
	var l []shellFile
	for _, ln := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimSpace(ln), "|", 4)
		if len(f) != 4 {
			continue
		}
		raw, e1 := strconv.ParseUint(f[0], 16, 32)
		size, e2 := strconv.ParseInt(f[1], 10, 64)
		sec, e3 := strconv.ParseInt(f[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		name := path.Base(f[3])
		if name == "." || name == ".." || name == "*" || name == ".*" {
			continue
		}
		l = append(l, shellFile{name: name, size: size, mode: modeOf(uint32(raw)), mod: time.Unix(sec, 0)})
	}
	return l
}

// parseLs reads "ls -la" lines (a shell without stat -c): "drwxr-xr-x  2 root root  4096 Nov 25 10:00 name".
func parseLs(out string) []shellFile {
	var l []shellFile
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 9 || len(f[0]) < 10 {
			continue
		}
		size, err := strconv.ParseInt(f[4], 10, 64)
		if err != nil {
			continue
		}
		name := strings.Join(f[8:], " ")
		if i := strings.Index(name, " -> "); i >= 0 && f[0][0] == 'l' {
			name = name[:i]
		}
		if name == "." || name == ".." {
			continue
		}
		var m os.FileMode
		for i, c := range f[0][1:10] {
			if c != '-' {
				m |= 1 << uint(8-i)
			}
		}
		switch f[0][0] {
		case 'd':
			m |= os.ModeDir
		case 'l':
			m |= os.ModeSymlink
		}
		t, err := time.Parse("Jan 2 15:04", f[5]+" "+f[6]+" "+f[7])
		if err == nil {
			t = t.AddDate(time.Now().Year(), 0, 0)
		} else if t, err = time.Parse("Jan 2 2006", f[5]+" "+f[6]+" "+f[7]); err != nil {
			t = time.Time{}
		}
		l = append(l, shellFile{name: name, size: size, mode: m, mod: t})
	}
	return l
}

func (f *shellFS) list(dir string) ([]shellFile, error) {
	out, err := f.run("cd " + shq(dir) + " 2>&1 && stat -c '%f|%s|%Y|%n' -- * .* 2>/dev/null; echo TW_RC=$?")
	if err != nil {
		return nil, err
	}
	if strings.Contains(out, "can't cd") || strings.Contains(out, "No such file") || strings.Contains(out, "Permission denied") || strings.Contains(out, "Not a directory") {
		return nil, shellErr(out)
	}
	l := parseStat(out)
	if len(l) == 0 && !strings.Contains(out, "|") { // no stat -c here (or an empty folder): ls -la says which
		if lo, err := f.run("ls -la " + shq(dir) + " 2>&1"); err == nil {
			l = parseLs(lo)
		}
	}
	return l, nil
}

func (f *shellFS) stat(p string) (shellFile, error) {
	out, err := f.run("stat -c '%f|%s|%Y|%n' -- " + shq(p) + " 2>&1")
	if err != nil {
		return shellFile{}, err
	}
	if l := parseStat(out); len(l) == 1 {
		l[0].name = path.Base(p)
		return l[0], nil
	}
	if p == "/" {
		return shellFile{name: "/", mode: os.ModeDir | 0o755}, nil
	}
	// no stat -c: the folder's listing names it
	l, err := f.list(path.Dir(p))
	if err != nil {
		return shellFile{}, err
	}
	for _, x := range l {
		if x.name == path.Base(p) {
			return x, nil
		}
	}
	return shellFile{}, os.ErrNotExist
}

// shellErr turns a shell message into an SFTP error the client understands.
func shellErr(out string) error {
	switch {
	case strings.Contains(out, "No such file") || strings.Contains(out, "can't cd"):
		return os.ErrNotExist
	case strings.Contains(out, "Permission denied") || strings.Contains(out, "Read-only"):
		return os.ErrPermission
	}
	return errors.New(strings.TrimSpace(out))
}

// ---- the SFTP request handlers ----

type listerAt []os.FileInfo

func (l listerAt) ListAt(out []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(out, l[off:])
	if n < len(out) {
		return n, io.EOF
	}
	return n, nil
}

func (f *shellFS) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		l, err := f.list(r.Filepath)
		if err != nil {
			return nil, err
		}
		out := make([]os.FileInfo, len(l))
		for i := range l {
			out[i] = l[i]
		}
		return listerAt(out), nil
	case "Stat", "Lstat":
		s, err := f.stat(r.Filepath)
		if err != nil {
			return nil, err
		}
		return listerAt{s}, nil
	case "Readlink":
		out, err := f.run("readlink " + shq(r.Filepath))
		if err != nil {
			return nil, err
		}
		return listerAt{shellFile{name: strings.TrimSpace(out)}}, nil
	}
	return nil, sftp.ErrSSHFxOpUnsupported
}

func (f *shellFS) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	s, err := f.stat(r.Filepath)
	if err != nil {
		return nil, err
	}
	if s.IsDir() {
		return nil, errors.New("a folder")
	}
	if s.size > shellMaxFile {
		return nil, fmt.Errorf("%s is %d MB: through the switch shell files up to %d MB are read", path.Base(r.Filepath), s.size>>20, shellMaxFile>>20)
	}
	out, err := f.run("base64 " + shq(r.Filepath) + " 2>&1; echo TW_RC=$?")
	if err != nil {
		return nil, err
	}
	body, rc, _ := strings.Cut(out, "TW_RC=")
	if strings.TrimSpace(rc) != "0" {
		if strings.Contains(body, "not found") { // no base64 applet: the text as it is
			txt, err := f.run("cat " + shq(r.Filepath))
			if err != nil {
				return nil, err
			}
			return bytes.NewReader([]byte(txt)), nil
		}
		return nil, shellErr(body)
	}
	b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(body), ""))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %v", path.Base(r.Filepath), err)
	}
	return bytes.NewReader(b), nil
}

// shellWriter collects an upload and writes it with base64 -d when the client closes the file.
type shellWriter struct {
	f    *shellFS
	p    string
	mu   sync.Mutex
	data []byte
	err  error
}

func (w *shellWriter) WriteAt(b []byte, off int64) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	end := off + int64(len(b))
	if end > shellMaxFile {
		w.err = fmt.Errorf("through the switch shell files up to %d MB are written", shellMaxFile>>20)
		return 0, w.err
	}
	if int64(len(w.data)) < end {
		w.data = append(w.data, make([]byte, end-int64(len(w.data)))...)
	}
	copy(w.data[off:], b)
	return len(b), nil
}

func (w *shellWriter) Close() error {
	if w.err != nil {
		return w.err
	}
	tmp := w.p + ".tw-upload"
	enc := base64.StdEncoding.EncodeToString(w.data)
	if _, err := w.f.run(": > " + shq(tmp)); err != nil {
		return err
	}
	for i := 0; i < len(enc); i += 3000 { // lines short enough for any terminal
		j := min(i+3000, len(enc))
		if _, err := w.f.run("echo " + enc[i:j] + " >> " + shq(tmp)); err != nil {
			return err
		}
	}
	out, err := w.f.run("base64 -d " + shq(tmp) + " > " + shq(w.p) + " 2>&1; rc=$?; rm -f " + shq(tmp) + "; echo TW_RC=$rc")
	if err != nil {
		return err
	}
	if body, rc, _ := strings.Cut(out, "TW_RC="); strings.TrimSpace(rc) != "0" {
		return shellErr(body)
	}
	return nil
}

func (f *shellFS) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return &shellWriter{f: f, p: r.Filepath}, nil
}

func (f *shellFS) Filecmd(r *sftp.Request) error {
	var cmd string
	switch r.Method {
	case "Setstat":
		return nil
	case "Mkdir":
		cmd = "mkdir " + shq(r.Filepath)
	case "Rmdir":
		cmd = "rmdir " + shq(r.Filepath)
	case "Remove":
		cmd = "rm -f " + shq(r.Filepath)
	case "Rename", "PosixRename":
		cmd = "mv -f " + shq(r.Filepath) + " " + shq(r.Target)
	case "Symlink":
		cmd = "ln -s " + shq(r.Filepath) + " " + shq(r.Target)
	default:
		return sftp.ErrSSHFxOpUnsupported
	}
	out, err := f.run(cmd + " 2>&1; echo TW_RC=$?")
	if err != nil {
		return err
	}
	if body, rc, _ := strings.Cut(out, "TW_RC="); strings.TrimSpace(rc) != "0" {
		return shellErr(body)
	}
	return nil
}

// ShellCommands are the commands that start a switch's Linux shell, tried in order.
var ShellCommands = []string{"!sh"}

// sftpOpenShell opens the "Switch Shell" view: a background login to the tab's session, its Linux shell, and
// an SFTP client served from it. The folder to start in is the shell's.
func (m *Manager) sftpOpenShell(tabID string, s session.Session) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := m.OpenCapture(ctx, s, "")
	if err != nil {
		return "", "", err
	}
	fs := &shellFS{c: c}
	ok := false
	for _, sh := range ShellCommands {
		if _, err := fs.run(sh); err != nil {
			continue
		}
		// a shell does arithmetic; the switch's own command line does not
		if out, err := fs.run("echo TW$((40+2))"); err == nil && strings.Contains(out, "TW42") {
			ok = true
			break
		}
	}
	if !ok {
		c.Close()
		return "", "", errors.New("the device's Linux shell (!sh) could not be started in a second session - on Switch Engine it may need debug mode for the whole switch")
	}
	_, _ = fs.run("export PS1='# ' LANG=C 2>/dev/null; unset HISTFILE")
	home := "/"
	if out, err := fs.run("pwd"); err == nil && strings.HasPrefix(strings.TrimSpace(out), "/") {
		home = strings.TrimSpace(out)
	}
	cl, stop, err := fs.serve(home)
	if err != nil {
		c.Close()
		return "", "", err
	}
	ss := &sftpSess{id: newID(), tabID: tabID, c: cl, stop: func() { stop(); c.Close() }}
	m.mu.Lock()
	m.sftps[ss.id] = ss
	m.mu.Unlock()
	return ss.id, home, nil
}

// serve answers an SFTP client from the shell, over an in-memory pipe.
func (f *shellFS) serve(home string) (*sftp.Client, func(), error) {
	a, b := net.Pipe()
	srv := sftp.NewRequestServer(a, sftp.Handlers{FileGet: f, FilePut: f, FileCmd: f, FileList: f}, sftp.WithStartDirectory(home))
	go func() { _ = srv.Serve(); _ = a.Close() }()
	cl, err := sftp.NewClientPipe(b, b)
	if err != nil {
		srv.Close()
		a.Close()
		b.Close()
		return nil, nil, err
	}
	return cl, func() { _ = srv.Close(); _ = a.Close(); _ = b.Close() }, nil
}
