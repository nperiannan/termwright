package term

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/logging"
	"github.com/termwright/termwright/internal/session"
)

// Prompts routes questions to the UI and waits for answers.
type Prompts struct {
	mu      sync.Mutex
	emit    Emitter
	pending map[string]*pending
	quiet   func(tabID string) bool // background connections that must not ask
}

type pending struct {
	tab string
	ch  chan PromptReply
}

// NewPrompts creates a prompt broker.
func NewPrompts(emit Emitter) *Prompts {
	return &Prompts{emit: emit, pending: map[string]*pending{}}
}

// Ask shows a prompt and blocks until it is answered or ctx is done.
func (p *Prompts) Ask(ctx context.Context, pr Prompt) (PromptReply, error) {
	if p.quiet != nil && p.quiet(pr.TabID) {
		what := strings.TrimSpace(pr.Title + ": " + pr.Message)
		return PromptReply{}, fmt.Errorf("%w (%s): connect once yourself to save the password or accept the host key", ErrNeedsInput, strings.Trim(what, ": "))
	}
	pr.ID = newID()
	ch := make(chan PromptReply, 1)
	p.mu.Lock()
	p.pending[pr.ID] = &pending{tab: pr.TabID, ch: ch}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.pending, pr.ID)
		p.mu.Unlock()
		p.emit.Emit("prompt:done", pr.ID)
	}()
	p.emit.Emit("prompt", pr)
	select {
	case r := <-ch:
		if !r.OK {
			return r, ErrCancelled
		}
		return r, nil
	case <-ctx.Done():
		return PromptReply{}, ErrCancelled
	}
}

// Answer delivers the UI's reply.
func (p *Prompts) Answer(id string, r PromptReply) {
	p.mu.Lock()
	pd := p.pending[id]
	p.mu.Unlock()
	if pd != nil {
		select {
		case pd.ch <- r:
		default:
		}
	}
}

// CancelTab cancels all prompts belonging to a tab.
func (p *Prompts) CancelTab(tab string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, pd := range p.pending {
		if pd.tab == tab {
			select {
			case pd.ch <- PromptReply{}:
			default:
			}
		}
	}
}

// logonRunner implements expect/send automation after connect.
type logonRunner struct {
	mu      sync.Mutex
	actions []session.LogonAction
	idx     int
	window  string
	strip   logging.Stripper
	basic   bool // Expect is a list of prompts ("a|b"), matched in any case, only as the last thing on screen
}

// Prompts the basic login answers: a session with a username but no device
// type and no logon actions still logs in at the usual Telnet and console prompts.
const (
	basicUserPrompts = "login:|username:|user name:|user:"
	basicPassPrompts = "password:"
)

// newBasicLogin answers a username prompt with the session's username and,
// when withPassword, the password prompt that follows with its saved password.
// credential is the account whose login is used ("" = the session's own).
func newBasicLogin(withPassword bool, credential string) *logonRunner {
	a := []session.LogonAction{{Expect: basicUserPrompts, Send: `\s`, TrailingCR: true, Credential: credential}}
	if withPassword {
		a = append(a, session.LogonAction{Expect: basicPassPrompts, Send: `\w`, Hide: true, TrailingCR: true, Credential: credential})
	}
	return &logonRunner{actions: a, basic: true}
}

// basicMatch finds one of the "|"-separated prompts, in any case, followed by
// nothing but spaces: a banner that mentions "login:" is not a prompt.
func basicMatch(window, prompts string) (int, int) {
	low := strings.ToLower(window)
	rest := strings.TrimRight(low, " \t")
	for _, p := range strings.Split(prompts, "|") {
		if strings.HasSuffix(rest, p) {
			return len(rest) - len(p), len(p)
		}
	}
	return -1, 0
}

func newLogonRunner(a []session.LogonAction) *logonRunner {
	return &logonRunner{actions: a}
}

func (l *logonRunner) done() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.idx >= len(l.actions)
}

// feed consumes output and returns the actions whose Expect text has arrived
// (actions with an empty Expect run immediately, in order).
func (l *logonRunner) feed(p []byte) []session.LogonAction {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.window += string(l.strip.Strip(p))
	if len(l.window) > 4096 {
		l.window = l.window[len(l.window)-4096:]
	}
	var out []session.LogonAction
	for l.idx < len(l.actions) {
		a := l.actions[l.idx]
		if l.basic {
			if l.idx == 0 && len(l.actions) > 1 {
				if i, n := basicMatch(l.window, basicPassPrompts); i >= 0 { // asks only for a password
					l.window = l.window[i+n:]
					l.idx = 1
					out = append(out, l.actions[1])
					l.idx++
					continue
				}
			}
			i, n := basicMatch(l.window, a.Expect)
			if i < 0 {
				break
			}
			l.window = l.window[i+n:]
		} else if a.Expect != "" {
			exp := Unescape(a.Expect)
			i := strings.Index(l.window, exp)
			if i < 0 {
				break
			}
			l.window = l.window[i+len(exp):]
		}
		out = append(out, a)
		l.idx++
	}
	return out
}

// sendPart is a piece of logon output: text, or a one-second pause.
type sendPart struct {
	text  string
	pause bool
}

// expandSend turns a logon Send string into parts, resolving \v (clipboard),
// \p (pause), \s and \w (credential user / password) and the usual escapes.
func expandSend(send string, trailingCR bool, user, pass string, clip func() string) []sendPart {
	var parts []sendPart
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			parts = append(parts, sendPart{text: b.String()})
			b.Reset()
		}
	}
	for i := 0; i < len(send); i++ {
		c := send[i]
		if c != '\\' || i+1 >= len(send) {
			b.WriteByte(c)
			continue
		}
		i++
		switch send[i] {
		case 'r':
			b.WriteByte('\r')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'b':
			b.WriteByte('\b')
		case 'e':
			b.WriteByte(0x1b)
		case '\\':
			b.WriteByte('\\')
		case 's':
			b.WriteString(user)
		case 'w':
			b.WriteString(pass)
		case 'v':
			if clip != nil {
				b.WriteString(clip())
			}
		case 'p':
			flush()
			parts = append(parts, sendPart{pause: true})
		case 'x':
			if i+2 < len(send) {
				if v, err := strconv.ParseUint(send[i+1:i+3], 16, 8); err == nil {
					b.WriteByte(byte(v))
					i += 2
					continue
				}
			}
			b.WriteString(`\x`)
		default:
			b.WriteByte('\\')
			b.WriteByte(send[i])
		}
	}
	if trailingCR {
		b.WriteByte('\r')
	}
	flush()
	return parts
}

// triggerRunner sends text whenever output matches a pattern.
type triggerRunner struct {
	mu       sync.Mutex
	triggers []session.Trigger
	res      []*regexp.Regexp
	window   string
	strip    logging.Stripper
	fired    []time.Time
}

func newTriggerRunner(ts []session.Trigger) (*triggerRunner, error) {
	tr := &triggerRunner{triggers: ts, res: make([]*regexp.Regexp, len(ts))}
	for i, t := range ts {
		if t.Pattern == "" {
			continue
		}
		if t.Regex {
			re, err := regexp.Compile(t.Pattern)
			if err != nil {
				return nil, fmt.Errorf("%q: %w", t.Pattern, err)
			}
			tr.res[i] = re
		} else {
			tr.res[i] = regexp.MustCompile(regexp.QuoteMeta(Unescape(t.Pattern)))
		}
	}
	return tr, nil
}

// feed consumes output and returns text to send for every match. To avoid
// runaway loops at most 20 triggers fire per 10 seconds.
func (tr *triggerRunner) feed(p []byte) []byte {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.window += string(tr.strip.Strip(p))
	if len(tr.window) > 4096 {
		tr.window = tr.window[len(tr.window)-4096:]
	}
	var out []byte
	for {
		best, end := -1, -1
		for i, re := range tr.res {
			if re == nil {
				continue
			}
			if loc := re.FindStringIndex(tr.window); loc != nil && (end < 0 || loc[1] < end) {
				best, end = i, loc[1]
			}
		}
		if best < 0 {
			break
		}
		tr.window = tr.window[end:]
		now := time.Now()
		recent := tr.fired[:0]
		for _, f := range tr.fired {
			if now.Sub(f) < 10*time.Second {
				recent = append(recent, f)
			}
		}
		tr.fired = recent
		if len(tr.fired) >= 20 {
			continue
		}
		tr.fired = append(tr.fired, now)
		out = append(out, Unescape(tr.triggers[best].Send)...)
	}
	return out
}

// Unescape expands \r \n \t \e \b \\ and \xHH sequences.
func Unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'r':
				b.WriteByte('\r')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'e':
				b.WriteByte(0x1b)
			case 'b':
				b.WriteByte('\b')
			case '\\':
				b.WriteByte('\\')
			case 'x':
				if i+2 < len(s) {
					if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
						b.WriteByte(byte(v))
						i += 2
						continue
					}
				}
				b.WriteString(`\x`)
			default:
				b.WriteByte('\\')
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func userHome() (string, error) { return os.UserHomeDir() }
