package term

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// shellQuote quotes a path for a POSIX shell.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func scpAck(r *bufio.Reader) error {
	b, err := r.ReadByte()
	if err != nil {
		return err
	}
	if b == 0 {
		return nil
	}
	msg, _ := r.ReadString('\n')
	return fmt.Errorf("scp: %s", strings.TrimSpace(msg))
}

// SCPTransfer copies a file or a whole folder with the classic SCP protocol over a connected
// SSH2 tab, for devices without an SFTP server.
// direction "up": src is local, dest is a remote path or folder.
// direction "down": src is remote, dest is a local folder.
func (m *Manager) SCPTransfer(tabID, direction, src, dest string) (string, error) {
	client, err := m.sshClientOf(tabID)
	if err != nil {
		return "", err
	}
	return m.scpStart(client, direction, src, dest)
}

// SCPTransferSFTP copies with SCP over the connection of an SFTP tab (the
// SFTP tab is used for browsing, SCP for the data - for devices whose SFTP
// server is slow or limited).
func (m *Manager) SCPTransferSFTP(sftpID, direction, src, dest string) (string, error) {
	s, err := m.sftpGet(sftpID)
	if err != nil {
		return "", err
	}
	if s.root {
		return "", errors.New("SCP cannot copy as another user - switch Copy via to SFTP (it runs as that user in this tab)")
	}
	client := s.client
	if client == nil {
		if client, err = m.sshClientOf(s.tabID); err != nil {
			return "", err
		}
	}
	return m.scpStartHop(client, s.hop, direction, src, dest)
}

func (m *Manager) scpStart(client *ssh.Client, direction, src, dest string) (string, error) {
	return m.scpStartHop(client, nil, direction, src, dest)
}

// scpStartHop runs scp on the hop target (through the jump station) when hop is set.
func (m *Manager) scpStartHop(client *ssh.Client, hop *Hop, direction, src, dest string) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	tr := &Transfer{ID: newID(), Direction: "scp-" + direction, File: src, FilesTotal: 1, State: "running"}
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
		sess, err := client.NewSession()
		if err == nil {
			defer sess.Close()
			stop := context.AfterFunc(ctx, func() { _ = sess.Close() })
			defer stop()
			var done atomic.Int64
			last := time.Now()
			tick := func() {
				if time.Since(last) > 200*time.Millisecond {
					last = time.Now()
					tr.Bytes = done.Load()
					m.emit.Emit("xfer:progress", *tr)
				}
			}
			var ss scpSession = sess
			if hop != nil {
				ss = hopSession{sess, *hop}
			}
			if direction == "up" {
				err = scpUpload(ctx, ss, src, dest, tr, &done, tick)
			} else {
				err = scpDownload(ctx, ss, src, dest, tr, &done, tick)
			}
			tr.Bytes = done.Load()
		}
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

type scpSession interface {
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.Reader, error)
	Start(cmd string) error
	Wait() error
}

// scpUpload copies a local file or folder (recursively) to remote.
func scpUpload(ctx context.Context, sess scpSession, local, remote string, tr *Transfer, done *atomic.Int64, tick func()) error {
	st, err := os.Stat(local)
	if err != nil {
		return err
	}
	files := 0
	_ = filepath.WalkDir(local, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, e := d.Info(); e == nil {
				tr.Total += fi.Size()
				files++
			}
		}
		return nil
	})
	tr.FilesTotal = files
	w, _ := sess.StdinPipe()
	out, _ := sess.StdoutPipe()
	r := bufio.NewReader(out)
	cmd := "scp -t "
	if st.IsDir() {
		cmd = "scp -r -t "
	}
	if err := sess.Start(cmd + shellQuote(remote)); err != nil {
		return err
	}
	if err := scpAck(r); err != nil {
		return err
	}
	if err := scpSendItem(ctx, w, r, local, tr, done, tick); err != nil {
		return err
	}
	w.Close()
	return sess.Wait()
}

func scpSendItem(ctx context.Context, w io.Writer, r *bufio.Reader, local string, tr *Transfer, done *atomic.Int64, tick func()) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	st, err := os.Stat(local)
	if err != nil {
		return err
	}
	name := filepath.Base(local)
	if st.IsDir() {
		if _, err := fmt.Fprintf(w, "D%04o 0 %s\n", st.Mode().Perm(), name); err != nil {
			return err
		}
		if err := scpAck(r); err != nil {
			return err
		}
		ents, err := os.ReadDir(local)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if e.IsDir() || e.Type().IsRegular() {
				if err := scpSendItem(ctx, w, r, filepath.Join(local, e.Name()), tr, done, tick); err != nil {
					return err
				}
			}
		}
		if _, err := w.Write([]byte("E\n")); err != nil {
			return err
		}
		return scpAck(r)
	}
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	tr.File = local
	if _, err := fmt.Fprintf(w, "C%04o %d %s\n", st.Mode().Perm(), st.Size(), name); err != nil {
		return err
	}
	if err := scpAck(r); err != nil {
		return err
	}
	if _, err := io.Copy(w, &progressReader{ctx: ctx, r: f, n: done, tick: tick}); err != nil {
		return err
	}
	if _, err := w.Write([]byte{0}); err != nil {
		return err
	}
	if err := scpAck(r); err != nil {
		return err
	}
	tr.FilesDone++
	return nil
}

// scpDownload copies a remote file or folder (recursively) into localDir.
func scpDownload(ctx context.Context, sess scpSession, remote, localDir string, tr *Transfer, done *atomic.Int64, tick func()) error {
	w, _ := sess.StdinPipe()
	out, _ := sess.StdoutPipe()
	r := bufio.NewReader(out)
	if err := sess.Start("scp -r -f " + shellQuote(remote)); err != nil {
		return err
	}
	ack := func() error { _, err := w.Write([]byte{0}); return err }
	if err := ack(); err != nil {
		return err
	}
	dirs := []string{localDir}
	cur := func() string { return dirs[len(dirs)-1] }
	got := false
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := r.ReadString('\n')
		if err == io.EOF && line == "" && got {
			w.Close()
			return sess.Wait()
		}
		if err != nil {
			return err
		}
		switch line[0] {
		case 1, 2:
			return fmt.Errorf("scp: %s", strings.TrimSpace(line[1:]))
		case 'T': // times
			_ = ack()
			continue
		case 'E':
			if len(dirs) > 1 {
				dirs = dirs[:len(dirs)-1]
			}
			_ = ack()
			continue
		case 'D', 'C':
		default:
			return fmt.Errorf("scp: unexpected %q", strings.TrimSpace(line))
		}
		got = true
		f := strings.SplitN(strings.TrimSpace(line[1:]), " ", 3)
		if len(f) != 3 {
			return errors.New("scp: bad header")
		}
		name := path.Base(f[2])
		if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return fmt.Errorf("scp: refusing unsafe name %q", f[2])
		}
		if line[0] == 'D' {
			d := filepath.Join(cur(), name)
			if len(dirs) == 1 {
				d = uniqueFile(d)
			}
			if err := os.MkdirAll(d, 0o755); err != nil {
				return err
			}
			dirs = append(dirs, d)
			_ = ack()
			continue
		}
		size, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			return err
		}
		tr.Total += size
		dst := filepath.Join(cur(), name)
		if len(dirs) == 1 {
			dst = uniqueFile(dst)
		}
		tr.File = dst
		fh, err := os.Create(dst)
		if err != nil {
			return err
		}
		_ = ack()
		_, err = io.CopyN(fh, &progressReader{ctx: ctx, r: r, n: done, tick: tick}, size)
		fh.Close()
		if err != nil {
			return err
		}
		if err := scpAck(r); err != nil {
			return err
		}
		tr.FilesDone++
		tr.FilesTotal = tr.FilesDone
		_ = ack()
	}
}
