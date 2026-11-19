// Package syslog is Termwright's syslog server and client: it receives RFC 3164 and RFC 5424 messages over
// UDP, TCP (newline or octet-counted framing, RFC 6587) and TLS (RFC 5425), keeps the latest in memory,
// can write them to daily files, and sends test messages. Standard library only.
package syslog

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Severities and facilities by number.
var (
	Severities = []string{"emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"}
	Facilities = []string{"kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "ntp", "audit", "alert", "clock",
		"local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"}
)

// Message is one syslog message received.
type Message struct {
	// ID: increases with every message (to ask for the newer ones).
	ID uint64 `json:"id"`
	// At: when it was received.
	At time.Time `json:"at"`
	// From: the sender's address.
	From string `json:"from"`
	// Proto: udp, tcp or tls.
	Proto string `json:"proto"`
	// Facility: 0-23 (16-23 are local0-local7).
	Facility int `json:"facility"`
	// FacilityName: kern, user, … local7.
	FacilityName string `json:"facilityName"`
	// Severity: 0 emerg, 1 alert, 2 crit, 3 err, 4 warning, 5 notice, 6 info, 7 debug.
	Severity int `json:"severity"`
	// SeverityName: emerg … debug.
	SeverityName string `json:"severityName"`
	// Time the device put in it ("" when none or unreadable).
	Time string `json:"time,omitempty"`
	// Host the device names itself.
	Host string `json:"host,omitempty"`
	// App: the program (TAG / APP-NAME).
	App string `json:"app,omitempty"`
	// ProcID: the process id.
	ProcID string `json:"procId,omitempty"`
	// MsgID: the message type (RFC 5424).
	MsgID string `json:"msgId,omitempty"`
	// SD: structured data (RFC 5424) as written.
	SD string `json:"sd,omitempty"`
	// Text: the message itself.
	Text string `json:"text"`
	// Format: 3164, 5424 or raw.
	Format string `json:"format"`
	// Raw: the message as received.
	Raw string `json:"raw"`
}

var (
	rfc5424 = regexp.MustCompile(`^(\d{1,2}) (\S+) (\S+) (\S+) (\S+) (\S+) (-|(?:\[.*?[^\\]\])+)(?: (.*))?$`)
	bsdTime = regexp.MustCompile(`^(?:\*|\.)?([A-Z][a-z]{2} [ \d]\d(?: \d{4})? \d{2}:\d{2}:\d{2}(?:\.\d+)?(?: [A-Z]{3,4})?):? `)
	isoTime = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?) `)
	seqNo   = regexp.MustCompile(`^\d+: `)
	tagRe   = regexp.MustCompile(`^([A-Za-z0-9_.\-/]{1,48})(?:\[(\d+)\])?: ?`)
)

// Parse reads one message (RFC 5424, RFC 3164 or what devices send in between).
func Parse(raw string) Message {
	raw = strings.TrimRight(raw, "\r\n\x00")
	m := Message{Raw: raw, Facility: 1, Severity: 5, Format: "raw", Text: raw}
	s := raw
	if strings.HasPrefix(s, "<") {
		if end := strings.IndexByte(s, '>'); end > 1 && end <= 4 {
			if pri, err := strconv.Atoi(s[1:end]); err == nil && pri >= 0 && pri <= 191 {
				m.Facility, m.Severity = pri/8, pri%8
				s = s[end+1:]
			}
		}
	}
	m.names()
	if g := rfc5424.FindStringSubmatch(s); g != nil && g[1] != "0" {
		m.Format = "5424"
		nil5 := func(x string) string {
			if x == "-" {
				return ""
			}
			return x
		}
		m.Time, m.Host, m.App, m.ProcID, m.MsgID, m.SD = nil5(g[2]), nil5(g[3]), nil5(g[4]), nil5(g[5]), nil5(g[6]), nil5(g[7])
		m.Text = strings.TrimPrefix(g[8], "\ufeff")
		return m
	}
	m.Format = "3164"
	s = seqNo.ReplaceAllString(s, "") // Cisco's sequence number
	if g := bsdTime.FindStringSubmatch(s); g != nil {
		m.Time = strings.TrimSpace(g[1])
		s = s[len(g[0]):]
	} else if g := isoTime.FindStringSubmatch(s); g != nil {
		m.Time = g[1]
		s = s[len(g[0]):]
	}
	// HOSTNAME then TAG: when the first word is followed by a tag ("host app[12]: text")
	if sp := strings.IndexByte(s, ' '); sp > 0 && m.Time != "" {
		first, rest := s[:sp], s[sp+1:]
		if !strings.HasSuffix(first, ":") && !strings.HasPrefix(first, "%") && (tagRe.MatchString(rest) || strings.HasPrefix(rest, "%")) {
			m.Host, s = first, rest
		}
	}
	if g := tagRe.FindStringSubmatch(s); g != nil && !strings.HasPrefix(s, "%") && strings.Contains(g[0], ":") {
		m.App, m.ProcID = g[1], g[2]
		s = s[len(g[0]):]
	}
	if m.App == "" && strings.HasPrefix(s, "%") { // %LINK-3-UPDOWN: …
		if i := strings.Index(s, ":"); i > 1 && i < 64 {
			m.App = strings.Trim(s[1:i], " ")
		}
	}
	m.Text = s
	return m
}

func (m *Message) names() {
	if m.Facility >= 0 && m.Facility < len(Facilities) {
		m.FacilityName = Facilities[m.Facility]
	}
	if m.Severity >= 0 && m.Severity < len(Severities) {
		m.SeverityName = Severities[m.Severity]
	}
}

// SeverityOf reads "err", "error", "warning", "3"; -1 when unknown.
func SeverityOf(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 7 {
		return n
	}
	switch s {
	case "error":
		return 3
	case "warn":
		return 4
	case "emergency", "panic":
		return 0
	case "critical":
		return 2
	case "informational":
		return 6
	}
	for i, n := range Severities {
		if n == s {
			return i
		}
	}
	return -1
}

// FacilityOf reads "local7", "23"; -1 when unknown.
func FacilityOf(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 23 {
		return n
	}
	for i, n := range Facilities {
		if n == s {
			return i
		}
	}
	return -1
}

// ---- server ----

// Config of the server; a port of 0 does not listen on that transport.
type Config struct {
	Bind string
	UDP  int
	TCP  int
	TLS  int
	// Cert, Key: the TLS certificate (PEM files); none: a self-signed one is made.
	Cert, Key string
	// Keep: how many messages are kept in memory (default 50000).
	Keep int
}

// Server receives syslog messages.
type Server struct {
	cfg     Config
	mu      sync.Mutex
	msgs    []Message
	next    uint64
	counts  [8]int
	udp     net.PacketConn
	lns     []net.Listener
	conns   map[net.Conn]bool
	closed  bool
	onMsg   func(Message)
	wg      sync.WaitGroup
	Started time.Time
}

// Listen starts the server on the transports with a port. onMsg is called for every message (from the
// receiving goroutines; keep it short).
func Listen(cfg Config, onMsg func(Message)) (*Server, error) {
	if cfg.Keep <= 0 {
		cfg.Keep = 50000
	}
	if cfg.UDP == 0 && cfg.TCP == 0 && cfg.TLS == 0 {
		return nil, errors.New("no port to listen on")
	}
	s := &Server{cfg: cfg, onMsg: onMsg, conns: map[net.Conn]bool{}, Started: time.Now()}
	if cfg.UDP > 0 {
		pc, err := net.ListenPacket("udp", net.JoinHostPort(cfg.Bind, strconv.Itoa(cfg.UDP)))
		if err != nil {
			return nil, portErr("UDP", cfg.UDP, err)
		}
		s.udp = pc
		s.wg.Add(1)
		go s.udpLoop()
	}
	if cfg.TCP > 0 {
		ln, err := net.Listen("tcp", net.JoinHostPort(cfg.Bind, strconv.Itoa(cfg.TCP)))
		if err != nil {
			s.Close()
			return nil, portErr("TCP", cfg.TCP, err)
		}
		s.lns = append(s.lns, ln)
		s.wg.Add(1)
		go s.acceptLoop(ln, "tcp")
	}
	if cfg.TLS > 0 {
		tc, err := tlsConfig(cfg.Cert, cfg.Key)
		if err != nil {
			s.Close()
			return nil, err
		}
		ln, err := tls.Listen("tcp", net.JoinHostPort(cfg.Bind, strconv.Itoa(cfg.TLS)), tc)
		if err != nil {
			s.Close()
			return nil, portErr("TLS", cfg.TLS, err)
		}
		s.lns = append(s.lns, ln)
		s.wg.Add(1)
		go s.acceptLoop(ln, "tls")
	}
	return s, nil
}

func portErr(what string, port int, err error) error {
	if port < 1024 && strings.Contains(err.Error(), "permission") {
		return fmt.Errorf("%s port %d needs administrator rights on this computer; use a port above 1023 (for example %d) and point the devices there", what, port, port+1000)
	}
	if strings.Contains(err.Error(), "address already in use") {
		return fmt.Errorf("%s port %d is used by another program (another syslog server?)", what, port)
	}
	return fmt.Errorf("%s port %d: %w", what, port, err)
}

// Addrs are the addresses it listens on ("udp 0.0.0.0:514").
func (s *Server) Addrs() []string {
	var out []string
	if s.udp != nil {
		out = append(out, "udp "+s.udp.LocalAddr().String())
	}
	for i, ln := range s.lns {
		kind := "tcp"
		if i == len(s.lns)-1 && s.cfg.TLS > 0 {
			kind = "tls"
		}
		out = append(out, kind+" "+ln.Addr().String())
	}
	return out
}

// Port of a transport as listening (the real port when 0 … was asked for tests).
func (s *Server) Port(kind string) int {
	var a net.Addr
	switch kind {
	case "udp":
		if s.udp != nil {
			a = s.udp.LocalAddr()
		}
	default:
		for i, ln := range s.lns {
			isTLS := i == len(s.lns)-1 && s.cfg.TLS > 0
			if (kind == "tls") == isTLS {
				a = ln.Addr()
			}
		}
	}
	if a == nil {
		return 0
	}
	_, p, _ := net.SplitHostPort(a.String())
	n, _ := strconv.Atoi(p)
	return n
}

func (s *Server) udpLoop() {
	defer s.wg.Done()
	buf := make([]byte, 65536)
	for {
		n, addr, err := s.udp.ReadFrom(buf)
		if err != nil {
			return
		}
		s.add(string(buf[:n]), hostOf(addr), "udp")
	}
}

func hostOf(a net.Addr) string {
	h, _, err := net.SplitHostPort(a.String())
	if err != nil {
		return a.String()
	}
	return h
}

func (s *Server) acceptLoop(ln net.Listener, kind string) {
	defer s.wg.Done()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			c.Close()
			return
		}
		s.conns[c] = true
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { s.mu.Lock(); delete(s.conns, c); s.mu.Unlock(); c.Close() }()
			s.stream(c, kind)
		}()
	}
}

// stream reads a TCP / TLS stream: octet-counted ("123 <34>…") or one message per line.
func (s *Server) stream(c net.Conn, kind string) {
	from := hostOf(c.RemoteAddr())
	r := bufio.NewReaderSize(c, 64<<10)
	for {
		_ = c.SetReadDeadline(time.Now().Add(30 * time.Minute))
		b, err := r.Peek(1)
		if err != nil {
			return
		}
		if b[0] >= '1' && b[0] <= '9' { // octet counting
			ls, err := r.ReadString(' ')
			if err != nil {
				return
			}
			n, err := strconv.Atoi(strings.TrimSpace(ls))
			if err != nil || n <= 0 || n > 1<<20 {
				return
			}
			msg := make([]byte, n)
			if _, err := io.ReadFull(r, msg); err != nil {
				return
			}
			s.add(string(msg), from, kind)
			continue
		}
		line, err := r.ReadString('\n')
		if t := strings.TrimRight(line, "\r\n\x00"); t != "" {
			s.add(t, from, kind)
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) add(raw, from, proto string) {
	m := Parse(raw)
	m.At, m.From, m.Proto = time.Now(), from, proto
	s.mu.Lock()
	s.next++
	m.ID = s.next
	s.msgs = append(s.msgs, m)
	if over := len(s.msgs) - s.cfg.Keep; over > 0 {
		s.msgs = append([]Message(nil), s.msgs[over+s.cfg.Keep/10:]...) // drop a tenth at once
	}
	if m.Severity >= 0 && m.Severity < 8 {
		s.counts[m.Severity]++
	}
	s.mu.Unlock()
	if s.onMsg != nil {
		s.onMsg(m)
	}
}

// Inject adds a message as if received (imports, tests).
func (s *Server) Inject(raw, from, proto string) { s.add(raw, from, proto) }

// Counts per severity since it started.
func (s *Server) Counts() [8]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts
}

// Clear forgets the messages kept (the counts too).
func (s *Server) Clear() {
	s.mu.Lock()
	s.msgs, s.counts = nil, [8]int{}
	s.mu.Unlock()
}

// Query filters the messages.
type Query struct {
	// After: only messages with a larger ID.
	After uint64 `json:"after,omitempty"`
	// Text: words that must all be in the message, host or app (case-insensitive); /regex/ for a regular expression.
	Text string `json:"text,omitempty"`
	// Host: the sender's address or host name.
	Host string `json:"host,omitempty"`
	// MaxSeverity: this severity or worse (0 emerg … 7 debug; -1 or empty all).
	MaxSeverity *int `json:"maxSeverity,omitempty"`
	// Since: received at or after.
	Since time.Time `json:"since,omitempty"`
	// Limit: at most this many, the newest (default 1000).
	Limit int `json:"limit,omitempty"`
}

// Matcher is a compiled query text (shared with alert rules).
type Matcher struct {
	re    *regexp.Regexp
	words []string
}

// NewMatcher reads "words" or "/regex/".
func NewMatcher(text string) (Matcher, error) {
	t := strings.TrimSpace(text)
	if len(t) >= 2 && strings.HasPrefix(t, "/") && strings.HasSuffix(t, "/") {
		re, err := regexp.Compile("(?i)" + t[1:len(t)-1])
		return Matcher{re: re}, err
	}
	return Matcher{words: strings.Fields(strings.ToLower(t))}, nil
}

// Match tells whether a message matches.
func (mt Matcher) Match(m Message) bool {
	hay := m.Host + " " + m.From + " " + m.App + " " + m.Text
	if mt.re != nil {
		return mt.re.MatchString(hay)
	}
	l := strings.ToLower(hay)
	for _, w := range mt.words {
		if !strings.Contains(l, w) {
			return false
		}
	}
	return true
}

// Messages are the ones that match, oldest first (the newest Limit).
func (s *Server) Messages(q Query) ([]Message, error) {
	mt, err := NewMatcher(q.Text)
	if err != nil {
		return nil, fmt.Errorf("the search is not a regular expression: %w", err)
	}
	lim := q.Limit
	if lim <= 0 {
		lim = 1000
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Message
	for i := len(s.msgs) - 1; i >= 0 && len(out) < lim; i-- {
		m := s.msgs[i]
		if m.ID <= q.After {
			break
		}
		if q.MaxSeverity != nil && *q.MaxSeverity >= 0 && m.Severity > *q.MaxSeverity {
			continue
		}
		if q.Host != "" && !strings.EqualFold(m.From, q.Host) && !strings.EqualFold(m.Host, q.Host) {
			continue
		}
		if !q.Since.IsZero() && m.At.Before(q.Since) {
			continue
		}
		if !mt.Match(m) {
			continue
		}
		out = append(out, m)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// Hosts are the senders seen, with their message counts.
func (s *Server) Hosts() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	for _, m := range s.msgs {
		out[m.From]++
	}
	return out
}

// Close stops listening.
func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	if s.udp != nil {
		s.udp.Close()
	}
	for _, ln := range s.lns {
		ln.Close()
	}
	s.wg.Wait()
}

// ---- client ----

// Out is a message to send.
type Out struct {
	// Target: host:port (port 514 / 6514 when left out).
	Target string
	// Proto: udp (default), tcp or tls.
	Proto string
	// Format: 5424 (default) or 3164.
	Format   string
	Facility int
	Severity int
	Host     string
	App      string
	Text     string
	// Insecure: TLS without checking the server's certificate.
	Insecure bool
}

// Format makes the message's text as sent.
func (o Out) Format5424or3164(now time.Time) string {
	pri := o.Facility*8 + o.Severity
	host, app := o.Host, o.App
	if host == "" {
		host, _ = osHostname()
	}
	if app == "" {
		app = "termwright"
	}
	if o.Format == "3164" {
		return fmt.Sprintf("<%d>%s %s %s: %s", pri, now.Format(time.Stamp), host, app, o.Text)
	}
	return fmt.Sprintf("<%d>1 %s %s %s - - - %s", pri, now.Format(time.RFC3339Nano), host, app, o.Text)
}

// Send sends one message.
func Send(o Out) (string, error) {
	proto := strings.ToLower(o.Proto)
	if proto == "" {
		proto = "udp"
	}
	target := o.Target
	if _, _, err := net.SplitHostPort(target); err != nil {
		port := "514"
		if proto == "tls" {
			port = "6514"
		}
		target = net.JoinHostPort(strings.Trim(target, "[]"), port)
	}
	if o.Severity < 0 || o.Severity > 7 || o.Facility < 0 || o.Facility > 23 {
		return "", errors.New("severity 0-7, facility 0-23")
	}
	msg := o.Format5424or3164(time.Now())
	var c net.Conn
	var err error
	d := net.Dialer{Timeout: 5 * time.Second}
	switch proto {
	case "udp":
		c, err = d.Dial("udp", target)
	case "tcp":
		c, err = d.Dial("tcp", target)
	case "tls":
		c, err = tls.DialWithDialer(&d, "tcp", target, &tls.Config{InsecureSkipVerify: o.Insecure}) //nolint:gosec // the user's choice
	default:
		return "", fmt.Errorf("unknown transport %q (udp, tcp, tls)", o.Proto)
	}
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if proto == "udp" {
		_, err = c.Write([]byte(msg))
	} else {
		_, err = fmt.Fprintf(c, "%d %s", len(msg), msg)
	}
	return msg, err
}
