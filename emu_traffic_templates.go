package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// EmuTrafficTemplate is a ready test of the traffic generator: streams and saved tests between test ports
// named by their role (A, B, C…), put into the lab on the test ports chosen for the roles.
type EmuTrafficTemplate struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Desc    string            `json:"desc,omitempty"`    // what it checks
	Builtin bool              `json:"builtin,omitempty"` // one of Termwright's (cannot be removed)
	Ports   int               `json:"ports"`             // how many test ports it needs (roles A, B, C…)
	Roles   []string          `json:"roles,omitempty"`   // what each role is for ("sender", "receiver"…)
	Streams []emu.Stream      `json:"streams,omitempty"` // From/To are roles (A, B…)
	Tests   []emu.TrafficTest `json:"tests,omitempty"`   // From/To/Monitor are roles
	Saved   string            `json:"saved,omitempty"`   // when it was saved (RFC 3339; yours)
}

var emuRoles = []string{"A", "B", "C", "D", "E", "F"}

// emuBuiltinTemplates: the library's own tests (common checks of switches, as hardware testers ship them).
func emuBuiltinTemplates() []EmuTrafficTemplate {
	st := func(name, from, to string, f func(*emu.Stream)) emu.Stream {
		s := emu.Stream{Name: name, From: from, To: to, Kind: "udp", Size: 512, PPS: 1000, Seconds: 30, MaxLoss: 0}
		if f != nil {
			f(&s)
		}
		return s
	}
	ts := []EmuTrafficTemplate{
		{ID: "b-unicast", Name: "Unicast both ways", Desc: "512-byte frames A → B and B → A at 1 % of line rate for 30 s; passes with no loss.", Ports: 2, Roles: []string{"one end", "the other end"},
			Streams: []emu.Stream{st("A to B", "A", "B", func(s *emu.Stream) { s.RateUnit, s.Rate = "pct", 1 }), st("B to A", "B", "A", func(s *emu.Stream) { s.RateUnit, s.Rate = "pct", 1 })}},
		{ID: "b-imix", Name: "IMIX both ways", Desc: "IMIX (64/594/1518 B, 7:4:1) A ↔ B at 2 % of line rate for 60 s; at most 0.1 % loss.", Ports: 2, Roles: []string{"one end", "the other end"},
			Streams: []emu.Stream{st("IMIX A to B", "A", "B", func(s *emu.Stream) { s.SizeMode, s.RateUnit, s.Rate, s.Seconds, s.MaxLoss = "imix", "pct", 2, 60, 0.1 }), st("IMIX B to A", "B", "A", func(s *emu.Stream) { s.SizeMode, s.RateUnit, s.Rate, s.Seconds, s.MaxLoss = "imix", "pct", 2, 60, 0.1 })}},
		{ID: "b-qos", Name: "Voice, video and data (QoS)", Desc: "Three classes A → B: voice (EF, DSCP 46, 200 B, 50 pps, latency ≤ 5 ms, jitter ≤ 1 ms), video (AF41, DSCP 34, 1200 B, 500 pps) and data (DSCP 0, 1500 B, 2000 pps). The marks the frames arrive with show remarking.", Ports: 2, Roles: []string{"sender", "receiver"},
			Streams: []emu.Stream{
				st("voice EF", "A", "B", func(s *emu.Stream) {
					s.DSCP, s.Size, s.PPS, s.Seconds, s.MaxLatency, s.MaxJitter = 46, 200, 50, 60, 5, 1
				}),
				st("video AF41", "A", "B", func(s *emu.Stream) { s.DSCP, s.Size, s.PPS, s.Seconds, s.MaxLoss = 34, 1200, 500, 60, 0.5 }),
				st("data BE", "A", "B", func(s *emu.Stream) { s.Size, s.PPS, s.Seconds, s.MaxLoss = 1500, 2000, 60, 5 })}},
		{ID: "b-qos-congest", Name: "QoS under congestion", Desc: "A and B send into C at 130 % of line rate together for 60 s: high (EF, DSCP 46, PCP 5) 10 % from A, medium (AF41, DSCP 34, PCP 4) 30 % from A, best effort 90 % from B. Passes when the high class loses nothing and keeps its latency (≤ 5 ms), and the medium class loses ≤ 0.5 %; best effort takes the loss (not checked). Give the three test ports the line rate of the switch port C is on (or shape that port), and use VLAN tags when the switch trusts 802.1p.", Ports: 3, Roles: []string{"sender (high and medium)", "sender (best effort)", "receiver (the congested port)"},
			Streams: []emu.Stream{
				st("high EF", "A", "C", func(s *emu.Stream) {
					s.DSCP, s.Prio, s.Size, s.RateUnit, s.Rate, s.Seconds, s.MaxLoss, s.MaxLatency = 46, 5, 256, "pct", 10, 60, 0, 5
				}),
				st("medium AF41", "A", "C", func(s *emu.Stream) {
					s.DSCP, s.Prio, s.Size, s.RateUnit, s.Rate, s.Seconds, s.MaxLoss = 34, 4, 1024, "pct", 30, 60, 0.5
				}),
				st("best effort", "B", "C", func(s *emu.Stream) { s.Size, s.RateUnit, s.Rate, s.Seconds, s.MaxLoss = 1500, "pct", 90, 60, 100 })}},
		{ID: "b-macfill", Name: "MAC table fill", Desc: "4,096 source MACs from A to B at 2,000 pps for 20 s: each MAC is a flow, so the flows with loss show the addresses the switch did not learn.", Ports: 2, Roles: []string{"sender (the MACs)", "receiver"},
			Streams: []emu.Stream{st("4096 MACs", "A", "B", func(s *emu.Stream) {
				s.Size, s.PPS, s.Seconds, s.MaxLoss = 128, 2000, 20, 1
				s.Mods = []emu.Mod{{Field: "srcMac", Mode: "inc", Count: 4096, Step: 1}}
			})}},
		{ID: "b-rtt", Name: "Round-trip latency", Desc: "64-byte frames A → B and back at 100 pps for 30 s: latency measured on A's clock, also when A and B are on two hosts. Passes at ≤ 10 ms.", Ports: 2, Roles: []string{"sender", "reflector"},
			Streams: []emu.Stream{st("round trip", "A", "B", func(s *emu.Stream) { s.Size, s.PPS, s.RTT, s.MaxLatency = 64, 100, true, 10 })}},
		{ID: "b-flood", Name: "Broadcast containment", Desc: "Broadcast frames from A at 200 pps for 20 s: every other test port should get them (B and C), and the loss shows a port that did not.", Ports: 3, Roles: []string{"sender", "receiver", "receiver"},
			Streams: []emu.Stream{st("broadcast", "A", "", func(s *emu.Stream) { s.Kind, s.Size, s.PPS, s.Seconds = "raw", 128, 200, 20 })}},
		{ID: "b-rfc2544", Name: "RFC 2544 throughput and latency", Desc: "The zero-loss throughput at 64 to 1518 B both ways, then the latency at that rate.", Ports: 2, Roles: []string{"one end", "the other end"},
			Tests: []emu.TrafficTest{{Name: "RFC 2544 throughput", Kind: "throughput", From: "A", To: "B", Both: true}, {Name: "RFC 2544 latency", Kind: "latency", From: "A", To: "B"}}},
		{ID: "b-b2b", Name: "RFC 2544 back-to-back", Desc: "The longest burst A → B takes without loss, per frame size.", Ports: 2, Roles: []string{"sender", "receiver"},
			Tests: []emu.TrafficTest{{Name: "RFC 2544 back-to-back", Kind: "back-to-back", From: "A", To: "B"}}},
		{ID: "b-learning", Name: "RFC 2889 address learning", Desc: "How many addresses the switch learns from A for B, with C watching for frames still flooded.", Ports: 3, Roles: []string{"sender", "receiver", "monitor"},
			Tests: []emu.TrafficTest{{Name: "RFC 2889 learning", Kind: "learning", From: "A", To: "B", Monitor: "C"}}},
	}
	for i := range ts {
		ts[i].Builtin = true
	}
	return ts
}

func (a *App) emuTemplatesPath() string { return filepath.Join(a.labDir(), "traffic-templates.json") }

func (a *App) emuMyTemplates() []EmuTrafficTemplate {
	var ts []EmuTrafficTemplate
	if b, err := os.ReadFile(a.emuTemplatesPath()); err == nil {
		_ = json.Unmarshal(b, &ts)
	}
	return ts
}

// EmuTrafficTemplates is the library: Termwright's own tests, then yours.
func (a *App) EmuTrafficTemplates() []EmuTrafficTemplate {
	return append(emuBuiltinTemplates(), a.emuMyTemplates()...)
}

// EmuTrafficTemplateSave keeps the lab's streams and saved tests (all, or the ones named) as a template of yours;
// their test ports become roles A, B, C… in the order they first appear. A template of yours with that name is replaced.
func (a *App) EmuTrafficTemplateSave(name, desc string, ids []string) (EmuTrafficTemplate, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return EmuTrafficTemplate{}, errors.New("give the template a name")
	}
	if slices.ContainsFunc(emuBuiltinTemplates(), func(t EmuTrafficTemplate) bool { return strings.EqualFold(t.Name, name) }) {
		return EmuTrafficTemplate{}, errors.New("that is the name of one of Termwright's templates: choose another")
	}
	tv, err := a.EmuTraffic()
	if err != nil {
		return EmuTrafficTemplate{}, err
	}
	want := func(id, nm string) bool { return len(ids) == 0 || slices.Contains(ids, id) || slices.Contains(ids, nm) }
	role := map[string]string{}
	var order []string
	as := func(id string) string {
		if id == "" {
			return ""
		}
		if r, ok := role[id]; ok {
			return r
		}
		if len(order) == len(emuRoles) {
			return ""
		}
		role[id] = emuRoles[len(order)]
		order = append(order, id)
		return role[id]
	}
	t := EmuTrafficTemplate{ID: "u" + randHex(4), Name: name, Desc: strings.TrimSpace(desc), Saved: time.Now().Format(time.RFC3339)}
	for _, s := range tv.Streams {
		if s.Test != "" || !want(s.ID, s.Name) {
			continue
		}
		s.ID, s.From, s.To = "", as(s.From), as(s.To)
		t.Streams = append(t.Streams, s)
	}
	for _, x := range a.EmuTests() {
		if !want(x.ID, x.Name) {
			continue
		}
		x.ID, x.From, x.To, x.Monitor = "", as(x.From), as(x.To), as(x.Monitor)
		t.Tests = append(t.Tests, x)
	}
	if len(t.Streams)+len(t.Tests) == 0 {
		return EmuTrafficTemplate{}, errors.New("no stream or test to keep")
	}
	if len(order) > len(emuRoles) {
		return EmuTrafficTemplate{}, fmt.Errorf("at most %d test ports in a template", len(emuRoles))
	}
	t.Ports = len(order)
	names := map[string]string{}
	for _, p := range tv.Testers {
		names[p.ID] = p.Name
	}
	for _, id := range order {
		t.Roles = append(t.Roles, "was "+names[id])
	}
	mine := slices.DeleteFunc(a.emuMyTemplates(), func(x EmuTrafficTemplate) bool { return strings.EqualFold(x.Name, name) })
	mine = append(mine, t)
	b, _ := json.MarshalIndent(mine, "", "  ")
	if err := os.WriteFile(a.emuTemplatesPath(), b, 0o644); err != nil {
		return EmuTrafficTemplate{}, err
	}
	return t, nil
}

// EmuTrafficTemplateRemove removes a template of yours.
func (a *App) EmuTrafficTemplateRemove(id string) error {
	mine := a.emuMyTemplates()
	n := len(mine)
	mine = slices.DeleteFunc(mine, func(x EmuTrafficTemplate) bool { return x.ID == id || strings.EqualFold(x.Name, id) })
	if len(mine) == n {
		return errors.New("no such template of yours (Termwright's own cannot be removed)")
	}
	b, _ := json.MarshalIndent(mine, "", "  ")
	return os.WriteFile(a.emuTemplatesPath(), b, 0o644)
}

// EmuTrafficTemplateApply puts a template into the lab: its streams and tests on the test ports given for its roles
// (ports[0] is A…; ids or names). Names already taken get a number.
func (a *App) EmuTrafficTemplateApply(id string, ports []string) (EmuTrafficView, error) {
	i := slices.IndexFunc(a.EmuTrafficTemplates(), func(t EmuTrafficTemplate) bool { return t.ID == id || strings.EqualFold(t.Name, id) })
	if i < 0 {
		return EmuTrafficView{}, errors.New("no such template")
	}
	t := a.EmuTrafficTemplates()[i]
	tv, err := a.EmuTraffic()
	if err != nil {
		return tv, err
	}
	if len(ports) < t.Ports {
		return tv, fmt.Errorf("%s needs %d test ports (%s): add test ports first", t.Name, t.Ports, strings.Join(emuRoles[:t.Ports], ", "))
	}
	byRole := map[string]string{}
	for k := 0; k < t.Ports; k++ {
		j := slices.IndexFunc(tv.Testers, func(x emu.Tester) bool { return x.ID == ports[k] || strings.EqualFold(x.Name, ports[k]) })
		if j < 0 {
			return tv, fmt.Errorf("no test port %s", ports[k])
		}
		for r, have := range byRole {
			if have == tv.Testers[j].ID {
				return tv, fmt.Errorf("%s is role %s already: each role needs its own test port", ports[k], r)
			}
		}
		byRole[emuRoles[k]] = tv.Testers[j].ID
	}
	taken := map[string]bool{}
	for _, s := range tv.Streams {
		taken[strings.ToLower(s.Name)] = true
	}
	uniq := func(n string) string {
		out := n
		for k := 2; taken[strings.ToLower(out)]; k++ {
			out = fmt.Sprintf("%s (%d)", n, k)
		}
		taken[strings.ToLower(out)] = true
		return out
	}
	for _, s := range t.Streams {
		s.ID, s.Name, s.From, s.To = "", uniq(s.Name), byRole[s.From], byRole[s.To]
		if tv, err = a.EmuStreamSave(s); err != nil {
			return tv, fmt.Errorf("%s: %w", s.Name, err)
		}
	}
	tests := map[string]bool{}
	for _, x := range a.EmuTests() {
		tests[strings.ToLower(x.Name)] = true
	}
	for _, x := range t.Tests {
		n := x.Name
		for k := 2; tests[strings.ToLower(n)]; k++ {
			n = fmt.Sprintf("%s (%d)", x.Name, k)
		}
		tests[strings.ToLower(n)] = true
		x.ID, x.Name, x.From, x.To, x.Monitor = "", n, byRole[x.From], byRole[x.To], byRole[x.Monitor]
		if _, err := a.EmuTestSave(x); err != nil {
			return tv, fmt.Errorf("%s: %w", x.Name, err)
		}
	}
	return a.EmuTraffic()
}
