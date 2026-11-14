package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/clirun"
	"github.com/termwright/termwright/internal/discover"
	"github.com/termwright/termwright/internal/facts"
)

// Device facts read by the team server: it logs in to a device with the login for its kind (EXOS, VOSS,
// Cisco IOS; the factory default until an owner or co-owner sets another on Discover), runs the show
// commands and keeps the model, serial number, base MAC address and the name the device gives itself.
// The logins stay on the server: members never get them.

// Login is how the server logs in to one kind of device.
type Login struct {
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
}

// The kinds of device the server reads, and their factory default logins.
var loginKinds = []struct{ fam, label string }{{"exos", "EXOS"}, {"voss", "VOSS"}, {"cisco", "Cisco IOS"}}

var defaultLogins = map[string]Login{"exos": {User: "admin"}, "voss": {User: "rwa", Password: "rwa"}}

// LoginView is a login as the admin page sees it (never the password).
type LoginView struct {
	Family      string `json:"family"`
	Label       string `json:"label"`
	User        string `json:"user"`
	HasPassword bool   `json:"hasPassword"`
	IsDefault   bool   `json:"isDefault"`
}

// FactsRun is the server's reading of device facts now.
type FactsRun struct {
	Running bool `json:"running"`
	Done    int  `json:"done"`
	Total   int  `json:"total"`
}

const factsAtOnce = 8

// loginFor is the login for a kind of device; ok is false when there is none (mu held).
func (s *Server) loginFor(fam string) (Login, bool) {
	l, ok := s.st.Logins[fam]
	if !ok {
		l = defaultLogins[fam]
	}
	return l, l.User != ""
}

func (s *Server) loginViews() []LoginView {
	var out []LoginView
	for _, k := range loginKinds {
		l, _ := s.loginFor(k.fam)
		_, own := s.st.Logins[k.fam]
		out = append(out, LoginView{Family: k.fam, Label: k.label, User: l.User, HasPassword: l.Password != "", IsDefault: !own})
	}
	return out
}

// saveLogins: POST /api/admin/logins {logins: [{family, user, password ("" = keep)}], reset: family, factsOnAdd}.
func (s *Server) saveLogins(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can set the device logins")
		return
	}
	var b struct {
		Logins []struct {
			Family   string `json:"family"`
			User     string `json:"user"`
			Password string `json:"password"`
		} `json:"logins"`
		Reset      string `json:"reset"`
		FactsOnAdd bool   `json:"factsOnAdd"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&b) != nil {
		fail(w, 400, "bad request")
		return
	}
	known := map[string]string{}
	for _, k := range loginKinds {
		known[k.fam] = k.label
	}
	if s.st.Logins == nil {
		s.st.Logins = map[string]Login{}
	}
	var changed []string
	if b.Reset != "" {
		if known[b.Reset] == "" {
			fail(w, 400, "no such kind of device")
			return
		}
		if _, ok := s.st.Logins[b.Reset]; ok {
			delete(s.st.Logins, b.Reset)
			changed = append(changed, known[b.Reset]+" (factory default)")
		}
	}
	for _, l := range b.Logins {
		if known[l.Family] == "" {
			fail(w, 400, "no such kind of device: "+l.Family)
			return
		}
		cur, _ := s.loginFor(l.Family)
		n := Login{User: clean(l.User, 64), Password: l.Password}
		if n.Password == "" && n.User == cur.User {
			n.Password = cur.Password
		}
		if len(n.Password) > 256 {
			fail(w, 400, "the password is too long")
			return
		}
		if n == cur {
			continue
		}
		if n == defaultLogins[l.Family] {
			delete(s.st.Logins, l.Family)
		} else {
			s.st.Logins[l.Family] = n
		}
		changed = append(changed, known[l.Family])
	}
	if s.st.NoFactsOnAdd == b.FactsOnAdd {
		s.st.NoFactsOnAdd = !b.FactsOnAdd
		s.log(me.Name, "turned %s reading device facts when devices are added", map[bool]string{true: "on", false: "off"}[b.FactsOnAdd])
	}
	if len(changed) > 0 {
		s.log(me.Name, "changed the device logins: %s", strings.Join(changed, ", "))
	}
	if err := s.save(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"logins": s.loginViews()})
}

// readFacts: POST /api/admin/facts {ids} reads the facts of these devices now.
func (s *Server) readFacts(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can read device facts")
		return
	}
	var b idsBody
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&b) != nil || len(b.IDs) == 0 {
		fail(w, 400, "select devices first")
		return
	}
	n, skipped, why := s.startFacts(b.IDs, true)
	if n > 0 {
		s.log(me.Name, "reads the facts of %s", plural(n))
	}
	writeJSON(w, 200, map[string]any{"started": n, "skipped": skipped, "why": why})
}

type factsJob struct {
	id, name, fam, known string
	t                    clirun.Target
}

// startFacts reads the facts of devices in the background; explicit (asked for on the admin page) takes a
// device's new SSH host key, otherwise a changed key stops it. Returns how many it reads, how many it
// skipped and why (mu held).
func (s *Server) startFacts(ids []string, explicit bool) (n, skipped int, why string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	reasons := map[string]bool{}
	var jobs []factsJob
	for _, d := range s.st.Inventory {
		if !want[d.ID] {
			continue
		}
		fam := facts.Family(d.DeviceType)
		l, ok := s.loginFor(fam)
		reason := ""
		switch {
		case fam == "" || fam == "linux":
			reason = "not EXOS, VOSS or Cisco IOS"
		case d.Protocol != "SSH2" && d.Protocol != "Telnet":
			reason = "not SSH or Telnet"
		case !ok:
			reason = "no login for " + d.DeviceType
		}
		if reason != "" {
			skipped++
			reasons[reason] = true
			continue
		}
		j := factsJob{id: d.ID, name: d.Name, fam: fam, known: s.st.HostKeys[devKey(d)],
			t: clirun.Target{Protocol: d.Protocol, Host: d.Host, Port: d.Port, User: l.User, Password: l.Password}}
		if !explicit {
			j.t.HostKey = j.known
		}
		jobs = append(jobs, j)
	}
	for r := range reasons {
		why = strings.TrimPrefix(why+"; "+r, "; ")
	}
	if len(jobs) == 0 {
		return 0, skipped, why
	}
	if !s.facts.Running {
		s.facts = FactsRun{}
	}
	s.facts.Running = true
	s.facts.Total += len(jobs)
	if s.factsSem == nil {
		s.factsSem = make(chan struct{}, factsAtOnce)
	}
	run := s.runCLI
	if run == nil {
		run = clirun.Run
	}
	for _, j := range jobs {
		go func() {
			s.factsSem <- struct{}{}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			outs, key, err := run(ctx, j.t, facts.PagingOff(j.fam), facts.Commands(j.fam))
			cancel()
			<-s.factsSem
			s.mu.Lock()
			defer s.mu.Unlock()
			s.applyFacts(j, facts.Parse(j.fam, strings.Join(outs, "\n")), key, err)
			if s.facts.Done++; s.facts.Done >= s.facts.Total {
				s.facts.Running = false
			}
		}()
	}
	return len(jobs), skipped, why
}

// applyFacts keeps what a reading found, or why it failed (mu held).
func (s *Server) applyFacts(j factsJob, f facts.Facts, key string, err error) {
	var d *Device
	for _, x := range s.st.Inventory {
		if x.ID == j.id {
			d = x
		}
	}
	if d == nil {
		return
	}
	d.FactsAt, d.FactsBy, d.FactsErr = s.now().UTC().Format(time.RFC3339), "server", ""
	var he *clirun.HostKeyError
	switch {
	case errors.Is(err, clirun.ErrLogin):
		d.FactsErr = fmt.Sprintf("the login %s was refused (set it on Discover → Device logins)", j.t.User)
	case errors.As(err, &he):
		d.FactsErr = he.Error() + ": select it on Inventory and choose Read Device Facts to take the new key"
	case err != nil:
		d.FactsErr = clean(err.Error(), 200)
	}
	if key != "" && err == nil || key != "" && j.known == "" {
		if j.known != "" && key != j.known {
			s.log("team server", "the SSH host key of %s changed (%s, was %s)", d.Name, key, j.known)
		}
		if s.st.HostKeys == nil {
			s.st.HostKeys = map[string]string{}
		}
		s.st.HostKeys[devKey(d)] = key
	}
	if m := clean(f.Model, 80); m != "" {
		d.Model = m
	}
	if v := clean(f.Serial, 80); v != "" {
		d.Serial = v
	}
	if mac := discover.NormalizeMAC(f.MAC); mac != "" {
		d.BaseMAC = mac
	}
	if h := clean(f.Hostname, 80); h != "" && !isIP(h) {
		d.Hostname = h
	}
	if err == nil && f.Model == "" && f.Serial == "" && f.MAC == "" {
		d.FactsErr = "it logged in, but the output had no model, serial number or MAC address"
	}
	_ = s.save()
}
