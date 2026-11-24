package webrun

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Result is what one action did in a step-through run.
type Result struct {
	I      int    `json:"i"`
	OK     bool   `json:"ok"`
	Note   string `json:"note,omitempty"`
	Error  string `json:"error,omitempty"`
	Healed string `json:"healed,omitempty"`
}

// Session is a browser kept open for step-through runs: run some actions, look, pick an element, record,
// run more. It ends when Close is called or the person closes the browser window.
type Session struct {
	in      io.WriteCloser
	mu      sync.Mutex // one command at a time
	next    int
	lines   chan line
	done    chan struct{}
	OnRec   func(Action) // recorded actions, while recording is on
	OnClose func()       // the browser was closed
	closeMu sync.Once
}

// Open starts a browser for the step (shown unless drive is given: tests) and goes to its page,
// logging in with the app's login when the page asks for it.
func (r Runner) Open(ctx context.Context, w Step, shots string, x Extra, drive ...Drive) (*Session, error) {
	for _, a := range w.Actions {
		if strings.Contains(a.Value, "{{password}}") {
			return nil, errors.New("{{password}} comes from the step's web app or login session - choose one")
		}
	}
	tmo := 10 // step by step: a missing target fails sooner
	if x.Person {
		tmo = 30
	}
	job := x.job(w, shots, tmo)
	job["serve"], job["pictures"], job["video"] = true, false, false
	if len(drive) > 0 {
		job["drive"] = true
	}
	c, stderr, err := r.start(ctx, job)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	c.Stdin = io.MultiReader(c.Stdin, pr)
	outp, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	s := &Session{in: pw, lines: make(chan line, 64), done: make(chan struct{})}
	go func() {
		sc := bufio.NewScanner(outp)
		sc.Buffer(make([]byte, 1<<20), 1<<23)
		for sc.Scan() {
			var l line
			if json.Unmarshal(sc.Bytes(), &l) != nil {
				continue
			}
			if l.Rec != nil {
				if s.OnRec != nil {
					s.OnRec(*l.Rec)
				}
				continue
			}
			if l.Closed {
				break
			}
			s.lines <- l
		}
		_ = c.Wait()
		s.closeMu.Do(func() { close(s.done) })
		if s.OnClose != nil {
			s.OnClose()
		}
		_ = stderr
	}()
	if _, err := s.do(map[string]any{"cmd": "start"}); err != nil {
		if x.Person && !s.closed() {
			return s, err // left open: the person sees the page
		}
		s.Close()
		return nil, err
	}
	return s, nil
}

// do sends a command and returns the lines it printed before its ack.
func (s *Session) do(cmd map[string]any) ([]line, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := s.next
	cmd["id"] = id
	b, _ := json.Marshal(cmd)
	select {
	case <-s.done:
		return nil, errors.New("the browser was closed")
	default:
	}
	if _, err := s.in.Write(append(b, '\n')); err != nil {
		return nil, errors.New("the browser was closed")
	}
	var got []line
	for {
		select {
		case l := <-s.lines:
			if l.Ack != nil && *l.Ack == id {
				for _, x := range got {
					if x.I == -1 && !x.OK && x.Error != "" {
						return got, errors.New(x.Error)
					}
				}
				return got, nil
			}
			got = append(got, l)
		case <-s.done:
			return got, errors.New("the browser was closed")
		}
	}
}

// Run does actions in the open browser (numbered from from), stopping at the first that fails.
func (s *Session) Run(actions []Action, from int) ([]Result, error) {
	ls, err := s.do(map[string]any{"cmd": "run", "actions": actions, "from": from})
	var out []Result
	for _, l := range ls {
		if l.I >= 0 {
			out = append(out, Result{I: l.I, OK: l.OK, Note: l.Note, Error: l.Error, Healed: l.Healed})
		}
	}
	return out, err
}

// Pick waits for the person to click an element in the browser; nil when they pressed Esc.
func (s *Session) Pick() (*Pick, error) {
	ls, err := s.do(map[string]any{"cmd": "pick"})
	for _, l := range ls {
		if l.Pick != nil {
			return l.Pick, err
		}
	}
	return nil, err
}

// Record turns recording on or off: while on, what the person does goes to OnRec.
func (s *Session) Record(on bool) error {
	_, err := s.do(map[string]any{"cmd": "record", "on": on})
	return err
}

// Restart goes to the step's page again (url: another address; "" the step's).
func (s *Session) Restart(url string) error {
	c := map[string]any{"cmd": "start"}
	if url != "" {
		c["url"] = url
	}
	_, err := s.do(c)
	return err
}

// Drive does what a person would (tests).
func (s *Session) Drive(d []Drive) error {
	_, err := s.do(map[string]any{"cmd": "drive", "drive": d})
	return err
}

// Page is the page's address, title and text now.
func (s *Session) Page() (url, title, text string, err error) {
	ls, err := s.do(map[string]any{"cmd": "text"})
	for _, l := range ls {
		if l.Done {
			return l.URL, l.Title, l.Text, err
		}
	}
	return "", "", "", err
}

// Closed is closed when the browser has gone.
func (s *Session) Closed() <-chan struct{} { return s.done }

// Close closes the browser (keeping the login state).
func (s *Session) Close() {
	select {
	case <-s.done:
		return
	default:
	}
	b, _ := json.Marshal(map[string]any{"cmd": "close", "id": -1})
	_, _ = s.in.Write(append(b, '\n'))
	_ = s.in.Close()
	<-s.done
}

// String is for logs.
func (r Result) String() string {
	if r.OK {
		return fmt.Sprintf("✓ %d %s", r.I+1, r.Note)
	}
	return fmt.Sprintf("✗ %d %s", r.I+1, r.Error)
}

// closed: the browser is gone.
func (s *Session) closed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}
