package apiclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Import reads a cURL command, a Postman v2.0/v2.1 collection, an OpenAPI 3 or Swagger 2 document
// (JSON or YAML) or a Termwright collection.
func Import(text, name string) (Collection, error) {
	t := strings.TrimSpace(text)
	if t == "" {
		return Collection{}, errors.New("nothing to import")
	}
	if strings.HasPrefix(t, "curl ") || strings.HasPrefix(t, "curl.exe ") {
		var c Collection
		for i, cmd := range splitCurls(t) {
			r, err := ParseCurl(cmd)
			if err != nil {
				return Collection{}, err
			}
			r.ID = fmt.Sprintf("r%d", i+1)
			c.Requests = append(c.Requests, r)
		}
		c.Name = firstNonEmpty(name, "Imported from cURL")
		return c, nil
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(t), &doc); err != nil || doc == nil {
		return Collection{}, errors.New("not a cURL command, a Postman collection or an OpenAPI document")
	}
	doc = normalize(doc).(map[string]any)
	switch {
	case doc["openapi"] != nil || doc["swagger"] != nil:
		return importOpenAPI(doc, name)
	case doc["info"] != nil && doc["item"] != nil:
		return importPostman(doc, name)
	case doc["requests"] != nil:
		var c Collection
		b, _ := json.Marshal(doc)
		if err := json.Unmarshal(b, &c); err != nil {
			return c, err
		}
		if name != "" {
			c.Name = name
		}
		return c, nil
	}
	return Collection{}, errors.New("not a cURL command, a Postman collection or an OpenAPI document")
}

// normalize turns YAML's map[any]any into map[string]any.
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = normalize(e)
		}
		return x
	case map[any]any:
		m := map[string]any{}
		for k, e := range x {
			m[fmt.Sprint(k)] = normalize(e)
		}
		return m
	case []any:
		for i, e := range x {
			x[i] = normalize(e)
		}
	}
	return v
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// ---- cURL ----

func splitCurls(t string) []string {
	t = strings.ReplaceAll(t, "\\\r\n", " ")
	t = strings.ReplaceAll(t, "\\\n", " ")
	t = strings.ReplaceAll(t, "^\r\n", " ") // Windows cmd
	var out []string
	for _, l := range strings.Split(t, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "curl ") || strings.HasPrefix(l, "curl.exe ") {
			out = append(out, l)
		} else if len(out) > 0 && l != "" {
			out[len(out)-1] += " " + l
		}
	}
	return out
}

// shellWords splits a command line like a POSIX shell (quotes and backslashes).
func shellWords(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	in, q := false, byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '\'':
			if c == '\'' {
				q = 0
			} else {
				cur.WriteByte(c)
			}
		case q == '"':
			if c == '"' {
				q = 0
			} else if c == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0 {
				i++
				cur.WriteByte(s[i])
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			q, in = c, true
		case c == '$' && i+1 < len(s) && s[i+1] == '\'': // $'...'
			q, in = '\'', true
			i++
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			in = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	if q != 0 {
		return nil, errors.New("a quote is not closed")
	}
	if in {
		out = append(out, cur.String())
	}
	return out, nil
}

// ParseCurl reads one curl command.
func ParseCurl(cmd string) (Request, error) {
	w, err := shellWords(cmd)
	if err != nil {
		return Request{}, err
	}
	r := Request{}
	var data []string
	get, method := false, ""
	for i := 1; i < len(w); i++ {
		a := w[i]
		val := func() string {
			if i+1 < len(w) {
				i++
				return w[i]
			}
			return ""
		}
		if strings.HasPrefix(a, "--") && strings.Contains(a, "=") {
			k, v, _ := strings.Cut(a, "=")
			a = k
			w = append(w[:i+1], append([]string{v}, w[i+1:]...)...)
		}
		switch a {
		case "-X", "--request":
			method = strings.ToUpper(val())
		case "-H", "--header":
			k, v, _ := strings.Cut(val(), ":")
			r.Headers = append(r.Headers, KV{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
		case "-d", "--data", "--data-raw", "--data-binary", "--data-ascii", "--data-urlencode":
			data = append(data, val())
		case "--json":
			data = append(data, val())
			r.Headers = append(r.Headers, KV{Key: "Content-Type", Value: "application/json"}, KV{Key: "Accept", Value: "application/json"})
		case "-F", "--form", "--form-string":
			k, v, _ := strings.Cut(val(), "=")
			r.Body.Mode = "form"
			r.Body.Form = append(r.Body.Form, KV{Key: k, Value: v})
		case "-u", "--user":
			r.Headers = append(r.Headers, KV{Key: "Authorization", Value: "Basic " + b64(val())})
		case "-b", "--cookie":
			r.Headers = append(r.Headers, KV{Key: "Cookie", Value: val()})
		case "-A", "--user-agent":
			r.Headers = append(r.Headers, KV{Key: "User-Agent", Value: val()})
		case "-e", "--referer":
			r.Headers = append(r.Headers, KV{Key: "Referer", Value: val()})
		case "-G", "--get":
			get = true
		case "--url":
			r.URL = val()
		case "-o", "--output", "-w", "--write-out", "-m", "--max-time", "--connect-timeout", "-x", "--proxy", "--cacert", "--cert", "--key", "-c", "--cookie-jar", "-K", "--config", "--retry":
			val()
		default:
			if !strings.HasPrefix(a, "-") && r.URL == "" {
				r.URL = a
			}
		}
	}
	if r.URL == "" {
		return r, errors.New("no URL in the curl command")
	}
	if len(data) > 0 {
		joined := strings.Join(data, "&")
		if get {
			if strings.Contains(r.URL, "?") {
				r.URL += "&" + joined
			} else {
				r.URL += "?" + joined
			}
		} else if r.Body.Mode == "" {
			r.Body.Mode, r.Body.Raw = "text", joined
			ct := ""
			for _, h := range r.Headers {
				if strings.EqualFold(h.Key, "Content-Type") {
					ct = strings.ToLower(h.Value)
				}
			}
			t := strings.TrimSpace(joined)
			switch {
			case strings.Contains(ct, "json") || (ct == "" && (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "["))):
				r.Body.Mode = "json"
			case strings.Contains(ct, "xml"):
				r.Body.Mode = "xml"
			case ct == "" || strings.Contains(ct, "x-www-form-urlencoded"):
				if v, err := url.ParseQuery(joined); err == nil && len(v) > 0 && !strings.ContainsAny(t, " {") {
					r.Body.Mode, r.Body.Raw = "urlencoded", ""
					keys := make([]string, 0, len(v))
					for k := range v {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						for _, x := range v[k] {
							r.Body.Form = append(r.Body.Form, KV{Key: k, Value: x})
						}
					}
				}
			}
		}
	}
	// a Content-Type header the body mode sets anyway is dropped
	if r.Body.Mode != "" {
		var hs []KV
		for _, h := range r.Headers {
			if strings.EqualFold(h.Key, "Content-Type") && (r.Body.Mode == "json" && strings.Contains(h.Value, "json") || r.Body.Mode == "urlencoded" || r.Body.Mode == "form") {
				continue
			}
			hs = append(hs, h)
		}
		r.Headers = hs
	}
	switch {
	case method != "":
		r.Method = method
	case len(data) > 0 && !get, r.Body.Mode == "form":
		r.Method = "POST"
	default:
		r.Method = "GET"
	}
	if u, err := url.Parse(r.URL); err == nil {
		r.Name = r.Method + " " + firstNonEmpty(u.Path, "/")
	}
	return r, nil
}

// ---- Postman ----

func importPostman(doc map[string]any, name string) (Collection, error) {
	info, _ := doc["info"].(map[string]any)
	c := Collection{Name: firstNonEmpty(name, str(info["name"]), "Postman collection"), Description: descText(info["description"])}
	for _, v := range asList(doc["variable"]) {
		m, _ := v.(map[string]any)
		if k := str(m["key"]); k != "" {
			c.Vars = append(c.Vars, KV{Key: k, Value: str(m["value"])})
		}
	}
	if a, _ := doc["auth"].(map[string]any); a != nil {
		c.Auth, c.AuthData = postmanAuth(a)
	}
	c.PreScript, c.Tests = postmanScripts(asList(doc["event"]))
	var walk func(items []any, folder string, inherited []any, auth map[string]any)
	n := 0
	walk = func(items []any, folder string, events []any, fauth map[string]any) {
		for _, it := range items {
			m, _ := it.(map[string]any)
			if m == nil {
				continue
			}
			if sub, ok := m["item"].([]any); ok {
				f := str(m["name"])
				if folder != "" {
					f = folder + "/" + f
				}
				fa := fauth
				if a, _ := m["auth"].(map[string]any); a != nil {
					fa = a
				}
				walk(sub, f, append(append([]any{}, events...), asList(m["event"])...), fa)
				continue
			}
			rq, _ := m["request"].(map[string]any)
			if rq == nil {
				if s, ok := m["request"].(string); ok {
					rq = map[string]any{"url": s, "method": "GET"}
				} else {
					continue
				}
			}
			n++
			r := Request{ID: fmt.Sprintf("r%d", n), Name: str(m["name"]), Folder: folder, Method: strings.ToUpper(firstNonEmpty(str(rq["method"]), "GET")), Description: descText(rq["description"])}
			switch u := rq["url"].(type) {
			case string:
				r.URL = u
			case map[string]any:
				r.URL = str(u["raw"])
				for _, pv := range asList(u["variable"]) {
					pm, _ := pv.(map[string]any)
					if k := str(pm["key"]); k != "" {
						r.PathVars = append(r.PathVars, KV{Key: k, Value: str(pm["value"]), Desc: descText(pm["description"])})
					}
				}
				if i := strings.IndexByte(r.URL, '?'); i >= 0 && len(asList(u["query"])) > 0 {
					r.URL = r.URL[:i]
					for _, q := range asList(u["query"]) {
						qm, _ := q.(map[string]any)
						r.Params = append(r.Params, KV{Key: str(qm["key"]), Value: str(qm["value"]), Off: qm["disabled"] == true})
					}
				}
			}
			for _, h := range asList(rq["header"]) {
				hm, _ := h.(map[string]any)
				r.Headers = append(r.Headers, KV{Key: str(hm["key"]), Value: str(hm["value"]), Off: hm["disabled"] == true})
			}
			if b, _ := rq["body"].(map[string]any); b != nil {
				switch str(b["mode"]) {
				case "raw":
					r.Body.Raw = str(b["raw"])
					lang := str(Path(b, "options.raw.language"))
					r.Body.Mode = map[string]string{"json": "json", "xml": "xml", "html": "html", "javascript": "javascript", "text": "text"}[lang]
					if r.Body.Mode == "" {
						r.Body.Mode = "text"
						if t := strings.TrimSpace(r.Body.Raw); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
							r.Body.Mode = "json"
						}
					}
				case "urlencoded", "formdata":
					r.Body.Mode = map[string]string{"urlencoded": "urlencoded", "formdata": "form"}[str(b["mode"])]
					for _, f := range asList(b[str(b["mode"])]) {
						fm, _ := f.(map[string]any)
						kv := KV{Key: str(fm["key"]), Value: str(fm["value"]), Off: fm["disabled"] == true, Desc: descText(fm["description"])}
						if str(fm["type"]) == "file" {
							kv.Type = "file"
							kv.Value = str(fm["src"])
							if l, ok := fm["src"].([]any); ok && len(l) > 0 {
								kv.Value = str(l[0])
							}
						}
						r.Body.Form = append(r.Body.Form, kv)
					}
				case "file":
					f, _ := b["file"].(map[string]any)
					r.Body.Mode, r.Body.File = "binary", str(f["src"])
				case "graphql":
					g, _ := b["graphql"].(map[string]any)
					r.Body.Mode, r.Body.Raw, r.Body.GraphQLVars = "graphql", str(g["query"]), str(g["variables"])
				}
			}
			if a, _ := rq["auth"].(map[string]any); a != nil {
				r.Auth, r.AuthData = postmanAuth(a)
			} else if fauth != nil {
				r.Auth, r.AuthData = postmanAuth(fauth)
			}
			if pb, _ := m["protocolProfileBehavior"].(map[string]any); pb != nil {
				r.Settings = postmanSettings(pb)
			}
			for _, e := range append(events, asList(m["event"])...) {
				em, _ := e.(map[string]any)
				sc, _ := em["script"].(map[string]any)
				var src string
				switch x := sc["exec"].(type) {
				case string:
					src = x
				case []any:
					var l []string
					for _, s := range x {
						l = append(l, str(s))
					}
					src = strings.Join(l, "\n")
				}
				if strings.TrimSpace(src) == "" {
					continue
				}
				if str(em["listen"]) == "test" {
					r.Tests = strings.TrimSpace(r.Tests + "\n" + src)
				} else if str(em["listen"]) == "prerequest" {
					r.PreScript = strings.TrimSpace(r.PreScript + "\n" + src)
				}
			}
			c.Requests = append(c.Requests, r)
		}
	}
	walk(asList(doc["item"]), "", nil, nil)
	return c, nil
}

// postmanAuth maps a Postman auth object to a request's own authentication ("" for inherit).
func postmanAuth(a map[string]any) (string, *ReqAuth) {
	switch str(a["type"]) {
	case "noauth":
		return "none", nil
	case "bearer":
		return "bearer", &ReqAuth{Token: postmanAuthVal(a["bearer"], "token")}
	case "basic":
		return "basic", &ReqAuth{User: postmanAuthVal(a["basic"], "username"), Password: postmanAuthVal(a["basic"], "password")}
	case "apikey":
		in := "header"
		if postmanAuthVal(a["apikey"], "in") == "query" {
			in = "query"
		}
		return "apikey", &ReqAuth{Key: postmanAuthVal(a["apikey"], "key"), Value: postmanAuthVal(a["apikey"], "value"), In: in}
	case "oauth2":
		if t := postmanAuthVal(a["oauth2"], "accessToken"); t != "" {
			return "bearer", &ReqAuth{Token: t}
		}
	}
	return "", nil
}

// postmanScripts joins the pre-request and test scripts of a Postman event list.
func postmanScripts(events []any) (pre, tests string) {
	for _, e := range events {
		em, _ := e.(map[string]any)
		sc, _ := em["script"].(map[string]any)
		var src string
		switch x := sc["exec"].(type) {
		case string:
			src = x
		case []any:
			var l []string
			for _, s := range x {
				l = append(l, str(s))
			}
			src = strings.Join(l, "\n")
		}
		if strings.TrimSpace(src) == "" {
			continue
		}
		switch str(em["listen"]) {
		case "test":
			tests = strings.TrimSpace(tests + "\n" + src)
		case "prerequest":
			pre = strings.TrimSpace(pre + "\n" + src)
		}
	}
	return pre, tests
}

// postmanSettings maps Postman's protocolProfileBehavior.
func postmanSettings(pb map[string]any) *Settings {
	st := &Settings{}
	b := func(k string) (bool, bool) { v, ok := pb[k].(bool); return v, ok }
	if v, ok := b("followRedirects"); ok && !v {
		st.NoRedirects = true
	}
	if v, ok := b("strictSSL"); ok {
		st.Verify = map[bool]string{true: "on", false: "off"}[v]
	}
	st.FollowMethod, _ = b("followOriginalHttpMethod")
	st.FollowAuth, _ = b("followAuthorizationHeader")
	st.RemoveReferer, _ = b("removeRefererHeaderOnRedirect")
	st.NoEncode, _ = b("disableUrlEncoding")
	st.NoCookies, _ = b("disableCookies")
	if v, ok := pb["maxRedirects"].(float64); ok {
		st.MaxRedirects = int(v)
	}
	if *st == (Settings{}) {
		return nil
	}
	return st
}

func postmanAuthVal(v any, key string) string {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			m, _ := e.(map[string]any)
			if str(m["key"]) == key {
				return str(m["value"])
			}
		}
	case map[string]any:
		return str(x[key])
	}
	return ""
}

func descText(v any) string {
	if m, ok := v.(map[string]any); ok {
		return str(m["content"])
	}
	return str(v)
}

func asList(v any) []any { l, _ := v.([]any); return l }

// ---- OpenAPI ----

var pathParam = regexp.MustCompile(`\{([^}/]+)\}`)

func importOpenAPI(doc map[string]any, name string) (Collection, error) {
	info, _ := doc["info"].(map[string]any)
	c := Collection{Name: firstNonEmpty(name, str(info["title"]), "OpenAPI"), Description: str(info["description"])}
	base := ""
	if s := asList(doc["servers"]); len(s) > 0 {
		sm, _ := s[0].(map[string]any)
		base = str(sm["url"])
		if vars, _ := sm["variables"].(map[string]any); vars != nil {
			for k, v := range vars {
				vm, _ := v.(map[string]any)
				base = strings.ReplaceAll(base, "{"+k+"}", str(vm["default"]))
			}
		}
	} else if h := str(doc["host"]); h != "" { // Swagger 2
		scheme := "https"
		if l := asList(doc["schemes"]); len(l) > 0 {
			scheme = str(l[0])
		}
		base = scheme + "://" + h + str(doc["basePath"])
	} else if bp := str(doc["basePath"]); bp != "" {
		base = bp
	}
	if base != "" {
		c.Vars = append(c.Vars, KV{Key: "baseUrl", Value: strings.TrimRight(base, "/"), Desc: "from the document's servers; empty uses the session's base URL"})
	}
	paths, _ := doc["paths"].(map[string]any)
	keys := make([]string, 0, len(paths))
	for p := range paths {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	n := 0
	for _, p := range keys {
		item, _ := paths[p].(map[string]any)
		common := asList(item["parameters"])
		for _, m := range []string{"get", "post", "put", "patch", "delete", "head", "options"} {
			op, _ := item[m].(map[string]any)
			if op == nil {
				continue
			}
			n++
			u := pathParam.ReplaceAllString(p, "{{$1}}")
			if base != "" {
				u = "{{baseUrl}}" + u
			}
			r := Request{ID: fmt.Sprintf("r%d", n), Method: strings.ToUpper(m), URL: u,
				Name: firstNonEmpty(str(op["summary"]), str(op["operationId"]), strings.ToUpper(m)+" "+p), Description: str(op["description"])}
			if tags := asList(op["tags"]); len(tags) > 0 {
				r.Folder = str(tags[0])
			}
			for _, prm := range append(common, asList(op["parameters"])...) {
				pm := resolveRef(doc, prm)
				pname, in := str(pm["name"]), str(pm["in"])
				ex := exampleOf(doc, pm, 0)
				exs := ""
				if ex != nil {
					exs = str(ex)
				}
				switch in {
				case "query":
					if pm["required"] == true || exs != "" {
						r.Params = append(r.Params, KV{Key: pname, Value: exs, Off: pm["required"] != true, Desc: str(pm["description"])})
					}
				case "header":
					r.Headers = append(r.Headers, KV{Key: pname, Value: exs, Off: pm["required"] != true, Desc: str(pm["description"])})
				case "path":
					if exs != "" && !seen[pname] {
						seen[pname] = true
						c.Vars = append(c.Vars, KV{Key: pname, Value: exs})
					}
				case "body": // Swagger 2
					r.Body.Mode = "json"
					if ex := exampleOf(doc, resolveRef(doc, pm["schema"]), 0); ex != nil {
						b, _ := json.MarshalIndent(ex, "", "  ")
						r.Body.Raw = string(b)
					}
				}
			}
			if rb := resolveRef(doc, op["requestBody"]); rb != nil {
				content, _ := rb["content"].(map[string]any)
				for _, ct := range []string{"application/json", "application/yang-data+json", "application/x-www-form-urlencoded", "multipart/form-data", "application/xml", "text/plain"} {
					var media map[string]any
					for k, v := range content {
						if strings.HasPrefix(k, ct) || (ct == "application/json" && strings.HasSuffix(k, "+json")) {
							media, _ = v.(map[string]any)
							break
						}
					}
					if media == nil {
						continue
					}
					ex := media["example"]
					if ex == nil {
						if exs, _ := media["examples"].(map[string]any); exs != nil {
							for _, e := range exs {
								em := resolveRef(doc, e)
								ex = em["value"]
								break
							}
						}
					}
					if ex == nil {
						ex = exampleOf(doc, resolveRef(doc, media["schema"]), 0)
					}
					switch {
					case strings.Contains(ct, "json"):
						r.Body.Mode = "json"
						if ex != nil {
							b, _ := json.MarshalIndent(ex, "", "  ")
							r.Body.Raw = string(b)
						}
					case strings.Contains(ct, "urlencoded"), strings.Contains(ct, "form-data"):
						r.Body.Mode = map[bool]string{true: "urlencoded", false: "form"}[strings.Contains(ct, "urlencoded")]
						if m, ok := ex.(map[string]any); ok {
							ks := make([]string, 0, len(m))
							for k := range m {
								ks = append(ks, k)
							}
							sort.Strings(ks)
							for _, k := range ks {
								r.Body.Form = append(r.Body.Form, KV{Key: k, Value: str(m[k])})
							}
						}
					case strings.Contains(ct, "xml"):
						r.Body.Mode = "xml"
					default:
						r.Body.Mode, r.Body.Raw = "text", str(ex)
					}
					r.Headers = append(r.Headers, KV{Key: "Accept", Value: "application/json"})
					break
				}
			}
			c.Requests = append(c.Requests, r)
		}
	}
	if len(c.Requests) == 0 {
		return c, errors.New("the document has no operations")
	}
	return c, nil
}

func resolveRef(doc map[string]any, v any) map[string]any {
	m, _ := v.(map[string]any)
	for i := 0; m != nil && i < 10; i++ {
		ref := str(m["$ref"])
		if !strings.HasPrefix(ref, "#/") {
			return m
		}
		var cur any = doc
		for _, k := range strings.Split(ref[2:], "/") {
			k = strings.ReplaceAll(strings.ReplaceAll(k, "~1", "/"), "~0", "~")
			cm, _ := cur.(map[string]any)
			cur = cm[k]
		}
		m, _ = cur.(map[string]any)
	}
	return m
}

// exampleOf makes an example value from a parameter or schema (its example, default, enum, or by type).
func exampleOf(doc map[string]any, s map[string]any, depth int) any {
	if s == nil || depth > 6 {
		return nil
	}
	if ex, ok := s["example"]; ok {
		return ex
	}
	if ex, ok := s["default"]; ok {
		return ex
	}
	if sc, ok := s["schema"]; ok {
		return exampleOf(doc, resolveRef(doc, sc), depth+1)
	}
	if e := asList(s["enum"]); len(e) > 0 {
		return e[0]
	}
	for _, k := range []string{"allOf", "oneOf", "anyOf"} {
		if l := asList(s[k]); len(l) > 0 {
			if k != "allOf" {
				return exampleOf(doc, resolveRef(doc, l[0]), depth+1)
			}
			out := map[string]any{}
			for _, x := range l {
				if m, ok := exampleOf(doc, resolveRef(doc, x), depth+1).(map[string]any); ok {
					for kk, vv := range m {
						out[kk] = vv
					}
				}
			}
			return out
		}
	}
	switch str(s["type"]) {
	case "object", "":
		props, _ := s["properties"].(map[string]any)
		if props == nil {
			if str(s["type"]) == "object" {
				return map[string]any{}
			}
			return nil
		}
		out := map[string]any{}
		for k, p := range props {
			if v := exampleOf(doc, resolveRef(doc, p), depth+1); v != nil {
				out[k] = v
			}
		}
		return out
	case "array":
		if it := exampleOf(doc, resolveRef(doc, s["items"]), depth+1); it != nil {
			return []any{it}
		}
		return []any{}
	case "integer", "number":
		return 0
	case "boolean":
		return false
	case "string":
		switch str(s["format"]) {
		case "date-time":
			return "2026-01-01T00:00:00Z"
		case "date":
			return "2026-01-01"
		case "ipv4":
			return "192.0.2.1"
		case "uuid":
			return "00000000-0000-0000-0000-000000000000"
		}
		return "string"
	}
	return nil
}
