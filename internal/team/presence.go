package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Who uses which device: every member's Termwright tells the server which of
// the team's network devices it has open (a heartbeat every 30 seconds), and
// members reserve devices for a while with a note. Termwright decides what
// counts - switches, routers, firewalls, access points and the like, never
// computers, phones, printers or IoT boards, which take several logins at
// once - and only reports devices that are in the team's inventory.

// presenceFor: an open device counts this long after the last heartbeat
// (a laptop that sleeps drops out by itself).
const presenceFor = 90 * time.Second

// maxReserve is the longest reservation.
const maxReserve = 31 * 24 * time.Hour

// DeviceRef names a device: its address and port, or its MAC address.
type DeviceRef struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	MAC      string `json:"mac,omitempty"`
	Name     string `json:"name,omitempty"`  // the name it has for whoever reports it
	Since    string `json:"since,omitempty"` // open since (RFC 3339)
}

func (r DeviceRef) dev() *Device {
	return &Device{Host: r.Host, Port: r.Port, Protocol: r.Protocol, MAC: r.MAC}
}

type presenceRec struct {
	devices []DeviceRef
	at      time.Time
}

// Reservation keeps a device for a member until a time.
type Reservation struct {
	ID     string    `json:"id"`
	Device DeviceRef `json:"device"`
	By     string    `json:"by"`   // member id
	Name   string    `json:"name"` // member name
	Until  string    `json:"until"`
	Note   string    `json:"note"`
	At     string    `json:"at"`
}

// InUse is one member having a device open.
type InUse struct {
	Device DeviceRef `json:"device"`
	By     string    `json:"by"`
	Name   string    `json:"name"`
	Since  string    `json:"since"`
}

// Note is a notice for one member (someone connected to their reserved device …).
type Note struct {
	At   string `json:"at"`
	Text string `json:"text"`
}

// Status is who uses and who reserved which devices, plus this member's notices.
type Status struct {
	Rev int64  `json:"rev"`
	Me  string `json:"me"`
	// Also: the other members with my name - me on another computer
	Also     []string       `json:"also"`
	InUse    []InUse        `json:"inUse"`
	Reserved []*Reservation `json:"reserved"`
	Notes    []Note         `json:"notes"`
	Last     []LastUse      `json:"last"` // per inventory device: the last time someone had it open
}

// LastUse is the most recent stretch someone had a device open (closed ones only).
type LastUse struct {
	Device DeviceRef `json:"device"`
	Name   string    `json:"name"`
	S      int64     `json:"s"`
	E      int64     `json:"e"`
}

func (s *Server) statusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/presence", s.auth(s.setPresence))
	mux.HandleFunc("GET /api/status", s.getStatus)
	mux.HandleFunc("POST /api/reserve", s.auth(s.reserve))
	mux.HandleFunc("POST /api/release", s.auth(s.release))
	mux.HandleFunc("POST /api/connected", s.auth(s.connected))
	mux.HandleFunc("GET /api/admin/usage", s.auth(s.usageReport))
}

// wake tells the status long polls that something changed (mu held).
func (s *Server) wake(changed bool) {
	if changed {
		s.statusRev++
	}
	close(s.statusCh)
	s.statusCh = make(chan struct{})
}

// prune drops heartbeats that stopped and reservations that ended (mu held).
func (s *Server) prune() {
	now := s.now()
	changed := false
	for id, p := range s.presence {
		if now.Sub(p.at) > presenceFor {
			delete(s.presence, id)
			s.endedMember(id, p.at)
			changed = true
		}
	}
	var keep []*Reservation
	for _, r := range s.st.Reservations {
		if t, err := time.Parse(time.RFC3339, r.Until); err == nil && now.After(t) {
			s.resEnded(r, t)
			changed = true
			continue
		}
		if _, m := s.find(r.By); m == nil { // the member left
			s.resEnded(r, now)
			changed = true
			continue
		}
		keep = append(keep, r)
	}
	if len(keep) != len(s.st.Reservations) {
		s.st.Reservations = keep
		_ = s.save()
	}
	if changed {
		s.wake(true)
	}
}

func tidyRef(r *DeviceRef) string {
	d := Device{Name: r.Name, Host: r.Host, Port: r.Port, Protocol: r.Protocol, MAC: r.MAC}
	if msg := tidy(&d); msg != "" {
		return msg
	}
	r.Host, r.Protocol, r.MAC, r.Name = d.Host, d.Protocol, d.MAC, d.Name
	if r.Port == 0 {
		r.Port = DefaultPortOf(r.Protocol)
	}
	return ""
}

func refsKey(l []DeviceRef) string {
	k := make([]string, len(l))
	for i, r := range l {
		k[i] = devKey(r.dev()) + "/" + r.MAC + "/" + r.Since
	}
	sort.Strings(k)
	return strings.Join(k, ",")
}

// setPresence: the devices this member has open now (replaces the last list).
func (s *Server) setPresence(w http.ResponseWriter, r *http.Request, me *memberRec) {
	var b struct {
		Devices []DeviceRef `json:"devices"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil || len(b.Devices) > 500 {
		fail(w, 400, "bad request")
		return
	}
	var list []DeviceRef
	for _, d := range b.Devices {
		// only the team's network devices count (older Termwrights send others too)
		if tidyRef(&d) == "" {
			if inv := s.find2(d.dev()); inv != nil && Tracked(inv.DeviceType) {
				list = append(list, d)
			}
		}
	}
	old := s.presence[me.ID]
	same := old != nil && refsKey(old.devices) == refsKey(list)
	s.trackUse(me, list)
	if len(list) == 0 {
		delete(s.presence, me.ID)
	} else {
		s.presence[me.ID] = &presenceRec{devices: list, at: s.now()}
	}
	if !same && (old != nil || len(list) > 0) {
		s.wake(true)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) statusFor(me *memberRec) Status {
	st := Status{Rev: s.statusRev, Me: me.ID, InUse: []InUse{}, Reserved: s.st.Reservations, Notes: s.notes[me.ID]}
	if st.Reserved == nil {
		st.Reserved = []*Reservation{}
	}
	if st.Notes == nil {
		st.Notes = []Note{}
	}
	st.Also = []string{}
	for _, m := range s.st.Members {
		if m.ID != me.ID && sameName(m.Name, me.Name) {
			st.Also = append(st.Also, m.ID)
		}
	}
	for id, p := range s.presence {
		_, m := s.find(id)
		if m == nil {
			continue
		}
		for _, d := range p.devices {
			st.InUse = append(st.InUse, InUse{Device: d, By: id, Name: m.Name, Since: d.Since})
		}
	}
	sort.Slice(st.InUse, func(i, j int) bool { return st.InUse[i].Since < st.InUse[j].Since })
	last := map[string]UseRec{}
	for _, u := range s.usage.Use {
		if u.E > last[u.Dev].E {
			last[u.Dev] = u
		}
	}
	st.Last = []LastUse{}
	for _, d := range s.st.Inventory {
		if u, ok := last[d.ID]; ok {
			port := d.Port
			if port == 0 {
				port = DefaultPortOf(d.Protocol)
			}
			st.Last = append(st.Last, LastUse{Device: DeviceRef{Host: d.Host, Port: port, Protocol: d.Protocol, MAC: d.MAC, Name: d.Name}, Name: u.Name, S: u.S, E: u.E})
		}
	}
	return st
}

// getStatus: GET /api/status?rev=N&wait=50 answers when the status is newer
// than N or a notice waits for this member, else after wait seconds.
func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	have, _ := strconv.ParseInt(r.URL.Query().Get("rev"), 10, 64)
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	if wait > 55 {
		wait = 55
	}
	deadline := time.After(time.Duration(wait) * time.Second)
	for {
		var ch chan struct{}
		ok := false
		s.auth(func(w http.ResponseWriter, _ *http.Request, me *memberRec) {
			ok = true
			s.prune()
			if s.statusRev != have || len(s.notes[me.ID]) > 0 || wait <= 0 {
				writeJSON(w, 200, s.statusFor(me))
				delete(s.notes, me.ID)
				return
			}
			ch = s.statusCh
		})(w, r)
		if !ok || ch == nil {
			return
		}
		select {
		case <-ch:
		case <-deadline:
			wait = 0
		case <-r.Context().Done():
			return
		}
	}
}

// reservationOf is the reservation of a device, if any (mu held).
func (s *Server) reservationOf(d DeviceRef) *Reservation {
	for _, r := range s.st.Reservations {
		if same(r.Device.dev(), d.dev()) {
			return r
		}
	}
	return nil
}

func (s *Server) note(member, f string, v ...any) {
	n := Note{At: s.now().UTC().Format(time.RFC3339), Text: sprintfMaybe(f, v...)}
	l := append(s.notes[member], n)
	if len(l) > 20 {
		l = l[len(l)-20:]
	}
	s.notes[member] = l
}

func sprintfMaybe(f string, v ...any) string {
	if len(v) == 0 {
		return f
	}
	return fmt.Sprintf(f, v...)
}

// reserve keeps a device for this member until a time (or changes their reservation).
func (s *Server) reserve(w http.ResponseWriter, r *http.Request, me *memberRec) {
	var b struct {
		Device DeviceRef `json:"device"`
		Until  string    `json:"until"`
		Note   string    `json:"note"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		fail(w, 400, "bad request")
		return
	}
	if msg := tidyRef(&b.Device); msg != "" {
		fail(w, 400, msg)
		return
	}
	until, err := time.Parse(time.RFC3339, b.Until)
	now := s.now()
	if err != nil || !until.After(now) {
		fail(w, 400, "the end of the reservation must be in the future")
		return
	}
	if until.Sub(now) > maxReserve {
		fail(w, 400, "a reservation can last 31 days at most")
		return
	}
	inv := s.find2(b.Device.dev())
	if inv == nil {
		fail(w, 404, "this device is not in the team's inventory - push it to the team first")
		return
	}
	// the reservation names the device as the inventory does
	b.Device = DeviceRef{Host: inv.Host, Port: inv.Port, Protocol: inv.Protocol, MAC: inv.MAC, Name: inv.Name}
	if b.Device.Port == 0 {
		b.Device.Port = DefaultPortOf(inv.Protocol)
	}
	s.prune()
	cur := s.reservationOf(b.Device)
	if cur != nil && !s.same(cur.By, me) {
		fail(w, 409, "reserved by "+cur.Name+" until "+cur.Until)
		return
	}
	if cur == nil {
		cur = &Reservation{ID: randToken(9), By: me.ID, Name: me.Name, At: now.UTC().Format(time.RFC3339)}
		s.st.Reservations = append(s.st.Reservations, cur)
	}
	cur.Device, cur.Until, cur.Note = b.Device, until.UTC().Format(time.RFC3339), clean(b.Note, 200)
	s.log(me.Name, "reserved %s until %s", nameOf(b.Device), timeMark(until))
	if err := s.save(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	s.wake(true)
	writeJSON(w, 200, cur)
}

// same: the member id is me, or me on another computer (a member with my name).
func (s *Server) same(id string, me *memberRec) bool {
	if id == me.ID {
		return true
	}
	_, m := s.find(id)
	return m != nil && sameName(m.Name, me.Name)
}

// sameName: the names people chose are the same person's ("Natarajan
// Periannan" on the laptop and on the lab PC).
func sameName(a, b string) bool {
	a, b = strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " ")
	return a != "" && strings.EqualFold(a, b)
}

// timeMark puts a time in an activity text; the admin page shows it in the
// browser's time zone ("{t:2026-11-08T12:30:00Z}").
func timeMark(t time.Time) string { return "{t:" + t.UTC().Format(time.RFC3339) + "}" }

func nameOf(d DeviceRef) string {
	if d.Name != "" && d.Name != d.Host {
		return d.Name + " (" + d.Host + ")"
	}
	return d.Host
}

// release ends a reservation: its member's own, or anyone's for owners and co-owners.
func (s *Server) release(w http.ResponseWriter, r *http.Request, me *memberRec) {
	var b MemberRef
	_ = json.NewDecoder(r.Body).Decode(&b)
	for i, x := range s.st.Reservations {
		if x.ID != b.ID {
			continue
		}
		if !s.same(x.By, me) && !manager(me) {
			fail(w, 403, "only "+x.Name+", the owner or a co-owner can release this reservation")
			return
		}
		s.st.Reservations = append(s.st.Reservations[:i], s.st.Reservations[i+1:]...)
		s.resEnded(x, s.now())
		s.log(me.Name, "released %s (reserved by %s)", nameOf(x.Device), x.Name)
		if !s.same(x.By, me) {
			s.note(x.By, "%s released your reservation of %s.", me.Name, nameOf(x.Device))
		}
		if err := s.save(); err != nil {
			fail(w, 500, err.Error())
			return
		}
		s.wake(true)
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	fail(w, 404, "no such reservation (it may have ended)")
}

// connected: this member connected to a device someone else reserved - they get a notice.
func (s *Server) connected(w http.ResponseWriter, r *http.Request, me *memberRec) {
	var b struct {
		Device DeviceRef `json:"device"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if tidyRef(&b.Device) != "" {
		fail(w, 400, "bad request")
		return
	}
	if x := s.reservationOf(b.Device); x != nil && !s.same(x.By, me) {
		s.note(x.By, "%s connected to %s, which you reserved.", me.Name, nameOf(x.Device))
		dev := ""
		if inv := s.find2(x.Device.dev()); inv != nil {
			dev = inv.ID
		}
		s.usage.Con = append(s.usage.Con, ConRec{Dev: dev, DevName: x.Device.Name, M: me.ID, Name: me.Name, Owner: x.Name, T: s.now().Unix()})
		s.saveUsage()
		s.log(me.Name, "connected to %s, reserved by %s", nameOf(x.Device), x.Name)
		s.wake(false)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
