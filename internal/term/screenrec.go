package term

import (
	"errors"
	"time"

	"github.com/termwright/termwright/internal/recording"
)

// Session recordings (asciinema .cast): what the device shows, with its
// timing, window size changes and markers. Keystrokes are not recorded.

// StartSessionRecording starts recording a connected tab and returns the file.
func (m *Manager) StartSessionRecording(id string) (string, error) {
	t, err := m.get(id)
	if err != nil {
		return "", err
	}
	if _, quiet := m.quiet.Load(id); quiet {
		return "", nil // background runs are never recorded
	}
	if m.RecordDir == nil {
		return "", errors.New("recordings are not available")
	}
	t.mu.Lock()
	if t.cast != nil {
		p := t.cast.Path()
		t.mu.Unlock()
		return p, nil
	}
	s, cols, rows := t.sess, t.cols, t.rows
	t.mu.Unlock()
	name := s.Path
	if name == "" {
		name = "Ad hoc/" + safeName(title(s))
	}
	st := recording.Store{Dir: m.RecordDir()}
	now := time.Now()
	path := st.FileName(name, now)
	w, err := recording.Create(path, recording.Header{Width: cols, Height: rows, Title: title(s), Session: s.Path,
		Host: s.Hostname, User: s.Username, Protocol: s.Protocol})
	if err != nil {
		// two connections in the same second: try the next second's name
		path = st.FileName(name, now.Add(time.Second))
		if w, err = recording.Create(path, recording.Header{Width: cols, Height: rows, Title: title(s), Session: s.Path,
			Host: s.Hostname, User: s.Username, Protocol: s.Protocol}); err != nil {
			return "", err
		}
	}
	t.mu.Lock()
	if t.cast != nil { // started meanwhile
		t.mu.Unlock()
		_ = w.Close()
		return t.cast.Path(), nil
	}
	t.cast = w
	t.mu.Unlock()
	m.emit.Emit("term:rec:"+id, path)
	return path, nil
}

// StopSessionRecording stops recording a tab.
func (m *Manager) StopSessionRecording(id string) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	t.mu.Lock()
	w := t.cast
	t.cast = nil
	t.mu.Unlock()
	if w == nil {
		return nil
	}
	m.emit.Emit("term:rec:"+id, "")
	return w.Close()
}

// AddMarker adds a marker with a note to a tab's recording.
func (m *Manager) AddMarker(id, note string) error {
	t, err := m.get(id)
	if err != nil {
		return err
	}
	t.mu.Lock()
	w := t.cast
	t.mu.Unlock()
	if w == nil {
		return errors.New("this tab is not being recorded")
	}
	w.Marker(note)
	return nil
}

// SessionRecording returns the file a tab is being recorded to ("" if none).
func (m *Manager) SessionRecording(id string) string {
	t, err := m.get(id)
	if err != nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cast == nil {
		return ""
	}
	return t.cast.Path()
}

func safeName(s string) string {
	b := []rune(s)
	for i, r := range b {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b[i] = '-'
		}
	}
	out := string(b)
	if out == "" || out == "." || out == ".." {
		return "session"
	}
	return out
}
