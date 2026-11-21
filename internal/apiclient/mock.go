package apiclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Mock serves a collection's saved examples over HTTP: a request is matched by method and path (":id",
// "{id}" and "{{var}}" parts of a saved URL match any one segment); the first example of the request answers
// (an example named like the x-mock-example header is picked when there is one). Unmatched requests get 404.
type Mock struct {
	Collection string    `json:"collection"`
	Name       string    `json:"name"`
	Port       int       `json:"port"`
	Since      time.Time `json:"since"`
	Hits       int       `json:"hits"`
	Missed     int       `json:"missed"`
	srv        *http.Server
	mu         sync.Mutex
	routes     []mockRoute
}

type mockRoute struct {
	method string
	re     *regexp.Regexp
	ex     []Example
	name   string
}

var segVar = regexp.MustCompile(`^(:\w+|\{[^}/]+\}|\{\{[^}]+\}\})$`)

// mockPath turns a saved URL into a path pattern: the host and {{baseUrl}} part are dropped.
func mockPath(raw string) *regexp.Regexp {
	p := raw
	if i := strings.Index(p, "?"); i >= 0 {
		p = p[:i]
	}
	if strings.HasPrefix(p, "{{") {
		if j := strings.Index(p, "}}"); j >= 0 {
			p = p[j+2:]
		}
	} else if u, err := url.Parse(p); err == nil && u.Host != "" {
		p = u.Path
	}
	segs := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range segs {
		if segVar.MatchString(s) {
			segs[i] = `[^/]+`
		} else {
			segs[i] = regexp.QuoteMeta(s)
		}
	}
	return regexp.MustCompile(`^/` + strings.Join(segs, "/") + `/?$`)
}

// NewMock prepares a mock of the collection; requests without examples are left out.
func NewMock(c Collection) (*Mock, error) {
	m := &Mock{Collection: c.ID, Name: c.Name}
	for _, r := range c.Requests {
		if len(r.Examples) == 0 {
			continue
		}
		meth := strings.ToUpper(r.Method)
		if meth == "" {
			meth = "GET"
		}
		m.routes = append(m.routes, mockRoute{method: meth, re: mockPath(r.URL), ex: r.Examples, name: r.Name})
	}
	if len(m.routes) == 0 {
		return nil, errors.New(c.Name + " has no saved examples: send a request and Save as Example first")
	}
	// specific paths before ones with variables
	for i := 1; i < len(m.routes); i++ {
		for j := i; j > 0 && strings.Count(m.routes[j].re.String(), "[^/]+") < strings.Count(m.routes[j-1].re.String(), "[^/]+"); j-- {
			m.routes[j], m.routes[j-1] = m.routes[j-1], m.routes[j]
		}
	}
	return m, nil
}

func (m *Mock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, rt := range m.routes {
		if rt.method != r.Method || !rt.re.MatchString(r.URL.Path) {
			continue
		}
		ex := rt.ex[0]
		if want := r.Header.Get("x-mock-example"); want != "" {
			for _, e := range rt.ex {
				if strings.EqualFold(e.Name, want) {
					ex = e
				}
			}
		}
		m.mu.Lock()
		m.Hits++
		m.mu.Unlock()
		ct := false
		for _, h := range ex.Headers {
			if !h.Off && h.Key != "" && !strings.EqualFold(h.Key, "content-length") {
				w.Header().Set(h.Key, h.Value)
				ct = ct || strings.EqualFold(h.Key, "content-type")
			}
		}
		if !ct && strings.HasPrefix(strings.TrimSpace(ex.Body), "{") || !ct && strings.HasPrefix(strings.TrimSpace(ex.Body), "[") {
			w.Header().Set("Content-Type", "application/json")
		}
		st := ex.Status
		if st == 0 {
			st = 200
		}
		w.WriteHeader(st)
		_, _ = w.Write([]byte(ex.Body))
		return
	}
	m.mu.Lock()
	m.Missed++
	m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(404)
	_, _ = fmt.Fprintf(w, `{"error":"no saved example for %s %s"}`, r.Method, strings.ReplaceAll(r.URL.Path, `"`, ``))
}

// Start listens on 127.0.0.1:port (0 = any free port) and serves until Stop.
func (m *Mock) Start(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	m.Port, m.Since = ln.Addr().(*net.TCPAddr).Port, time.Now()
	m.srv = &http.Server{Handler: m, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = m.srv.Serve(ln) }()
	return nil
}

// Stop ends the server.
func (m *Mock) Stop() {
	if m.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = m.srv.Shutdown(ctx)
	}
}

// MockInfo is a running mock server, for lists.
type MockInfo struct {
	Collection string    `json:"collection"`
	Name       string    `json:"name"`
	Port       int       `json:"port"`
	Since      time.Time `json:"since"`
	Hits       int       `json:"hits"`
	Missed     int       `json:"missed"`
	Routes     int       `json:"routes"`
}

// Info describes the server.
func (m *Mock) Info() MockInfo {
	h, mi := m.Counts()
	return MockInfo{Collection: m.Collection, Name: m.Name, Port: m.Port, Since: m.Since, Hits: h, Missed: mi, Routes: len(m.routes)}
}

// Counts returns the hits and misses so far.
func (m *Mock) Counts() (int, int) { m.mu.Lock(); defer m.mu.Unlock(); return m.Hits, m.Missed }
