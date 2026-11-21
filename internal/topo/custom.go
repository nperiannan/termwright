package topo

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/textfsm"
)

// Custom is a network map plan written by the user for a device type the map has no parser for: the
// commands to run and a TextFSM template for each output. Neighbour templates give LOCAL_PORT, NEIGHBOR,
// NEIGHBOR_PORT, CHASSIS_ID and MGMT_IP (the ntc-templates names LOCAL_INTERFACE, NEIGHBOR_NAME,
// NEIGHBOR_INTERFACE and MGMT_ADDRESS work too); identity templates HOSTNAME, CHASSIS_ID and MODEL;
// port templates PORT, ADMIN, OPER (or STATUS) and SPEED.
type Custom struct {
	Paging            string `json:"paging,omitempty"`
	Identity          string `json:"identity,omitempty"`
	IdentityTemplate  string `json:"identityTemplate,omitempty"`
	Neighbors         string `json:"neighbors,omitempty"`
	NeighborsTemplate string `json:"neighborsTemplate,omitempty"`
	Ports             string `json:"ports,omitempty"`
	PortsTemplate     string `json:"portsTemplate,omitempty"`
}

// Set reports whether the plan says how to read neighbours.
func (c Custom) Set() bool { return strings.TrimSpace(c.Neighbors) != "" && strings.TrimSpace(c.NeighborsTemplate) != "" }

var customPaging = regexp.MustCompile(`^(screen-length 0 temporary|skip-page-display|terminal (length 0|datadump|more disable|pager 0|width 0)|no page|disable cli paging|disable clipaging|set cli screen-length 0|terminal no more|config paging disable)$`)

// CustomAllowed reports whether a command may be run by a user's plan: a "show" or "display" command
// (optionally ending in "| no-more" or "| json"), or one of the known commands that only turn paging off.
func CustomAllowed(cmd string) bool {
	c := strings.Join(strings.Fields(cmd), " ")
	if c == "" || strings.ContainsAny(c, ";&`$<>\n\r\\") {
		return false
	}
	if customPaging.MatchString(strings.ToLower(c)) {
		return true
	}
	l := strings.ToLower(c)
	if !strings.HasPrefix(l, "show ") && !strings.HasPrefix(l, "display ") {
		return false
	}
	if i := strings.Index(c, "|"); i >= 0 {
		tail := strings.TrimSpace(c[i+1:])
		return (tail == "no-more" || tail == "json") && !strings.Contains(tail, "|")
	}
	return true
}

// builtinCustom are the plans for device types read with templates instead of Go parsers.
var builtinCustom = map[string]Custom{
	"huawei": {
		Paging: "screen-length 0 temporary", Identity: "display lldp local",
		IdentityTemplate: "Value HOSTNAME (\\S+)\nValue CHASSIS_ID (\\S+)\n\nStart\n  ^\\s*Chassis ID\\s*:\\s*${CHASSIS_ID}\n  ^\\s*System name\\s*:\\s*${HOSTNAME}\n",
		Neighbors:        "display lldp neighbor",
		NeighborsTemplate: "Value Filldown LOCAL_PORT (\\S+)\nValue Required CHASSIS_ID (\\S+)\nValue NEIGHBOR_PORT (\\S+)\nValue NEIGHBOR (\\S+)\nValue MGMT_IP (\\d+\\.\\d+\\.\\d+\\.\\d+)\n\n" +
			"Start\n  ^\\S+\\s+has\\s+\\d+\\s+neighbor -> Continue.Record\n  ^${LOCAL_PORT}\\s+has\\s+\\d+\\s+neighbor\n  ^\\s*Neighbor index\\s*: -> Record\n" +
			"  ^\\s*Chassis ID\\s*:\\s*${CHASSIS_ID}\n  ^\\s*Port ID\\s*:\\s*${NEIGHBOR_PORT}\n  ^\\s*System name\\s*:\\s*${NEIGHBOR}\n  ^\\s*Management address\\s*(?:value)?\\s*:\\s*${MGMT_IP}\n",
	},
	"icx": {
		Paging: "skip-page-display", Identity: "show lldp local-info",
		IdentityTemplate: "Value HOSTNAME ([^\"\\s]+)\nValue CHASSIS_ID ([0-9a-fA-F.:-]+)\n\nStart\n  ^\\s*Chassis ID \\([^)]*\\):\\s*${CHASSIS_ID}\n  ^\\s*System name\\s*:\\s*\"?${HOSTNAME}\"?\n",
		Neighbors:        "show lldp neighbors detail",
		NeighborsTemplate: "Value Filldown LOCAL_PORT (\\S+)\nValue Required CHASSIS_ID ([0-9a-fA-F.:-]+)\nValue NEIGHBOR_PORT (\\S+)\nValue NEIGHBOR ([^\"]+)\nValue MGMT_IP (\\d+\\.\\d+\\.\\d+\\.\\d+)\n\n" +
			"Start\n  ^Local port: -> Continue.Record\n  ^Local port:\\s*${LOCAL_PORT}\n  ^\\s*Neighbor: -> Continue.Record\n" +
			"  ^\\s*\\+ Chassis ID \\([^)]*\\):\\s*${CHASSIS_ID}\n  ^\\s*\\+ Port ID \\([^)]*\\):\\s*${NEIGHBOR_PORT}\n  ^\\s*\\+ System name\\s*:\\s*\"${NEIGHBOR}\"\n  ^\\s*\\+ Management address \\(IPv4\\):\\s*${MGMT_IP}\n",
	},
	"eltex": {
		Paging: "terminal datadump", Identity: "show system",
		IdentityTemplate: "Value HOSTNAME (\\S+)\nValue CHASSIS_ID ([0-9a-fA-F.:-]+)\n\nStart\n  ^System Name:\\s*${HOSTNAME}\n  ^System MAC Address:\\s*${CHASSIS_ID}\n",
		Neighbors:        "show lldp neighbors",
		NeighborsTemplate: "Value LOCAL_PORT ([a-zA-Z]+\\d\\S*)\nValue CHASSIS_ID ([0-9a-fA-F]{2}(?::[0-9a-fA-F]{2}){5})\nValue NEIGHBOR_PORT (\\S+)\nValue NEIGHBOR (\\S*)\n\n" +
			"Start\n  ^\\s*${LOCAL_PORT}\\s+${CHASSIS_ID}\\s+${NEIGHBOR_PORT}\\s+${NEIGHBOR}\\s+.*\\d+\\s*$$ -> Record\n",
	},
}

// BuiltinCustom is the template plan Termwright ships for an OS family ("huawei", "icx", "eltex").
func BuiltinCustom(os string) (Custom, bool) { c, ok := builtinCustom[os]; return c, ok }

func first(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(m[k]); v != "" {
			return v
		}
	}
	return ""
}

var upWord = regexp.MustCompile(`(?i)^(up|enabled?|connected|forwarding|active|yes|true|e|a)$`)

func runTemplate(tpl, out string) ([]map[string]string, error) {
	t, err := textfsm.Parse(tpl)
	if err != nil {
		return nil, err
	}
	tab, err := t.Run(out)
	if err != nil {
		return nil, err
	}
	return tab.Maps(), nil
}

// CustomNeighbors turns a neighbour template's rows into neighbours.
func CustomNeighbors(rows []map[string]string) []Neighbor {
	var ns []Neighbor
	for _, r := range rows {
		n := Neighbor{Proto: LLDP, Age: -1,
			LocalPort:  first(r, "LOCAL_PORT", "LOCAL_INTERFACE"),
			Name:       first(r, "NEIGHBOR", "NEIGHBOR_NAME", "SYSTEM_NAME"),
			RemotePort: first(r, "NEIGHBOR_PORT", "NEIGHBOR_INTERFACE", "NEIGHBOR_PORT_ID", "PORT_ID"),
			IP:         first(r, "MGMT_IP", "MGMT_ADDRESS", "MANAGEMENT_IP"),
		}
		ch := first(r, "CHASSIS_ID")
		n.Chassis = NormMAC(ch)
		if n.Name == "" && n.Chassis == "" {
			n.Name = ch
		}
		if n.LocalPort == "" || (n.Name == "" && n.Chassis == "" && n.IP == "") {
			continue
		}
		ns = append(ns, n)
	}
	return ns
}

// CollectCustom reads a device with a user's (or a built-in template) plan.
func CollectCustom(ctx context.Context, t Target, c Custom, run Runner, now time.Time) Device {
	d := Device{Session: t.Session, Name: t.Name, OS: t.OS, IP: t.IP, Read: now, Neighbors: []Neighbor{}, Ports: []Port{}, LAGs: []LAG{}}
	type job struct{ cmd, tpl, role string }
	var jobs []job
	for _, j := range []job{{c.Paging, "", "paging"}, {c.Identity, c.IdentityTemplate, "identity"}, {c.Neighbors, c.NeighborsTemplate, "lldp"}, {c.Ports, c.PortsTemplate, "ports"}} {
		j.cmd = strings.Join(strings.Fields(j.cmd), " ")
		if j.cmd == "" || (j.role != "paging" && strings.TrimSpace(j.tpl) == "") {
			continue
		}
		if !CustomAllowed(j.cmd) {
			d.Err = "the map only runs show/display commands and the commands that turn paging off: " + j.cmd
			return d
		}
		jobs = append(jobs, j)
	}
	if !c.Set() {
		d.Err = "the device type's network map has no neighbour command and template"
		return d
	}
	cmds := make([]string, len(jobs))
	for i, j := range jobs {
		cmds[i] = j.cmd
	}
	outs, err := run(ctx, cmds)
	if len(outs) != len(cmds) {
		if err != nil {
			d.Err = err.Error()
		} else {
			d.Err = "the device answered " + itoa(len(outs)) + " of " + itoa(len(cmds)) + " commands"
		}
		return d
	}
	got := 0
	for i, j := range jobs {
		out := outs[i]
		if j.role == "paging" {
			continue
		}
		if refused.MatchString(out) {
			d.Notes = append(d.Notes, j.cmd+": not supported on this device")
			continue
		}
		rows, err := runTemplate(j.tpl, out)
		if err != nil {
			d.Notes = append(d.Notes, j.cmd+": template: "+err.Error())
			continue
		}
		got++
		switch j.role {
		case "identity":
			for _, r := range rows {
				if v := first(r, "HOSTNAME", "SYSNAME", "NAME"); v != "" && d.SysName == "" {
					d.SysName = v
				}
				if v := NormMAC(first(r, "CHASSIS_ID", "MAC", "MAC_ADDRESS")); v != "" && d.Chassis == "" {
					d.Chassis = v
				}
				if v := first(r, "MODEL", "HARDWARE"); v != "" && d.Model == "" {
					d.Model = v
				}
			}
		case "lldp":
			d.Neighbors = append(d.Neighbors, CustomNeighbors(rows)...)
			if len(rows) == 0 && lldpOff.MatchString(out) {
				d.Notes = append(d.Notes, "LLDP is off on this device")
			}
		case "ports":
			for _, r := range rows {
				name := first(r, "PORT", "INTERFACE")
				if name == "" {
					continue
				}
				oper := first(r, "OPER", "STATUS", "LINK_STATUS", "PROTOCOL_STATUS")
				admin := first(r, "ADMIN", "ADMIN_STATUS")
				p := Port{Name: name, Oper: upWord.MatchString(oper), Speed: first(r, "SPEED")}
				p.Admin = admin == "" || upWord.MatchString(admin)
				d.Ports = append(d.Ports, p)
			}
		}
	}
	if got == 0 {
		d.Err = "no command could be read"
	}
	return d
}

// steps are the plan's commands as Steps, for the progress of a reading.
func (c Custom) steps() []Step {
	var st []Step
	for _, x := range []Step{{c.Paging, "paging"}, {c.Identity, "mac"}, {c.Neighbors, "lldp"}, {c.Ports, "ports"}} {
		if strings.TrimSpace(x.Cmd) != "" {
			st = append(st, x)
		}
	}
	return st
}
