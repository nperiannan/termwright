package app

import (
	"fmt"
	"io/fs"
	"math"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// What the AI assistant knows about Termwright itself: the guides that ship with this version (searched a few
// sections at a time - "help"), and Termwright's own state now ("status": its version, KVM hosts and the VMs on
// each, labs, teams, file servers, tabs).

// aiDocs are the guides that ship with this version (Use).
var aiDocs fs.FS

type aiDocChunk struct {
	where string // "User Guide › Teams"
	text  string
	low   string
}

var aiDocIndex []aiDocChunk

// aiDocChunks splits the guides into sections and long bullets, each with the headings above it.
func aiDocChunks() []aiDocChunk {
	if aiDocIndex != nil {
		return aiDocIndex
	}
	files := []struct{ path, name string }{{"docs/USER_GUIDE.md", "User Guide"}, {"docs/FEATURES.md", "Features"},
		{"docs/TEAM_SERVER.md", "Team server guide"}, {"docs/EMULATOR_GUIDE.md", "Emulated lab guide"}, {"CHANGELOG.md", "Changes"}}
	var out []aiDocChunk
	for _, f := range files {
		if aiDocs == nil {
			break
		}
		b, err := fs.ReadFile(aiDocs, f.path)
		if err != nil {
			continue
		}
		text := string(b)
		if f.path == "CHANGELOG.md" && len(text) > 140000 { // the recent releases; older ones are in the guides by now
			text = text[:140000]
		}
		heads := []string{}
		var cur []string
		flush := func() {
			t := strings.TrimSpace(strings.Join(cur, "\n"))
			cur = nil
			if len(t) < 40 {
				return
			}
			where := f.name
			if len(heads) > 0 {
				where += " › " + strings.Join(heads, " › ")
			}
			out = append(out, aiDocChunk{where: where, text: t, low: strings.ToLower(where + "\n" + t)})
		}
		for _, l := range strings.Split(text, "\n") {
			if h := strings.TrimLeft(l, "#"); len(h) < len(l) && strings.HasPrefix(h, " ") && len(l)-len(h) <= 4 {
				flush()
				lv := len(l) - len(h)
				if lv-1 < len(heads) {
					heads = heads[:max(0, lv-1)]
				}
				for len(heads) < lv-1 {
					heads = append(heads, "")
				}
				heads = append(heads[:lv-1], strings.TrimSpace(h))
				continue
			}
			if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") {
				flush()
			}
			if strings.TrimSpace(l) != "" {
				cur = append(cur, l)
			}
		}
		flush()
	}
	aiDocIndex = out
	return out
}

var aiStop = map[string]bool{"the": true, "a": true, "an": true, "is": true, "are": true, "do": true, "does": true, "we": true, "i": true, "to": true,
	"of": true, "in": true, "on": true, "for": true, "and": true, "or": true, "it": true, "can": true, "how": true, "what": true, "where": true,
	"there": true, "any": true, "termwright": true, "with": true, "from": true, "my": true, "me": true, "this": true, "that": true, "be": true}

// aiSyn: how people ask, and the words the guides use for it.
var aiSyn = map[string][]string{
	"using": {"use", "open"}, "uses": {"use", "open"}, "used": {"use", "open"}, "busy": {"in use", "open", "busy"}, "occupied": {"in use", "open"},
	"another": {"someone else", "other"}, "other": {"someone else", "other"}, "others": {"someone else", "other"}, "else": {"else"},
	"member": {"member", "someone"}, "user": {"member", "someone", "user"}, "colleague": {"member", "someone"}, "people": {"member", "someone", "people"},
	"vm": {"vm", "switch", "computer", "device"}, "vms": {"vm", "switch", "computer", "device"}, "virtual": {"virtual", "emulated"},
	"book": {"reserv"}, "booking": {"reserv"}, "reserve": {"reserv"}, "reserved": {"reserv"}, "reservation": {"reserv"},
	"password": {"password", "credential", "login"}, "credentials": {"credential", "login", "password"},
	"upgrade": {"update"}, "update": {"update"}, "version": {"version", "release"},
}

func aiStem(w string) string {
	for _, s := range []string{"ing", "ed", "es", "s"} {
		if len(w) > len(s)+3 && strings.HasSuffix(w, s) {
			return strings.TrimSuffix(w, s)
		}
	}
	return w
}

// AIHelp finds the sections of the guides of this version that answer a question about Termwright (at most 3,
// about 7000 characters in all).
func (a *App) AIHelp(query string) string {
	var words []string
	for _, w := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.')
	}) {
		if w = strings.Trim(w, "-."); len(w) > 1 && !aiStop[w] {
			words = append(words, w)
		}
	}
	alts := func(w string) []string {
		if l, ok := aiSyn[w]; ok {
			return l
		}
		return []string{aiStem(w)}
	}
	count := func(text, w string) int {
		n := 0
		for _, x := range alts(w) {
			n += strings.Count(text, x)
		}
		return n
	}
	if len(words) == 0 {
		return "Say what to look up, e.g. help team in use."
	}
	chunks := aiDocChunks()
	df := map[string]int{}
	for _, c := range chunks {
		for _, w := range words {
			if count(c.low, w) > 0 {
				df[w]++
			}
		}
	}
	type hit struct {
		i     int
		score float64
	}
	var hits []hit
	for i, c := range chunks {
		s, found := 0.0, 0
		head := strings.ToLower(c.where)
		for _, w := range words {
			n := count(c.low, w)
			if n == 0 {
				continue
			}
			found++
			idf := math.Log(1 + float64(len(chunks))/float64(1+df[w]))
			s += idf * (1 + math.Log(float64(min(n, 8))))
			if count(head, w) > 0 {
				s += 2 * idf
			}
		}
		if found == 0 {
			continue
		}
		s *= float64(found) / float64(len(words)) // sections that have all the words first
		if strings.HasPrefix(c.where, "Changes") {
			s *= 0.7 // the guides describe; the change log says when
		}
		hits = append(hits, hit{i, s})
	}
	if len(hits) == 0 {
		return "Nothing in the guides of Termwright " + Version + " matches \"" + query + "\". Try other words."
	}
	sort.Slice(hits, func(x, y int) bool { return hits[x].score > hits[y].score })
	var b strings.Builder
	fmt.Fprintf(&b, "From the guides of Termwright %s (the version running):\n", Version)
	for k, h := range hits {
		if k == 3 {
			break
		}
		c := chunks[h.i]
		t := c.text
		if len(t) > 2400 {
			t = t[:2400] + " …"
		}
		fmt.Fprintf(&b, "\n[%s]\n%s\n", c.where, t)
	}
	return b.String()
}

// AIStatus is Termwright's own state now, in a few lines: its version, the tabs, the teams and who uses which
// team device, the emulated lab's KVM hosts with the VMs on each, the simulated lab and the file servers.
func (a *App) AIStatus() string {
	var b strings.Builder
	kind := "a release"
	if strings.Contains(Version, "~git") || strings.HasSuffix(Version, "-dev") {
		kind = "a development (nightly) build"
	}
	fmt.Fprintf(&b, "Termwright %s (%s) on %s/%s.\n", Version, kind, runtime.GOOS, runtime.GOARCH)
	if a.store != nil {
		if all, err := a.store.AllSessions(nil); err == nil {
			fmt.Fprintf(&b, "Saved sessions: %d.\n", len(all))
		}
	}
	if a.terms != nil {
		var open []string
		for _, t := range a.terms.List() {
			open = append(open, t.Title+" ("+t.State+")")
		}
		fmt.Fprintf(&b, "Tabs open: %d%s.\n", len(open), map[bool]string{true: ": " + strings.Join(open, ", "), false: ""}[len(open) > 0])
	}
	// teams: who has which device open or reserved (the presence the Session Manager shows)
	if a.cfg != nil && len(a.cfg.Get().Teams) > 0 {
		st := map[string]TeamStatusView{}
		for _, s := range a.TeamStatuses() {
			st[s.TeamID] = s
		}
		for _, t := range a.cfg.Get().Teams {
			fmt.Fprintf(&b, "Team %s (server %s).", t.Name, t.Address)
			s, ok := st[t.ID]
			if !ok {
				b.WriteString(" Its status is not known yet.\n")
				continue
			}
			var use []string
			for _, u := range s.Status.InUse {
				use = append(use, fmt.Sprintf("%s by %s since %s", aiRef(u.Device.Host, u.Device.Port), u.Name, aiWhen(u.Since)))
			}
			var res []string
			for _, r := range s.Status.Reserved {
				res = append(res, fmt.Sprintf("%s by %s until %s%s", aiRef(r.Device.Host, r.Device.Port), r.Name, aiWhen(r.Until), map[bool]string{true: " (" + r.Note + ")", false: ""}[r.Note != ""]))
			}
			fmt.Fprintf(&b, " Open by others now: %s. Reserved: %s.\n", orNone(use), orNone(res))
		}
	}
	// the emulated lab: each KVM host with the VMs on it
	emuLab.mu.Lock()
	a.emuLoad()
	if len(emuLab.f.Hosts) > 0 || emuLab.f.Lab != nil {
		hosts := map[string][]string{}
		var order []string
		for _, h := range emuLab.f.Hosts {
			hosts[h.Session] = nil
			order = append(order, h.Session)
		}
		running := 0
		if l := emuLab.f.Lab; l != nil {
			for _, v := range l.VMs {
				h := emuHostOf(v)
				if _, ok := hosts[h]; !ok {
					order = append(order, h)
				}
				st := "stopped"
				if emuRunning(v.ID) {
					running++
					st = "running"
					if !emuLab.ready[v.ID] {
						st = "booting"
					}
				}
				im := v.Image
				if x := emuImage(v.Image); x != nil {
					im = strings.TrimSpace(x.Name + " " + x.Version)
				}
				hosts[h] = append(hosts[h], fmt.Sprintf("%s (%s, %s)", v.Name, im, st))
			}
			fmt.Fprintf(&b, "Emulated lab: %d devices, %d running; %d KVM host(s).\n", len(l.VMs), running, len(emuLab.f.Hosts))
		} else {
			fmt.Fprintf(&b, "Emulated lab: none open; %d KVM host(s).\n", len(emuLab.f.Hosts))
		}
		for _, h := range order {
			line := "- " + emuHostName(h)
			for _, x := range emuLab.f.Hosts {
				if x.Session == h && x.Info.CPUs > 0 {
					line += fmt.Sprintf(" [%s, %d CPUs, %.0f GB memory (%.0f GB free), KVM %s, load %.1f]", strings.TrimSpace(x.Info.OS), x.Info.CPUs,
						float64(x.Info.MemTotal)/1024, float64(x.Info.MemFree)/1024, orStr(x.Info.KVM, "?"), x.Info.Load)
				}
			}
			if h == emuLab.f.Host {
				line += " (the lab's host)"
			}
			if vs := hosts[h]; len(vs) > 0 {
				line += ": " + strings.Join(vs, ", ")
			} else {
				line += ": no VMs"
			}
			b.WriteString(line + "\n")
		}
		var inst []string
		for _, j := range emuLab.f.Installs {
			if j.Stage != "done" && j.Stage != "failed" && j.Stage != "cancelled" {
				inst = append(inst, j.Name+" ("+j.Stage+")")
			}
		}
		if len(inst) > 0 {
			fmt.Fprintf(&b, "Installs from ISO running: %s.\n", strings.Join(inst, ", "))
		}
	}
	emuLab.mu.Unlock()
	if a.cfg == nil { // not started yet (tests)
		return b.String()
	}
	if v, err := a.LabState(); err == nil && v.Exists {
		on := 0
		for _, d := range v.Devices {
			if d.Running {
				on++
			}
		}
		pack := "no device pack"
		if v.Pack != nil {
			pack = "device pack " + v.Pack.Name + " " + v.Pack.Version
		}
		fmt.Fprintf(&b, "Simulated lab: %d switches, %d running (%s).\n", len(v.Devices), on, pack)
	}
	var fs []string
	for _, s := range a.FileServers().State.Servers {
		if s.Running {
			fs = append(fs, fmt.Sprintf("%s on %s:%d", strings.ToUpper(s.Kind), orStr(s.Address, "all addresses"), s.Port))
		}
	}
	fmt.Fprintf(&b, "File servers running: %s.\n", orNone(fs))
	return b.String()
}

func aiRef(host string, port int) string {
	if port == 0 || port == 22 {
		return host
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func aiWhen(t string) string {
	if p, err := time.Parse(time.RFC3339, t); err == nil {
		return p.Local().Format("Mon 15:04")
	}
	return t
}

func orNone(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, "; ")
}

var _ = emu.LocalHost
