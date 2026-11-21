package testrec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// IsAPIDoc reports whether a file is a Postman collection or an OpenAPI/Swagger description.
func IsAPIDoc(b []byte) bool {
	var m map[string]any
	if json.Unmarshal(b, &m) != nil && yaml.Unmarshal(b, &m) != nil {
		return false
	}
	_, oa := m["openapi"]
	_, sw := m["swagger"]
	return oa || sw || isPostman(m)
}

func isPostman(m map[string]any) bool {
	info, _ := m["info"].(map[string]any)
	s, _ := info["schema"].(string)
	_, items := m["item"]
	return strings.Contains(s, "postman") || (info != nil && items)
}

// ImportAPI makes tests from a Postman collection (v2.0/v2.1) or an OpenAPI 3 / Swagger 2 description: one
// test per request, with an API request step and a status check (from the test script or the saved
// example responses; for OpenAPI, the first 2xx answer). Postman folders and OpenAPI tags become suites.
// session: the API session the requests go to (its base URL replaces the collection's {{baseUrl}} or the
// description's servers); "" keeps full URLs.
func ImportAPI(b []byte, session string) ([]ImportedCase, error) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		if err := yaml.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("not JSON or YAML: %v", err)
		}
	}
	switch {
	case isPostman(m):
		return importPostman(m, session), nil
	case m["openapi"] != nil || m["swagger"] != nil:
		return importOpenAPI(m, session), nil
	}
	return nil, fmt.Errorf("not a Postman collection or an OpenAPI description")
}

var pmStatus = regexp.MustCompile(`(?:to\.have\.status|response\.code\)?\.to\.(?:eql|equal)|responseCode\.code\s*===?)\(?\s*(\d{3})`)
var pmInclude = regexp.MustCompile(`to\.(?:include|contain)\(\s*["']([^"']{2,})["']\s*\)`)
var leadVar = regexp.MustCompile(`^\{\{[^}]+\}\}`)

func str(x any) string { s, _ := x.(string); return s }

func importPostman(m map[string]any, session string) []ImportedCase {
	var out []ImportedCase
	seen := map[string]int{}
	var walk func(items []any, suite string)
	walk = func(items []any, suite string) {
		for _, it := range items {
			im, _ := it.(map[string]any)
			if im == nil {
				continue
			}
			name := str(im["name"])
			if sub, ok := im["item"].([]any); ok {
				s := name
				if suite != "" {
					s = suite + "/" + name
				}
				walk(sub, s)
				continue
			}
			req, _ := im["request"].(map[string]any)
			if req == nil {
				if u := str(im["request"]); u != "" {
					req = map[string]any{"url": u}
				} else {
					continue
				}
			}
			h := &HTTPStep{Session: session, Method: strings.ToUpper(str(req["method"]))}
			if h.Method == "" || h.Method == "GET" {
				h.Method = ""
			}
			switch u := req["url"].(type) {
			case string:
				h.URL = u
			case map[string]any:
				h.URL = str(u["raw"])
			}
			if session != "" {
				h.URL = stripHost(h.URL)
			}
			for _, x := range anyList(req["header"]) {
				hm, _ := x.(map[string]any)
				if hm == nil || hm["disabled"] == true || str(hm["key"]) == "" {
					continue
				}
				if h.Headers == nil {
					h.Headers = map[string]string{}
				}
				h.Headers[str(hm["key"])] = str(hm["value"])
			}
			if body, _ := req["body"].(map[string]any); body != nil && str(body["mode"]) == "raw" {
				h.Body = str(body["raw"])
			}
			st := Step{HTTP: h}
			script := ""
			for _, e := range anyList(im["event"]) {
				em, _ := e.(map[string]any)
				if str(em["listen"]) != "test" {
					continue
				}
				sc, _ := em["script"].(map[string]any)
				for _, l := range anyList(sc["exec"]) {
					script += str(l) + "\n"
				}
				if s := str(sc["exec"]); s != "" {
					script += s
				}
			}
			code := 0
			if mm := pmStatus.FindStringSubmatch(script); mm != nil {
				code, _ = strconv.Atoi(mm[1])
			} else {
				for _, r := range anyList(im["response"]) {
					rm, _ := r.(map[string]any)
					if c, ok := rm["code"].(float64); ok && c >= 200 && c < 300 {
						code = int(c)
						break
					}
				}
			}
			if code != 0 {
				st.Expect = append(st.Expect, Expect{Status: code})
			}
			for _, mm := range pmInclude.FindAllStringSubmatch(script, 5) {
				st.Expect = append(st.Expect, Expect{Contains: mm[1]})
			}
			if name == "" {
				name = strings.TrimSpace(h.Method + " " + h.URL)
			}
			out = append(out, newImported(seen, suite, name, st))
		}
	}
	walk(anyList(m["item"]), "")
	return out
}

func anyList(x any) []any { l, _ := x.([]any); return l }

// stripHost keeps the path of a URL for an API session: "{{baseUrl}}/vlans" and "https://h/rest/vlans" → "/vlans", "/rest/vlans".
func stripHost(u string) string {
	if l := leadVar.FindString(u); l != "" {
		u = strings.TrimPrefix(u, l)
	} else if i := strings.Index(u, "://"); i >= 0 {
		if j := strings.Index(u[i+3:], "/"); j >= 0 {
			u = u[i+3+j:]
		} else {
			u = "/"
		}
	}
	if u == "" {
		u = "/"
	}
	return u
}

func newImported(seen map[string]int, suite, name string, st Step) ImportedCase {
	file := "cases/"
	if suite != "" {
		parts := strings.Split(suite, "/")
		for i, p := range parts {
			parts[i] = slug(p)
		}
		file += strings.Join(parts, "/") + "/"
	}
	base := slug(name)
	seen[file+base]++
	if n := seen[file+base]; n > 1 {
		base += "-" + strconv.Itoa(n)
	}
	return ImportedCase{File: file + base + ".yaml", Case: Case{Name: name, Steps: []Step{st}}}
}

var oaParam = regexp.MustCompile(`\{([^}]+)\}`)

func importOpenAPI(m map[string]any, session string) []ImportedCase {
	base := ""
	if session == "" {
		if sv := anyList(m["servers"]); len(sv) > 0 {
			base = strings.TrimRight(str(sv[0].(map[string]any)["url"]), "/")
		} else if h := str(m["host"]); h != "" {
			sch := "https"
			if l := anyList(m["schemes"]); len(l) > 0 {
				sch = str(l[0])
			}
			base = sch + "://" + h + strings.TrimRight(str(m["basePath"]), "/")
		}
	} else if bp := str(m["basePath"]); bp != "" {
		base = strings.TrimRight(bp, "/")
	}
	paths, _ := m["paths"].(map[string]any)
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []ImportedCase
	seen := map[string]int{}
	for _, p := range keys {
		ops, _ := paths[p].(map[string]any)
		for _, meth := range []string{"get", "post", "put", "patch", "delete"} {
			op, _ := ops[meth].(map[string]any)
			if op == nil {
				continue
			}
			h := &HTTPStep{Session: session, URL: base + oaParam.ReplaceAllString(p, "{{$1}}")}
			if meth != "get" {
				h.Method = strings.ToUpper(meth)
			}
			if rb, _ := op["requestBody"].(map[string]any); rb != nil {
				if c, _ := rb["content"].(map[string]any); c != nil {
					if j, _ := c["application/json"].(map[string]any); j != nil {
						h.Body = exampleJSON(j, m)
					}
				}
			}
			for _, prm := range anyList(op["parameters"]) {
				pm, _ := prm.(map[string]any)
				if str(pm["in"]) == "body" {
					h.Body = exampleJSON(pm, m)
				}
			}
			st := Step{HTTP: h}
			if rs, _ := op["responses"].(map[string]any); rs != nil {
				codes := make([]string, 0, len(rs))
				for c := range rs {
					codes = append(codes, c)
				}
				sort.Strings(codes)
				for _, c := range codes {
					if n, err := strconv.Atoi(c); err == nil && n >= 200 && n < 300 {
						st.Expect = append(st.Expect, Expect{Status: n})
						break
					}
				}
			}
			name := str(op["summary"])
			if name == "" {
				name = str(op["operationId"])
			}
			if name == "" {
				name = strings.ToUpper(meth) + " " + p
			}
			suite := "api"
			if t := anyList(op["tags"]); len(t) > 0 {
				suite = str(t[0])
			}
			out = append(out, newImported(seen, suite, name, st))
		}
	}
	return out
}

// exampleJSON is a request body for a media type or body parameter: its example, else one made from the schema.
func exampleJSON(x map[string]any, doc map[string]any) string {
	v := x["example"]
	if v == nil {
		if ex, _ := x["examples"].(map[string]any); ex != nil {
			for _, e := range ex {
				if em, _ := e.(map[string]any); em != nil {
					v = em["value"]
					break
				}
			}
		}
	}
	if v == nil {
		s, _ := x["schema"].(map[string]any)
		v = fromSchema(s, doc, 0)
	}
	if v == nil {
		return ""
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func fromSchema(s map[string]any, doc map[string]any, depth int) any {
	if s == nil || depth > 6 {
		return nil
	}
	if ref := str(s["$ref"]); strings.HasPrefix(ref, "#/") {
		var cur any = doc
		for _, p := range strings.Split(ref[2:], "/") {
			cm, _ := cur.(map[string]any)
			cur = cm[p]
		}
		r, _ := cur.(map[string]any)
		return fromSchema(r, doc, depth+1)
	}
	if e, ok := s["example"]; ok {
		return e
	}
	if d, ok := s["default"]; ok {
		return d
	}
	if en := anyList(s["enum"]); len(en) > 0 {
		return en[0]
	}
	switch str(s["type"]) {
	case "object", "":
		props, _ := s["properties"].(map[string]any)
		if props == nil {
			if str(s["type"]) == "" {
				return nil
			}
			return map[string]any{}
		}
		o := map[string]any{}
		for k, p := range props {
			pm, _ := p.(map[string]any)
			o[k] = fromSchema(pm, doc, depth+1)
		}
		return o
	case "array":
		it, _ := s["items"].(map[string]any)
		if v := fromSchema(it, doc, depth+1); v != nil {
			return []any{v}
		}
		return []any{}
	case "integer", "number":
		return 0
	case "boolean":
		return false
	}
	return "string"
}
