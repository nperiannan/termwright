package team

import (
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// onlineFor: a member counts as online this long after its last call.
const onlineFor = 2 * time.Minute

type memberRec struct {
	Member
	TokenHash string `json:"tokenHash"`
}

// state is what the server keeps in team.json.
type state struct {
	Name          string         `json:"name"`
	Created       string         `json:"created"`
	Invite        string         `json:"invite"`
	OwnerCodeHash string         `json:"ownerCodeHash,omitempty"` // until the owner joins
	Members       []*memberRec   `json:"members"`
	Rev           int64          `json:"rev"` // inventory revision: grows with every change
	Inventory     []*Device      `json:"inventory"`
	Folders       []string       `json:"folders,omitempty"`  // folders made on the admin page, also when empty
	Pending       []*Pending     `json:"pending,omitempty"`  // devices members pushed, waiting for approval
	Activity      []Activity     `json:"activity,omitempty"` // newest last, at most maxActivity
	Reservations  []*Reservation `json:"reservations,omitempty"`
	Watches       []*Watch       `json:"watches,omitempty"` // members waiting for a device to be free
}

// Server is a team server. Its data (team.json, the certificate and key) is in Dir.
type Server struct {
	Dir     string
	Version string
	Log     *log.Logger

	mu         sync.Mutex
	st         state
	cert       tls.Certificate
	fp         string
	signIns    map[string]time.Time // the last admin sign-in noted per member
	usageDirty bool                 // usage.json to be written (flushLoop)
	usageWrite sync.Mutex           // one writer of usage.json at a time
	lastUse    map[string]UseRec    // per device its last closed session (lastUses)
	lastRev    int64                // changes when Status.Last does
	seen       map[string]time.Time // member id → last call (not saved on every call)
	fails      map[string]time.Time // remote address → last failed join
	now        func() time.Time

	changed  chan struct{}           // closed and replaced when the inventory changes (long polls wait on it)
	links    map[string]linkCode     // admin sign-in codes
	sessions map[string]adminSession // admin page cookies
	scan     *scanState

	presence  map[string]*presenceRec // member id → the devices it has open (not saved)
	notes     map[string][]Note       // member id → notices waiting for it
	statusRev int64
	statusCh  chan struct{} // closed and replaced when the status changes

	usage usageFile           // usage history (usage.json)
	opens map[string]*openUse // member|device → open now
}

// Open loads the team in dir, or makes a new one named name: a certificate, an
// invite key and the one-time owner setup code, which is returned (and only
// then; "" when the team exists or already has an owner).
func Open(dir, name, version string, logger *log.Logger) (*Server, string, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	s := &Server{Dir: dir, Version: version, Log: logger, seen: map[string]time.Time{}, fails: map[string]time.Time{}, now: time.Now,
		changed: make(chan struct{}), links: map[string]linkCode{}, sessions: map[string]adminSession{},
		presence: map[string]*presenceRec{}, notes: map[string][]Note{}, statusCh: make(chan struct{}), signIns: map[string]time.Time{}}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	if err := s.loadCert(); err != nil {
		return nil, "", err
	}
	s.loadUsage()
	code := ""
	b, err := os.ReadFile(filepath.Join(dir, "team.json"))
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &s.st); err != nil {
			return nil, "", fmt.Errorf("team.json: %w", err)
		}
		if s.ownerID() == "" {
			// no owner yet (the setup code was lost with the logs): a new code
			code = randToken(18)
			s.st.OwnerCodeHash = hashToken(code)
		}
	case errors.Is(err, os.ErrNotExist):
		if strings.TrimSpace(name) == "" {
			name = "Team"
		}
		code = randToken(18)
		s.st = state{Name: strings.TrimSpace(name), Created: s.now().UTC().Format(time.RFC3339), Invite: randToken(18), OwnerCodeHash: hashToken(code)}
	default:
		return nil, "", err
	}
	if err := s.save(); err != nil {
		return nil, "", err
	}
	go s.flushLoop()
	return s, code, nil
}

// RecoverFile is made in the data folder by "termwright-team -recover-owner"
// (run with docker exec): the server then prints a new owner setup code, for
// an owner whose Termwright lost the team. Only who can run commands on the
// host can do it.
const RecoverFile = "recover-owner"

// WatchRecovery checks for RecoverFile every two seconds until ctx ends.
func (s *Server) WatchRecovery(stop <-chan struct{}) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		f := filepath.Join(s.Dir, RecoverFile)
		if _, err := os.Stat(f); err != nil {
			continue
		}
		_ = os.Remove(f)
		code := randToken(18)
		s.mu.Lock()
		s.st.OwnerCodeHash = hashToken(code)
		_ = s.save()
		s.mu.Unlock()
		s.Log.Printf("TERMWRIGHT-TEAM owner-code=%s fingerprint=%s (recovery: the next to join with it is the owner)", code, s.fp)
	}
}

// Fingerprint is the SHA-256 of the server's certificate (hex), for invites.
func (s *Server) Fingerprint() string { return s.fp }

// TLSConfig serves the server's own certificate.
func (s *Server) TLSConfig() *tls.Config {
	// the certificate is read at each connection: a restored backup's applies at once
	cur := func() tls.Certificate {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.cert
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cur()},
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cur()}}, nil
		}}
}

func (s *Server) loadCert() error {
	cf, kf := filepath.Join(s.Dir, "cert.pem"), filepath.Join(s.Dir, "key.pem")
	if c, err := tls.LoadX509KeyPair(cf, kf); err == nil {
		s.cert = c
	} else {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
		tpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Termwright team server"},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(20, 0, 0),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
		if err != nil {
			return err
		}
		kd, _ := x509.MarshalECPrivateKey(key)
		if err := os.WriteFile(kf, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(cf, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
			return err
		}
		if s.cert, err = tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})); err != nil {
			return err
		}
	}
	sum := sha256.Sum256(s.cert.Certificate[0])
	s.fp = hex.EncodeToString(sum[:])
	return nil
}

// save writes team.json (the caller holds mu, or nobody else runs yet).
func (s *Server) save() error {
	b, err := json.Marshal(s.st) // compact: a big lab's inventory is MBs
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.Dir, "team.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Dir, "team.json"))
}

func (s *Server) ownerID() string {
	for _, m := range s.st.Members {
		if m.Role == RoleOwner {
			return m.ID
		}
	}
	return ""
}

// Handler is the HTTP API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("POST /api/join", s.join)
	mux.HandleFunc("GET /api/team", s.auth(s.team))
	mux.HandleFunc("POST /api/invite/rotate", s.auth(s.rotate))
	mux.HandleFunc("POST /api/members/role", s.auth(s.setRole))
	mux.HandleFunc("POST /api/members/remove", s.auth(s.remove))
	mux.HandleFunc("POST /api/leave", s.auth(s.leave))
	s.moreRoutes(mux)
	s.inventoryRoutes(mux)
	limited := http.MaxBytesHandler(mux, 16<<20)
	return gzipJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/admin/restore" {
			mux.ServeHTTP(w, r) // a backup carries the usage history: limited there (256 MB)
			return
		}
		limited.ServeHTTP(w, r)
	}))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) online(id string) bool {
	t, ok := s.seen[id]
	return ok && s.now().Sub(t) < onlineFor
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.st.Members {
		if s.online(m.ID) {
			n++
		}
	}
	writeJSON(w, 200, Info{Name: s.st.Name, Version: s.Version, Members: len(s.st.Members), Online: n, Owner: s.ownerID() != ""})
}

func clean(v string, max int) string {
	v = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if len([]rune(v)) > max {
		v = string([]rune(v)[:max])
	}
	return v
}

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	var req JoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "bad request")
		return
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.fails[host]; ok && s.now().Sub(t) < 2*time.Second {
		fail(w, 429, "too many attempts - wait a moment")
		return
	}
	role := ""
	if req.OwnerCode == "" && s.st.OwnerCodeHash != "" {
		req.OwnerCode = req.Invite // a hand-made invite with the setup code (manual installs)
	}
	switch {
	case req.OwnerCode != "" && s.st.OwnerCodeHash != "" && subtle.ConstantTimeCompare([]byte(hashToken(req.OwnerCode)), []byte(s.st.OwnerCodeHash)) == 1:
		role = RoleOwner
	case req.Invite != "" && subtle.ConstantTimeCompare([]byte(req.Invite), []byte(s.st.Invite)) == 1:
		role = RoleMember
	default:
		s.fails[host] = s.now()
		fail(w, 403, "the invite is not valid any more - ask the team's owner for a new one")
		return
	}
	name := clean(req.Name, 60)
	if name == "" {
		name = clean(req.User, 60)
	}
	now := s.now().UTC().Format(time.RFC3339)
	tok := randToken(32)
	m := &memberRec{Member: Member{ID: randToken(9), Name: name, User: clean(req.User, 60), Machine: clean(req.Machine, 80), Role: role, Joined: now, LastSeen: now}, TokenHash: hashToken(tok)}
	s.st.Members = append(s.st.Members, m)
	if role == RoleOwner {
		s.st.OwnerCodeHash = ""
		for _, o := range s.st.Members { // a recovered owner: the one before stays as co-owner
			if o.Role == RoleOwner && o != m {
				o.Role = RoleCoOwner
				s.log(name, "took over as owner from %s (now co-owner)", o.Name)
			}
		}
	}
	if err := s.save(); err != nil {
		fail(w, 500, "could not save: "+err.Error())
		return
	}
	s.seen[m.ID] = s.now()
	s.Log.Printf("joined: %s (%s@%s) as %s", m.Name, m.User, m.Machine, role)
	writeJSON(w, 200, Joined{MemberID: m.ID, Token: tok, Role: role, Team: s.st.Name})
}

// auth finds the member of the request's token; handlers run with mu held.
func (s *Server) auth(h func(http.ResponseWriter, *http.Request, *memberRec)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.Lock()
		defer s.mu.Unlock()
		if tok == "" {
			if m := s.cookieMember(r); m != nil {
				if r.Method != http.MethodGet && r.Header.Get("X-Termwright") != "1" {
					fail(w, 403, "missing request header")
					return
				}
				s.seen[m.ID] = s.now()
				h(w, r, m)
				return
			}
		}
		if tok != "" {
			th := hashToken(tok)
			for _, m := range s.st.Members {
				if subtle.ConstantTimeCompare([]byte(th), []byte(m.TokenHash)) == 1 {
					s.seen[m.ID] = s.now()
					if t, err := time.Parse(time.RFC3339, m.LastSeen); err != nil || s.now().Sub(t) > time.Hour {
						m.LastSeen = s.now().UTC().Format(time.RFC3339)
						_ = s.save()
					}
					h(w, r, m)
					return
				}
			}
		}
		fail(w, 401, "not a member of this team (any more)")
	}
}

func manager(m *memberRec) bool { return m.Role == RoleOwner || m.Role == RoleCoOwner }

func (s *Server) team(w http.ResponseWriter, _ *http.Request, me *memberRec) {
	writeJSON(w, 200, s.teamView(me))
}

// teamView is the team as me sees it (mu held).
func (s *Server) teamView(me *memberRec) Team {
	t := Team{Name: s.st.Name, Version: s.Version, Me: me.ID, Role: me.Role}
	if manager(me) {
		t.Invite = s.st.Invite
	}
	for _, m := range s.st.Members {
		v := m.Member
		v.Online = s.online(m.ID)
		if t, ok := s.seen[m.ID]; ok {
			v.LastSeen = t.UTC().Format(time.RFC3339)
		}
		t.Members = append(t.Members, v)
	}
	rank := map[string]int{RoleOwner: 0, RoleCoOwner: 1, RoleMember: 2}
	sort.SliceStable(t.Members, func(i, j int) bool {
		a, b := t.Members[i], t.Members[j]
		if rank[a.Role] != rank[b.Role] {
			return rank[a.Role] < rank[b.Role]
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return t
}

func (s *Server) rotate(w http.ResponseWriter, _ *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can make a new invite")
		return
	}
	s.st.Invite = randToken(18)
	if err := s.save(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	s.Log.Printf("new invite by %s", me.Name)
	writeJSON(w, 200, map[string]string{"invite": s.st.Invite})
}

func (s *Server) find(id string) (int, *memberRec) {
	for i, m := range s.st.Members {
		if m.ID == id {
			return i, m
		}
	}
	return -1, nil
}

func (s *Server) setRole(w http.ResponseWriter, r *http.Request, me *memberRec) {
	var req RoleChange
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Role != RoleCoOwner && req.Role != RoleMember && req.Role != RoleOwner) {
		fail(w, 400, "the role is owner, co-owner or member")
		return
	}
	_, m := s.find(req.ID)
	switch {
	case m == nil:
		fail(w, 404, "no such member")
	case req.Role == RoleOwner:
		// handing the team over: the new owner, and the old one stays as co-owner
		if me.Role != RoleOwner || m.ID == me.ID {
			fail(w, 403, "only the owner can hand the team over to another member")
			return
		}
		m.Role, me.Role = RoleOwner, RoleCoOwner
		s.log(me.Name, "made %s the owner (%s is a co-owner now)", m.Name, me.Name)
		s.note(m.ID, "%s made you the owner of %s.", me.Name, s.st.Name)
		if err := s.save(); err != nil {
			fail(w, 500, err.Error())
			return
		}
		s.wake(true)
		writeJSON(w, 200, m.Member)
	case !manager(me):
		fail(w, 403, "only the owner and co-owners can change roles")
	case m.Role == RoleOwner:
		fail(w, 403, "the owner stays the owner")
	case me.Role != RoleOwner && m.Role == RoleCoOwner && m.ID != me.ID:
		fail(w, 403, "only the owner can change another co-owner")
	default:
		m.Role = req.Role
		s.log(me.Name, "made %s a %s", m.Name, m.Role)
		if err := s.save(); err != nil {
			fail(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, m.Member)
	}
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request, me *memberRec) {
	var req MemberRef
	_ = json.NewDecoder(r.Body).Decode(&req)
	i, m := s.find(req.ID)
	switch {
	case m == nil:
		fail(w, 404, "no such member")
	case !manager(me):
		fail(w, 403, "only the owner and co-owners can remove members")
	case m.Role == RoleOwner:
		fail(w, 403, "the owner cannot be removed")
	case me.Role != RoleOwner && m.Role == RoleCoOwner:
		fail(w, 403, "only the owner can remove a co-owner")
	default:
		s.drop(i)
		if err := s.save(); err != nil {
			fail(w, 500, err.Error())
			return
		}
		s.Log.Printf("%s removed %s", me.Name, m.Name)
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

func (s *Server) leave(w http.ResponseWriter, _ *http.Request, me *memberRec) {
	if me.Role == RoleOwner && len(s.st.Members) > 1 {
		fail(w, 403, "the owner cannot leave while others are in the team - remove them first, or remove the team server")
		return
	}
	i, _ := s.find(me.ID)
	s.drop(i)
	if me.Role == RoleOwner { // the last one: the next who sets up becomes the owner
		code := randToken(18)
		s.st.OwnerCodeHash = hashToken(code)
		s.Log.Printf("TERMWRIGHT-TEAM owner-code=%s fingerprint=%s", code, s.fp)
	}
	if err := s.save(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	s.Log.Printf("%s left", me.Name)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) drop(i int) {
	delete(s.seen, s.st.Members[i].ID)
	s.st.Members = append(s.st.Members[:i], s.st.Members[i+1:]...)
}

// gzipJSON compresses JSON answers for clients that accept it: a year of
// usage history is tens of MB, a few MB compressed.
func gzipJSON(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/admin/backup" ||
			!strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		defer gz.Close()
		h.ServeHTTP(gzipWriter{ResponseWriter: w, w: gz}, r)
	})
}

type gzipWriter struct {
	http.ResponseWriter
	w *gzip.Writer
}

func (g gzipWriter) Write(b []byte) (int, error) { return g.w.Write(b) }
func (g gzipWriter) WriteHeader(code int) {
	g.ResponseWriter.Header().Del("Content-Length")
	g.ResponseWriter.WriteHeader(code)
}
