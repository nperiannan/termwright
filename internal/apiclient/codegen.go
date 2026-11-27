package apiclient

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// Code is the request as a curl command, Python (requests), JavaScript (fetch) or PowerShell. Variables are
// filled in except the secret ones; the session's authentication is an environment variable ($API_TOKEN).
func Code(r Request, t *Target, lang string) (string, error) {
	u, err := FullURL(r, t)
	if err != nil {
		return "", err
	}
	m := strings.ToUpper(firstNonEmpty(r.Method, "GET"))
	var hs []KV
	for _, h := range r.Headers {
		if !h.Off && h.Key != "" {
			hs = append(hs, KV{Key: Resolve(h.Key, t.Vars), Value: Resolve(h.Value, t.Vars)})
		}
	}
	env := ""
	mode, ad := r.Auth, r.AuthData
	if mode == "" && t.CollAuth != "" {
		mode, ad = t.CollAuth, t.CollAuthData
	}
	if ad == nil {
		ad = &ReqAuth{}
	}
	switch mode {
	case "bearer":
		env = "API_TOKEN"
		hs = append(hs, KV{Key: "Authorization", Value: "Bearer $API_TOKEN"})
	case "basic":
		env = "API_PASSWORD"
		hs = append(hs, KV{Key: "Authorization", Value: "Basic $(printf '%s' '" + Resolve(ad.User, t.Vars) + ":'\"$API_PASSWORD\" | base64)"})
	case "apikey":
		env = "API_KEY"
		if ad.In != "query" {
			hs = append(hs, KV{Key: Resolve(ad.Key, t.Vars), Value: "$API_KEY"})
		}
	case "oauth2": // the token from the token URL, in an environment variable
		env = "API_TOKEN"
		hs = append(hs, KV{Key: "Authorization", Value: "Bearer $API_TOKEN"})
	}
	if mode == "" {
		switch t.Auth.Type {
		case "apikey":
			env = "API_KEY"
			if t.Auth.KeyIn != "query" {
				hs = append(hs, KV{Key: firstNonEmpty(t.Auth.KeyName, "X-API-Key"), Value: "$API_KEY"})
			}
		case "bearer", "login", "oauth2":
			env = "API_TOKEN"
			if t.Auth.Type == "login" && t.Auth.KeyName != "" {
				hs = append(hs, KV{Key: t.Auth.KeyName, Value: "$API_TOKEN"})
			} else {
				hs = append(hs, KV{Key: "Authorization", Value: "Bearer $API_TOKEN"})
			}
		case "basic":
			env = "API_PASSWORD"
			hs = append(hs, KV{Key: "Authorization", Value: "Basic $(printf '%s' '" + Resolve(t.Auth.User, t.Vars) + ":'\"$API_PASSWORD\" | base64)"})
		}
	}
	body, ctype := "", ""
	switch r.Body.Mode {
	case "json", "text", "xml", "html", "javascript":
		body = Resolve(r.Body.Raw, t.Vars)
		ctype = map[string]string{"json": "application/json", "text": "text/plain", "xml": "application/xml", "html": "text/html", "javascript": "application/javascript"}[r.Body.Mode]
	case "graphql":
		b, _ := json.Marshal(map[string]any{"query": Resolve(r.Body.Raw, t.Vars)})
		body, ctype = string(b), "application/json"
	case "urlencoded":
		var l []string
		for _, f := range r.Body.Form {
			if !f.Off {
				l = append(l, Resolve(f.Key, t.Vars)+"="+Resolve(f.Value, t.Vars))
			}
		}
		body, ctype = strings.Join(l, "&"), "application/x-www-form-urlencoded"
	}
	if ctype != "" && !hasHeader(hs, "Content-Type") {
		hs = append([]KV{{Key: "Content-Type", Value: ctype}}, hs...)
	}
	sq := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	var b strings.Builder
	switch lang {
	case "", "curl":
		fmt.Fprintf(&b, "curl -X %s %s", m, sq(u))
		for _, h := range hs {
			if strings.Contains(h.Value, "$") {
				fmt.Fprintf(&b, " \\\n  -H \"%s: %s\"", h.Key, h.Value)
			} else {
				fmt.Fprintf(&b, " \\\n  -H %s", sq(h.Key+": "+h.Value))
			}
		}
		if r.Body.Mode == "form" {
			for _, f := range r.Body.Form {
				if !f.Off {
					v := Resolve(f.Value, t.Vars)
					if f.Type == "file" {
						v = "@" + v
					}
					fmt.Fprintf(&b, " \\\n  -F %s", sq(Resolve(f.Key, t.Vars)+"="+v))
				}
			}
		} else if r.Body.Mode == "binary" {
			fmt.Fprintf(&b, " \\\n  --data-binary %s", sq("@"+Resolve(r.Body.File, t.Vars)))
		} else if body != "" {
			fmt.Fprintf(&b, " \\\n  --data-raw %s", sq(body))
		}
		if t.Insecure {
			b.WriteString(" \\\n  -k")
		}
	case "python":
		b.WriteString("import os\nimport requests\n\n")
		fmt.Fprintf(&b, "url = %s\nheaders = {\n", pyStr(u))
		for _, h := range hs {
			fmt.Fprintf(&b, "    %s: %s,\n", pyStr(h.Key), pyVal(h.Value))
		}
		b.WriteString("}\n")
		args := "url, headers=headers"
		if body != "" {
			fmt.Fprintf(&b, "data = %s\n", pyStr(body))
			args += ", data=data"
		}
		if t.Insecure {
			args += ", verify=False"
		}
		fmt.Fprintf(&b, "r = requests.request(%s, %s, timeout=30)\nprint(r.status_code)\nprint(r.text)\n", pyStr(m), args)
	case "javascript", "js":
		fmt.Fprintf(&b, "const res = await fetch(%s, {\n  method: %s,\n  headers: {\n", jsStr(u), jsStr(m))
		for _, h := range hs {
			fmt.Fprintf(&b, "    %s: %s,\n", jsStr(h.Key), jsVal(h.Value))
		}
		b.WriteString("  },\n")
		if body != "" {
			fmt.Fprintf(&b, "  body: %s,\n", jsStr(body))
		}
		b.WriteString("});\nconsole.log(res.status, await res.text());\n")
	case "powershell":
		b.WriteString("$headers = @{\n")
		for _, h := range hs {
			v := strings.ReplaceAll(h.Value, "$"+env, "$env:"+env)
			fmt.Fprintf(&b, "  '%s' = \"%s\"\n", h.Key, strings.ReplaceAll(v, `"`, "`\""))
		}
		b.WriteString("}\n")
		fmt.Fprintf(&b, "Invoke-RestMethod -Method %s -Uri '%s' -Headers $headers", m, strings.ReplaceAll(u, "'", "''"))
		if body != "" {
			fmt.Fprintf(&b, " -Body '%s'", strings.ReplaceAll(body, "'", "''"))
		}
		if t.Insecure {
			b.WriteString(" -SkipCertificateCheck")
		}
		b.WriteString("\n")
	default:
		return "", fmt.Errorf("no code for %q (curl, python, javascript, powershell)", lang)
	}
	if env != "" && lang != "powershell" {
		return "# " + env + ": the session's secret (not shown here)\n" + strings.TrimRight(b.String(), "\n") + "\n", nil
	}
	return b.String(), nil
}

func hasHeader(hs []KV, k string) bool {
	for _, h := range hs {
		if strings.EqualFold(h.Key, k) {
			return true
		}
	}
	return false
}

var envRe = regexp.MustCompile(`\$(API_[A-Z]+)`)

func pyStr(s string) string { b, _ := json.Marshal(s); return string(b) }
func pyVal(s string) string {
	if m := envRe.FindStringSubmatch(s); m != nil {
		pre, post, _ := strings.Cut(s, m[0])
		return pyStr(pre) + " + os.environ[" + pyStr(m[1]) + "]" + map[bool]string{true: "", false: " + " + pyStr(post)}[post == ""]
	}
	return pyStr(s)
}
func jsStr(s string) string { b, _ := json.Marshal(s); return string(b) }
func jsVal(s string) string {
	if m := envRe.FindStringSubmatch(s); m != nil {
		return "`" + strings.ReplaceAll(strings.ReplaceAll(s, "`", "\\`"), m[0], "${process.env."+m[1]+"}") + "`"
	}
	return jsStr(s)
}

// ExportPostman writes a collection in Postman v2.1 format (folders, scripts, variables).
func ExportPostman(c Collection) ([]byte, error) {
	type item = map[string]any
	root := []any{}
	folders := map[string]*[]any{"": &root}
	var folderOf func(string) *[]any
	folderOf = func(path string) *[]any {
		if l, ok := folders[path]; ok {
			return l
		}
		parent, name := "", path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			parent, name = path[:i], path[i+1:]
		}
		pl := folderOf(parent)
		l := &[]any{}
		*pl = append(*pl, item{"name": name, "item": l})
		folders[path] = l
		return l
	}
	kv := func(l []KV) []any {
		out := []any{}
		for _, x := range l {
			m := item{"key": x.Key, "value": x.Value}
			if x.Off {
				m["disabled"] = true
			}
			out = append(out, m)
		}
		return out
	}
	for _, r := range c.Requests {
		u := item{"raw": r.URL, "query": kv(r.Params)}
		if len(r.PathVars) > 0 {
			u["variable"] = kv(r.PathVars)
		}
		rq := item{"method": r.Method, "header": kv(r.Headers), "url": u, "description": r.Description}
		switch r.Body.Mode {
		case "json", "text", "xml", "html", "javascript":
			rq["body"] = item{"mode": "raw", "raw": r.Body.Raw, "options": item{"raw": item{"language": r.Body.Mode}}}
		case "binary":
			rq["body"] = item{"mode": "file", "file": item{"src": r.Body.File}}
		case "urlencoded":
			rq["body"] = item{"mode": "urlencoded", "urlencoded": kv(r.Body.Form)}
		case "form":
			fd := []any{}
			for _, x := range r.Body.Form {
				m := item{"key": x.Key, "value": x.Value, "type": "text"}
				if x.Type == "file" {
					m = item{"key": x.Key, "src": x.Value, "type": "file"}
				}
				if x.Off {
					m["disabled"] = true
				}
				fd = append(fd, m)
			}
			rq["body"] = item{"mode": "formdata", "formdata": fd}
		case "graphql":
			rq["body"] = item{"mode": "graphql", "graphql": item{"query": r.Body.Raw, "variables": r.Body.GraphQLVars}}
		}
		if a := postmanAuthOut(r.Auth, r.AuthData); a != nil {
			rq["auth"] = a
		}
		it := item{"name": firstNonEmpty(r.Name, r.Method+" "+r.URL), "request": rq}
		if st := r.Settings; st != nil {
			pb := item{}
			if st.NoRedirects {
				pb["followRedirects"] = false
			}
			if st.Verify != "" {
				pb["strictSSL"] = st.Verify == "on"
			}
			for k, v := range map[string]bool{"followOriginalHttpMethod": st.FollowMethod, "followAuthorizationHeader": st.FollowAuth,
				"removeRefererHeaderOnRedirect": st.RemoveReferer, "disableUrlEncoding": st.NoEncode, "disableCookies": st.NoCookies} {
				if v {
					pb[k] = true
				}
			}
			if st.MaxRedirects > 0 {
				pb["maxRedirects"] = st.MaxRedirects
			}
			if len(pb) > 0 {
				it["protocolProfileBehavior"] = pb
			}
		}
		var ev []any
		if r.PreScript != "" {
			ev = append(ev, item{"listen": "prerequest", "script": item{"type": "text/javascript", "exec": strings.Split(r.PreScript, "\n")}})
		}
		if r.Tests != "" {
			ev = append(ev, item{"listen": "test", "script": item{"type": "text/javascript", "exec": strings.Split(r.Tests, "\n")}})
		}
		if ev != nil {
			it["event"] = ev
		}
		l := folderOf(r.Folder)
		*l = append(*l, it)
	}
	// pointers to slices become slices
	var fix func(any) any
	fix = func(v any) any {
		switch x := v.(type) {
		case *[]any:
			return fix(*x)
		case []any:
			for i := range x {
				x[i] = fix(x[i])
			}
			return x
		case map[string]any:
			for k := range x {
				x[k] = fix(x[k])
			}
		}
		return v
	}
	doc := item{"info": item{"name": c.Name, "description": c.Description, "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": fix(root), "variable": kv(c.Vars)}
	if a := postmanAuthOut(c.Auth, c.AuthData); a != nil {
		doc["auth"] = a
	}
	var ev []any
	if c.PreScript != "" {
		ev = append(ev, item{"listen": "prerequest", "script": item{"type": "text/javascript", "exec": strings.Split(c.PreScript, "\n")}})
	}
	if c.Tests != "" {
		ev = append(ev, item{"listen": "test", "script": item{"type": "text/javascript", "exec": strings.Split(c.Tests, "\n")}})
	}
	if ev != nil {
		doc["event"] = ev
	}
	return json.MarshalIndent(doc, "", "  ")
}

// postmanAuthOut is a request's own authentication in Postman's form (nil: inherit).
func postmanAuthOut(mode string, a *ReqAuth) map[string]any {
	if a == nil {
		a = &ReqAuth{}
	}
	kv := func(p ...string) []any {
		var out []any
		for i := 0; i+1 < len(p); i += 2 {
			out = append(out, map[string]any{"key": p[i], "value": p[i+1], "type": "string"})
		}
		return out
	}
	switch mode {
	case "none":
		return map[string]any{"type": "noauth"}
	case "bearer":
		return map[string]any{"type": "bearer", "bearer": kv("token", a.Token)}
	case "basic":
		return map[string]any{"type": "basic", "basic": kv("username", a.User, "password", a.Password)}
	case "apikey":
		return map[string]any{"type": "apikey", "apikey": kv("key", a.Key, "value", a.Value, "in", firstNonEmpty(a.In, "header"))}
	case "digest":
		return map[string]any{"type": "digest", "digest": kv("username", a.User, "password", a.Password)}
	case "awsv4":
		return map[string]any{"type": "awsv4", "awsv4": kv("accessKey", a.AccessKey, "secretKey", a.SecretKey, "region", a.Region, "service", a.Service, "sessionToken", a.SessionToken)}
	case "oauth2":
		grant := firstNonEmpty(a.Grant, "client_credentials")
		ca := "header"
		if a.ClientAuth == "body" {
			ca = "body"
		}
		return map[string]any{"type": "oauth2", "oauth2": kv("grant_type", grant, "accessTokenUrl", a.TokenURL, "clientId", a.ClientID, "clientSecret", a.ClientSecret,
			"scope", a.Scope, "username", a.User, "password", a.Password, "client_authentication", ca, "accessToken", a.Token, "addTokenTo", "header")}
	}
	return nil
}

// ---- collections on disk ----

// Store keeps collections as JSON files in a folder (plain files: keep them in git, share them).
type Store struct{ Dir string }

var idClean = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// NewID makes a file-safe id from a name, unique in the folder.
func (s Store) NewID(name string) string {
	base := strings.Trim(idClean.ReplaceAllString(strings.ToLower(name), "-"), "-.")
	if base == "" {
		base = "collection"
	}
	id := base
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(s.Dir, id+".json")); os.IsNotExist(err) {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}

func (s Store) path(id string) (string, error) {
	if id == "" || idClean.MatchString(id) || strings.HasPrefix(id, ".") {
		return "", fmt.Errorf("bad collection id %q", id)
	}
	return filepath.Join(s.Dir, id+".json"), nil
}

// List the collections by name.
func (s Store) List() []CollectionInfo {
	ents, _ := os.ReadDir(s.Dir)
	var out []CollectionInfo
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		c, err := s.Get(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		out = append(out, CollectionInfo{ID: c.ID, Name: c.Name, Session: c.Session, Requests: len(c.Requests)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Get a collection by id.
func (s Store) Get(id string) (Collection, error) {
	p, err := s.path(id)
	if err != nil {
		return Collection{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Collection{}, fmt.Errorf("no collection %s", id)
	}
	var c Collection
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("collection %s: %w", id, err)
	}
	c.ID = id
	if c.Requests == nil {
		c.Requests = []Request{}
	}
	return c, nil
}

// Find a collection by id or name (case-insensitive).
func (s Store) Find(idOrName string) (Collection, error) {
	if c, err := s.Get(idOrName); err == nil {
		return c, nil
	}
	for _, i := range s.List() {
		if strings.EqualFold(i.Name, idOrName) {
			return s.Get(i.ID)
		}
	}
	return Collection{}, fmt.Errorf("no collection %q", idOrName)
}

// Save writes a collection (a new id from its name when it has none); request ids are filled in.
func (s Store) Save(c Collection) (Collection, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return c, err
	}
	if strings.TrimSpace(c.Name) == "" {
		c.Name = "Collection"
	}
	if c.ID == "" {
		c.ID = s.NewID(c.Name)
	}
	p, err := s.path(c.ID)
	if err != nil {
		return c, err
	}
	used := map[string]bool{}
	for _, r := range c.Requests {
		used[r.ID] = true
	}
	n := 0
	for i := range c.Requests {
		if c.Requests[i].ID == "" {
			for {
				n++
				if id := fmt.Sprintf("r%d", n); !used[id] {
					c.Requests[i].ID, used[id] = id, true
					break
				}
			}
		}
		if c.Requests[i].Method == "" {
			c.Requests[i].Method = "GET"
		}
		c.Requests[i].AuthData.Scrub()
	}
	c.AuthData.Scrub()
	if c.Requests == nil {
		c.Requests = []Request{}
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return c, err
	}
	return c, os.Rename(tmp, p)
}

// Delete a collection's file.
func (s Store) Delete(id string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// Request finds a request of a collection by id or name.
func (c Collection) Request(idOrName string) (Request, bool) {
	for _, r := range c.Requests {
		if r.ID == idOrName {
			return r, true
		}
	}
	for _, r := range c.Requests {
		if strings.EqualFold(r.Name, idOrName) || strings.EqualFold(strings.Trim(r.Folder+"/"+r.Name, "/"), idOrName) {
			return r, true
		}
	}
	return Request{}, false
}
