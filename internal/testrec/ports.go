package testrec

import (
	"regexp"
	"strconv"
	"strings"
)

// PortCount is how many port variables a device role has by default: {{role.port1}} … {{role.port8}}.
// A testbed can give a device more or fewer (role.port_count), e.g. all its ports for a snake test.
const PortCount = 8

// slotPorts reads a stack's operational slots ("1:28,2:28,3:28": slot:ports, from show slot).
func slotPorts(s string) (slots []string, ports map[string]int) {
	ports = map[string]int{}
	for _, x := range strings.Split(s, ",") {
		sl, n, ok := strings.Cut(strings.TrimSpace(x), ":")
		if c, err := strconv.Atoi(n); ok && err == nil && sl != "" {
			slots = append(slots, sl)
			ports[sl] = c
		}
	}
	return slots, ports
}

// DefaultPorts are a device's test ports by default (n of them; 0 = PortCount): a VOSS switch slot/1…,
// an EXOS stack slot:2… (port 1 is left for the uplink; past the slot's last port it goes on with the
// next operational slot from port 1 - slots is "slot:ports,…" from show slot), a standalone EXOS
// switch 2…. Other device types have none (set them in the testbed).
func DefaultPorts(deviceType string, stack bool, slot string, n int, slots string) []string {
	if n <= 0 {
		n = PortCount
	}
	sl, per := slotPorts(slots)
	if slot == "" {
		slot = "1"
		if len(sl) > 0 {
			slot = sl[0]
		}
	}
	dt := strings.ToLower(deviceType)
	var out []string
	switch {
	case strings.Contains(dt, "voss") || strings.Contains(dt, "fabric engine"):
		for i := 1; i <= n; i++ {
			out = append(out, slot+"/"+strconv.Itoa(i))
		}
	case strings.Contains(dt, "exos") || strings.Contains(dt, "switch engine"):
		if !stack {
			for i := 1; i <= n; i++ {
				out = append(out, strconv.Itoa(i+1))
			}
			return out
		}
		cur, p := slot, 2
		next := func() bool { // the operational slot after cur
			for i, x := range sl {
				if x == cur && i+1 < len(sl) {
					cur, p = sl[i+1], 1
					return true
				}
			}
			return false
		}
		for len(out) < n {
			if max := per[cur]; max > 0 && p > max && !next() {
				break
			}
			out = append(out, cur+":"+strconv.Itoa(p))
			p++
		}
	}
	return out
}

// addPorts gives a device role its port variables ({{role.port1}}…, as many as role.port_count, and
// {{role.ports}}: all of them, comma-separated) from its device type, unless the testbed or the run sets
// them (explicit); the test's own defaults are for when the device type has none. A role is a stack when
// its facts or the testbed say so (role.stack: true, or a role.slot).
func addPorts(k string, v, explicit map[string]string) {
	stack := v[k+".stack"] == "true" || v[k+".slot"] != ""
	n, _ := strconv.Atoi(v[k+".port_count"])
	if n <= 0 {
		n = PortCount
	}
	ps := DefaultPorts(v[k+".device_type"], stack, v[k+".slot"], n, v[k+".slots"])
	var all []string
	for i := 1; i <= n; i++ {
		key := k + ".port" + strconv.Itoa(i)
		if explicit[key] == "" && i <= len(ps) {
			v[key] = ps[i-1]
		}
		if v[key] != "" {
			all = append(all, v[key])
		}
	}
	if explicit[k+".ports"] == "" && len(all) > 0 {
		v[k+".ports"] = strings.Join(all, ",")
	}
}

var (
	portTok  = regexp.MustCompile(`\d+[:/]\d+|\d+`)
	portList = regexp.MustCompile(`(?i)\b(ports?)(\s+)([\d,\- ]*\d)`)
)

var singleBrace = regexp.MustCompile(`\{+((?:[\w-]+\.)?ports?\d*)\}+`)

// fixBraces turns {port1} (one brace, as people type it) into {{port1}}.
func fixBraces(s string) (string, int) {
	n := 0
	out := singleBrace.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, "{{") && strings.HasSuffix(m, "}}") {
			return m
		}
		n++
		return "{{" + strings.Trim(m, "{}") + "}}"
	})
	return out, n
}

func isPortEdge(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_' || b == ':' || b == '/' || b == '.'
}

// replacePorts puts {{<prefix>portN}} (prefix "role." or "" for a test with one device) for the device's ports in s: slot:port and slot/port anywhere, plain
// port numbers (a standalone switch) only in commands, after "port" or "ports".
func replacePorts(s, role string, ports []string, command bool) (string, int) {
	idx := map[string]int{}
	plain := false
	for i, p := range ports {
		idx[p] = i + 1
		plain = plain || !strings.ContainsAny(p, ":/")
	}
	n := 0
	sub := func(txt string, onlyPlain bool) string {
		var b strings.Builder
		last := 0
		for _, m := range portTok.FindAllStringIndex(txt, -1) {
			tok := txt[m[0]:m[1]]
			k, ok := idx[tok]
			if !ok || onlyPlain != !strings.ContainsAny(tok, ":/") ||
				m[0] > 0 && isPortEdge(txt[m[0]-1]) || m[1] < len(txt) && isPortEdge(txt[m[1]]) {
				continue
			}
			b.WriteString(txt[last:m[0]])
			b.WriteString("{{" + role + "port" + strconv.Itoa(k) + "}}")
			last, n = m[1], n+1
		}
		b.WriteString(txt[last:])
		return b.String()
	}
	if !plain {
		return sub(s, false), n
	}
	if !command {
		return s, 0
	}
	return portList.ReplaceAllStringFunc(s, func(m string) string {
		p := portList.FindStringSubmatch(m)
		return p[1] + p[2] + sub(p[3], true)
	}), n
}

// UsePortVars replaces a test's port numbers by its devices' port variables: for each step on a device role
// ({{role}}), the ports of that device (ports gives them for its saved session: port1…port8, e.g. 1:2…1:9)
// become {{role.port1}}… in the command and the checks - {{port1}}… when the test has one device. It
// returns how many it replaced.
func UsePortVars(c Case, ports func(session string) []string) (Case, int) {
	cache := map[string][]string{}
	total := 0
	roleOf := func(st *Step) string {
		if st.Run == nil {
			return ""
		}
		m := varRef.FindStringSubmatch(strings.TrimSpace(st.Run.Session))
		if m == nil || m[0] != strings.TrimSpace(st.Run.Session) {
			return ""
		}
		return m[1]
	}
	roles := map[string]bool{}
	var count func(l []Step)
	count = func(l []Step) {
		for i := range l {
			if l[i].Loop != nil {
				count(l[i].Loop.Steps)
			}
			if r := roleOf(&l[i]); r != "" && c.Vars[r] != "" && len(ports(c.Vars[r])) > 0 {
				roles[r] = true
			}
		}
	}
	count(c.Setup)
	count(c.Steps)
	count(c.Cleanup)
	var walk func(l []Step)
	walk = func(l []Step) {
		for i := range l {
			st := &l[i]
			if st.Loop != nil {
				walk(st.Loop.Steps)
			}
			role := roleOf(st)
			if role == "" {
				continue
			}
			prefix := role + "."
			if len(roles) == 1 { // one device: {{port1}}
				prefix = ""
			}
			ps, ok := cache[role]
			if !ok {
				if sess := c.Vars[role]; sess != "" {
					ps = ports(sess)
				}
				cache[role] = ps
			}
			if len(ps) == 0 {
				continue
			}
			var n int
			st.Run.Command, n = fixBraces(st.Run.Command)
			total += n
			st.Run.Command, n = replacePorts(st.Run.Command, prefix, ps, true)
			total += n
			for j := range st.Expect {
				e := &st.Expect[j]
				for _, p := range []*string{&e.Contains, &e.NotContains, e.Equals, e.NotEquals} {
					if p != nil {
						*p, n = fixBraces(*p)
						total += n
						*p, n = replacePorts(*p, prefix, ps, false)
						total += n
					}
				}
			}
		}
	}
	walk(c.Setup)
	walk(c.Steps)
	walk(c.Cleanup)
	return c, total
}
