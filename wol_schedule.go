package main

import (
	"context"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/backup"
	"github.com/termwright/termwright/internal/session"
)

// wakeLate is how long after its time a scheduled wake still goes out (Termwright
// opened late, the computer woke from sleep); later than that the slot is skipped.
const wakeLate = 15 * time.Minute

// wakeDue reports whether a Wake-on-LAN session's schedule calls for a magic
// packet at now: its slot today has come, at most wakeLate ago, and it was not
// sent for that slot yet.
func wakeDue(schedule, at string, last, now time.Time) bool {
	if !backup.Due(schedule, at, last, now) {
		return false
	}
	hm, _ := time.ParseInLocation("15:04", at, now.Location())
	slot := time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, now.Location())
	return now.Sub(slot) <= wakeLate
}

// wolScheduler wakes the Wake-on-LAN sessions that have a schedule, while
// Termwright is open.
func (a *App) wolScheduler(ctx context.Context) {
	var mu sync.Mutex
	last := map[string]time.Time{} // session path → last scheduled wake
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
			for _, s := range a.scheduledWakes() {
				mu.Lock()
				due := wakeDue(s.WakeSchedule, s.WakeAt, last[s.Path], now)
				if due {
					last[s.Path] = now
				}
				mu.Unlock()
				if !due {
					continue
				}
				if _, err := a.WakeDevice(s); err != nil {
					emitter{a}.Emit("toast", map[string]string{"level": "error", "message": "Scheduled wake of " + s.Path + ": " + err.Error()})
				} else {
					emitter{a}.Emit("toast", map[string]string{"level": "info", "message": "Scheduled wake: magic packet sent to " + s.Path})
				}
			}
		}
	}
}

// scheduledWakes lists the Wake-on-LAN sessions with a schedule.
func (a *App) scheduledWakes() []session.Session {
	tree, err := a.store.Tree()
	if err != nil {
		return nil
	}
	var out []session.Session
	var walk func(n *session.Node)
	walk = func(n *session.Node) {
		for _, c := range n.Children {
			if c.IsFolder {
				walk(c)
			} else if c.Protocol == session.ProtoWOL {
				if s, err := a.store.Get(c.Path); err == nil && s.WakeSchedule != "" && s.WakeAt != "" {
					out = append(out, s)
				}
			}
		}
	}
	walk(tree)
	return out
}
