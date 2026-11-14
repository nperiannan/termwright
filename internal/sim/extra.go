package sim

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// What the simulator does not model itself: configuration commands it does
// not know are taken and kept (shown in the configuration), and show commands
// can be given the output of a real switch (with placeholders), so scripts,
// templates and tools that read real output work. It does not run what the
// commands configure.

// Extra is a configuration line the simulator keeps without modelling it.
type Extra struct {
	Ctx  string `json:"ctx,omitempty"` // the interface it belongs to ("" = global)
	Line string `json:"line"`
}

// Output is the text a show command prints, for one OS ("" = both).
type Output struct {
	OS      string `json:"os"`      // a pack OS id ("" = every)
	Command string `json:"command"` // "show …"
	Text    string `json:"text"`    // placeholders: {hostname} {serial} {mac} {mgmtIp} {mgmtAddr} {model} {version} {uptime} {time} {line:prefix|default} {word:prefix|default} {if:prefix|yes|no}
}

// acceptConfig keeps a configuration command the pack does not model (its keep rules).
func (s *Shell) acceptConfig(text string, words []string) bool {
	k := s.d.osd.Keep
	if k == nil || (k.Modes != "" && !strings.Contains(k.Modes, s.mode)) {
		return false
	}
	line := strings.Join(words, " ")
	ctx := ""
	if len(s.ifPorts) > 0 && k.CtxPorts != "" {
		ctx = strings.ReplaceAll(k.CtxPorts, "{ports}", portList(s.ifPorts, s.d.style()))
	} else if s.ifVLAN != 0 && k.CtxVLAN != "" {
		ctx = strings.ReplaceAll(k.CtxVLAN, "{id}", itoa(s.ifVLAN))
	}
	if k.No != "" && strings.EqualFold(words[0], k.No) && len(words) > 1 {
		s.d.dropExtra(ctx, strings.Join(words[1:], " "))
		s.d.changed()
		return true
	}
	if len(k.Verbs) == 0 {
		s.d.keepExtra(ctx, line, true)
		s.d.changed()
		return true
	}
	verb := ""
	for _, v := range k.Verbs {
		if len(words[0]) >= 3 && strings.HasPrefix(v, strings.ToLower(words[0])) {
			verb = v
		}
	}
	if verb == "" || len(words) < 2 {
		return false
	}
	line = verb + " " + strings.Join(words[1:], " ")
	rest := strings.Join(words[1:], " ")
	// the opposite of a kept line takes it back ("unconfigure x" drops "configure x")
	if opp := k.Opposites[verb]; opp != "" && s.d.dropExtra(ctx, opp+" "+rest) && k.Opposites[opp] != verb {
		s.d.changed()
		return true
	}
	s.d.keepExtra(ctx, line, true)
	s.d.changed()
	return true
}

// keepExtra adds a line; replace = a kept line of the same length that differs
// in one word only (the value) is replaced by it.
func (d *Device) keepExtra(ctx, line string, replace bool) {
	f := strings.Fields(line)
	for i, e := range d.cur.Extra {
		if e.Ctx != ctx {
			continue
		}
		if e.Line == line {
			return
		}
		g := strings.Fields(e.Line)
		if !replace || len(g) != len(f) || len(f) < 3 {
			continue
		}
		diff := 0
		for j := range f {
			if !strings.EqualFold(f[j], g[j]) {
				diff++
			}
		}
		if diff == 1 {
			d.cur.Extra[i].Line = line
			return
		}
	}
	d.cur.Extra = append(d.cur.Extra, Extra{Ctx: ctx, Line: line})
}

// dropExtra removes the kept lines that start with line.
func (d *Device) dropExtra(ctx, line string) bool {
	var keep []Extra
	found := false
	for _, e := range d.cur.Extra {
		if e.Ctx == ctx && (strings.EqualFold(e.Line, line) || strings.HasPrefix(strings.ToLower(e.Line), strings.ToLower(line)+" ")) {
			found = true
			continue
		}
		keep = append(keep, e)
	}
	d.cur.Extra = keep
	return found
}

// template prints the output kept for a show command (true = it had one).
func (s *Shell) template(words []string) bool {
	if len(words) == 0 || !strings.HasPrefix("show", strings.ToLower(words[0])) || len(words[0]) < 2 {
		return false
	}
	for _, o := range s.d.lab.outputs {
		if o.OS != "" && o.OS != s.d.OS {
			continue
		}
		cw := strings.Fields(o.Command)
		if len(cw) != len(words) {
			continue
		}
		ok := true
		for i, w := range words {
			if !strings.HasPrefix(strings.ToLower(cw[i]), strings.ToLower(w)) {
				ok = false
				break
			}
		}
		if ok {
			t := s.d.fill(o.Text)
			if !strings.HasSuffix(t, "\n") {
				t += "\n"
			}
			s.p("%s", t)
			return true
		}
	}
	return false
}

var placeholder = regexp.MustCompile(`\{(hostname|serial|mac|mgmtIp|mgmtAddr|model|version|uptime|time|line:[^}|]+|word:[^}|]+|if:[^}|]+)(\|[^}]*)?\}`)

// fill puts the switch's values in an output.
func (d *Device) fill(t string) string {
	return placeholder.ReplaceAllStringFunc(t, func(m string) string {
		sub := placeholder.FindStringSubmatch(m)
		key, def := sub[1], strings.TrimPrefix(sub[2], "|")
		v := ""
		switch {
		case key == "hostname":
			v = d.cur.Hostname
		case key == "serial":
			v = d.Serial
		case key == "mac":
			v = d.MAC
		case key == "mgmtIp":
			v = d.cur.MgmtIP
		case key == "mgmtAddr":
			if p, err := netip.ParsePrefix(d.cur.MgmtIP); err == nil {
				v = p.Addr().String()
			}
		case key == "model":
			v = d.Model
		case key == "version":
			v = d.Version
		case key == "uptime":
			v = uptime(d.booted)
		case key == "time":
			v = time.Now().Format("Mon Jan  2 15:04:05 2006")
		case strings.HasPrefix(key, "if:"):
			yes, no, _ := strings.Cut(def, "|")
			pre := strings.ToLower(strings.TrimSpace(key[3:]))
			for _, e := range d.lines() {
				if l := strings.ToLower(e); l == pre || strings.HasPrefix(l, pre+" ") {
					return yes
				}
			}
			return no
		case strings.HasPrefix(key, "line:"), strings.HasPrefix(key, "word:"):
			pre := strings.ToLower(strings.TrimSpace(key[5:]))
			for _, e := range d.lines() {
				if l := strings.ToLower(e); strings.HasPrefix(l, pre+" ") {
					v = strings.TrimSpace(e[len(pre):])
					if key[0] == 'w' {
						v = strings.Fields(v)[0]
					}
					break
				}
			}
		}
		if v == "" {
			v = def
		}
		return v
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

// SetOutputs replaces the kept show outputs.
func (l *Lab) SetOutputs(o []Output) { l.mu.Lock(); l.outputs = o; l.mu.Unlock() }

// Outputs returns the kept show outputs.
func (l *Lab) Outputs() []Output {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Output(nil), l.outputs...)
}

// SetIdentity changes a switch's serial number and MAC address ("" keeps it).
func (l *Lab) SetIdentity(id, serial, mac string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.Device(id)
	if d == nil {
		return false
	}
	if serial = strings.TrimSpace(serial); serial != "" {
		d.Serial = serial
	}
	if mac = strings.TrimSpace(mac); mac != "" {
		d.MAC = mac
	}
	return true
}

// lines: the configuration lines the placeholders look in (kept lines, and
// the pack's values as "kv <key> <value>").
func (d *Device) lines() []string {
	var out []string
	for k, v := range d.cur.KV {
		out = append(out, "kv "+k+" "+v)
	}
	for _, e := range d.cur.Extra {
		out = append(out, e.Line)
	}
	return out
}
