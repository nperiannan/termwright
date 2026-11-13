package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/emu"
)

// The local API (Settings → AI Assistant → Local API): the emulated lab for
// scripts and test frameworks, on this computer only, with the same random key
// as the Claude connector (in mcp.json in the data folder). No change is asked
// first - the scripts are the user's own. "termwright lab …" is its command line.
//
//	GET    /api/v1/lab                      the lab (switches, cables, hosts)
//	GET    /api/v1/lab/tools                the lab tools and their arguments
//	POST   /api/v1/lab/{tool}               run a tool: {"action": …, …} → {"result": text}
//	GET    /api/v1/lab/traffic              test ports, streams and live results (JSON)
//	GET    /api/v1/lab/traffic/report       ?format=json|csv|html|text
//	POST   /api/v1/lab/traffic/ports        {switch, port, ip, ip6, gateway, vlan, groups}
//	DELETE /api/v1/lab/traffic/ports/{name}
//	POST   /api/v1/lab/traffic/streams      a stream (as in GET …/traffic); with "id" it is changed
//	DELETE /api/v1/lab/traffic/streams/{name}
//	POST   /api/v1/lab/traffic/start        {"streams": [names]} (none = every stream that is not off)
//	POST   /api/v1/lab/traffic/stop

func (a *App) apiHTTP(s *mcpServer, w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" { // never a web page
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !a.cfg.Get().AI.API {
		apiErr(w, http.StatusForbidden, errors.New("the local API is off (Settings → AI Assistant → Local API)"))
		return
	}
	defer emuAs("api")()
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
	parts := strings.Split(path, "/")
	body := map[string]any{}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &body); err != nil {
				apiErr(w, http.StatusBadRequest, fmt.Errorf("the body is not a JSON object: %w", err))
				return
			}
		}
	}
	if parts[0] != "lab" {
		apiErr(w, http.StatusNotFound, errors.New("the API is /api/v1/lab…"))
		return
	}
	key := r.Method + " " + strings.Join(parts[1:], "/")
	switch {
	case key == "GET ":
		v, err := a.EmuState()
		apiOut(w, v, err)
	case key == "GET tools":
		apiOut(w, apiTools(), nil)
	case key == "GET traffic":
		v, err := a.EmuTraffic()
		apiOut(w, v, err)
	case key == "GET traffic/report":
		f := orStr(r.URL.Query().Get("format"), "json")
		if !slices.Contains([]string{"json", "csv", "html", "text"}, f) {
			apiErr(w, http.StatusBadRequest, errors.New("format is json, csv, html or text"))
			return
		}
		b, ct, err := a.emuTrafficReportAs(f)
		if err != nil {
			apiErr(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(b)
	case key == "POST traffic/ports":
		body["action"] = "add_port"
		a.apiTool(w, "lab_traffic", body)
	case len(parts) == 4 && r.Method == http.MethodDelete && parts[1] == "traffic" && parts[2] == "ports":
		a.apiTool(w, "lab_traffic", map[string]any{"action": "remove_port", "port": parts[3]})
	case key == "POST traffic/streams":
		var st emu.Stream
		b, _ := json.Marshal(body)
		if err := json.Unmarshal(b, &st); err != nil {
			apiErr(w, http.StatusBadRequest, err)
			return
		}
		if err := a.apiStreamRefs(&st); err != nil {
			apiErr(w, http.StatusBadRequest, err)
			return
		}
		v, err := a.EmuStreamSave(st)
		apiOut(w, v, err)
	case len(parts) == 4 && r.Method == http.MethodDelete && parts[1] == "traffic" && parts[2] == "streams":
		a.apiTool(w, "lab_traffic", map[string]any{"action": "remove_stream", "name": parts[3]})
	case key == "POST traffic/start", key == "POST traffic/stop":
		body["action"] = parts[2]
		a.apiTool(w, "lab_traffic", body)
	case len(parts) == 2 && r.Method == http.MethodPost:
		a.apiTool(w, "lab_"+parts[1], body)
	default:
		apiErr(w, http.StatusNotFound, fmt.Errorf("no %s /api/v1/%s (GET /api/v1/lab/tools lists the tools)", r.Method, path))
	}
}

// apiStreamRefs lets a stream name its test ports by name (from, to) as well as by id.
func (a *App) apiStreamRefs(st *emu.Stream) error {
	tv, err := a.EmuTraffic()
	if err != nil {
		return err
	}
	find := func(n string) string {
		for _, t := range tv.Testers {
			if t.ID == n || strings.EqualFold(t.Name, n) {
				return t.ID
			}
		}
		return n
	}
	st.From, st.To = find(st.From), find(st.To)
	if st.ID != "" {
		for _, s := range tv.Streams {
			if s.Name == st.ID {
				st.ID = s.ID
			}
		}
	}
	return nil
}

func (a *App) apiTool(w http.ResponseWriter, tool string, args map[string]any) {
	if !slices.ContainsFunc(mcpLabTools, func(t mcpTool) bool { return t.Name == tool }) {
		apiErr(w, http.StatusNotFound, fmt.Errorf("no lab tool %s (GET /api/v1/lab/tools lists them)", strings.TrimPrefix(tool, "lab_")))
		return
	}
	out, err := a.emuMCPRun("api", tool, args, func(string, string, bool, bool) error { return nil })
	if err != nil {
		apiErr(w, http.StatusBadRequest, err)
		return
	}
	apiOut(w, map[string]string{"result": out}, nil)
}

// apiToolInfo is a lab tool for the API's callers.
type apiToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Args        map[string]any `json:"args"`
}

func apiTools() []apiToolInfo {
	var out []apiToolInfo
	for _, t := range mcpLabTools {
		p, _ := t.InputSchema["properties"].(map[string]any)
		out = append(out, apiToolInfo{strings.TrimPrefix(t.Name, "lab_"), t.Description, p})
	}
	return out
}

func apiOut(w http.ResponseWriter, v any, err error) {
	if err != nil {
		apiErr(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func apiErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// ---- the command line: termwright lab <tool> [action] [name=value …]

// labCLIMain is "termwright lab …": it calls the running app's local API.
func labCLIMain(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(labCLIHelp)
		return 0
	}
	dir, err := config.DataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var ep mcpEndpoint
	if b, err := os.ReadFile(mcpFile(dir)); err != nil || json.Unmarshal(b, &ep) != nil || ep.URL == "" {
		fmt.Fprintln(os.Stderr, "Termwright is not running with its local API on (Settings → AI Assistant → Local API).")
		return 2
	}
	base := strings.TrimSuffix(ep.URL, "/mcp") + "/api/v1/lab"
	method, path, body, accept := labCLIRequest(args)
	if path == "" {
		fmt.Fprintln(os.Stderr, body["error"])
		return 2
	}
	var rd io.Reader
	if method != http.MethodGet && method != http.MethodDelete {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+ep.Token)
	req.Header.Set("Content-Type", "application/json")
	timeout := 30 * time.Minute // lab wait, move and console can take long
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Termwright did not answer:", err)
		return 2
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if resp.StatusCode >= 300 {
		if json.Unmarshal(b, &m) == nil && m["error"] != nil {
			fmt.Fprintln(os.Stderr, m["error"])
		} else {
			fmt.Fprintln(os.Stderr, strings.TrimSpace(string(b)))
		}
		return 1
	}
	if accept == "raw" {
		os.Stdout.Write(b)
		return 0
	}
	if json.Unmarshal(b, &m) == nil && len(m) == 1 && m["result"] != nil {
		fmt.Println(m["result"])
		return 0
	}
	os.Stdout.Write(b)
	return 0
}

// labCLIRequest turns the command line into an API request.
func labCLIRequest(args []string) (method, path string, body map[string]any, accept string) {
	body = map[string]any{}
	tool := args[0]
	rest := args[1:]
	flags := map[string]bool{}
	var pos []string
	for _, a := range rest {
		if k, v, ok := strings.Cut(a, "="); ok && !strings.HasPrefix(a, "-") {
			body[strings.ReplaceAll(k, "-", "_")] = labCLIValue(tool, k, v)
		} else if strings.HasPrefix(a, "--") {
			flags[strings.TrimPrefix(a, "--")] = true
		} else {
			pos = append(pos, a)
		}
	}
	switch tool {
	case "show":
		return http.MethodGet, "", nil, "raw"
	case "tools":
		return http.MethodGet, "/tools", nil, "raw"
	}
	if tool == "traffic" && len(pos) > 0 {
		switch pos[0] {
		case "show":
			return http.MethodGet, "/traffic", nil, "raw"
		case "report":
			f := "text"
			for _, x := range []string{"json", "csv", "html"} {
				if flags[x] {
					f = x
				}
			}
			return http.MethodGet, "/traffic/report?format=" + f, nil, "raw"
		case "start":
			if len(pos) > 1 {
				body["streams"] = pos[1:]
			}
		}
	}
	known := false
	for _, t := range mcpLabTools {
		known = known || t.Name == "lab_"+tool
	}
	if !known {
		return "", "", map[string]any{"error": "no lab tool " + tool + " (termwright lab tools lists them)"}, ""
	}
	if len(pos) > 0 && body["action"] == nil {
		body["action"] = pos[0]
	}
	return http.MethodPost, "/" + tool, body, ""
}

// labCLIValue: name=value as the tool's argument wants it (numbers, true/false, lists split at commas, JSON as it is).
func labCLIValue(tool, k, v string) any {
	k = strings.ReplaceAll(k, "-", "_")
	typ := ""
	for _, t := range mcpLabTools {
		if t.Name == "lab_"+tool {
			if p, ok := t.InputSchema["properties"].(map[string]any); ok {
				if d, ok := p[k].(map[string]any); ok {
					typ, _ = d["type"].(string)
				}
			}
		}
	}
	if strings.HasPrefix(v, "[") || strings.HasPrefix(v, "{") {
		var x any
		if json.Unmarshal([]byte(v), &x) == nil {
			return x
		}
	}
	switch typ {
	case "integer", "number":
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	case "boolean":
		return v == "true" || v == "yes" || v == "1"
	case "array":
		var out []any
		for _, x := range strings.Split(v, ",") {
			if x = strings.TrimSpace(x); x != "" {
				out = append(out, x)
			}
		}
		return out
	}
	return v
}

var labCLIHelp = func() string {
	var b strings.Builder
	b.WriteString(`termwright lab - the emulated lab from the command line (Termwright must be running, Settings → AI Assistant → Local API on)

  termwright lab show                     the lab as JSON
  termwright lab tools                    every tool and its arguments as JSON
  termwright lab <tool> [action] [name=value …]
  termwright lab traffic report [--json|--csv|--html]
  termwright lab traffic start [stream …]

Values: numbers and true/false as they are, lists with commas (groups=239.1.1.1,239.1.1.2), JSON for the rest (mods='[{"field":"srcIp","count":100}]').

Examples:
  termwright lab power action=start switches=leaf-1,leaf-2
  termwright lab wait timeout=900
  termwright lab traffic add_port switch=leaf-1 port=5 ip=10.10.0.1/24
  termwright lab traffic add_port switch=leaf-2 port=5 ip=10.10.0.2/24 groups=239.1.1.1
  termwright lab traffic add_stream name=voice from="Test port 1" to="Test port 2" kind=udp pps=1000 seconds=30 dscp=46 size_mode=imix max_loss=0.1
  termwright lab traffic start voice
  termwright lab traffic report --csv > report.csv

Tools:
`)
	names := make([]string, 0, len(mcpLabTools))
	for _, t := range mcpLabTools {
		names = append(names, strings.TrimPrefix(t.Name, "lab_"))
	}
	sort.Strings(names)
	b.WriteString("  " + strings.Join(names, ", ") + "\n")
	return b.String()
}()
