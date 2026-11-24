package term

import (
	"testing"

	"github.com/termwright/termwright/internal/session"
)

func TestDeviceLogonFlex(t *testing.T) {
	steps := []session.LogonAction{{Expect: "login:", Send: `\s`, TrailingCR: true}, {Expect: "password:", Send: `\w`, Hide: true, TrailingCR: true}, {Expect: "#", Send: "disable clipaging", TrailingCR: true}}
	sends := func(l *logonRunner, chunks ...string) (out []string) {
		for _, c := range chunks {
			for _, a := range l.feed([]byte(c)) {
				out = append(out, a.Send)
			}
		}
		return
	}
	// other words and case for the prompts
	if got := sends(newDeviceLogon(steps, 2), "Welcome\r\nUsername: ", "\r\nPassword: ", "\r\nsw1 # "); len(got) != 3 || got[2] != "disable clipaging" {
		t.Fatalf("%q", got)
	}
	// a device that asks only for a password
	if got := sends(newDeviceLogon(steps, 2), "\r\nPassword: ", "\r\nsw1 # "); len(got) != 2 || got[0] != `\w` {
		t.Fatalf("%q", got)
	}
	// without flex (SSH): as before, the exact prompt only
	if got := sends(newDeviceLogon(steps, 0), "Username: "); len(got) != 0 {
		t.Fatalf("%q", got)
	}
}
