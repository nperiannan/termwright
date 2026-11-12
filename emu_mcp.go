package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/emu"
	"github.com/termwright/termwright/internal/secrets"
)

// The emulated lab for an AI agent (the Termwright connector): enough to take
// a test case from a topology to a result - see the images, build switches and
// cables, start them and wait until they are ready, type configuration and
// show commands on their consoles, check the cabling against LLDP, move a
// switch to another host, and read the Events. Each change is asked in
// Termwright first, a whole list at once.

var mcpLabTools = []mcpTool{
	{"lab_images", "List the images and templates the emulated lab can make switches from: id, name, software version, device type, port naming, and whether the image file is there.", mcpSchema(map[string]any{})},
	{"lab_add_switches", "Add switches to the emulated lab from an image or a template (ids from lab_images), optionally on a given KVM host and started. The user is asked in Termwright first.",
		mcpSchema(map[string]any{
			"image":    map[string]any{"type": "string", "description": "image id (or give template)"},
			"template": map[string]any{"type": "string", "description": "template id (its image, resources and persona)"},
			"model":    map[string]any{"type": "string", "description": "persona model of the image, e.g. 5520-24T (optional)"},
			"count":    map[string]any{"type": "integer", "description": "how many (default 1, at most 32)"},
			"names":    map[string]any{"type": "string", "description": "their names, comma separated, or a pattern like leaf-{n} (optional)"},
			"host":     map[string]any{"type": "string", "description": "KVM host name or session path (default: the lab's host)"},
			"start":    map[string]any{"type": "boolean", "description": "start them at once"},
		})},
	{"lab_cable", "Connect (or remove) cables between switches of the emulated lab: switch names and port names as the switches call them (show_lab). A whole list at once; the user is asked first. New cables work once both switches have restarted.",
		mcpSchema(map[string]any{
			"cables": map[string]any{"type": "array", "description": "cables: {a, a_port, b, b_port}; for remove only a and a_port", "items": map[string]any{"type": "object", "properties": map[string]any{
				"a": map[string]any{"type": "string"}, "a_port": map[string]any{"type": "string"}, "b": map[string]any{"type": "string"}, "b_port": map[string]any{"type": "string"}}}},
			"remove": map[string]any{"type": "boolean", "description": "remove these cables instead"},
		}, "cables")},
	{"lab_power", "Start, stop, restart or shut down switches of the emulated lab (names, or all). The user is asked first.",
		mcpSchema(map[string]any{
			"switches": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "switch names; empty = all"},
			"action":   map[string]any{"type": "string", "enum": []string{"start", "stop", "restart", "shutdown"}},
		}, "action")},
	{"lab_wait", "Wait until switches of the emulated lab are booted and configured (running, no step left), or the time is up; returns each one's state and problem.",
		mcpSchema(map[string]any{
			"switches": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "switch names; empty = all"},
			"timeout":  map[string]any{"type": "integer", "description": "seconds (default 600, at most 1800)"},
		})},
	{"lab_console", "Type commands on a switch's serial console (Termwright logs in with the image's login) and return each one's output: configuration or show commands, in the switch's own CLI. The user is asked first, with the whole list. Close the switch's console tab first.",
		mcpSchema(map[string]any{
			"switch":   map[string]any{"type": "string"},
			"commands": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "command lines, in order"},
			"timeout":  map[string]any{"type": "integer", "description": "seconds for all of them (default 180, at most 900)"},
		}, "switch", "commands")},
	{"lab_check_cabling", "Compare the switches' LLDP neighbours with the cables of the emulated lab (the switches must be running): each cable ok, wrong (what the port sees) or without a neighbour.", mcpSchema(map[string]any{})},
	{"lab_move", "Move a switch of the emulated lab to another KVM host: live (it keeps running), copy (saved, its disk copied, resumed) or image (boots there from day 0). The user is asked first.",
		mcpSchema(map[string]any{
			"switch": map[string]any{"type": "string"},
			"host":   map[string]any{"type": "string", "description": "KVM host name or session path"},
			"how":    map[string]any{"type": "string", "enum": []string{"live", "copy", "image"}},
		}, "switch", "host", "how")},
	{"lab_capture", "Packet capture on a switch port of the emulated lab (its cable, both ways; the switch keeps running): start, stop, or read the packets (number, time, source, destination, protocol, length, info). Start and stop are asked in Termwright first.",
		mcpSchema(map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"start", "stop", "read", "list"}},
			"switch": map[string]any{"type": "string"},
			"port":   map[string]any{"type": "string"},
			"filter": map[string]any{"type": "string", "description": "read: only packets whose source, destination, protocol or info contains this (e.g. ICMP, LLDP, 10.0.0.1)"},
			"last":   map[string]any{"type": "integer", "description": "read: how many of the newest packets (default 100, at most 2000)"},
		}, "action")},
	{"lab_events", "Read the emulated lab's Events log: what Termwright did to each switch (started, booted, day 0, persona, moves, errors), newest last.",
		mcpSchema(map[string]any{"last": map[string]any{"type": "integer", "description": "how many lines (default 60, at most 300)"}})},
}

// emuMCPID finds a switch by its name (or id).
func emuMCPID(v EmuView, name string) (EmuDevice, error) {
	for _, d := range v.Devices {
		if strings.EqualFold(d.Hostname, name) || d.ID == name {
			return d, nil
		}
	}
	return EmuDevice{}, fmt.Errorf("no switch %q in the lab (show_lab)", name)
}

// emuMCPHost finds a KVM host by its name or session path.
func emuMCPHost(name string) (string, error) {
	emuLab.mu.Lock()
	defer emuLab.mu.Unlock()
	if name == "" {
		return emuLab.f.Host, nil
	}
	for _, h := range emuLab.f.Hosts {
		if h.Session == name || strings.EqualFold(h.Name, name) || strings.EqualFold(h.Session[strings.LastIndex(h.Session, "/")+1:], name) {
			return h.Session, nil
		}
	}
	return "", fmt.Errorf("no KVM host %q (Hosts in the emulated lab)", name)
}

func argStrs(m map[string]any, k string) []string {
	var out []string
	if l, ok := m[k].([]any); ok {
		for _, x := range l {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

// emuMCP runs a lab_ tool.
func (a *App) emuMCP(name string, args map[string]any) (string, error) {
	defer emuAs("ai")()
	v, err := a.EmuState()
	if err != nil {
		return "", err
	}
	ask := func(title, msg string, danger bool) error { return a.mcpAsk("", title, msg, danger, false) }
	ids := func(names []string) ([]string, []string, error) {
		var out, shown []string
		if len(names) == 0 {
			for _, d := range v.Devices {
				out, shown = append(out, d.ID), append(shown, d.Hostname)
			}
			return out, shown, nil
		}
		for _, n := range names {
			d, err := emuMCPID(v, n)
			if err != nil {
				return nil, nil, err
			}
			out, shown = append(out, d.ID), append(shown, d.Hostname)
		}
		return out, shown, nil
	}
	switch name {
	case "lab_images":
		var b strings.Builder
		b.WriteString("Images (id | name | version | device type | ports | file):\n")
		for _, im := range v.Images {
			file := "missing: download or choose it in Termwright"
			if im.File != "" {
				file = "there"
			}
			fmt.Fprintf(&b, "- %s | %s | %s | %s | %d ports named %s | %s\n", im.ID, im.Name, im.Version, orDash(im.DeviceType), max(im.NICs-1, 0), orDash(im.PortName), file)
		}
		if len(v.Templates) > 0 {
			b.WriteString("Templates (id | name | image):\n")
			for _, t := range v.Templates {
				fmt.Fprintf(&b, "- %s | %s | %s\n", t.ID, t.Name, t.Image)
			}
		}
		return b.String(), nil
	case "lab_add_switches":
		host, err := emuMCPHost(argStr(args, "host"))
		if err != nil {
			return "", err
		}
		g := EmuGroup{Image: argStr(args, "image"), Template: argStr(args, "template"), Model: argStr(args, "model"), Count: argInt(args, "count", 1, 32), Names: argStr(args, "names")}
		if g.Image == "" && g.Template == "" {
			return "", errors.New("give an image or a template id (lab_images)")
		}
		start, _ := args["start"].(bool)
		what := orStr(g.Template, g.Image)
		if err := ask("Claude wants to add switches", fmt.Sprintf("Claude (through the Termwright connector) asks to add %d switch(es) from %s%s on %s%s.", g.Count, what, map[bool]string{true: " (" + g.Model + ")", false: ""}[g.Model != ""], host[strings.LastIndex(host, "/")+1:], map[bool]string{true: " and start them", false: ""}[start]), false); err != nil {
			return "", err
		}
		before := map[string]bool{}
		for _, d := range v.Devices {
			before[d.ID] = true
		}
		nv, err := a.EmuAdd(EmuRequest{Host: host, Groups: []EmuGroup{g}, Start: start})
		if err != nil {
			return "", err
		}
		var made []string
		for _, d := range nv.Devices {
			if !before[d.ID] {
				made = append(made, d.Hostname)
			}
		}
		return "Added: " + strings.Join(made, ", ") + map[bool]string{true: " - starting (lab_wait until they are ready)", false: " - not started (lab_power start)"}[start], nil
	case "lab_cable":
		remove, _ := args["remove"].(bool)
		list, _ := args["cables"].([]any)
		if len(list) == 0 {
			return "", errors.New("give the cables")
		}
		type cab struct{ a, ap, b, bp string }
		var cs []cab
		var lines []string
		for _, x := range list {
			m, _ := x.(map[string]any)
			c := cab{argStr(m, "a"), argStr(m, "a_port"), argStr(m, "b"), argStr(m, "b_port")}
			if c.a == "" || c.ap == "" || !remove && (c.b == "" || c.bp == "") {
				return "", errors.New("each cable needs a, a_port, b and b_port")
			}
			if _, err := emuMCPID(v, c.a); err != nil {
				return "", err
			}
			if !remove {
				if _, err := emuMCPID(v, c.b); err != nil {
					return "", err
				}
				lines = append(lines, fmt.Sprintf("%s %s - %s %s", c.a, c.ap, c.b, c.bp))
			} else {
				lines = append(lines, fmt.Sprintf("%s %s", c.a, c.ap))
			}
			cs = append(cs, c)
		}
		title := map[bool]string{true: "Claude wants to remove cables", false: "Claude wants to connect cables"}[remove]
		if err := ask(title, "Claude (through the Termwright connector) asks to "+map[bool]string{true: "remove the cables at", false: "connect"}[remove]+":\n\n    "+strings.Join(lines, "\n    "), remove); err != nil {
			return "", err
		}
		var done, failed []string
		for i, c := range cs {
			da, _ := emuMCPID(v, c.a)
			var err error
			if remove {
				_, err = a.EmuDisconnect(da.ID, c.ap)
			} else {
				db, _ := emuMCPID(v, c.b)
				_, err = a.EmuConnect(EmuLinkView{A: da.ID, APort: c.ap, B: db.ID, BPort: c.bp})
			}
			if err != nil {
				failed = append(failed, lines[i]+": "+err.Error())
			} else {
				done = append(done, lines[i])
			}
		}
		out := fmt.Sprintf("Done: %d of %d.", len(done), len(cs))
		if len(failed) > 0 {
			out += "\nNot done:\n- " + strings.Join(failed, "\n- ")
		}
		if !remove && len(done) > 0 {
			out += "\nNew cables work once both switches have restarted (lab_power restart)."
		}
		return out, nil
	case "lab_power":
		act := argStr(args, "action")
		list, shown, err := ids(argStrs(args, "switches"))
		if err != nil {
			return "", err
		}
		if err := ask("Claude wants to "+act+" switches", "Claude (through the Termwright connector) asks to "+act+": "+strings.Join(shown, ", "), act != "start"); err != nil {
			return "", err
		}
		switch act {
		case "start":
			_, err = a.EmuStart(list)
		case "stop":
			_, err = a.EmuStop(list)
		case "restart":
			_, err = a.EmuRestart(list)
		case "shutdown":
			_, err = a.EmuShutDown(list)
		default:
			return "", errors.New("action is start, stop, restart or shutdown")
		}
		if err != nil {
			return "", err
		}
		return strings.Title(act) + " given for " + strings.Join(shown, ", ") + ". lab_wait follows them until they are ready.", nil //nolint:staticcheck
	case "lab_wait":
		names := argStrs(args, "switches")
		if _, _, err := ids(names); err != nil {
			return "", err
		}
		end := time.Now().Add(time.Duration(argInt(args, "timeout", 600, 1800)) * time.Second)
		for {
			v, _ = a.EmuState()
			var b strings.Builder
			all := true
			for _, d := range v.Devices {
				if len(names) > 0 && !slicesContainsFold(names, d.Hostname) {
					continue
				}
				ready := d.Running && d.StateText == "" && d.Error == ""
				all = all && (ready || d.Error != "")
				st := map[bool]string{true: "running", false: "stopped"}[d.Running]
				if d.StateText != "" {
					st = d.StateText
				}
				fmt.Fprintf(&b, "- %s: %s", d.Hostname, st)
				if d.Error != "" {
					b.WriteString(" - problem: " + d.Error)
				}
				if d.SSH != "" {
					b.WriteString(" - management " + d.SSH)
				}
				b.WriteString("\n")
			}
			if all {
				return "Ready (or stopped with a problem):\n" + b.String(), nil
			}
			if time.Now().After(end) {
				return "Not all ready when the time was up:\n" + b.String(), nil
			}
			time.Sleep(5 * time.Second)
		}
	case "lab_console":
		d, err := emuMCPID(v, argStr(args, "switch"))
		if err != nil {
			return "", err
		}
		cmds := argStrs(args, "commands")
		if len(cmds) == 0 {
			return "", errors.New("give the commands")
		}
		danger := false
		for _, c := range cmds {
			danger = danger || mcpRisky.MatchString(c)
		}
		if err := ask("Claude wants to type on a console", "Claude (through the Termwright connector) asks to type on the console of "+d.Hostname+":\n\n    "+strings.Join(cmds, "\n    "), danger); err != nil {
			return "", err
		}
		out, err := a.emuConsoleRun(d.ID, cmds, time.Duration(argInt(args, "timeout", 180, 900))*time.Second)
		red, _ := aiRedact(out, false)
		if err != nil {
			return red, err
		}
		return red, nil
	case "lab_check_cabling":
		nv, err := a.EmuCheckCabling()
		if err != nil {
			return "", err
		}
		name := map[string]string{}
		for _, d := range nv.Devices {
			name[d.ID] = d.Hostname
		}
		var b strings.Builder
		for _, k := range nv.Links {
			fmt.Fprintf(&b, "- %s %s - %s %s: %s\n", name[k.A], k.APort, name[k.B], k.BPort, orStr(k.Lldp, "not checked (is it running?)"))
		}
		if b.Len() == 0 {
			return "There are no cables.", nil
		}
		return b.String(), nil
	case "lab_move":
		d, err := emuMCPID(v, argStr(args, "switch"))
		if err != nil {
			return "", err
		}
		host, err := emuMCPHost(argStr(args, "host"))
		if err != nil {
			return "", err
		}
		how := argStr(args, "how")
		if err := ask("Claude wants to move a switch", fmt.Sprintf("Claude (through the Termwright connector) asks to move %s to %s (%s).", d.Hostname, host[strings.LastIndex(host, "/")+1:], how), how != "live"); err != nil {
			return "", err
		}
		switch how {
		case "image":
			_, err = a.EmuMoveVM(d.ID, host)
		case "live", "copy":
			_, err = a.EmuMigrate(d.ID, host, how == "live")
		default:
			return "", errors.New("how is live, copy or image")
		}
		if err != nil {
			return "", err
		}
		return "Moving " + d.Hostname + " - lab_events shows each step; lab_wait until it is ready.", nil
	case "lab_capture":
		act := argStr(args, "action")
		if act == "list" {
			var b strings.Builder
			for _, c := range a.EmuCaptures() {
				fmt.Fprintf(&b, "- %s %s%s on %s: %s, %d bytes, started %s\n", c.Switch, c.Port, map[bool]string{true: " (cable to " + c.Peer + ")", false: ""}[c.Peer != ""], c.Host[strings.LastIndex(c.Host, "/")+1:], map[bool]string{true: "running", false: "stopped"}[c.Running], c.Size, c.Started)
			}
			if b.Len() == 0 {
				return "No capture yet.", nil
			}
			return b.String(), nil
		}
		d, err := emuMCPID(v, argStr(args, "switch"))
		if err != nil {
			return "", err
		}
		port := argStr(args, "port")
		if port == "" {
			return "", errors.New("give the port")
		}
		key := d.ID + "|" + port
		switch act {
		case "start":
			if err := ask("Claude wants to capture packets", fmt.Sprintf("Claude (through the Termwright connector) asks to capture %s %s (both ways; it keeps running).", d.Hostname, port), false); err != nil {
				return "", err
			}
			if _, err := a.EmuCaptureStart(d.ID, port); err != nil {
				return "", err
			}
			return "Capturing " + d.Hostname + " " + port + " - generate the traffic, then lab_capture read.", nil
		case "stop":
			if _, err := a.EmuCaptureStop(key); err != nil {
				return "", err
			}
			return "Stopped; the pcap file stays on the host (lab_capture read still works).", nil
		case "read":
			ps, err := a.EmuCapturePackets(key, 0)
			if err != nil {
				return "", err
			}
			f := strings.ToLower(argStr(args, "filter"))
			var keep []EmuPacket
			for _, p := range ps {
				if f == "" || strings.Contains(strings.ToLower(p.Src+" "+p.Dst+" "+p.Proto+" "+p.Info), f) {
					keep = append(keep, p)
				}
			}
			n := argInt(args, "last", 100, 2000)
			total := len(keep)
			if len(keep) > n {
				keep = keep[len(keep)-n:]
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d packets match (%d in the capture); the last %d:\n", total, len(ps), len(keep))
			for _, p := range keep {
				fmt.Fprintf(&b, "%d %.3f %s -> %s %s %d %s\n", p.No, p.Time, p.Src, p.Dst, p.Proto, p.Len, p.Info)
			}
			return b.String(), nil
		}
		return "", errors.New("action is start, stop, read or list")
	case "lab_events":
		n := argInt(args, "last", 60, 300)
		ev := v.Events
		if len(ev) > n {
			ev = ev[len(ev)-n:]
		}
		var b strings.Builder
		for _, e := range ev {
			h := e.Host
			if h != "" {
				h = h[strings.LastIndex(h, "/")+1:] + " "
			}
			fmt.Fprintf(&b, "%s %s%s %s %s\n", e.At.Format("15:04:05"), h, e.Name, strings.ToUpper(e.Level), e.Text)
		}
		out, _ := aiRedact(b.String(), false)
		return out, nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

func slicesContainsFold(l []string, s string) bool {
	for _, x := range l {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// emuConsoleRun logs in on a switch's console and types commands, returning each one's output.
func (a *App) emuConsoleRun(id string, cmds []string, timeout time.Duration) (string, error) {
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
	host := emuHostOf(*v)
	lh := a.emuFor(host)
	port := lh.ConsolePort(*v)
	ip := emuImage(v.Image)
	if ip == nil {
		emuLab.mu.Unlock()
		return "", errors.New("its image is gone")
	}
	im := *ip
	emuLab.mu.Unlock()
	im.Password, _ = secrets.Lookup(emuImageKey(im.ID))
	im.Password = orStr(im.Password, im.DefaultPassword)
	cl, err := a.emuClient(context.Background(), host)
	if err != nil {
		return "", err
	}
	conn, err := cl.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return "", fmt.Errorf("its console: %w (is it running, and its console tab closed?)", err)
	}
	c := emu.NewConsole(conn)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	prompt, err := emu.Login(ctx, c, &im)
	if err != nil {
		return "", fmt.Errorf("logging in on its console: %w", err)
	}
	var b strings.Builder
	for _, cmd := range cmds {
		emuLogf(emuLvInfo, host, v.Name, "console (AI connector) types: %s", cmd)
		out, err := emu.Run(ctx, c, &im, prompt, cmd)
		fmt.Fprintf(&b, "%s\n%s\n", cmd, strings.TrimRight(out, "\r\n "))
		if err != nil {
			return b.String(), fmt.Errorf("%s: %w", cmd, err)
		}
	}
	return b.String(), nil
}
