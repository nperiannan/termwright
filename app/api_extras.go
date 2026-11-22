package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/termwright/termwright/internal/integrations"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/apiclient"
	"github.com/termwright/termwright/internal/config"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---- mock servers ----

var (
	httpMocksMu sync.Mutex
	httpMocks   = map[string]*apiclient.Mock{}
)

// HTTPMockStart serves a collection's saved examples on 127.0.0.1:port (0 = a free port), for front ends
// and tests that need the API before the device has it.
func (a *App) HTTPMockStart(collection string, port int) (apiclient.MockInfo, error) {
	c, err := a.httpStore().Find(collection)
	if err != nil {
		return apiclient.MockInfo{}, err
	}
	m, err := apiclient.NewMock(c)
	if err != nil {
		return apiclient.MockInfo{}, err
	}
	httpMocksMu.Lock()
	defer httpMocksMu.Unlock()
	if old := httpMocks[c.ID]; old != nil {
		old.Stop()
		if port == 0 {
			port = old.Port
		}
	}
	if err := m.Start(port); err != nil {
		return apiclient.MockInfo{}, err
	}
	httpMocks[c.ID] = m
	return m.Info(), nil
}

// HTTPMockStop stops a collection's mock server.
func (a *App) HTTPMockStop(collection string) {
	httpMocksMu.Lock()
	defer httpMocksMu.Unlock()
	for id, m := range httpMocks {
		if id == collection || m.Name == collection {
			m.Stop()
			delete(httpMocks, id)
		}
	}
}

// HTTPMocks lists the mock servers running.
func (a *App) HTTPMocks() []apiclient.MockInfo {
	httpMocksMu.Lock()
	defer httpMocksMu.Unlock()
	out := []apiclient.MockInfo{}
	for _, m := range httpMocks {
		out = append(out, m.Info())
	}
	return out
}

// ---- monitors ----

// APIMonitorState is a monitor with its last results.
type APIMonitorState struct {
	config.APIMonitor
	LastRun time.Time `json:"lastRun,omitempty"`
	Passed  int       `json:"passed"`
	Failed  int       `json:"failed"`
	Ms      int64     `json:"ms"`
	Error   string    `json:"error,omitempty"`
	// History: the last runs, oldest first (true = all passed).
	History []bool `json:"history,omitempty"`
	Running bool   `json:"running,omitempty"`
}

var (
	monMu    sync.Mutex
	monState = map[string]*APIMonitorState{}
)

// APIMonitors lists the monitors with their last results.
func (a *App) APIMonitors() []APIMonitorState {
	monMu.Lock()
	defer monMu.Unlock()
	out := []APIMonitorState{}
	for _, m := range a.cfg.Get().APIMonitors {
		st := APIMonitorState{APIMonitor: m}
		if s := monState[m.ID]; s != nil {
			st = *s
			st.APIMonitor = m
		}
		out = append(out, st)
	}
	return out
}

// APISaveMonitor adds or changes a monitor (an empty ID adds one).
func (a *App) APISaveMonitor(m config.APIMonitor) (config.APIMonitor, error) {
	if _, err := a.httpStore().Find(m.Collection); err != nil {
		return m, err
	}
	m.Every = max(m.Every, 1)
	if strings.TrimSpace(m.Name) == "" {
		m.Name = m.Collection
	}
	cfg := a.cfg.Get()
	if m.ID == "" {
		m.ID = "mon-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		cfg.APIMonitors = append(cfg.APIMonitors, m)
	} else {
		found := false
		for i := range cfg.APIMonitors {
			if cfg.APIMonitors[i].ID == m.ID {
				cfg.APIMonitors[i], found = m, true
			}
		}
		if !found {
			return m, fmt.Errorf("no monitor %s", m.ID)
		}
	}
	return m, a.cfg.Save(cfg)
}

// APIDeleteMonitor removes a monitor.
func (a *App) APIDeleteMonitor(id string) error {
	cfg := a.cfg.Get()
	var l []config.APIMonitor
	for _, m := range cfg.APIMonitors {
		if m.ID != id {
			l = append(l, m)
		}
	}
	if len(l) == len(cfg.APIMonitors) {
		return errors.New("no monitor " + id)
	}
	cfg.APIMonitors = l
	return a.cfg.Save(cfg)
}

// APIRunMonitor runs a monitor now (in the background).
func (a *App) APIRunMonitor(id string) error {
	for _, m := range a.cfg.Get().APIMonitors {
		if m.ID == id {
			go a.runMonitor(context.Background(), m)
			return nil
		}
	}
	return errors.New("no monitor " + id)
}

func (a *App) runMonitor(ctx context.Context, m config.APIMonitor) {
	monMu.Lock()
	st := monState[m.ID]
	if st == nil {
		st = &APIMonitorState{}
		monState[m.ID] = st
	}
	if st.Running {
		monMu.Unlock()
		return
	}
	st.Running = true
	wasOK := len(st.History) == 0 || st.History[len(st.History)-1]
	monMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	// changing requests are refused on production sessions (never Confirmed)
	res, err := a.httpRun(ctx, HTTPRunRequest{Collection: m.Collection, Session: m.Session, Environment: m.Environment})
	monMu.Lock()
	st.Running, st.LastRun, st.Passed, st.Failed, st.Ms, st.Error = false, time.Now(), res.Passed, res.Failed, res.Ms, ""
	if err != nil {
		st.Error = err.Error()
	}
	ok := err == nil && res.Failed == 0
	st.History = append(st.History, ok)
	if len(st.History) > 30 {
		st.History = st.History[len(st.History)-30:]
	}
	monMu.Unlock()
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "api:monitor", m.ID)
	}
	switch {
	case !ok && wasOK:
		msg := fmt.Sprintf("%s: %d of %d requests failed", m.Name, res.Failed, res.Passed+res.Failed)
		if err != nil {
			msg = m.Name + ": " + err.Error()
		}
		_ = a.Notify("API monitor failed", msg)
		a.notifyResults(integrations.Message{Title: "API monitor failed", Text: msg, Failed: true})
	case ok && !wasOK:
		_ = a.Notify("API monitor passes again", m.Name)
		a.notifyResults(integrations.Message{Title: "API monitor passes again", Text: m.Name})
	}
}

// apiMonitorLoop runs the monitors that are due, every minute.
func (a *App) apiMonitorLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if a.ready() != nil {
				continue
			}
			for _, m := range a.cfg.Get().APIMonitors {
				if !m.Enabled {
					continue
				}
				monMu.Lock()
				st := monState[m.ID]
				due := st == nil || now.Sub(st.LastRun) >= time.Duration(max(m.Every, 1))*time.Minute-5*time.Second
				monMu.Unlock()
				if due {
					go a.runMonitor(ctx, m)
				}
			}
		}
	}
}
