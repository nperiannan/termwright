package team

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Watch: a member waits for a device to be free (nobody else has it open and
// nobody else reserved it); they get a notice then.
type Watch struct {
	ID     string    `json:"id"`
	Device DeviceRef `json:"device"`
	By     string    `json:"by"`
	Name   string    `json:"name"`
	At     string    `json:"at"`
}

// watchFor is how long a member waits at most.
const watchFor = 24 * time.Hour

func (s *Server) moreRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/activity", s.auth(s.activityPage))
	mux.HandleFunc("GET /api/admin/backup", s.auth(s.backup))
	mux.HandleFunc("POST /api/admin/restore", s.auth(s.restore))
	mux.HandleFunc("POST /api/watch", s.auth(s.watch))
}

// activityPage: the activity log, newest first, filtered by q (who or what), a page at a time.
func (s *Server) activityPage(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can see the activity")
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	lim, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if lim <= 0 || lim > maxActivity {
		lim = 200
	}
	var out []Activity
	for i := len(s.st.Activity) - 1; i >= 0; i-- {
		a := s.st.Activity[i]
		if q == "" || strings.Contains(strings.ToLower(a.Who+" "+a.What), q) {
			out = append(out, a)
		}
	}
	total := len(out)
	if off > total {
		off = total
	}
	end := min(off+lim, total)
	page := out[off:end]
	if page == nil {
		page = []Activity{}
	}
	writeJSON(w, 200, map[string]any{"total": total, "items": page})
}

// watch starts or stops waiting for a device to be free.
func (s *Server) watch(w http.ResponseWriter, r *http.Request, me *memberRec) {
	var b struct {
		Device DeviceRef `json:"device"`
		On     bool      `json:"on"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil || tidyRef(&b.Device) != "" {
		fail(w, 400, "bad request")
		return
	}
	inv := s.find2(b.Device.dev())
	if inv == nil {
		fail(w, 404, "this device is not in the team's inventory")
		return
	}
	ref := DeviceRef{Host: inv.Host, Port: inv.Port, Protocol: inv.Protocol, MAC: inv.MAC, Name: inv.Name}
	if ref.Port == 0 {
		ref.Port = DefaultPortOf(inv.Protocol)
	}
	keep := s.st.Watches[:0]
	for _, x := range s.st.Watches {
		if !(x.By == me.ID && same(x.Device.dev(), ref.dev())) {
			keep = append(keep, x)
		}
	}
	s.st.Watches = keep
	if b.On {
		if s.freeFor(ref, me) {
			writeJSON(w, 200, map[string]any{"free": true})
			return
		}
		s.st.Watches = append(s.st.Watches, &Watch{ID: randToken(9), Device: ref, By: me.ID, Name: me.Name, At: s.now().UTC().Format(time.RFC3339)})
	}
	if err := s.save(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	s.wake(true)
	writeJSON(w, 200, map[string]any{"free": false})
}

// freeFor: nobody but me (on any of my computers) has the device open or reserved (mu held).
func (s *Server) freeFor(d DeviceRef, me *memberRec) bool {
	if r := s.reservationOf(d); r != nil && !s.same(r.By, me) {
		return false
	}
	for id, p := range s.presence {
		if s.same(id, me) {
			continue
		}
		for _, x := range p.devices {
			if same(x.dev(), d.dev()) {
				return false
			}
		}
	}
	return true
}

// checkWatches tells the members who wait for a device that it is free now,
// and drops waits older than watchFor (mu held).
func (s *Server) checkWatches() {
	if len(s.st.Watches) == 0 {
		return
	}
	keep, changed := s.st.Watches[:0], false
	for _, x := range s.st.Watches {
		at, _ := time.Parse(time.RFC3339, x.At)
		_, m := s.find(x.By)
		switch {
		case m == nil || s.now().Sub(at) > watchFor:
			changed = true
		case s.freeFor(x.Device, m):
			s.note(x.By, "%s is free now.", nameOf(x.Device))
			changed = true
		default:
			keep = append(keep, x)
		}
	}
	s.st.Watches = keep
	if changed {
		_ = s.save()
	}
}

// the files a backup holds
var backupFiles = []string{"team.json", "usage.json", "cert.pem", "key.pem"}

// backup: the team's data as a .tar.gz - team, members, inventory, usage
// history and the server's certificate and key (owner only).
func (s *Server) backup(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if me.Role != RoleOwner {
		fail(w, 403, "only the owner can download a backup")
		return
	}
	s.saveUsage()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range backupFiles {
		b, err := os.ReadFile(filepath.Join(s.Dir, f))
		if err != nil {
			continue
		}
		_ = tw.WriteHeader(&tar.Header{Name: f, Mode: 0o600, Size: int64(len(b)), ModTime: s.now()})
		_, _ = tw.Write(b)
	}
	_ = tw.Close()
	_ = gz.Close()
	s.log(me.Name, "downloaded a backup")
	_ = s.save()
	name := fmt.Sprintf("termwright-team-%s-%s.tar.gz", slug(s.st.Name), s.now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

// restore puts a backup back: the team, members, inventory, reservations and
// usage history as they were then, and the backup's certificate (owner only).
func (s *Server) restore(w http.ResponseWriter, r *http.Request, me *memberRec) {
	if me.Role != RoleOwner {
		fail(w, 403, "only the owner can restore a backup")
		return
	}
	files, err := readBackup(http.MaxBytesReader(w, r.Body, 64<<20))
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	var st state
	if err := json.Unmarshal(files["team.json"], &st); err != nil || st.Name == "" {
		fail(w, 400, "the backup's team.json cannot be read")
		return
	}
	var u usageFile
	if b, ok := files["usage.json"]; ok && json.Unmarshal(b, &u) != nil {
		fail(w, 400, "the backup's usage.json cannot be read")
		return
	}
	newCert := false
	if c, k := files["cert.pem"], files["key.pem"]; c != nil || k != nil {
		pair, err := tls.X509KeyPair(c, k)
		if err != nil {
			fail(w, 400, "the backup's certificate and key do not belong together")
			return
		}
		sum := sha256.Sum256(pair.Certificate[0])
		if fp := hex.EncodeToString(sum[:]); fp != s.fp {
			newCert = true
			for _, f := range []string{"cert.pem", "key.pem"} {
				if err := os.WriteFile(filepath.Join(s.Dir, f), files[f], 0o600); err != nil {
					fail(w, 500, err.Error())
					return
				}
			}
			s.cert, s.fp = pair, fp
		}
	}
	keepMe := false
	for _, m := range st.Members {
		if m.ID == me.ID && m.Role == RoleOwner {
			keepMe = true
		}
	}
	s.st = st
	s.usage = u
	s.opens = map[string]*openUse{}
	s.presence = map[string]*presenceRec{}
	s.log(me.Name, "restored a backup of %s", timeMark(s.now()))
	if err := s.save(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	s.saveUsage()
	s.st.Rev++
	close(s.changed)
	s.changed = make(chan struct{})
	s.wake(true)
	writeJSON(w, 200, map[string]any{"newCertificate": newCert, "signedOut": !keepMe})
}

// readBackup reads the files of a backup .tar.gz (only the known ones).
func readBackup(r io.Reader) (map[string][]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("this is not a team server backup (.tar.gz)")
	}
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the backup is damaged: %v", err)
		}
		name := filepath.Base(h.Name)
		for _, f := range backupFiles {
			if name == f {
				b, err := io.ReadAll(io.LimitReader(tr, 64<<20))
				if err != nil {
					return nil, err
				}
				out[f] = b
			}
		}
	}
	if out["team.json"] == nil {
		return nil, fmt.Errorf("the backup has no team.json")
	}
	return out, nil
}

func slug(n string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(n) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
