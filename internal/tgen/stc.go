package tgen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Spirent TestCenter through its REST API (the stcweb / LabServer REST service, as stcrestclient uses it):
// sessions, files, objects (get / put with attributes) and perform (commands).

func stcGet(ctx context.Context, c *client, handle string, attrs ...string) (map[string]any, error) {
	q := ""
	if len(attrs) > 0 {
		q = "?" + strings.Join(attrs, "&")
	}
	var raw any
	if err := c.do(ctx, "GET", "/stcapi/objects/"+url.PathEscape(handle)+q, nil, &raw); err != nil {
		return nil, err
	}
	switch t := raw.(type) {
	case map[string]any:
		return t, nil
	case string: // one attribute asked: the value itself
		if len(attrs) == 1 {
			return map[string]any{attrs[0]: t}, nil
		}
	}
	return map[string]any{}, nil
}

func handles(v any) []string {
	s, _ := v.(string)
	return strings.Fields(s)
}

func attr(m map[string]any, name string) any {
	if v, ok := m[name]; ok {
		return v
	}
	for k, v := range m { // attribute names are case-insensitive
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}

func stcPerform(ctx context.Context, c *client, cmd string, args map[string]any) (map[string]any, error) {
	body := map[string]any{"command": cmd}
	for k, v := range args {
		body[k] = v
	}
	out := map[string]any{}
	err := c.do(ctx, "POST", "/stcapi/perform", body, &out)
	return out, err
}

func stcUpload(ctx context.Context, c *client, name string, data []byte) error {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("file", name)
	if err != nil {
		return err
	}
	_, _ = fw.Write(data)
	w.Close()
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/stcapi/files/", &b)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("uploading %s: %v", name, err)
	}
	defer res.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	c.log = append(c.log, fmt.Sprintf("POST /stcapi/files/ (%s) → %d", name, res.StatusCode))
	if res.StatusCode >= 300 {
		return fmt.Errorf("uploading %s: %s %s", name, res.Status, strings.TrimSpace(string(rb)))
	}
	return nil
}

func runSTC(ctx context.Context, c *client, j Job, res *Result) (err error) {
	defer func() { res.Log = append(res.Log, c.log...) }()
	user := j.Username
	if user == "" {
		user = "termwright"
	}
	var sess map[string]any
	if err := c.do(ctx, "POST", "/stcapi/sessions/", map[string]string{"userid": user, "sessionname": "tw-" + time.Now().Format("150405")}, &sess); err != nil {
		return fmt.Errorf("STC session: %v", err)
	}
	id, _ := attr(sess, "session_id").(string)
	if id == "" {
		return fmt.Errorf("STC session: no session_id in %v", sess)
	}
	res.Session = id
	c.header = map[string]string{"X-STC-API-Session": id}
	if !j.Keep {
		defer func() {
			dctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = c.do(dctx, "DELETE", "/stcapi/sessions/"+url.PathEscape(id), nil, nil)
		}()
	}
	name := path.Base(j.ConfigName)
	if len(j.Config) > 0 {
		if err := stcUpload(ctx, c, name, j.Config); err != nil {
			return err
		}
		load := map[string]any{"FileName": name}
		cmd := "LoadFromXml"
		if strings.HasSuffix(strings.ToLower(name), ".tcc") {
			cmd, load = "LoadFromDatabase", map[string]any{"DatabaseConnectionString": name}
		}
		if _, err := stcPerform(ctx, c, cmd, load); err != nil {
			return fmt.Errorf("loading %s: %v", name, err)
		}
	}
	proj, err := stcGet(ctx, c, "project1", "children-port")
	if err != nil {
		return err
	}
	ports := handles(attr(proj, "children-port"))
	if len(ports) == 0 {
		return fmt.Errorf("the configuration has no ports")
	}
	for i, loc := range j.Ports {
		if i >= len(ports) {
			return fmt.Errorf("%d chassis ports given, the configuration has %d", len(j.Ports), len(ports))
		}
		if !strings.HasPrefix(loc, "//") {
			loc = "//" + loc
		}
		if err := c.do(ctx, "PUT", "/stcapi/objects/"+ports[i], map[string]string{"location": loc}, nil); err != nil {
			return err
		}
	}
	if _, err := stcPerform(ctx, c, "AttachPorts", map[string]any{"PortList": strings.Join(ports, " "), "AutoConnect": "true"}); err != nil {
		return fmt.Errorf("reserving the ports: %v", err)
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = stcPerform(rctx, c, "DetachPorts", map[string]any{"PortList": strings.Join(ports, " ")})
	}()
	for _, rt := range []string{"RxStreamSummaryResults", "TxStreamResults"} {
		if _, err := stcPerform(ctx, c, "ResultsSubscribe", map[string]any{"Parent": "project1", "ConfigType": "StreamBlock", "ResultType": rt}); err != nil {
			return fmt.Errorf("subscribing to %s: %v", rt, err)
		}
	}
	if j.Protocols {
		if _, err := stcPerform(ctx, c, "DevicesStartAll", nil); err != nil {
			return fmt.Errorf("starting the devices: %v", err)
		}
		if err := wait(ctx, time.Duration(max(j.Settle, 5))*time.Second); err != nil {
			return err
		}
	}
	var gens []string
	for _, p := range ports {
		m, err := stcGet(ctx, c, p, "children-generator")
		if err != nil {
			return err
		}
		gens = append(gens, handles(attr(m, "children-generator"))...)
	}
	if len(gens) == 0 {
		return fmt.Errorf("the ports have no generators")
	}
	if _, err := stcPerform(ctx, c, "GeneratorStart", map[string]any{"GeneratorList": strings.Join(gens, " ")}); err != nil {
		return fmt.Errorf("starting the traffic: %v", err)
	}
	werr := wait(ctx, time.Duration(j.Duration)*time.Second)
	if _, err := stcPerform(context.Background(), c, "GeneratorStop", map[string]any{"GeneratorList": strings.Join(gens, " ")}); err != nil && werr == nil {
		return fmt.Errorf("stopping the traffic: %v", err)
	}
	if werr != nil {
		return werr
	}
	_ = wait(ctx, 2*time.Second) // the last frames arrive and the counters settle
	var sbs []string
	for _, p := range ports {
		m, err := stcGet(ctx, c, p, "children-streamblock")
		if err != nil {
			return err
		}
		sbs = append(sbs, handles(attr(m, "children-streamblock"))...)
	}
	for _, sb := range sbs {
		m, err := stcGet(ctx, c, sb, "Name", "children-rxstreamsummaryresults", "children-txstreamresults")
		if err != nil {
			return err
		}
		s := Stream{Name: fmt.Sprint(attr(m, "Name"))}
		if s.Name == "" || s.Name == "<nil>" {
			s.Name = sb
		}
		for _, h := range handles(attr(m, "children-txstreamresults")) {
			r, err := stcGet(ctx, c, h, "FrameCount")
			if err == nil {
				s.TxFrames += num(attr(r, "FrameCount"))
			}
		}
		lat := 0.0
		for _, h := range handles(attr(m, "children-rxstreamsummaryresults")) {
			r, err := stcGet(ctx, c, h, "FrameCount", "AvgLatency")
			if err == nil {
				s.RxFrames += num(attr(r, "FrameCount"))
				lat = num(attr(r, "AvgLatency"))
			}
		}
		s.LatencyUs = lat
		if s.TxFrames > 0 {
			s.Loss = round3(100 * (s.TxFrames - s.RxFrames) / s.TxFrames)
		}
		res.Streams = append(res.Streams, s)
	}
	return nil
}

var _ = json.Marshal
