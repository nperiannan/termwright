package session

import (
	"regexp"
	"testing"
)

func TestHostnameFacts(t *testing.T) {
	cases := map[string]string{
		"VOSS":      "SysName           : VSP-8284XSQ\nModelName         : 8284XSQ\n",
		"EXOS":      "SysName:          lab-x465-1\nSysLocation:\nSystem Type:      X465-48T\n",
		"CISCO IOS": "core-rtr1 uptime is 2 weeks, 3 days\nCisco IOS Software, Version 15.2(4)E10\n",
		"LINUX":     "lab-desktop\n",
	}
	want := map[string]string{"VOSS": "VSP-8284XSQ", "EXOS": "lab-x465-1", "CISCO IOS": "core-rtr1", "LINUX": "lab-desktop"}
	for name, out := range cases {
		var got string
		for _, r := range BuiltinFacts(name) {
			if r.Name == "Hostname" {
				if m := regexp.MustCompile(`(?m)` + r.Regex).FindStringSubmatch(out); m != nil {
					got = m[1]
				}
			}
		}
		if got != want[name] {
			t.Errorf("%s: hostname %q, want %q", name, got, want[name])
		}
	}
	// the echoed command or a shell prompt is not a hostname
	for _, r := range BuiltinFacts("LINUX") {
		if r.Name == "Hostname" && regexp.MustCompile(`(?m)`+r.Regex).MatchString("hostname -s\nops@lab:~$ \n") {
			t.Error("linux hostname rule matches the command or the prompt")
		}
	}
}
