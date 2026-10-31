package term

import (
	"testing"

	"github.com/termwright/termwright/internal/session"
)

func TestBasicLogin(t *testing.T) {
	l := newBasicLogin(true, "")
	if out := l.feed([]byte("Welcome to the ALTUSEN SN0108/SN0116\r\nCopyright (c) 2004-2007 ATEN\r\nLogin: ")); len(out) != 1 || out[0].Send != `\s` {
		t.Fatalf("Login: not answered: %+v", out)
	}
	if out := l.feed([]byte("administrator\r\nPassword: ")); len(out) != 1 || out[0].Send != `\w` || !l.done() {
		t.Fatalf("Password: not answered: %+v", out)
	}

	l = newBasicLogin(true, "")
	if out := l.feed([]byte("Unauthorized login: is prohibited\r\n")); len(out) != 0 {
		t.Fatal("a banner mentioning login: was answered")
	}
	if out := l.feed([]byte("User Name:")); len(out) != 1 {
		t.Fatal("User Name: not answered")
	}

	l = newBasicLogin(true, "")
	if out := l.feed([]byte("\r\nUser Access Verification\r\n\r\nPassword: ")); len(out) != 1 || out[0].Send != `\w` || !l.done() {
		t.Fatalf("password-only prompt not answered: %+v", out)
	}

	l = newBasicLogin(false, "")
	if out := l.feed([]byte("username: ")); len(out) != 1 || !l.done() {
		t.Fatal("username only not answered")
	}
}

func TestRealActionsDropsEmptyRows(t *testing.T) {
	if got := realActions([]session.LogonAction{{}, {Expect: "login:", Send: `\s`}, {}}); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	l := newBasicLogin(false, "")
	if out := l.feed([]byte("login: \x00")); len(out) != 1 {
		t.Fatal("prompt followed by NUL not answered")
	}
}

// A console that is still logged in: the user types "exit"; the login prompt
// that follows is not answered.
func TestBasicLoginStandsDownWhenTyping(t *testing.T) {
	l := newBasicLogin(true, "")
	if l.typed([]byte("\r")) || l.typed([]byte("\x1b[75;247R")) {
		t.Fatal("Enter or a terminal answer counted as typing")
	}
	if !l.typed([]byte("e")) || !l.done() {
		t.Fatal("typing did not stop the basic login")
	}
	if out := l.feed([]byte("exit\r\n\r\nlogin: ")); len(out) != 0 {
		t.Fatal("login answered after the user typed")
	}
	// once logging in has started, typing does not stop it
	l = newBasicLogin(true, "")
	l.feed([]byte("login: "))
	if l.typed([]byte("x")) {
		t.Fatal("stopped after the username was sent")
	}
}
