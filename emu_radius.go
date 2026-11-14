package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// EmuRadiusLogin is one login the RADIUS server answered.
type EmuRadiusLogin struct {
	At     int64  `json:"at"`             // when (Unix seconds)
	Switch string `json:"switch"`         // the switch that asked (its address)
	User   string `json:"user"`           // user name or MAC
	How    string `json:"how"`            // EAP-MD5, PAP or MAC
	Result string `json:"result"`         // accept or reject
	VLAN   string `json:"vlan,omitempty"` // the VLAN it got
}

// EmuRadiusHost is the server on one host.
type EmuRadiusHost struct {
	Host      string           `json:"host"`          // the host (its name)
	Address   string           `json:"address"`       // where the switches there reach it ("" = the host's address on their network)
	Running   bool             `json:"running"`       // it answered the last look
	Err       string           `json:"err,omitempty"` // why it is not running
	Accept    int              `json:"accept"`        // logins let in
	Reject    int              `json:"reject"`        // logins refused
	Challenge int              `json:"challenge"`     // EAP challenges sent
	Acct      int              `json:"acct"`          // accounting records taken
	Logins    []EmuRadiusLogin `json:"logins"`        // the last ones, newest first
}

// EmuRadiusView is the lab's RADIUS server: its settings and how it does on each host.
type EmuRadiusView struct {
	emu.Radius
	Port  int             `json:"port"` // authentication port (accounting on the next)
	Hosts []EmuRadiusHost `json:"hosts"`
}

// emuRadiusCfg is radius.json for a host (mu held).
func emuRadiusCfg(r emu.Radius, bind string) []byte {
	if bind == "" {
		bind = "0.0.0.0"
	}
	b, _ := json.Marshal(map[string]any{"bind": bind, "port": r.RadiusPort(), "secret": r.Secret, "users": r.Users, "mab": r.MAB, "mabVlan": r.MABVLAN})
	return b
}

// emuRadiusAddr is where a host's switches reach the server (mu held).
func (a *App) emuRadiusAddr(host string) string {
	c := a.emuFor(host)
	if c == nil {
		return ""
	}
	if ip := c.RadiusAddr(); ip != "" {
		return ip
	}
	if c.Mgmt == "lan" {
		for _, h := range emuLab.f.Hosts {
			if h.Session == host {
				return strings.Split(h.Info.LanAddr, "/")[0]
			}
		}
	}
	return ""
}

// EmuRadius is the RADIUS server's settings and what it answered on each host.
func (a *App) EmuRadius() (EmuRadiusView, error) {
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuRadiusView{}, errors.New("there is no emulated lab")
	}
	v := EmuRadiusView{Hosts: []EmuRadiusHost{}}
	if l.Radius != nil {
		v.Radius = *l.Radius
	}
	v.Port = v.RadiusPort()
	hosts := l.Hosts(emuLab.f.Host)
	dir := l.VMDir()
	for _, h := range hosts {
		v.Hosts = append(v.Hosts, EmuRadiusHost{Host: h[strings.LastIndex(h, "/")+1:], Address: a.emuRadiusAddr(h), Logins: []EmuRadiusLogin{}})
	}
	emuLab.mu.Unlock()
	if !v.On {
		return v, nil
	}
	for i, h := range hosts {
		out, err := a.emuRun(h, "cd "+emu.Q(dir)+" 2>/dev/null && p=$(cat radius.pid 2>/dev/null) && grep -q tw-radius /proc/$p/cmdline 2>/dev/null && echo running; cat radius-stats.json 2>/dev/null", 15*time.Second)
		if err != nil {
			v.Hosts[i].Err = errText(err)
			continue
		}
		var st struct {
			Accept, Reject, Challenge, Acct int
			Log                             [][]any `json:"log"`
			Err                             string  `json:"err"`
		}
		run, js, _ := strings.Cut(out, "\n")
		if !strings.HasPrefix(run, "running") {
			js = out
		}
		_ = json.Unmarshal([]byte(strings.TrimSpace(js)), &st)
		hv := &v.Hosts[i]
		hv.Running, hv.Err = strings.HasPrefix(run, "running") && st.Err == "", st.Err
		if !hv.Running && hv.Err == "" {
			hv.Err = "not running"
		}
		hv.Accept, hv.Reject, hv.Challenge, hv.Acct = st.Accept, st.Reject, st.Challenge, st.Acct
		for j := len(st.Log) - 1; j >= 0; j-- {
			x := st.Log[j]
			if len(x) < 6 {
				continue
			}
			at, _ := x[0].(float64)
			s := func(k int) string { t, _ := x[k].(string); return t }
			hv.Logins = append(hv.Logins, EmuRadiusLogin{At: int64(at), Switch: s(1), User: s(2), How: s(3), Result: s(4), VLAN: s(5)})
		}
	}
	return v, nil
}

// EmuRadiusSet changes the RADIUS server (on, port, secret, users, MAC logins) and
// starts or stops it on the lab's hosts.
func (a *App) EmuRadiusSet(r emu.Radius) (EmuRadiusView, error) {
	defer emuAs("user")()
	if r.Port != 0 && (r.Port < 1 || r.Port > 65534) {
		return EmuRadiusView{}, errors.New("the port is 1 to 65534 (accounting takes the next)")
	}
	seen := map[string]bool{}
	for i, u := range r.Users {
		r.Users[i].User, r.Users[i].VLAN = strings.TrimSpace(u.User), strings.TrimSpace(u.VLAN)
		if r.Users[i].User == "" {
			return EmuRadiusView{}, errors.New("a user has no name")
		}
		if seen[r.Users[i].User] {
			return EmuRadiusView{}, fmt.Errorf("%s is there twice", r.Users[i].User)
		}
		seen[r.Users[i].User] = true
	}
	for i, m := range r.MAB {
		r.MAB[i] = strings.ToLower(strings.TrimSpace(m))
	}
	r.Secret = strings.TrimSpace(r.Secret)
	if r.On && r.Secret == "" {
		r.Secret = randHex(8)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	l := emuLab.f.Lab
	if l == nil {
		emuLab.mu.Unlock()
		return EmuRadiusView{}, errors.New("there is no emulated lab")
	}
	l.Radius = &r
	err := a.emuSave()
	emuLab.mu.Unlock()
	if err != nil {
		return EmuRadiusView{}, err
	}
	if err := a.emuRadiusSync(); err != nil {
		return EmuRadiusView{}, err
	}
	time.Sleep(300 * time.Millisecond) // its first stats
	return a.EmuRadius()
}

// emuRadiusSync writes the server's settings to every host of the lab and starts
// it there (or stops it).
func (a *App) emuRadiusSync() error {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	if l == nil || l.Radius == nil {
		emuLab.mu.Unlock()
		return nil
	}
	r := *l.Radius
	type job struct{ host, script string }
	var jobs []job
	for _, h := range l.Hosts(emuLab.f.Host) {
		bind := ""
		if c := a.emuFor(h); c != nil && c.Mgmt == "bridge" {
			bind = c.RadiusAddr() // only on the lab's own bridge
		}
		jobs = append(jobs, job{h, a.emuFor(h).RadiusScript(emuRadiusCfg(r, bind), r.On)})
	}
	emuLab.mu.Unlock()
	var bad []string
	for _, j := range jobs {
		if out, err := a.emuRun(j.host, j.script, 30*time.Second); err != nil || !strings.Contains(out, "radius-ok") {
			bad = append(bad, j.host[strings.LastIndex(j.host, "/")+1:]+": "+strings.TrimSpace(out+" "+errText(err)))
		}
	}
	if len(bad) > 0 {
		return errors.New("the RADIUS server did not start on " + strings.Join(bad, "; "))
	}
	return nil
}
