package team

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/discover"
)

// The team inventory: the lab's devices, kept by the server and shown in every
// member's Termwright as a folder of its own. Owners and co-owners change it
// (on the admin page, or by pushing from Termwright); what a member pushes
// waits for their approval. Logins are never part of it.

// Device is one device of the inventory.
type Device struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Folder      string `json:"folder"` // "Core/Access", "" = the team folder itself
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"` // SSH2, Telnet, …
	DeviceType  string `json:"deviceType"`
	MAC         string `json:"mac"`
	Description string `json:"description"`
	Hostname    string `json:"hostname,omitempty"` // the name the device gives itself (read by a member's Termwright, or SNMP sysName from a scan)
	Model       string `json:"model,omitempty"`
	AddedBy     string `json:"addedBy"`
	Added       string `json:"added"`
	Updated     string `json:"updated"`
}

// Pending is a device a member pushed, waiting for an owner or co-owner.
type Pending struct {
	ID     string `json:"id"`
	Device Device `json:"device"`
	By     string `json:"by"`   // member id
	Name   string `json:"name"` // member name, for showing
	At     string `json:"at"`
}

// Activity is one line of the admin page's log.
type Activity struct {
	At   string `json:"at"`
	Who  string `json:"who"`
	What string `json:"what"`
}

const maxActivity = 5000

// Inventory is what members fetch.
type Inventory struct {
	Rev     int64     `json:"rev"`
	Devices []*Device `json:"devices"`
	Folders []string  `json:"folders"`
}

// PushResult says what happened to one pushed device.
type PushResult struct {
	Status string `json:"status"` // added, exists, pending, invalid
	Name   string `json:"name"`   // the device's name in the team (exists) or as pushed
	Folder string `json:"folder"`
	Msg    string `json:"msg,omitempty"`
}

// DefaultPortOf is the usual port of a protocol.
func DefaultPortOf(proto string) int {
	switch strings.ToUpper(proto) {
	case "TELNET":
		return 23
	case "RDP":
		return 3389
	case "VNC":
		return 5900
	case "RAW":
		return 0
	}
	return 22
}

// key: the same device has the same address and port (names differ between people).
func devKey(d *Device) string {
	p := d.Port
	if p == 0 {
		p = DefaultPortOf(d.Protocol)
	}
	return strings.ToLower(strings.TrimSpace(d.Host)) + ":" + strconv.Itoa(p)
}

// same: the same address and port, or the same MAC address.
func same(a, b *Device) bool {
	if devKey(a) == devKey(b) {
		return true
	}
	return a.MAC != "" && b.MAC != "" && a.MAC == b.MAC
}

func cleanFolder(f string) string {
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(f, `\`, "/"), "/") {
		if p = clean(p, 80); p != "" && p != "." && p != ".." {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

// tidy checks and cleans a device from outside; "" = fine.
func tidy(d *Device) string {
	d.Name = clean(d.Name, 80)
	d.Host = clean(d.Host, 255)
	d.Folder = cleanFolder(d.Folder)
	d.Protocol = clean(d.Protocol, 20)
	d.DeviceType = clean(d.DeviceType, 60)
	d.Description = clean(d.Description, 300)
	d.Hostname = clean(d.Hostname, 80)
	d.Model = clean(d.Model, 80)
	d.MAC = discover.NormalizeMAC(d.MAC)
	if d.Protocol == "" {
		d.Protocol = "SSH2"
	}
	if d.Host == "" || strings.ContainsAny(d.Host, " /") {
		return "no address"
	}
	if d.Port < 0 || d.Port > 65535 {
		return "wrong port"
	}
	if d.Name == "" {
		d.Name = d.Host
	}
	d.Name = strings.ReplaceAll(d.Name, "/", "-")
	return ""
}

// log adds an activity line (mu held).
func (s *Server) log(who, f string, v ...any) {
	what := f
	if len(v) > 0 {
		what = fmt.Sprintf(f, v...)
	}
	s.st.Activity = append(s.st.Activity, Activity{At: s.now().UTC().Format(time.RFC3339), Who: who, What: what})
	if n := len(s.st.Activity); n > maxActivity {
		s.st.Activity = s.st.Activity[n-maxActivity:]
	}
	s.Log.Printf("%s: %s", who, what)
}

// bump saves a changed inventory and wakes the long polls (mu held).
func (s *Server) bump() error {
	s.st.Rev++
	s.lastRev++ // names and addresses in Last come from the inventory
	close(s.changed)
	s.changed = make(chan struct{})
	return s.save()
}

func (s *Server) find2(d *Device) *Device {
	for _, x := range s.st.Inventory {
		if same(x, d) {
			return x
		}
	}
	return nil
}

func (s *Server) pendingOf(d *Device) *Pending {
	for _, p := range s.st.Pending {
		if same(&p.Device, d) {
			return p
		}
	}
	return nil
}

func (s *Server) inventoryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/inventory", s.getInventory)
	s.statusRoutes(mux)
	mux.HandleFunc("POST /api/inventory/check", s.auth(s.check))
	mux.HandleFunc("POST /api/inventory/push", s.auth(s.push))
	mux.HandleFunc("POST /api/inventory/facts", s.auth(s.deviceFacts))
	mux.HandleFunc("GET /api/admin/state", s.auth(s.adminState))
	mux.HandleFunc("POST /api/admin/devices", s.auth(s.saveDevices))
	mux.HandleFunc("POST /api/admin/devices/delete", s.auth(s.deleteDevices))
	mux.HandleFunc("POST /api/admin/folders", s.auth(s.saveFolder))
	mux.HandleFunc("POST /api/admin/pending/approve", s.auth(s.approve))
	mux.HandleFunc("POST /api/admin/pending/reject", s.auth(s.reject))
	mux.HandleFunc("POST /api/admin/link", s.auth(s.adminLink))
	mux.HandleFunc("POST /api/admin/session", s.adminSignIn)
	mux.HandleFunc("POST /api/admin/signout", s.adminSignOut)
	mux.HandleFunc("POST /api/admin/scan", s.auth(s.startScan))
	mux.HandleFunc("GET /api/admin/scan", s.auth(s.scanStatus))
	mux.HandleFunc("POST /api/admin/scan/stop", s.auth(s.stopScan))
	mux.HandleFunc("GET /admin", s.adminPage)
	mux.HandleFunc("GET /admin/icon.svg", s.adminIcon)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin", http.StatusFound)
	})
}

// getInventory: GET /api/inventory?rev=N&wait=25 answers at once when the
// inventory is newer than N, else when it changes or after wait seconds.
func (s *Server) getInventory(w http.ResponseWriter, r *http.Request) {
	have, _ := strconv.ParseInt(r.URL.Query().Get("rev"), 10, 64)
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	if wait > 55 {
		wait = 55
	}
	deadline := time.After(time.Duration(wait) * time.Second)
	for {
		var ch chan struct{}
		ok := false
		s.auth(func(w http.ResponseWriter, _ *http.Request, _ *memberRec) {
			ok = true
			if s.st.Rev != have || wait <= 0 {
				inv := Inventory{Rev: s.st.Rev, Devices: s.st.Inventory, Folders: s.st.Folders}
				if inv.Devices == nil {
					inv.Devices = []*Device{}
				}
				writeJSON(w, 200, inv)
				ch = nil
				return
			}
			ch = s.changed
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

type devicesBody struct {
	Devices []Device `json:"devices"`
}

// check tells for each device whether the team has it already (maybe under another name).
func (s *Server) check(w http.ResponseWriter, r *http.Request, me *memberRec) {
	var b devicesBody
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		fail(w, 400, "bad request")
		return
	}
	out := make([]PushResult, len(b.Devices))
	for i := range b.Devices {
		d := &b.Devices[i]
		if msg := tidy(d); msg != "" {
			out[i] = PushResult{Status: "invalid", Name: d.Name, Msg: msg}
		} else if x := s.find2(d); x != nil {
			out[i] = PushResult{Status: "exists", Name: x.Name, Folder: x.Folder}
		} else if p := s.pendingOf(d); p != nil {
			out[i] = PushResult{Status: "pending", Name: p.Device.Name, Folder: p.Device.Folder, Msg: "pushed by " + p.Name}
		} else {
			out[i] = PushResult{Status: "new", Name: d.Name, Folder: d.Folder}
		}
	}
	writeJSON(w, 200, out)
}

// push adds devices (owners and co-owners) or offers them for approval (members).
func (s *Server) push(w http.ResponseWriter, r *http.Request, me *memberRec) {
	var b devicesBody
	if json.NewDecoder(r.Body).Decode(&b) != nil || len(b.Devices) > 5000 {
		fail(w, 400, "bad request")
		return
	}
	now := s.now().UTC().Format(time.RFC3339)
	out := make([]PushResult, len(b.Devices))
	added, waiting := 0, 0
	for i := range b.Devices {
		d := b.Devices[i]
		switch {
		case tidy(&d) != "":
			out[i] = PushResult{Status: "invalid", Name: d.Name, Msg: tidy(&d)}
		case s.find2(&d) != nil:
			x := s.find2(&d)
			out[i] = PushResult{Status: "exists", Name: x.Name, Folder: x.Folder}
		case manager(me):
			d.ID, d.AddedBy, d.Added, d.Updated = randToken(9), me.Name, now, now
			s.st.Inventory = append(s.st.Inventory, &d)
			out[i] = PushResult{Status: "added", Name: d.Name, Folder: d.Folder}
			added++
		case s.pendingOf(&d) != nil:
			p := s.pendingOf(&d)
			out[i] = PushResult{Status: "pending", Name: p.Device.Name, Folder: p.Device.Folder, Msg: "pushed by " + p.Name}
		default:
			s.st.Pending = append(s.st.Pending, &Pending{ID: randToken(9), Device: d, By: me.ID, Name: me.Name, At: now})
			out[i] = PushResult{Status: "pending", Name: d.Name, Folder: d.Folder}
			waiting++
		}
	}
	if added > 0 {
		s.log(me.Name, "added %d device%s from Termwright", added, plural(added))
		if err := s.bump(); err != nil {
			fail(w, 500, err.Error())
			return
		}
	} else if waiting > 0 {
		s.log(me.Name, "pushed %d device%s for approval", waiting, plural(waiting))
		_ = s.save()
	}
	writeJSON(w, 200, out)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// AdminState is everything the admin page shows.
type AdminState struct {
	Team      Team       `json:"team"`
	Inventory Inventory  `json:"inventory"`
	Pending   []*Pending `json:"pending"`
	Activity  []Activity `json:"activity"`
	Me        string     `json:"me"`
	Invite    string     `json:"invite"`              // the invite link, with the address the browser used
	Status    Status     `json:"status"`              // who uses and reserved which devices
	SSO       *SSOView   `json:"sso,omitempty"`       // single sign-on (owner only; never the secret)
	SSOInvite string     `json:"ssoInvite,omitempty"` // an invite without the key, for single sign-on
}

// SSOView is the single sign-on setup as the admin page shows it.
type SSOView struct {
	SSOConfig
	HasSecret bool `json:"hasSecret"`
}

func (s *Server) adminState(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can manage the team")
		return
	}
	t := s.teamView(me)
	// the latest events (the Activity page asks for the rest a page at a time)
	n := min(len(s.st.Activity), 50)
	act := make([]Activity, n)
	for i := 0; i < n; i++ { // newest first
		act[i] = s.st.Activity[len(s.st.Activity)-1-i]
	}
	inv := Inventory{Rev: s.st.Rev, Devices: s.st.Inventory, Folders: s.st.Folders}
	if inv.Devices == nil {
		inv.Devices = []*Device{}
	}
	p := s.st.Pending
	if p == nil {
		p = []*Pending{}
	}
	link := Invitation{Address: r.Host, Key: s.st.Invite, Fingerprint: s.fp, Name: s.st.Name}.String()
	s.prune()
	stat := s.statusFor(me)
	stat.Notes = []Note{}
	st := AdminState{Team: t, Inventory: inv, Pending: p, Activity: act, Me: me.ID, Invite: link, Status: stat}
	if me.Role == RoleOwner {
		st.SSO = &SSOView{}
		if c := s.st.SSO; c != nil {
			st.SSO = &SSOView{SSOConfig: *c, HasSecret: c.ClientSecret != ""}
			st.SSO.ClientSecret = ""
		}
	}
	if s.st.SSO != nil && manager(me) {
		st.SSOInvite = Invitation{Address: r.Host, Fingerprint: s.fp, Name: s.st.Name}.String()
	}
	writeJSON(w, 200, st)
}

// saveDevices adds or changes devices (an empty id adds).
func (s *Server) saveDevices(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can change the inventory")
		return
	}
	var b devicesBody
	if json.NewDecoder(r.Body).Decode(&b) != nil || len(b.Devices) > 5000 {
		fail(w, 400, "bad request")
		return
	}
	now := s.now().UTC().Format(time.RFC3339)
	added, changed, skipped := 0, 0, 0
	for i := range b.Devices {
		d := b.Devices[i]
		if msg := tidy(&d); msg != "" {
			fail(w, 400, d.Name+": "+msg)
			return
		}
		var cur *Device
		for _, x := range s.st.Inventory {
			if x.ID == d.ID && d.ID != "" {
				cur = x
			}
		}
		for _, x := range s.st.Inventory {
			if x != cur && same(x, &d) {
				if len(b.Devices) == 1 {
					fail(w, 409, "the team has this device already: "+x.Name+" ("+x.Folder+")")
					return
				}
				cur = nil
				skipped++
				goto next
			}
		}
		if cur == nil {
			d.ID, d.AddedBy, d.Added, d.Updated = randToken(9), me.Name, now, now
			s.st.Inventory = append(s.st.Inventory, &d)
			added++
		} else {
			d.ID, d.AddedBy, d.Added, d.Updated = cur.ID, cur.AddedBy, cur.Added, now
			if d.Hostname == "" {
				d.Hostname = cur.Hostname
			}
			if d.Model == "" {
				d.Model = cur.Model
			}
			*cur = d
			changed++
		}
	next:
	}
	switch {
	case added == 1 && changed == 0:
		s.log(me.Name, "added %s", b.Devices[0].Name)
	case changed == 1 && added == 0:
		s.log(me.Name, "changed %s", b.Devices[0].Name)
	default:
		s.log(me.Name, "added %d and changed %d devices (%d already there)", added, changed, skipped)
	}
	if err := s.bump(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]int{"added": added, "changed": changed, "skipped": skipped})
}

type idsBody struct {
	IDs    []string `json:"ids"`
	Folder *string  `json:"folder,omitempty"`
}

func (s *Server) deleteDevices(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can change the inventory")
		return
	}
	var b idsBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	del := map[string]bool{}
	for _, id := range b.IDs {
		del[id] = true
	}
	var keep []*Device
	var names []string
	for _, d := range s.st.Inventory {
		if del[d.ID] {
			names = append(names, d.Name)
		} else {
			keep = append(keep, d)
		}
	}
	s.st.Inventory = keep
	if len(names) == 1 {
		s.log(me.Name, "deleted %s", names[0])
	} else if len(names) > 1 {
		s.log(me.Name, "deleted %d devices", len(names))
	}
	if err := s.bump(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]int{"deleted": len(names)})
}

// saveFolder makes a folder ({"folder": "a/b"}), renames one ({"from", "folder"}) or deletes an empty one ({"from"}).
func (s *Server) saveFolder(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can change the inventory")
		return
	}
	var b struct {
		From   string `json:"from"`
		Folder string `json:"folder"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	from, to := cleanFolder(b.From), cleanFolder(b.Folder)
	under := func(f, root string) bool { return f == root || strings.HasPrefix(f, root+"/") }
	var fs []string
	for _, f := range s.st.Folders {
		if from == "" || !under(f, from) {
			fs = append(fs, f)
		} else if to != "" {
			fs = append(fs, to+strings.TrimPrefix(f, from))
		}
	}
	if from != "" {
		n := 0
		for _, d := range s.st.Inventory {
			if under(d.Folder, from) {
				if to == "" {
					fail(w, 409, "the folder has devices - move or delete them first")
					return
				}
				d.Folder = to + strings.TrimPrefix(d.Folder, from)
				n++
			}
		}
		if to == "" {
			s.log(me.Name, "deleted the folder %s", from)
		} else {
			s.log(me.Name, "renamed the folder %s to %s (%d devices)", from, to, n)
		}
	} else if to != "" {
		fs = append(fs, to)
		s.log(me.Name, "made the folder %s", to)
	}
	sort.Strings(fs)
	s.st.Folders = dedupeStrings(fs)
	if err := s.bump(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func dedupeStrings(l []string) []string {
	var out []string
	for i, v := range l {
		if v != "" && (i == 0 || l[i-1] != v) {
			out = append(out, v)
		}
	}
	return out
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request, me *memberRec) {
	s.decide(w, r, me, true)
}
func (s *Server) reject(w http.ResponseWriter, r *http.Request, me *memberRec) {
	s.decide(w, r, me, false)
}

// decide approves (into the inventory, optionally into another folder) or rejects pushed devices.
func (s *Server) decide(w http.ResponseWriter, r *http.Request, me *memberRec, ok bool) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can approve")
		return
	}
	var b idsBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	pick := map[string]bool{}
	for _, id := range b.IDs {
		pick[id] = true
	}
	now := s.now().UTC().Format(time.RFC3339)
	var keep []*Pending
	n, dup := 0, 0
	for _, p := range s.st.Pending {
		if !pick[p.ID] {
			keep = append(keep, p)
			continue
		}
		if !ok {
			n++
			continue
		}
		d := p.Device
		if b.Folder != nil {
			d.Folder = cleanFolder(*b.Folder)
		}
		if s.find2(&d) != nil {
			dup++
			continue
		}
		d.ID, d.AddedBy, d.Added, d.Updated = randToken(9), p.Name, now, now
		s.st.Inventory = append(s.st.Inventory, &d)
		n++
	}
	s.st.Pending = keep
	if ok {
		s.log(me.Name, "approved %d device%s", n, plural(n))
		if err := s.bump(); err != nil {
			fail(w, 500, err.Error())
			return
		}
	} else {
		s.log(me.Name, "rejected %d device%s", n, plural(n))
		_ = s.save()
	}
	writeJSON(w, 200, map[string]int{"done": n, "alreadyThere": dup})
}

// ---- scans from the server ----

type scanState struct {
	ID      string            `json:"id"`
	Targets string            `json:"targets"`
	By      string            `json:"by"`
	Running bool              `json:"running"`
	Done    int               `json:"done"`
	Total   int               `json:"total"`
	Results []discover.Result `json:"results"`
	Error   string            `json:"error,omitempty"`
	Started string            `json:"started"`
	cancel  context.CancelFunc
}

// ScanView is the scan as the admin page sees it.
type ScanView struct {
	*scanState
	InTeam map[string]string `json:"inTeam"`
}

func (s *Server) startScan(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can scan")
		return
	}
	var b struct {
		Targets   string `json:"targets"`
		Community string `json:"community"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if s.scan != nil && s.scan.Running {
		fail(w, 409, "a scan is running ("+s.scan.Targets+" by "+s.scan.By+")")
		return
	}
	addrs, err := discover.ParseTargets(b.Targets)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	sc := &scanState{ID: randToken(6), Targets: strings.TrimSpace(b.Targets), By: me.Name, Running: true, Total: len(addrs),
		Started: s.now().UTC().Format(time.RFC3339), Results: []discover.Result{}, cancel: cancel}
	s.scan = sc
	s.log(me.Name, "scans %s from the server", sc.Targets)
	opt := discover.Options{Community: strings.TrimSpace(b.Community)}
	go func() {
		defer cancel()
		discover.Scan(ctx, addrs, opt, func(res discover.Result) {
			s.mu.Lock()
			defer s.mu.Unlock()
			for i := range sc.Results {
				if sc.Results[i].IP == res.IP {
					sc.Results[i] = res
					return
				}
			}
			sc.Results = append(sc.Results, res)
		}, func(done, total int) {
			s.mu.Lock()
			sc.Done, sc.Total = done, total
			s.mu.Unlock()
		})
		s.mu.Lock()
		sc.Running = false
		if s.namesFromScan(sc.Results) > 0 {
			_ = s.save()
		}
		sort.Slice(sc.Results, func(i, j int) bool {
			a, _ := netip.ParseAddr(sc.Results[i].IP)
			b, _ := netip.ParseAddr(sc.Results[j].IP)
			return a.Less(b)
		})
		s.mu.Unlock()
	}()
	writeJSON(w, 200, map[string]string{"id": sc.ID})
}

func (s *Server) scanStatus(w http.ResponseWriter, _ *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can scan")
		return
	}
	if s.scan == nil {
		writeJSON(w, 200, nil)
		return
	}
	in := map[string]string{}
	for _, r := range s.scan.Results {
		port := r.Port
		if port == 0 {
			port = DefaultPortOf(r.Protocol)
		}
		if x := s.find2(&Device{Host: r.IP, Port: port, Protocol: r.Protocol, MAC: discover.NormalizeMAC(r.MAC)}); x != nil {
			in[r.IP] = x.Name
		}
	}
	writeJSON(w, 200, ScanView{scanState: s.scan, InTeam: in})
}

func (s *Server) stopScan(w http.ResponseWriter, _ *http.Request, me *memberRec) {
	if s.scan != nil && s.scan.cancel != nil && manager(me) {
		s.scan.cancel()
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// SameDevice: the same address and port, or the same MAC address.
func SameDevice(a, b *Device) bool { return same(a, b) }

// Dev is the device a reference names.
func (r DeviceRef) Dev() *Device { return r.dev() }

// namesFromScan gives inventory devices without a hostname the name they told
// the scan (SNMP sysName); returns how many got one (mu held).
func (s *Server) namesFromScan(results []discover.Result) int {
	n := 0
	for _, r := range results {
		name := clean(r.SysName, 80)
		if name == "" || isIP(name) {
			continue
		}
		for _, d := range s.st.Inventory {
			if d.Host == r.IP && d.Hostname == "" {
				d.Hostname = name
				n++
			}
		}
	}
	return n
}

func isIP(s string) bool { _, err := netip.ParseAddr(s); return err == nil }

// deviceFacts: a member's Termwright read a team device's hostname and model.
func (s *Server) deviceFacts(w http.ResponseWriter, r *http.Request, me *memberRec) {
	var b struct {
		Device   DeviceRef `json:"device"`
		Hostname string    `json:"hostname"`
		Model    string    `json:"model"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&b) != nil || tidyRef(&b.Device) != "" {
		fail(w, 400, "bad request")
		return
	}
	h, m := clean(b.Hostname, 80), clean(b.Model, 80)
	if isIP(h) {
		h = ""
	}
	d := s.find2(b.Device.dev())
	if d == nil || (h == "" || h == d.Hostname) && (m == "" || m == d.Model) {
		writeJSON(w, 200, map[string]bool{"changed": false})
		return
	}
	if h != "" {
		d.Hostname = h
	}
	if m != "" {
		d.Model = m
	}
	if err := s.save(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"changed": true})
}
