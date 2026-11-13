package main

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// The AI agent and the emulated lab: what it sees of the lab (switches,
// cables, LAGs, KVM hosts) and the lab commands it can give.

// emuAgentText is the emulated lab as text for the AI ("" when there is none).
func (a *App) emuAgentText() string {
	v, err := a.EmuState()
	if err != nil || !v.Exists || len(v.Devices) == 0 {
		return ""
	}
	name := map[string]string{}
	for _, d := range v.Devices {
		name[d.ID] = d.Hostname
	}
	hostName := func(h string) string { return h[strings.LastIndex(h, "/")+1:] }
	var b strings.Builder
	topo := v.Topology
	if topo == "" {
		topo = "not saved under a name"
	}
	fast := "off: Stop shuts the switches down"
	if v.Fast {
		fast = "on: Stop saves a running switch's state, Start resumes it in seconds"
	}
	fmt.Fprintf(&b, "Topology: %s. Fast start %s.\n", topo, fast)
	b.WriteString("KVM hosts:\n")
	for _, u := range v.HostUse {
		addr := strings.Split(u.Addr, "/")[0]
		if addr != "" {
			addr = " (" + addr + ")"
		}
		fmt.Fprintf(&b, "- %s%s: management %s; %d switches, %d running; vCPU given %d of %d cores (load %.1f, in use %.1f); memory given %d MB, %d of %d MB free\n",
			u.Name, addr, emuMgmtText(u.Mgmt, u.MgmtNet, u.LanFixed), u.Switches, u.Running, u.VCPUs, u.CPUs, u.Load, u.Cores, u.RAM, u.MemFree, u.MemTotal)
	}
	b.WriteString("Switches (name | model and software | state | KVM host | management (SSH) | serial console | saved sessions to open | persona):\n")
	for _, d := range v.Devices {
		model := d.Model
		if d.Persona != nil && len(d.Persona.Slots) > 0 && d.Persona.Slots[0].Model != "" {
			model = d.Persona.Slots[0].Model
		}
		sw := strings.TrimSpace(d.OS + " " + d.Version)
		host := hostName(d.Host)
		if host == "" {
			host = hostName(v.Host)
		}
		persona := "none"
		switch {
		case d.PersonaError != "":
			persona = "not taken: " + d.PersonaError
		case d.PersonaOK:
			persona = "ok"
		case d.Persona != nil:
			persona = "not checked yet"
		}
		state := orStr(d.StateText, d.State)
		if d.Error != "" && !strings.Contains(state, "failed") {
			state += " (problem: " + d.Error + ")"
		}
		up := ""
		if len(d.Uplinks) > 0 {
			up = " | data ports on the host's network: " + strings.Join(d.Uplinks, ", ")
		}
		fmt.Fprintf(&b, "- %s | %s, %s | %s | %s | %s | telnet %s:%d | %q, console %q | %s%s\n",
			d.Hostname, model, sw, state, host, orStr(d.SSH, "-"), host, d.Console, v.Sessions[d.ID], v.Consoles[d.ID], persona, up)
	}
	if len(v.Links) > 0 {
		b.WriteString("Cables:\n")
		for _, l := range v.Links {
			st := "up"
			switch {
			case l.Down:
				st = "pulled"
			case !l.Up:
				st = "down (a switch is off)"
			}
			lag := ""
			if l.LagA != nil {
				lag += fmt.Sprintf("; LAG %s (%s) on %s", l.LagA.Name, l.LagA.Mode, name[l.A])
			}
			if l.LagB != nil {
				lag += fmt.Sprintf("; LAG %s (%s) on %s", l.LagB.Name, l.LagB.Mode, name[l.B])
			}
			tun := ""
			for _, da := range v.Devices {
				for _, db := range v.Devices {
					if da.ID == l.A && db.ID == l.B && da.Host != db.Host {
						tun = "; a UDP tunnel between two KVM hosts"
					}
				}
			}
			check := ""
			if l.Lldp != "" {
				check = "; cabling check: " + l.Lldp
			}
			fmt.Fprintf(&b, "- %s %s <-> %s %s: %s%s%s%s\n", name[l.A], l.APort, name[l.B], l.BPort, st, lag, tun, check)
		}
	} else {
		b.WriteString("Cables: none.\n")
	}
	n := 0
	for i := len(v.Events) - 1; i >= 0 && n < 8; i-- {
		if e := v.Events[i]; e.Level == "error" || e.Level == "warn" {
			if n == 0 {
				b.WriteString("Recent problems (newest first):\n")
			}
			fmt.Fprintf(&b, "- %s %s: %s\n", e.At.Format("15:04"), e.Name, e.Text)
			n++
		}
	}
	return b.String()
}

func emuMgmtText(mgmt, net string, fixed bool) string {
	a := strings.Split(net, "/")[0]
	switch mgmt {
	case "cloud":
		return "the host's network, routed, fixed addresses from " + a
	case "lan":
		if fixed {
			return "the host's network, bridged, fixed addresses from " + a
		}
		return "the host's network, bridged, DHCP"
	case "nat":
		return "the host's NAT network (reached through the host)"
	case "bridge":
		return "a lab bridge on the host (reached through the host)"
	case "forward":
		return "SSH forwarded to ports on the host"
	}
	return mgmt
}

// emuAgentRules tells the AI how to use the lab.
const emuAgentRules = `

EMULATED LAB. The user's emulated lab (real switch images on KVM hosts) is below. You can also give these lines in the termwright block at the end of your answer (one per line):
lab show                                  (the lab again, as it is now)
lab start <switch>|all                    lab stop <switch>|all        lab shutdown <switch>
lab cable <switch> <port> <switch> <port>  lab uncable <switch> <port>
lab pull <switch> <port>                  lab plug <switch> <port>     (take a cable's link down / up)
lab lag <switch> <name> lacp|static <port,port,...>   lab nolag <switch> <name>   (mark a LAG in the topology)
lab readlags <switch>                     (read the LAGs the switch has)
lab setlag <switch> <name>                (type a LAG marked in the topology on the switch, with the image's own commands)
lab unsetlag <switch> <name>              (remove a LAG from the switch)
lab checkcabling                          (compare the switches' LLDP neighbours with the cables)
lab uplink <switch> <port> on|off         (put a data port on the KVM host's network, like a cloud node; applies when the switch starts)
To configure a switch, open its saved session (the first quoted name; its console session when its management does not answer) and run commands there, in that switch's own CLI syntax. A LAG has to be set on the switches themselves (and both ends must match: static or LACP, the same ports): mark it with lab lag on each end, then lab setlag on each end (or configure it in the switch's session yourself), then lab readlags so the topology shows it. New cables work once both switches restart (lab stop, then lab start). Starting a switch takes minutes unless it resumes from a saved state; a switch is ready when its state is running. Never stop, restart or remove switches the user did not ask about.`

// emuAgentTools: the connector's lab tools as JSON lines for the chat agent ("lab traffic {...}").
func emuAgentTools() string {
	var b strings.Builder
	b.WriteString("\nThe lab's other tools take their arguments as JSON after the name (one line; omit what you do not need):\n")
	for _, t := range mcpLabTools {
		props, _ := t.InputSchema["properties"].(map[string]any)
		var ks []string
		for k := range props {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for i, k := range ks {
			p, _ := props[k].(map[string]any)
			if e, ok := p["enum"].([]string); ok {
				ks[i] = k + ": " + strings.Join(e, "|")
			}
		}
		fmt.Fprintf(&b, "lab %s {%s} - %s\n", strings.TrimPrefix(t.Name, "lab_"), strings.Join(ks, ", "), t.Description)
	}
	b.WriteString(`For example: lab traffic {"action":"add_port","switch":"leaf1","port":"5","ip":"10.10.0.1/24"} or lab capture {"action":"read","switch":"leaf1","port":"1","filter":"LLDP"}. Lab changes are not asked (each is kept in the lab's change history, which the user can revert); results come back in the next message.`)
	return b.String()
}

// EmuToolStep is a chat agent's lab tool call: done (Out), or to be asked first (Ask, with what to ask).
type EmuToolStep struct {
	Ask    bool   `json:"ask,omitempty"`
	Title  string `json:"title,omitempty"`
	Text   string `json:"text,omitempty"`
	Danger bool   `json:"danger,omitempty"`
	Out    string `json:"out,omitempty"`
}

var errEmuAsk = errors.New("ask first")

// EmuAgentTool runs a lab tool for the in-app AI chat. The chat calls it approved (lab changes are not
// asked - automated tests must not wait for a person - and each is kept in the lab's change history,
// so it can be reverted), or not approved in Read only: then a change stops before it is made and
// comes back as Ask, and the chat refuses it.
func (a *App) EmuAgentTool(name string, args map[string]any, approved bool) (EmuToolStep, error) {
	if !strings.HasPrefix(name, "lab_") {
		name = "lab_" + name
	}
	if !slices.ContainsFunc(mcpLabTools, func(t mcpTool) bool { return t.Name == name }) {
		return EmuToolStep{}, fmt.Errorf("no lab tool %s", strings.TrimPrefix(name, "lab_"))
	}
	if args == nil {
		args = map[string]any{}
	}
	var need *EmuToolStep
	out, err := a.emuMCPRun("agent", name, args, func(title, msg string, danger, _ bool) error {
		if approved {
			return nil
		}
		r := strings.NewReplacer("Claude (through the Termwright connector) asks", "The AI asks", "Claude wants", "The AI wants")
		need = &EmuToolStep{Ask: true, Title: r.Replace(title), Text: r.Replace(msg), Danger: danger}
		return errEmuAsk
	})
	if need != nil {
		return *need, nil
	}
	if err != nil {
		return EmuToolStep{}, err
	}
	return EmuToolStep{Out: out}, nil
}

// emuAgentLab is what AIChat adds to the system prompt when the agent may use the lab.
func (a *App) emuAgentLab() string {
	t := a.emuAgentText()
	if t == "" {
		return emuAgentRules + emuAgentTools() + "\nThe lab has no switches yet: lab images lists what lab add_switches can make them from."
	}
	return emuAgentRules + emuAgentTools() + "\n" + t
}

// EmuAgentText is the lab as the AI agent sees it (lab show).
func (a *App) EmuAgentText() string {
	if t := a.emuAgentText(); t != "" {
		return t
	}
	return "There is no emulated lab with switches."
}
