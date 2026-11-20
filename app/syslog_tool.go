package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/syslog"
)

// syslogTool is the "syslog" network tool (AI assistant, MCP, POST /tools/syslog).
func (a *App) syslogTool(args map[string]any) (string, error) {
	switch strings.ToLower(argStr(args, "action")) {
	case "status", "":
		return syslogStatusText(a.SyslogState()), nil
	case "start":
		s := a.SyslogSettingsGet()
		for k, p := range map[string]*int{"udp": &s.UDP, "tcp": &s.TCP, "tls": &s.TLS} {
			if _, ok := args[k]; ok {
				*p = argInt(args, k, 0, 65535)
			}
		}
		st, err := a.SyslogStart(s)
		if err != nil {
			return "", err
		}
		return syslogStatusText(st), nil
	case "stop":
		a.SyslogStop()
		return "The syslog server stopped (the messages are kept until cleared).", nil
	case "clear":
		a.SyslogClear()
		return "Cleared.", nil
	case "send":
		msg, err := a.SyslogSend(SyslogSendIn{Target: argStr(args, "target"), Proto: argStr(args, "proto"), Facility: argStr(args, "facility"), Severity: argStr(args, "severity"), Text: argStr(args, "text")})
		if err != nil {
			return "", err
		}
		return "Sent: " + msg, nil
	case "messages":
		q := syslog.Query{Text: argStr(args, "query"), Host: argStr(args, "host"), Limit: argInt(args, "limit", 200, 5000)}
		if sv := argStr(args, "severity"); sv != "" {
			n := syslog.SeverityOf(sv)
			if n < 0 {
				return "", fmt.Errorf("unknown severity %q", sv)
			}
			q.MaxSeverity = &n
		}
		if m := argInt(args, "minutes", 0, 60*24*30); m > 0 {
			q.Since = time.Now().Add(-time.Duration(m) * time.Minute)
		}
		list, err := a.SyslogMessages(q)
		if err != nil {
			return "", err
		}
		st := a.SyslogState()
		if len(list) == 0 {
			if !st.On && st.Total == 0 {
				return "The syslog server is not running (syslog start) and has no messages.", nil
			}
			return "No messages match.", nil
		}
		var b strings.Builder
		for _, m := range list {
			who := m.From
			if m.Session != "" {
				who += " (" + m.Session + ")"
			}
			fmt.Fprintf(&b, "%s %s %s.%s %s%s\n", m.At.Format(time.DateTime), who, m.FacilityName, m.SeverityName, map[bool]string{true: m.App + ": "}[m.App != "" && !strings.HasPrefix(m.Text, "%")], clip(m.Text, 500))
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("unknown action %q", argStr(args, "action"))
}

func syslogStatusText(st SyslogState) string {
	var b strings.Builder
	if st.On {
		fmt.Fprintf(&b, "Listening on %s since %s.\n", strings.Join(st.Addrs, ", "), st.Since)
	} else {
		b.WriteString("Not listening.")
		if st.Error != "" {
			b.WriteString(" " + st.Error)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%d messages:", st.Total)
	for i, n := range st.Counts {
		if n > 0 {
			fmt.Fprintf(&b, " %s %d", syslog.Severities[i], n)
		}
	}
	b.WriteString("\n")
	for h, n := range st.Hosts {
		fmt.Fprintf(&b, "  %s: %d\n", h, n)
	}
	if st.Folder != "" {
		b.WriteString("Written to " + st.Folder + "\n")
	}
	return b.String()
}
