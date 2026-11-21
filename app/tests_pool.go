package app

import (
	"context"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/term"
)

// testPool keeps the background logins of the generated tests' steps (POST /tools/tests step): the
// steps of one Go, Python or TCL test arrive one by one, so a login is kept per session and prompt
// and logged out after testPoolIdle without use.
var testPool = &capPool{conns: map[string]*pooledConn{}}

const testPoolIdle = 90 * time.Second

type pooledConn struct {
	c    *term.CaptureConn
	busy bool
	used time.Time
}

type capPool struct {
	mu    sync.Mutex
	conns map[string]*pooledConn
	once  sync.Once
}

// get returns a login for s (a pooled one when free, else a new one) and the function that gives it
// back; bad drops it.
func (p *capPool) get(ctx context.Context, a *App, s session.Session, prompt string) (*term.CaptureConn, func(bad bool), error) {
	p.once.Do(func() { go p.janitor() })
	key := s.Path + "\x00" + prompt
	p.mu.Lock()
	if pc := p.conns[key]; pc != nil && !pc.busy {
		if pc.c.Alive() {
			pc.busy = true
			p.mu.Unlock()
			return pc.c, p.release(key, pc), nil
		}
		pc.c.Close()
		delete(p.conns, key)
	}
	inUse := p.conns[key] != nil
	p.mu.Unlock()
	c, err := a.terms.OpenCapture(ctx, s, prompt)
	if err != nil {
		return nil, nil, err
	}
	if inUse { // another test uses the pooled one: this login is for this step only
		return c, func(bool) { c.Close() }, nil
	}
	pc := &pooledConn{c: c, busy: true}
	p.mu.Lock()
	p.conns[key] = pc
	p.mu.Unlock()
	return c, p.release(key, pc), nil
}

func (p *capPool) release(key string, pc *pooledConn) func(bad bool) {
	return func(bad bool) {
		p.mu.Lock()
		defer p.mu.Unlock()
		pc.busy, pc.used = false, time.Now()
		if bad {
			pc.c.Close()
			if p.conns[key] == pc {
				delete(p.conns, key)
			}
		}
	}
}

func (p *capPool) janitor() {
	for range time.Tick(15 * time.Second) {
		p.mu.Lock()
		for k, pc := range p.conns {
			if !pc.busy && time.Since(pc.used) > testPoolIdle {
				pc.c.Close()
				delete(p.conns, k)
			}
		}
		p.mu.Unlock()
	}
}
