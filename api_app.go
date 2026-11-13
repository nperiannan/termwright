package main

import (
	"bytes"
	"crypto/subtle"
	_ "embed"
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
	"sync"
	"time"

	"github.com/termwright/termwright/internal/config"
	"github.com/termwright/termwright/internal/emu"
	"gopkg.in/yaml.v3"
)

// The local API (Settings → AI Assistant → Local API): the emulated lab for
// scripts and test frameworks, on this computer only, with the same random key
// as the Claude connector (in mcp.json in the data folder). No change is asked
// first - the scripts are the user's own. "termwright lab …" is its command line.
// The OpenAPI 3.2 document (api_openapi.go, docs/api/openapi.yaml) describes it;
// errors are RFC 9457 problem details.

func (a *App) apiHTTP(s *mcpServer, w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" { // never a web page
		apiProblem(w, http.StatusForbidden, "forbidden", "Requests from web pages are refused.")
		return
	}
	path := "/" + strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1"), "/")
	if path == "/openapi.json" || path == "/openapi.yaml" { // the description is public (no secrets in it)
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			apiProblem(w, http.StatusMethodNotAllowed, "method-not-allowed", r.Method+" is not allowed on "+path+"; use GET.")
			return
		}
		b, ct := apiOpenAPIDoc(strings.HasSuffix(path, ".yaml"))
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(b)
		return
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="termwright"`)
		apiProblem(w, http.StatusUnauthorized, "unauthorized", "Send the key as Authorization: Bearer <key> (Settings → AI Assistant → Local API → Copy Key).")
		return
	}
	if !a.cfg.Get().AI.API {
		apiProblem(w, http.StatusForbidden, "api-off", "The local API is off: Settings → AI Assistant → Local API.")
		return
	}
	defer emuAs("api")()
	var body map[string]any
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		body = map[string]any{}
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &body); err != nil {
				apiProblem(w, http.StatusBadRequest, "bad-json", "The body is not a JSON object: "+err.Error())
				return
			}
		}
	}
	seg := strings.Split(strings.Trim(path, "/"), "/")
	route, arg := path, ""
	if len(seg) == 5 && seg[0] == "lab" && seg[1] == "traffic" && seg[2] == "tests" && seg[4] == "run" {
		route, arg = "/lab/traffic/tests/{id}/run", seg[3]
	} else if len(seg) == 4 && seg[0] == "lab" && seg[1] == "traffic" && seg[2] == "runs" && seg[3] == "stop" {
		route = path
	} else if len(seg) == 4 && seg[0] == "lab" && seg[1] == "traffic" && (seg[2] == "ports" || seg[2] == "streams" || seg[2] == "tests" || seg[2] == "runs") {
		route, arg = "/lab/traffic/"+seg[2]+"/{id}", seg[3]
	} else if len(seg) == 3 && seg[0] == "lab" && seg[1] == "tools" {
		route, arg = "/lab/tools/{tool}", seg[2]
	}
	ms, ok := apiRoutes[route]
	if !ok {
		apiProblem(w, http.StatusNotFound, "not-found", "No "+path+" in the API (GET /api/v1/openapi.json describes it).")
		return
	}
	if !slices.Contains(ms, r.Method) {
		w.Header().Set("Allow", strings.Join(ms, ", "))
		apiProblem(w, http.StatusMethodNotAllowed, "method-not-allowed", r.Method+" is not allowed on "+path+"; use "+strings.Join(ms, " or ")+".")
		return
	}
	switch route {
	case "/lab":
		v, err := a.EmuState()
		apiOut(w, http.StatusOK, v, err)
	case "/lab/tools":
		apiOut(w, http.StatusOK, apiTools(), nil)
	case "/lab/tools/{tool}":
		a.apiTool(w, "lab_"+arg, body)
	case "/lab/traffic":
		v, err := a.EmuTraffic()
		apiOut(w, http.StatusOK, v, err)
	case "/lab/traffic/report":
		f := orStr(r.URL.Query().Get("format"), "json")
		if !slices.Contains([]string{"json", "csv", "html", "text"}, f) {
			apiProblem(w, http.StatusBadRequest, "bad-format", "format is json, csv, html or text.")
			return
		}
		b, ct, err := a.emuTrafficReportAs(f)
		if err != nil {
			apiProblem(w, http.StatusInternalServerError, "report", err.Error())
			return
		}
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(b)
	case "/lab/traffic/start":
		v, err := a.EmuTrafficStart(a.apiStreamIDs(argStrs(body, "streams")))
		apiOut(w, http.StatusOK, v, err)
	case "/lab/traffic/stop":
		v, err := a.EmuTrafficStop()
		apiOut(w, http.StatusOK, v, err)
	case "/lab/traffic/ports":
		v, err := a.apiPortAdd(body)
		apiOut(w, http.StatusCreated, v, err)
	case "/lab/traffic/ports/{id}":
		id, ok := a.apiFind(arg, true)
		if !ok {
			apiProblem(w, http.StatusNotFound, "no-port", "No test port "+arg+".")
			return
		}
		if r.Method == http.MethodDelete {
			_, err := a.EmuTesterRemove(id)
			apiOut(w, http.StatusNoContent, nil, err)
			return
		}
		v, err := a.apiPortSet(id, body)
		apiOut(w, http.StatusOK, v, err)
	case "/lab/traffic/tests":
		if r.Method == http.MethodGet {
			apiOut(w, http.StatusOK, a.EmuTests(), nil)
			return
		}
		var t emu.TrafficTest
		if err := apiDecode(body, &t); err != nil {
			apiProblem(w, http.StatusBadRequest, "bad-test", err.Error())
			return
		}
		t.ID = ""
		a.apiTestRefs(&t)
		v, err := a.EmuTestSave(t)
		apiOut(w, http.StatusCreated, v, err)
	case "/lab/traffic/tests/{id}", "/lab/traffic/tests/{id}/run":
		id, ok := a.apiFindTest(arg)
		if !ok {
			apiProblem(w, http.StatusNotFound, "no-test", "No test "+arg+".")
			return
		}
		switch {
		case strings.HasSuffix(route, "/run"):
			run, err := a.EmuTestRun(id)
			if err == nil {
				w.Header().Set("Location", "/api/v1/lab/traffic/runs/"+run.ID)
			}
			apiOut(w, http.StatusAccepted, run, err)
		case r.Method == http.MethodDelete:
			_, err := a.EmuTestRemove(id)
			apiOut(w, http.StatusNoContent, nil, err)
		default:
			var t emu.TrafficTest
			if err := apiDecode(body, &t); err != nil {
				apiProblem(w, http.StatusBadRequest, "bad-test", err.Error())
				return
			}
			t.ID = id
			a.apiTestRefs(&t)
			v, err := a.EmuTestSave(t)
			apiOut(w, http.StatusOK, v, err)
		}
	case "/lab/traffic/runs":
		apiOut(w, http.StatusOK, a.EmuTestRuns(), nil)
	case "/lab/traffic/runs/stop":
		apiOut(w, http.StatusOK, map[string]string{"result": "stopped"}, a.EmuTestStop())
	case "/lab/traffic/runs/{id}":
		f := orStr(r.URL.Query().Get("format"), "json")
		if !slices.Contains([]string{"json", "csv", "html", "text"}, f) {
			apiProblem(w, http.StatusBadRequest, "bad-format", "format is json, csv, html or text.")
			return
		}
		if arg == "latest" {
			if rs := a.EmuTestRuns(); len(rs) > 0 {
				arg = rs[0].ID
			}
		}
		b, ct, err := a.EmuTestRunReport(arg, f)
		if err != nil {
			apiProblem(w, http.StatusNotFound, "no-run", "No run "+arg+".")
			return
		}
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(b)
	case "/lab/traffic/streams":
		var st emu.Stream
		if err := apiDecode(body, &st); err != nil {
			apiProblem(w, http.StatusBadRequest, "bad-stream", err.Error())
			return
		}
		st.ID = ""
		a.apiStreamRefs(&st)
		v, err := a.EmuStreamSave(st)
		apiOut(w, http.StatusCreated, v, err)
	case "/lab/traffic/streams/{id}":
		id, ok := a.apiFind(arg, false)
		if !ok {
			apiProblem(w, http.StatusNotFound, "no-stream", "No stream "+arg+".")
			return
		}
		if r.Method == http.MethodDelete {
			_, err := a.EmuStreamRemove(id)
			apiOut(w, http.StatusNoContent, nil, err)
			return
		}
		var st emu.Stream
		if err := apiDecode(body, &st); err != nil {
			apiProblem(w, http.StatusBadRequest, "bad-stream", err.Error())
			return
		}
		st.ID = id
		a.apiStreamRefs(&st)
		v, err := a.EmuStreamSave(st)
		apiOut(w, http.StatusOK, v, err)
	}
}

// apiRoutes: the API's paths (under /api/v1) and their methods.
var apiRoutes = map[string][]string{
	"/lab": {"GET"}, "/lab/tools": {"GET"}, "/lab/tools/{tool}": {"POST"},
	"/lab/traffic": {"GET"}, "/lab/traffic/report": {"GET"}, "/lab/traffic/start": {"POST"}, "/lab/traffic/stop": {"POST"},
	"/lab/traffic/ports": {"POST"}, "/lab/traffic/ports/{id}": {"PATCH", "DELETE"},
	"/lab/traffic/streams": {"POST"}, "/lab/traffic/streams/{id}": {"PUT", "DELETE"},
	"/lab/traffic/tests": {"GET", "POST"}, "/lab/traffic/tests/{id}": {"PUT", "DELETE"}, "/lab/traffic/tests/{id}/run": {"POST"},
	"/lab/traffic/runs": {"GET"}, "/lab/traffic/runs/{id}": {"GET"}, "/lab/traffic/runs/stop": {"POST"},
}

//go:embed docs/api/openapi.yaml
var apiOpenAPIYAML []byte

// apiOpenAPIDoc is the OpenAPI document, as YAML or JSON.
func apiOpenAPIDoc(asYAML bool) ([]byte, string) {
	if asYAML {
		return apiOpenAPIYAML, "application/yaml"
	}
	apiJSONOnce.Do(func() {
		var n yaml.Node
		if yaml.Unmarshal(apiOpenAPIYAML, &n) == nil {
			var v any
			if n.Decode(&v) == nil {
				apiJSON, _ = json.MarshalIndent(v, "", "  ")
			}
		}
	})
	return apiJSON, "application/json"
}

var (
	apiJSONOnce sync.Once
	apiJSON     []byte
)

func apiDecode(m map[string]any, v any) error {
	b, _ := json.Marshal(m)
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// apiFind finds a test port (or stream) by its id or name.
func (a *App) apiFind(n string, port bool) (string, bool) {
	tv, err := a.EmuTraffic()
	if err != nil {
		return "", false
	}
	if port {
		for _, t := range tv.Testers {
			if t.ID == n || strings.EqualFold(t.Name, n) {
				return t.ID, true
			}
		}
		return "", false
	}
	for _, s := range tv.Streams {
		if s.ID == n || strings.EqualFold(s.Name, n) {
			return s.ID, true
		}
	}
	return "", false
}

func (a *App) apiStreamIDs(names []string) []string {
	var out []string
	for _, n := range names {
		if id, ok := a.apiFind(n, false); ok {
			out = append(out, id)
		} else {
			out = append(out, n)
		}
	}
	return out
}

// apiStreamRefs lets a stream name its test ports by name (from, to) as well as by id.
func (a *App) apiStreamRefs(st *emu.Stream) {
	if id, ok := a.apiFind(st.From, true); ok {
		st.From = id
	}
	if id, ok := a.apiFind(st.To, true); ok {
		st.To = id
	}
}

func (a *App) apiFindTest(n string) (string, bool) {
	for _, t := range a.EmuTests() {
		if t.ID == n || strings.EqualFold(t.Name, n) {
			return t.ID, true
		}
	}
	return "", false
}

// apiTestRefs lets a test name its test ports by name.
func (a *App) apiTestRefs(t *emu.TrafficTest) {
	for _, p := range []*string{&t.From, &t.To, &t.Monitor} {
		if id, ok := a.apiFind(*p, true); ok {
			*p = id
		}
	}
}

func (a *App) apiPortAdd(b map[string]any) (EmuTrafficView, error) {
	v, err := a.EmuState()
	if err != nil {
		return EmuTrafficView{}, err
	}
	d, err := emuMCPID(v, argStr(b, "switch"))
	if err != nil {
		return EmuTrafficView{}, err
	}
	tv, err := a.EmuTesterAdd(d.ID, argStr(b, "port"), argStr(b, "ip"), argStr(b, "gateway"), argInt(b, "vlan", 0, 4094))
	if err != nil {
		return tv, err
	}
	t := tv.Testers[len(tv.Testers)-1]
	if ip6, gs := argStr(b, "ip6"), argStrs(b, "groups"); ip6 != "" || len(gs) > 0 || argStr(b, "name") != "" || b["lldp"] != nil {
		t.IP6, t.Groups, t.Name = ip6, gs, orStr(argStr(b, "name"), t.Name)
		if x, ok := b["lldp"].(bool); ok {
			t.LLDP = x
		}
		note := tv.Note
		tv, err = a.EmuTesterSet(t)
		tv.Note = note
	}
	return tv, err
}

func (a *App) apiPortSet(id string, b map[string]any) (EmuTrafficView, error) {
	tv, err := a.EmuTraffic()
	if err != nil {
		return tv, err
	}
	i := slices.IndexFunc(tv.Testers, func(t emu.Tester) bool { return t.ID == id })
	if i < 0 {
		return tv, errors.New("no such test port")
	}
	t := tv.Testers[i]
	for k := range b {
		switch k {
		case "name":
			t.Name = argStr(b, k)
		case "ip":
			t.IP = argStr(b, k)
		case "ip6":
			t.IP6 = argStr(b, k)
		case "gateway":
			t.GW = argStr(b, k)
		case "vlan":
			t.VLAN = argInt(b, k, 0, 4094)
		case "groups":
			t.Groups = argStrs(b, k)
		case "lldp":
			t.LLDP, _ = b[k].(bool)
		default:
			return tv, fmt.Errorf("a test port has no %q to change (name, ip, ip6, gateway, vlan, groups, lldp)", k)
		}
	}
	return a.EmuTesterSet(t)
}

func (a *App) apiTool(w http.ResponseWriter, tool string, args map[string]any) {
	if !slices.ContainsFunc(mcpLabTools, func(t mcpTool) bool { return t.Name == tool }) {
		apiProblem(w, http.StatusNotFound, "no-tool", "No lab tool "+strings.TrimPrefix(tool, "lab_")+" (GET /api/v1/lab/tools lists them).")
		return
	}
	out, err := a.emuMCPRun("api", tool, args, func(string, string, bool, bool) error { return nil })
	apiOut(w, http.StatusOK, map[string]string{"result": out}, err)
}

// apiToolInfo is a lab tool for the API's callers.
type apiToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Args        map[string]any `json:"args"`
	Required    []string       `json:"required,omitempty"`
}

func apiTools() []apiToolInfo {
	var out []apiToolInfo
	for _, t := range mcpLabTools {
		p, _ := t.InputSchema["properties"].(map[string]any)
		req, _ := t.InputSchema["required"].([]string)
		out = append(out, apiToolInfo{strings.TrimPrefix(t.Name, "lab_"), t.Description, p, req})
	}
	return out
}

func apiOut(w http.ResponseWriter, code int, v any, err error) {
	if err != nil {
		apiProblem(w, http.StatusUnprocessableEntity, "lab", err.Error())
		return
	}
	if code == http.StatusNoContent {
		w.WriteHeader(code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// apiProblem answers with an RFC 9457 problem detail.
func apiProblem(w http.ResponseWriter, code int, kind, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "https://github.com/nperiannan/termwright/blob/main/docs/API.md#" + kind, "title": http.StatusText(code), "status": code, "detail": detail})
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
	return http.MethodPost, "/tools/" + tool, body, ""
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
