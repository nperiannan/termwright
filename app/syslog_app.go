package app

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/syslog"
)

// The syslog server (a free alternative to Kiwi): receives from devices over UDP, TCP and TLS while
// Termwright runs, matches senders to saved sessions, highlights and alerts with rules, writes daily
// files, and sends test messages.

// SyslogSettings of the server (kept in <data>/syslog/settings.json).
type SyslogSettings struct {
	// UDP port (514; 0 = off). Ports below 1024 need administrator rights on macOS and Linux.
	UDP int `json:"udp"`
	// TCP port (0 = off; 514 or 601 usually).
	TCP int `json:"tcp"`
	// TLS port (0 = off; 6514 usually). A self-signed certificate is made unless Cert and Key are given.
	TLS int `json:"tls"`
	// Cert: TLS certificate (PEM file).
	Cert string `json:"cert,omitempty"`
	// Key: its private key (PEM file).
	Key string `json:"key,omitempty"`
	// Bind: the address to listen on ("" all).
	Bind string `json:"bind,omitempty"`
	// SaveFiles: write every message to a file per day.
	SaveFiles bool `json:"saveFiles,omitempty"`
	// PerHost: one folder per sender.
	PerHost bool `json:"perHost,omitempty"`
	// Folder for the files ("" = the syslog folder in Termwright's data folder).
	Folder string `json:"folder,omitempty"`
	// AutoStart: start receiving when Termwright starts.
	AutoStart bool `json:"autoStart,omitempty"`
	// Keep: messages kept in memory (default 50000).
	Keep int `json:"keep,omitempty"`
}

// SyslogRule highlights or alerts on messages.
type SyslogRule struct {
	// Name shown with the messages it matches.
	Name string `json:"name"`
	// Match: words that must all be in the message, or /regex/ ("" = any).
	Match string `json:"match,omitempty"`
	// MaxSeverity: this severity or worse (0 emerg … 7 debug; -1 any).
	MaxSeverity int `json:"maxSeverity"`
	// Host: only from this sender ("" any).
	Host string `json:"host,omitempty"`
	// Color of the matching lines (red, amber, green, blue, purple; "" none).
	Color string `json:"color,omitempty"`
	// Notify: a notification in Termwright for each one.
	Notify bool `json:"notify,omitempty"`
	// Hide: not shown (noise).
	Hide bool `json:"hide,omitempty"`
	// Off: kept but not used.
	Off bool `json:"off,omitempty"`
}

// SyslogState is whether the server listens and what it got.
type SyslogState struct {
	// On: it listens.
	On bool `json:"on"`
	// Addrs it listens on ("udp 0.0.0.0:514").
	Addrs []string `json:"addrs"`
	// Error: why it could not start.
	Error string `json:"error,omitempty"`
	// Counts of messages per severity since it started (emerg … debug).
	Counts [8]int `json:"counts"`
	// Total messages kept.
	Total int `json:"total"`
	// Hosts: senders and their message counts.
	Hosts map[string]int `json:"hosts"`
	// Settings in use.
	Settings SyslogSettings `json:"settings"`
	// Folder the files are written to (when SaveFiles).
	Folder string `json:"folder,omitempty"`
	// Since: when it started.
	Since string `json:"since,omitempty"`
}

// SyslogView is a message with the session it came from and the rule that matched.
type SyslogView struct {
	syslog.Message
	// Session: the saved session at the sender's address.
	Session string `json:"session,omitempty"`
	// Rule: the first rule that matched.
	Rule string `json:"rule,omitempty"`
	// Color of that rule.
	Color string `json:"color,omitempty"`
}

var (
	slMu      sync.Mutex
	slSrv     *syslog.Server
	slSet     SyslogSettings
	slErr     string
	slRules   []SyslogRule
	slMatch   []syslog.Matcher
	slFiles   = map[string]*os.File{}
	slHosts   = map[string]string{}
	slHostsAt time.Time
	slPending int
	slLastID  uint64
)

func (a *App) syslogDir() string { return filepath.Join(a.cfg.DataDir(), "syslog") }

func (a *App) syslogLoad() {
	if b, err := os.ReadFile(filepath.Join(a.syslogDir(), "settings.json")); err == nil {
		_ = json.Unmarshal(b, &slSet)
	} else {
		slSet = SyslogSettings{UDP: 514}
	}
	if b, err := os.ReadFile(filepath.Join(a.syslogDir(), "rules.json")); err == nil {
		_ = json.Unmarshal(b, &slRules)
	}
	slCompile()
}

func slCompile() {
	slMatch = make([]syslog.Matcher, len(slRules))
	for i, r := range slRules {
		slMatch[i], _ = syslog.NewMatcher(r.Match)
	}
}

// SyslogSettingsGet are the saved settings.
func (a *App) SyslogSettingsGet() SyslogSettings {
	slMu.Lock()
	defer slMu.Unlock()
	if slSet == (SyslogSettings{}) {
		a.syslogLoad()
	}
	return slSet
}

// SyslogStart starts (or restarts) the server with these settings and saves them.
func (a *App) SyslogStart(s SyslogSettings) (SyslogState, error) {
	slMu.Lock()
	if slSrv != nil {
		srv := slSrv
		slSrv = nil
		slMu.Unlock()
		srv.Close()
		slMu.Lock()
	}
	if slRules == nil {
		a.syslogLoad()
	}
	slSet, slErr = s, ""
	_ = os.MkdirAll(a.syslogDir(), 0o700)
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(filepath.Join(a.syslogDir(), "settings.json"), b, 0o600)
	srv, err := syslog.Listen(syslog.Config{Bind: s.Bind, UDP: s.UDP, TCP: s.TCP, TLS: s.TLS, Cert: s.Cert, Key: s.Key, Keep: s.Keep}, a.syslogGot)
	if err != nil {
		slErr = err.Error()
		slMu.Unlock()
		return a.SyslogState(), err
	}
	slSrv = srv
	slMu.Unlock()
	return a.SyslogState(), nil
}

// SyslogStop stops listening (the messages stay until cleared or it starts again).
func (a *App) SyslogStop() {
	slMu.Lock()
	srv := slSrv
	slSrv = nil
	for k, f := range slFiles {
		f.Close()
		delete(slFiles, k)
	}
	if srv != nil {
		slStopped = srv
	}
	slMu.Unlock()
	if srv != nil {
		srv.Close()
	}
}

// slStopped keeps the messages of a stopped server readable.
var slStopped *syslog.Server

func slCur() *syslog.Server {
	if slSrv != nil {
		return slSrv
	}
	return slStopped
}

// SyslogState is whether the server listens and what it got.
func (a *App) SyslogState() SyslogState {
	slMu.Lock()
	defer slMu.Unlock()
	st := SyslogState{Settings: slSet, Error: slErr, Hosts: map[string]int{}, Addrs: []string{}}
	if slSrv != nil {
		st.On, st.Addrs, st.Since = true, slSrv.Addrs(), slSrv.Started.Format(time.RFC3339)
	}
	if s := slCur(); s != nil {
		st.Counts, st.Hosts = s.Counts(), s.Hosts()
		for _, n := range st.Hosts {
			st.Total += n
		}
	}
	if slSet.SaveFiles {
		st.Folder = a.syslogFolder()
	}
	return st
}

func (a *App) syslogFolder() string {
	if slSet.Folder != "" {
		return slSet.Folder
	}
	return filepath.Join(a.syslogDir(), "logs")
}

// syslogGot is called for every message: the file, the alert rules, a batched event.
func (a *App) syslogGot(m syslog.Message) {
	slMu.Lock()
	if slSet.SaveFiles {
		a.syslogWrite(m)
	}
	var alert *SyslogRule
	for i, r := range slRules {
		if !r.Off && r.Notify && slRuleMatch(r, slMatch[i], m) {
			alert = &slRules[i]
			break
		}
	}
	slPending++
	first := slPending == 1
	slMu.Unlock()
	if a.ctx == nil {
		return
	}
	if alert != nil {
		wruntime.EventsEmit(a.ctx, "syslog:alert", map[string]any{"rule": alert.Name, "from": m.From, "host": m.Host, "severity": m.SeverityName, "text": clip(m.Text, 300)})
	}
	if first { // one event per 300 ms however many arrive
		time.AfterFunc(300*time.Millisecond, func() {
			slMu.Lock()
			n := slPending
			slPending = 0
			slMu.Unlock()
			wruntime.EventsEmit(a.ctx, "syslog:msgs", n)
		})
	}
}

var fileSafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// syslogWrite appends a line to the day's file (slMu held).
func (a *App) syslogWrite(m syslog.Message) {
	dir := a.syslogFolder()
	if slSet.PerHost {
		dir = filepath.Join(dir, fileSafe.ReplaceAllString(m.From, "_"))
	}
	p := filepath.Join(dir, m.At.Format("2006-01-02")+".log")
	f := slFiles[p]
	if f == nil {
		for k, of := range slFiles { // a new day: the old files are closed
			if filepath.Dir(k) == dir {
				of.Close()
				delete(slFiles, k)
			}
		}
		_ = os.MkdirAll(dir, 0o700)
		var err error
		if f, err = os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err != nil {
			slErr = "could not write " + p + ": " + err.Error()
			return
		}
		slFiles[p] = f
	}
	fmt.Fprintf(f, "%s %s %s.%s %s\n", m.At.Format(time.RFC3339), m.From, m.FacilityName, m.SeverityName, strings.ReplaceAll(m.Raw, "\n", " "))
}

func slRuleMatch(r SyslogRule, mt syslog.Matcher, m syslog.Message) bool {
	if r.MaxSeverity >= 0 && m.Severity > r.MaxSeverity {
		return false
	}
	if r.Host != "" && !strings.EqualFold(r.Host, m.From) && !strings.EqualFold(r.Host, m.Host) {
		return false
	}
	return mt.Match(m)
}

// sessionByAddr finds the saved session of a sender (the list is read again every minute).
func (a *App) sessionByAddr(addr string) string {
	if a.store == nil {
		return ""
	}
	if time.Since(slHostsAt) > time.Minute {
		slHosts = map[string]string{}
		paths, _ := a.store.AllSessions(nil)
		for _, p := range paths {
			if s, err := a.store.Get(p); err == nil {
				for _, h := range []string{s.Hostname, s.SNMPHost} {
					if h != "" && slHosts[h] == "" {
						slHosts[h] = p
					}
				}
			}
		}
		slHostsAt = time.Now()
	}
	return slHosts[addr]
}

// SyslogMessages are the messages that match, oldest first; hidden ones (rules) are left out.
func (a *App) SyslogMessages(q syslog.Query) ([]SyslogView, error) {
	slMu.Lock()
	s := slCur()
	rules, ms := slRules, slMatch
	slMu.Unlock()
	out := []SyslogView{}
	if s == nil {
		return out, nil
	}
	list, err := s.Messages(q)
	if err != nil {
		return nil, err
	}
	slMu.Lock()
	defer slMu.Unlock()
	for _, m := range list {
		v := SyslogView{Message: m, Session: a.sessionByAddr(m.From)}
		if v.Session == "" && m.Host != "" {
			v.Session = a.sessionByAddr(m.Host)
		}
		hide := false
		for i, r := range rules {
			if !r.Off && slRuleMatch(r, ms[i], m) {
				if r.Hide {
					hide = true
				}
				v.Rule, v.Color = r.Name, r.Color
				break
			}
		}
		if !hide {
			out = append(out, v)
		}
	}
	return out, nil
}

// SyslogClear forgets the messages received.
func (a *App) SyslogClear() {
	slMu.Lock()
	defer slMu.Unlock()
	if s := slCur(); s != nil {
		s.Clear()
	}
}

// SyslogRules are the highlight and alert rules.
func (a *App) SyslogRules() []SyslogRule {
	slMu.Lock()
	defer slMu.Unlock()
	if slRules == nil {
		a.syslogLoad()
	}
	return append([]SyslogRule{}, slRules...)
}

// SyslogSetRules saves the rules (first match wins).
func (a *App) SyslogSetRules(rules []SyslogRule) error {
	for _, r := range rules {
		if _, err := syslog.NewMatcher(r.Match); err != nil {
			return fmt.Errorf("rule %s: %w", r.Name, err)
		}
	}
	slMu.Lock()
	defer slMu.Unlock()
	if rules == nil {
		rules = []SyslogRule{}
	}
	slRules = rules
	slCompile()
	_ = os.MkdirAll(a.syslogDir(), 0o700)
	b, _ := json.MarshalIndent(rules, "", "  ")
	return os.WriteFile(filepath.Join(a.syslogDir(), "rules.json"), b, 0o600)
}

// SyslogSendIn is a test message to send to a syslog server.
type SyslogSendIn struct {
	// Target: host or host:port (514; 6514 for TLS).
	Target string `json:"target"`
	// Proto: udp (default), tcp or tls.
	Proto string `json:"proto,omitempty"`
	// Format: 5424 (default) or 3164.
	Format string `json:"format,omitempty"`
	// Facility: a name (local7) or number (default user).
	Facility string `json:"facility,omitempty"`
	// Severity: a name (err, warning) or number (default notice).
	Severity string `json:"severity,omitempty"`
	// Host: the host name in the message (default this computer's).
	Host string `json:"host,omitempty"`
	// App: the program name (default termwright).
	App string `json:"app,omitempty"`
	// Text of the message.
	Text string `json:"text"`
	// Count: send it this many times (1-1000).
	Count int `json:"count,omitempty"`
	// Insecure: TLS without checking the server's certificate.
	Insecure bool `json:"insecure,omitempty"`
}

// SyslogSend sends a test message; it returns the message as sent.
func (a *App) SyslogSend(in SyslogSendIn) (string, error) {
	if strings.TrimSpace(in.Target) == "" {
		return "", errors.New("give the syslog server's address")
	}
	fac, sev := 1, 5
	if in.Facility != "" {
		if fac = syslog.FacilityOf(in.Facility); fac < 0 {
			return "", fmt.Errorf("unknown facility %q", in.Facility)
		}
	}
	if in.Severity != "" {
		if sev = syslog.SeverityOf(in.Severity); sev < 0 {
			return "", fmt.Errorf("unknown severity %q", in.Severity)
		}
	}
	n := min(max(in.Count, 1), 1000)
	var msg string
	for i := 0; i < n; i++ {
		var err error
		if msg, err = syslog.Send(syslog.Out{Target: in.Target, Proto: in.Proto, Format: in.Format, Facility: fac, Severity: sev, Host: in.Host, App: in.App, Text: in.Text, Insecure: in.Insecure}); err != nil {
			return "", err
		}
	}
	return msg, nil
}

// SyslogExport saves the messages that match as CSV (or text with .log / .txt) where the person chooses.
func (a *App) SyslogExport(q syslog.Query) (string, error) {
	q.Limit = 1 << 30
	list, err := a.SyslogMessages(q)
	if err != nil {
		return "", err
	}
	p, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "Export Syslog", DefaultFilename: "syslog-" + time.Now().Format("2006-01-02") + ".csv", CanCreateDirectories: true})
	if err != nil || p == "" {
		return "", err
	}
	f, err := os.Create(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if ext := strings.ToLower(filepath.Ext(p)); ext == ".log" || ext == ".txt" {
		for _, m := range list {
			fmt.Fprintf(f, "%s %s %s.%s %s\n", m.At.Format(time.RFC3339), m.From, m.FacilityName, m.SeverityName, m.Raw)
		}
		return p, nil
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"received", "from", "session", "facility", "severity", "time", "host", "app", "message"})
	for _, m := range list {
		_ = w.Write([]string{m.At.Format(time.RFC3339), m.From, m.Session, m.FacilityName, m.SeverityName, m.Time, m.Host, m.App, m.Text})
	}
	w.Flush()
	return p, w.Error()
}

// syslogAutoStart starts the server when the settings say so.
func (a *App) syslogAutoStart() {
	s := a.SyslogSettingsGet()
	slMu.Lock()
	running := slSrv != nil
	slMu.Unlock()
	if s.AutoStart && !running {
		_, _ = a.SyslogStart(s)
	}
}
