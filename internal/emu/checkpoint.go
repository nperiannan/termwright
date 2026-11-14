package emu

import (
	_ "embed"
	"fmt"
	"strings"
)

// Checkpoint is a running switch's whole state (memory, CPU, disk) kept in its disk while it runs (a QEMU
// snapshot): restoring it brings the switch back to that moment - its configuration, tables and sessions.
type Checkpoint struct {
	Tag    string `json:"tag"`              // the snapshot's name in its disk (tw-ck-…)
	At     string `json:"at"`               // when (RFC 3339)
	Note   string `json:"note,omitempty"`   // why it was taken
	Change string `json:"change,omitempty"` // the change of the lab's history it was taken before
}

// CheckpointKeep: how many checkpoints a switch keeps (the oldest goes).
const CheckpointKeep = 3

// monitorRun types commands into a running VM's QEMU monitor on the host and waits until a line matches
// ok (prints "ok"), an error line comes ("err …"), or the time is up. (Never "quit": that ends QEMU.)
func (l *Lab) monitorRun(v VM, cmds []string, ok string, secs int) string {
	dir := Q(l.VMDir())
	var b strings.Builder
	send := strings.ReplaceAll(strings.Join(cmds, `\r\n`)+`\r\n`, "'", "")
	fmt.Fprintf(&b, "if %s; then\n", qemuUp(dir+"/"+v.ID+".pid"))
	fmt.Fprintf(&b, " r=$(timeout %d bash -c 'exec 3<>/dev/tcp/127.0.0.1/%d || exit 1; printf \"%s\" >&3; while IFS= read -r -t %d l <&3; do case \"$l\" in %s) echo ok; break;; *rror*|*annot*|*ailed*) echo \"err $l\"; break;; esac; done; exec 3>&-' 2>&1 | tr -d '\\r' | tail -1)\n", secs, l.MonitorPort(v), send, secs, ok)
	b.WriteString(" echo \"${r:-err no answer from its monitor}\"\nelse echo 'err it is not running'; fi\n")
	return b.String()
}

// CheckpointScript takes a checkpoint of a running VM (it pauses while its memory is written, then runs on)
// and deletes the checkpoints in drop; it prints "ok" or "err why".
func (l *Lab) CheckpointScript(v VM, tag string, drop []string) string {
	cmds := []string{"savevm " + tag}
	for _, d := range drop {
		cmds = append(cmds, "delvm "+d)
	}
	cmds = append(cmds, "info snapshots")
	return l.monitorRun(v, cmds, "*"+tag+"*[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]*", 300)
}

// RestoreScript brings a running VM back to a checkpoint; it prints "ok" or "err why".
func (l *Lab) RestoreScript(v VM, tag string) string {
	return l.monitorRun(v, []string{"loadvm " + tag, "cont", "info status"}, "*VM?status:?running*", 300)
}

// DropCheckpointScript deletes checkpoints: by the monitor while the VM runs, else from its disk.
func (l *Lab) DropCheckpointScript(v VM, tags []string, running bool) string {
	if running {
		cmds := []string{}
		for _, t := range tags {
			cmds = append(cmds, "delvm "+t)
		}
		return l.monitorRun(v, append(cmds, "info snapshots"), "*ID*TAG*|*o?snapshot*", 60)
	}
	var b strings.Builder
	for _, t := range tags {
		fmt.Fprintf(&b, "for d in %s; do [ -f \"$d\" ] && qemu-img snapshot -d %s \"$d\" 2>/dev/null; done\n", l.disks(v.ID), Q(t))
	}
	b.WriteString("echo ok\n")
	return b.String()
}

//go:embed patch.py
var PatchPy string

// PatchScript writes a host's patch.json (pairs of switch ports joined while they run: [a listen, a send,
// b listen, b send]) and starts patch.py there when it is not running (it ends when there are none).
func (l *Lab) PatchScript(pairs [][4]int) string {
	dir := Q(l.VMDir())
	var b strings.Builder
	fmt.Fprintf(&b, "mkdir -p %s && cd %s || exit 1\n", dir, dir)
	fmt.Fprintf(&b, "cat > tw-patch.py.new <<'TWPYEOF'\n%sTWPYEOF\n", PatchPy)
	b.WriteString("cmp -s tw-patch.py.new tw-patch.py || { mv -f tw-patch.py.new tw-patch.py; p=$(cat patch.pid 2>/dev/null); [ -n \"$p\" ] && kill $p 2>/dev/null; }; rm -f tw-patch.py.new\n")
	js := "["
	for i, p := range pairs {
		if i > 0 {
			js += ","
		}
		js += fmt.Sprintf("[%d,%d,%d,%d]", p[0], p[1], p[2], p[3])
	}
	fmt.Fprintf(&b, "echo '%s]' > patch.json.new && mv -f patch.json.new patch.json\n", js)
	if len(pairs) > 0 {
		b.WriteString("p=$(cat patch.pid 2>/dev/null); if [ -z \"$p\" ] || ! grep -q tw-patch /proc/$p/cmdline 2>/dev/null; then command -v python3 >/dev/null || { echo 'patch: python3 is missing'; exit 1; }; nohup python3 tw-patch.py \"$PWD\" >patch.log 2>&1 & echo $! > patch.pid; fi\n")
	}
	b.WriteString("echo patch-ok\n")
	return b.String()
}

// Note is a note or an area on the lab's canvas: text where it is put, or a labelled rectangle behind the devices.
type Note struct {
	ID    string  `json:"id"`
	Kind  string  `json:"kind"` // note, area
	Text  string  `json:"text"` // a note's text, an area's label
	X     float64 `json:"x"`    // its top left corner on the canvas
	Y     float64 `json:"y"`
	W     float64 `json:"w"` // its size
	H     float64 `json:"h"`
	Color int     `json:"color,omitempty"` // 0-7: the palette's colours (s1-s8)
}

// Hub is a hub or an L2 switch of the lab: no VM, its legs are switch ports without a cable, joined by hub.py
// on the host they run on (all its legs on one host). A hub repeats every frame to its other legs; an L2
// switch learns MAC addresses.
type Hub struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Kind string   `json:"kind"`           // hub, switch
	Legs []HubLeg `json:"legs,omitempty"` // the switch ports on it, in the order of its ports
	X    float64  `json:"x,omitempty"`    // on the canvas
	Y    float64  `json:"y,omitempty"`
}

// HubLeg is a switch port on a hub.
type HubLeg struct {
	VM   string `json:"vm"`
	Port int    `json:"port"`
}

// HubMaxLegs: how many ports a hub or an L2 switch has.
const HubMaxLegs = 16

// HubOn finds the hub a switch port is on.
func (l *Lab) HubOn(vm string, port int) *Hub {
	for i := range l.Hubs {
		for _, g := range l.Hubs[i].Legs {
			if g.VM == vm && g.Port == port {
				return &l.Hubs[i]
			}
		}
	}
	return nil
}

// HubByID finds a hub by id or name.
func (l *Lab) HubByID(id string) *Hub {
	for i := range l.Hubs {
		if l.Hubs[i].ID == id || strings.EqualFold(l.Hubs[i].Name, id) {
			return &l.Hubs[i]
		}
	}
	return nil
}

//go:embed hub.py
var HubPy string

// HubScript writes a host's hubs.json and starts hub.py there when it is not running (it ends when there are none).
func (l *Lab) HubScript(cfg []byte, any bool) string {
	dir := Q(l.VMDir())
	var b strings.Builder
	fmt.Fprintf(&b, "mkdir -p %s && cd %s || exit 1\n", dir, dir)
	fmt.Fprintf(&b, "cat > tw-hub.py.new <<'TWPYEOF'\n%sTWPYEOF\n", HubPy)
	b.WriteString("cmp -s tw-hub.py.new tw-hub.py || { mv -f tw-hub.py.new tw-hub.py; p=$(cat hub.pid 2>/dev/null); [ -n \"$p\" ] && kill $p 2>/dev/null; }; rm -f tw-hub.py.new\n")
	fmt.Fprintf(&b, "cat > hubs.json.new <<'TWJSEOF'\n%s\nTWJSEOF\nmv -f hubs.json.new hubs.json\n", cfg)
	if any {
		b.WriteString("p=$(cat hub.pid 2>/dev/null); if [ -z \"$p\" ] || ! grep -q tw-hub /proc/$p/cmdline 2>/dev/null; then command -v python3 >/dev/null || { echo 'hub: python3 is missing'; exit 1; }; nohup python3 tw-hub.py \"$PWD\" >hub.log 2>&1 & echo $! > hub.pid; fi\n")
	}
	b.WriteString("echo hub-ok\n")
	return b.String()
}

// VMByName finds a VM by its name (any case).
func (l *Lab) VMByName(name string) *VM {
	for i := range l.VMs {
		if strings.EqualFold(l.VMs[i].Name, name) {
			return &l.VMs[i]
		}
	}
	return nil
}

//go:embed radius.py
var RadiusPy string

// Radius is the lab's built-in RADIUS server: it runs on every host of the lab
// and answers the switches there (802.1X and MAC logins).
type Radius struct {
	On      bool         `json:"on"`                // running
	Port    int          `json:"port,omitempty"`    // authentication port (default 1812; accounting on the next)
	Secret  string       `json:"secret,omitempty"`  // shared with the switches
	Users   []RadiusUser `json:"users,omitempty"`   // who may log in (the first match wins)
	MAB     []string     `json:"mab,omitempty"`     // MAC addresses let in by MAC authentication (patterns: 02:63:*, * = any)
	MABVLAN string       `json:"mabVlan,omitempty"` // the VLAN those get
}

// RadiusUser is a user name (or a pattern, user*) with its password and VLAN.
type RadiusUser struct {
	User     string `json:"user"`
	Password string `json:"password"`
	VLAN     string `json:"vlan,omitempty"` // the VLAN (name or number) the switch puts it in
}

// RadiusPort is the server's authentication port.
func (r Radius) RadiusPort() int {
	if r.Port > 0 {
		return r.Port
	}
	return 1812
}

// RadiusAddr is where this host's switches reach the server: the lab bridge's
// address, or the host's own on its network ("" = the host's address on the
// switches' network, not known to Termwright).
func (l *Lab) RadiusAddr() string {
	switch l.Mgmt {
	case "bridge":
		return strings.Split(l.gatewayPrefix(), "/")[0]
	case "cloud":
		return strings.Split(l.HostIP, "/")[0]
	}
	return ""
}

// RadiusScript writes a host's radius.json and starts radius.py there (on: false stops it).
func (l *Lab) RadiusScript(cfg []byte, on bool) string {
	dir := Q(l.VMDir())
	var b strings.Builder
	fmt.Fprintf(&b, "mkdir -p %s && cd %s || exit 1\n", dir, dir)
	if !on {
		b.WriteString("rm -f radius.json; echo radius-ok\n")
		return b.String()
	}
	fmt.Fprintf(&b, "cat > tw-radius.py.new <<'TWPYEOF'\n%sTWPYEOF\n", RadiusPy)
	b.WriteString("cmp -s tw-radius.py.new tw-radius.py || { mv -f tw-radius.py.new tw-radius.py; p=$(cat radius.pid 2>/dev/null); [ -n \"$p\" ] && kill $p 2>/dev/null; }; rm -f tw-radius.py.new\n")
	fmt.Fprintf(&b, "umask 077; cat > radius.json.new <<'TWJSEOF'\n%s\nTWJSEOF\nmv -f radius.json.new radius.json\n", cfg)
	b.WriteString("p=$(cat radius.pid 2>/dev/null); if [ -z \"$p\" ] || ! grep -q tw-radius /proc/$p/cmdline 2>/dev/null; then command -v python3 >/dev/null || { echo 'radius: python3 is missing'; exit 1; }; nohup python3 tw-radius.py \"$PWD\" >radius.log 2>&1 & echo $! > radius.pid; fi\n")
	b.WriteString("echo radius-ok\n")
	return b.String()
}
