package sim

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// The actions a pack's commands do ("do" in os.json). Words in {braces} are
// the command's values. An action that fails prints the OS's error template
// of that code (.Err.Code, .Err.Value, .Args) and stops the command.
//
//	mode <m>                 enter a mode            exit        the mode's exitTo (quit = log out)
//	quit                     log out                 quitsave "<question>"  ask to save when unsaved, then log out
//	confirm "<question>"     the rest only after y   save        keep the configuration     reboot  back to the saved one, log out
//	paging on|off            set <key> <value>       unset <key> keys: sys.name sys.location sys.contact kv.<any>
//	vr <name> <kv key>       a known VR / VRF into kv (error vr)
//	vlan.create <name> [tag] vlan.createid <id> [name]   vlan.delete <ref>   vlan.tag <ref> <tag>
//	vlan.add <ref> <ports> tagged|untagged|port          vlan.remove <ref> <ports>
//	vlan.ip <ref> <ip> [mask]                            vlan.noip <ref>     (ref: a name, or #id)
//	ports.enable <ports|ctx> on|off   ports.name <ports|ctx> <text>   ports.tagging ctx on|off
//	if.ports <ports>  if.vlan <id>    route.add <net> <gateway> [mask]   route.del <net> [mask]
//	ping <host> [count] [vr]          trace <host> [vr]     log <text>     error <code> [value]
//	vlan.check <ref>  ports.select <ports> (.Sel)  if.clear  ctx.vlan / ctx.ports (error ctx when not in one)
//	check kv.<key> empty|set <code> [value]   (error <code> unless the value is empty / set)
//	values: {ctxvlan} = the VLAN interface being configured

func (s *Shell) runCommand(def *cmdDef, a args) {
	steps := def.cmd.Do
	s.ping, s.err, s.args, s.sel = nil, ErrView{}, a, nil
	a["ctxvlan"] = strconv.Itoa(s.ifVLAN)
	if !s.doSteps(def, steps, a) {
		return
	}
	s.output(def, a)
}

func (s *Shell) output(def *cmdDef, a args) {
	if def.cmd.Out == "" {
		return
	}
	name := def.cmd.Out
	if !strings.HasSuffix(name, ".tmpl") {
		name = "out:" + strconv.Itoa(def.idx)
	}
	s.p("%s", s.render(name, a))
}

// doSteps runs actions; false = stopped (an error, or a question waits).
func (s *Shell) doSteps(def *cmdDef, steps []string, a args) bool {
	for i, step := range steps {
		w := splitWords(step)
		for j := range w {
			w[j] = subst(w[j], a)
		}
		if len(w) == 0 {
			continue
		}
		arg := func(k int) string {
			if k < len(w) {
				return w[k]
			}
			return ""
		}
		if w[0] == "confirm" {
			rest := steps[i+1:]
			s.askText = arg(1) + " (y/N) "
			s.ask = func(ans string) {
				if !strings.HasPrefix(strings.ToLower(ans), "y") {
					s.p("%s", s.render("error:cancelled", a))
					return
				}
				s.d.lab.mu.Lock()
				defer s.d.lab.mu.Unlock()
				if s.doSteps(def, rest, a) {
					s.output(def, a)
				}
			}
			return false
		}
		if w[0] == "quitsave" {
			if !s.d.unsaved {
				s.quit = true
				return true
			}
			s.askText = arg(1) + " (y/N) "
			s.ask = func(ans string) {
				if strings.HasPrefix(strings.ToLower(ans), "y") {
					s.d.lab.mu.Lock()
					s.d.save()
					s.d.lab.mu.Unlock()
					s.p("%s", s.render("error:saved", a))
				}
				s.quit = true
			}
			return false
		}
		if code, val := s.act(w[0], arg); code != "" {
			s.err = ErrView{Code: code, Value: val}
			out := s.render("error:"+code, a)
			if out == "" {
				out = "Error: " + code + " " + val + "\n"
			}
			s.p("%s", out)
			return false
		}
	}
	return true
}

// act does one action; it returns an error code and value ("" = done).
func (s *Shell) act(verb string, arg func(int) string) (string, string) {
	d := s.d
	switch verb {
	case "mode":
		s.mode = arg(1)
	case "exit":
		to := d.osd.ExitTo[s.mode]
		if to == "" || to == "quit" {
			s.quit = true
		} else {
			if s.mode != to {
				s.ifPorts, s.ifVLAN = nil, 0
			}
			s.mode = to
		}
	case "quit":
		s.quit = true
	case "save":
		d.save()
	case "reboot":
		d.reboot()
		s.quit = true
	case "paging":
		s.paging = arg(1) != "off"
	case "set", "unset":
		v := arg(2)
		if verb == "unset" {
			v = ""
		}
		v = strings.Trim(v, `"`)
		switch k := arg(1); {
		case k == "sys.name":
			if v == "" {
				return "value", k
			}
			d.cur.Hostname = v
		case k == "sys.location":
			d.cur.Location = v
		case k == "sys.contact":
			d.cur.Contact = v
		case strings.HasPrefix(k, "kv."):
			if d.cur.KV == nil {
				d.cur.KV = map[string]string{}
			}
			if v == "" {
				delete(d.cur.KV, k[3:])
			} else {
				d.cur.KV[k[3:]] = v
			}
		default:
			return "key", k
		}
		d.changed()
	case "vr":
		name, ok := d.osd.VRs[strings.ToLower(arg(1))]
		if !ok {
			return "vr", arg(1)
		}
		if k := arg(2); strings.HasPrefix(k, "kv.") {
			if d.cur.KV == nil {
				d.cur.KV = map[string]string{}
			}
			d.cur.KV[k[3:]] = name
			d.changed()
		}
	case "vlan.create", "vlan.createid":
		name, tag := strings.Trim(arg(1), `"`), 0
		if verb == "vlan.createid" {
			t, err := strconv.Atoi(arg(1))
			if err != nil || t < 1 || t > 4094 {
				return "vlan.tag", arg(1)
			}
			tag, name = t, strings.Trim(arg(2), `"`)
			if name == "" {
				name = "VLAN-" + arg(1)
			}
		} else if arg(2) != "" {
			t, err := strconv.Atoi(arg(2))
			if err != nil || t < 1 || t > 4094 {
				return "vlan.tag", arg(2)
			}
			tag = t
		}
		if d.vlanByName(name) != nil {
			return "vlan.exists", name
		}
		if tag != 0 && d.vlanByTag(tag) != nil {
			return "vlan.tagused", strconv.Itoa(tag)
		}
		if tag == 0 {
			for tag = 4094; d.vlanByTag(tag) != nil; tag-- {
			}
		}
		d.cur.VLANs = append(d.cur.VLANs, &VLAN{Name: name, Tag: tag, Ports: map[string]bool{}})
		d.changed()
	case "vlan.delete":
		v := d.vlanRef(arg(1))
		if v == nil {
			return "vlan.missing", arg(1)
		}
		if v.Tag == 1 {
			return "vlan.default", arg(1)
		}
		for i, x := range d.cur.VLANs {
			if x == v {
				d.cur.VLANs = append(d.cur.VLANs[:i], d.cur.VLANs[i+1:]...)
				break
			}
		}
		d.changed()
	case "vlan.tag":
		v := d.vlanRef(arg(1))
		t, err := strconv.Atoi(arg(2))
		switch {
		case v == nil:
			return "vlan.missing", arg(1)
		case err != nil || t < 1 || t > 4094:
			return "vlan.tag", arg(2)
		case d.vlanByTag(t) != nil && d.vlanByTag(t) != v:
			return "vlan.tagused", arg(2)
		}
		v.Tag = t
		d.changed()
	case "vlan.add", "vlan.remove":
		v := d.vlanRef(arg(1))
		if v == nil {
			return "vlan.missing", arg(1)
		}
		ps, bad := d.parsePorts(arg(2))
		if bad != "" {
			return "port", bad
		}
		for _, p := range ps {
			if verb == "vlan.remove" {
				delete(v.Ports, p)
				continue
			}
			tagged := arg(3) == "tagged" || (arg(3) == "port" && d.port(p).Tagged)
			if !tagged { // untagged in one VLAN only; the default VLAN gives it up
				for _, w := range d.cur.VLANs {
					if t, in := w.Ports[p]; in && !t && w != v {
						if w.Tag == 1 || arg(3) == "port" {
							delete(w.Ports, p)
							continue
						}
						s.args["vlan"] = w.Name
						return "vlan.untagged", p
					}
				}
			}
			v.Ports[p] = tagged
		}
		d.changed()
	case "vlan.ip", "vlan.noip":
		v := d.vlanRef(arg(1))
		if v == nil {
			return "vlan.missing", arg(1)
		}
		if verb == "vlan.noip" {
			v.IP = ""
			d.changed()
			break
		}
		ip := arg(2)
		if arg(3) != "" {
			ip += "/" + maskLen(arg(3))
		}
		p, err := netip.ParsePrefix(ip)
		if err != nil {
			return "ip", arg(2)
		}
		v.IP = p.String()
		d.changed()
	case "ports.enable", "ports.name", "ports.tagging":
		ps := s.ifPorts
		if arg(1) != "ctx" {
			var bad string
			if ps, bad = d.parsePorts(arg(1)); bad != "" {
				return "port", bad
			}
		}
		if len(ps) == 0 {
			return "ctx", ""
		}
		for _, p := range ps {
			pt := d.port(p)
			switch verb {
			case "ports.enable":
				pt.Enabled = arg(2) != "off"
			case "ports.name":
				pt.Desc = strings.Trim(arg(2), `"`)
			case "ports.tagging":
				pt.Tagged = arg(2) != "off"
				for _, v := range d.cur.VLANs {
					if _, in := v.Ports[p]; in {
						v.Ports[p] = pt.Tagged
					}
				}
			}
		}
		d.changed()
	case "if.ports":
		ps, bad := d.parsePorts(arg(1))
		if bad != "" {
			return "port", bad
		}
		s.ifPorts, s.ifVLAN = ps, 0
	case "if.vlan":
		id, _ := strconv.Atoi(arg(1))
		if d.vlanByTag(id) == nil {
			return "vlan.missing", arg(1)
		}
		s.ifPorts, s.ifVLAN = nil, id
	case "route.add", "route.del":
		net := arg(1)
		if m := arg(3); verb == "route.add" && m != "" {
			net += "/" + maskLen(m)
		} else if m := arg(2); verb == "route.del" && m != "" {
			net += "/" + maskLen(m)
		}
		p, err := netip.ParsePrefix(net)
		if err != nil {
			return "ip", arg(1)
		}
		var keep []Route
		for _, r := range d.cur.Routes {
			if r.Net != p.Masked().String() {
				keep = append(keep, r)
			}
		}
		if verb == "route.add" {
			gw, err := netip.ParseAddr(arg(2))
			if err != nil {
				return "ip", arg(2)
			}
			keep = append(keep, Route{Net: p.Masked().String(), Gateway: gw.String()})
		}
		d.cur.Routes = keep
		d.changed()
	case "ping", "trace":
		dst, err := netip.ParseAddr(arg(1))
		if err != nil {
			return "host", arg(1)
		}
		n, vr := 4, arg(3)
		if verb == "trace" {
			vr = arg(2)
		} else if c, err := strconv.Atoi(arg(2)); err == nil && c > 0 && c <= 100 {
			n = c
		}
		shown := ""
		if vr != "" {
			var ok bool
			if shown, ok = d.osd.VRs[strings.ToLower(vr)]; !ok {
				return "vr", vr
			}
		}
		mgmt := false
		for _, m := range d.osd.MgmtVRs {
			mgmt = mgmt || strings.EqualFold(m, vr)
		}
		r := d.ping(dst, mgmt)
		s.ping = &PingView{Host: dst.String(), Count: n, Route: r.Route, OK: r.OK, MS: float64(r.RTT.Microseconds()) / 1000, Mgmt: mgmt, VR: shown}
	case "log":
		d.logf("%s", arg(1))
	case "if.clear":
		s.ifPorts, s.ifVLAN = nil, 0
	case "ctx.vlan":
		if s.ifVLAN == 0 {
			return "ctx", ""
		}
	case "ctx.ports":
		if len(s.ifPorts) == 0 {
			return "ctx", ""
		}
	case "vlan.check":
		if d.vlanRef(arg(1)) == nil {
			return "vlan.missing", arg(1)
		}
	case "ports.select":
		ps, bad := d.parsePorts(arg(1))
		if bad != "" {
			return "port", bad
		}
		s.sel = ps
	case "error":
		return arg(1), arg(2)
	case "check": // check kv.<key> empty|set <code> [value]: the error when the value is not so
		k := arg(1)
		if !strings.HasPrefix(k, "kv.") {
			return "key", k
		}
		if (arg(2) == "empty") != (d.cur.KV[k[3:]] == "") {
			return arg(3), arg(4)
		}
	default:
		return "action", verb
	}
	return "", ""
}

// vlanRef: a VLAN by name, or by tag as #id.
func (d *Device) vlanRef(r string) *VLAN {
	r = strings.Trim(r, `"`)
	if strings.HasPrefix(r, "#") {
		n, _ := strconv.Atoi(r[1:])
		return d.vlanByTag(n)
	}
	return d.vlanByName(r)
}

// splitWords splits an action, keeping "quoted text" together.
func splitWords(s string) []string {
	var out []string
	var b strings.Builder
	q, any := false, false
	for _, c := range s {
		switch {
		case c == '"':
			q = !q
			any = true
		case c == ' ' && !q:
			if any {
				out = append(out, b.String())
				b.Reset()
				any = false
			}
		default:
			b.WriteRune(c)
			any = true
		}
	}
	if any {
		out = append(out, b.String())
	}
	return out
}

// subst puts the command's values in a word ({name}; a missing one is empty).
func subst(w string, a args) string {
	if !strings.Contains(w, "{") {
		return w
	}
	return valueRef.ReplaceAllStringFunc(w, func(m string) string { return a[m[1:len(m)-1]] })
}

var valueRef = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)
