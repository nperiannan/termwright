// Package recording writes and reads terminal session recordings in the
// asciinema v2 format (.cast): a JSON header line followed by one JSON array
// per event: [seconds, code, data] where code is "o" (output), "r" (resize,
// "COLSxROWS") or "m" (marker).
package recording

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Header is the first line of a recording.
type Header struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp"`
	Title     string            `json:"title,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	// Termwright extras (ignored by other players).
	Session  string `json:"termwright_session,omitempty"`
	Host     string `json:"termwright_host,omitempty"`
	User     string `json:"termwright_user,omitempty"`
	Protocol string `json:"termwright_protocol,omitempty"`
}

// Event is one recorded event.
type Event struct {
	T    float64
	Code string
	Data string
}

// MarshalJSON writes the event as [t, code, data].
func (e Event) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{e.T, e.Code, e.Data})
}

// UnmarshalJSON reads [t, code, data].
func (e *Event) UnmarshalJSON(b []byte) error {
	var a []json.RawMessage
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if len(a) < 3 {
		return errors.New("event needs 3 values")
	}
	if err := json.Unmarshal(a[0], &e.T); err != nil {
		return err
	}
	if err := json.Unmarshal(a[1], &e.Code); err != nil {
		return err
	}
	return json.Unmarshal(a[2], &e.Data)
}

// Writer records one connection.
type Writer struct {
	mu      sync.Mutex
	f       *os.File
	w       *bufio.Writer
	start   time.Time
	pending []byte // incomplete UTF-8 sequence from the last output
	path    string
	err     error
	now     func() time.Time
	closed  bool
	flushAt time.Time
}

// Create starts a recording at path (folders are created).
func Create(path string, h Header) (*Writer, error) {
	return create(path, h, time.Now)
}

func create(path string, h Header, now func() time.Time) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	start := now()
	h.Version = 2
	if h.Timestamp == 0 {
		h.Timestamp = start.Unix()
	}
	if h.Width <= 0 {
		h.Width = 80
	}
	if h.Height <= 0 {
		h.Height = 24
	}
	if h.Env == nil {
		h.Env = map[string]string{"TERM": "xterm-256color"}
	}
	w := &Writer{f: f, w: bufio.NewWriterSize(f, 64*1024), start: start, path: path, now: now}
	b, _ := json.Marshal(h)
	_, _ = w.w.Write(append(b, '\n'))
	if err := w.w.Flush(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return w, nil
}

// Path is the file being written.
func (w *Writer) Path() string { return w.path }

func (w *Writer) event(code, data string) {
	if w.closed || w.err != nil {
		return
	}
	now := w.now()
	t := now.Sub(w.start).Seconds()
	b, _ := json.Marshal(Event{T: float64(int64(t*1e6)) / 1e6, Code: code, Data: data})
	if _, err := w.w.Write(append(b, '\n')); err != nil {
		w.err = err
		return
	}
	// flush at most every half second so a crash loses little
	if now.Sub(w.flushAt) > 500*time.Millisecond {
		w.flushAt = now
		if err := w.w.Flush(); err != nil {
			w.err = err
		}
	}
}

// Output records what the device sent. Incomplete UTF-8 at the end is kept
// for the next call; other invalid bytes become U+FFFD.
func (w *Writer) Output(p []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	buf := append(w.pending, p...)
	w.pending = nil
	cut := len(buf)
	// keep up to 3 trailing bytes of an unfinished rune
	for i := 1; i <= 3 && i <= len(buf); i++ {
		c := buf[len(buf)-i]
		if c < 0x80 {
			break
		}
		if utf8.RuneStart(c) {
			if !utf8.FullRune(buf[len(buf)-i:]) {
				cut = len(buf) - i
			}
			break
		}
	}
	if cut < len(buf) {
		w.pending = append([]byte(nil), buf[cut:]...)
	}
	if cut > 0 {
		w.event("o", strings.ToValidUTF8(string(buf[:cut]), "�"))
	}
}

// Resize records a new window size.
func (w *Writer) Resize(cols, rows int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.event("r", fmt.Sprintf("%dx%d", cols, rows))
}

// Marker records a marker with a label.
func (w *Writer) Marker(label string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.event("m", label)
	if w.err == nil && !w.closed {
		w.err = w.w.Flush()
	}
}

// Close finishes the recording.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	if len(w.pending) > 0 {
		p := w.pending
		w.pending = nil
		w.event("o", strings.ToValidUTF8(string(p), "�"))
	}
	w.closed = true
	err := w.w.Flush()
	if cerr := w.f.Close(); err == nil {
		err = cerr
	}
	if w.err != nil {
		return w.err
	}
	return err
}

// Recording is a loaded recording.
type Recording struct {
	Header Header  `json:"header"`
	Events []Event `json:"events"`
}

// Load reads a recording; lines that cannot be read are skipped.
func Load(path string) (*Recording, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Read reads a recording from r.
func Read(r io.Reader) (*Recording, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	if !sc.Scan() {
		return nil, errors.New("empty recording")
	}
	var rec Recording
	if err := json.Unmarshal(sc.Bytes(), &rec.Header); err != nil {
		return nil, fmt.Errorf("not a recording: %w", err)
	}
	if rec.Header.Version != 2 {
		return nil, fmt.Errorf("recording format version %d is not supported (only asciinema v2)", rec.Header.Version)
	}
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			rec.Events = append(rec.Events, e)
		}
	}
	if rec.Events == nil {
		rec.Events = []Event{}
	}
	return &rec, sc.Err()
}

// Info describes a recording file.
type Info struct {
	Path     string  `json:"path"`
	Session  string  `json:"session"`
	Title    string  `json:"title"`
	Host     string  `json:"host"`
	Started  string  `json:"started"`
	Duration float64 `json:"duration"`
	Markers  int     `json:"markers"`
	Size     int64   `json:"size"`
}

// Stat reads the header and the last event time of a recording.
func Stat(path string) (Info, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	if !sc.Scan() {
		return Info{}, errors.New("empty recording")
	}
	var h Header
	if err := json.Unmarshal(sc.Bytes(), &h); err != nil || h.Version != 2 {
		return Info{}, errors.New("not a recording")
	}
	in := Info{Path: path, Session: h.Session, Title: h.Title, Host: h.Host, Size: st.Size(),
		Started: time.Unix(h.Timestamp, 0).Format(time.RFC3339)}
	for sc.Scan() {
		line := sc.Bytes()
		// cheap parse: only the time and the code are needed
		if len(line) < 3 || line[0] != '[' {
			continue
		}
		rest := line[1:]
		i := strings.IndexByte(string(rest[:min(len(rest), 32)]), ',')
		if i < 0 {
			continue
		}
		var t float64
		if _, err := fmt.Sscanf(string(rest[:i]), "%g", &t); err == nil && t > in.Duration {
			in.Duration = t
		}
		if strings.HasPrefix(strings.TrimSpace(string(rest[i+1:min(len(rest), i+8)])), `"m"`) {
			in.Markers++
		}
	}
	return in, nil
}

// Store is the folder recordings are kept in.
type Store struct{ Dir string }

// FileName makes a recording path for a session at time t.
func (s Store) FileName(sessionPath string, t time.Time) string {
	p := filepath.FromSlash(strings.Trim(sessionPath, "/"))
	p = filepath.Clean(p)
	if p == "." || p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "..") {
		p = "Ad hoc"
	}
	return filepath.Join(s.Dir, p, t.Format("2006-01-02_150405")+".cast")
}

// List returns the recordings, newest first.
func (s Store) List() ([]Info, error) {
	var out []Info
	err := filepath.WalkDir(s.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if p == s.Dir && errors.Is(err, os.ErrNotExist) {
				return filepath.SkipAll
			}
			return nil
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".cast") {
			return nil
		}
		in, err := Stat(p)
		if err != nil {
			return nil
		}
		if in.Session == "" {
			if rel, err := filepath.Rel(s.Dir, filepath.Dir(p)); err == nil {
				in.Session = filepath.ToSlash(rel)
			}
		}
		out = append(out, in)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Started > out[j].Started })
	if out == nil {
		out = []Info{}
	}
	return out, err
}

// Prune removes recordings older than days (0 keeps everything) and returns
// how many were removed.
func (s Store) Prune(days int, now time.Time) int {
	if days <= 0 || s.Dir == "" {
		return 0
	}
	limit := now.AddDate(0, 0, -days)
	n := 0
	_ = filepath.WalkDir(s.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".cast") {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().Before(limit) {
			if os.Remove(p) == nil {
				n++
			}
		}
		return nil
	})
	return n
}
