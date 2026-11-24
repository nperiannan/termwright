package term

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pkg/sftp"

	"github.com/termwright/termwright/internal/session"
)

// localSFTP serves this computer's files to an SFTP client over an in-memory pipe (no network port): the
// Files panel of a Local Shell tab then works as for SSH tabs - list, open, edit, rename, transfers. On
// Windows the top folder lists the drives (/C:, /D:).
func localSFTP() (*sftp.Client, func(), error) {
	a, b := net.Pipe()
	srv, err := sftp.NewServer(a, sftp.WindowsRootEnumeratesDrives())
	if err != nil {
		a.Close()
		b.Close()
		return nil, nil, err
	}
	go func() { _ = srv.Serve(); _ = a.Close() }()
	c, err := sftp.NewClientPipe(b, b)
	if err != nil {
		srv.Close()
		a.Close()
		b.Close()
		return nil, nil, err
	}
	return c, func() { _ = srv.Close(); _ = a.Close(); _ = b.Close() }, nil
}

// LocalSFTPPath is a path of this computer as the local SFTP server names it ("/C:/Users/x" on Windows).
func LocalSFTPPath(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// LocalPathOf is the other way: a path of the local SFTP server as this computer names it.
func LocalPathOf(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	return filepath.FromSlash(strings.TrimPrefix(p, "/"))
}

// sftpOpenLocal opens the Files panel of a Local Shell tab: this computer's files, starting in its home.
func (m *Manager) sftpOpenLocal(tabID string, s session.Session) (string, string, error) {
	c, stop, err := localSFTP()
	if err != nil {
		return "", "", err
	}
	home, _ := os.UserHomeDir()
	if s.LocalDir != "" {
		home = expandHome(s.LocalDir)
	}
	ss := &sftpSess{id: newID(), tabID: tabID, c: c, stop: stop, local: true}
	m.mu.Lock()
	m.sftps[ss.id] = ss
	m.mu.Unlock()
	return ss.id, LocalSFTPPath(home), nil
}

// SFTPIsLocal says whether an SFTP session is this computer's files (a Local Shell tab's panel).
func (m *Manager) SFTPIsLocal(id string) bool {
	s, err := m.sftpGet(id)
	return err == nil && s.local
}

// localShellName: the shell of Local Shell tabs ("zsh", "bash", "powershell").
func localShellName(s session.Session) string {
	if strings.TrimSpace(s.LocalCommand) != "" {
		f := strings.Fields(s.LocalCommand)
		return strings.TrimSuffix(strings.ToLower(filepath.Base(f[0])), ".exe")
	}
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		return "zsh"
	}
	return filepath.Base(sh)
}

// SFTPStat: a file's size and modification time (Unix milliseconds) - the viewer checks it before saving.
func (m *Manager) SFTPStat(id, p string) (int64, int64, error) {
	s, err := m.sftpGet(id)
	if err != nil {
		return 0, 0, err
	}
	fi, err := s.c.Stat(p)
	if err != nil {
		return 0, 0, err
	}
	return fi.Size(), fi.ModTime().UnixMilli(), nil
}

// SFTPReadTail reads the last max bytes of a file (a log the viewer follows) and its size; it starts at a
// line when it cut the beginning.
func (m *Manager) SFTPReadTail(id, p string, max int64) (string, int64, error) {
	s, err := m.sftpGet(id)
	if err != nil {
		return "", 0, err
	}
	f, err := s.c.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	size, from := fi.Size(), int64(0)
	if size > max {
		from = size - max
	}
	if _, err := f.Seek(from, 0); err != nil {
		return "", 0, err
	}
	b := make([]byte, size-from)
	n, err := io.ReadFull(f, b)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", 0, err
	}
	b = b[:n]
	if from > 0 {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	return string(b), size, nil
}

// SFTPSaveText writes a text file the viewer edited, keeping its permissions. expect: the modification
// time it had when it was opened (0: do not check) - a file changed since then is not overwritten.
// It returns the new modification time.
func (m *Manager) SFTPSaveText(id, p, text string, expect int64) (int64, error) {
	s, err := m.sftpGet(id)
	if err != nil {
		return 0, err
	}
	if fi, err := s.c.Stat(p); err == nil && expect > 0 && fi.ModTime().UnixMilli() != expect {
		return 0, errors.New("the file changed since you opened it (" + fi.ModTime().Format("15:04:05") + "): reload it, or copy your text first")
	}
	f, err := s.c.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return 0, err
	}
	if _, err := f.Write([]byte(text)); err != nil {
		_ = f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	fi, err := s.c.Stat(p)
	if err != nil {
		return 0, nil
	}
	return fi.ModTime().UnixMilli(), nil
}
