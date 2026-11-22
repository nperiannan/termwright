// Package tgen runs traffic on commercial traffic generators through their REST APIs: Spirent TestCenter
// (the STC REST API of a LabServer or a TestCenter REST server) and Keysight IxNetwork (the IxNetwork API
// server, Linux or Windows). A run loads a saved configuration, connects its ports to the chassis ports,
// starts the protocols, sends the traffic for a while and reads the per-stream counters.
//
// Only the standard library is used, so the standalone suites carry it too.
package tgen

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Job is one traffic run.
type Job struct {
	// Kind: stc (Spirent TestCenter) or ixnetwork.
	Kind string `json:"kind"`
	// Server: the REST API's address, e.g. http://10.1.1.20 (STC LabServer / REST server) or
	// https://10.1.1.30 (IxNetwork API server).
	Server   string `json:"server"`
	Username string `json:"username,omitempty"`
	Password string `json:"-"`
	Insecure bool   `json:"insecure,omitempty"`
	// Config: the saved configuration (STC .xml or .tcc; IxNetwork .ixncfg or .json) - its bytes and name.
	Config     []byte `json:"-"`
	ConfigName string `json:"configName,omitempty"`
	// Ports: the chassis ports in the order of the configuration's ports: "chassis/slot/port"
	// (10.1.1.5/1/1). Empty: the configuration's own locations.
	Ports []string `json:"ports,omitempty"`
	// Duration: seconds of traffic (default 10).
	Duration int `json:"duration,omitempty"`
	// Protocols: start the configuration's protocols (devices, BGP…) first, and wait Settle seconds.
	Protocols bool `json:"protocols,omitempty"`
	Settle    int  `json:"settle,omitempty"`
	// Session: an existing IxNetwork session id to use (Windows API servers have session 1); "" makes one.
	Session string `json:"session,omitempty"`
	// Keep: leave the session (and the configuration) on the server when done.
	Keep bool `json:"keep,omitempty"`
}

// Stream is one stream's (STC stream block's, IxNetwork traffic item's) counters.
type Stream struct {
	Name     string  `json:"name"`
	TxFrames float64 `json:"tx"`
	RxFrames float64 `json:"rx"`
	// Loss: lost frames in percent of the sent ones.
	Loss float64 `json:"loss"`
	// LatencyUs: the average latency in microseconds (0: not measured).
	LatencyUs float64 `json:"latencyUs,omitempty"`
}

// Result is what a run measured.
type Result struct {
	Kind      string   `json:"kind"`
	Server    string   `json:"server"`
	Duration  int      `json:"duration"`
	Streams   []Stream `json:"streams"`
	TxFrames  float64  `json:"tx"`
	RxFrames  float64  `json:"rx"`
	Loss      float64  `json:"loss"`
	Ports     []string `json:"ports,omitempty"`
	Session   string   `json:"session,omitempty"`
	Log       []string `json:"log,omitempty"`
	StartedAt string   `json:"startedAt"`
}

// Run runs a job on its generator.
func Run(ctx context.Context, j Job) (Result, error) {
	if j.Duration <= 0 {
		j.Duration = 10
	}
	if strings.TrimSpace(j.Server) == "" {
		return Result{}, errors.New("no traffic generator server: give the REST API's address")
	}
	if !strings.Contains(j.Server, "://") {
		j.Server = "http://" + j.Server
	}
	j.Server = strings.TrimRight(j.Server, "/")
	c := &client{base: j.Server, hc: &http.Client{Timeout: 5 * time.Minute}}
	if j.Insecure || strings.HasPrefix(j.Server, "https://") && j.Kind == "ixnetwork" {
		// API servers come with self-signed certificates
		c.hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec
	}
	res := Result{Kind: j.Kind, Server: j.Server, Duration: j.Duration, Ports: j.Ports, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	var err error
	switch strings.ToLower(j.Kind) {
	case "stc", "spirent", "testcenter":
		res.Kind = "stc"
		err = runSTC(ctx, c, j, &res)
	case "ixnetwork", "ixia", "ixn":
		res.Kind = "ixnetwork"
		err = runIxN(ctx, c, j, &res)
	default:
		return res, fmt.Errorf("traffic generator %q: stc or ixnetwork", j.Kind)
	}
	for _, s := range res.Streams {
		res.TxFrames += s.TxFrames
		res.RxFrames += s.RxFrames
	}
	if res.TxFrames > 0 {
		res.Loss = round3(100 * (res.TxFrames - res.RxFrames) / res.TxFrames)
	}
	return res, err
}

func round3(f float64) float64 { return float64(int64(f*1000+0.5)) / 1000 }

// Text is a result for a run log: one line per stream.
func (r Result) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s · %d s · tx %.0f · rx %.0f · loss %.3f %%\n", r.Kind, r.Server, r.Duration, r.TxFrames, r.RxFrames, r.Loss)
	for _, s := range r.Streams {
		fmt.Fprintf(&b, "  %-32s tx %-12.0f rx %-12.0f loss %7.3f %%", s.Name, s.TxFrames, s.RxFrames, s.Loss)
		if s.LatencyUs > 0 {
			fmt.Fprintf(&b, "  latency %.1f µs", s.LatencyUs)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// ---- HTTP ----

type client struct {
	base   string
	hc     *http.Client
	header map[string]string
	log    []string
}

func (c *client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	ct := "application/json"
	switch b := body.(type) {
	case nil:
	case []byte:
		rd, ct = bytes.NewReader(b), "application/octet-stream"
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	u := path
	if !strings.Contains(path, "://") {
		u = c.base + path
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	if rd != nil {
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	c.log = append(c.log, fmt.Sprintf("%s %s → %d", method, path, res.StatusCode))
	if res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(rb))
		if len(msg) > 400 {
			msg = msg[:400] + "…"
		}
		return fmt.Errorf("%s %s: %s %s", method, path, res.Status, msg)
	}
	if out != nil && len(bytes.TrimSpace(rb)) > 0 {
		if err := json.Unmarshal(rb, out); err != nil {
			return fmt.Errorf("%s %s: the answer is not JSON: %v", method, path, err)
		}
	}
	return nil
}

func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func num(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		var f float64
		fmt.Sscanf(strings.ReplaceAll(strings.TrimSpace(t), ",", ""), "%g", &f)
		return f
	case json.Number:
		f, _ := t.Float64()
		return f
	}
	return 0
}
