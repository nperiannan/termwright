package apiclient

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// a small echo WebSocket server: it answers each message with "echo: " + the message, and closes on "bye".
func wsEcho(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		h := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + wsGUID))
		conn, brw, _ := w.(http.Hijacker).Hijack()
		defer conn.Close()
		brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n")
		brw.Flush()
		send := func(op byte, b []byte) {
			hdr := []byte{0x80 | op}
			if len(b) < 126 {
				hdr = append(hdr, byte(len(b)))
			} else {
				hdr = append(hdr, 126)
				hdr = binary.BigEndian.AppendUint16(hdr, uint16(len(b)))
			}
			conn.Write(append(hdr, b...))
		}
		send(9, []byte("hi")) // a ping first: the client must answer with a pong
		br := bufio.NewReader(conn)
		for {
			h := make([]byte, 2)
			if _, err := io.ReadFull(br, h); err != nil {
				return
			}
			n := int(h[1] & 0x7f)
			if n == 126 {
				b := make([]byte, 2)
				io.ReadFull(br, b)
				n = int(binary.BigEndian.Uint16(b))
			}
			if h[1]&0x80 == 0 {
				return // a client must mask
			}
			m := make([]byte, 4)
			io.ReadFull(br, m)
			d := make([]byte, n)
			io.ReadFull(br, d)
			for i := range d {
				d[i] ^= m[i%4]
			}
			switch h[0] & 0x0f {
			case 10:
				send(1, []byte("got pong "+string(d)))
			case 8:
				send(8, d)
				return
			case 2:
				send(2, d)
			default:
				if string(d) == "bye" {
					send(8, append(binary.BigEndian.AppendUint16(nil, 1001), "going away"...))
					return
				}
				send(1, append([]byte("echo: "), d...))
			}
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func TestWebSocket(t *testing.T) {
	s := wsEcho(t)
	u := "ws://" + strings.TrimPrefix(s.URL, "http://")
	if _, res, err := DialWS(context.Background(), Request{URL: u}, &Target{}); err == nil || res.Status != 401 {
		t.Fatalf("no auth: %v %d", err, res.Status)
	}
	c, res, err := DialWS(context.Background(), Request{URL: u + "/{{p}}", Auth: "bearer", AuthData: &ReqAuth{Token: "tok"}}, &Target{Vars: map[string]string{"p": "chat"}})
	if err != nil || res.Status != 101 {
		t.Fatalf("%v %+v", err, res)
	}
	got := make(chan WSMessage, 10)
	done := make(chan string, 1)
	go func() { done <- c.Read(func(m WSMessage) { got <- m }) }()
	next := func() WSMessage {
		select {
		case m := <-got:
			return m
		case <-time.After(3 * time.Second):
			t.Fatal("no message")
		}
		return WSMessage{}
	}
	if m := next(); m.Text != "got pong hi" {
		t.Fatalf("pong: %+v", m)
	}
	if _, err := c.Send("hello "+strings.Repeat("x", 300), false); err != nil {
		t.Fatal(err)
	}
	if m := next(); m.Dir != "received" || !strings.HasPrefix(m.Text, "echo: hello xxx") || m.Size != 306+6-6+6 {
		t.Fatalf("%+v", m)
	}
	if _, err := c.Send(base64.StdEncoding.EncodeToString([]byte{0, 1, 255}), true); err != nil {
		t.Fatal(err)
	}
	if m := next(); !m.Binary || m.Text != "AAH/" {
		t.Fatalf("binary %+v", m)
	}
	c.Send("bye", false)
	select {
	case why := <-done:
		if !strings.Contains(why, "1001 going away") {
			t.Fatal(why)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("not closed")
	}
	if _, err := c.Send("x", false); err == nil {
		t.Fatal("send after close")
	}
}
