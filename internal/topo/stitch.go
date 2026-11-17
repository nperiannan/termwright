package topo

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

type claim struct {
	dev    int // index in devs
	port   string
	np     string // NormPort(port)
	n      Neighbor
	remote int // index of the session device it is, -1 = not one
	rport  string
	nrp    string
	seen   []Proto
}

func rank(c *claim) int {
	if c.n.Age >= 0 {
		return c.n.Age
	}
	return 1 << 30
}

type finder struct {
	mac, name, ip map[string]int
}

func newFinder(devs []Device) *finder {
	f := &finder{map[string]int{}, map[string]int{}, map[string]int{}}
	for i, d := range devs {
		if m := NormMAC(d.Chassis); m != "" {
			f.mac[m] = i
		}
		if n := NormName(d.Name); n != "" {
			f.name[n] = i
		}
		if n := NormName(d.SysName); n != "" {
			f.name[n] = i
		}
		if d.IP != "" {
			f.ip[d.IP] = i
		}
	}
	return f
}

func (f *finder) find(n Neighbor) int {
	if i, ok := f.mac[n.Chassis]; ok && n.Chassis != "" {
		return i
	}
	if i, ok := f.ip[n.IP]; ok && n.IP != "" {
		return i
	}
	if i, ok := f.name[NormName(n.Name)]; ok && n.Name != "" {
		return i
	}
	return -1
}

func portOf(d *Device, np string) *Port {
	for i := range d.Ports {
		if NormPort(d.Ports[i].Name) == np {
			return &d.Ports[i]
		}
	}
	return nil
}

func lagOf(d *Device, np string) *LAG {
	for i := range d.LAGs {
		for _, m := range d.LAGs[i].Members {
			if NormPort(m) == np {
				g := d.LAGs[i]
				return &g
			}
		}
	}
	return nil
}

// nodeID is stable when a ghost later gets a session: the name when there is one.
func nodeID(name, chassis, ip string) string {
	switch {
	case NormName(name) != "":
		return "n:" + NormName(name)
	case chassis != "":
		return "m:" + chassis
	case ip != "":
		return "i:" + ip
	}
	return ""
}

func keyOf(a, ap, b, bp string) string {
	x, y := a+"|"+NormPort(ap), b+"|"+NormPort(bp)
	if x > y {
		x, y = y, x
	}
	return x + "||" + y
}

// Build joins what the devices read into one map. prev is the map before (nil the first time): a link that
// is gone but whose port is now down and not used by any new link stays as a "down" link; a link that moved
// (a port of it now has another neighbour) is dropped, not drawn down.
func Build(devs []Device, prev *Map, now time.Time) Map {
	ids := make([]string, len(devs))
	used := map[string]int{}
	for i, d := range devs {
		id := nodeID(firstOf(d.SysName, d.Name), NormMAC(d.Chassis), d.IP)
		if id == "" {
			id = "s:" + d.Session
		}
		if used[id]++; used[id] > 1 {
			id += "#" + d.Session
		}
		ids[i] = id
	}
	f := newFinder(devs)
	// learn a session device's MAC from the neighbours that name it, so a neighbour that gives only its MAC is found too
	for pass := 0; pass < 2; pass++ {
		for _, d := range devs {
			if d.Err != "" {
				continue
			}
			for _, n := range d.Neighbors {
				if i := f.find(n); i >= 0 && n.Chassis != "" {
					if _, ok := f.mac[n.Chassis]; !ok {
						f.mac[n.Chassis] = i
					}
				}
			}
		}
	}

	// the freshest neighbour on every port of every device read
	claims := map[[2]string]*claim{} // {dev index, np}
	var order []*claim
	for i := range devs {
		d := &devs[i]
		if d.Err != "" {
			continue
		}
		by := map[string][]Neighbor{}
		for _, n := range d.Neighbors {
			np := NormPort(n.LocalPort)
			if p := portOf(d, np); p != nil && !p.Oper {
				continue // the cable is out: what it heard before is old
			}
			by[np] = append(by[np], n)
		}
		nps := make([]string, 0, len(by))
		for np := range by {
			nps = append(nps, np)
		}
		sort.Strings(nps)
		for _, np := range nps {
			ns := by[np]
			sort.SliceStable(ns, func(a, b int) bool {
				ra, rb := ns[a].Age, ns[b].Age
				if ra < 0 {
					ra = 1 << 30
				}
				if rb < 0 {
					rb = 1 << 30
				}
				return ra < rb
			})
			best := ns[0]
			c := &claim{dev: i, port: best.LocalPort, np: np, n: best, remote: f.find(best), rport: best.RemotePort, nrp: NormPort(best.RemotePort)}
			for _, n := range ns {
				same := n.Chassis != "" && n.Chassis == best.Chassis || n.Name != "" && NormName(n.Name) == NormName(best.Name)
				if same {
					c.seen = append(c.seen, n.Proto)
				}
			}
			if c.remote == i {
				continue // a device hearing itself
			}
			claims[[2]string{itoa(i), np}] = c
			order = append(order, c)
		}
	}

	// a claim the other end contradicts is old: the fresher of the two wins; with no age, the port's owner wins
	dead := map[*claim]bool{}
	for _, c := range order {
		if c.remote < 0 || c.nrp == "" {
			continue
		}
		r := &devs[c.remote]
		if r.Err != "" {
			continue
		}
		if p := portOf(r, c.nrp); p != nil && !p.Oper {
			dead[c] = true
			continue
		}
		o := claims[[2]string{itoa(c.remote), c.nrp}]
		if o == nil || o.remote == c.dev && o.nrp == c.np {
			continue
		}
		if o.remote == c.dev && o.nrp == "" {
			continue
		}
		if rank(c) < rank(o) {
			dead[o] = true
		} else {
			dead[c] = true
		}
	}

	m := Map{At: now}
	nodeAt := map[string]int{}
	addNode := func(n Node) {
		if _, ok := nodeAt[n.ID]; !ok {
			nodeAt[n.ID] = len(m.Nodes)
			m.Nodes = append(m.Nodes, n)
		}
	}
	for i, d := range devs {
		notes := append([]string(nil), d.Notes...)
		if d.Err != "" {
			notes = append(notes, d.Err)
		}
		addNode(Node{ID: ids[i], Name: d.Name, OS: d.OS, IP: d.IP, Chassis: NormMAC(d.Chassis), Session: d.Session, Notes: notes})
	}
	linkAt := map[string]int{}
	for _, c := range order {
		if dead[c] {
			continue
		}
		d := &devs[c.dev]
		a := ids[c.dev]
		var b, bport string
		var rd *Device
		if c.remote >= 0 {
			b, bport, rd = ids[c.remote], c.rport, &devs[c.remote]
			if o := claims[[2]string{itoa(c.remote), c.nrp}]; o != nil && !dead[o] && o.remote == c.dev {
				bport = o.port
			}
		} else {
			b = nodeID(c.n.Name, c.n.Chassis, c.n.IP)
			if b == "" {
				continue
			}
			name := c.n.Name
			if name == "" {
				name = firstOf(c.n.IP, c.n.Chassis)
			}
			addNode(Node{ID: b, Name: name, IP: c.n.IP, Chassis: c.n.Chassis, Ghost: true, Caps: c.n.Caps})
			bport = c.rport
		}
		key := keyOf(a, c.port, b, bport)
		if at, ok := linkAt[key]; ok {
			l := &m.Links[at]
			l.Ends = 2
			l.Seen = addProtos(l.Seen, c.seen)
			continue
		}
		l := Link{Key: key, A: a, APort: c.port, B: b, BPort: bport, State: "up", Seen: addProtos(nil, c.seen), Ends: 1}
		if p := portOf(d, c.np); p != nil {
			l.STPA = p.STP
			l.Speed = p.Speed
		}
		l.LagA = lagOf(d, c.np)
		var pb *Port
		if rd != nil {
			if p := portOf(rd, NormPort(bport)); p != nil {
				l.STPB = p.STP
				pb = p
			}
			l.LagB = lagOf(rd, NormPort(bport))
		}
		l.VLANs = vlansOf(portOf(d, c.np), pb)
		if l.STPA == "blocked" || l.STPB == "blocked" {
			l.State = "blocked"
		}
		l.Warn = lagWarn(l.LagA, l.LagB, rd != nil)
		linkAt[key] = len(m.Links)
		m.Links = append(m.Links, l)
	}
	// links of the last map that are gone
	if prev != nil {
		portTaken := map[string]bool{}
		for _, l := range m.Links {
			portTaken[l.A+"|"+NormPort(l.APort)] = true
			portTaken[l.B+"|"+NormPort(l.BPort)] = true
		}
		byID := map[string]int{}
		for i, id := range ids {
			byID[id] = i
		}
		for _, pl := range prev.Links {
			if _, ok := linkAt[pl.Key]; ok {
				continue
			}
			if portTaken[pl.A+"|"+NormPort(pl.APort)] || portTaken[pl.B+"|"+NormPort(pl.BPort)] {
				continue // moved: a port of it has a new neighbour
			}
			keep, down := false, false
			for _, e := range [][2]string{{pl.A, pl.APort}, {pl.B, pl.BPort}} {
				i, ok := byID[e[0]]
				if !ok {
					continue
				}
				if devs[i].Err != "" {
					keep = true // could not be read now: leave the link as it was
					continue
				}
				if p := portOf(&devs[i], NormPort(e[1])); p != nil && !p.Oper {
					keep, down = true, true
				}
			}
			if !keep {
				continue
			}
			for _, id := range []string{pl.A, pl.B} {
				if _, ok := nodeAt[id]; !ok {
					for _, pn := range prev.Nodes {
						if pn.ID == id {
							addNode(pn)
						}
					}
				}
			}
			if down {
				pl.State = "down"
			}
			m.Links = append(m.Links, pl)
		}
	}
	sort.SliceStable(m.Links, func(i, j int) bool { return m.Links[i].Key < m.Links[j].Key })
	// VLANs: which device has which, and their names
	names, nodeV := map[int]string{}, map[string][]int{}
	for i, d := range devs {
		for _, p := range d.Ports {
			for _, v := range p.VLANs {
				nodeV[ids[i]] = appendUniq(nodeV[ids[i]], v)
				if _, ok := names[v]; !ok {
					names[v] = ""
				}
			}
		}
	}
	for _, d := range devs {
		for k, v := range d.VLANNames {
			if n, ok := names[k]; ok && n == "" {
				names[k] = v
			}
		}
	}
	devOf := map[string]*Device{}
	for i := range devs {
		devOf[ids[i]] = &devs[i]
	}
	for i := range m.Nodes {
		n := &m.Nodes[i]
		n.VLANs = nodeV[n.ID]
		if d := devOf[n.ID]; d != nil && d.Err == "" {
			n.Model = d.Model
			n.PortsTotal = len(d.Ports)
			for _, p := range d.Ports {
				if p.Oper {
					n.PortsUp++
				}
			}
			for k, v := range d.VLANNames {
				if mgmtName.MatchString(v) {
					n.MgmtVLAN = &VLANName{ID: k, Name: v}
					for _, p := range d.Ports {
						for _, x := range p.VLANs {
							if x == k {
								n.MgmtPorts = append(n.MgmtPorts, p.Name)
							}
						}
					}
					break
				}
			}
		}
	}
	for k, v := range names {
		m.VLANs = append(m.VLANs, VLANName{ID: k, Name: v})
	}
	sort.Slice(m.VLANs, func(i, j int) bool { return m.VLANs[i].ID < m.VLANs[j].ID })
	return m
}

var mgmtName = regexp.MustCompile(`(?i)^(mgmt|management|oob)`)

func firstOf(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func addProtos(a, b []Proto) []Proto {
	for _, p := range b {
		found := false
		for _, q := range a {
			found = found || q == p
		}
		if !found {
			a = append(a, p)
		}
	}
	return a
}

func lagWarn(a, b *LAG, bKnown bool) string {
	switch {
	case a == nil && b == nil:
		return ""
	case a != nil && b != nil && a.Mode != b.Mode:
		return "static on one side, LACP on the other"
	case (a == nil || b == nil) && bKnown:
		return "the LAG is set on one side only"
	}
	return ""
}

// Diff lists what changed from old to new.
func Diff(old, nw Map, now time.Time) []Change {
	name := func(m Map, id string) string {
		for _, n := range m.Nodes {
			if n.ID == id {
				return n.Name
			}
		}
		return strings.TrimPrefix(id, "n:")
	}
	end := func(m Map, id, port string) string {
		if port == "" {
			return name(m, id)
		}
		return name(m, id) + " " + port
	}
	text := func(m Map, l Link) string { return end(m, l.A, l.APort) + " ↔ " + end(m, l.B, l.BPort) }
	var out []Change
	add := func(kind, t string) { out = append(out, Change{At: now, Kind: kind, Text: t}) }

	oldAt, newAt := map[string]Link{}, map[string]Link{}
	for _, l := range old.Links {
		oldAt[l.Key] = l
	}
	for _, l := range nw.Links {
		newAt[l.Key] = l
	}
	var removed, added []Link
	for _, l := range old.Links {
		if _, ok := newAt[l.Key]; !ok {
			removed = append(removed, l)
		}
	}
	for _, l := range nw.Links {
		if _, ok := oldAt[l.Key]; !ok {
			added = append(added, l)
		}
	}
	share := func(a, b Link) bool {
		ea := []string{a.A + "|" + NormPort(a.APort), a.B + "|" + NormPort(a.BPort)}
		eb := []string{b.A + "|" + NormPort(b.APort), b.B + "|" + NormPort(b.BPort)}
		n := 0
		for _, x := range ea {
			for _, y := range eb {
				if x == y {
					n++
				}
			}
		}
		return n == 1
	}
	usedAdd := map[int]bool{}
	for _, r := range removed {
		moved := false
		for j, a := range added {
			if !usedAdd[j] && share(r, a) {
				usedAdd[j], moved = true, true
				add("moved", "moved: "+text(old, r)+" → "+text(nw, a))
				break
			}
		}
		if !moved {
			add("link-down", text(old, r)+" is gone")
		}
	}
	for j, a := range added {
		if !usedAdd[j] {
			add("link-up", text(nw, a)+" is up")
		}
	}
	for _, l := range nw.Links {
		o, ok := oldAt[l.Key]
		if !ok {
			continue
		}
		switch {
		case o.State != "down" && l.State == "down":
			add("link-down", text(nw, l)+" is down")
		case o.State == "down" && l.State != "down":
			add("link-up", text(nw, l)+" is up")
		case o.State != "blocked" && l.State == "blocked":
			add("blocked", text(nw, l)+" is blocked by spanning tree")
		case o.State == "blocked" && l.State == "up":
			add("forwarding", text(nw, l)+" is forwarding again")
		}
		if lagSig(o) != lagSig(l) {
			add("lag", text(nw, l)+": LAG changed ("+lagSig(o)+" → "+lagSig(l)+")")
		}
	}
	oldN := map[string]bool{}
	for _, n := range old.Nodes {
		oldN[n.ID] = true
	}
	newN := map[string]bool{}
	for _, n := range nw.Nodes {
		newN[n.ID] = true
		if !oldN[n.ID] {
			t := "new device: " + n.Name
			if n.Ghost {
				t += " (seen by a neighbour, no session)"
			}
			add("node-new", t)
		}
	}
	for _, n := range old.Nodes {
		if !newN[n.ID] {
			add("node-lost", "device gone: "+n.Name)
		}
	}
	return out
}

func lagSig(l Link) string {
	s := func(g *LAG) string {
		if g == nil {
			return "none"
		}
		return g.Mode + " " + strings.Join(g.Members, ",")
	}
	return s(l.LagA) + " | " + s(l.LagB)
}

// Compare lists the links of the planned map that the discovered one lacks, and the discovered ones the
// plan does not have (by the two devices and their ports).
func Compare(planned, found Map) (missing, unexpected []Link) {
	have := map[string]bool{}
	for _, l := range found.Links {
		have[l.Key] = true
	}
	want := map[string]bool{}
	for _, l := range planned.Links {
		want[l.Key] = true
		if !have[l.Key] {
			missing = append(missing, l)
		}
	}
	for _, l := range found.Links {
		if !want[l.Key] {
			unexpected = append(unexpected, l)
		}
	}
	return
}
