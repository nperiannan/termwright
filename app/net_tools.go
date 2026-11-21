package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/apiclient"
)

// Network tools (SNMP now; the API client and syslog join them): one implementation for the in-app AI
// assistant ("snmp {json}" lines), the Claude connector (MCP tools) and the local API (POST /tools/{name}).
// Reads never ask. A change (an SNMP Set) runs by itself except on a production session, where the
// person is asked - in every caller; in the AI chat the chosen mode decides too (Read only refuses
// changes, "Changes ask first" asks for each).

// netAsker is called before a change; it returns an error to stop it.
type netAsker func(title, msg string, danger bool) error

var netTools = []mcpTool{
	{"tests", "Termwright tests: YAML files in the tests folder (cases/), each a list of steps - a command on a saved session (run), an HTTP request (http) or an SNMP get / walk / set (snmp) - with checks on what came back (contains, matches, a field, JSON or SNMP value equals / greater / less, status, time). list lists the tests; show gives one as YAML; run runs tests (files, or none for all) and returns pass/fail per step, writing a JUnit XML file and an HTML report under reports/; save writes a test from YAML (and its Go test); generate writes the Go, Python and TCL tests again (go test ./cases/..., python lib/python/run_tests.py or tclsh lib/tcl/run_tests.tcl in the tests folder runs them through Termwright); step runs one step without its checks and returns its answer as JSON (used by the generated Go tests); new_testbed writes testbeds/<name>.yaml with a role per saved session of a session folder (core-sw1 → {{core_sw1}}); a test with params runs once per entry; schedules lists the scheduled runs, schedule adds or (with id) changes one (repeat daily|weekdays|weekly|hours, at HH:MM, day 0-6, hours, files, testbed, production=true to allow production sessions), unschedule removes one; save_state (kinds config|lab, sessions) and restore_state (token) save and put back the devices' state for the generated tests; a test's setup and cleanup sections run before and after its steps (cleanup always), and restore: [config, lab] saves and puts back the devices; results gives the last run's report. Runs log in with the sessions' saved logins in the background; a run touching production sessions is asked first.",
		mcpSchema(map[string]any{
			"action":          map[string]any{"type": "string", "enum": []string{"list", "show", "run", "save", "generate", "step", "new_testbed", "schedules", "schedule", "unschedule", "save_state", "restore_state", "results", "history", "send_qtest", "defect", "approvals", "request_approval", "approve", "reject"}, "description": "what to do"},
			"file":            map[string]any{"type": "string", "description": "show/save/run: a test file relative to the tests folder, e.g. cases/vlan/add-vlan.yaml (cases/ and .yaml may be left out)"},
			"files":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "run: the tests to run - files, folders of cases/ or suites/… files (none: all)"},
			"dir":             map[string]any{"type": "string", "description": "send_qtest/defect: a run's folder (reports/…); default the last run"},
			"reason":          map[string]any{"type": "string", "description": "request_approval: why the run should go ahead"},
			"from":            map[string]any{"type": "integer", "description": "run: start each test at this step (1-based; the setup still runs)"},
			"only":            map[string]any{"type": "integer", "description": "run: run only this step of the test (no setup, no cleanup with cleanup off)"},
			"note":            map[string]any{"type": "string", "description": "approve/reject: a note"},
			"testbeds":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "run: several testbeds - the tests run on each at the same time"},
			"force_lock":      map[string]any{"type": "boolean", "description": "run: take over the testbed when someone else's run has it locked"},
			"tags":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "run: only tests with one of these tags; !tag leaves its tests out"},
			"yaml":            map[string]any{"type": "string", "description": "save: the test as YAML"},
			"step":            map[string]any{"type": "object", "description": "step: one step {run: {session, command, prompt}}, {http: {session, method, url, headers, body}} or {snmp: {session, op, oids, values}}, timeout"},
			"name":            map[string]any{"type": "string", "description": "new_testbed: the testbed's name"},
			"folder":          map[string]any{"type": "string", "description": "new_testbed: a session folder, e.g. Lab"},
			"id":              map[string]any{"type": "string", "description": "schedule/unschedule: the schedule's id (schedules lists them); approve/reject: the request's id"},
			"repeat":          map[string]any{"type": "string", "enum": []string{"daily", "weekdays", "weekly", "hours"}, "description": "schedule: how often"},
			"at":              map[string]any{"type": "string", "description": "schedule: time of day HH:MM (daily, weekdays, weekly)"},
			"day":             map[string]any{"type": "integer", "description": "schedule: weekday for weekly, 0 Sunday … 6 Saturday"},
			"hours":           map[string]any{"type": "integer", "description": "schedule: every N hours (repeat hours)"},
			"production":      map[string]any{"type": "string", "enum": []string{"true", "false"}, "description": "schedule: allow production sessions (asked)"},
			"cleanup":         map[string]any{"type": "string", "enum": []string{"steps", "off", "auto"}, "description": "run: steps (default: each test's cleanup steps), off (no cleanup - tests build on each other) or auto (the cleanup steps, else the undo of the configuration commands that ran)"},
			"on_cleanup_fail": map[string]any{"type": "string", "enum": []string{"stop", "restore", "factory"}, "description": "run: when a test's cleanup fails - stop (skip the rest), restore (the configuration saved before the run) or factory (factory default)"},
			"kinds":           map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"config", "lab"}}, "description": "save_state: what to save"},
			"sessions":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "save_state: the saved sessions"},
			"token":           map[string]any{"type": "string", "description": "restore_state: the token save_state returned"},
			"testbed":         map[string]any{"type": "string", "description": "run: a file in testbeds/ whose values are used as {{variables}}"},
			"vars":            map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "run: values for {{variables}} that win over the tests' own"},
		}, "action")},
	{"device", "A saved session's device facts and device type. facts: the model, version, serial number, MAC address, uptime and other facts read from it last time; refresh_facts logs in in the background (with the session's saved login) and reads them now with the device type's fact rules. identify logs in, runs the device types' detect commands (show version, display version …) and says which device type's Detect pattern matches; set_type sets the session's device type (asked first). types lists the device types with their Detect patterns. Nothing is typed in the user's tabs.",
		mcpSchema(map[string]any{
			"action":  map[string]any{"type": "string", "enum": []string{"facts", "refresh_facts", "identify", "set_type", "types"}, "description": "what to do"},
			"session": map[string]any{"type": "string", "description": "the saved session's path, e.g. Datacenter/Core/core-sw1"},
			"type":    map[string]any{"type": "string", "description": "set_type: the device type's name (\"\" clears it)"},
		}, "action")},
	{"snmp", "SNMP with a saved session's SNMP settings (session properties → SNMP; v1, v2c or v3) or an address and credentials: get, getnext, bulk and walk read; set writes (asked first on a production session, and only when the session allows writes). Values come back with the MIB's names and readable values (enum names, times, MAC addresses). mib_search and mib_info look objects up in the loaded MIBs (the standard IETF MIBs and the user's own); traps lists the notifications the trap receiver got; send_trap sends a test notification to a receiver.",
		mcpSchema(map[string]any{
			"action":    map[string]any{"type": "string", "enum": []string{"get", "getnext", "bulk", "walk", "set", "mib_search", "mib_info", "traps", "send_trap", "dashboards", "poll_dashboard", "simulate", "stop_simulator", "simulator", "load_walk"}, "description": "what to do"},
			"session":   map[string]any{"type": "string", "description": "a saved session's path (list_sessions); its SNMP settings and saved community / v3 passwords are used"},
			"address":   map[string]any{"type": "string", "description": "instead of session: host or host:port"},
			"version":   map[string]any{"type": "string", "enum": []string{"1", "2c", "3"}, "description": "with address (default 2c)"},
			"community": map[string]any{"type": "string", "description": "with address, v1/v2c (default public)"},
			"oids":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "OIDs or names: sysDescr.0, IF-MIB::ifTable, ifOperStatus, 1.3.6.1.2.1.2.2.1.8"},
			"values":    map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"oid": map[string]any{"type": "string"}, "type": map[string]any{"type": "string"}, "value": map[string]any{"type": "string"}}}, "description": "set: [{oid, value, type?}] - the type comes from the MIB when left out; an enum takes its name (down)"},
			"max":       map[string]any{"type": "integer", "description": "walk: at most this many values (default 2000 here)"},
			"query":     map[string]any{"type": "string", "description": "mib_search: a name, module or OID; mib_info: one object"},
			"trap":      map[string]any{"type": "string", "description": "send_trap: the notification (IF-MIB::linkDown or an OID), sent to address (port 162) with values"},
			"dashboard": map[string]any{"type": "string", "description": "poll_dashboard: the saved dashboard's name or id"},
			"file":      map[string]any{"type": "string", "description": "simulate / load_walk: a walk file (snmpwalk -On format)"},
			"port":      map[string]any{"type": "integer", "description": "simulate: UDP port (default 1161)"},
		}, "action")},
	{"http", "The REST API client with saved API sessions (Protocol API: base URL, authentication - API key, bearer token, login, basic, OAuth 2.0 - and {{variables}}; secrets stay in the credential store) and collections. send sends one request (a path goes after the session's base URL; the login token is fetched and renewed by itself) or a saved one (collection + request); POST, PUT, PATCH and DELETE on a production session are asked first. run runs a collection's requests with their tests; collections / collection list them; save adds or replaces a request in a collection (made when missing), with its pre-request script (preScript) and tests in Postman's pm.* style (pm.test, pm.expect, pm.response.json(), pm.variables.set); import reads a cURL command, a Postman collection or an OpenAPI document into a collection; history lists the last requests; environments lists the environments (Postman's) and globals; variables lists the variables a request can use and where each comes from (secret values hidden).",
		mcpSchema(map[string]any{
			"action":      map[string]any{"type": "string", "enum": []string{"send", "run", "collections", "collection", "save", "import", "history", "environments", "variables", "mock", "stop_mock", "mocks", "monitors", "run_monitor"}, "description": "what to do"},
			"environment": map[string]any{"type": "string", "description": "send / run / variables: an environment's id or name (environments lists them); its variables win over the session's"},
			"session":     map[string]any{"type": "string", "description": "an API session's path (list_sessions shows protocol API)"},
			"env":         map[string]any{"type": "string", "enum": []string{"", "production"}, "description": "production uses the variables' production values"},
			"method":      map[string]any{"type": "string", "description": "send: GET (default), POST, PUT, PATCH, DELETE, HEAD, OPTIONS"},
			"url":         map[string]any{"type": "string", "description": "send: a path (/rest/restconf/data/openconfig-interfaces:interfaces) or a full URL; {{variables}} work"},
			"headers":     map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "send: extra headers"},
			"query":       map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "send: query parameters"},
			"body":        map[string]any{"description": "send: a JSON value (sent as JSON) or text"},
			"collection":  map[string]any{"type": "string", "description": "a collection's id or name"},
			"request":     map[string]any{"description": "send / run: a saved request's id or name; save: the request object {name, folder, method, url, params, headers, body {mode, raw}, tests}"},
			"folder":      map[string]any{"type": "string", "description": "run: only this folder"},
			"iterations":  map[string]any{"type": "integer", "description": "run: how many times"},
			"data":        map[string]any{"type": "string", "description": "run: CSV with a header line or a JSON array; one iteration per row"},
			"text":        map[string]any{"type": "string", "description": "import: the cURL command, Postman collection or OpenAPI document"},
			"name":        map[string]any{"type": "string", "description": "import / save: the collection's name"},
		}, "action")},
	{"syslog", "The syslog server in Termwright (receives from devices over UDP, TCP and TLS while Termwright runs; senders are matched to saved sessions) and a syslog client. status: whether it listens, counts per severity, senders. messages: what was received, filtered by words or /regex/, sender, severity and minutes back (newest last). start / stop the server (ports: udp 514, tcp, tls 6514; ports below 1024 may need administrator rights - use 1514 then). send: a test message to a syslog server. clear forgets the messages.",
		mcpSchema(map[string]any{
			"action":   map[string]any{"type": "string", "enum": []string{"status", "messages", "start", "stop", "send", "clear"}, "description": "what to do"},
			"query":    map[string]any{"type": "string", "description": "messages: words that must all be in the message, or /regex/"},
			"host":     map[string]any{"type": "string", "description": "messages: only from this sender (address or host name)"},
			"severity": map[string]any{"type": "string", "description": "messages: this severity or worse (emerg, alert, crit, err, warning, notice, info, debug or 0-7); send: the message's severity"},
			"minutes":  map[string]any{"type": "integer", "description": "messages: only the last minutes"},
			"limit":    map[string]any{"type": "integer", "description": "messages: at most this many (default 200)"},
			"udp":      map[string]any{"type": "integer", "description": "start: UDP port (default the saved one, 514)"},
			"tcp":      map[string]any{"type": "integer", "description": "start: TCP port (0 off)"},
			"tls":      map[string]any{"type": "integer", "description": "start: TLS port (0 off)"},
			"target":   map[string]any{"type": "string", "description": "send: the syslog server, host or host:port"},
			"proto":    map[string]any{"type": "string", "enum": []string{"udp", "tcp", "tls"}, "description": "send: transport (default udp)"},
			"facility": map[string]any{"type": "string", "description": "send: local7, user, … (default user)"},
			"text":     map[string]any{"type": "string", "description": "send: the message"},
		}, "action")},
	{"keep_awake", "Whether Termwright keeps the computer from going to sleep on its own while it is open (for long test runs, captures and labs). status says whether it is kept awake; on / off changes the setting (saved).",
		mcpSchema(map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"status", "on", "off"}, "description": "what to do"},
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
	case "http":
		return a.httpTool(ctx, args, ask)
	case "syslog":
		return a.syslogTool(args)
	case "device":
		return a.deviceTool(ctx, args, ask)
	case "tests":
		return a.testsTool(ctx, args, ask)
	case "keep_awake":
		return a.keepAwakeTool(args)
	}
	return "", fmt.Errorf("no tool %s", name)
}

// keepAwakeTool reads or changes Keep the computer awake.
func (a *App) keepAwakeTool(args map[string]any) (string, error) {
	switch strings.ToLower(argStr(args, "action")) {
	case "on", "off":
		on := strings.EqualFold(argStr(args, "action"), "on")
		cfg := a.cfg.Get()
		cfg.KeepAwake = &on
		if err := a.cfg.Save(cfg); err != nil {
			return "", err
		}
		a.applyKeepAwake()
	}
	st := a.KeepAwakeStatus()
	if st.Error != "" {
		return "Not kept awake: " + st.Error, nil
	}
	if st.Active {
		return "The computer is kept awake while Termwright is open.", nil
	}
	return "The computer may go to sleep on its own.", nil
}

func (a *App) snmpTool(ctx context.Context, args map[string]any, ask netAsker) (string, error) {
	act := strings.ToLower(argStr(args, "action"))
	switch act {
	case "dashboards":
		var b strings.Builder
		for _, d := range a.SNMPDashboards() {
			fmt.Fprintf(&b, "%s  %s  %d values every %d s  alarms=%v\n", d.ID, d.Name, len(d.Items), d.Every, d.Alarms)
		}
		if b.Len() == 0 {
			return "No dashboards (SNMP Browser → Dashboards).", nil
		}
		return b.String(), nil
	case "poll_dashboard":
		id := argStr(args, "dashboard")
		for _, d := range a.SNMPDashboards() {
			if strings.EqualFold(d.Name, id) || d.ID == id {
				p := a.pollDash(ctx, d)
				var b strings.Builder
				for i, v := range p.Values {
					it := d.Items[i]
					fmt.Fprintf(&b, "%s on %s = %s", orDefault(it.Label, it.OID), it.Session, v.Display)
					if v.Alarm != "" {
						b.WriteString("  ALARM: " + v.Alarm)
					}
					if v.Error != "" {
						b.WriteString("  error: " + v.Error)
					}
					b.WriteString("\n")
				}
				return b.String(), nil
			}
		}
		return "", errors.New("no dashboard " + id)
	case "simulate":
		st, err := a.SNMPSimStart(SNMPSimIn{File: argStr(args, "file"), Port: argInt(args, "port", 0, 65535), Community: argStr(args, "community")})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Simulating %d values from %s on udp %s (community %s)", st.Values, st.Source, st.Address, st.Community), nil
	case "stop_simulator":
		a.SNMPSimStop()
		return "Stopped.", nil
	case "simulator":
		st := a.SNMPSim()
		if !st.Running {
			return "The simulator is not running.", nil
		}
		return fmt.Sprintf("Simulating %d values from %s on udp %s", st.Values, st.Source, st.Address), nil
	case "load_walk":
		w, err := a.readWalk(argStr(args, "file"))
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for i, r := range w.Rows {
			if i >= 500 {
				fmt.Fprintf(&b, "… %d more\n", len(w.Rows)-i)
				break
			}
			fmt.Fprintf(&b, "%s = %s\n", r.Name, r.Display)
		}
		return b.String(), nil
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
	if act == "send_trap" {
		in := SNMPTrapSendIn{Address: argStr(args, "address"), Version: argStr(args, "version"), Community: argStr(args, "community"), TrapOID: argStr(args, "trap")}
		if l, ok := args["values"].([]any); ok {
			for _, x := range l {
				if m, ok := x.(map[string]any); ok {
					in.Values = append(in.Values, SNMPValue{OID: argStr(m, "oid"), Type: argStr(m, "type"), Value: fmt.Sprint(m["value"])})
				}
			}
		}
		if in.Address == "" || in.TrapOID == "" {
			return "", errors.New("send_trap needs address and trap")
		}
		if err := a.SNMPSendTrap(in); err != nil {
			return "", err
		}
		return "Sent " + in.TrapOID + " to " + in.Address + ".", nil
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
	if res.Unknown > 0 {
		fmt.Fprintf(&b, "%d values are not described by the loaded MIBs", res.Unknown)
		if len(res.Suggest) > 0 {
			b.WriteString(" (most likely in " + strings.Join(res.Suggest, ", ") + ": the user can load it in SNMP → Load MIB Files)")
		}
		b.WriteString(".\n")
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
	switch name {
	case "device":
		return strings.EqualFold(argStr(args, "action"), "set_type")
	case "tests":
		act := strings.ToLower(argStr(args, "action"))
		return act == "run" || act == "save" || act == "step" || act == "schedule" || act == "unschedule" || act == "save_state" || act == "restore_state"
	case "snmp":
		return strings.EqualFold(argStr(args, "action"), "set")
	case "http":
		act := strings.ToLower(argStr(args, "action"))
		return act == "run" || act == "send" && apiclient.Changes(strings.ToUpper(argStr(args, "method")))
	}
	return false
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
	if b.Len() > 0 && slices.Contains(tools, "http") {
		b.WriteString(`For example: http {"action":"send","session":"APIs/Lab switches REST","url":"/rest/restconf/data/openconfig-interfaces:interfaces"} or http {"action":"run","collection":"EXOS RESTCONF"}. A path goes after the session's base URL and the session logs in by itself; never ask the user for tokens or passwords. Changes (POST, PUT, PATCH, DELETE) on a production session are asked. When the user wants tests or scripts, write them into the request with save (its "tests" and "preScript" fields) and run it to check them. ` + pmReference + "\n")
	}
	if b.Len() > 0 && slices.Contains(tools, "device") {
		b.WriteString(`For example: device {"action":"identify","session":"Lab/edge-sw3"} or device {"action":"refresh_facts","session":"Lab/edge-sw3"}. Use identify when a session has no device type or a wrong one, then set_type.` + "\n")
	}
	if b.Len() > 0 && slices.Contains(tools, "tests") {
		b.WriteString(`For example: tests {"action":"list"}, tests {"action":"show","file":"cases/vlan/add-vlan.yaml"} or tests {"action":"run","files":["cases/smoke.yaml"]}. To write a test, save YAML: name, steps of run {session, command}, http {session, method, url, body} or snmp {session, op: get|walk|set, oids, values: [{oid, value}]}, each with expect (contains, notContains, matches, field + equals/greater/less, json + equals/greater/less/exists, oid + equals (readable or raw value)/greater/less/exists, status, maxMs) and save {var, field|json|oid} for later {{var}}. Add tags: [smoke, l2] to group tests and dependsOn: [cases/vlan/create.yaml] to skip a test when one it needs failed; suites/<name>.yaml (name, tests: files, folders or suites, tags) are named sets to run. Steps can have wait: seconds, if: "{{x}} == y" (!=, contains, ~, >, <), retry: {timeout: 60, interval: 5} (repeat until the checks pass) a step can be traffic: {test: <saved lab traffic test>} (JSON results: json rows[0].loss less 0.01), the test can have watch: {syslog: [patterns], traps: [patterns]} (fails when they arrive while it runs) and configDiff: true (compares the configuration before and after), an upgrade: {session, image} step installs software with the device type's upgrade commands, qtest: {testCase: TC-12} links a test to qTest; send_qtest (dir: a run, default the last) sends its results to qTest; defect (file, dir) opens a Jira defect for a failed test; history (file) shows a test's past results, roles.yaml (editors, runners, approvers, approval: production|always) limits who edits and runs and which runs need an approval - approvals lists requests, request_approval (files, testbed, reason) asks for one, approve/reject (id, note) decides it, pass rate, flips (flaky) and results per software version, a step can have expected: "what should happen, in words" and can be manual: {instruction} (a person answers Passed/Failed in the Tests tab; skipped elsewhere), and a step can be a loop: {var: port, values: [..] | range: "1-8" | from: "{{list}}", steps: [...]}. Add setup: [...] and cleanup: [...] (same step format; cleanup always runs and must undo what the steps configured, so the next test finds a clean device) and restore: [config] or [lab] when the device type or emulated lab supports it. Use {{role}} for sessions and testbeds (new_testbed) to map roles to sessions; params: [{vlan: "42"}, {vlan: "7"}] runs a test once per entry. To check that an SNMP value changed, save it in one step and use notEquals "{{var}}" in a later one. Sessions are saved session paths; never put passwords in a test.` + "\n")
	}
	if b.Len() > 0 && slices.Contains(tools, "syslog") {
		b.WriteString(`For example: syslog {"action":"messages","severity":"warning","minutes":30} or syslog {"action":"messages","host":"10.127.34.33","query":"link down"}.` + "\n")
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
