package term

import "testing"

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
