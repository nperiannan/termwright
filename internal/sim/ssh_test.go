package sim

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func demoLab(t *testing.T, password string) *Lab {
	t.Helper()
	p, err := LoadPackFile("testdata/demo")
	if err != nil {
		t.Fatal(err)
	}
	specs, links, err := Plan(p, []Group{{Model: "D100-24T-4X", Count: 2, Names: "sw{n}"}}, "10.99.0.0/24", CableChain, password, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	lab := NewLab(p, specs, links)
	_, key, _ := NewHostKey()
	if err := lab.Serve(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lab.Stop)
	return lab
}

type term struct {
	in  io.WriteCloser
	mu  sync.Mutex
	buf strings.Builder
	ch  chan struct{}
}

func (tm *term) text() string { tm.mu.Lock(); defer tm.mu.Unlock(); return tm.buf.String() }

func shell(t *testing.T, lab *Lab, id, pw string) *term {
	t.Helper()
	c, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", lab.Device(id).Port), &ssh.ClientConfig{User: "admin", Auth: []ssh.AuthMethod{ssh.Password(pw)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	s, _ := c.NewSession()
	_ = s.RequestPty("xterm", 40, 120, nil)
	in, _ := s.StdinPipe()
	out, _ := s.StdoutPipe()
	if err := s.Shell(); err != nil {
		t.Fatal(err)
	}
	tm := &term{in: in, ch: make(chan struct{}, 1024)}
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := out.Read(b)
			if n > 0 {
				tm.mu.Lock()
				tm.buf.Write(b[:n])
				tm.mu.Unlock()
				tm.ch <- struct{}{}
			}
			if err != nil {
				close(tm.ch)
				return
			}
		}
	}()
	tm.wait(t, 0, `[>#] ?$`)
	return tm
}

func (tm *term) wait(t *testing.T, start int, re string) string {
	t.Helper()
	rx := regexp.MustCompile(re)
	deadline := time.After(3 * time.Second)
	for {
		out := strings.ReplaceAll(tm.text()[start:], "\r", "")
		if rx.MatchString(out) {
			return out
		}
		select {
		case <-tm.ch:
		case <-deadline:
			t.Fatalf("no %q:\n%s", re, out)
		}
	}
}

func (tm *term) send(t *testing.T, keys, re string) string {
	t.Helper()
	start := len(tm.text())
	_, _ = io.WriteString(tm.in, keys)
	return tm.wait(t, start, re)
}

func TestSSHLoginPagingHelp(t *testing.T) {
	lab := demoLab(t, "lab")
	if _, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", lab.Device(lab.SortedIDs()[0]).Port), &ssh.ClientConfig{User: "admin", Auth: []ssh.AuthMethod{ssh.Password("bad")}, HostKeyCallback: ssh.InsecureIgnoreHostKey()}); err == nil {
		t.Fatal("wrong password taken")
	}
	id := lab.SortedIDs()[0]
	tm := shell(t, lab, id, "lab")
	if !strings.Contains(tm.text(), "Demo OS 1.2.3") {
		t.Fatalf("banner: %q", tm.text())
	}
	tm.send(t, "enable\r", `sw1#$`)
	// a long output stops after a screen; space shows more, q stops
	out := tm.send(t, "show long\r", `-- more --$`)
	if !strings.Contains(out, "line 38\n") || strings.Contains(out, "line 39\n") {
		t.Fatalf("first page:\n%s", out)
	}
	out = tm.send(t, " ", `sw1#$`)
	if !strings.Contains(out, "line 59") {
		t.Fatalf("second page:\n%s", out)
	}
	tm.send(t, "show long\r", `-- more --$`)
	if out := tm.send(t, "q", `sw1#$`); strings.Contains(out, "line 40") {
		t.Fatalf("q did not stop:\n%s", out)
	}
	tm.send(t, "paging off\r", `sw1#$`)
	if out := tm.send(t, "show long\r", `sw1#$`); !strings.Contains(out, "line 59") {
		t.Fatalf("no paging:\n%s", out)
	}
	// Tab completes, ? lists
	tm.send(t, "show sy\t", `show system $`)
	tm.send(t, "\x15", `$`)
	if out := tm.send(t, "show ?", `show $`); !strings.Contains(out, "system") || !strings.Contains(out, "serial") {
		t.Fatalf("help:\n%s", out)
	}
	tm.send(t, "\x15\r", `sw1#$`)
	// exec without a shell
	c, _ := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", lab.Device(id).Port), &ssh.ClientConfig{User: "admin", Auth: []ssh.AuthMethod{ssh.Password("lab")}, HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	defer c.Close()
	s, _ := c.NewSession()
	b, err := s.Output("show system")
	if err != nil || !strings.Contains(string(b), "Name    : sw1") {
		t.Fatalf("exec %v %s", err, b)
	}
	if lab.Logins()[id] < 1 {
		t.Fatal("logins")
	}
}

func TestLayoutFromModelName(t *testing.T) {
	p, _ := LoadPackFile("testdata/demo")
	m, _ := p.ModelOf("D100-24T-4X")
	if g := p.Layout(m); len(g) != 2 || g[0].Count != 24 || g[0].Type != "T" || g[1].Count != 4 || g[1].Type != "X" {
		t.Fatalf("layout %+v", g)
	}
	if u := p.Uplinks(m); len(u) != 4 || u[0] != 25 {
		t.Fatalf("uplinks %v", u)
	}
	if d := p.Describe(m); d != "24 1G copper, 4 10G fiber" {
		t.Fatalf("describe %q", d)
	}
	for i := 0; i < 20; i++ {
		if s := Generate("AR{R:01-14}{YY}{WW}Q-4{R:0000-0199}"); !regexp.MustCompile(`^AR\d{6}Q-40[01]\d\d$`).MatchString(s) {
			t.Fatalf("generate %s", s)
		}
	}
}
