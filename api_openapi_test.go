package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/termwright/termwright/internal/emu"
)

// The local API's OpenAPI 3.2.1 document, docs/api/openapi.yaml, is made here from
// the Go types the API sends and takes (their field comments are the descriptions)
// and the lab tools' own schemas, so it cannot drift from the code:
//
//	go test -run TestOpenAPIDoc -update   writes it
//	go test -run TestOpenAPIDoc           fails when it is not up to date

var updateOpenAPI = flag.Bool("update", false, "write docs/api/openapi.yaml")

// om is a JSON / YAML object that keeps its keys in order.
type om []kv
type kv struct {
	k string
	v any
}

func (o om) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, x := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(x.k)
		v, err := json.Marshal(x.v)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (o om) MarshalYAML() (any, error) {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, x := range o {
		var v yaml.Node
		if err := v.Encode(x.v); err != nil {
			return nil, err
		}
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: x.k}, &v)
	}
	return n, nil
}

// fieldDocs reads the field comments of the API's Go types: "Type.jsonName" → text.
func fieldDocs(t *testing.T) map[string]string {
	out := map[string]string{}
	for _, dir := range []string{".", "internal/emu"} {
		fs := token.NewFileSet()
		pkgs, err := parser.ParseDir(fs, dir, func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		pre := ""
		if dir != "." {
			pre = "emu."
		}
		for _, p := range pkgs {
			for _, f := range p.Files {
				ast.Inspect(f, func(n ast.Node) bool {
					ts, ok := n.(*ast.TypeSpec)
					if !ok {
						return true
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						return true
					}
					for _, fl := range st.Fields.List {
						if fl.Tag == nil || len(fl.Names) == 0 {
							continue
						}
						tag := reflect.StructTag(strings.Trim(fl.Tag.Value, "`")).Get("json")
						name := strings.Split(tag, ",")[0]
						if name == "" || name == "-" {
							continue
						}
						c := ""
						if fl.Comment != nil {
							c = fl.Comment.Text()
						} else if fl.Doc != nil {
							c = fl.Doc.Text()
						}
						if c = strings.TrimSpace(strings.Join(strings.Fields(c), " ")); c != "" {
							out[pre+ts.Name.Name+"."+name] = c
						}
					}
					return true
				})
			}
		}
	}
	return out
}

// The component names the document uses for Go types.
var oasNames = map[string]string{
	"main.EmuView": "Lab", "main.EmuDevice": "Device", "main.EmuLinkView": "Cable", "main.EmuEvent": "Event",
	"main.EmuTrafficView": "TrafficView", "main.EmuStreamResult": "StreamResult", "emu.Tester": "TestPort", "emu.Stream": "Stream", "emu.Mod": "FieldChange",
	"main.apiToolInfo": "Tool",
}

type schemaGen struct {
	docs  map[string]string
	comps map[string]om
	order []string
}

func (g *schemaGen) name(t reflect.Type) string {
	full := t.String()
	if n, ok := oasNames[full]; ok {
		return n
	}
	n := t.Name()
	if strings.HasPrefix(full, "emu.") {
		return "Emu" + n
	}
	return strings.TrimPrefix(n, "Emu")
}

func (g *schemaGen) schema(t reflect.Type) any {
	switch t.Kind() {
	case reflect.Pointer:
		return om{{"anyOf", []any{g.schema(t.Elem()), om{{"type", "null"}}}}}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return om{{"type", "string"}, {"contentEncoding", "base64"}}
		}
		return om{{"type", []string{"array", "null"}}, {"items", g.schema(t.Elem())}}
	case reflect.Array:
		return om{{"type", "array"}, {"items", g.schema(t.Elem())}, {"minItems", t.Len()}, {"maxItems", t.Len()}}
	case reflect.Map:
		return om{{"type", []string{"object", "null"}}, {"additionalProperties", g.schema(t.Elem())}}
	case reflect.String:
		return om{{"type", "string"}}
	case reflect.Bool:
		return om{{"type", "boolean"}}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return om{{"type", "integer"}}
	case reflect.Float32, reflect.Float64:
		return om{{"type", "number"}}
	case reflect.Interface:
		return om{}
	case reflect.Struct:
		if t == reflect.TypeOf(time.Time{}) {
			return om{{"type", "string"}, {"format", "date-time"}}
		}
		if t == reflect.TypeOf(json.RawMessage{}) {
			return om{}
		}
		n := g.name(t)
		if _, ok := g.comps[n]; !ok {
			g.comps[n] = nil // being made (a type that holds itself)
			g.comps[n] = g.object(t)
			g.order = append(g.order, n)
		}
		return om{{"$ref", "#/components/schemas/" + n}}
	}
	return om{}
}

func (g *schemaGen) object(t reflect.Type) om {
	key := t.String()
	if strings.HasPrefix(key, "main.") {
		key = strings.TrimPrefix(key, "main.")
	}
	props := om{}
	var req []string
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				walk(f.Type)
				continue
			}
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			name := tag[0]
			if name == "" {
				name = f.Name
			}
			s := g.schema(f.Type)
			tn := t.String()
			if strings.HasPrefix(tn, "main.") {
				tn = strings.TrimPrefix(tn, "main.")
			}
			if d := g.docs[tn+"."+name]; d != "" {
				if so, ok := s.(om); ok {
					if len(so) == 1 && so[0].k == "$ref" { // a $ref with a description beside it (allowed in 3.1+)
						s = append(so, kv{"description", d})
					} else {
						s = append(om{{"description", d}}, so...)
					}
				}
			}
			props = append(props, kv{name, s})
			if !slices.Contains(tag[1:], "omitempty") {
				req = append(req, name)
			}
		}
	}
	walk(t)
	o := om{{"type", "object"}}
	if d := oasTypeDocs[g.name(t)]; d != "" {
		o = append(o, kv{"description", d})
	}
	o = append(o, kv{"properties", props})
	if len(req) > 0 {
		o = append(o, kv{"required", req})
	}
	return o
}

var oasTypeDocs = map[string]string{
	"Lab":          "The emulated lab as Termwright shows it: its switches, cables, KVM hosts, images and Events. The open topology only; topologies in the background are not in it.",
	"Device":       "A switch (VM) of the lab and its state.",
	"Cable":        "A cable between two switch ports, with the LAGs on its ends.",
	"Event":        "A line of the lab's Events log.",
	"TrafficView":  "The traffic generator: its test ports, streams and their results now (live while a run goes on).",
	"TestPort":     "A test port: a sender and counter on a free switch port (no VM). It answers ARP, neighbour solicitations and ping at its addresses.",
	"Stream":       "Frames from one test port to another, to every other one (broadcast), to an address or to a multicast group.",
	"FieldChange":  "A field of a stream's frames that changes from frame to frame: its value plus 0, step, 2·step… (count values, then again), or one of them at random.",
	"StreamResult": "A stream's counters, added up over every KVM host, and its verdict.",
	"Tool":         "A lab tool: the same tools the Claude connector and the in-app AI agent use.",
}

func TestOpenAPIDoc(t *testing.T) {
	doc := openAPIDoc(t)
	y, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	head := "# The Termwright local API, OpenAPI 3.2.1. Made by: go test -run TestOpenAPIDoc -update (do not edit by hand).\n"
	out := append([]byte(head), y...)
	path := filepath.Join("docs", "api", "openapi.yaml")
	if *updateOpenAPI {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, out, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	cur, _ := os.ReadFile(path)
	if !bytes.Equal(cur, out) {
		t.Fatal("docs/api/openapi.yaml is not up to date: go test -run TestOpenAPIDoc -update")
	}
	// the API's own routes are all in it, and nothing else
	var d struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(out, &d); err != nil {
		t.Fatal(err)
	}
	for _, tl := range mcpLabTools {
		if d.Paths["/lab/tools/"+strings.TrimPrefix(tl.Name, "lab_")]["post"] == nil {
			t.Fatalf("no path for the tool %s", tl.Name)
		}
	}
	for route, ms := range apiRoutes {
		if strings.Contains(route, "{tool}") {
			continue
		}
		p := strings.NewReplacer("{id}", map[bool]string{true: "{port}", false: "{stream}"}[strings.Contains(route, "ports")]).Replace(route)
		for _, m := range ms {
			if d.Paths[p][strings.ToLower(m)] == nil {
				t.Fatalf("%s %s is not in the document", m, p)
			}
		}
	}
	// every field of what the traffic API sends and takes is described
	g := &schemaGen{docs: fieldDocs(t), comps: map[string]om{}}
	for _, ty := range []reflect.Type{reflect.TypeOf(emu.Stream{}), reflect.TypeOf(emu.Tester{}), reflect.TypeOf(emu.Mod{}), reflect.TypeOf(EmuStreamResult{}), reflect.TypeOf(EmuTrafficView{})} {
		tn := strings.TrimPrefix(ty.String(), "main.")
		for i := 0; i < ty.NumField(); i++ {
			name := strings.Split(ty.Field(i).Tag.Get("json"), ",")[0]
			if name != "" && name != "-" && g.docs[tn+"."+name] == "" {
				t.Errorf("%s.%s has no comment: the document describes fields from them", tn, name)
			}
		}
	}
}

func problemRef(desc string) om {
	return om{{"description", desc}, {"content", om{{"application/problem+json", om{{"schema", om{{"$ref", "#/components/schemas/Problem"}}}}}}}}
}

func jsonBody(ref string, examples om) om {
	c := om{{"schema", om{{"$ref", "#/components/schemas/" + ref}}}}
	if examples != nil {
		c = append(c, kv{"examples", examples})
	}
	return om{{"content", om{{"application/json", c}}}}
}

func resp(desc, ref string) om {
	r := om{{"description", desc}}
	if ref != "" {
		r = append(r, kv{"content", om{{"application/json", om{{"schema", om{{"$ref", "#/components/schemas/" + ref}}}}}}})
	}
	return r
}

func errs(codes ...string) []kv {
	var out []kv
	for _, c := range codes {
		out = append(out, kv{c, om{{"$ref", "#/components/responses/" + map[string]string{"400": "BadRequest", "401": "Unauthorized", "403": "Forbidden", "404": "NotFound", "405": "MethodNotAllowed", "422": "Unprocessable"}[c]}}})
	}
	return out
}

func op(id, tag, summary, desc string, extra ...kv) om {
	o := om{{"operationId", id}, {"tags", []string{tag}}, {"summary", summary}, {"description", desc}}
	return append(o, extra...)
}

func openAPIDoc(t *testing.T) om {
	g := &schemaGen{docs: fieldDocs(t), comps: map[string]om{}}
	for _, ty := range []any{EmuView{}, EmuTrafficView{}, emu.Stream{}, emu.Tester{}, apiToolInfo{}} {
		g.schema(reflect.TypeOf(ty))
	}
	std := append([]kv{}, errs("401", "403", "405")...)
	ex := func(summary string, v any) om { return om{{"summary", summary}, {"value", v}} }
	streamEx := om{
		{"voice", ex("Voice-like UDP with DSCP EF", om{{"name", "voice"}, {"from", "Test port 1"}, {"to", "Test port 2"}, {"kind", "udp"}, {"size", 200}, {"pps", 1000}, {"seconds", 30}, {"dscp", 46}, {"maxLoss", 0.1}, {"maxLatency", 5}, {"maxJitter", 1}})},
		{"imixFlows", ex("IMIX with 100 source addresses (for LAG / ECMP hashing)", om{{"name", "imix-100"}, {"from", "Test port 1"}, {"to", "Test port 2"}, {"kind", "udp"}, {"size", 64}, {"sizeMode", "imix"}, {"pps", 5000}, {"seconds", 60}, {"maxLoss", 0}, {"mods", []om{{{"field", "srcIp"}, {"mode", "inc"}, {"count", 100}}}}})},
		{"multicast", ex("Multicast to the test ports that joined 239.1.1.1", om{{"name", "mcast"}, {"from", "Test port 1"}, {"kind", "udp"}, {"dstIp", "239.1.1.1"}, {"size", 512}, {"pps", 500}, {"seconds", 20}, {"maxLoss", 0}})},
		{"ipv6", ex("IPv6 ping between two test ports", om{{"name", "v6-ping"}, {"from", "Test port 1"}, {"to", "Test port 2"}, {"kind", "icmp"}, {"dstIp", "fd00::2"}, {"size", 100}, {"pps", 100}, {"count", 500}, {"maxLoss", 0}})},
	}
	portSchema := om{{"type", "object"}, {"required", []string{"switch", "port"}}, {"properties", om{
		{"switch", om{{"type", "string"}, {"description", "The switch's name (or id)."}}},
		{"port", om{{"type", "string"}, {"description", "A free data port, named as the switch names it (1, 1/5, …)."}}},
		{"name", om{{"type", "string"}, {"description", "Its name (default: Test port n)."}}},
		{"ip", om{{"type", "string"}, {"description", "IPv4 address with prefix; /24 when it has none."}}},
		{"ip6", om{{"type", "string"}, {"description", "IPv6 address with prefix; /64 when it has none."}}},
		{"gateway", om{{"type", "string"}, {"description", "Gateway for streams to another network."}}},
		{"vlan", om{{"type", "integer"}, {"minimum", 0}, {"maximum", 4094}, {"description", "Tags its own frames (ARP and ping answers) with this VLAN."}}},
		{"groups", om{{"type", "array"}, {"items", om{{"type", "string"}}}, {"description", "Multicast groups it joins (IGMPv2 / MLDv1 reports every 10 s and on a query)."}}},
	}}}
	portPatch := om{{"type", "object"}, {"minProperties", 1}, {"additionalProperties", false}, {"description", "Only the fields given change."}, {"properties", om{
		{"name", om{{"type", "string"}}}, {"ip", om{{"type", "string"}}}, {"ip6", om{{"type", "string"}}}, {"gateway", om{{"type", "string"}}},
		{"vlan", om{{"type", "integer"}, {"minimum", 0}, {"maximum", 4094}}}, {"groups", om{{"type", "array"}, {"items", om{{"type", "string"}}}}}}}}
	portParam := om{{"name", "port"}, {"in", "path"}, {"required", true}, {"description", "The test port's name (Test port 1) or id."}, {"schema", om{{"type", "string"}}}}
	streamParam := om{{"name", "stream"}, {"in", "path"}, {"required", true}, {"description", "The stream's name or id."}, {"schema", om{{"type", "string"}}}}

	paths := om{
		{"/openapi.json", om{{"get", op("getOpenAPI", "API", "This description, as JSON", "The OpenAPI document of this API (as JSON; /openapi.yaml as YAML). It needs no key.",
			kv{"security", []om{{}}}, kv{"responses", om{{"405", om{{"$ref", "#/components/responses/MethodNotAllowed"}}}, {"200", om{{"description", "The OpenAPI document."}, {"content", om{{"application/json", om{{"schema", om{{"type", "object"}}}}}}}}}}})}}},
		{"/openapi.yaml", om{{"get", op("getOpenAPIYaml", "API", "This description, as YAML", "The OpenAPI document of this API as YAML. It needs no key.",
			kv{"security", []om{{}}}, kv{"responses", om{{"405", om{{"$ref", "#/components/responses/MethodNotAllowed"}}}, {"200", om{{"description", "The OpenAPI document."}, {"content", om{{"application/yaml", om{{"schema", om{{"type", "string"}}}}}}}}}}})}}},
		{"/lab", om{{"get", op("getLab", "Lab", "The lab", "The open topology: its switches with their state, management addresses and consoles, the cables and LAGs, the KVM hosts, images and the latest Events.",
			kv{"responses", append(om{{"200", resp("The lab.", "Lab")}}, std...)})}}},
		{"/lab/tools", om{{"get", op("listTools", "Tools", "The lab tools", "Every lab tool with its arguments (JSON Schema). Each one is POST /lab/tools/{name}.",
			kv{"responses", append(om{{"200", om{{"description", "The tools."}, {"content", om{{"application/json", om{{"schema", om{{"type", "array"}, {"items", om{{"$ref", "#/components/schemas/Tool"}}}}}}}}}}}}, std...)})}}},
		{"/lab/traffic", om{{"get", op("getTraffic", "Traffic", "Test ports, streams and results", "The traffic generator now. While a run goes on, the results are live (counters, rates and the rate history for graphs); after it, they are final.",
			kv{"responses", append(om{{"200", resp("The traffic generator.", "TrafficView")}}, std...)})}}},
		{"/lab/traffic/report", om{{"get", op("getTrafficReport", "Traffic", "The report", "The last (or running) run's report: a pass or fail per stream with its counters, loss, latency, jitter, order and the reason of a fail.",
			kv{"parameters", []om{{{"name", "format"}, {"in", "query"}, {"required", false}, {"description", "json (the traffic view), csv, html or text."}, {"schema", om{{"type", "string"}, {"enum", []string{"json", "csv", "html", "text"}}, {"default", "json"}}}}}},
			kv{"responses", append(om{{"200", om{{"description", "The report."}, {"content", om{
				{"application/json", om{{"schema", om{{"$ref", "#/components/schemas/TrafficView"}}}}},
				{"text/csv", om{{"schema", om{{"type", "string"}}}}},
				{"text/html", om{{"schema", om{{"type", "string"}}}}},
				{"text/plain", om{{"schema", om{{"type", "string"}}}}}}}}}}, append(errs("400"), std...)...)})}}},
		{"/lab/traffic/ports", om{{"post", op("addTestPort", "Traffic ports", "Add a test port", "Puts a test port on a free port of a switch. A running switch takes it when it restarts (the result's note says so).",
			kv{"requestBody", om{{"required", true}, {"content", om{{"application/json", om{{"schema", portSchema}, {"examples", om{
				{"v4", ex("An IPv4 test port", om{{"switch", "leaf-1"}, {"port", "5"}, {"ip", "10.10.0.1/24"}})},
				{"dual", ex("IPv4 and IPv6, in a multicast group", om{{"switch", "leaf-2"}, {"port", "5"}, {"ip", "10.10.0.2/24"}, {"ip6", "fd00::2/64"}, {"groups", []string{"239.1.1.1"}}})}}}}}}}}},
			kv{"responses", append(om{{"201", resp("Added; the traffic view with it.", "TrafficView")}}, append(errs("400", "422"), std...)...)})}}},
		{"/lab/traffic/ports/{port}", om{{"parameters", []om{portParam}},
			{"patch", op("changeTestPort", "Traffic ports", "Change a test port", "Changes its name, addresses, gateway, VLAN or multicast groups.",
				kv{"requestBody", om{{"required", true}, {"content", om{{"application/json", om{{"schema", portPatch}, {"example", om{{"groups", []string{"239.1.1.1", "239.1.1.2"}}}}}}}}}},
				kv{"responses", append(om{{"200", resp("Changed; the traffic view.", "TrafficView")}}, append(errs("400", "404", "422"), std...)...)})},
			{"delete", op("removeTestPort", "Traffic ports", "Remove a test port", "Removes the test port and its streams.",
				kv{"responses", append(om{{"204", om{{"description", "Removed."}}}}, append(errs("404", "422"), std...)...)})}}},
		{"/lab/traffic/streams", om{{"post", op("addStream", "Traffic streams", "Add a stream", "Adds a stream. from and to can be test port names or ids. Without to: every other test port (broadcast), dstIp, or a multicast group in dstIp.",
			kv{"requestBody", om{{"required", true}, {"content", om{{"application/json", om{{"schema", om{{"$ref", "#/components/schemas/StreamInput"}}}, {"examples", streamEx}}}}}}},
			kv{"responses", append(om{{"201", resp("Added; the traffic view with it.", "TrafficView")}}, append(errs("400", "422"), std...)...)})}}},
		{"/lab/traffic/streams/{stream}", om{{"parameters", []om{streamParam}},
			{"put", op("replaceStream", "Traffic streams", "Replace a stream", "Replaces the stream with the one given (its id stays).",
				kv{"requestBody", om{{"required", true}, {"content", om{{"application/json", om{{"schema", om{{"$ref", "#/components/schemas/StreamInput"}}}, {"examples", streamEx}}}}}}},
				kv{"responses", append(om{{"200", resp("Replaced; the traffic view.", "TrafficView")}}, append(errs("400", "404", "422"), std...)...)})},
			{"delete", op("removeStream", "Traffic streams", "Remove a stream", "Removes the stream.",
				kv{"responses", append(om{{"204", om{{"description", "Removed."}}}}, append(errs("404", "422"), std...)...)})}}},
		{"/lab/traffic/start", om{{"post", op("startTraffic", "Traffic", "Start a run", "Starts the streams given, or every stream that is not marked off. A new run resets the counters. Poll GET /lab/traffic for live results; the run ends by itself when every stream has sent its frames.",
			kv{"requestBody", om{{"required", false}, {"content", om{{"application/json", om{{"schema", om{{"type", "object"}, {"properties", om{{"streams", om{{"type", "array"}, {"items", om{{"type", "string"}}}, {"description", "Stream names or ids; none = all."}}}}}}}, {"example", om{{"streams", []string{"voice", "imix-100"}}}}}}}}}},
			kv{"responses", append(om{{"200", resp("Started; the traffic view.", "TrafficView")}}, append(errs("422"), std...)...)})}}},
		{"/lab/traffic/stop", om{{"post", op("stopTraffic", "Traffic", "Stop the run", "Stops sending. The counters stay; a moment later (frames on their way) they are final.",
			kv{"responses", append(om{{"200", resp("Stopped; the traffic view.", "TrafficView")}}, append(errs("422"), std...)...)})}}},
	}
	// every lab tool, with its own arguments
	tools := append([]mcpTool{}, mcpLabTools...)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	for _, tl := range tools {
		n := strings.TrimPrefix(tl.Name, "lab_")
		schema := om{{"type", "object"}}
		if p, ok := tl.InputSchema["properties"].(map[string]any); ok {
			keys := make([]string, 0, len(p))
			for k := range p {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			props := om{}
			for _, k := range keys {
				props = append(props, kv{k, p[k]})
			}
			schema = append(schema, kv{"properties", props})
		}
		if r, ok := tl.InputSchema["required"].([]string); ok && len(r) > 0 {
			schema = append(schema, kv{"required", r})
		}
		summary := tl.Description
		if i := strings.IndexAny(summary, ".:"); i > 0 {
			summary = summary[:i]
		}
		paths = append(paths, kv{"/lab/tools/" + n, om{{"post", op("tool_"+n, "Tools", summary, tl.Description+" Through the API it runs without asking.",
			kv{"requestBody", om{{"required", false}, {"content", om{{"application/json", om{{"schema", schema}}}}}}},
			kv{"responses", append(om{{"200", resp("What it did or found, as text.", "ToolResult")}}, append(errs("400", "404", "422"), std...)...)})}}})
	}
	schemas := om{
		{"Problem", om{{"type", "object"}, {"description", "An error, as RFC 9457 problem details."}, {"required", []string{"type", "title", "status", "detail"}}, {"properties", om{
			{"type", om{{"type", "string"}, {"format", "uri"}, {"description", "What kind of problem (a link into docs/API.md)."}}},
			{"title", om{{"type", "string"}, {"description", "The HTTP status text."}}},
			{"status", om{{"type", "integer"}, {"description", "The HTTP status code."}}},
			{"detail", om{{"type", "string"}, {"description", "What went wrong, for a person."}}}}}}},
		{"ToolResult", om{{"type", "object"}, {"required", []string{"result"}}, {"properties", om{{"result", om{{"type", "string"}, {"description", "What the tool did or found, as text."}}}}}}},
	}
	sort.Strings(g.order)
	for _, n := range g.order {
		schemas = append(schemas, kv{n, g.comps[n]})
	}
	// a stream as it is sent: no id (it is made, or comes from the path), only from needed
	var in om
	for _, x := range g.comps["Stream"] {
		switch x.k {
		case "description":
			in = append(in, kv{"description", "A stream to add or to replace one with. Only from is needed; the rest has defaults (udp, 128 bytes, 100 pps, 10 s, no loss allowed). from and to can be test port names or ids. Unknown fields are refused."})
		case "properties":
			var ps om
			for _, p := range x.v.(om) {
				if p.k != "id" {
					ps = append(ps, p)
				}
			}
			in = append(in, kv{"properties", ps})
		case "required":
			in = append(in, kv{"required", []string{"from"}}, kv{"additionalProperties", false})
		default:
			in = append(in, x)
		}
	}
	schemas = append(schemas, kv{"StreamInput", in})
	prob := func(desc string) om { return problemRef(desc) }
	return om{
		{"openapi", "3.2.1"},
		{"info", om{
			{"title", "Termwright local API"},
			{"summary", "The emulated lab of Termwright for scripts, test frameworks and CI on this computer."},
			{"description", openAPIIntro},
			{"version", "1.0.0"},
			{"license", om{{"name", "Apache 2.0"}, {"identifier", "Apache-2.0"}}}}},
		{"servers", []om{{{"url", "http://127.0.0.1:{port}/api/v1"}, {"description", "Termwright on this computer (its address is in mcp.json in the data folder, and in Settings → AI Assistant → Local API)."},
			{"variables", om{{"port", om{{"default", "8080"}, {"description", "Chosen when Termwright starts."}}}}}}}},
		{"security", []om{{{"bearerAuth", []string{}}}}},
		{"tags", []om{
			{{"name", "Lab"}, {"description", "The open topology."}},
			{{"name", "Tools"}, {"description", "The lab tools: switches, cables, power, consoles, cabling check, moves, captures, link check, Events and the traffic generator as one tool."}},
			{{"name", "Traffic"}, {"description", "The traffic generator: runs, live results and the report."}},
			{{"name", "Traffic ports"}, {"summary", "Test ports"}, {"parent", "Traffic"}, {"description", "Test ports on switch ports."}},
			{{"name", "Traffic streams"}, {"summary", "Streams"}, {"parent", "Traffic"}, {"description", "Streams between test ports."}},
			{{"name", "API"}, {"description", "This description."}}}},
		{"paths", paths},
		{"components", om{
			{"schemas", schemas},
			{"responses", om{
				{"BadRequest", prob("The request is not right: not JSON, an unknown field or format.")},
				{"Unauthorized", prob("No key, or a wrong one.")},
				{"Forbidden", prob("The local API is off, or the request came from a web page.")},
				{"NotFound", prob("No such path, test port, stream or tool.")},
				{"MethodNotAllowed", prob("Not this method here (the Allow header lists the ones that are).")},
				{"Unprocessable", prob("The lab could not do it: the reason is in detail (no such switch, a port in use, no stream to start…).")}}},
			{"securitySchemes", om{{"bearerAuth", om{{"type", "http"}, {"scheme", "bearer"}, {"description", "The key from Settings → AI Assistant → Local API → Copy Key (or token in mcp.json). A new one each time Termwright starts."}}}}}}},
	}
}

const openAPIIntro = `The emulated lab of Termwright (switch VMs on KVM hosts) for your own scripts, test frameworks and CI, on this computer only.

**Turn it on:** Settings → AI Assistant → Local API. The address and the key are there (*Copy Key*), and in ` + "`mcp.json`" + ` in Termwright's data folder (` + "`url`" + ` and ` + "`token`" + `), which a script can read. The key changes each time Termwright starts.

**Send the key** as ` + "`Authorization: Bearer <key>`" + `. Requests from web pages (with an Origin header) are refused.

**Nothing is asked first**: unlike the Claude connector, the API is your own automation. Its changes show as *API* in the lab's Events.

**Errors** are RFC 9457 problem details (` + "`application/problem+json`" + `): 400 a bad request, 401 no or a wrong key, 403 the API is off, 404 no such thing, 405 a wrong method (see Allow), 422 the lab could not do it.

**The same from a terminal:** ` + "`termwright lab help`" + `, for example ` + "`termwright lab traffic start voice`" + ` or ` + "`termwright lab traffic report --csv`" + `.

A typical test: add test ports (POST /lab/traffic/ports), add streams (POST /lab/traffic/streams), start (POST /lab/traffic/start), poll GET /lab/traffic until running is false, then GET /lab/traffic/report.`
