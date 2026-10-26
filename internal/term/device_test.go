package term

import (
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/session"
)

func TestDeviceTypeAndAlerts(t *testing.T) {
	addr := startSSHServer(t, "pw")
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	autoAnswer(m, em, "pw")
	defer m.CloseAll()
	m.DeviceType = func(name string) (session.DeviceType, bool) {
		if name != "TestOS" {
			return session.DeviceType{}, false
		}
		return session.DeviceType{Name: "TestOS", Username: "admin",
			LoginSteps: []session.LogonAction{{Expect: "login:", Send: `\s`, TrailingCR: true}}, // telnet only
			AfterLogin: []session.LogonAction{{Expect: "router>", Send: "enable", TrailingCR: true},
				{Expect: "echo:enable", Send: "terminal length 0", TrailingCR: true}}}, true
	}
	alerts := make(chan Alert, 4)
	em.onEvent = func(name string, d any) {
		if name == "alert" {
			alerts <- d.(Alert)
		}
	}
	m.SetAlerts([]AlertRule{{Pattern: "critical", Sound: true}})
	s := sshSession(addr)
	s.Username = "" // filled from the device type
	s.DeviceType = "TestOS"
	ti := m.Create(s)
	m.Start(ti.ID, 80, 24)
	waitFor(t, func() bool { return strings.Contains(em.output(), "echo:terminal length 0") })
	m.Write(ti.ID, []byte("CRITICAL fan failure\r\n"))
	select {
	case a := <-alerts:
		if a.TabID != ti.ID || !strings.Contains(a.Line, "CRITICAL fan failure") {
			t.Fatalf("%+v", a)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no alert; output:\n%s", em.output())
	}
	m.Write(ti.ID, []byte("critical again\r\n"))
	select {
	case a := <-alerts:
		t.Fatalf("alert not rate-limited: %+v", a)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestDeviceTypeSteps(t *testing.T) {
	d := session.BuiltinDeviceTypes()[1] // VOSS
	if got := d.Steps(session.ProtoSSH2); len(got) != 2 || got[0].Send != "enable" {
		t.Fatalf("ssh steps %+v", got)
	}
	d.Credential = "lab"
	got := d.Steps(session.ProtoTelnet)
	if len(got) != 4 || got[0].Credential != "lab" || got[2].Credential != "" {
		t.Fatalf("telnet steps %+v", got)
	}
}

func TestDeviceTypeLoginPassword(t *testing.T) {
	addr := startSSHServer(t, "")
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	defer m.CloseAll()
	asked := make(chan string, 4)
	em.onPrompt = func(p Prompt) {
		if p.Kind == "hostkey" {
			m.Prompts().Answer(p.ID, PromptReply{OK: true, Choice: "Accept Once"})
			return
		}
		asked <- p.Title
		m.Prompts().Answer(p.ID, PromptReply{OK: false})
	}
	m.DeviceType = func(name string) (session.DeviceType, bool) {
		return session.DeviceType{Name: "EXOS", Username: "admin"}, name == "EXOS"
	}
	m.DevicePassword = func(name string) (string, bool) { return "", name == "EXOS" } // saved empty password
	s := sshSession(addr)
	s.Username, s.DeviceType = "someone-else", "EXOS" // the device type's login wins (OwnLogin is false)
	ti := m.Create(s)
	m.Start(ti.ID, 80, 24)
	waitFor(t, func() bool { return strings.Contains(em.output(), "router>") })
	select {
	case title := <-asked:
		t.Fatalf("unexpected prompt %q", title)
	default:
	}
}

// A password typed at a logon prompt can be remembered for the device type;
// an empty one means the device has no password.
func TestAskLogonRememberDeviceType(t *testing.T) {
	em := &recEmitter{}
	m := NewManager(em, t.TempDir(), func() string { return t.TempDir() })
	defer m.CloseAll()
	var savedName, savedPw string
	calls := 0
	m.OnDevicePasswordTyped = func(name, pw string) { savedName, savedPw, calls = name, pw, calls+1 }
	var label string
	em.onPrompt = func(p Prompt) {
		label = p.RememberLabel
		m.Prompts().Answer(p.ID, PromptReply{OK: true, Values: []string{""}, Remember: true})
	}
	s := session.Defaults()
	s.Hostname, s.DeviceType = "192.0.2.10", "EXOS"
	if pw := m.askLogon(&Tab{id: "x", sess: s}, "Password", true, "admin"); pw != "" {
		t.Fatalf("password %q", pw)
	}
	if calls != 1 || savedName != "EXOS" || savedPw != "" || !strings.Contains(label, "EXOS") {
		t.Fatalf("saved %q %q (%d), label %q", savedName, savedPw, calls, label)
	}
	// own login: not saved for the device type
	s.OwnLogin = true
	m.askLogon(&Tab{id: "y", sess: s}, "Password", true, "admin")
	if calls != 1 {
		t.Fatal("own login saved for the device type")
	}
}
