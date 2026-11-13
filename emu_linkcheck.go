package main

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
)

// The link check: what QEMU's e1000 network cards of a running switch do for
// a while, from QEMU's own trace events - whether QEMU takes links down
// (auto-negotiation the switch restarted), the switch turns its receivers off
// (resets), or frames are dropped (link down, filters, overruns). Tells a
// problem of the emulation from one inside the switch's software.

var emuLinkEvents = map[string]string{
	"e1000x_link_negotiation_start":  "the switch restarted auto-negotiation: QEMU takes the link down for it (about 0.5 s)",
	"e1000x_link_negotiation_done":   "auto-negotiation done: the link is up again",
	"e1000x_rx_link_down":            "frames dropped because the link was down",
	"e1000x_rx_disabled":             "frames dropped because the switch had its receiver off (a reset of the card)",
	"e1000x_rx_can_recv_disabled":    "the switch had its receiver off",
	"e1000x_rx_flt_inexact_mismatch": "multicast frames the card's filter dropped (the switch did not ask for them)",
	"e1000x_rx_flt_ucast_mismatch":   "frames for another MAC the card dropped (normal)",
	"e1000x_rx_flt_vlan_mismatch":    "frames the card's VLAN filter dropped",
	"e1000x_rx_oversized":            "frames too big for the card",
	"e1000_receiver_overrun":         "frames dropped because the switch did not take them in time (overrun)",
}

// EmuLinkCheck watches a running switch's network cards in QEMU for some seconds and says what happened.
func (a *App) EmuLinkCheck(id string, seconds int) (string, error) {
	defer emuAs("user")()
	return a.emuLinkCheck(id, seconds)
}

func (a *App) emuLinkCheck(id string, seconds int) (string, error) {
	seconds = min(120, max(5, seconds))
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return "", errors.New("no such switch")
	}
	host, name := emuHostOf(*v), v.Name
	lh := a.emuFor(host)
	mon, dir := lh.MonitorPort(*v), lh.VMDir()
	nic := orStr(emuImage(v.Image).NIC, "e1000")
	emuLab.mu.Unlock()
	if !strings.HasPrefix(nic, "e1000") {
		return "", fmt.Errorf("the link check reads e1000 and e1000e cards' events; %s uses %s (it has no auto-negotiation that takes links down)", name, nic)
	}
	home, err := a.emuHome(host)
	if err != nil {
		return "", err
	}
	file := home + "/" + dir + "/" + id + "-linkcheck.log"
	h, err := a.emuMonitor(host, mon)
	if err != nil {
		return "", errors.New(name + " is not running")
	}
	_, _ = a.emuRun(host, ": > "+emu.Q(file), 10*time.Second)
	for _, c := range []string{"logfile " + file, "trace-event e1000x_* on", "trace-event e1000_receiver_overrun on", "trace-event e1000e_core_ctrl_*_reset on"} {
		if out, err := h.do(c, 10*time.Second); err != nil || strings.Contains(strings.ToLower(out), "error") || strings.Contains(out, "not found") {
			h.close()
			return "", fmt.Errorf("QEMU did not start the check (%s): %v %s", c, err, strings.TrimSpace(out))
		}
	}
	emuEvent(id, "info", fmt.Sprintf("link check: watching its network cards in QEMU for %d s", seconds))
	time.Sleep(time.Duration(seconds) * time.Second)
	for _, c := range []string{"trace-event e1000x_* off", "trace-event e1000_receiver_overrun off", "trace-event e1000e_core_ctrl_*_reset off", "logfile /dev/null"} {
		_, _ = h.do(c, 10*time.Second)
	}
	h.close()
	out, err := a.emuRun(host, "grep -ao 'e1000[ex]\\?_[a-z_]*' "+emu.Q(file)+" | sort | uniq -c; rm -f "+emu.Q(file), 20*time.Second)
	if err != nil {
		return "", err
	}
	counts := map[string]int{}
	for _, ln := range strings.Split(out, "\n") {
		if f := strings.Fields(ln); len(f) == 2 {
			if n, err := strconv.Atoi(f[0]); err == nil {
				counts[f[1]] = n
			}
		}
	}
	var keys []string
	for k := range counts {
		if emuLinkEvents[k] != "" {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })
	var b strings.Builder
	fmt.Fprintf(&b, "%s, %d s, as QEMU saw its network cards:\n", name, seconds)
	for _, k := range keys {
		fmt.Fprintf(&b, "- %d × %s\n", counts[k], emuLinkEvents[k])
	}
	switch neg := counts["e1000x_link_negotiation_start"]; {
	case len(keys) == 0:
		b.WriteString("Nothing: QEMU kept every link up and passed every frame. Links going down inside the switch come from its own software, not the emulation.\n")
	case neg > 0:
		fmt.Fprintf(&b, "QEMU took links down %d times because the switch restarted auto-negotiation: link flaps of the switch in this window come from that.\n", neg)
	case counts["e1000x_rx_disabled"]+counts["e1000x_rx_can_recv_disabled"] > 0:
		b.WriteString("The switch turned a card's receiver off (it reset the card): its own software did that.\n")
	default:
		b.WriteString("QEMU kept the links up: link flaps of the switch in this window come from its own software.\n")
	}
	res := strings.TrimSpace(b.String())
	emuEvent(id, "info", "link check: "+strings.ReplaceAll(res, "\n", " "))
	return res, nil
}

// EmuQemuCommand is the QEMU command line a switch runs with now (read from the host), or the one it
// starts with when it is not running.
func (a *App) EmuQemuCommand(id string) (string, error) {
	emuLab.mu.Lock()
	l := emuLab.f.Lab
	var v *emu.VM
	if l != nil {
		v = l.VMByID(id)
	}
	if v == nil {
		emuLab.mu.Unlock()
		return "", errors.New("no such switch")
	}
	host, lh, vv := emuHostOf(*v), a.emuFor(emuHostOf(*v)), *v
	im := emuImage(v.Image)
	emuLab.mu.Unlock()
	if out, err := a.emuRun(host, lh.CmdLineScript(id), 20*time.Second); err == nil && strings.Contains(out, "qemu") {
		return "Running on " + host[strings.LastIndex(host, "/")+1:] + ":\n" + strings.TrimSpace(out), nil
	}
	if im == nil {
		return "", errors.New("its image is gone")
	}
	return "Not running; it starts with (the CPU as the host has KVM):\nqemu-system-x86_64 " + strings.Join(lh.Args(vv, im), " "), nil
}
