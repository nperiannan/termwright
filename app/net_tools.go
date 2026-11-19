package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Network tools (SNMP now; the API client and syslog join them): one implementation for the in-app AI
// assistant ("snmp {json}" lines), the Claude connector (MCP tools) and the local API (POST /tools/{name}).
// Reads never ask. A change (an SNMP Set) runs by itself except on a production session, where the
// person is asked - in every caller; in the AI chat the chosen mode decides too (Read only refuses
// changes, "Changes ask first" asks for each).

// netAsker is called before a change; it returns an error to stop it.
type netAsker func(title, msg string, danger bool) error

var netTools = []mcpTool{
	{"snmp", "SNMP with a saved session's SNMP settings (session properties → SNMP; v1, v2c or v3) or an address and credentials: get, getnext, bulk and walk read; set writes (asked first on a production session, and only when the session allows writes). Values come back with the MIB's names and readable values (enum names, times, MAC addresses). mib_search and mib_info look objects up in the loaded MIBs (the standard IETF MIBs and the user's own); traps lists the notifications the trap receiver got.",
		mcpSchema(map[string]any{
			"action":    map[string]any{"type": "string", "enum": []string{"get", "getnext", "bulk", "walk", "set", "mib_search", "mib_info", "traps"}, "description": "what to do"},
			"session":   map[string]any{"type": "string", "description": "a saved session's path (list_sessions); its SNMP settings and saved community / v3 passwords are used"},
			"address":   map[string]any{"type": "string", "description": "instead of session: host or host:port"},
			"version":   map[string]any{"type": "string", "enum": []string{"1", "2c", "3"}, "description": "with address (default 2c)"},
			"community": map[string]any{"type": "string", "description": "with address, v1/v2c (default public)"},
			"oids":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "OIDs or names: sysDescr.0, IF-MIB::ifTable, ifOperStatus, 1.3.6.1.2.1.2.2.1.8"},
			"values":    map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"oid": map[string]any{"type": "string"}, "type": map[string]any{"type": "string"}, "value": map[string]any{"type": "string"}}}, "description": "set: [{oid, value, type?}] - the type comes from the MIB when left out; an enum takes its name (down)"},
			"max":       map[string]any{"type": "integer", "description": "walk: at most this many values (default 2000 here)"},
			"query":     map[string]any{"type": "string", "description": "mib_search: a name, module or OID; mib_info: one object"},
		}, "action")},
}

// isNetTool: the name is one of the network tools.
func isNetTool(name string) bool {
	return slices.ContainsFunc(netTools, func(t mcpTool) bool { return t.Name == name })
}

// netToolRun runs a network tool.
func (a *App) netToolRun(ctx context.Context, name string, args map[string]any, ask netAsker) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	switch name {
	case "snmp":
		return a.snmpTool(ctx, args, ask)
	}
	return "", fmt.Errorf("no tool %s", name)
}

func (a *App) snmpTool(ctx context.Context, args map[string]any, ask netAsker) (string, error) {
	act := strings.ToLower(argStr(args, "action"))
	switch act {
	case "mib_search":
		var b strings.Builder
		for i, n := range a.SNMPMibSearch(argStr(args, "query")) {
			if i >= 60 {
				b.WriteString("… more - search for a longer name\n")
				break
			}
			fmt.Fprintf(&b, "%s::%s  %s  %s %s\n", n.Module, n.Name, n.OID, n.Kind, n.Syntax)
		}
		if b.Len() == 0 {
			return "Nothing in the loaded MIBs matches.", nil
		}
		return b.String(), nil
	case "mib_info":
		n, err := a.SNMPMibInfo(argStr(args, "query"))
		if err != nil {
			return "", err
		}
		j, _ := json.MarshalIndent(n, "", "  ")
		return string(j), nil
	case "traps":
		v := a.SNMPTraps()
		st, list := v.State, v.Traps
		var b strings.Builder
		if !st.On {
			b.WriteString("The trap receiver is off (SNMP → Traps).\n")
		} else {
			fmt.Fprintf(&b, "Listening on UDP %d; %d received.\n", st.Port, st.Count)
		}
		for i, t := range list {
			if i >= 50 {
				break
			}
			fmt.Fprintf(&b, "%s %s v%s %s %s\n", t.At.Format(time.DateTime), t.From, t.Version, t.Kind, t.Name)
			for _, r := range t.Rows {
				fmt.Fprintf(&b, "   %s = %s\n", r.Name, r.Display)
			}
		}
		return b.String(), nil
	}
	r := SNMPRequest{Session: argStr(args, "session"), Op: act, OIDs: argStrs(args, "oids"), Max: argInt(args, "max", 2000, 20000)}
	if ad := argStr(args, "address"); ad != "" {
		r.Session = ""
		r.Target = &SNMPTarget{Address: ad, Version: argStr(args, "version"), Community: argStr(args, "community")}
	}
	if act == "set" {
		if l, ok := args["values"].([]any); ok {
			for _, x := range l {
				if m, ok := x.(map[string]any); ok {
					r.Values = append(r.Values, SNMPValue{OID: argStr(m, "oid"), Type: argStr(m, "type"), Value: fmt.Sprint(m["value"])})
				}
			}
		}
		var parts []string
		for _, v := range r.Values {
			parts = append(parts, v.OID+" = "+v.Value)
		}
		where := r.Session
		if where == "" && r.Target != nil {
			where = r.Target.Address
		}
		prod := false
		if r.Session != "" {
			if s, err := a.store.Get(r.Session); err == nil {
				prod = a.isProtected(s)
			}
		}
		if err := ask("SNMP Set on "+where, "Write with SNMP:\n"+strings.Join(parts, "\n"), prod); err != nil {
			return "", err
		}
		r.Confirmed = true
	}
	res, err := a.snmpRun(ctx, r)
	var b strings.Builder
	for _, row := range res.Rows {
		fmt.Fprintf(&b, "%s = %s (%s)\n", row.Name, row.Display, row.Type)
	}
	if res.Truncated {
		b.WriteString("… stopped at the limit (max)\n")
	}
	if len(res.Rows) > 0 {
		fmt.Fprintf(&b, "[%d values from %s, SNMPv%s, %d ms]", len(res.Rows), res.Address, res.Version, res.Ms)
	}
	if err != nil {
		if b.Len() > 0 {
			return b.String() + "\nthen: " + err.Error(), nil
		}
		return "", err
	}
	if b.Len() == 0 {
		return "No values.", nil
	}
	return b.String(), nil
}

// netWrites: the tool call changes something (asked per the rules above).
func netWrites(name string, args map[string]any) bool {
	return name == "snmp" && strings.EqualFold(argStr(args, "action"), "set")
}

// NetAgentTool runs a network tool for the in-app AI chat. mode is the chat's permission mode ("" changes
// ask first, every, readonly, auto); approved: the person said yes to this step. A step that needs the
// person comes back as Ask.
func (a *App) NetAgentTool(name string, args map[string]any, mode string, approved bool) (EmuToolStep, error) {
	if !isNetTool(name) {
		return EmuToolStep{}, fmt.Errorf("no tool %s", name)
	}
	var need *EmuToolStep
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if mode == "every" && !approved {
		return EmuToolStep{Ask: true, Title: "The AI wants to use " + name, Text: name + " " + toJSON(args)}, nil
	}
	out, err := a.netToolRun(ctx, name, args, func(title, msg string, danger bool) error {
		switch {
		case approved:
			return nil
		case mode == "readonly":
			need = &EmuToolStep{Ask: true, Title: title, Text: msg, Danger: danger}
			return errEmuAsk
		case mode == "auto" && !danger:
			return nil
		}
		need = &EmuToolStep{Ask: true, Title: "The AI wants to change a device: " + title, Text: msg, Danger: danger}
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

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// netAgentText is what the AI chat's system prompt gets for the network tools.
func netAgentText(tools []string) string {
	var b strings.Builder
	for _, t := range netTools {
		if !slices.Contains(tools, t.Name) {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("\n\nNETWORK TOOLS. Put a line in the termwright block: the tool's name and its arguments as one JSON object (results come back in the next message):\n")
		}
		props, _ := t.InputSchema["properties"].(map[string]any)
		var ks []string
		for k := range props {
			ks = append(ks, k)
		}
		slices.Sort(ks)
		fmt.Fprintf(&b, "%s {%s} - %s\n", t.Name, strings.Join(ks, ", "), t.Description)
	}
	if b.Len() > 0 && slices.Contains(tools, "snmp") {
		b.WriteString(`For example: snmp {"action":"walk","session":"Lab/core-sw1","oids":["IF-MIB::ifOperStatus"]} or snmp {"action":"get","session":"Lab/core-sw1","oids":["sysDescr.0","sysUpTime.0"]}. Find the session with find first. A set on a production session is asked; the result says when it was refused.` + "\n")
	}
	return b.String()
}

// netMCP runs a network tool for the Claude connector: reads run, changes run except on production
// sessions (asked in Termwright) or when the user does not let Claude change anything.
func (a *App) netMCP(ctx context.Context, name string, args map[string]any) (string, error) {
	return a.netToolRun(ctx, name, args, func(title, msg string, danger bool) error {
		if a.cfg.Get().AI.MCPCommands == "never" {
			return errors.New("the user does not let Claude change devices (Settings → AI Assistant) - tell the user what to do instead")
		}
		if danger {
			return a.mcpAsk("", "Claude wants to change a PRODUCTION device", "Claude (through the Termwright connector) asks: "+title+"\n\n"+msg, true, false)
		}
		return nil
	})
}
