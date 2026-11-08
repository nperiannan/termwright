package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/discover"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/team"
)

// The team folder: every joined team's inventory is a folder named after the
// team in the Session Manager, kept in step with the team server (a long poll
// wakes up within seconds of a change). Its sessions carry Team =
// "teamID/deviceID"; the team decides their name, folder, address, port,
// protocol, device type and MAC address, while logins (username, password,
// account, keys) stay each person's own and are never sent.

var (
	teamSyncMu sync.Mutex
	teamSyncs  = map[string]context.CancelFunc{}
)

// startTeamSync runs one sync loop per joined team and stops those of teams
// that were left.
func (a *App) startTeamSync() {
	if a.cfg == nil || a.store == nil {
		return
	}
	teamSyncMu.Lock()
	defer teamSyncMu.Unlock()
	want := map[string]bool{}
	for _, t := range a.cfg.Get().Teams {
		want[t.ID] = true
		if _, ok := teamSyncs[t.ID]; !ok {
			ctx, cancel := context.WithCancel(context.Background())
			teamSyncs[t.ID] = cancel
			go a.teamSyncLoop(ctx, t.ID)
			go a.teamStatusLoop(ctx, t.ID)
			go a.teamHeartbeat(ctx, t.ID)
		}
	}
	for id, cancel := range teamSyncs {
		if !want[id] {
			cancel()
			delete(teamSyncs, id)
		}
	}
}

func (a *App) teamSyncLoop(ctx context.Context, id string) {
	rev := int64(-1)
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		t, c, err := a.teamConn(id)
		if err != nil {
			return // left
		}
		wait := 50
		if rev < 0 {
			wait = 0
		}
		inv, err := c.Inventory(ctx, rev, wait)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if team.NeedsSSO(err) {
				if a.ctx != nil { // once: the Teams window then offers Sign In Again
					wruntime.EventsEmit(a.ctx, "team:sso", map[string]string{"teamId": id, "team": t.Name})
				}
				return
			}
			if team.Unauthorized(err) {
				return // removed from the team: the Teams window says so; the folder stays until Leave
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = 5 * time.Second
		if inv.Rev != rev {
			teamInvs.Store(id, inv)
			go a.sendPresence(id, false) // devices open now that just came into the inventory
			if err := a.applyInventory(id, inv); err == nil {
				rev = inv.Rev
			} else {
				time.Sleep(10 * time.Second)
			}
		}
	}
}

// teamRoot is the Session Manager folder of a team.
func teamRoot(t config.TeamConn) string {
	n := session.CleanName(strings.ReplaceAll(t.Name, "/", "-"))
	if n == "" {
		n = "Team"
	}
	return n
}

// teamSessions are the sessions of a team's folder, by device id.
func (a *App) teamSessions(teamID string) map[string][]session.Session {
	out := map[string][]session.Session{}
	root, err := a.store.Tree()
	if err != nil {
		return out
	}
	var walk func(n *session.Node)
	walk = func(n *session.Node) {
		for _, c := range n.Children {
			if c.IsFolder {
				walk(c)
			} else if tid, dev, ok := strings.Cut(c.Team, "/"); ok && tid == teamID {
				if s, err := a.store.Get(c.Path); err == nil {
					out[dev] = append(out[dev], s)
				}
			}
		}
	}
	walk(root)
	return out
}

var teamApplyMu sync.Mutex

// applyInventory makes the team folder match the inventory.
func (a *App) applyInventory(teamID string, inv team.Inventory) error {
	teamApplyMu.Lock()
	defer teamApplyMu.Unlock()
	t, _, err := a.teamConn(teamID)
	if err != nil {
		return err
	}
	root := teamRoot(t)
	if !a.store.Exists(root) {
		_ = a.store.CreateFolder(root)
	}
	for _, f := range inv.Folders {
		_ = a.store.CreateFolder(root + "/" + f)
	}
	have := a.teamSessions(teamID)
	var firstErr error
	keep := map[string]bool{}
	for _, d := range inv.Devices {
		keep[d.ID] = true
		folder := root
		if d.Folder != "" {
			folder = root + "/" + d.Folder
		}
		list := have[d.ID]
		var s session.Session
		old := ""
		if len(list) > 0 {
			s, old = list[0], list[0].Path
			for _, extra := range list[1:] { // a copy made by hand: not part of the team
				extra.Team = ""
				_, _ = a.store.Save(extra, extra.Path)
			}
		} else {
			s = a.store.NewSession()
			s.Team = teamID + "/" + d.ID
		}
		port := d.Port
		if port == 0 {
			port = team.DefaultPortOf(d.Protocol)
		}
		desc := []string{}
		if d.Description != "" {
			desc = []string{d.Description}
		}
		if old != "" && s.Name == session.CleanName(d.Name) && s.Folder == folder && s.Hostname == d.Host && s.Port == port &&
			s.Protocol == d.Protocol && s.DeviceType == d.DeviceType && s.MAC == d.MAC && strings.Join(s.Description, "\n") == d.Description {
			continue
		}
		s.Name, s.Folder, s.Hostname, s.Port, s.Protocol, s.DeviceType, s.MAC, s.Description =
			session.CleanName(d.Name), folder, d.Host, port, d.Protocol, d.DeviceType, discover.NormalizeMAC(d.MAC), desc
		save := func() error {
			if old == "" {
				_, err := a.store.Save(s, "")
				return err
			}
			_, err := a.SaveSession(s, old)
			return err
		}
		if err := save(); err != nil {
			// a session of the same name is there already (one of your own): keep both
			s.Name = session.CleanName(d.Name) + " (team)"
			if err2 := save(); err2 != nil && firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", d.Name, err)
			}
		}
	}
	for dev, list := range have {
		if !keep[dev] {
			for _, s := range list {
				_ = a.DeleteSession(s.Path)
			}
		}
	}
	return firstErr
}

// removeTeamFolder deletes the sessions of a team (when leaving it) and its
// folder when nothing else is in it.
func (a *App) removeTeamFolder(t config.TeamConn) {
	teamApplyMu.Lock()
	defer teamApplyMu.Unlock()
	for _, list := range a.teamSessions(t.ID) {
		for _, s := range list {
			_ = a.DeleteSession(s.Path)
		}
	}
	root := teamRoot(t)
	if n, err := a.store.Tree(); err == nil {
		for _, c := range n.Children {
			if c.IsFolder && c.Path == root && !hasSessions(c) {
				_ = a.store.DeleteFolder(root)
			}
		}
	}
}

func hasSessions(n *session.Node) bool {
	for _, c := range n.Children {
		if !c.IsFolder || hasSessions(c) {
			return true
		}
	}
	return false
}

// ---- pushing sessions to a team ----

// TeamPushItem is a device to push (from sessions or Discover results).
type TeamPushItem struct {
	team.Device
	Path string `json:"path"` // the session it comes from ("" from Discover)
}

// TeamPushDevices are the devices of sessions, for the push window: the
// folder is relative to the folder they were picked in.
func (a *App) TeamPushDevices(paths []string, base string) ([]TeamPushItem, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	var out []TeamPushItem
	for _, p := range paths {
		s, err := a.store.Get(p)
		if err != nil {
			continue
		}
		switch s.Protocol {
		case session.ProtoSerial, session.ProtoLocal, session.ProtoWOL, session.ProtoConsole:
			continue // nothing to reach on the network
		}
		if strings.TrimSpace(s.Hostname) == "" {
			continue
		}
		f := strings.TrimPrefix(strings.TrimPrefix(s.Folder, base), "/")
		if base != "" && !strings.HasPrefix(s.Folder, base) {
			f = s.Folder
		}
		out = append(out, TeamPushItem{Path: p, Device: team.Device{Name: s.Name, Folder: f, Host: s.Hostname, Port: s.Port, Protocol: s.Protocol,
			DeviceType: s.DeviceType, MAC: s.MAC, Description: strings.Join(s.Description, " ")}})
	}
	return out, nil
}

// TeamCheck says for each device whether the team has it already (perhaps under another name).
func (a *App) TeamCheck(id string, items []TeamPushItem) ([]team.PushResult, error) {
	_, c, err := a.teamConn(id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := teamCtx()
	defer cancel()
	return c.Check(ctx, devicesOf(items))
}

// TeamPush pushes devices: added at once for owners and co-owners, waiting
// for their approval for members.
func (a *App) TeamPush(id string, items []TeamPushItem) ([]team.PushResult, error) {
	_, c, err := a.teamConn(id)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("nothing to push")
	}
	ctx, cancel := teamCtx()
	defer cancel()
	return c.Push(ctx, devicesOf(items))
}

func devicesOf(items []TeamPushItem) []team.Device {
	d := make([]team.Device, len(items))
	for i, it := range items {
		d[i] = it.Device
	}
	return d
}

// TeamOpenAdmin opens the team's admin page in the browser, signed in (owners and co-owners).
func (a *App) TeamOpenAdmin(id string) error {
	t, c, err := a.teamConn(id)
	if err != nil {
		return err
	}
	ctx, cancel := teamCtx()
	defer cancel()
	p, err := c.AdminLink(ctx)
	if err != nil {
		return err
	}
	if a.ctx != nil {
		wruntime.BrowserOpenURL(a.ctx, "https://"+t.Address+p)
	}
	return nil
}
