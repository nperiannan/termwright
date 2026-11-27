package apiclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

// gRPC calls over HTTP/2 (TLS or plain h2c) with the schema from .proto files or the server's reflection.
// All four kinds are supported: unary, server streaming, client streaming and bidirectional (the request
// messages are sent, then the answers are read).

// GRPCSpec is a gRPC request's own settings (Request.Method is "GRPC", the message is Body.Raw and
// metadata are Headers).
type GRPCSpec struct {
	// Method: "package.Service/Method".
	Method string `json:"method,omitempty"`
	// Proto: .proto files (separated by ";"); none: the server's reflection.
	Proto string `json:"proto,omitempty"`
	// ImportPaths: folders where imports are looked up (separated by ";").
	ImportPaths string `json:"importPaths,omitempty"`
	// TLS: "" (by the URL: grpcs:// or https:// or port 443), "on" or "off".
	TLS string `json:"tls,omitempty"`
}

// GRPCMethodInfo describes a method for the editor.
type GRPCMethodInfo struct {
	Name         string `json:"name"` // package.Service/Method
	Service      string `json:"service"`
	Method       string `json:"method"`
	Input        string `json:"input"`
	Output       string `json:"output"`
	ClientStream bool   `json:"clientStream,omitempty"`
	ServerStream bool   `json:"serverStream,omitempty"`
	// Example: a JSON request message with every field.
	Example string `json:"example"`
}

// GRPCSchema is what a server or .proto files offer.
type GRPCSchema struct {
	Methods  []GRPCMethodInfo `json:"methods"`
	Warnings []string         `json:"warnings,omitempty"`
}

var grpcCodes = []string{"OK", "CANCELLED", "UNKNOWN", "INVALID_ARGUMENT", "DEADLINE_EXCEEDED", "NOT_FOUND", "ALREADY_EXISTS", "PERMISSION_DENIED",
	"RESOURCE_EXHAUSTED", "FAILED_PRECONDITION", "ABORTED", "OUT_OF_RANGE", "UNIMPLEMENTED", "INTERNAL", "UNAVAILABLE", "DATA_LOSS", "UNAUTHENTICATED"}

// GRPCCodeName is the name of a gRPC status code.
func GRPCCodeName(c int) string {
	if c >= 0 && c < len(grpcCodes) {
		return grpcCodes[c]
	}
	return "CODE_" + strconv.Itoa(c)
}

// an HTTP status for a gRPC status, so tests and the runner can use pm.response.code.
func grpcHTTPStatus(c int) int {
	switch c {
	case 0:
		return 200
	case 1:
		return 499
	case 3, 9, 11:
		return 400
	case 4:
		return 504
	case 5:
		return 404
	case 6, 10:
		return 409
	case 7:
		return 403
	case 8:
		return 429
	case 12:
		return 501
	case 14:
		return 503
	case 16:
		return 401
	}
	return 500
}

type grpcAddr struct {
	base string // https://host:port or http://host:port
	tls  bool
}

func grpcTarget(raw string, spec GRPCSpec, t *Target) (grpcAddr, error) {
	u := strings.TrimSpace(Resolve(raw, t.Vars))
	if u == "" {
		return grpcAddr{}, errors.New("the server address (host:port) is needed")
	}
	if strings.Contains(u, "{{") {
		return grpcAddr{}, fmt.Errorf("the address has a variable without a value: %s", u)
	}
	useTLS := false
	switch {
	case strings.HasPrefix(u, "grpcs://"), strings.HasPrefix(u, "https://"):
		useTLS, u = true, u[strings.Index(u, "://")+3:]
	case strings.HasPrefix(u, "grpc://"), strings.HasPrefix(u, "http://"):
		u = u[strings.Index(u, "://")+3:]
	default:
		if strings.HasPrefix(t.BaseURL, "https://") && !strings.Contains(u, ":") {
			useTLS = true
		}
		if strings.HasSuffix(u, ":443") {
			useTLS = true
		}
	}
	if i := strings.Index(u, "/"); i >= 0 {
		u = u[:i]
	}
	switch spec.TLS {
	case "on":
		useTLS = true
	case "off":
		useTLS = false
	}
	if !strings.Contains(u, ":") || strings.HasSuffix(u, "]") {
		if useTLS {
			u += ":443"
		} else {
			u += ":80"
		}
	}
	if useTLS {
		return grpcAddr{base: "https://" + u, tls: true}, nil
	}
	return grpcAddr{base: "http://" + u}, nil
}

func (t *Target) grpcClient(a grpcAddr) (*http.Client, error) {
	c, err := t.client(Request{})
	if err != nil {
		return nil, err
	}
	var tc *tls.Config
	if ht, ok := c.Transport.(*http.Transport); ok && ht.TLSClientConfig != nil {
		tc = ht.TLSClientConfig.Clone()
	} else {
		tc = &tls.Config{}
	}
	tc.NextProtos = []string{"h2"}
	tr := &http2.Transport{TLSClientConfig: tc, AllowHTTP: !a.tls, ReadIdleTimeout: 30 * time.Second}
	if !a.tls {
		tr.DialTLSContext = func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}
	}
	return &http.Client{Transport: tr}, nil
}

func grpcFrame(msg []byte) []byte {
	b := make([]byte, 5, 5+len(msg))
	binary.BigEndian.PutUint32(b[1:], uint32(len(msg)))
	return append(b, msg...)
}

type grpcAnswer struct {
	status   int
	message  string
	headers  []KV
	trailers []KV
	msgs     [][]byte
	ms       int64
}

// grpcInvoke sends the messages on one stream and reads every answer.
func grpcInvoke(ctx context.Context, c *http.Client, a grpcAddr, path string, md http.Header, msgs [][]byte, timeout time.Duration) (grpcAnswer, error) {
	var ans grpcAnswer
	var body bytes.Buffer
	for _, m := range msgs {
		body.Write(grpcFrame(m))
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+path, bytes.NewReader(body.Bytes()))
	if err != nil {
		return ans, err
	}
	for k, vs := range md {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")
	req.Header.Set("Grpc-Accept-Encoding", "gzip")
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "termwright-grpc")
	}
	if timeout > 0 {
		req.Header.Set("Grpc-Timeout", strconv.FormatInt(timeout.Milliseconds(), 10)+"m")
	}
	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		return ans, err
	}
	defer resp.Body.Close()
	ans.headers = sortedKV(resp.Header)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ans, fmt.Errorf("the server answered HTTP %s (not a gRPC server?) %s", resp.Status, strings.TrimSpace(string(b)))
	}
	gz := resp.Header.Get("Grpc-Encoding") == "gzip"
	hdr := make([]byte, 5)
	for {
		if _, err := io.ReadFull(resp.Body, hdr); err != nil {
			if err != io.EOF {
				return ans, err
			}
			break
		}
		n := binary.BigEndian.Uint32(hdr[1:])
		if n > MaxBody {
			return ans, fmt.Errorf("an answer of %d bytes is too big", n)
		}
		m := make([]byte, n)
		if _, err := io.ReadFull(resp.Body, m); err != nil {
			return ans, err
		}
		if hdr[0] == 1 {
			if !gz {
				return ans, errors.New("a compressed answer without grpc-encoding")
			}
			zr, err := gzip.NewReader(bytes.NewReader(m))
			if err != nil {
				return ans, err
			}
			if m, err = io.ReadAll(io.LimitReader(zr, MaxBody)); err != nil {
				return ans, err
			}
		}
		ans.msgs = append(ans.msgs, m)
	}
	ans.ms = time.Since(start).Milliseconds()
	tr := resp.Trailer
	if tr.Get("Grpc-Status") == "" { // a trailers-only answer
		tr = resp.Header
	} else {
		ans.trailers = sortedKV(resp.Trailer)
	}
	code := tr.Get("Grpc-Status")
	if code == "" {
		return ans, errors.New("the answer has no grpc-status")
	}
	ans.status, _ = strconv.Atoi(code)
	ans.message, _ = url.PathUnescape(tr.Get("Grpc-Message"))
	return ans, nil
}

// ---- server reflection ----

func grpcReflect(ctx context.Context, c *http.Client, a grpcAddr, md http.Header) (*ProtoSet, error) {
	ps := newProtoSet()
	var svc string
	ask := func(req []byte) ([]byte, error) {
		for _, s := range []string{svc, "grpc.reflection.v1.ServerReflection", "grpc.reflection.v1alpha.ServerReflection"} {
			if s == "" {
				continue
			}
			ans, err := grpcInvoke(ctx, c, a, "/"+s+"/ServerReflectionInfo", md, [][]byte{req}, 20*time.Second)
			if err != nil {
				return nil, err
			}
			if ans.status == 12 && svc == "" { // UNIMPLEMENTED: the older name
				continue
			}
			if ans.status != 0 {
				return nil, fmt.Errorf("reflection: %s %s", GRPCCodeName(ans.status), ans.message)
			}
			svc = s
			if len(ans.msgs) == 0 {
				return nil, errors.New("reflection: no answer")
			}
			return ans.msgs[0], nil
		}
		return nil, errors.New("the server has no reflection service: choose its .proto files")
	}
	parseAns := func(b []byte) (services []string, files [][]byte, err error) {
		err = pbEach(b, func(num, wt int, v uint64, d []byte) {
			switch num {
			case 4:
				_ = pbEach(d, func(n, wt int, v uint64, d []byte) {
					if n == 1 {
						files = append(files, d)
					}
				})
			case 6:
				_ = pbEach(d, func(n, wt int, v uint64, d []byte) {
					if n == 1 {
						_ = pbEach(d, func(n, wt int, v uint64, d []byte) {
							if n == 1 {
								services = append(services, string(d))
							}
						})
					}
				})
			case 7:
				msg := ""
				_ = pbEach(d, func(n, wt int, v uint64, d []byte) {
					if n == 2 {
						msg = string(d)
					}
				})
				if err == nil {
					err = errors.New("reflection: " + msg)
				}
			}
		})
		return
	}
	b, err := ask(pbAppendBytes(nil, 7, []byte("*")))
	if err != nil {
		return nil, err
	}
	services, _, err := parseAns(b)
	if err != nil {
		return nil, err
	}
	need := map[string]bool{}
	add := func(files [][]byte) {
		for _, f := range files {
			_, deps, err := ps.addDescriptor(f)
			if err != nil {
				ps.Warnings = append(ps.Warnings, err.Error())
				continue
			}
			for _, d := range deps {
				if !ps.files[d] {
					need[d] = true
				}
			}
		}
	}
	for _, s := range services {
		if strings.HasPrefix(s, "grpc.reflection.") {
			continue
		}
		b, err := ask(pbAppendBytes(nil, 4, []byte(s)))
		if err != nil {
			ps.Warnings = append(ps.Warnings, s+": "+err.Error())
			continue
		}
		_, files, err := parseAns(b)
		if err != nil {
			ps.Warnings = append(ps.Warnings, s+": "+err.Error())
			continue
		}
		add(files)
	}
	for i := 0; i < 50 && len(need) > 0; i++ { // dependencies the server did not send with them
		for d := range need {
			delete(need, d)
			if ps.files[d] {
				continue
			}
			if wkt, ok := wellKnown[d]; ok {
				ps.files[d] = true
				_ = ps.parse(d, wkt, nil, "")
				continue
			}
			b, err := ask(pbAppendBytes(nil, 3, []byte(d)))
			if err != nil {
				ps.Warnings = append(ps.Warnings, d+": "+err.Error())
				continue
			}
			_, files, _ := parseAns(b)
			add(files)
		}
	}
	return ps, nil
}

// ---- schemas, cached for a few minutes ----

var grpcCache = struct {
	sync.Mutex
	m map[string]grpcCached
}{m: map[string]grpcCached{}}

type grpcCached struct {
	ps *ProtoSet
	at time.Time
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == '\n' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func grpcSchemaFor(ctx context.Context, r Request, t *Target, c *http.Client, a grpcAddr, md http.Header, fresh bool) (*ProtoSet, error) {
	spec := GRPCSpec{}
	if r.GRPC != nil {
		spec = *r.GRPC
	}
	files := splitList(Resolve(spec.Proto, t.Vars))
	key := "reflect|" + a.base
	if len(files) > 0 {
		key = "proto|" + strings.Join(files, ";") + "|" + spec.ImportPaths
	}
	grpcCache.Lock()
	cached, ok := grpcCache.m[key]
	grpcCache.Unlock()
	if ok && !fresh && time.Since(cached.at) < 5*time.Minute {
		return cached.ps, nil
	}
	var ps *ProtoSet
	var err error
	if len(files) > 0 {
		ps, err = ParseProtoFiles(files, splitList(Resolve(spec.ImportPaths, t.Vars)))
	} else {
		ps, err = grpcReflect(ctx, c, a, md)
	}
	if err != nil {
		return nil, err
	}
	grpcCache.Lock()
	grpcCache.m[key] = grpcCached{ps, time.Now()}
	grpcCache.Unlock()
	return ps, nil
}

// grpcMetadata: the request's headers and its authorization, as gRPC metadata.
func grpcMetadata(ctx context.Context, r Request, t *Target, c *http.Client) (http.Header, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://grpc.invalid/", nil)
	for _, h := range r.Headers {
		if !h.Off && strings.TrimSpace(h.Key) != "" {
			req.Header.Add(strings.ToLower(Resolve(h.Key, t.Vars)), Resolve(h.Value, t.Vars))
		}
	}
	mode, adata := r.Auth, r.AuthData
	if mode == "" && t.CollAuth != "" {
		mode, adata = t.CollAuth, t.CollAuthData
	}
	var err error
	switch mode {
	case "none":
	case "bearer", "basic", "apikey":
		if adata != nil && adata.In == "query" {
			return nil, errors.New("an API key in the query cannot be sent with gRPC: put it in the header")
		}
		err = ownAuth(req, mode, adata, t)
	case "oauth2":
		a := adata
		if a == nil {
			a = &ReqAuth{}
		}
		var tok string
		if tok, err = oauth2Token(ctx, a, t, c, false); err == nil {
			req.Header.Set("Authorization", "Bearer "+tok)
			t.Secrets = append(t.Secrets, tok)
		}
	case "":
		if t.Auth.Type != "" {
			_, err = t.Auth.apply(ctx, req, t, c, false)
		}
	default:
		err = fmt.Errorf("%s authorization is not available for gRPC (use metadata or Bearer, Basic, API key or OAuth 2.0)", mode)
	}
	return req.Header, err
}

// GRPCDescribe lists the methods a server (reflection) or the request's .proto files offer.
func GRPCDescribe(ctx context.Context, r Request, t *Target) (GRPCSchema, error) {
	spec := GRPCSpec{}
	if r.GRPC != nil {
		spec = *r.GRPC
	}
	var out GRPCSchema
	a, err := grpcTarget(r.URL, spec, t)
	if err != nil && len(splitList(spec.Proto)) == 0 {
		return out, err
	}
	c, err := t.grpcClient(a)
	if err != nil {
		return out, err
	}
	md, err := grpcMetadata(ctx, r, t, c)
	if err != nil {
		return out, err
	}
	ps, err := grpcSchemaFor(ctx, r, t, c, a, md, true)
	if err != nil {
		return out, err
	}
	out.Warnings = ps.Warnings
	for _, s := range ps.Services {
		if strings.HasPrefix(s.Full, ".grpc.reflection.") {
			continue
		}
		for _, m := range s.Methods {
			ex, _ := json.MarshalIndent(ps.example(m.In, 0), "", "  ")
			sn := strings.TrimPrefix(s.Full, ".")
			out.Methods = append(out.Methods, GRPCMethodInfo{Name: sn + "/" + m.Name, Service: sn, Method: m.Name, Input: strings.TrimPrefix(m.In, "."),
				Output: strings.TrimPrefix(m.Out, "."), ClientStream: m.ClientStream, ServerStream: m.ServerStream, Example: string(ex)})
		}
	}
	sort.Slice(out.Methods, func(i, j int) bool { return out.Methods[i].Name < out.Methods[j].Name })
	return out, nil
}

// grpcMessages: one JSON object, or for a client stream an array of them (or one object per line).
func grpcMessages(text string, stream bool) []string {
	s := strings.TrimSpace(text)
	if !stream {
		return []string{s}
	}
	if strings.HasPrefix(s, "[") {
		var l []json.RawMessage
		if json.Unmarshal([]byte(s), &l) == nil {
			out := make([]string, len(l))
			for i, x := range l {
				out[i] = string(x)
			}
			return out
		}
	}
	d := json.NewDecoder(strings.NewReader(s))
	var out []string
	for {
		var x json.RawMessage
		if err := d.Decode(&x); err != nil {
			break
		}
		out = append(out, string(x))
	}
	if len(out) == 0 {
		out = []string{"{}"}
	}
	return out
}

// sendGRPC calls the request's gRPC method and gives the answer as a Response: Body is the answer as
// JSON (an array of them for a server stream), Status an HTTP-like status (200 for OK, 404 for
// NOT_FOUND ...), StatusText the gRPC status, Headers the answer's headers and trailers.
func sendGRPC(ctx context.Context, r Request, t *Target) Response {
	start := time.Now()
	res := Response{Method: "GRPC"}
	spec := GRPCSpec{}
	if r.GRPC != nil {
		spec = *r.GRPC
	}
	a, err := grpcTarget(r.URL, spec, t)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.URL = t.mask(a.base + "/" + strings.TrimPrefix(spec.Method, "/"))
	c, err := t.grpcClient(a)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	md, err := grpcMetadata(ctx, r, t, c)
	res.ReqHeaders = t.headers(md)
	if err != nil {
		res.Error = "authentication: " + err.Error()
		return res
	}
	ps, err := grpcSchemaFor(ctx, r, t, c, a, md, false)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	_, m, err := ps.method(spec.Method)
	if err != nil {
		if ps2, err2 := grpcSchemaFor(ctx, r, t, c, a, md, true); err2 == nil { // the server may have changed
			if _, m2, err3 := ps2.method(spec.Method); err3 == nil {
				ps, m, err = ps2, m2, nil
			}
		}
		if err != nil {
			res.Error = err.Error()
			return res
		}
	}
	var msgs [][]byte
	shown := grpcMessages(Resolve(r.Body.Raw, t.Vars), m.ClientStream)
	for i, s := range shown {
		b, err := ps.encodeJSON(m.In, s)
		if err != nil {
			if len(shown) > 1 {
				err = fmt.Errorf("message %d: %w", i+1, err)
			}
			res.Error = err.Error()
			return res
		}
		msgs = append(msgs, b)
	}
	res.ReqBody = t.mask(strings.Join(shown, "\n"))
	to := t.Timeout
	if r.Settings != nil && r.Settings.TimeoutMs > 0 {
		to = time.Duration(r.Settings.TimeoutMs) * time.Millisecond
	}
	if to <= 0 {
		to = 30 * time.Second
	}
	if m.ServerStream || m.ClientStream {
		to = max(to, 5*time.Minute)
	}
	svcPath := "/" + strings.TrimPrefix(spec.Method, "/")
	if !strings.Contains(svcPath[1:], "/") {
		svcPath = "/" + svcPath[1:strings.LastIndex(svcPath, ".")] + "/" + m.Name
	}
	ans, err := grpcInvoke(ctx, c, a, svcPath, md, msgs, to)
	res.Ms = time.Since(start).Milliseconds()
	res.Headers = append(ans.headers, ans.trailers...)
	if err != nil {
		res.Error = t.mask(err.Error())
		return res
	}
	res.Proto = "gRPC"
	res.Status, res.StatusText = grpcHTTPStatus(ans.status), fmt.Sprintf("%d %s", ans.status, GRPCCodeName(ans.status))
	if ans.message != "" {
		res.StatusText += ": " + ans.message
	}
	res.ContentType = "application/json"
	var outs []json.RawMessage
	for _, b := range ans.msgs {
		s, err := ps.decodeJSON(m.Out, b)
		if err != nil {
			s, _ := json.Marshal(map[string]string{"undecoded": base64.StdEncoding.EncodeToString(b), "error": err.Error()})
			outs = append(outs, s)
			continue
		}
		outs = append(outs, json.RawMessage(s))
	}
	switch {
	case m.ServerStream:
		if outs == nil {
			outs = []json.RawMessage{}
		}
		b, _ := json.MarshalIndent(outs, "", "  ")
		res.Body = string(b)
	case len(outs) > 0:
		res.Body = string(outs[0])
	}
	res.Size = len(res.Body)
	res.Timing = Timing{Total: float64(res.Ms)}
	return res
}
