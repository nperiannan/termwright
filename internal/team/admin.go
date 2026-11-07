package team

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"time"
)

// The admin page (/admin) is for owners and co-owners only, and only opened
// from Termwright: its Teams window asks for a sign-in link (POST
// /api/admin/link with the member's key), valid once and for two minutes, and
// opens it in the browser. The page trades it for a cookie of its own (eight
// hours). There are no passwords on the server.

//go:embed admin.html
var adminHTML []byte

//go:embed icon.svg
var iconSVG []byte

const (
	linkFor    = 2 * time.Minute
	sessionFor = 8 * time.Hour
	cookieName = "tw_admin"
)

type linkCode struct {
	member string
	until  time.Time
}

type adminSession struct {
	member string
	until  time.Time
}

// cookieMember is the owner or co-owner of the request's admin cookie (mu held).
func (s *Server) cookieMember(r *http.Request) *memberRec {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	as, ok := s.sessions[hashToken(c.Value)]
	if !ok || s.now().After(as.until) {
		return nil
	}
	_, m := s.find(as.member)
	if m == nil || !manager(m) {
		return nil
	}
	return m
}

// AdminLink is a one-time sign-in for the admin page.
type AdminLink struct {
	Path string `json:"path"` // "/admin#c=…"
}

func (s *Server) adminLink(w http.ResponseWriter, _ *http.Request, me *memberRec) {
	if !manager(me) {
		fail(w, 403, "only the owner and co-owners can open the admin page")
		return
	}
	now := s.now()
	for k, l := range s.links {
		if now.After(l.until) {
			delete(s.links, k)
		}
	}
	code := randToken(24)
	s.links[hashToken(code)] = linkCode{member: me.ID, until: now.Add(linkFor)}
	writeJSON(w, 200, AdminLink{Path: "/admin#c=" + code})
}

func (s *Server) adminSignIn(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	l, ok := s.links[hashToken(b.Code)]
	delete(s.links, hashToken(b.Code))
	_, m := s.find(l.member)
	if !ok || now.After(l.until) || m == nil || !manager(m) {
		fail(w, 401, "the sign-in link is used or expired - open the admin page again from Termwright (Options → Teams…)")
		return
	}
	for k, as := range s.sessions {
		if now.After(as.until) {
			delete(s.sessions, k)
		}
	}
	tok := randToken(32)
	s.sessions[hashToken(tok)] = adminSession{member: m.ID, until: now.Add(sessionFor)}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: tok, Path: "/", MaxAge: int(sessionFor / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	// a sign-in is noted once per admin session's length, not each time the page opens
	if last, ok := s.signIns[m.ID]; !ok || now.Sub(last) > sessionFor {
		s.signIns[m.ID] = now
		s.log(m.Name, "signed in to the admin page")
	}
	writeJSON(w, 200, map[string]string{"name": m.Name, "role": m.Role})
}

func (s *Server) adminSignOut(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.sessions, hashToken(c.Value))
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminIcon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "max-age=86400")
	_, _ = w.Write(iconSVG)
}

func (s *Server) adminPage(w http.ResponseWriter, _ *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
	_, _ = w.Write(adminHTML)
}
