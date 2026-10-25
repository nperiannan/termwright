package term

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
)

var (
	zSigRQINIT = []byte{zPAD, zPAD, zDLE, zHEX, '0', '0'}
	zSigRINIT  = []byte{zPAD, zPAD, zDLE, zHEX, '0', '1'}
)

// zdetect looks for the start of a ZMODEM session in output: ZRQINIT (remote
// ran "sz", we receive) or ZRINIT (remote ran "rz", we send). It returns the
// index in p where the sequence starts (or 0 if it began in a previous chunk).
func (m *Manager) zdetect(t *Tab, p []byte) (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	win := append(append([]byte(nil), t.zwin...), p...)
	keep := len(win)
	if keep > 8 {
		keep = 8
	}
	t.zwin = append([]byte(nil), win[len(win)-keep:]...)
	for _, sig := range []struct {
		b    []byte
		kind int
	}{{zSigRQINIT, zRQINIT}, {zSigRINIT, zRINIT}} {
		if i := bytes.Index(win, sig.b); i >= 0 {
			if sig.kind == zRINIT && t.zpending {
				return -1, 0
			}
			t.zwin = nil
			if sig.kind == zRINIT {
				t.zpending = true
			}
			idx := i - (len(win) - len(p))
			if idx < 0 {
				idx = 0
			}
			return idx, sig.kind
		}
	}
	return -1, 0
}

// beginCapture diverts a tab's output to a byte channel for a transfer.
func (m *Manager) beginCapture(t *Tab, initial []byte) (chan byte, error) {
	in := make(chan byte, 1<<16)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.capture != nil {
		return nil, errors.New("a transfer is already running on this tab")
	}
	for _, b := range initial {
		in <- b
	}
	t.capture = func(p []byte) {
		for _, b := range p {
			select {
			case in <- b:
			default:
			}
		}
	}
	return in, nil
}

func (m *Manager) endCapture(t *Tab) {
	t.mu.Lock()
	t.capture = nil
	t.zpending = false
	t.mu.Unlock()
}

// runCaptured runs a transfer function with the tab's stream captured and
// reports progress as a Transfer.
func (m *Manager) runCaptured(t *Tab, tr *Transfer, in chan byte, fn func(ctx context.Context, in chan byte) error) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.xfers[tr.ID] = cancel
	m.mu.Unlock()
	m.emit.Emit("xfer:progress", *tr)
	go func() {
		defer func() {
			m.endCapture(t)
			if tr.State == "done" {
				m.flushCaptured(t, tr.rest, in)
			}
			cancel()
			m.mu.Lock()
			delete(m.xfers, tr.ID)
			m.mu.Unlock()
		}()
		err := fn(ctx, in)
		switch {
		case ctx.Err() != nil:
			_ = t.rawWrite(zAbortSeq)
			tr.State = "cancelled"
		case err != nil:
			tr.State, tr.Error = "error", err.Error()
		default:
			tr.State = "done"
		}
		m.emit.Emit("xfer:progress", *tr)
	}()
}

// flushCaptured shows output that arrived after the transfer finished but
// before the capture ended (e.g. the shell prompt right after sz).
func (m *Manager) flushCaptured(t *Tab, rest []byte, in chan byte) {
	for {
		select {
		case b := <-in:
			rest = append(rest, b)
			continue
		default:
		}
		break
	}
	if bytes.HasPrefix(rest, []byte("OO")) {
		rest = rest[2:]
	}
	if len(rest) > 0 {
		m.emit.Emit("term:data:"+t.id, base64.StdEncoding.EncodeToString(rest))
	}
}

func (m *Manager) downloadDir() string {
	if m.DownloadDir != nil {
		if d := m.DownloadDir(); d != "" {
			return expandHome(d)
		}
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Downloads")
}

// startZReceive handles "sz" on the remote side: files go to the download folder.
func (m *Manager) startZReceive(t *Tab, initial []byte) {
	in, err := m.beginCapture(t, initial)
	if err != nil {
		return
	}
	tr := &Transfer{ID: newID(), Direction: "zmodem-down", State: "running", File: m.downloadDir()}
	m.runCaptured(t, tr, in, func(ctx context.Context, in chan byte) error {
		files, err := zmodemReceive(ctx, in, t.rawWrite, m.downloadDir(), func(f string, done, total int64) {
			tr.File, tr.Bytes, tr.Total = f, done, total
			m.emit.Emit("xfer:progress", *tr)
		}, &tr.rest)
		tr.FilesDone, tr.FilesTotal = len(files), len(files)
		return err
	})
}

// ZmodemSend answers a remote "rz" with the chosen files; an empty list cancels.
func (m *Manager) ZmodemSend(tabID string, files []string) (string, error) {
	t, err := m.get(tabID)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		_ = t.rawWrite(zAbortSeq)
		m.endCapture(t)
		return "", nil
	}
	in, err := m.beginCapture(t, nil)
	if err != nil {
		return "", err
	}
	tr := &Transfer{ID: newID(), Direction: "zmodem-up", State: "running", FilesTotal: len(files)}
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			tr.Total += st.Size()
		}
	}
	var base int64
	last := ""
	m.runCaptured(t, tr, in, func(ctx context.Context, in chan byte) error {
		return zmodemSend(ctx, in, t.rawWrite, files, func(f string, done, _ int64) {
			if f != last {
				if last != "" {
					tr.FilesDone++
					if st, err := os.Stat(last); err == nil {
						base += st.Size()
					}
				}
				last = f
			}
			tr.File, tr.Bytes = f, base+done
			m.emit.Emit("xfer:progress", *tr)
		})
	})
	return tr.ID, nil
}

// ReceiveXmodem receives a file with XMODEM into outPath (start the sender on
// the device after calling this).
func (m *Manager) ReceiveXmodem(tabID, outPath string) (string, error) {
	t, err := m.get(tabID)
	if err != nil {
		return "", err
	}
	if !t.isConnected() {
		return "", errors.New("tab is not connected")
	}
	in, err := m.beginCapture(t, nil)
	if err != nil {
		return "", err
	}
	tr := &Transfer{ID: newID(), Direction: "xmodem-down", File: filepath.Base(outPath), FilesTotal: 1, State: "running"}
	m.runCaptured(t, tr, in, func(ctx context.Context, in chan byte) error {
		err := xmodemReceive(ctx, t.rawWrite, in, outPath, func(n int64) {
			tr.Bytes = n
			m.emit.Emit("xfer:progress", *tr)
		})
		if err == nil {
			tr.FilesDone = 1
		}
		return err
	})
	return tr.ID, nil
}

// SendYmodem uploads files with YMODEM batch (start "rb" on the device first).
func (m *Manager) SendYmodem(tabID string, files []string) (string, error) {
	t, err := m.get(tabID)
	if err != nil {
		return "", err
	}
	if !t.isConnected() {
		return "", errors.New("tab is not connected")
	}
	in, err := m.beginCapture(t, nil)
	if err != nil {
		return "", err
	}
	tr := &Transfer{ID: newID(), Direction: "ymodem-up", State: "running", FilesTotal: len(files)}
	sizes := map[string]int64{}
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			tr.Total += st.Size()
			sizes[f] = st.Size()
		}
	}
	var base int64
	last := ""
	m.runCaptured(t, tr, in, func(ctx context.Context, in chan byte) error {
		return ymodemSend(ctx, t.rawWrite, in, files, func(f string, done, _ int64) {
			if f != last {
				if last != "" {
					tr.FilesDone++
					base += sizes[last]
				}
				last = f
			}
			tr.File, tr.Bytes = filepath.Base(f), base+done
			m.emit.Emit("xfer:progress", *tr)
		})
	})
	return tr.ID, nil
}

// ReceiveYmodem receives a YMODEM batch into the download folder (start "sb" on the device).
func (m *Manager) ReceiveYmodem(tabID string) (string, error) {
	t, err := m.get(tabID)
	if err != nil {
		return "", err
	}
	if !t.isConnected() {
		return "", errors.New("tab is not connected")
	}
	in, err := m.beginCapture(t, nil)
	if err != nil {
		return "", err
	}
	dir := m.downloadDir()
	_ = os.MkdirAll(dir, 0o755)
	tr := &Transfer{ID: newID(), Direction: "ymodem-down", State: "running", File: dir}
	m.runCaptured(t, tr, in, func(ctx context.Context, in chan byte) error {
		files, err := ymodemReceive(ctx, t.rawWrite, in, dir, func(f string, done, total int64) {
			tr.File, tr.Bytes, tr.Total = filepath.Base(f), done, total
			m.emit.Emit("xfer:progress", *tr)
		})
		tr.FilesDone, tr.FilesTotal = len(files), len(files)
		return err
	})
	return tr.ID, nil
}
