package emu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Console is a VM's serial console (QEMU's telnet server, reached through the
// host): what it shows, and typing on it.
type Console struct {
	rw   io.ReadWriteCloser
	mu   sync.Mutex
	buf  strings.Builder // everything shown (cleaned)
	mark int             // what is matched already
	news chan struct{}
	err  error
	// OnText gets what the console shows, as it comes (for a live view).
	OnText func(string)
	// OnAnswer is told each question answered on the console.
	OnAnswer func(question, sent string)
	// Confirm answers yes/no questions with y (while typing day-0 commands:
	// the user asked for them, so their confirmations are answered).
	Confirm bool
}

// leave: a question whose yes would end the session or the switch's run (it takes no).
var leave = regexp.MustCompile(`(?i)\b(quit|exit|log ?out|logout|reboot|reload|shut ?down|power ?off)\b`)

func lastLineOf(s string) string {
	s = strings.TrimRight(s, " \t\r\n")
	if k := strings.LastIndexAny(s, "\r\n"); k >= 0 {
		return s[k+1:]
	}
	return s
}

// yesNo is a yes/no question waiting at the end of the console.
var yesNo = regexp.MustCompile(`(?i)[(\[]\s*(y/n|yes/no)\s*[)\]]\s*[:?]?\s*$`)

// rePrompt: how long a command may show no prompt before Enter is typed.
var rePrompt = 15 * time.Second

// pager is a "more" prompt waiting at the end of the console.
var pager = regexp.MustCompile(`(?i)(press\s+<?space>?\s+to\s+continue[^\n]*|--\s*more\s*--(?:\s*\(q\s*=\s*quit\))?)\s*$`)

func (c *Console) answered(q, sent string) {
	if c.OnAnswer != nil {
		q = strings.TrimSpace(q)
		if i := strings.LastIndex(q, "\n"); i >= 0 {
			q = strings.TrimSpace(q[i+1:])
		}
		c.OnAnswer(q, sent)
	}
}

// NewConsole reads the console in the background; telnet options are refused.
func NewConsole(rw io.ReadWriteCloser) *Console {
	c := &Console{rw: rw, news: make(chan struct{}, 1)}
	go c.read()
	return c
}

func (c *Console) read() {
	p := make([]byte, 4096)
	var iac []byte
	for {
		n, err := c.rw.Read(p)
		var text []byte
		for _, b := range p[:n] {
			switch {
			case len(iac) == 0 && b == 0xff:
				iac = []byte{b}
			case len(iac) == 1:
				if b == 0xff { // escaped 255
					text = append(text, b)
					iac = nil
				} else if b >= 251 && b <= 254 {
					iac = append(iac, b)
				} else {
					iac = nil
				}
			case len(iac) == 2:
				// DO x → WONT x, WILL x → DONT x (QEMU only wants to echo and suppress go-ahead, which is fine)
				switch iac[1] {
				case 253:
					_, _ = c.rw.Write([]byte{0xff, 252, b})
				case 251:
					if b != 1 && b != 3 {
						_, _ = c.rw.Write([]byte{0xff, 254, b})
					} else {
						_, _ = c.rw.Write([]byte{0xff, 253, b})
					}
				}
				iac = nil
			default:
				text = append(text, b)
			}
		}
		if len(text) > 0 {
			t := CleanConsole(string(text))
			c.mu.Lock()
			c.buf.WriteString(t)
			c.mu.Unlock()
			if c.OnText != nil {
				c.OnText(t)
			}
			select {
			case c.news <- struct{}{}:
			default:
			}
		}
		if err != nil {
			c.mu.Lock()
			c.err = err
			c.mu.Unlock()
			close(c.news)
			return
		}
	}
}

// Send types text and Enter.
func (c *Console) Send(text string) error {
	_, err := c.rw.Write([]byte(text + "\r"))
	return err
}

// Text is everything the console showed.
func (c *Console) Text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Close ends the console connection.
func (c *Console) Close() error { return c.rw.Close() }

// ErrTimeout: the console did not show what was waited for.
var ErrTimeout = errors.New("timed out")

// Wait waits until the new console text matches one of res; it returns which.
// Each answer whose text shows up meanwhile is typed once per appearance.
// builtinAnswers are questions any image may ask at the first login, after
// the image's own answers: "[y/N/q]" questionnaires quit with the defaults.
var builtinAnswers = []Answer{{Match: "[y/N/q]:", Send: "q"}}

func (c *Console) Wait(ctx context.Context, timeout time.Duration, answers []Answer, res ...*regexp.Regexp) (int, error) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	for {
		c.mu.Lock()
		all := c.buf.String()
		tail := all[c.mark:]
		err := c.err
		c.mu.Unlock()
		for i, re := range res {
			if loc := re.FindStringIndex(tail); loc != nil {
				c.mu.Lock()
				c.mark += loc[1]
				c.mu.Unlock()
				return i, nil
			}
		}
		// a question is answered only while it waits at the end of the
		// console: text that went by (a questionnaire of an earlier login,
		// a show command's output) is not answered again
		answeredOne := false
		for _, a := range append(answers[:len(answers):len(answers)], builtinAnswers...) {
			if a.Match != "" {
				if k := strings.LastIndex(tail, a.Match); k >= 0 && strings.Trim(tail[k+len(a.Match):], " \t\r\n:?>") == "" {
					c.mu.Lock()
					c.mark += k + len(a.Match)
					c.mu.Unlock()
					if err := c.Send(a.Send); err != nil {
						return -1, err
					}
					sent := a.Send
					if a.secret {
						sent = "the image's password"
					}
					c.answered(tail[:k+len(a.Match)], sent)
					tail, answeredOne = "", true
					break
				}
			}
		}
		if !answeredOne && tail != "" {
			var send string
			loc := pager.FindStringIndex(tail)
			if loc != nil {
				send = " " // the next page (no Enter)
			} else if loc = yesNo.FindStringIndex(tail); loc != nil {
				// day-0 commands are confirmed; any other question (at a
				// login: "save your configuration changes to primary.cfg?
				// (y/N)") takes its default, the capital letter
				send = "n"
				if c.Confirm {
					send = "y"
				} else if q := tail[loc[0]:loc[1]]; (strings.Contains(q, "Y/") || strings.Contains(q, "Yes/")) && !leave.MatchString(lastLineOf(tail)) {
					send = "y" // its default - but never to leave the session ("quit this shell session? (Y/n)")
				}
			}
			if loc != nil && send != "" {
				c.mu.Lock()
				c.mark += loc[1]
				c.mu.Unlock()
				var err error
				if send == " " {
					_, err = c.rw.Write([]byte(" "))
				} else {
					err = c.Send(send)
					c.answered(tail[:loc[1]], send)
				}
				if err != nil {
					return -1, err
				}
			}
		}
		if err != nil {
			return -1, err
		}
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-t.C:
			return -1, ErrTimeout
		case _, ok := <-c.news:
			if !ok {
				c.mu.Lock()
				err := c.err
				c.mu.Unlock()
				if err == nil {
					err = io.EOF
				}
				// look once more at what came last
				c.mu.Lock()
				tail := c.buf.String()[c.mark:]
				c.mu.Unlock()
				for i, re := range res {
					if re.MatchString(tail) {
						return i, nil
					}
				}
				return -1, err
			}
		}
	}
}

// DefaultPrompt matches a CLI prompt at the end of the output.
const DefaultPrompt = `[>#$%]\s*$`

// LastLine is the console's last line with text (a prompt waits there).
func (c *Console) LastLine() string {
	ls := strings.Split(strings.ReplaceAll(CleanConsole(c.Text()), "\r", "\n"), "\n")
	for i := len(ls) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(ls[i]); t != "" {
			return t
		}
	}
	return ""
}

// Tail is the end of what the console showed (n characters, one line).
func (c *Console) Tail(n int) string {
	t := strings.Join(strings.Fields(CleanConsole(c.Text())), " ")
	if len(t) > n {
		t = "…" + t[len(t)-n:]
	}
	return t
}

// lockOut: a switch that locks its console after failed logins ("Lock out for
// 60 seconds.").
var lockOut = regexp.MustCompile(`(?i)lock(?:ed)? ?out for (\d+) sec`)

// Login gets the console to the CLI prompt: logs in when it shows a login
// prompt. A refused login is tried again (a switch still starting up can
// refuse the first one); the error ends with what the console showed.
func Login(ctx context.Context, c *Console, im *Image) (*regexp.Regexp, error) {
	var err error
	for try := 0; try < 3; try++ {
		var p *regexp.Regexp
		if p, err = login1(ctx, c, im); err == nil {
			return p, nil
		}
		if !strings.Contains(err.Error(), "refused") || im.Password == "" && !im.NotReadyIn(c.LastLine()) {
			break // without a password it is refused again (unless its login was not ready)
		}
		wait := 10 * time.Second
		if m := lockOut.FindStringSubmatch(c.Tail(600)); m != nil { // too many attempts: the console is locked for a while
			n, _ := strconv.Atoi(m[1])
			wait = time.Duration(n+5) * time.Second
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, fmt.Errorf("%w - the console showed: %s", err, c.Tail(220))
}

// notReadyWait: how long a login that is not ready yet is left alone before Enter is typed again.
var notReadyWait = 10 * time.Second

func login1(ctx context.Context, c *Console, im *Image) (*regexp.Regexp, error) {
	prompt, err := regexp.Compile(orStr(im.Prompt, DefaultPrompt))
	if err != nil {
		return nil, err
	}
	login := regexp.MustCompile(`(?i)(login|username|user name)\s*:\s*$`)
	pass := regexp.MustCompile(`(?i)password\s*:\s*$`)
	_ = c.Send("")
	// logged in already (a console left at a prompt), or the login prompt; the
	// Enter that woke the console may have been taken as an empty user name
	i := -1
	for try := 0; try < 3 && i != 0 && i != 1; try++ {
		var err error
		i, err = c.Wait(ctx, 90*time.Second, im.answers(), login, prompt, pass)
		if err != nil {
			return nil, errors.New("no login prompt on the console: " + err.Error())
		}
		if i == 2 {
			_ = c.Send("")
		}
	}
	if i == 2 {
		return nil, errors.New("the console asks for a password but shows no login prompt")
	}
	// a login prompt that is not ready yet ("(pending-AAA) login:"): wait, then look again
	for try := 0; i == 0 && im.NotReadyIn(c.LastLine()) && try < 30; try++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(notReadyWait):
		}
		_ = c.Send("")
		var err error
		if i, err = c.Wait(ctx, 60*time.Second, im.answers(), login, prompt, pass); err != nil {
			return nil, errors.New("no login prompt on the console: " + err.Error())
		}
	}
	if i == 0 && im.NotReadyIn(c.LastLine()) {
		return nil, errors.New("its login did not get ready in 5 minutes (still " + c.Tail(80) + ")")
	}
	if i == 0 {
		_ = c.Send(orStr(im.Login, "admin"))
		j, err := c.Wait(ctx, 60*time.Second, im.answers(), pass, prompt, login)
		if err != nil {
			return nil, errors.New("no prompt after the login: " + err.Error())
		}
		if j == 2 {
			return nil, errors.New("the login was refused")
		}
		if j == 0 {
			_ = c.Send(im.Password)
			k, err := waitNudged(ctx, c, im, 2*time.Minute, prompt, login)
			if err != nil {
				return nil, errors.New("no prompt after the password: " + err.Error())
			}
			if k == 1 {
				if im.Password == "" {
					return nil, errors.New("the login was refused without a password: set the image's password (Images → the image → Login)")
				}
				return nil, errors.New("the login or password was refused")
			}
		}
	}
	return prompt, nil
}

// waitNudged waits like Wait, typing Enter when nothing matched for a while:
// log messages printed after a prompt (Fabric Engine's "ZTP+ discovery
// failed" right after the login) hide it from the end of the console, and
// Enter shows it again.
func waitNudged(ctx context.Context, c *Console, im *Image, total time.Duration, res ...*regexp.Regexp) (int, error) {
	var err error
	for end := time.Now().Add(total); ; {
		step := min(rePrompt, time.Until(end))
		if step <= 0 {
			return -1, err
		}
		var i int
		if i, err = c.Wait(ctx, step, im.answers(), res...); !errors.Is(err, ErrTimeout) {
			return i, err
		}
		_ = c.Send("")
	}
}

// Run types a command and returns what it printed (up to the next prompt).
func Run(ctx context.Context, c *Console, im *Image, prompt *regexp.Regexp, cmd string) (string, error) {
	c.mu.Lock()
	from := c.buf.Len()
	c.mu.Unlock()
	if err := c.Send(cmd); err != nil {
		return "", err
	}
	// a log message printed after the prompt hides it from the end of the
	// console: Enter shows it again (every 15 s, for 2 minutes)
	var err error
	for try := 0; try < int(2*time.Minute/rePrompt); try++ {
		if _, err = c.Wait(ctx, rePrompt, im.answers(), prompt); !errors.Is(err, ErrTimeout) {
			break
		}
		_ = c.Send("")
	}
	if err != nil {
		c.mu.Lock()
		q := strings.TrimSpace(c.buf.String()[c.mark:])
		c.mu.Unlock()
		if i := strings.LastIndex(q, "\n"); i >= 0 {
			q = strings.TrimSpace(q[i+1:])
		}
		if m := yesNo.FindString(q); m != "" {
			return "", fmt.Errorf("%s asks %q: add an answer to the image (Answers: %s => y or n)", cmd, q, strings.TrimSpace(m))
		}
		return "", errors.New("no prompt after " + cmd + ": " + err.Error())
	}
	c.mu.Lock()
	out := c.buf.String()[from:c.mark]
	c.mu.Unlock()
	return out, nil
}

// Day0 logs in on the console and types the image's first-boot commands for v
// (the VM's number n fills {n}); step is told each command as it is typed.
func Day0(ctx context.Context, c *Console, im *Image, v VM, n int, step func(i int, cmd string)) error {
	prompt, err := Login(ctx, c, im)
	if err != nil {
		return err
	}
	c.Confirm = true
	defer func() { c.Confirm = false }()
	for k, cmd := range im.Day0 {
		cmd = Fill(cmd, v, n)
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		if step != nil {
			step(k, cmd)
		}
		if _, err := Run(ctx, c, im, prompt, cmd); err != nil {
			return err
		}
	}
	return nil
}

var ipv4 = regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})(/\d{1,2})?\b`)

// FindAddress is the first usable IPv4 address in a command's output (not
// 0.0.0.0, loopback, link-local or a netmask), with its /bits when shown.
func FindAddress(out string) string {
	if i := strings.IndexAny(out, "\n"); i >= 0 {
		out = out[i+1:] // the echoed command
	}
	for _, m := range ipv4.FindAllStringSubmatch(out, -1) {
		ip, err := netip.ParseAddr(m[1])
		if err != nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || strings.HasPrefix(m[1], "255.") {
			continue
		}
		return m[1] + m[2]
	}
	return ""
}

// Ask logs in and returns what a command printed.
func Ask(ctx context.Context, c *Console, im *Image, cmd string) (string, error) {
	prompt, err := Login(ctx, c, im)
	if err != nil {
		return "", err
	}
	return Run(ctx, c, im, prompt, cmd)
}

// AskAddress logs in and reads the management address with the image's address command.
func AskAddress(ctx context.Context, c *Console, im *Image) (string, error) {
	if im.AddrCmd == "" {
		return "", errors.New("the image has no address command")
	}
	prompt, err := Login(ctx, c, im)
	if err != nil {
		return "", err
	}
	out, err := Run(ctx, c, im, prompt, im.AddrCmd)
	if err != nil {
		return "", err
	}
	if a := FindAddress(out); a != "" {
		return a, nil
	}
	return "", errors.New(im.AddrCmd + " shows no address yet")
}

func orStr(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
