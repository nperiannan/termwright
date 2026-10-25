package term

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/termwright/termwright/internal/session"
)

// FileEntry is a local or remote directory entry.
type FileEntry struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"modTime"`
	IsDir   bool      `json:"isDir"`
	IsLink  bool      `json:"isLink"`
}

type sftpSess struct {
	id     string
	tabID  string
	c      *sftp.Client
	client *ssh.Client // own connection (standalone SFTP session), nil when sharing a tab's
	clean  func()
	hop    *Hop   // set when the files are on the remote command's target, reached through the connection
	stop   func() // closes the exec channel that carries a hop's sftp
	root   bool   // browsing as root through sudo
	// sudoPass is the sudo password typed for this tab (memory only)
	sudoPass string
}

// SFTP modes: "" follows the session (Session.SFTPToTarget), "jump" is the
// host Termwright logs in to, "target" is the host of an "ssh … host" remote command.
func sftpHop(s session.Session, mode string) (*Hop, error) {
	h, ok := HopOf(s)
	switch {
	case mode == "target" && !ok:
		return nil, errors.New(`SFTP to the target needs a remote command like "ssh -i key user@host" (Session Options → Logon Actions)`)
	case mode == "target", mode == "" && ok && s.SFTPToTarget:
		return &h, nil
	}
	return nil, nil
}

// startSFTP starts the sftp subsystem on client, or on the hop target through it.
func startSFTP(client *ssh.Client, hop *Hop) (*sftp.Client, func(), error) {
	if hop != nil {
		return sftpThroughHop(client, *hop)
	}
	c, err := sftp.NewClient(client, sftp.UseConcurrentWrites(true), sftp.UseConcurrentReads(true))
	if err != nil {
		return nil, nil, fmt.Errorf("start SFTP: %w", err)
	}
	return c, func() {}, nil
}

// sshClientOf returns the SSH client of a connected SSH2 tab.
func (m *Manager) sshClientOf(tabID string) (*ssh.Client, error) {
	t, err := m.get(tabID)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	sc, ok := t.conn.(*sshConn)
	if !ok || sc == nil {
		return nil, errors.New("SFTP needs a connected SSH2 tab")
	}
	return sc.client, nil
}

// SFTPConnect opens a standalone SFTP session: its own SSH login that only
// starts the sftp subsystem (no shell). Use it for devices that allow just
// one session per connection, or to browse files without a terminal.
// It returns the session id, the remote home folder and a connection summary.
func (m *Manager) SFTPConnect(s session.Session, mode string) (string, string, string, error) {
	hop, err := sftpHop(s, mode)
	if err != nil {
		return "", "", "", err
	}
	id := newID()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // includes password prompts
	defer cancel()
	client, info, cleanup, err := m.sshClient(ctx, &Tab{id: id, sess: s}, s, 0)
	if err != nil {
		return "", "", "", err
	}
	c, stop, err := startSFTP(client, hop)
	if err != nil {
		_ = client.Close()
		cleanup()
		return "", "", "", err
	}
	home, err := c.Getwd()
	if err != nil || home == "" {
		home = "/"
	}
	ss := &sftpSess{id: id, c: c, client: client, clean: cleanup, hop: hop, stop: stop}
	if hop != nil {
		info += " → " + hop.Target
	}
	m.mu.Lock()
	m.sftps[id] = ss
	m.mu.Unlock()
	go func() {
		_ = client.Wait() // server closed or network lost
		m.mu.Lock()
		_, open := m.sftps[id]
		m.mu.Unlock()
		if open {
			m.SFTPClose(id)
			m.emit.Emit("sftp:closed", id)
		}
	}()
	return id, home, strings.Replace(info, "SSH2 ", "SFTP ", 1), nil
}

// SFTPOpen starts an SFTP session over an existing SSH2 tab connection
// (no second login). It returns the session id and the remote home folder.
func (m *Manager) SFTPOpen(tabID, mode string) (string, string, error) {
	client, err := m.sshClientOf(tabID)
	if err != nil {
		return "", "", err
	}
	t, _ := m.get(tabID)
	t.mu.Lock()
	sess := t.sess
	t.mu.Unlock()
	hop, err := sftpHop(sess, mode)
	if err != nil {
		return "", "", err
	}
	c, stop, err := startSFTP(client, hop)
	if err != nil {
		return "", "", err
	}
	home, err := c.Getwd()
	if err != nil || home == "" {
		home = "/"
	}
	s := &sftpSess{id: newID(), tabID: tabID, c: c, hop: hop, stop: stop}
	m.mu.Lock()
	m.sftps[s.id] = s
	m.mu.Unlock()
	return s.id, home, nil
}

// SFTPTarget returns the hop target an SFTP session browses, or "" for the host logged in to.
func (m *Manager) SFTPTarget(id string) string {
	if s, err := m.sftpGet(id); err == nil && s.hop != nil {
		return s.hop.Target
	}
	return ""
}

func (m *Manager) sftpGet(id string) (*sftpSess, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sftps[id]
	if !ok {
		return nil, errors.New("SFTP session is closed")
	}
	return s, nil
}

// SFTPClose ends an SFTP session.
func (m *Manager) SFTPClose(id string) {
	m.mu.Lock()
	s := m.sftps[id]
	delete(m.sftps, id)
	m.mu.Unlock()
	if s != nil {
		_ = s.c.Close()
		if s.stop != nil {
			s.stop()
		}
		if s.client != nil {
			_ = s.client.Close()
			s.clean()
		}
	}
}

// closeTabExtras stops SFTP sessions, transfers and scripts of a tab.
func (m *Manager) closeTabExtras(tabID string) {
	m.mu.Lock()
	var ids []string
	for id, s := range m.sftps {
		if s.tabID == tabID {
			ids = append(ids, id)
		}
	}
	var runs []*scriptRun
	for _, r := range m.scripts {
		if r.tabID == tabID {
			runs = append(runs, r)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.SFTPClose(id)
		m.emit.Emit("sftp:closed", id)
	}
	for _, r := range runs {
		r.cancel()
	}
}

func toEntries(infos []fs.FileInfo) []FileEntry {
	out := make([]FileEntry, 0, len(infos))
	for _, fi := range infos {
		out = append(out, FileEntry{
			Name: fi.Name(), Size: fi.Size(), Mode: fi.Mode().String(), ModTime: fi.ModTime(),
			IsDir: fi.IsDir(), IsLink: fi.Mode()&fs.ModeSymlink != 0,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// SFTPList lists a remote directory. Symlinks to directories are reported as directories.
func (m *Manager) SFTPList(id, dir string) ([]FileEntry, error) {
	s, err := m.sftpGet(id)
	if err != nil {
		return nil, err
	}
	infos, err := s.c.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := toEntries(infos)
	for i := range out {
		if out[i].IsLink {
			if st, err := s.c.Stat(path.Join(dir, out[i].Name)); err == nil && st.IsDir() {
				out[i].IsDir = true
			}
		}
	}
	return out, nil
}

// SFTPRealPath resolves a remote path (e.g. "..").
func (m *Manager) SFTPRealPath(id, p string) (string, error) {
	s, err := m.sftpGet(id)
	if err != nil {
		return "", err
	}
	return s.c.RealPath(p)
}

// SFTPMkdir creates a remote directory.
func (m *Manager) SFTPMkdir(id, p string) error {
	s, err := m.sftpGet(id)
	if err != nil {
		return err
	}
	return s.c.MkdirAll(p)
}

// SFTPRename renames or moves a remote file.
func (m *Manager) SFTPRename(id, from, to string) error {
	s, err := m.sftpGet(id)
	if err != nil {
		return err
	}
	return s.c.Rename(from, to)
}

// SFTPRemove deletes a remote file or directory tree.
func (m *Manager) SFTPRemove(id, p string) error {
	s, err := m.sftpGet(id)
	if err != nil {
		return err
	}
	st, err := s.c.Lstat(p)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return s.c.Remove(p)
	}
	return s.c.RemoveAll(p)
}

// LocalList lists a local directory.
func LocalList(dir string) ([]FileEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	infos := make([]fs.FileInfo, 0, len(entries))
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && st.IsDir() {
				infos = append(infos, dirLink{fi})
				continue
			}
		}
		infos = append(infos, fi)
	}
	return toEntries(infos), nil
}

type dirLink struct{ fs.FileInfo }

func (d dirLink) IsDir() bool { return true }

// Transfer progress reported to the UI as "xfer:progress".
type Transfer struct {
	ID         string `json:"id"`
	Direction  string `json:"direction"` // up | down | xmodem
	File       string `json:"file"`
	Bytes      int64  `json:"bytes"`
	Total      int64  `json:"total"`
	FilesDone  int    `json:"filesDone"`
	FilesTotal int    `json:"filesTotal"`
	State      string `json:"state"` // running | done | error | cancelled
	Error      string `json:"error"`
	Resumable  bool   `json:"resumable"` // SFTP copy that can be resumed after an error/cancel
	resume     bool
	rest       []byte // terminal output read by a ZMODEM receiver after the session ended
}

// xferJob remembers an SFTP copy so it can be resumed.
type xferJob struct {
	sftpID, direction, dest string
	sources                 []string
}

type xferItem struct {
	src, dst string
	size     int64
	dir      bool
}

// StartTransfer copies files/folders between this computer and the server.
// direction "up": sources are local paths, destDir is remote.
// direction "down": sources are remote paths, destDir is local.
func (m *Manager) StartTransfer(sftpID, direction string, sources []string, destDir string) (string, error) {
	return m.startCopy(xferJob{sftpID: sftpID, direction: direction, sources: sources, dest: destDir}, false)
}

func (m *Manager) startCopy(job xferJob, resume bool) (string, error) {
	return m.startXfer(job.sftpID, job.direction, func(ctx context.Context, c *sftp.Client, tr *Transfer) error {
		tr.Resumable, tr.resume = true, resume
		m.mu.Lock()
		m.jobs[tr.ID] = job
		m.mu.Unlock()
		return m.runTransfer(ctx, c, tr, job.sources, job.dest)
	})
}

// ResumeTransfer restarts an interrupted SFTP copy: partially copied files
// are continued from where they stopped and complete files are skipped.
func (m *Manager) ResumeTransfer(id string) (string, error) {
	m.mu.Lock()
	job, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return "", errors.New("this transfer can no longer be resumed")
	}
	return m.startCopy(job, true)
}

// StartSync copies the contents of localDir/remoteDir in one direction,
// skipping files whose destination copy has the same size and is not older
// ("up": local -> remote, "down": remote -> local). Nothing is deleted.
func (m *Manager) StartSync(sftpID, direction, localDir, remoteDir string) (string, error) {
	return m.startXfer(sftpID, direction, func(ctx context.Context, c *sftp.Client, tr *Transfer) error {
		var items []xferItem
		if direction == "up" {
			err := filepath.WalkDir(localDir, func(p string, d fs.DirEntry, err error) error {
				if err != nil || p == localDir {
					return err
				}
				rel, _ := filepath.Rel(localDir, p)
				it := xferItem{src: p, dst: path.Join(remoteDir, filepath.ToSlash(rel)), dir: d.IsDir()}
				if !d.IsDir() {
					fi, err := d.Info()
					if err != nil {
						return nil
					}
					it.size = fi.Size()
					if rs, err := c.Stat(it.dst); err == nil && rs.Size() == fi.Size() && !rs.ModTime().Before(fi.ModTime().Truncate(time.Second)) {
						return nil
					}
				}
				items = append(items, it)
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			w := c.Walk(remoteDir)
			for w.Step() {
				if w.Err() != nil {
					return w.Err()
				}
				if w.Path() == remoteDir {
					continue
				}
				rel := strings.TrimPrefix(strings.TrimPrefix(w.Path(), remoteDir), "/")
				st := w.Stat()
				it := xferItem{src: w.Path(), dst: filepath.Join(localDir, filepath.FromSlash(rel)), dir: st.IsDir(), size: st.Size()}
				if !st.IsDir() {
					if ls, err := os.Stat(it.dst); err == nil && ls.Size() == st.Size() && !ls.ModTime().Before(st.ModTime()) {
						continue
					}
				}
				items = append(items, it)
			}
		}
		return m.copyItems(ctx, c, tr, items, true)
	})
}

func (m *Manager) startXfer(sftpID, direction string, run func(ctx context.Context, c *sftp.Client, tr *Transfer) error) (string, error) {
	s, err := m.sftpGet(sftpID)
	if err != nil {
		return "", err
	}
	if direction != "up" && direction != "down" {
		return "", errors.New("direction must be up or down")
	}
	ctx, cancel := context.WithCancel(context.Background())
	tr := &Transfer{ID: newID(), Direction: direction, State: "running"}
	m.mu.Lock()
	m.xfers[tr.ID] = cancel
	m.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			m.mu.Lock()
			delete(m.xfers, tr.ID)
			m.mu.Unlock()
		}()
		err := run(ctx, s.c, tr)
		switch {
		case ctx.Err() != nil:
			tr.State = "cancelled"
		case err != nil:
			tr.State, tr.Error = "error", err.Error()
		default:
			tr.State = "done"
		}
		m.emit.Emit("xfer:progress", *tr)
	}()
	return tr.ID, nil
}

// CancelTransfer stops a running transfer.
func (m *Manager) CancelTransfer(id string) {
	m.mu.Lock()
	c := m.xfers[id]
	m.mu.Unlock()
	if c != nil {
		c()
	}
}

func (m *Manager) runTransfer(ctx context.Context, c *sftp.Client, tr *Transfer, sources []string, destDir string) error {
	var items []xferItem
	for _, src := range sources {
		if tr.Direction == "up" {
			base := filepath.Dir(src)
			err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel(base, p)
				it := xferItem{src: p, dst: path.Join(destDir, filepath.ToSlash(rel)), dir: d.IsDir()}
				if !d.IsDir() {
					if fi, err := d.Info(); err == nil {
						it.size = fi.Size()
					}
				}
				items = append(items, it)
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			base := path.Dir(src)
			w := c.Walk(src)
			for w.Step() {
				if w.Err() != nil {
					return w.Err()
				}
				rel := strings.TrimPrefix(strings.TrimPrefix(w.Path(), base), "/")
				st := w.Stat()
				items = append(items, xferItem{src: w.Path(), dst: filepath.Join(destDir, filepath.FromSlash(rel)), dir: st.IsDir(), size: st.Size()})
			}
		}
	}
	return m.copyItems(ctx, c, tr, items, false)
}

// copyItems copies the collected items; keepTimes copies modification times
// so later syncs can compare them.
func (m *Manager) copyItems(ctx context.Context, c *sftp.Client, tr *Transfer, items []xferItem, keepTimes bool) error {
	for _, it := range items {
		if !it.dir {
			tr.FilesTotal++
			tr.Total += it.size
		}
	}
	var done atomic.Int64
	last := time.Now()
	report := func(force bool) {
		if force || time.Since(last) > 200*time.Millisecond {
			last = time.Now()
			tr.Bytes = done.Load()
			m.emit.Emit("xfer:progress", *tr)
		}
	}
	report(true)
	for _, it := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var err error
		if it.dir {
			if tr.Direction == "up" {
				err = c.MkdirAll(it.dst)
			} else {
				err = os.MkdirAll(it.dst, 0o755)
			}
			if err != nil {
				return err
			}
			continue
		}
		tr.File = it.src
		// Resume: continue a shorter destination file, skip a complete one.
		var offset int64
		if tr.resume {
			var have int64 = -1
			if tr.Direction == "up" {
				if st, e := c.Stat(it.dst); e == nil && !st.IsDir() {
					have = st.Size()
				}
			} else if st, e := os.Stat(it.dst); e == nil && !st.IsDir() {
				have = st.Size()
			}
			if have == it.size {
				done.Add(it.size)
				tr.FilesDone++
				report(true)
				continue
			}
			if have > 0 && have < it.size {
				offset = have
			}
		}
		var src io.ReadSeekCloser
		var dst io.WriteCloser
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if offset > 0 {
			flags = os.O_WRONLY | os.O_APPEND
		}
		if tr.Direction == "up" {
			if src, err = os.Open(it.src); err == nil {
				var rf *sftp.File
				if rf, err = c.OpenFile(it.dst, flags&^os.O_APPEND); err == nil {
					if offset > 0 {
						_, err = rf.Seek(offset, io.SeekStart)
					}
					dst = rf
				}
			}
		} else {
			if src, err = c.Open(it.src); err == nil {
				if err = os.MkdirAll(filepath.Dir(it.dst), 0o755); err == nil {
					dst, err = os.OpenFile(it.dst, flags, 0o644)
				}
			}
		}
		if err == nil && offset > 0 {
			_, err = src.Seek(offset, io.SeekStart)
			done.Add(offset)
		}
		if err != nil {
			if src != nil {
				src.Close()
			}
			if dst != nil {
				dst.Close()
			}
			return fmt.Errorf("%s: %w", it.src, err)
		}
		_, err = io.Copy(dst, &progressReader{ctx: ctx, r: src, n: &done, tick: func() { report(false) }})
		src.Close()
		if cerr := dst.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("%s: %w", it.src, err)
		}
		if keepTimes {
			if tr.Direction == "up" {
				if fi, err := os.Stat(it.src); err == nil {
					_ = c.Chtimes(it.dst, fi.ModTime(), fi.ModTime())
				}
			} else if fi, err := c.Stat(it.src); err == nil {
				_ = os.Chtimes(it.dst, fi.ModTime(), fi.ModTime())
			}
		}
		tr.FilesDone++
		report(true)
	}
	tr.Bytes = done.Load()
	return nil
}

type progressReader struct {
	ctx  context.Context
	r    io.Reader
	n    *atomic.Int64
	tick func()
}

func (p *progressReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.r.Read(b)
	p.n.Add(int64(n))
	p.tick()
	return n, err
}

// StartRemoteCopy copies files/folders from one SFTP session to another
// (site-to-site, e.g. server A -> server B). Data streams through this
// computer; nothing is stored locally.
func (m *Manager) StartRemoteCopy(srcID, dstID string, sources []string, destDir string) (string, error) {
	src, err := m.sftpGet(srcID)
	if err != nil {
		return "", err
	}
	dst, err := m.sftpGet(dstID)
	if err != nil {
		return "", err
	}
	if srcID == dstID {
		return "", errors.New("choose a different SFTP session as the destination")
	}
	ctx, cancel := context.WithCancel(context.Background())
	tr := &Transfer{ID: newID(), Direction: "s2s", State: "running"}
	m.mu.Lock()
	m.xfers[tr.ID] = cancel
	m.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			m.mu.Lock()
			delete(m.xfers, tr.ID)
			m.mu.Unlock()
		}()
		err := m.remoteCopy(ctx, src.c, dst.c, tr, sources, destDir)
		switch {
		case ctx.Err() != nil:
			tr.State = "cancelled"
		case err != nil:
			tr.State, tr.Error = "error", err.Error()
		default:
			tr.State = "done"
		}
		m.emit.Emit("xfer:progress", *tr)
	}()
	return tr.ID, nil
}

func (m *Manager) remoteCopy(ctx context.Context, from, to *sftp.Client, tr *Transfer, sources []string, destDir string) error {
	var items []xferItem
	for _, s := range sources {
		base := path.Dir(s)
		w := from.Walk(s)
		for w.Step() {
			if w.Err() != nil {
				return w.Err()
			}
			rel := strings.TrimPrefix(strings.TrimPrefix(w.Path(), base), "/")
			st := w.Stat()
			it := xferItem{src: w.Path(), dst: path.Join(destDir, rel), dir: st.IsDir(), size: st.Size()}
			if !it.dir {
				tr.FilesTotal++
				tr.Total += it.size
			}
			items = append(items, it)
		}
	}
	var done atomic.Int64
	last := time.Now()
	report := func(force bool) {
		if force || time.Since(last) > 200*time.Millisecond {
			last = time.Now()
			tr.Bytes = done.Load()
			m.emit.Emit("xfer:progress", *tr)
		}
	}
	report(true)
	for _, it := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if it.dir {
			if err := to.MkdirAll(it.dst); err != nil {
				return fmt.Errorf("%s: %w", it.dst, err)
			}
			continue
		}
		tr.File = it.src
		r, err := from.Open(it.src)
		if err != nil {
			return fmt.Errorf("%s: %w", it.src, err)
		}
		w, err := to.Create(it.dst)
		if err != nil {
			r.Close()
			return fmt.Errorf("%s: %w", it.dst, err)
		}
		_, err = io.Copy(w, &progressReader{ctx: ctx, r: r, n: &done, tick: func() { report(false) }})
		r.Close()
		if cerr := w.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("%s: %w", it.src, err)
		}
		if st, err := from.Stat(it.src); err == nil {
			_ = to.Chtimes(it.dst, st.ModTime(), st.ModTime())
		}
		tr.FilesDone++
		report(true)
	}
	tr.Bytes = done.Load()
	return nil
}
