package term

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"

	"github.com/termwright/termwright/internal/session"
)

// Telnet protocol bytes (RFC 854 and friends).
const (
	tIAC  = 255
	tDONT = 254
	tDO   = 253
	tWONT = 252
	tWILL = 251
	tSB   = 250
	tSE   = 240

	optBinary = 0
	optEcho   = 1
	optSGA    = 3
	optTType  = 24
	optNAWS   = 31
)

type telnetConn struct {
	c        net.Conn
	term     string
	wmu      sync.Mutex
	mu       sync.Mutex
	cols     int
	rows     int
	naws     bool
	state    int
	cmd      byte
	sb       []byte
	raw      []byte
	telnet   bool
	addrInfo string
	us, him  [256]bool // options we said WILL to / DO to (asked or agreed)
}

// dialFunc opens a TCP connection (directly, via proxy or via jump host).
type dialFunc func(ctx context.Context, addr string) (net.Conn, string, error)

func dialTelnet(ctx context.Context, s session.Session, cols, rows int, dial dialFunc) (Conn, error) {
	if s.Hostname == "" {
		return nil, errors.New("no hostname")
	}
	port := s.Port
	if port == 0 {
		port = 23
	}
	addr := net.JoinHostPort(s.Hostname, strconv.Itoa(port))
	c, via, err := dial(ctx, addr)
	if err != nil {
		return nil, err
	}
	t := &telnetConn{c: c, term: termType(s), cols: cols, rows: rows, raw: make([]byte, 32*1024), telnet: true, addrInfo: "Telnet " + addr + via}
	t.start()
	return t, nil
}

// start opens the option negotiation the way PuTTY and SecureCRT do, instead
// of only answering the server: some servers (terminal and console servers
// among them) stay in line-at-a-time mode unless the client asks for
// character mode. They then hold what is typed - and the terminal's answers
// to the device's questions, such as its cursor position - until Enter, and
// the device prints those answers as text (^[[73;247R).
func (t *telnetConn) start() {
	t.mu.Lock()
	t.us[optNAWS], t.us[optTType], t.us[optSGA] = true, true, true
	t.him[optSGA], t.him[optEcho] = true, true
	t.mu.Unlock()
	t.send(tIAC, tWILL, optNAWS, tIAC, tWILL, optTType, tIAC, tDO, optEcho, tIAC, tWILL, optSGA, tIAC, tDO, optSGA)
}

func dialRaw(ctx context.Context, s session.Session, dial dialFunc) (Conn, error) {
	addr := net.JoinHostPort(s.Hostname, strconv.Itoa(s.Port))
	c, via, err := dial(ctx, addr)
	if err != nil {
		return nil, err
	}
	return &telnetConn{c: c, raw: make([]byte, 32*1024), addrInfo: "Raw TCP " + addr + via}, nil
}

func (t *telnetConn) Info() string { return t.addrInfo }
func (t *telnetConn) Close() error { return t.c.Close() }

// SendBreak sends the telnet BREAK command.
func (t *telnetConn) SendBreak() error {
	if !t.telnet {
		return errors.New("break is not supported on raw connections")
	}
	t.send(tIAC, 243)
	return nil
}

func (t *telnetConn) send(b ...byte) {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_, _ = t.c.Write(b)
}

// Write escapes IAC and maps CR to CR NUL as required by the NVT.
func (t *telnetConn) Write(p []byte) (int, error) {
	if !t.telnet {
		return t.c.Write(p)
	}
	out := make([]byte, 0, len(p)+8)
	for i, b := range p {
		out = append(out, b)
		if b == tIAC {
			out = append(out, tIAC)
		} else if b == '\r' && (i+1 >= len(p) || p[i+1] != '\n') {
			out = append(out, 0)
		}
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	if _, err := t.c.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *telnetConn) Resize(cols, rows int) error {
	t.mu.Lock()
	t.cols, t.rows = cols, rows
	naws := t.naws
	t.mu.Unlock()
	if naws {
		t.sendNAWS()
	}
	return nil
}

func (t *telnetConn) sendNAWS() {
	t.mu.Lock()
	w, h := t.cols, t.rows
	t.mu.Unlock()
	b := []byte{tIAC, tSB, optNAWS}
	for _, v := range []int{w >> 8, w & 0xff, h >> 8, h & 0xff} {
		b = append(b, byte(v))
		if byte(v) == tIAC {
			b = append(b, tIAC)
		}
	}
	b = append(b, tIAC, tSE)
	t.send(b...)
}

// Read returns application data with telnet negotiation removed.
func (t *telnetConn) Read(p []byte) (int, error) {
	if !t.telnet {
		return t.c.Read(p)
	}
	for {
		limit := len(p)
		if limit > len(t.raw) {
			limit = len(t.raw)
		}
		n, err := t.c.Read(t.raw[:limit])
		out := 0
		for _, b := range t.raw[:n] {
			if d, ok := t.process(b); ok {
				p[out] = d
				out++
			}
		}
		if out > 0 || err != nil {
			return out, err
		}
	}
}

// states: 0 data, 1 IAC, 2 option cmd, 3 SB, 4 SB IAC
func (t *telnetConn) process(b byte) (byte, bool) {
	switch t.state {
	case 0:
		if b == tIAC {
			t.state = 1
			return 0, false
		}
		return b, true
	case 1:
		switch b {
		case tIAC:
			t.state = 0
			return tIAC, true
		case tDO, tDONT, tWILL, tWONT:
			t.cmd = b
			t.state = 2
		case tSB:
			t.sb = t.sb[:0]
			t.state = 3
		default:
			t.state = 0
		}
		return 0, false
	case 2:
		t.negotiate(t.cmd, b)
		t.state = 0
		return 0, false
	case 3:
		if b == tIAC {
			t.state = 4
		} else if len(t.sb) < 1024 {
			t.sb = append(t.sb, b)
		}
		return 0, false
	case 4:
		if b == tSE {
			t.subneg()
			t.state = 0
		} else {
			if b == tIAC {
				t.sb = append(t.sb, tIAC)
			}
			t.state = 3
		}
		return 0, false
	}
	t.state = 0
	return 0, false
}

func (t *telnetConn) negotiate(cmd, opt byte) {
	// Each side answers a request only when it changes the option's state,
	// so the requests sent by start() are not answered again (no loops).
	t.mu.Lock()
	us, him := t.us[opt], t.him[opt]
	t.mu.Unlock()
	set := func(we bool, v bool) {
		t.mu.Lock()
		if we {
			t.us[opt] = v
		} else {
			t.him[opt] = v
		}
		t.mu.Unlock()
	}
	switch cmd {
	case tDO:
		switch opt {
		case optTType, optBinary, optSGA, optNAWS:
			if !us {
				set(true, true)
				t.send(tIAC, tWILL, opt)
			}
			if opt == optNAWS {
				t.mu.Lock()
				t.naws = true
				t.mu.Unlock()
				t.sendNAWS()
			}
		default:
			t.send(tIAC, tWONT, opt)
		}
	case tWILL:
		switch opt {
		case optEcho, optSGA, optBinary:
			if !him {
				set(false, true)
				t.send(tIAC, tDO, opt)
			}
		default:
			t.send(tIAC, tDONT, opt)
		}
	case tDONT:
		if opt == optNAWS {
			t.mu.Lock()
			t.naws = false
			t.mu.Unlock()
		}
		if us {
			set(true, false)
			t.send(tIAC, tWONT, opt)
		}
	case tWONT:
		if him {
			set(false, false)
			t.send(tIAC, tDONT, opt)
		}
	}
}

func (t *telnetConn) subneg() {
	if len(t.sb) >= 2 && t.sb[0] == optTType && t.sb[1] == 1 { // SEND
		b := []byte{tIAC, tSB, optTType, 0}
		b = append(b, []byte(t.term)...)
		b = append(b, tIAC, tSE)
		t.send(b...)
	}
}
