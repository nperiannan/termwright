package tgen

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

// Keysight IxNetwork through its REST API (the API server's /api/v1): an API key from the login, a session,
// files, operations (asynchronous: polled until they end) and the statistics views.

type ixOp struct {
	ID     any    `json:"id"`
	State  string `json:"state"`
	URL    string `json:"url"`
	Result any    `json:"result"`
	Msg    any    `json:"message"`
}

// ixDo runs an operation and waits for it to end.
func ixDo(ctx context.Context, c *client, p string, body any) (any, error) {
	var op ixOp
	if err := c.do(ctx, "POST", p, body, &op); err != nil {
		return nil, err
	}
	for i := 0; strings.EqualFold(op.State, "IN_PROGRESS") || strings.EqualFold(op.State, "NOTSTARTED"); i++ {
		if op.URL == "" || i > 1800 {
			return nil, fmt.Errorf("%s: no end in sight (%s)", p, op.State)
		}
		if err := wait(ctx, time.Second); err != nil {
			return nil, err
		}
		u := op.URL
		if strings.HasPrefix(u, "/") {
			u = c.base + u
		}
		if err := c.do(ctx, "GET", u, nil, &op); err != nil {
			return nil, err
		}
	}
	if s := strings.ToUpper(op.State); s != "" && s != "SUCCESS" && s != "COMPLETED" {
		return op.Result, fmt.Errorf("%s: %s %v %v", path.Base(p), op.State, op.Msg, op.Result)
	}
	return op.Result, nil
}

func hrefs(list []map[string]any) []string {
	var out []string
	for _, x := range list {
		if links, ok := x["links"].([]any); ok {
			for _, l := range links {
				if m, ok := l.(map[string]any); ok && fmt.Sprint(m["rel"]) == "self" {
					out = append(out, fmt.Sprint(m["href"]))
				}
			}
			continue
		}
		if h, ok := x["href"].(string); ok {
			out = append(out, h)
		}
	}
	return out
}

func runIxN(ctx context.Context, c *client, j Job, res *Result) (err error) {
	defer func() { res.Log = append(res.Log, c.log...) }()
	c.header = map[string]string{}
	if j.Username != "" { // Linux API servers: an API key from the login
		var auth map[string]any
		if err := c.do(ctx, "POST", "/api/v1/auth/session", map[string]string{"username": j.Username, "password": j.Password}, &auth); err != nil {
			return fmt.Errorf("IxNetwork login: %v", err)
		}
		key, _ := attr(auth, "apiKey").(string)
		if key == "" {
			return fmt.Errorf("IxNetwork login: no apiKey in the answer")
		}
		c.header["X-Api-Key"] = key
	}
	sid := j.Session
	if sid == "" {
		var s map[string]any
		if err := c.do(ctx, "POST", "/api/v1/sessions", map[string]string{"applicationType": "ixnrest"}, &s); err != nil {
			return fmt.Errorf("IxNetwork session: %v", err)
		}
		sid = fmt.Sprint(attr(s, "id"))
		if sid == "" || sid == "<nil>" {
			return fmt.Errorf("IxNetwork session: no id in %v", s)
		}
		if _, err := ixDo(ctx, c, "/api/v1/sessions/"+sid+"/operations/start", map[string]any{"applicationType": "ixnrest"}); err != nil {
			return fmt.Errorf("starting the IxNetwork session: %v", err)
		}
		if !j.Keep {
			defer func() {
				dctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				_, _ = ixDo(dctx, c, "/api/v1/sessions/"+sid+"/operations/stop", map[string]any{})
				_ = c.do(dctx, "DELETE", "/api/v1/sessions/"+sid, nil, nil)
			}()
		}
	}
	res.Session = sid
	ix := "/api/v1/sessions/" + sid + "/ixnetwork"
	if len(j.Config) > 0 {
		name := path.Base(j.ConfigName)
		if err := c.do(ctx, "POST", ix+"/files?filename="+url.QueryEscape(name), j.Config, nil); err != nil {
			return fmt.Errorf("uploading %s: %v", name, err)
		}
		if _, err := ixDo(ctx, c, ix+"/operations/loadconfig", map[string]any{"arg1": ix + "/files/" + name}); err != nil {
			return fmt.Errorf("loading %s: %v", name, err)
		}
	}
	var vports []map[string]any
	if err := c.do(ctx, "GET", ix+"/vport", nil, &vports); err != nil {
		return err
	}
	vp := hrefs(vports)
	if len(vp) == 0 {
		return fmt.Errorf("the configuration has no ports (vports)")
	}
	if len(j.Ports) > 0 {
		if len(j.Ports) > len(vp) {
			return fmt.Errorf("%d chassis ports given, the configuration has %d", len(j.Ports), len(vp))
		}
		var real []map[string]string
		for _, p := range j.Ports {
			parts := strings.Split(strings.TrimPrefix(p, "//"), "/")
			if len(parts) != 3 {
				return fmt.Errorf("chassis port %q: chassis/card/port", p)
			}
			real = append(real, map[string]string{"arg1": parts[0], "arg2": parts[1], "arg3": parts[2]})
		}
		if _, err := ixDo(ctx, c, ix+"/operations/assignports", map[string]any{"arg1": real, "arg2": []string{}, "arg3": vp[:len(real)], "arg4": true}); err != nil {
			return fmt.Errorf("connecting the ports: %v", err)
		}
	}
	if j.Protocols {
		if _, err := ixDo(ctx, c, ix+"/operations/startallprotocols", map[string]any{"arg1": "sync"}); err != nil {
			return fmt.Errorf("starting the protocols: %v", err)
		}
		if err := wait(ctx, time.Duration(max(j.Settle, 5))*time.Second); err != nil {
			return err
		}
	}
	var items []map[string]any
	if err := c.do(ctx, "GET", ix+"/traffic/trafficItem", nil, &items); err != nil {
		return err
	}
	ti := hrefs(items)
	if len(ti) == 0 {
		return fmt.Errorf("the configuration has no traffic items")
	}
	if _, err := ixDo(ctx, c, ix+"/traffic/trafficItem/operations/generate", map[string]any{"arg1": ti}); err != nil {
		return fmt.Errorf("generating the traffic: %v", err)
	}
	if _, err := ixDo(ctx, c, ix+"/traffic/operations/apply", map[string]any{"arg1": ix + "/traffic"}); err != nil {
		return fmt.Errorf("applying the traffic: %v", err)
	}
	if _, err := ixDo(ctx, c, ix+"/traffic/operations/start", map[string]any{"arg1": ix + "/traffic"}); err != nil {
		return fmt.Errorf("starting the traffic: %v", err)
	}
	werr := wait(ctx, time.Duration(j.Duration)*time.Second)
	if _, err := ixDo(context.Background(), c, ix+"/traffic/operations/stop", map[string]any{"arg1": ix + "/traffic"}); err != nil && werr == nil {
		return fmt.Errorf("stopping the traffic: %v", err)
	}
	if werr != nil {
		return werr
	}
	_ = wait(ctx, 3*time.Second)
	var views []map[string]any
	if err := c.do(ctx, "GET", ix+"/statistics/view", nil, &views); err != nil {
		return err
	}
	vid := ""
	for _, v := range views {
		if strings.EqualFold(fmt.Sprint(attr(v, "caption")), "Traffic Item Statistics") {
			vid = fmt.Sprint(attr(v, "id"))
		}
	}
	if vid == "" {
		return fmt.Errorf("no Traffic Item Statistics view")
	}
	var page map[string]any
	if err := c.do(ctx, "GET", ix+"/statistics/view/"+vid+"/page", nil, &page); err != nil {
		return err
	}
	cols := map[string]int{}
	if cc, ok := attr(page, "columnCaptions").([]any); ok {
		for i, x := range cc {
			cols[strings.ToLower(fmt.Sprint(x))] = i
		}
	}
	col := func(row []any, names ...string) (any, bool) {
		for _, n := range names {
			if i, ok := cols[strings.ToLower(n)]; ok && i < len(row) {
				return row[i], true
			}
		}
		return nil, false
	}
	for _, row := range ixRows(attr(page, "rowValues")) {
		s := Stream{}
		if v, ok := col(row, "Traffic Item"); ok {
			s.Name = fmt.Sprint(v)
		}
		if v, ok := col(row, "Tx Frames"); ok {
			s.TxFrames = num(v)
		}
		if v, ok := col(row, "Rx Frames"); ok {
			s.RxFrames = num(v)
		}
		if v, ok := col(row, "Loss %"); ok {
			s.Loss = num(v)
		} else if s.TxFrames > 0 {
			s.Loss = round3(100 * (s.TxFrames - s.RxFrames) / s.TxFrames)
		}
		if v, ok := col(row, "Store-Forward Avg Latency (ns)", "Cut-Through Avg Latency (ns)"); ok {
			s.LatencyUs = num(v) / 1000
		}
		res.Streams = append(res.Streams, s)
	}
	return nil
}

// ixRows reads a view page's rows: {"arg1": [[...]], "arg2": …} or [[[...]]], in page order.
func ixRows(v any) [][]any {
	var out [][]any
	var walk func(x any)
	walk = func(x any) {
		l, ok := x.([]any)
		if !ok {
			return
		}
		if len(l) > 0 {
			if _, nested := l[0].([]any); !nested { // a row
				out = append(out, l)
				return
			}
		}
		for _, y := range l {
			walk(y)
		}
	}
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walk(t[k])
		}
	default:
		walk(t)
	}
	return out
}
