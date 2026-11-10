package main

import (
	"net/netip"
	"strings"
	"sync"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/team"
)

// Name After Devices: sessions named by an address get the name the device
// gives itself (its hostname fact), optionally with the model.

// DeviceName is what Name After Devices found for one session.
type DeviceName struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Hostname string `json:"hostname"`
	Model    string `json:"model"`
	Team     bool   `json:"team"`  // a team device: the team decides its name
	State    string `json:"state"` // read, nologin, norule, notfound, failed, team
	Detail   string `json:"detail"`
}

// IsAddressName: a session name that is just its address (or any IP address).
func IsAddressName(name, host string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return false
	}
	if strings.EqualFold(n, strings.TrimSpace(host)) {
		return true
	}
	_, err := netip.ParseAddr(n)
	return err == nil
}

// ReadDeviceNames reads the hostname and model of the given sessions with
// their own saved logins, in the background (several at once; nothing is
// typed in a tab). Facts read before are used as they are.
func (a *App) ReadDeviceNames(paths []string) []DeviceName {
	out := make([]DeviceName, len(paths))
	if a.ready() != nil {
		return out
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, p := range paths {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = a.readDeviceName(p)
		}(i, p)
	}
	wg.Wait()
	return out
}

func (a *App) readDeviceName(path string) DeviceName {
	r := DeviceName{Path: path}
	s, err := a.store.Get(path)
	if err != nil {
		r.State, r.Detail = "failed", err.Error()
		return r
	}
	r.Name, r.Host, r.Team = s.Name, s.Hostname, s.Team != ""
	f := a.cfg.Get().DeviceFacts[path]
	if f["Hostname"] == "" {
		f, err = a.RefreshFacts(path, false)
	}
	r.Hostname, r.Model = f["Hostname"], f["Model"]
	switch {
	case r.Hostname != "" && r.Team:
		r.State = "team"
	case r.Hostname != "":
		r.State = "read"
	case err != nil:
		r.State, r.Detail = "failed", err.Error()
	case !a.hasHostnameRule(a.withFolderDefaults(s)):
		r.State = "norule"
	default:
		if s2, e := a.consoleSession(a.withFolderDefaults(s)); e == nil && !a.terms.CanLogInUnattended(s2) {
			r.State = "nologin"
		} else {
			r.State = "notfound"
		}
	}
	return r
}

func (a *App) hasHostnameRule(s session.Session) bool {
	if s.DeviceType == "" || a.terms.DeviceType == nil {
		return false
	}
	dt, ok := a.terms.DeviceType(s.DeviceType)
	if !ok {
		return false
	}
	for _, r := range dt.Facts {
		if strings.EqualFold(r.Name, "Hostname") && r.Command != "" && r.Regex != "" {
			return true
		}
	}
	return false
}

// SessionRename is one rename of Name After Devices.
type SessionRename struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// RenamedSession is the result of one rename (Path = the new path).
type RenamedSession struct {
	Old   string `json:"old"`
	Path  string `json:"path"`
	Error string `json:"error,omitempty"`
}

// RenameSessions renames sessions (team devices are skipped: the team names
// them). A name that is not the address is kept in the description.
func (a *App) RenameSessions(items []SessionRename) []RenamedSession {
	out := make([]RenamedSession, 0, len(items))
	for _, it := range items {
		r := RenamedSession{Old: it.Path}
		s, err := a.store.Get(it.Path)
		name := session.CleanName(it.Name)
		switch {
		case err != nil:
			r.Error = err.Error()
		case s.Team != "":
			r.Error = "a team device - the team decides its name"
		case name == "" || name == s.Name:
			r.Path = it.Path
		default:
			if old := s.Name; !IsAddressName(old, s.Hostname) {
				s.Description = append([]string{"Was named " + old}, s.Description...)
			}
			s.Name = name
			saved, err := a.SaveSession(s, it.Path)
			if err != nil {
				r.Error = err.Error()
			} else {
				r.Path = saved.Path
			}
		}
		out = append(out, r)
	}
	return out
}

// teamReportFacts tells the teams that have this device its hostname and model.
func (a *App) teamReportFacts(s session.Session, f map[string]string) {
	h, m := f["Hostname"], f["Model"]
	if (h == "" && m == "") || s.Hostname == "" || a.cfg == nil {
		return
	}
	d := normRef(team.DeviceRef{Host: s.Hostname, Port: s.Port, Protocol: string(s.Protocol), MAC: s.MAC})
	for _, t := range a.cfg.Get().Teams {
		if len(inInventory(t.ID, []team.DeviceRef{d})) == 0 {
			continue
		}
		if _, c, err := a.teamConn(t.ID); err == nil {
			ctx, cancel := teamCtx()
			_ = c.Facts(ctx, d, h, m)
			cancel()
		}
	}
}
