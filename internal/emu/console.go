package emu

import (
	"context"
	"errors"
	"io"
	"regexp"
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
		for _, a := range answers {
			if a.Match != "" {
				if k := strings.Index(tail, a.Match); k >= 0 {
					c.mu.Lock()
					c.mark += k + len(a.Match)
					c.mu.Unlock()
					if err := c.Send(a.Send); err != nil {
						return -1, err
					}
					tail = ""
					break
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

// Day0 logs in on the console and types the image's first-boot commands for v
// (the VM's number n fills {n}); step is told each command as it is typed.
func Day0(ctx context.Context, c *Console, im *Image, v VM, n int, step func(i int, cmd string)) error {
	prompt, err := regexp.Compile(orStr(im.Prompt, DefaultPrompt))
	if err != nil {
		return err
	}
	login := regexp.MustCompile(`(?i)(login|username|user name)\s*:\s*$`)
	pass := regexp.MustCompile(`(?i)password\s*:\s*$`)
	_ = c.Send("")
	// logged in already (a console left at a prompt), or the login prompt; the
	// Enter that woke the console may have been taken as an empty user name
	i := -1
	for try := 0; try < 3 && i != 0 && i != 1; try++ {
		var err error
		i, err = c.Wait(ctx, 90*time.Second, im.Answers, login, prompt, pass)
		if err != nil {
			return errors.New("no login prompt on the console: " + err.Error())
		}
		if i == 2 {
			_ = c.Send("")
		}
	}
	if i == 2 {
		return errors.New("the console asks for a password but shows no login prompt")
	}
	if i == 0 {
		_ = c.Send(orStr(im.Login, "admin"))
		j, err := c.Wait(ctx, 60*time.Second, im.Answers, pass, prompt, login)
		if err != nil {
			return errors.New("no prompt after the login: " + err.Error())
		}
		if j == 2 {
			return errors.New("the login was refused")
		}
		if j == 0 {
			_ = c.Send(im.Password)
			k, err := c.Wait(ctx, 60*time.Second, im.Answers, prompt, login)
			if err != nil {
				return errors.New("no prompt after the password: " + err.Error())
			}
			if k == 1 {
				return errors.New("the login or password was refused")
			}
		}
	}
	for k, cmd := range im.Day0 {
		cmd = Fill(cmd, v, n)
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		if step != nil {
			step(k, cmd)
		}
		if err := c.Send(cmd); err != nil {
			return err
		}
		if _, err := c.Wait(ctx, 120*time.Second, im.Answers, prompt); err != nil {
			return errors.New("no prompt after " + cmd + ": " + err.Error())
		}
	}
	return nil
}

func orStr(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
