package localssh

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestLocalSSH(t *testing.T) {
	dir := t.TempDir()
	cl, err := Dial(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	// a command: in dir, its stdin, its output and exit status
	s, _ := cl.NewSession()
	s.Stdin = strings.NewReader("pwd; echo hi >&2; read x; echo got $x; exit 3\n")
	_ = s.Setenv("TW_X", "1")
	out, err := s.CombinedOutput("sh -s")
	var ee *ssh.ExitError
	if !errAs(err, &ee) || ee.ExitStatus() != 3 {
		t.Fatalf("exit: %v", err)
	}
	if got := string(out); !strings.Contains(got, "hi") || !strings.HasPrefix(strings.TrimSpace(got), mustEval(t, dir)) {
		t.Fatalf("output %q", got)
	}
	s2, _ := cl.NewSession()
	if out, err := s2.Output("echo ok"); err != nil || string(out) != "ok\n" {
		t.Fatalf("%q %v", out, err)
	}

	// files: relative to dir
	sc, err := sftp.NewClient(cl)
	if err != nil {
		t.Fatal(err)
	}
	f, err := sc.Create("a.bin")
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("x"), 300000)
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	f.Close()
	sc.Close()
	if b, err := os.ReadFile(filepath.Join(dir, "a.bin")); err != nil || !bytes.Equal(b, data) {
		t.Fatalf("sftp copy: %v", err)
	}

	// a tunnel from this computer
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_, _ = io.WriteString(c, "hello")
			c.Close()
		}
	}()
	c, err := cl.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(c); string(b) != "hello" {
		t.Fatalf("tunnel read %q", b)
	}
	if _, err := cl.Dial("tcp", "127.0.0.1:1"); err == nil {
		t.Fatal("a closed port must fail")
	}
	if _, _, err := cl.SendRequest("keepalive@termwright", true, nil); err != nil {
		t.Fatal(err)
	}
}

func mustEval(t *testing.T, d string) string {
	p, err := filepath.EvalSymlinks(d)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func errAs(err error, ee **ssh.ExitError) bool {
	e, ok := err.(*ssh.ExitError)
	*ee = e
	return ok
}
