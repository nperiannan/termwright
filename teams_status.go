package main

import (
	"context"
	"errors"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/termwright/termwright/internal/discover"
	"github.com/termwright/termwright/internal/team"
)

// Who uses which device: the window tells TeamPresence which network devices
// have a connected tab; each team server gets those of them that are in its
// inventory (a heartbeat every 30 s), and its status - who has what open, who
// reserved what, notices for this member - comes back as "team:status" and
// "team:note" events. Nothing of it runs without a team.

var (
	teamInvs     sync.Map // team id → team.Inventory (the last one synced)
	teamStatuses sync.Map // team id → team.Status
	presenceMu   sync.Mutex
	presenceAll  []team.DeviceRef                // the open network devices, from the window
	presenceSent = map[string][]team.DeviceRef{} // team id → what that team was told last
)

// TeamStatusView is a team's status for the window.
type TeamStatusView struct {
	TeamID string      `json:"teamId"`
	Team   string      `json:"team"`
	Status team.Status `json:"status"`
}

// TeamStatuses are the statuses known now (the window asks once, then follows the events).
func (a *App) TeamStatuses() []TeamStatusView {
	var out []TeamStatusView
	for _, t := range a.cfg.Get().Teams {
		if v, ok := teamStatuses.Load(t.ID); ok {
			out = append(out, TeamStatusView{TeamID: t.ID, Team: t.Name, Status: v.(team.Status)})
		}
	}
	return out
}

func normRef(d team.DeviceRef) team.DeviceRef {
	d.MAC = discover.NormalizeMAC(d.MAC)
	if d.Port == 0 {
		d.Port = team.DefaultPortOf(d.Protocol)
	}
	return d
}

// inInventory: the devices of list that the team's inventory has.
func inInventory(id string, list []team.DeviceRef) []team.DeviceRef {
	v, ok := teamInvs.Load(id)
	if !ok {
		return nil
	}
	inv := v.(team.Inventory)
	var out []team.DeviceRef
	for _, d := range list {
		for _, x := range inv.Devices {
			if team.SameDevice(d.Dev(), x) {
				out = append(out, d)
				break
			}
		}
	}
	return out
}

func sameRefs(a, b []team.DeviceRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TeamPresence: the network devices with a connected tab now (from the window).
func (a *App) TeamPresence(devices []team.DeviceRef) {
	for i := range devices {
		devices[i] = normRef(devices[i])
	}
	presenceMu.Lock()
	presenceAll = devices
	presenceMu.Unlock()
	for _, t := range a.cfg.Get().Teams {
		go a.sendPresence(t.ID, false)
	}
}

// sendPresence tells a team what is open of its devices: when it changed, or as heartbeat.
func (a *App) sendPresence(id string, heartbeat bool) {
	presenceMu.Lock()
	list := inInventory(id, presenceAll)
	last, told := presenceSent[id]
	if !heartbeat && told && sameRefs(last, list) {
		presenceMu.Unlock()
		return
	}
	if heartbeat && len(list) == 0 && told && len(last) == 0 {
		presenceMu.Unlock()
		return
	}
	presenceSent[id] = list
	presenceMu.Unlock()
	_, c, err := a.teamConn(id)
	if err != nil {
		return
	}
	ctx, cancel := teamCtx()
	defer cancel()
	if err := c.Presence(ctx, list); err != nil {
		presenceMu.Lock()
		delete(presenceSent, id) // try again with the next heartbeat
		presenceMu.Unlock()
	}
}

func (a *App) teamHeartbeat(ctx context.Context, id string) {
	tk := time.NewTicker(30 * time.Second)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			a.sendPresence(id, true)
		}
	}
}

func (a *App) teamStatusLoop(ctx context.Context, id string) {
	rev := int64(-1)
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		t, c, err := a.teamConn(id)
		if err != nil {
			return
		}
		wait := 50
		if rev < 0 {
			wait = 0
		}
		st, err := c.Status(ctx, rev, wait)
		if err != nil {
			if ctx.Err() != nil || team.Unauthorized(err) {
				return
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
		for _, n := range st.Notes {
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "team:note", map[string]string{"teamId": id, "team": t.Name, "text": n.Text})
			}
		}
		st.Notes = nil
		if st.Rev != rev {
			rev = st.Rev
			teamStatuses.Store(id, st)
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "team:status", TeamStatusView{TeamID: id, Team: t.Name, Status: st})
			}
		}
	}
}

// teamOf is the first joined team whose inventory has the device.
func (a *App) teamOf(d team.DeviceRef) (string, error) {
	for _, t := range a.cfg.Get().Teams {
		if len(inInventory(t.ID, []team.DeviceRef{d})) > 0 {
			return t.ID, nil
		}
	}
	return "", errors.New("this device is not in a team's inventory - push it to your team first")
}

// TeamReserve reserves a device in the team whose inventory has it, until a time (RFC 3339).
func (a *App) TeamReserve(d team.DeviceRef, until, note string) (team.Reservation, error) {
	d = normRef(d)
	id, err := a.teamOf(d)
	if err != nil {
		return team.Reservation{}, err
	}
	u, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return team.Reservation{}, errors.New("pick when the reservation ends")
	}
	_, c, err := a.teamConn(id)
	if err != nil {
		return team.Reservation{}, err
	}
	ctx, cancel := teamCtx()
	defer cancel()
	return c.Reserve(ctx, d, u, note)
}

// TeamRelease ends a reservation.
func (a *App) TeamRelease(teamID, id string) error {
	_, c, err := a.teamConn(teamID)
	if err != nil {
		return err
	}
	ctx, cancel := teamCtx()
	defer cancel()
	return c.Release(ctx, id)
}

// TeamConnected tells whoever reserved the device that this member connected to it.
func (a *App) TeamConnected(d team.DeviceRef) {
	d = normRef(d)
	for _, t := range a.cfg.Get().Teams {
		if len(inInventory(t.ID, []team.DeviceRef{d})) == 0 {
			continue
		}
		if _, c, err := a.teamConn(t.ID); err == nil {
			ctx, cancel := teamCtx()
			_ = c.Connected(ctx, d)
			cancel()
		}
	}
}
