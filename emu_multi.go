package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Several topologies can run at once (on one host or on different ones): the
// open one is worked on; the others keep running in the background. Their
// labs must not share a folder, ports, or management addresses.

// emuOther is what the other saved topologies use.
type emuOther struct {
	ids   map[string]bool // lab IDs (folders lab-<id> on the hosts)
	bases []int           // first ports
	addrs map[string]bool // fixed management addresses
	nets  map[string]bool // lab bridge networks
}

var emuOthersCache struct {
	at   time.Time
	open string
	o    emuOther
}

// emuOthers reads the saved topologies other than the open one (emuLab.mu held; cached a few seconds).
func (a *App) emuOthers() emuOther {
	if time.Since(emuOthersCache.at) < 5*time.Second && emuOthersCache.open == emuLab.f.Topology {
		return emuOthersCache.o
	}
	o := emuOther{ids: map[string]bool{}, addrs: map[string]bool{}, nets: map[string]bool{}}
	ents, _ := os.ReadDir(a.emuTopoDir())
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(a.emuTopoDir(), e.Name()))
		var t emuTopo
		if err != nil || json.Unmarshal(b, &t) != nil || t.Lab == nil || t.Name == emuLab.f.Topology {
			continue
		}
		o.ids[t.Lab.ID] = true
		o.bases = append(o.bases, t.Lab.Base)
		if t.Lab.Mgmt == "bridge" {
			o.nets[t.Lab.MgmtNet] = true
		}
		for _, v := range t.Lab.VMs {
			if v.MgmtIP != "" && v.MgmtIP != "10.0.2.15/24" {
				o.addrs[v.MgmtIP] = true
			}
		}
	}
	emuOthersCache.at, emuOthersCache.open, emuOthersCache.o = time.Now(), emuLab.f.Topology, o
	return o
}

// emuFreeLabID is a lab ID no other topology has.
func emuFreeLabID(o emuOther) string {
	for i := 0; i < 1000; i++ {
		if id := randHex(1); !o.ids[id] {
			return id
		}
	}
	return randHex(2)
}

// emuFreeBase is a first port as far as it can be from the other topologies' (each lab uses its
// first port + 4 per switch for consoles, + 4000 + 64 per switch for cables).
func emuFreeBase(o emuOther) int {
	best, bestGap := 20000+int(randByte())%40*100, -1
	for k := 0; k < 40; k++ {
		b := 20000 + k*100
		gap := 1 << 30
		for _, x := range o.bases {
			gap = min(gap, max(b-x, x-b))
		}
		if gap > bestGap || gap == bestGap && randByte()%2 == 0 {
			best, bestGap = b, gap
		}
	}
	return best
}

// emuFreeBridgeNet is a lab bridge network no other topology has (10.99.0.0/24, 10.99.1.0/24…).
func emuFreeBridgeNet(o emuOther) string {
	for k := 0; k < 250; k++ {
		if n := fmt.Sprintf("10.99.%d.0/24", k); !o.nets[n] {
			return n
		}
	}
	return "10.99.0.0/24"
}

// EmuRunRow is what runs on one host for one topology.
type EmuRunRow struct {
	Host     string   `json:"host"`
	Topology string   `json:"topology"`
	Open     bool     `json:"open"`
	Switches []string `json:"switches"`
	Lab      string   `json:"lab"`              // its lab ID on the host (termwright-lab/lab-<id>)
	Orphan   bool     `json:"orphan,omitempty"` // no saved topology has it: left running when its topology went
}

var emuRunCache struct {
	mu   sync.Mutex
	at   time.Time
	rows []EmuRunRow
}

// EmuRunning is every running switch on the lab's hosts, by host and topology (also those of
// topologies not open: they run in the background).
func (a *App) EmuRunning() []EmuRunRow {
	emuRunCache.mu.Lock()
	defer emuRunCache.mu.Unlock()
	if time.Since(emuRunCache.at) < 5*time.Second && emuRunCache.rows != nil {
		return emuRunCache.rows
	}
	emuLab.mu.Lock()
	a.emuLoad()
	var hosts []string
	for _, h := range emuLab.f.Hosts {
		hosts = append(hosts, h.Session)
	}
	if emuLab.f.Host != "" && !slices.Contains(hosts, emuLab.f.Host) {
		hosts = append(hosts, emuLab.f.Host)
	}
	open := emuLab.f.Topology
	// lab ID → topology, and its switches' names
	type topo struct {
		name  string
		names map[string]string
	}
	byLab := map[string]topo{}
	ents, _ := os.ReadDir(a.emuTopoDir())
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(a.emuTopoDir(), e.Name()))
		var t emuTopo
		if e.IsDir() || err != nil || json.Unmarshal(b, &t) != nil || t.Lab == nil {
			continue
		}
		tp := topo{t.Name, map[string]string{}}
		for _, v := range t.Lab.VMs {
			tp.names[v.ID] = v.Name
		}
		byLab[t.Lab.ID] = tp
	}
	if l := emuLab.f.Lab; l != nil { // the open one as it is now
		tp := topo{orStr(open, "Untitled"), map[string]string{}}
		for _, v := range l.VMs {
			tp.names[v.ID] = v.Name
		}
		byLab[l.ID] = tp
	}
	emuLab.mu.Unlock()
	script := "cd && for f in termwright-lab/lab-*/*.pid; do [ -f \"$f\" ] || continue; p=$(cat \"$f\" 2>/dev/null); [ -n \"$p\" ] && grep -q qemu /proc/$p/comm 2>/dev/null && echo \"run $f\"; done; true"
	rows := []EmuRunRow{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, h := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := a.emuRun(h, script, 20*time.Second)
			if err != nil {
				return
			}
			by := map[string]*EmuRunRow{}
			for _, ln := range strings.Split(out, "\n") {
				f := strings.TrimPrefix(strings.TrimSpace(ln), "run termwright-lab/lab-")
				if f == strings.TrimSpace(ln) {
					continue
				}
				lab, file, ok := strings.Cut(f, "/")
				if !ok {
					continue
				}
				id := strings.TrimSuffix(file, ".pid")
				tp, known := byLab[lab]
				name, tname := id, "lab-"+lab+" (no saved topology)"
				if known {
					tname = tp.name
					name = orStr(tp.names[id], id)
				}
				r := by[tname]
				if r == nil {
					r = &EmuRunRow{Host: h, Topology: tname, Open: known && tp.name == orStr(open, "Untitled"), Lab: lab, Orphan: !known}
					by[tname] = r
				}
				r.Switches = append(r.Switches, name)
			}
			mu.Lock()
			for _, r := range by {
				slices.Sort(r.Switches)
				rows = append(rows, *r)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	slices.SortFunc(rows, func(x, y EmuRunRow) int { return strings.Compare(x.Host+"|"+x.Topology, y.Host+"|"+y.Topology) })
	emuRunCache.at, emuRunCache.rows = time.Now(), rows
	return rows
}

// emuLabIDRe is a lab ID as its folder on a host names it.
var emuLabIDRe = regexp.MustCompile(`^[0-9a-z]{1,16}$`)

// emuStopLabScript switches off every running switch of a lab on a host (its QEMU processes): what runs in the
// background for a topology that is not open, or for none any more.
func emuStopLabScript(lab string) string {
	return `cd && n=0; for f in termwright-lab/lab-` + lab + `/*.pid; do [ -f "$f" ] || continue; p=$(cat "$f" 2>/dev/null); if [ -n "$p" ] && grep -q qemu /proc/$p/comm 2>/dev/null; then kill "$p" 2>/dev/null && n=$((n+1)); fi; done; ` +
		`for i in 1 2 3 4 5 6 7 8 9 10; do a=0; for f in termwright-lab/lab-` + lab + `/*.pid; do [ -f "$f" ] || continue; p=$(cat "$f" 2>/dev/null); [ -n "$p" ] && grep -q qemu /proc/$p/comm 2>/dev/null && [ "$(cut -d' ' -f3 /proc/$p/stat 2>/dev/null)" != Z ] && a=1; done; [ $a = 0 ] && break; sleep 1; done; echo "@@stopped $n"`
}

// EmuStopBackground switches off the switches of a lab that runs on a host in the background - a topology that is
// not open, or one no saved topology has any more (deleted while it ran). The open topology's are stopped as usual.
func (a *App) EmuStopBackground(host, lab string) (int, error) {
	defer emuAs("user")()
	if !emuLabIDRe.MatchString(lab) {
		return 0, fmt.Errorf("%q is not a lab", lab)
	}
	emuLab.mu.Lock()
	a.emuLoad()
	open := emuLab.f.Lab != nil && emuLab.f.Lab.ID == lab
	emuLab.mu.Unlock()
	if open {
		return 0, errors.New("that is the open topology: stop its switches with Stop All")
	}
	out, err := a.emuRun(host, emuStopLabScript(lab), time.Minute)
	if err != nil {
		return 0, err
	}
	n := 0
	if i := strings.Index(out, "@@stopped "); i >= 0 {
		n, _ = strconv.Atoi(strings.TrimSpace(strings.Fields(out[i+10:])[0]))
	}
	emuRunCache.mu.Lock()
	emuRunCache.rows = nil
	emuRunCache.mu.Unlock()
	emuEvOn(host, "", "info", fmt.Sprintf("switched off %d switches of lab-%s running in the background", n, lab))
	return n, nil
}
