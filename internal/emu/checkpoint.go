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
