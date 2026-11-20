package app

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/syslog"
)

// The syslog server: receives, names the session of the sender, writes the day's file, rules hide and
// colour, the tool reads it back, and it stops.
func TestSyslogServerToolRulesFiles(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := session.NewStore(dir + "/Sessions")
	_, _ = st.Save(session.Session{Name: "lo", Folder: "Lab", Protocol: session.ProtoSSH2, Hostname: "127.0.0.1"}, "")
	a := &App{cfg: cfg, store: st}
	defer a.SyslogStop()
	pc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()
	if _, err := a.SyslogStart(SyslogSettings{Bind: "127.0.0.1", UDP: port, SaveFiles: true, PerHost: true}); err != nil {
		t.Fatal(err)
	}
	if err := a.SyslogSetRules([]SyslogRule{{Name: "noise", Match: "keepalive", MaxSeverity: -1, Hide: true}, {Name: "links", Match: "/link (up|down)/", MaxSeverity: 4, Color: "red", Notify: true}}); err != nil {
		t.Fatal(err)
	}
	target := "127.0.0.1:" + strconv.Itoa(port)
	for _, m := range []SyslogSendIn{{Target: target, Facility: "local7", Severity: "err", App: "PORT", Host: "core-sw1", Text: "1/1 link down"},
		{Target: target, Severity: "debug", Text: "keepalive"}, {Target: target, Severity: "info", Format: "3164", Text: "user admin logged in", Count: 2}} {
		if _, err := a.SyslogSend(m); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for a.SyslogState().Total < 4 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	l, err := a.SyslogMessages(syslog.Query{})
	if err != nil || len(l) != 3 { // keepalive hidden
		t.Fatalf("%v %+v", err, l)
	}
	if l[0].Session != "Lab/lo" || l[0].Rule != "links" || l[0].Color != "red" || l[0].Severity != 3 || l[0].Host != "core-sw1" {
		t.Fatalf("%+v", l[0])
	}
	out, err := a.netToolRun(nil, "syslog", map[string]any{"action": "messages", "severity": "warning"}, nil)
	if err != nil || !strings.Contains(out, "local7.err PORT: 1/1 link down") || strings.Contains(out, "logged in") {
		t.Fatalf("%v\n%s", err, out)
	}
	out, _ = a.netToolRun(nil, "syslog", map[string]any{"action": "status"}, nil)
	if !strings.Contains(out, "Listening on udp 127.0.0.1:") || !strings.Contains(out, "err 1") {
		t.Fatal(out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "syslog", "logs", "127.0.0.1", time.Now().Format("2006-01-02")+".log"))
	if err != nil || strings.Count(string(b), "\n") != 4 || !strings.Contains(string(b), "local7.err") {
		t.Fatalf("%v %s", err, b)
	}
	a.SyslogStop()
	if a.SyslogState().On {
		t.Fatal("still on")
	}
	if l, _ := a.SyslogMessages(syslog.Query{}); len(l) != 3 {
		t.Fatal("messages lost when stopped")
	}
	if err := a.SyslogSetRules([]SyslogRule{{Name: "bad", Match: "/(/"}}); err == nil {
		t.Fatal("bad regex saved")
	}
	if s := a.SyslogSettingsGet(); s.UDP != port || !s.SaveFiles {
		t.Fatalf("%+v", s)
	}
}
