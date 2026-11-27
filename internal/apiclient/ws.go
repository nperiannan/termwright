package apiclient

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// A WebSocket client (RFC 6455) for the API client: connect with the request's URL, headers, variables
// and authorization, send text or binary messages, and receive them until either side closes.

// WSMessage is one line of a WebSocket conversation.
type WSMessage struct {
	// Dir: "sent", "received", "info" (connected, closed) or "error".
	Dir string `json:"dir"`
	// Text of the message (base64 when Binary).
	Text   string `json:"text"`
	Binary bool   `json:"binary,omitempty"`
	// Size in bytes.
	Size int `json:"size"`
	// At: when (RFC 3339 with milliseconds).
	At string `json:"at"`
}

// WSConn is an open WebSocket.
type WSConn struct {
	rw     io.ReadWriteCloser
	br     *bufio.Reader
	wmu    sync.Mutex
	closed chan struct{}
	once   sync.Once
	secret []string
}

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// MaxWSMessage is the largest message kept (bigger ones are cut).
const MaxWSMessage = 4 << 20

func wsNow() string { return time.Now().Format("2006-01-02T15:04:05.000Z07:00") }

// DialWS opens a WebSocket: r.URL is ws:// or wss:// (or http(s)://); the handshake is shown as a Response.
func DialWS(ctx context.Context, r Request, t *Target) (*WSConn, Response, error) {
	u := strings.TrimSpace(r.URL)
	switch {
	case strings.HasPrefix(strings.ToLower(u), "ws://"):
		u = "http://" + u[5:]
	case strings.HasPrefix(strings.ToLower(u), "wss://"):
		u = "https://" + u[6:]
	}
	r.URL, r.Method, r.Body = u, "GET", Body{}
	st := Settings{}
	if r.Settings != nil {
		st = *r.Settings
	}
	st.HTTPVersion, st.NoRedirects = "1.1", true
	r.Settings = &st
	res := Response{Method: "GET"}
	c, err := t.client(r)
	if err != nil {
		return nil, res, err
	}
	to := c.Timeout
	c.Timeout = 0 // the connection stays open; only the handshake has a time limit
	if tr, ok := c.Transport.(*http.Transport); ok {
		tr.ResponseHeaderTimeout = to
	}
	req, _, err := build(context.WithoutCancel(ctx), r, t)
	if err != nil {
		return nil, res, err
	}
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	k := base64.StdEncoding.EncodeToString(key)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", k)
	mode, adata := r.Auth, r.AuthData
	if mode == "" && t.CollAuth != "" {
		mode, adata = t.CollAuth, t.CollAuthData
	}
	switch mode {
	case "none":
	case "bearer", "basic", "apikey":
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
	case "awsv4":
		a := adata
		if a == nil {
			a = &ReqAuth{}
		}
		err = awsSign(req, a, t, nil, time.Now())
	case "":
		_, err = t.Auth.apply(ctx, req, t, c, false)
	default:
		err = fmt.Errorf("%s authorization is not available for WebSocket (use a header or Bearer, Basic, API key, OAuth 2.0 or AWS)", mode)
	}
	res.URL, res.ReqHeaders = t.mask(req.URL.String()), t.headers(req.Header)
	if err != nil {
		return nil, res, fmt.Errorf("authentication: %w", err)
	}
	start := time.Now()
	resp, err := c.Do(req)
	res.Ms = time.Since(start).Milliseconds()
	if err != nil {
		return nil, res, errors.New(t.mask(err.Error()))
	}
	res.Status, res.StatusText, res.Proto, res.Headers = resp.StatusCode, resp.Status, resp.Proto, sortedKV(resp.Header)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		res.Body = string(b)
		return nil, res, fmt.Errorf("the server answered %s, not 101 Switching Protocols", resp.Status)
	}
	h := sha1.Sum([]byte(k + wsGUID))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(h[:]) {
		resp.Body.Close()
		return nil, res, errors.New("the server's Sec-WebSocket-Accept is wrong: not a WebSocket server")
	}
	rw, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		resp.Body.Close()
		return nil, res, errors.New("the connection cannot be used as a WebSocket")
	}
	return &WSConn{rw: rw, br: bufio.NewReader(rw), closed: make(chan struct{}), secret: t.Secrets}, res, nil
}

func (w *WSConn) writeFrame(op byte, data []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	hdr := []byte{0x80 | op, 0x80}
	switch n := len(data); {
	case n < 126:
		hdr[1] |= byte(n)
	case n < 65536:
		hdr[1] |= 126
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(n))
	default:
		hdr[1] |= 127
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	hdr = append(hdr, mask...)
	body := make([]byte, len(data))
	for i, b := range data {
		body[i] = b ^ mask[i%4]
	}
	_, err := w.rw.Write(append(hdr, body...))
	return err
}

// Send sends a text message, or a binary one (data in base64) when binary.
func (w *WSConn) Send(text string, bin bool) (WSMessage, error) {
	m := WSMessage{Dir: "sent", Text: text, Binary: bin, At: wsNow()}
	data := []byte(text)
	op := byte(1)
	if bin {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
		if err != nil {
			return m, errors.New("a binary message is written in base64")
		}
		data, op = b, 2
	}
	m.Size = len(data)
	select {
	case <-w.closed:
		return m, errors.New("the WebSocket is closed")
	default:
	}
	return m, w.writeFrame(op, data)
}

// Ping sends a ping (the server answers with a pong).
func (w *WSConn) Ping() error { return w.writeFrame(9, []byte("termwright")) }

// Close closes the WebSocket (code 1000).
func (w *WSConn) Close() error {
	var err error
	w.once.Do(func() {
		_ = w.writeFrame(8, binary.BigEndian.AppendUint16(nil, 1000))
		close(w.closed)
		time.AfterFunc(2*time.Second, func() { _ = w.rw.Close() }) // let the server's close arrive
	})
	return err
}

// Read gives the messages as they come (each to got) until the connection closes; it returns why.
func (w *WSConn) Read(got func(WSMessage)) string {
	defer w.rw.Close()
	var buf []byte
	var bufOp byte
	for {
		h := make([]byte, 2)
		if _, err := io.ReadFull(w.br, h); err != nil {
			select {
			case <-w.closed:
				return "closed"
			default:
			}
			return "the connection ended: " + err.Error()
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0f
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			b := make([]byte, 2)
			if _, err := io.ReadFull(w.br, b); err != nil {
				return err.Error()
			}
			n = uint64(binary.BigEndian.Uint16(b))
		case 127:
			b := make([]byte, 8)
			if _, err := io.ReadFull(w.br, b); err != nil {
				return err.Error()
			}
			n = binary.BigEndian.Uint64(b)
		}
		var mask []byte
		if h[1]&0x80 != 0 {
			mask = make([]byte, 4)
			if _, err := io.ReadFull(w.br, mask); err != nil {
				return err.Error()
			}
		}
		keep := n
		if keep > MaxWSMessage {
			keep = MaxWSMessage
		}
		data := make([]byte, keep)
		if _, err := io.ReadFull(w.br, data); err != nil {
			return err.Error()
		}
		if n > keep {
			_, _ = io.CopyN(io.Discard, w.br, int64(n-keep))
		}
		if mask != nil {
			for i := range data {
				data[i] ^= mask[i%4]
			}
		}
		switch op {
		case 8: // close
			code, why := 1005, ""
			if len(data) >= 2 {
				code, why = int(binary.BigEndian.Uint16(data)), string(data[2:])
			}
			w.once.Do(func() {
				_ = w.writeFrame(8, data[:min(len(data), 2)])
				close(w.closed)
			})
			msg := fmt.Sprintf("closed by the server (%d", code)
			if why != "" {
				msg += " " + why
			}
			return msg + ")"
		case 9: // ping
			_ = w.writeFrame(10, data)
			continue
		case 10: // pong
			got(WSMessage{Dir: "info", Text: "pong", At: wsNow()})
			continue
		case 0:
			buf = append(buf, data...)
		case 1, 2:
			buf, bufOp = data, op
		default:
			continue
		}
		if !fin {
			continue
		}
		m := WSMessage{Dir: "received", Size: len(buf), At: wsNow()}
		if bufOp == 1 && utf8.Valid(buf) {
			m.Text = string(buf)
		} else {
			m.Text, m.Binary = base64.StdEncoding.EncodeToString(buf), true
		}
		got(m)
		buf = nil
	}
}

// sendWS is a WebSocket request in a collection run or a test: connect, send the body (when there is
// one), and take the first message that comes back (within the timeout) as the answer's body.
func sendWS(ctx context.Context, r Request, t *Target) Response {
	start := time.Now()
	c, res, err := DialWS(ctx, r, t)
	res.Method = "WS"
	if err != nil {
		res.Error = err.Error()
		if res.Status == 0 || res.Status == 101 {
			res.Status = 0
		}
		return res
	}
	defer c.Close()
	got := make(chan WSMessage, 1)
	go c.Read(func(m WSMessage) {
		if m.Dir == "received" {
			select {
			case got <- m:
			default:
			}
		}
	})
	if body := strings.TrimSpace(Resolve(r.Body.Raw, t.Vars)); body != "" {
		res.ReqBody = t.mask(body)
		if _, err := c.Send(body, false); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	to := t.Timeout
	if r.Settings != nil && r.Settings.TimeoutMs > 0 {
		to = time.Duration(r.Settings.TimeoutMs) * time.Millisecond
	}
	if to <= 0 {
		to = 10 * time.Second
	}
	select {
	case m := <-got:
		res.Body, res.Base64, res.Size = m.Text, m.Binary, m.Size
	case <-time.After(to):
		res.Error = "no message came back within " + to.String()
	case <-ctx.Done():
		res.Error = "stopped"
	}
	res.Ms = time.Since(start).Milliseconds()
	return res
}
