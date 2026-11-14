package main

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/session"
)

// WebServerState is a device's own web server, as its device type reads it (Open Web Interface).
type WebServerState struct {
	Known  bool     `json:"known"`            // the device type says how to check and turn it on
	HTTP   bool     `json:"http"`             // HTTP is on
	HTTPS  bool     `json:"https"`            // HTTPS is on
	Output string   `json:"output,omitempty"` // what the check printed
	On     []string `json:"on,omitempty"`     // the commands that turn both on
	Save   []string `json:"save,omitempty"`   // the commands that keep it after a restart
}

// webDeviceType is the session's device type and its web server commands.
func (a *App) webDeviceType(s session.Session) (session.DeviceType, []string, string, string, []string, []string, bool) {
	if s.DeviceType == "" || a.terms.DeviceType == nil || (s.Protocol != session.ProtoSSH2 && s.Protocol != session.ProtoTelnet && s.Protocol != session.ProtoConsole) {
		return session.DeviceType{}, nil, "", "", nil, nil, false
	}
	dt, ok := a.terms.DeviceType(s.DeviceType)
	if !ok {
		return session.DeviceType{}, nil, "", "", nil, nil, false
	}
	check, http, https, on, save, ok := dt.Web()
	name := webName(orStr(s.Name, s.Hostname))
	for i := range on {
		on[i] = strings.ReplaceAll(on[i], "{name}", name)
	}
	return dt, check, http, https, on, save, ok
}

// webName makes a session's name usable in a device command (a certificate's common name): letters, digits, - and .
func webName(n string) string {
	n = regexp.MustCompile(`[^A-Za-z0-9.-]+`).ReplaceAllString(n, "-")
	n = strings.Trim(n, "-.")
	if n == "" {
		return "device"
	}
	if len(n) > 60 {
		n = n[:60]
	}
	return n
}

// webState reads the check's output with the device type's patterns.
func webState(out, httpRe, httpsRe string) (http, https bool) {
	if re, err := regexp.Compile(httpRe); err == nil {
		http = re.MatchString(out)
	}
	if re, err := regexp.Compile(httpsRe); err == nil {
		https = re.MatchString(out)
	}
	return http, https
}

// WebServerCheck logs in to the device in the background and reads whether its web server serves HTTP and HTTPS.
func (a *App) WebServerCheck(s session.Session) (WebServerState, error) {
	if err := a.ready(); err != nil {
		return WebServerState{}, err
	}
	s = a.withFolderDefaults(s)
	dt, check, http, https, on, save, ok := a.webDeviceType(s)
	if !ok {
		return WebServerState{}, nil
	}
	s2, err := a.consoleSession(s)
	if err != nil {
		return WebServerState{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	outs, err := a.terms.CaptureEach(ctx, s2, check, dt.Prompt)
	if err != nil {
		return WebServerState{Known: true, On: on, Save: save}, err
	}
	st := WebServerState{Known: true, On: on, Save: save, Output: strings.TrimSpace(strings.Join(outs, "\n"))}
	st.HTTP, st.HTTPS = webState(st.Output, http, https)
	return st, nil
}

// WebServerEnable turns the device's web server on for HTTP and HTTPS with its device type's commands (and saves
// the configuration when save is set), then checks again. The user approved the commands in the dialog.
func (a *App) WebServerEnable(s session.Session, save bool) (WebServerState, error) {
	if err := a.ready(); err != nil {
		return WebServerState{}, err
	}
	s = a.withFolderDefaults(s)
	dt, _, _, _, on, saveCmds, ok := a.webDeviceType(s)
	if !ok {
		return WebServerState{}, errors.New("this device type does not say how to turn on its web server (Global Options → Device Types)")
	}
	s2, err := a.consoleSession(s)
	if err != nil {
		return WebServerState{}, err
	}
	cmds := append([]string{}, on...)
	if save {
		cmds = append(cmds, saveCmds...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute) // a certificate takes a while on some devices
	outs, err := a.terms.CaptureYes(ctx, s2, cmds, dt.Prompt)
	cancel()
	if err != nil {
		return WebServerState{Known: true}, err
	}
	if bad := webCommandError(outs); bad != "" {
		st, _ := a.WebServerCheck(s)
		return st, errors.New("the device said: " + bad)
	}
	return a.WebServerCheck(s)
}

// webCommandError is the first error a device printed for a command ("Error: …", "% Invalid input …").
func webCommandError(outs []string) string {
	re := regexp.MustCompile(`(?im)^\s*(%+\s*\S.*|error:.*|invalid input.*)$`)
	for _, o := range outs {
		if m := re.FindString(o); m != "" {
			return strings.TrimSpace(m)
		}
	}
	return ""
}
