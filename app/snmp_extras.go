package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/termwright/termwright/internal/integrations"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/snmp"
	"github.com/termwright/termwright/internal/xlsx"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---- table export ----

// SNMPExportXLSX saves a table (a walk, a table view, a poll) as an Excel workbook; it returns the file.
func (a *App) SNMPExportXLSX(title string, headers []string, rows [][]string) (string, error) {
	name := strings.Trim(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, title), " -")
	if name == "" {
		name = "snmp"
	}
	p, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "Export to Excel", DefaultFilename: name + ".xlsx", CanCreateDirectories: true})
	if err != nil || p == "" {
		return "", err
	}
	return p, writeTableXLSX(p, title, headers, rows)
}

func writeTableXLSX(p, title string, headers []string, rows [][]string) error {
	sh := xlsx.Sheet{Name: "SNMP", Freeze: true}
	hd := make([]any, len(headers))
	w := make([]float64, len(headers))
	for i, h := range headers {
		hd[i], w[i] = xlsx.Cell{V: h, S: xlsx.Bold}, float64(max(len(h)+2, 10))
	}
	sh.Rows = append(sh.Rows, hd)
	for _, r := range rows {
		row := make([]any, len(r))
		for i, c := range r {
			if n, err := strconv.ParseFloat(c, 64); err == nil && len(c) < 16 && !strings.HasPrefix(c, "0") || c == "0" {
				row[i] = n
			} else {
				row[i] = c
			}
			if i < len(w) {
				w[i] = min(max(w[i], float64(len(c)+2)), 60)
			}
		}
		sh.Rows = append(sh.Rows, row)
	}
	sh.Widths = w
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	_ = title
	return xlsx.Write(f, []xlsx.Sheet{sh})
}

// ---- walk files ----

func rowVarbind(r SNMPRow) (snmp.Varbind, bool) {
	v := snmp.Varbind{OID: strings.TrimPrefix(r.OID, "."), Type: snmp.Type(r.Type), Value: r.Value}
	switch v.Type {
	case snmp.Integer:
		n, err := strconv.ParseInt(r.Value, 10, 64)
		if err != nil {
			return v, false
		}
		v.Int = n
	case snmp.Counter32, snmp.Gauge32, snmp.Counter64, snmp.TimeTicks:
		n, err := strconv.ParseUint(r.Value, 10, 64)
		if err != nil {
			return v, false
		}
		v.Uint = n
	case snmp.OctetString, snmp.Opaque:
		v.Bytes = []byte(r.Value)
		if r.Raw != "" {
			if b, err := hex.DecodeString(r.Raw); err == nil {
				v.Bytes = b
			}
		}
	case snmp.IPAddress, snmp.ObjectID, snmp.Null:
	default:
		return v, false
	}
	return v, true
}

func rowsVarbinds(rows []SNMPRow) []snmp.Varbind {
	var vs []snmp.Varbind
	for _, r := range rows {
		if v, ok := rowVarbind(r); ok {
			vs = append(vs, v)
		}
	}
	return vs
}

// SNMPSaveWalk saves values as a walk file (snmpwalk -On format); it returns the file.
func (a *App) SNMPSaveWalk(name string, rows []SNMPRow) (string, error) {
	if len(rows) == 0 {
		return "", errors.New("nothing to save: walk first")
	}
	p, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "Save Walk", DefaultFilename: strings.ReplaceAll(orDefault(name, "walk"), "/", "-") + ".snmpwalk", CanCreateDirectories: true})
	if err != nil || p == "" {
		return "", err
	}
	return p, os.WriteFile(p, []byte(snmp.FormatWalk(rowsVarbinds(rows))), 0o644)
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// SNMPWalkFile is a walk read from a file.
type SNMPWalkFile struct {
	File string    `json:"file"`
	Rows []SNMPRow `json:"rows"`
}

// SNMPOpenWalk reads a walk file (snmpwalk -On, or saved by Termwright) and names its values with the loaded MIBs.
func (a *App) SNMPOpenWalk() (*SNMPWalkFile, error) {
	p, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Open a Walk File"})
	if err != nil || p == "" {
		return nil, err
	}
	return a.readWalk(p)
}

func (a *App) readWalk(p string) (*SNMPWalkFile, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	vs := snmp.ParseWalk(string(b))
	if len(vs) == 0 {
		return nil, errors.New(filepath.Base(p) + " has no values in the snmpwalk -On format (.1.3.6.1… = TYPE: value)")
	}
	mib := a.snmpMIB()
	out := &SNMPWalkFile{File: p, Rows: make([]SNMPRow, 0, len(vs))}
	for _, v := range vs {
		if v.Value == "" {
			v = snmp.WithText(v)
		}
		out.Rows = append(out.Rows, snmpRow(mib, v))
	}
	return out, nil
}

// ---- agent simulator ----

// SNMPSimIn starts the simulator.
type SNMPSimIn struct {
	// File: a walk file; else Rows (what the browser shows).
	File string    `json:"file,omitempty"`
	Rows []SNMPRow `json:"rows,omitempty"`
	// Port: UDP port (default 1161; 161 needs administrator rights).
	Port      int    `json:"port,omitempty"`
	Community string `json:"community,omitempty"`
	// All: listen on every network interface (other devices can reach it), else 127.0.0.1 only.
	All      bool `json:"all,omitempty"`
	Writable bool `json:"writable,omitempty"`
}

// SNMPSimState is the simulator running, if any.
type SNMPSimState struct {
	Running   bool      `json:"running"`
	Address   string    `json:"address,omitempty"`
	Community string    `json:"community,omitempty"`
	Values    int       `json:"values,omitempty"`
	Source    string    `json:"source,omitempty"`
	Since     time.Time `json:"since,omitempty"`
}

var (
	simMu    sync.Mutex
	simAgent *snmp.Agent
	simState SNMPSimState
)

// SNMPSimStart answers SNMP v1/v2c from a walk, like the device it was taken from (one simulator at a time).
func (a *App) SNMPSimStart(in SNMPSimIn) (SNMPSimState, error) {
	var vs []snmp.Varbind
	src := "the browser's values"
	if in.File != "" {
		b, err := os.ReadFile(in.File)
		if err != nil {
			return SNMPSimState{}, err
		}
		vs, src = snmp.ParseWalk(string(b)), filepath.Base(in.File)
	} else {
		vs = rowsVarbinds(in.Rows)
	}
	if len(vs) == 0 {
		return SNMPSimState{}, errors.New("no values to answer with: walk a device or open a walk file first")
	}
	if in.Community == "" {
		in.Community = "public"
	}
	if in.Port == 0 {
		in.Port = 1161
	}
	host := "127.0.0.1"
	if in.All {
		host = "0.0.0.0"
	}
	ag := snmp.NewAgent(in.Community, []byte("termwright-sim"))
	for _, v := range vs {
		ag.Put(v)
	}
	ag.SetWritable(in.Writable)
	a.SNMPSimStop()
	addr, err := ag.Listen(fmt.Sprintf("%s:%d", host, in.Port))
	if err != nil {
		return SNMPSimState{}, err
	}
	simMu.Lock()
	defer simMu.Unlock()
	simAgent = ag
	simState = SNMPSimState{Running: true, Address: addr, Community: in.Community, Values: len(vs), Source: src, Since: time.Now()}
	return simState, nil
}

// SNMPSimStop stops the simulator.
func (a *App) SNMPSimStop() {
	simMu.Lock()
	defer simMu.Unlock()
	if simAgent != nil {
		_ = simAgent.Close()
		simAgent = nil
	}
	simState = SNMPSimState{}
}

// SNMPSim says whether the simulator runs.
func (a *App) SNMPSim() SNMPSimState { simMu.Lock(); defer simMu.Unlock(); return simState }

// ---- dashboards and alarms ----

// SNMPDashValue is one item's value in a poll.
type SNMPDashValue struct {
	Display string   `json:"display"`
	Value   *float64 `json:"value,omitempty"` // the number shown (the rate for rate items)
	Alarm   string   `json:"alarm,omitempty"` // why it is in alarm
	Error   string   `json:"error,omitempty"`
}

// SNMPDashPoll is one poll of a dashboard.
type SNMPDashPoll struct {
	At     time.Time       `json:"at"`
	Values []SNMPDashValue `json:"values"`
}

type dashPrev struct {
	v float64
	t time.Time
}

var (
	dashMu    sync.Mutex
	dashPrevs = map[string]dashPrev{}
	dashAlarm = map[string]string{}
	dashLast  = map[string]time.Time{}
)

// SNMPDashboards lists the saved dashboards.
func (a *App) SNMPDashboards() []config.SNMPDashboard {
	l := a.cfg.Get().SNMPDashboards
	if l == nil {
		return []config.SNMPDashboard{}
	}
	return l
}

// SNMPSaveDashboard adds (empty ID) or changes a dashboard.
func (a *App) SNMPSaveDashboard(d config.SNMPDashboard) (config.SNMPDashboard, error) {
	if strings.TrimSpace(d.Name) == "" {
		return d, errors.New("give the dashboard a name")
	}
	d.Every = max(d.Every, 5)
	cfg := a.cfg.Get()
	if d.ID == "" {
		d.ID = "dash-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		cfg.SNMPDashboards = append(cfg.SNMPDashboards, d)
	} else {
		found := false
		for i := range cfg.SNMPDashboards {
			if cfg.SNMPDashboards[i].ID == d.ID {
				cfg.SNMPDashboards[i], found = d, true
			}
		}
		if !found {
			return d, errors.New("no dashboard " + d.ID)
		}
	}
	return d, a.cfg.Save(cfg)
}

// SNMPDeleteDashboard removes a dashboard.
func (a *App) SNMPDeleteDashboard(id string) error {
	cfg := a.cfg.Get()
	var l []config.SNMPDashboard
	for _, d := range cfg.SNMPDashboards {
		if d.ID != id {
			l = append(l, d)
		}
	}
	cfg.SNMPDashboards = l
	return a.cfg.Save(cfg)
}

// SNMPPollDashboard polls a dashboard's values once (rates from the previous poll; alarms notify when they start).
func (a *App) SNMPPollDashboard(id string) (SNMPDashPoll, error) {
	for _, d := range a.cfg.Get().SNMPDashboards {
		if d.ID == id {
			return a.pollDash(context.Background(), d), nil
		}
	}
	return SNMPDashPoll{}, errors.New("no dashboard " + id)
}

func (a *App) pollDash(ctx context.Context, d config.SNMPDashboard) SNMPDashPoll {
	now := time.Now()
	out := SNMPDashPoll{At: now, Values: make([]SNMPDashValue, len(d.Items))}
	bySess := map[string][]int{}
	for i, it := range d.Items {
		bySess[it.Session] = append(bySess[it.Session], i)
	}
	for sess, idx := range bySess {
		oids := make([]string, len(idx))
		for k, i := range idx {
			oids[k] = d.Items[i].OID
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		res, err := a.snmpRun(cctx, SNMPRequest{Session: sess, Op: "get", OIDs: oids})
		cancel()
		for k, i := range idx {
			if err != nil {
				out.Values[i].Error = err.Error()
				continue
			}
			if k >= len(res.Rows) {
				out.Values[i].Error = "no answer"
				continue
			}
			out.Values[i] = a.dashValue(d, i, res.Rows[k], now)
		}
	}
	return out
}

func (a *App) dashValue(d config.SNMPDashboard, i int, r SNMPRow, now time.Time) SNMPDashValue {
	it := d.Items[i]
	key := d.ID + "\x00" + strconv.Itoa(i) + "\x00" + it.Session + it.OID
	v := SNMPDashValue{Display: r.Display}
	n, err := strconv.ParseFloat(r.Value, 64)
	dashMu.Lock()
	if err == nil {
		if it.Rate {
			if p, ok := dashPrevs[key]; ok && now.After(p.t) && n >= p.v {
				rate := (n - p.v) / now.Sub(p.t).Seconds()
				v.Value, v.Display = &rate, strconv.FormatFloat(rate, 'f', 1, 64)+"/s"
			} else {
				v.Display = "…"
			}
			dashPrevs[key] = dashPrev{n, now}
		} else {
			v.Value = &n
		}
	}
	switch {
	case it.Equals != "" && !strings.EqualFold(r.Display, it.Equals) && r.Value != it.Equals:
		v.Alarm = "is " + r.Display + ", should be " + it.Equals
	case v.Value != nil && it.Above != nil && *v.Value > *it.Above:
		v.Alarm = fmt.Sprintf("%s is above %g", v.Display, *it.Above)
	case v.Value != nil && it.Below != nil && *v.Value < *it.Below:
		v.Alarm = fmt.Sprintf("%s is below %g", v.Display, *it.Below)
	}
	was := dashAlarm[key]
	dashAlarm[key] = v.Alarm
	dashMu.Unlock()
	label := orDefault(it.Label, it.OID)
	if v.Alarm != "" && was == "" {
		_ = a.Notify("SNMP alarm: "+d.Name, label+" on "+it.Session+" "+v.Alarm)
		a.notifyResults(integrations.Message{Title: "SNMP alarm: " + d.Name, Text: label + " on " + it.Session + " " + v.Alarm, Failed: true})
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "snmp:alarm", map[string]string{"dashboard": d.Name, "item": label, "session": it.Session, "alarm": v.Alarm})
		}
	} else if v.Alarm == "" && was != "" {
		_ = a.Notify("SNMP alarm cleared: "+d.Name, label+" on "+it.Session+" is "+v.Display)
	}
	return v
}

// snmpDashLoop polls the dashboards with background alarms on.
func (a *App) snmpDashLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if a.ready() != nil {
				continue
			}
			for _, d := range a.cfg.Get().SNMPDashboards {
				if !d.Alarms || len(d.Items) == 0 {
					continue
				}
				dashMu.Lock()
				due := now.Sub(dashLast[d.ID]) >= time.Duration(max(d.Every, 5))*time.Second-time.Second
				if due {
					dashLast[d.ID] = now
				}
				dashMu.Unlock()
				if due {
					go a.pollDash(ctx, d)
				}
			}
		}
	}
}
