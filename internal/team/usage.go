package team

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Usage history for the admin page's reports: how long whom had which device
// open (from the presence heartbeats), reservations from start to end, and
// connections to devices someone else had reserved. Kept in usage.json for
// keepUsage; only devices of the inventory are counted.

const keepUsage = 400 * 24 * time.Hour

// UseRec is one stretch of a member having a device open.
type UseRec struct {
	Dev     string `json:"d"`  // inventory device id
	DevName string `json:"dn"` // its name then
	M       string `json:"m"`  // member id
	Name    string `json:"n"`  // member name then
	S       int64  `json:"s"`  // start, unix seconds
	E       int64  `json:"e"`  // end
	Open    bool   `json:"open,omitempty"`
}

// ResRec is one reservation from start to end (release, expiry or now).
type ResRec struct {
	Dev     string `json:"d"`
	DevName string `json:"dn"`
	M       string `json:"m"`
	Name    string `json:"n"`
	S       int64  `json:"s"`
	E       int64  `json:"e"`
	Until   int64  `json:"u"` // the end that was asked for
	Note    string `json:"note,omitempty"`
	Active  bool   `json:"active,omitempty"`
}

// ConRec is a connection to a device someone else had reserved.
type ConRec struct {
	Dev     string `json:"d"`
	DevName string `json:"dn"`
	M       string `json:"m"`
	Name    string `json:"n"`
	Owner   string `json:"o"` // who had reserved it
	T       int64  `json:"t"`
}

type usageFile struct {
	Use []UseRec `json:"use"`
	Res []ResRec `json:"res"`
	Con []ConRec `json:"con"`
}

type openUse struct {
	rec  UseRec
	last time.Time
}

func (s *Server) usageFile() string { return filepath.Join(s.Dir, "usage.json") }

func (s *Server) loadUsage() {
	s.opens = map[string]*openUse{}
	if b, err := os.ReadFile(s.usageFile()); err == nil {
		_ = json.Unmarshal(b, &s.usage)
	} else if !errors.Is(err, os.ErrNotExist) {
		s.Log.Printf("usage.json: %v", err)
	}
}

// saveUsage writes usage.json, dropping what is older than keepUsage (mu held).
func (s *Server) saveUsage() {
	cut := s.now().Add(-keepUsage).Unix()
	u := &s.usage
	for len(u.Use) > 0 && u.Use[0].E < cut {
		u.Use = u.Use[1:]
	}
	for len(u.Res) > 0 && u.Res[0].E < cut {
		u.Res = u.Res[1:]
	}
	for len(u.Con) > 0 && u.Con[0].T < cut {
		u.Con = u.Con[1:]
	}
	b, err := json.Marshal(u)
	if err != nil {
		return
	}
	tmp := s.usageFile() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.usageFile())
	}
}

// trackUse records which inventory devices a member has open now (mu held).
func (s *Server) trackUse(me *memberRec, list []DeviceRef) {
	now := s.now()
	have := map[string]bool{}
	for _, d := range list {
		inv := s.find2(d.dev())
		if inv == nil {
			continue
		}
		k := me.ID + "|" + inv.ID
		have[k] = true
		if o := s.opens[k]; o != nil {
			o.last = now
			continue
		}
		s.opens[k] = &openUse{rec: UseRec{Dev: inv.ID, DevName: inv.Name, M: me.ID, Name: me.Name, S: now.Unix()}, last: now}
	}
	for k, o := range s.opens {
		if o.rec.M == me.ID && !have[k] {
			s.closeUse(k, now)
		}
	}
}

// closeUse ends an open stretch at end (mu held).
func (s *Server) closeUse(k string, end time.Time) {
	o := s.opens[k]
	if o == nil {
		return
	}
	delete(s.opens, k)
	o.rec.E = end.Unix()
	if o.rec.E-o.rec.S < 5 { // a blink
		return
	}
	s.usage.Use = append(s.usage.Use, o.rec)
	s.saveUsage()
}

// endedMember closes the stretches of a member whose heartbeat stopped at last (mu held).
func (s *Server) endedMember(id string, last time.Time) {
	for k, o := range s.opens {
		if o.rec.M == id {
			s.closeUse(k, last)
		}
	}
}

// resEnded records a reservation that ended at end (mu held).
func (s *Server) resEnded(r *Reservation, end time.Time) {
	start, _ := time.Parse(time.RFC3339, r.At)
	until, _ := time.Parse(time.RFC3339, r.Until)
	if until.Before(end) {
		end = until
	}
	dev := ""
	if inv := s.find2(r.Device.dev()); inv != nil {
		dev = inv.ID
	}
	s.usage.Res = append(s.usage.Res, ResRec{Dev: dev, DevName: r.Device.Name, M: r.By, Name: r.Name, S: start.Unix(), E: end.Unix(), Until: until.Unix(), Note: r.Note})
	s.saveUsage()
}

// UsageReport is the history of a period for the admin page (it computes the figures).
type UsageReport struct {
	Now  int64    `json:"now"`
	From int64    `json:"from"`
	Use  []UseRec `json:"use"`
	Res  []ResRec `json:"res"`
	Con  []ConRec `json:"con"`
}

func (s *Server) usageReport(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners see the reports")
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 366 {
		days = 30
	}
	s.prune()
	now := s.now()
	from := now.Add(-time.Duration(days) * 24 * time.Hour).Unix()
	rep := UsageReport{Now: now.Unix(), From: from, Use: []UseRec{}, Res: []ResRec{}, Con: []ConRec{}}
	for _, u := range s.usage.Use {
		if u.E >= from {
			rep.Use = append(rep.Use, u)
		}
	}
	for _, o := range s.opens {
		u := o.rec
		u.E, u.Open = now.Unix(), true
		rep.Use = append(rep.Use, u)
	}
	for _, x := range s.usage.Res {
		if x.E >= from {
			rep.Res = append(rep.Res, x)
		}
	}
	for _, x := range s.st.Reservations {
		start, _ := time.Parse(time.RFC3339, x.At)
		until, _ := time.Parse(time.RFC3339, x.Until)
		dev := ""
		if inv := s.find2(x.Device.dev()); inv != nil {
			dev = inv.ID
		}
		rep.Res = append(rep.Res, ResRec{Dev: dev, DevName: x.Device.Name, M: x.By, Name: x.Name, S: start.Unix(), E: now.Unix(), Until: until.Unix(), Note: x.Note, Active: true})
	}
	for _, c := range s.usage.Con {
		if c.T >= from {
			rep.Con = append(rep.Con, c)
		}
	}
	writeJSON(w, 200, rep)
}
