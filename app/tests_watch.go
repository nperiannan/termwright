package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/syslog"
	"github.com/termwright/termwright/internal/testrec"
)

type watchMark struct {
	id uint64
	at time.Time
}

// WatchMark: where syslog and the SNMP notifications are now; an error when what the test watches
// is not being received.
func (d *testDriver) WatchMark(w *testrec.Watch) (any, error) {
	if len(w.Syslog) > 0 && !d.a.SyslogState().On {
		return nil, errors.New("the test watches syslog, but the syslog server is off: start it (Tools → Syslog) and point the devices at it")
	}
	if len(w.Traps) > 0 && !d.a.SNMPTraps().State.On {
		return nil, errors.New("the test watches SNMP notifications, but the trap receiver is off: start it in the SNMP Browser")
	}
	m := watchMark{at: time.Now()}
	if l, err := d.a.SyslogMessages(syslog.Query{Limit: 1}); err == nil && len(l) > 0 {
		m.id = l[len(l)-1].ID
		for _, x := range l {
			m.id = max(m.id, x.ID)
		}
	}
	return m, nil
}

// WatchHits: the syslog messages and SNMP notifications since the mark that match the watch.
func (d *testDriver) WatchHits(mark any, w testrec.Watch, sessions []string) ([]string, error) {
	m, _ := mark.(watchMark)
	from := sessions
	if len(w.Devices) > 0 {
		from = w.Devices
	}
	anyone := slices.Contains(from, "any") || len(from) == 0
	ok := func(sess string) bool { return anyone || slices.Contains(from, sess) }
	var hits []string
	seen := map[uint64]bool{}
	for _, p := range w.Syslog {
		l, err := d.a.SyslogMessages(syslog.Query{After: m.id, Text: p, Limit: 200})
		if err != nil {
			return hits, err
		}
		for _, x := range l {
			if seen[x.ID] || !ok(x.Session) {
				continue
			}
			seen[x.ID] = true
			hits = append(hits, fmt.Sprintf("syslog %s %s: %s", x.At.Local().Format("15:04:05"), orDash(x.Session), clip(x.Text, 200)))
		}
	}
	if len(w.Traps) > 0 {
		for _, t := range d.a.SNMPTraps().Traps {
			if t.At.Before(m.at) || !ok(t.Session) {
				continue
			}
			j, _ := json.Marshal(t.Rows)
			text := t.Name + " " + t.TrapOID + " " + string(j)
			for _, p := range w.Traps {
				if textMatch(text, p) {
					hits = append(hits, fmt.Sprintf("trap %s %s: %s", t.At.Local().Format("15:04:05"), orDash(t.Session), t.Name))
					break
				}
			}
		}
	}
	return hits, nil
}

// textMatch: all words in text (case-insensitive), or /regex/.
func textMatch(text, p string) bool {
	if len(p) > 2 && strings.HasPrefix(p, "/") && strings.HasSuffix(p, "/") {
		re, err := regexp.Compile(p[1 : len(p)-1])
		return err == nil && re.MatchString(text)
	}
	lt := strings.ToLower(text)
	for _, w := range strings.Fields(strings.ToLower(p)) {
		if !strings.Contains(lt, w) {
			return false
		}
	}
	return true
}

// ConfigSnapshot reads a device's configuration with its device type's backup commands.
func (d *testDriver) ConfigSnapshot(ctx context.Context, path string) (string, error) {
	s, err := d.a.store.Get(path)
	if err != nil {
		return "", fmt.Errorf("no saved session %q", path)
	}
	dt, _ := d.a.deviceTypeNamed(s.DeviceType)
	cmds := dt.Backup()
	if len(cmds) == 0 {
		return "", fmt.Errorf("%s: its device type has no backup commands to read the configuration (Global Options → Device Types)", path)
	}
	var b strings.Builder
	for _, c := range cmds {
		out, err := d.Command(ctx, path, c, "")
		if err != nil {
			return "", err
		}
		b.WriteString(out + "\n")
	}
	return b.String(), nil
}

// Traffic runs a saved lab traffic test and waits for its results (as JSON: status, rows, error).
func (d *testDriver) Traffic(ctx context.Context, test string) (string, error) {
	r, err := d.a.EmuTestRun(test)
	if err != nil {
		return "", err
	}
	for {
		select {
		case <-ctx.Done():
			d.a.EmuTestStop()
			return "", fmt.Errorf("traffic test %s: no result in time", test)
		case <-time.After(time.Second):
		}
		for _, x := range d.a.EmuTestRuns() {
			if x.ID != r.ID || x.Status == "running" {
				continue
			}
			j, _ := json.Marshal(map[string]any{"status": x.Status, "rows": x.Rows, "error": x.Error})
			if x.Status != "done" {
				return string(j), fmt.Errorf("traffic test %s %s: %s", test, x.Status, x.Error)
			}
			return string(j), nil
		}
	}
}

// Versions reads the software version of each device (its device facts: refreshed in a background
// login where that needs no password, else the ones saved), at most 60 s for all.
func (d *testDriver) Versions(ctx context.Context, sessions []string) map[string]string {
	out := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range sessions {
		if s, err := d.a.store.Get(p); err != nil || s.DeviceType == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			done := make(chan map[string]string, 1)
			go func() { m, _ := d.a.RefreshFacts(p, false); done <- m }()
			var m map[string]string
			select {
			case m = <-done:
			case <-time.After(60 * time.Second):
				m = d.a.SessionFacts(p)
			case <-ctx.Done():
				return
			}
			if v := strings.TrimSpace(m["Version"]); v != "" {
				mu.Lock()
				out[p] = v
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

// Upgrade installs an image with the session's device type's upgrade commands ({image}), waits until
// the device logs in again and returns the version it runs then.
func (d *testDriver) Upgrade(ctx context.Context, path, image string) (string, error) {
	s, err := d.a.store.Get(path)
	if err != nil {
		return "", fmt.Errorf("no saved session %q", path)
	}
	if d.a.isProtected(s) {
		return "", errors.New("not on a production session")
	}
	dt, _ := d.a.deviceTypeNamed(s.DeviceType)
	var cmds []string
	for _, l := range strings.Split(dt.Upgrade, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			cmds = append(cmds, strings.ReplaceAll(l, "{image}", image))
		}
	}
	if len(cmds) == 0 {
		return "", fmt.Errorf("the device type %q has no upgrade commands (Global Options → Device Types)", orDash(s.DeviceType))
	}
	d.EndCase()
	outs, err := d.a.terms.CaptureYes(ctx, s, cmds, "")
	out := strings.Join(outs, "\n")
	if err != nil && !strings.Contains(err.Error(), "closed") {
		return out, err
	}
	if err := d.waitUp(ctx, path, 25*time.Minute); err != nil {
		return out, err
	}
	m, _ := d.a.RefreshFacts(path, false)
	if v := m["Version"]; v != "" {
		return out + "\nnow running " + v, nil
	}
	return out, nil
}
