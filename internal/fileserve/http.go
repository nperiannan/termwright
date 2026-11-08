package fileserve

import (
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

// HTTP: downloads only (GET and HEAD), with ranges; a folder shows a plain list.

type httpServer struct {
	ln  net.Listener
	srv *http.Server
}

func (s *httpServer) addr() net.Addr { return s.ln.Addr() }
func (s *httpServer) stop()          { s.srv.Close() }

func (m *Manager) startHTTP(addr string) (runner, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: http.HandlerFunc(m.serveHTTP), ReadHeaderTimeout: 20 * time.Second, IdleTimeout: 2 * time.Minute}
	go srv.Serve(ln)
	return &httpServer{ln: ln, srv: srv}, nil
}

type countReadSeeker struct {
	f  *os.File
	fn func(int64)
}

func (c countReadSeeker) Read(p []byte) (int, error) {
	n, err := c.f.Read(p)
	c.fn(int64(n))
	return n, err
}

func (c countReadSeeker) Seek(o int64, w int) (int64, error) { return c.f.Seek(o, w) }

func (m *Manager) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Server", "Termwright")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "This server only sends files.", http.StatusMethodNotAllowed)
		if r.Method == http.MethodPut || r.Method == http.MethodPost {
			m.refuse("HTTP", remote(r), r.URL.Path, true, "Refused - downloads only")
		}
		return
	}
	n, err := CleanName(r.URL.Path)
	if err != nil {
		m.refuse("HTTP", remote(r), r.URL.Path, false, "Refused - outside the folder")
		http.Error(w, "Not allowed.", http.StatusForbidden)
		return
	}
	root := m.folder()
	if root == nil {
		http.Error(w, "The server is stopping.", http.StatusServiceUnavailable)
		return
	}
	dir := n
	if dir == "" {
		dir = "."
	}
	f, err := root.Open(dir)
	if err != nil {
		if r.Method == http.MethodGet && n != "" && !strings.HasSuffix(n, "favicon.ico") {
			m.refuse("HTTP", remote(r), filepathSlash(n), false, "Not found")
		}
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if fi.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		des, _ := f.ReadDir(-1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<!doctype html><meta charset=utf-8><title>Termwright files</title><h3>/%s</h3><ul>", html.EscapeString(filepathSlash(n)))
		for _, d := range des {
			if strings.HasPrefix(d.Name(), ".") {
				continue
			}
			name := d.Name()
			if d.IsDir() {
				name += "/"
			}
			fmt.Fprintf(w, "<li><a href=\"%s\">%s</a></li>", (&url.URL{Path: name}).String(), html.EscapeString(name))
		}
		fmt.Fprint(w, "</ul>")
		return
	}
	if !fi.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodHead {
		http.ServeContent(w, r, path.Base(filepathSlash(n)), fi.ModTime(), f)
		return
	}
	xf := m.begin("HTTP", remote(r), filepathSlash(n), false, fi.Size())
	rec := &statusRec{ResponseWriter: w, code: 200}
	http.ServeContent(rec, r, path.Base(filepathSlash(n)), fi.ModTime(), countReadSeeker{f, func(k int64) { m.progress(xf, k) }})
	if rec.code >= 400 {
		m.finish(xf, "error", fmt.Errorf("HTTP %d", rec.code))
		return
	}
	if rec.err != nil {
		m.finish(xf, "error", rec.err)
		return
	}
	if rec.code == http.StatusPartialContent || rec.code == http.StatusNotModified {
		m.mu.Lock()
		xf.Size = xf.Bytes
		m.mu.Unlock()
	}
	m.finish(xf, "done", nil)
}

type statusRec struct {
	http.ResponseWriter
	code int
	err  error
}

func (s *statusRec) WriteHeader(c int) { s.code = c; s.ResponseWriter.WriteHeader(c) }
func (s *statusRec) Write(p []byte) (int, error) {
	n, err := s.ResponseWriter.Write(p)
	if err != nil && s.err == nil {
		s.err = err
	}
	return n, err
}

func remote(r *http.Request) net.Addr {
	a, err := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	if err != nil {
		return &net.TCPAddr{}
	}
	return a
}

var _ io.Reader = countReadSeeker{}
