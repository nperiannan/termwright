// Package logging writes session output to log files.
package logging

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Vars holds values for filename/prefix substitutions.
type Vars struct {
	Host    string
	Session string
	Port    int
	User    string
	Proto   string
}

// Expand replaces substitution tokens:
//
//	%H host  %S session name  %P port  %U user  %R protocol
//	%Y year  %M month  %D day  %h hour  %m minute  %s second  %t milliseconds
//	%%  literal percent
func Expand(pattern string, v Vars, t time.Time) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c != '%' || i+1 >= len(pattern) {
			b.WriteByte(c)
			continue
		}
		i++
		switch pattern[i] {
		case 'H':
			b.WriteString(v.Host)
		case 'S':
			b.WriteString(strings.ReplaceAll(v.Session, "/", "_"))
		case 'P':
			b.WriteString(itoa(v.Port))
		case 'U':
			b.WriteString(v.User)
		case 'R':
			b.WriteString(v.Proto)
		case 'Y':
			b.WriteString(t.Format("2006"))
		case 'M':
			b.WriteString(t.Format("01"))
		case 'D':
			b.WriteString(t.Format("02"))
		case 'h':
			b.WriteString(t.Format("15"))
		case 'm':
			b.WriteString(t.Format("04"))
		case 's':
			b.WriteString(t.Format("05"))
		case 't':
			b.WriteString(t.Format(".000")[1:])
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(pattern[i])
		}
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	if neg {
		d = append([]byte{'-'}, d...)
	}
	return string(d)
}

// Options configure a log.
type Options struct {
	Path       string // already expanded
	Append     bool
	Raw        bool   // write bytes exactly as received (with escape sequences)
	LinePrefix string // pattern expanded at the start of each line
	Vars       Vars
}

// Logger writes terminal output to a file.
type Logger struct {
	mu      sync.Mutex
	f       *os.File
	w       *bufio.Writer
	opt     Options
	strip   Stripper
	atStart bool
	stop    chan struct{}
}

// Open creates the log file.
func Open(opt Options) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(opt.Path), 0o700); err != nil {
		return nil, err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if opt.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(opt.Path, flags, 0o600)
	if err != nil {
		return nil, err
	}
	l := &Logger{f: f, w: bufio.NewWriterSize(f, 32*1024), opt: opt, atStart: true, stop: make(chan struct{})}
	go l.flusher()
	return l, nil
}

// Path returns the log file path.
func (l *Logger) Path() string { return l.opt.Path }

func (l *Logger) flusher() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			l.mu.Lock()
			if l.w != nil {
				_ = l.w.Flush()
			}
			l.mu.Unlock()
		case <-l.stop:
			return
		}
	}
}

// Write logs a chunk of terminal output.
func (l *Logger) Write(p []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil {
		return
	}
	if l.opt.Raw {
		_, _ = l.w.Write(p)
		return
	}
	text := l.strip.Strip(p)
	for _, c := range text {
		if c == '\r' {
			continue
		}
		if l.atStart && l.opt.LinePrefix != "" {
			_, _ = l.w.WriteString(Expand(l.opt.LinePrefix, l.opt.Vars, time.Now()))
		}
		l.atStart = c == '\n'
		_ = l.w.WriteByte(c)
	}
}

// Close flushes and closes the file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil {
		return nil
	}
	close(l.stop)
	_ = l.w.Flush()
	err := l.f.Close()
	l.w, l.f = nil, nil
	return err
}

// Stripper removes ANSI/VT escape sequences from a byte stream. It keeps
// state between calls so sequences split across reads are handled.
type Stripper struct {
	state int // 0 text, 1 ESC, 2 CSI, 3 OSC/DCS string, 4 string ESC, 5 one-char after ESC (charset)
}

// Strip returns p with control sequences removed. Tabs, CR, LF, backspace
// are kept; backspace removes the previous character of the same chunk.
func (s *Stripper) Strip(p []byte) []byte {
	out := make([]byte, 0, len(p))
	for _, c := range p {
		switch s.state {
		case 0:
			switch {
			case c == 0x1b:
				s.state = 1
			case c == 0x9b:
				s.state = 2
			case c == '\b':
				if n := len(out); n > 0 && out[n-1] != '\n' {
					out = out[:n-1]
				}
			case c == '\n' || c == '\r' || c == '\t' || c >= 0x20 && c != 0x7f:
				out = append(out, c)
			}
		case 1:
			switch c {
			case '[':
				s.state = 2
			case ']', 'P', '_', '^', 'X':
				s.state = 3
			case '(', ')', '*', '+', '-', '.', '/', '#', '%', ' ':
				s.state = 5
			default:
				s.state = 0
			}
		case 2:
			if c >= 0x40 && c <= 0x7e {
				s.state = 0
			}
		case 3:
			if c == 0x07 {
				s.state = 0
			} else if c == 0x1b {
				s.state = 4
			}
		case 4:
			if c == '\\' {
				s.state = 0
			} else {
				s.state = 3
			}
		case 5:
			s.state = 0
		}
	}
	return out
}
