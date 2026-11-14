package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/term"
)

// Termwright as an MCP server ("connector") for Claude Desktop, Claude Code
// and other MCP clients. Off until turned on in Settings → AI Assistant.
//
// The running app listens on 127.0.0.1 (a random port, a random token) and
// writes both to mcp.json in its data folder (readable by this user only).
// Claude starts "Termwright mcp" - this same program - which speaks MCP on
// stdin/stdout and passes the calls to the running app. Claude Code can also
// use the address directly (HTTP transport).
//
// The tools: list the open tabs and saved sessions, read a tab (secrets
// removed), open a session and type a command - the last two only after the
// user allows it in Termwright (or never, as set).

const mcpProtocol = "2025-06-18"

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func mcpSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var mcpTools = []mcpTool{
	{"list_tabs", "List the terminal tabs open in Termwright: id, name, device type, address and state. Use the id or the name with the other tools.", mcpSchema(map[string]any{})},
	{"read_tab", "Read what a Termwright terminal tab shows: the end of its screen and scrollback, as plain text. Passwords, keys and SNMP communities are removed.",
		mcpSchema(map[string]any{
			"tab":   map[string]any{"type": "string", "description": "the tab id or name (from list_tabs); empty = the tab the user has open"},
			"lines": map[string]any{"type": "integer", "description": "how many lines from the end (default 200, at most 3000)"},
		})},
	{"show_lab", "Show Termwright's emulated lab: its switches (model, software, state, KVM host, management address, console, the saved sessions to open), the cables between them with LAGs, and the KVM hosts with their CPU and memory. To configure a switch, open its saved session and send commands.", mcpSchema(map[string]any{})},
	{"list_sessions", "List the saved sessions (devices) in Termwright's Session Manager: path, address, protocol and device type.",
		mcpSchema(map[string]any{"filter": map[string]any{"type": "string", "description": "only sessions whose path, address or device type contains this"}})},
	{"open_session", "Connect to a saved session in a new Termwright tab. The user is asked in Termwright first.",
		mcpSchema(map[string]any{"session": map[string]any{"type": "string", "description": "the session path from list_sessions, e.g. Datacenter/Core/core-sw1"}}, "session")},
	{"send_command", "Type a command (and Enter) in a Termwright terminal tab and return the output that follows. The user is asked in Termwright first, and may refuse. Turn off paging on the device first (EXOS: disable clipaging, VOSS: terminal more disable, Cisco: terminal length 0). Prefer show commands; never change the configuration unless the user asked for it.",
		mcpSchema(map[string]any{
			"tab":     map[string]any{"type": "string", "description": "the tab id or name"},
			"command": map[string]any{"type": "string", "description": "one command line"},
			"wait":    map[string]any{"type": "integer", "description": "seconds to wait for the output to settle (default 10, at most 120)"},
		}, "tab", "command")},
}

const mcpInstructions = "Termwright is the user's terminal for network devices (switches, routers, firewalls) and servers. " +
	"Start with list_tabs and read_tab to see what the user is looking at. send_command and open_session need the user's approval in Termwright each time. " +
	"Its emulated lab runs real switch images on KVM hosts: show_lab and lab_images to see it; for a test case build the topology (lab_add_switches, lab_cable), start it (lab_power) and lab_wait, " +
	"configure and verify with lab_console (the switches' own CLI), check the cabling with lab_check_cabling, add a server (HTTP, FTP, TFTP, DHCP, SFTP) and put files on it with lab_server, put ESP32 microcontrollers in the lab (on this computer too) with lab_microcontrollers and type on their MicroPython console with lab_console, set up Linux computers (addresses, gateway, DNS) and run ping, traceroute or a web request on them with lab_computer, limit or share the hosts' CPU with lab_cpu, run traffic between test ports with lab_traffic (a pass/fail report) and capture a port's packets with lab_capture to verify it, and read lab_events and lab_debug (state, Events and console of one switch) when something fails (lab_link_check and lab_qemu_command for link flaps). Each change is approved by the user in Termwright."

// ---- the server in the app ----

type mcpServer struct {
	srv   *http.Server
	url   string
	token string
}

var (
	mcpMu     sync.Mutex
	mcpCur    *mcpServer
	mcpAllow  = map[string]time.Time{} // tab id → typing allowed without asking until
	mcpTexts  sync.Map                 // text request id → chan mcpText
	mcpTextID atomic.Int64
)

type mcpText struct {
	Text  string `json:"text"`
	Total int    `json:"total"`
}

// mcpFile is where the running app tells the bridge how to reach it.
func mcpFile(dataDir string) string { return filepath.Join(dataDir, "mcp.json") }

type mcpEndpoint struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

// mcpApply starts or stops the server to match the settings.
func (a *App) mcpApply() {
	on := a.cfg.Get().AI.MCP || a.cfg.Get().AI.API
	mcpMu.Lock()
	defer mcpMu.Unlock()
	if on == (mcpCur != nil) {
		return
	}
	if !on {
		mcpCur.srv.Close()
		mcpCur = nil
		_ = os.WriteFile(mcpFile(a.cfg.DataDir()), []byte(`{"stopped":true}`), 0o600)
		return
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	s := &mcpServer{token: hex.EncodeToString(b), url: "http://" + ln.Addr().String() + "/mcp"}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) { a.mcpHTTP(s, w, r) })
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { a.apiHTTP(s, w, r) })
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go s.srv.Serve(ln)
	mcpCur = s
	j, _ := json.Marshal(mcpEndpoint{URL: s.url, Token: s.token, PID: os.Getpid()})
	_ = os.WriteFile(mcpFile(a.cfg.DataDir()), j, 0o600)
}

func (a *App) mcpStop() {
	mcpMu.Lock()
	defer mcpMu.Unlock()
	if mcpCur != nil {
		mcpCur.srv.Close()
		mcpCur = nil
		_ = os.WriteFile(mcpFile(a.cfg.DataDir()), []byte(`{"stopped":true}`), 0o600)
	}
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

func (a *App) mcpHTTP(s *mcpServer, w http.ResponseWriter, r *http.Request) {
	// only local programs with the token; never a web page (DNS rebinding)
	if r.Header.Get("Origin") != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !a.cfg.Get().AI.MCP {
		http.Error(w, "the Claude connector is off (Settings → AI Assistant)", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST JSON-RPC messages here", http.StatusMethodNotAllowed)
		return
	}
	var req rpcReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error"}})
		return
	}
	resp, ok := a.mcpHandle(r.Context(), req)
	if !ok { // a notification
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// mcpStatic answers what needs no running app (the bridge answers these itself too).
func mcpStatic(req rpcReq) (rpcResp, bool) {
	resp := rpcResp{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := p.ProtocolVersion
		if v == "" {
			v = mcpProtocol
		}
		resp.Result = map[string]any{"protocolVersion": v, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{"name": "termwright", "title": "Termwright", "version": Version}, "instructions": mcpInstructions}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": append(append([]mcpTool(nil), mcpTools...), mcpLabTools...)}
	default:
		return resp, false
	}
	return resp, true
}

func (a *App) mcpHandle(ctx context.Context, req rpcReq) (rpcResp, bool) {
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return rpcResp{}, false // notifications/initialized and friends
	}
	if r, ok := mcpStatic(req); ok {
		return r, true
	}
	resp := rpcResp{JSONRPC: "2.0", ID: req.ID}
	if req.Method != "tools/call" {
		resp.Error = &rpcErr{-32601, "method not found: " + req.Method}
		return resp, true
	}
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		resp.Error = &rpcErr{-32602, "bad params"}
		return resp, true
	}
	text, err := a.mcpCall(ctx, p.Name, p.Arguments)
	if err != nil {
		resp.Result = map[string]any{"content": []map[string]string{{"type": "text", "text": err.Error()}}, "isError": true}
	} else {
		resp.Result = map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}
	}
	return resp, true
}

func argStr(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func argInt(m map[string]any, k string, def, max int) int {
	n := def
	if v, ok := m[k].(float64); ok && v > 0 {
		n = int(v)
	}
	if n > max {
		n = max
	}
	return n
}

func (a *App) mcpTabs() []term.TabInfo {
	if a.terms == nil {
		return nil
	}
	l := a.terms.List()
	sort.Slice(l, func(i, j int) bool { return l[i].Connected.Before(l[j].Connected) })
	return l
}

// mcpTab finds a tab by id or name ("" = the one the user has open).
func (a *App) mcpTab(want string) (term.TabInfo, error) {
	tabs := a.mcpTabs()
	if len(tabs) == 0 {
		return term.TabInfo{}, errors.New("no terminal tab is open in Termwright")
	}
	if want == "" {
		if id := a.activeTab(); id != "" {
			for _, t := range tabs {
				if t.ID == id {
					return t, nil
				}
			}
		}
		if len(tabs) == 1 {
			return tabs[0], nil
		}
		return term.TabInfo{}, errors.New("several tabs are open - name one (list_tabs)")
	}
	var hit []term.TabInfo
	for _, t := range tabs {
		if t.ID == want {
			return t, nil
		}
		if strings.EqualFold(t.Title, want) || strings.EqualFold(t.Session.Path, want) {
			hit = append(hit, t)
		}
	}
	switch len(hit) {
	case 1:
		return hit[0], nil
	case 0:
		return term.TabInfo{}, fmt.Errorf("no open tab is called %q (list_tabs)", want)
	}
	return term.TabInfo{}, fmt.Errorf("several tabs are called %q - use the id from list_tabs", want)
}

// activeTab asks the window which tab the user has open.
func (a *App) activeTab() string {
	t, ok := a.tabText("", 0)
	if !ok {
		return ""
	}
	return t.Text
}

// tabText asks the terminal view for the last lines of a tab ("" tab with 0
// lines: the id of the open tab).
func (a *App) tabText(tabID string, lines int) (mcpText, bool) {
	if a.ctx == nil {
		return mcpText{}, false
	}
	id := fmt.Sprintf("x%d", mcpTextID.Add(1))
	ch := make(chan mcpText, 1)
	mcpTexts.Store(id, ch)
	defer mcpTexts.Delete(id)
	emitter{a}.Emit("text:request", map[string]any{"id": id, "tabId": tabID, "lines": lines})
	select {
	case t := <-ch:
		return t, true
	case <-time.After(2 * time.Second):
		return mcpText{}, false
	}
}

// TextReply is the terminal view's answer to a text request.
func (a *App) TextReply(id, text string, total int) {
	if v, ok := mcpTexts.Load(id); ok {
		select {
		case v.(chan mcpText) <- mcpText{Text: text, Total: total}:
		default:
		}
	}
}

var mcpRisky = regexp.MustCompile(`(?i)^\s*(conf(ig(ure)?)?\b|configure|unconfigure|delete|del\b|erase|clear\b|reboot|reload|restart|shutdown|no\s|write\b|copy\b|save\b|disable\b|enable\s+(port|ports)\b|format|rm\s|kill|halt|poweroff|set\s|unset\s|download|upload|install|upgrade|reset)`)

func (a *App) mcpAsk(tab, title, msg string, danger bool, allowTab bool) error {
	if a.cfg.Get().AI.MCPCommands == "never" {
		return errors.New("the user does not let Claude type in Termwright (Settings → AI Assistant) - tell the user the command instead")
	}
	if allowTab {
		mcpMu.Lock()
		until, ok := mcpAllow[tab]
		mcpMu.Unlock()
		if ok && time.Now().Before(until) && !danger {
			return nil
		}
	}
	choices := []string{"Allow"}
	if allowTab && !danger {
		choices = append(choices, "Allow in This Tab for 15 Minutes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	r, err := a.terms.Prompts().Ask(ctx, term.Prompt{TabID: tab, Kind: "confirm", Title: title, Message: msg, Choices: choices, Danger: danger})
	if err != nil {
		return errors.New("the user did not allow it")
	}
	if strings.HasPrefix(r.Choice, "Allow in This Tab") {
		mcpMu.Lock()
		mcpAllow[tab] = time.Now().Add(15 * time.Minute)
		mcpMu.Unlock()
	}
	return nil
}

func (a *App) mcpCall(_ context.Context, name string, args map[string]any) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	switch name {
	case "list_tabs":
		tabs := a.mcpTabs()
		if len(tabs) == 0 {
			return "No terminal tab is open in Termwright.", nil
		}
		active := a.activeTab()
		var b strings.Builder
		for _, t := range tabs {
			dt := t.Session.DeviceType
			if dt == "" {
				dt = "-"
			}
			mark := ""
			if t.ID == active {
				mark = "  (the tab the user has open)"
			}
			fmt.Fprintf(&b, "%s · %s · %s · %s %s · %s%s\n", t.ID, t.Title, dt, t.Session.Protocol, t.Session.Hostname, t.State, mark)
		}
		return b.String(), nil
	case "read_tab":
		t, err := a.mcpTab(argStr(args, "tab"))
		if err != nil {
			return "", err
		}
		n := argInt(args, "lines", 200, 3000)
		txt, ok := a.tabText(t.ID, n)
		if !ok {
			return "", errors.New("Termwright's window did not answer - is it open?")
		}
		out, _ := aiRedact(txt.Text, false)
		return fmt.Sprintf("%s (%s, %s) - the last %d of %d lines:\n%s", t.Title, orDash(t.Session.DeviceType), t.State, min(n, txt.Total), txt.Total, out), nil
	case "show_lab":
		out, _ := aiRedact(a.EmuAgentText(), false)
		return out, nil
	case "list_sessions":
		f := strings.ToLower(argStr(args, "filter"))
		paths, _ := a.store.AllSessions(nil)
		var b strings.Builder
		n := 0
		for _, p := range paths {
			s, err := a.store.Get(p)
			if err != nil {
				continue
			}
			line := fmt.Sprintf("%s · %s %s · %s", s.Path, s.Protocol, s.Hostname, orDash(s.DeviceType))
			if f != "" && !strings.Contains(strings.ToLower(line), f) {
				continue
			}
			b.WriteString(line + "\n")
			if n++; n >= 300 {
				b.WriteString("… more - use a filter\n")
				break
			}
		}
		if n == 0 {
			return "No saved session matches.", nil
		}
		return b.String(), nil
	case "open_session":
		p := argStr(args, "session")
		s, err := a.store.Get(p)
		if err != nil {
			return "", fmt.Errorf("no saved session %q (list_sessions)", p)
		}
		switch {
		case a.mcpLabPath(s.Path): // a device of Termwright's own lab: not asked
		case s.Environment == "production":
			if err := a.mcpAsk("", "Claude wants to connect to a PRODUCTION device", "Claude (through the Termwright connector) asks to open "+s.Path+" ("+s.Hostname+").\n\nThis session is marked production.", true, false); err != nil {
				return "", err
			}
		default:
			if err := a.mcpAsk("", "Claude wants to connect", "Claude (through the Termwright connector) asks to open "+s.Path+" ("+s.Hostname+").", false, false); err != nil {
				return "", err
			}
		}
		emitter{a}.Emit("mcp:open", s.Path)
		return "Opening " + s.Path + " in a new tab - call list_tabs in a few seconds, then read_tab.", nil
	case "send_command":
		t, err := a.mcpTab(argStr(args, "tab"))
		if err != nil {
			return "", err
		}
		cmd := strings.TrimRight(argStr(args, "command"), "\r\n")
		if cmd == "" || strings.ContainsAny(cmd, "\r\n") {
			return "", errors.New("give one command line")
		}
		if t.State != "connected" {
			return "", fmt.Errorf("the tab %s is %s, not connected", t.Title, t.State)
		}
		danger := mcpRisky.MatchString(cmd)
		msg := "Claude (through the Termwright connector) asks to type in " + t.Title + ":\n\n    " + cmd + "\n"
		if danger {
			msg += "\nThis command may change the device or interrupt traffic."
		}
		lab := a.mcpLabPath(t.Session.Path)
		switch {
		case lab: // a device of Termwright's own lab: not asked (automated tests), kept in the change history
			if a.cfg.Get().AI.MCPCommands == "never" {
				return "", errors.New("the user does not let Claude type in Termwright (Settings → AI Assistant) - tell the user the command instead")
			}
		case t.Session.Environment == "production": // always asked, every command, never "for 15 minutes"
			if err := a.mcpAsk(t.ID, "Claude wants to run a command on a PRODUCTION device", msg+"\nThis session is marked production.", true, false); err != nil {
				return "", err
			}
		default:
			if err := a.mcpAsk(t.ID, "Claude wants to run a command", msg, danger, true); err != nil {
				return "", err
			}
		}
		if lab {
			id, ck, note := randHex(4), map[string]string{}, "commands typed on a console are not undone"
			if vm := emuVMOfSession(t.Session.Path); vm != "" {
				tag, why := a.emuAutoCheckpoint(vm, "Claude typed `"+cmd+"`", id)
				if tag != "" {
					ck[vm], note = tag, ""
				} else if why != "" {
					note = "not undone: " + why
				}
			}
			a.emuChangeNoteCk(id, "ai", "send_command", "Typed `"+cmd+"` on "+t.Title, "ok", note, ck)
		}
		before, _ := a.tabText(t.ID, 1)
		if err := a.terms.Write(t.ID, []byte(cmd+"\r")); err != nil {
			return "", err
		}
		out := a.mcpSettle(t.ID, before.Total, time.Duration(argInt(args, "wait", 10, 120))*time.Second)
		red, _ := aiRedact(out, false)
		note := ""
		if regexp.MustCompile(`(?i)(--more--|press any key|<--- more --->|q to quit)\s*$`).MatchString(strings.TrimSpace(out)) {
			note = "\n[the device is paging - turn paging off and run it again]"
		}
		return red + note, nil
	}
	if strings.HasPrefix(name, "lab_") {
		return a.emuMCP(name, args)
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

// mcpSettle waits until the tab's output stops changing (or wait passes) and
// returns what came after the line the command was typed on.
func (a *App) mcpSettle(tab string, before int, wait time.Duration) string {
	start, last, lastChange := time.Now(), "", time.Now()
	var cur mcpText
	for time.Since(start) < wait {
		time.Sleep(400 * time.Millisecond)
		t, ok := a.tabText(tab, 3000)
		if !ok {
			break
		}
		cur = t
		if t.Text != last {
			last, lastChange = t.Text, time.Now()
			continue
		}
		if time.Since(lastChange) > 1500*time.Millisecond && time.Since(start) > time.Second {
			break
		}
	}
	lines := strings.Split(cur.Text, "\n")
	// the lines past the old end, counted from the end (the scrollback may have dropped old lines)
	newN := cur.Total - before + 1
	if newN < 1 {
		newN = 1
	}
	if newN > len(lines) {
		newN = len(lines)
	}
	return strings.Join(lines[len(lines)-newN:], "\n")
}

// ---- set up Claude ----

// MCPInfo is what the settings page shows about the connector.
type MCPInfo struct {
	Running    bool   `json:"running"`
	Command    string `json:"command"`    // this program, for Claude's settings
	CodeCmd    string `json:"codeCmd"`    // the claude mcp add command
	DesktopCfg string `json:"desktopCfg"` // Claude Desktop's settings file
	InDesktop  bool   `json:"inDesktop"`
	InCode     bool   `json:"inCode"`            // Claude Code has it (user scope, in its .claude.json)
	APIURL     string `json:"apiUrl,omitempty"`  // the local API, when it runs
	KeyFile    string `json:"keyFile,omitempty"` // where its key is (mcp.json: url and token)
}

func claudeDesktopConfig() string {
	home, _ := os.UserHomeDir()
	switch {
	case os.Getenv("APPDATA") != "" && filepath.Separator == '\\':
		return filepath.Join(os.Getenv("APPDATA"), "Claude", "claude_desktop_config.json")
	case fileExists(filepath.Join(home, "Library")):
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	}
	return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// MCPState describes the connector for the settings page.
func (a *App) MCPState() MCPInfo {
	exe, _ := os.Executable()
	mcpMu.Lock()
	running := mcpCur != nil
	api := ""
	if running {
		api = strings.TrimSuffix(mcpCur.url, "/mcp") + "/api/v1/lab"
	}
	mcpMu.Unlock()
	info := MCPInfo{Running: running, Command: exe, DesktopCfg: claudeDesktopConfig(), APIURL: api}
	if a.cfg != nil {
		info.KeyFile = mcpFile(a.cfg.DataDir())
	}
	info.CodeCmd = "claude mcp add --scope user termwright -- " + shellQuoteArg(exe) + " mcp"
	info.InCode = claudeCodeHas(os.Getenv("CLAUDE_CONFIG_DIR"))
	if b, err := os.ReadFile(info.DesktopCfg); err == nil {
		var c struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if json.Unmarshal(b, &c) == nil {
			_, info.InDesktop = c.MCPServers["termwright"]
		}
	}
	return info
}

// claudeCodeHas says whether Claude Code has a "termwright" connector for the user: its
// .claude.json (in the home folder, or in CLAUDE_CONFIG_DIR) lists it under mcpServers.
func claudeCodeHas(configDir string) bool {
	home, _ := os.UserHomeDir()
	files := []string{filepath.Join(home, ".claude.json"), filepath.Join(home, ".claude", ".claude.json")}
	if configDir != "" {
		files = []string{filepath.Join(configDir, ".claude.json")}
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var c struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if json.Unmarshal(b, &c) == nil {
			if _, ok := c.MCPServers["termwright"]; ok {
				return true
			}
		}
	}
	return false
}

// APIKey is the local API's key, for Settings to copy to the clipboard (it is never shown).
func (a *App) APIKey() string {
	mcpMu.Lock()
	defer mcpMu.Unlock()
	if mcpCur == nil {
		return ""
	}
	return mcpCur.token
}

func shellQuoteArg(s string) string {
	if !strings.ContainsAny(s, " '\"$`\\()&;") {
		return s
	}
	if filepath.Separator == '\\' {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// MCPAddToDesktop adds Termwright to Claude Desktop's settings file (a copy
// of the old file is kept next to it). Claude Desktop must be restarted.
func (a *App) MCPAddToDesktop() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	p := claudeDesktopConfig()
	cfg := map[string]any{}
	if b, err := os.ReadFile(p); err == nil && len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("Claude Desktop's settings file cannot be read (%v) - add Termwright by hand", err)
		}
		_ = os.WriteFile(p+".termwright-backup", b, 0o600)
	} else if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["termwright"] = map[string]any{"command": exe, "args": []string{"mcp"}}
	cfg["mcpServers"] = servers
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// MCPAddToClaudeCode runs "claude mcp add" for the user.
func (a *App) MCPAddToClaudeCode() (string, error) {
	bin, err := claudeCodePath(a.cfg.Get().AI.BaseURL)
	if err != nil && a.cfg.Get().AI.Provider == "claudecode" {
		return "", err
	}
	if err != nil {
		if bin, err = claudeCodePath(""); err != nil {
			return "", err
		}
	}
	exe, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = runQuiet(ctx, bin, "mcp", "remove", "--scope", "user", "termwright")
	out, err := runOutput(ctx, bin, "mcp", "add", "--scope", "user", "termwright", "--", exe, "mcp")
	if err != nil {
		return "", fmt.Errorf("claude mcp add: %s", strings.TrimSpace(out))
	}
	return strings.TrimSpace(out), nil
}

// ---- the bridge: "Termwright mcp" ----

// runMCPBridge speaks MCP on stdin/stdout for Claude and passes tool calls to
// the running Termwright. It answers initialize and tools/list itself, so
// Claude sees the tools also while Termwright is closed.
func runMCPBridge(in io.Reader, out io.Writer, dataDir string) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	enc := json.NewEncoder(out)
	hc := &http.Client{Timeout: 3 * time.Minute}
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error"}})
			continue
		}
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue // notification
		}
		if req.Method == "tools/list" { // the running Termwright's own list when it answers (this program may be an older copy)
			if resp, err := mcpForward(&http.Client{Timeout: 5 * time.Second}, dataDir, line); err == nil && bytes.Contains(resp, []byte(`"tools"`)) {
				if _, err := out.Write(append(bytes.TrimSpace(resp), '\n')); err != nil {
					return err
				}
				continue
			}
		}
		if r, ok := mcpStatic(req); ok {
			_ = enc.Encode(r)
			continue
		}
		resp, err := mcpForward(hc, dataDir, line)
		if err != nil {
			msg := "Termwright is not running, or its Claude connector is off - open Termwright and turn on Settings → AI Assistant → Use Termwright from Claude. (" + err.Error() + ")"
			_ = enc.Encode(rpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"content": []map[string]string{{"type": "text", "text": msg}}, "isError": true}})
			continue
		}
		if _, err := out.Write(append(bytes.TrimSpace(resp), '\n')); err != nil {
			return err
		}
	}
	return sc.Err()
}

func mcpForward(hc *http.Client, dataDir string, body []byte) ([]byte, error) {
	b, err := os.ReadFile(mcpFile(dataDir))
	if err != nil {
		return nil, errors.New("not started")
	}
	var ep mcpEndpoint
	if json.Unmarshal(b, &ep) != nil || ep.URL == "" {
		return nil, errors.New("not started")
	}
	req, _ := http.NewRequest(http.MethodPost, ep.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ep.Token)
	r, err := hc.Do(req)
	if err != nil {
		return nil, errors.New("cannot reach it")
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", r.StatusCode)
	}
	return io.ReadAll(io.LimitReader(r.Body, 16<<20))
}

// mcpBridgeMain is "Termwright mcp".
func mcpBridgeMain() int {
	dir, err := config.DataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := runMCPBridge(os.Stdin, os.Stdout, dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
