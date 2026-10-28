package main

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/discover"
	"github.com/termwright/termwright/internal/session"

	"github.com/termwright/termwright/internal/facts"
)

// DeviceFacts returns the model, OS version, serial number and base MAC
// address of a device for a report: first from the given terminal text
// (output already on screen), then - when fetch is true, something is still
// missing, and the session is an SSH or Telnet session with a known device
// type - by running the device's "show" commands in a background connection
// (nothing is typed in the tab). The MAC address saved with the session is
// the last resort.
func (a *App) DeviceFacts(path, deviceType, text string, fetch bool) facts.Facts {
	fam := facts.Family(deviceType)
	f := facts.Parse(fam, text)
	if path == "" {
		return f
	}
	s, err := a.store.Get(path)
	if err != nil {
		return f
	}
	if fam == "" {
		fam = facts.Family(s.DeviceType)
	}
	if fetch && !f.Complete() && fam != "" && (s.Protocol == "SSH2" || s.Protocol == "Telnet") {
		if s2, err := a.consoleSession(a.withFolderDefaults(s)); err == nil {
			prompt := ""
			if s2.DeviceType != "" && a.terms.DeviceType != nil {
				if dt, ok := a.terms.DeviceType(s2.DeviceType); ok {
					prompt = dt.Prompt
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			out, err := a.terms.Capture(ctx, s2, facts.Commands(fam), prompt)
			cancel()
			if err == nil {
				f = f.Merge(facts.Parse(fam, out))
			}
		}
	}
	if st := a.cfg.Get().DeviceFacts[path]; st != nil {
		f = f.Merge(facts.Facts{Model: st["Model"], Version: st["Version"], Serial: st["Serial"], MAC: st["MAC"]})
	}
	if f.MAC == "" {
		f.MAC = s.MAC
	}
	return f
}

// SessionFacts returns the facts stored for a session.
func (a *App) SessionFacts(path string) map[string]string {
	if a.cfg == nil {
		return nil
	}
	return a.cfg.Get().DeviceFacts[path]
}

// RefreshFacts reads a session's device facts with the rules of its device
// type, in a background connection (nothing is typed in its tabs). all =
// every rule (Refresh Device Facts); otherwise the rules marked Every
// (version, uptime) and those not read yet (model, serial, MAC) - and
// nothing when the login would have to ask for a password. A MAC address
// found is saved with the session when it has none. Returns the stored facts.
func (a *App) RefreshFacts(path string, all bool) (map[string]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	s, err := a.store.Get(path)
	if err != nil {
		return nil, err
	}
	s = a.withFolderDefaults(s)
	have := a.cfg.Get().DeviceFacts[path]
	if s.DeviceType == "" || a.terms.DeviceType == nil || (s.Protocol != session.ProtoSSH2 && s.Protocol != session.ProtoTelnet) {
		return have, nil
	}
	dt, ok := a.terms.DeviceType(s.DeviceType)
	if !ok || len(dt.Facts) == 0 {
		return have, nil
	}
	var rules []session.FactRule
	for _, r := range dt.Facts {
		if r.Command == "" || r.Regex == "" || r.Name == "" {
			continue
		}
		if all || r.Every || have[r.Name] == "" {
			rules = append(rules, r)
		}
	}
	if len(rules) == 0 {
		return have, nil
	}
	s2, err := a.consoleSession(s)
	if err != nil {
		return have, err
	}
	if !all && !a.terms.CanLogInUnattended(s2) {
		return have, nil
	}
	var cmds []string
	seen := map[string]int{}
	for _, r := range rules {
		if _, ok := seen[r.Command]; !ok {
			seen[r.Command] = len(cmds)
			cmds = append(cmds, r.Command)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	outs, err := a.terms.CaptureEach(ctx, s2, cmds, dt.Prompt)
	cancel()
	if err != nil {
		return have, err
	}
	got := ApplyFactRules(rules, cmds, outs)
	if len(got) == 0 {
		return have, nil
	}
	got["_at"] = time.Now().Format(time.RFC3339)
	if mac := discover.NormalizeMAC(got["MAC"]); mac != "" && s.MAC == "" {
		if cur, err := a.store.Get(path); err == nil && cur.MAC == "" {
			cur.MAC = mac
			_, _ = a.store.Save(cur, path)
		}
	}
	return a.cfg.SetFacts(path, got), nil
}

// ApplyFactRules reads each rule's value from the output of its command
// (cmds[i] printed outs[i]).
func ApplyFactRules(rules []session.FactRule, cmds, outs []string) map[string]string {
	got := map[string]string{}
	for _, r := range rules {
		i := -1
		for j, c := range cmds {
			if c == r.Command {
				i = j
			}
		}
		if i < 0 || i >= len(outs) {
			continue
		}
		re, err := regexp.Compile(`(?m)` + r.Regex)
		if err != nil {
			continue
		}
		if m := re.FindStringSubmatch(strings.ReplaceAll(outs[i], "\r", "")); m != nil {
			v := m[0]
			if len(m) > 1 {
				v = m[1]
			}
			if v = strings.TrimSpace(v); v != "" {
				if len(v) > 160 {
					v = v[:160]
				}
				got[r.Name] = v
			}
		}
	}
	return got
}
