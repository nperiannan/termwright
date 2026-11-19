package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/termwright/termwright/internal/apiclient"
)

// httpTool is the "http" network tool (AI assistant, MCP, POST /tools/http).
func (a *App) httpTool(ctx context.Context, args map[string]any, ask netAsker) (string, error) {
	act := strings.ToLower(argStr(args, "action"))
	switch act {
	case "collections":
		var b strings.Builder
		for _, c := range a.HTTPCollections() {
			fmt.Fprintf(&b, "%s  %q  %d requests", c.ID, c.Name, c.Requests)
			if c.Session != "" {
				fmt.Fprintf(&b, "  session %s", c.Session)
			}
			b.WriteString("\n")
		}
		if b.Len() == 0 {
			return "No collections yet (import one, or save requests).", nil
		}
		return b.String(), nil
	case "collection":
		c, err := a.httpStore().Find(argStr(args, "collection"))
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s (%s)", c.Name, c.ID)
		if c.Session != "" {
			fmt.Fprintf(&b, ", session %s", c.Session)
		}
		b.WriteString("\n")
		for _, v := range c.Vars {
			fmt.Fprintf(&b, "  var %s = %s\n", v.Key, v.Value)
		}
		for _, r := range c.Requests {
			fmt.Fprintf(&b, "%s  %-6s %s  %s", r.ID, r.Method, r.URL, r.Name)
			if r.Folder != "" {
				fmt.Fprintf(&b, "  [%s]", r.Folder)
			}
			if r.Tests != "" {
				b.WriteString("  (tests)")
			}
			b.WriteString("\n")
		}
		return b.String(), nil
	case "history":
		var b strings.Builder
		for i, h := range a.HTTPHistory() {
			if i >= 30 {
				break
			}
			fmt.Fprintf(&b, "%s %s %s %d %d ms %s\n", h.At.Format(time.DateTime), h.Request.Method, h.URL, h.Status, h.Ms, h.Error)
		}
		if b.Len() == 0 {
			return "No requests sent yet.", nil
		}
		return b.String(), nil
	case "import":
		c, err := a.HTTPImport(argStr(args, "text"), argStr(args, "name"), argStr(args, "session"))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Imported %q (%s) with %d requests.", c.Name, c.ID, len(c.Requests)), nil
	case "save":
		return a.httpToolSave(args)
	case "run":
		return a.httpToolRun(ctx, args, ask)
	case "send", "":
		return a.httpToolSend(ctx, args, ask)
	}
	return "", fmt.Errorf("unknown action %q", act)
}

func (a *App) httpToolRequest(args map[string]any) (apiclient.Request, string, error) {
	if coll := argStr(args, "collection"); coll != "" {
		if id, ok := args["request"].(string); ok && id != "" {
			c, err := a.httpStore().Find(coll)
			if err != nil {
				return apiclient.Request{}, "", err
			}
			r, ok := c.Request(id)
			if !ok {
				return r, "", fmt.Errorf("no request %q in %s", id, c.Name)
			}
			return r, c.ID, nil
		}
	}
	r := apiclient.Request{Method: strings.ToUpper(argStr(args, "method")), URL: argStr(args, "url")}
	if r.Method == "" {
		r.Method = "GET"
	}
	if r.URL == "" {
		return r, "", errors.New("give url (or collection and request)")
	}
	r.Headers = kvs(args["headers"])
	r.Params = kvs(args["query"])
	switch b := args["body"].(type) {
	case nil:
	case string:
		r.Body = apiclient.Body{Mode: "text", Raw: b}
		if t := strings.TrimSpace(b); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
			r.Body.Mode = "json"
		}
	default:
		j, _ := json.MarshalIndent(b, "", "  ")
		r.Body = apiclient.Body{Mode: "json", Raw: string(j)}
	}
	return r, argStr(args, "collection"), nil
}

func kvs(v any) []apiclient.KV {
	m, _ := v.(map[string]any)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []apiclient.KV
	for _, k := range keys {
		out = append(out, apiclient.KV{Key: k, Value: fmt.Sprint(m[k])})
	}
	return out
}

func (a *App) httpToolSend(ctx context.Context, args map[string]any, ask netAsker) (string, error) {
	r, coll, err := a.httpToolRequest(args)
	if err != nil {
		return "", err
	}
	req := HTTPSendRequest{Session: argStr(args, "session"), Env: argStr(args, "env"), Collection: coll, Request: r}
	if req.Session == "" && coll != "" {
		if c, err := a.httpStore().Find(coll); err == nil {
			req.Session = c.Session
		}
	}
	if apiclient.Changes(r.Method) {
		_, _, prod, err := a.httpTarget(req)
		if err != nil {
			return "", err
		}
		where := req.Session
		if where == "" {
			where = r.URL
		}
		msg := r.Method + " " + r.URL
		if r.Body.Raw != "" {
			msg += "\n" + clip(r.Body.Raw, 600)
		}
		if err := ask(r.Method+" on "+where, msg, prod); err != nil {
			return "", err
		}
		req.Confirmed = true
	}
	res, err := a.httpSend(ctx, req)
	if err != nil {
		return "", err
	}
	return httpText(res), nil
}

// httpText is an answer for the AI: status, time, a few headers, the body (cut), tests.
func httpText(res apiclient.Response) string {
	var b strings.Builder
	if res.Status == 0 {
		return res.Method + " " + res.URL + ": not sent - " + res.Error
	}
	fmt.Fprintf(&b, "%s %s → %s · %d ms · %d bytes\n", res.Method, res.URL, res.StatusText, res.Ms, res.Size)
	for _, h := range res.Headers {
		switch strings.ToLower(h.Key) {
		case "content-type", "location", "etag", "retry-after", "x-total-count", "link":
			fmt.Fprintf(&b, "%s: %s\n", h.Key, h.Value)
		}
	}
	body := res.Body
	if res.Base64 {
		body = fmt.Sprintf("(binary, %d bytes)", res.Size)
	} else if strings.Contains(res.ContentType, "json") {
		var v any
		if json.Unmarshal([]byte(body), &v) == nil {
			if j, err := json.MarshalIndent(v, "", " "); err == nil {
				body = string(j)
			}
		}
	}
	b.WriteString("\n" + clip(body, 20000) + "\n")
	for _, t := range res.Tests {
		mark := "PASS"
		if !t.Pass {
			mark = "FAIL " + t.Error
		}
		fmt.Fprintf(&b, "test %s: %s\n", t.Name, mark)
	}
	if res.Error != "" {
		b.WriteString("error: " + res.Error + "\n")
	}
	return b.String()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("\n… (%d more bytes)", len(s)-n)
}

func (a *App) httpToolRun(ctx context.Context, args map[string]any, ask netAsker) (string, error) {
	r := HTTPRunRequest{Collection: argStr(args, "collection"), Session: argStr(args, "session"), Env: argStr(args, "env"), Folder: argStr(args, "folder"),
		Iterations: argInt(args, "iterations", 1, 1000), Data: argStr(args, "data")}
	if id, ok := args["request"].(string); ok && id != "" {
		r.Requests = []string{id}
	}
	c, err := a.httpStore().Find(r.Collection)
	if err != nil {
		return "", err
	}
	changes := 0
	for _, rq := range c.Requests {
		if apiclient.Changes(rq.Method) && (len(r.Requests) == 0 || rq.ID == r.Requests[0] || strings.EqualFold(rq.Name, r.Requests[0])) {
			changes++
		}
	}
	if changes > 0 {
		sess := r.Session
		if sess == "" {
			sess = c.Session
		}
		_, _, prod, err := a.httpTarget(HTTPSendRequest{Session: sess, Env: r.Env})
		if err != nil {
			return "", err
		}
		if err := ask("Run "+c.Name, fmt.Sprintf("Runs %s with %d requests that change the server (POST, PUT, PATCH, DELETE).", c.Name, changes), prod); err != nil {
			return "", err
		}
		r.Confirmed = true
	}
	res, err := a.httpRun(ctx, r)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d passed, %d failed, %d tests, %d ms%s\n", c.Name, res.Passed, res.Failed, res.Tests, res.Ms, map[bool]string{true: " (stopped)"}[res.Stopped])
	for _, s := range res.Steps {
		mark := "ok  "
		if !s.Pass {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "%s #%d %s %s %d %d ms %s\n", mark, s.Iteration, s.Method, s.Name, s.Status, s.Ms, s.Error)
		for _, t := range s.Tests {
			if !t.Pass {
				fmt.Fprintf(&b, "     test %s: %s\n", t.Name, t.Error)
			}
		}
	}
	return b.String(), nil
}

func (a *App) httpToolSave(args map[string]any) (string, error) {
	st := a.httpStore()
	name := argStr(args, "collection")
	if name == "" {
		name = argStr(args, "name")
	}
	if name == "" {
		return "", errors.New("give the collection")
	}
	c, err := st.Find(name)
	if err != nil {
		c = apiclient.Collection{Name: name, Session: argStr(args, "session")}
	}
	var r apiclient.Request
	b, _ := json.Marshal(args["request"])
	if err := json.Unmarshal(b, &r); err != nil || r.URL == "" {
		return "", errors.New("request: an object with at least url (and method, name, headers, body)")
	}
	r.Method = strings.ToUpper(r.Method)
	done := false
	for i, x := range c.Requests {
		if (r.ID != "" && x.ID == r.ID) || (r.ID == "" && r.Name != "" && strings.EqualFold(x.Name, r.Name) && x.Folder == r.Folder) {
			r.ID = x.ID
			c.Requests[i], done = r, true
			break
		}
	}
	if !done {
		r.ID = ""
		c.Requests = append(c.Requests, r)
	}
	c, err = st.Save(c)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Saved in %q (%s); it has %d requests.", c.Name, c.ID, len(c.Requests)), nil
}
