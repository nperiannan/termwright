// Package netconf is a small NETCONF client (RFC 6241) over any stream - in Termwright the "netconf" SSH
// subsystem: hello with base 1.0 and 1.1, end-of-message (1.0) or chunked (1.1) framing, RPCs and their
// errors.
package netconf

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const eom = "]]>]]>"

// Client talks NETCONF on a stream.
type Client struct {
	mu  sync.Mutex
	r   *bufio.Reader
	w   io.Writer
	v11 bool
	id  int
	// Hello is the server's hello (its capabilities).
	Hello string
}

const hello = `<?xml version="1.0" encoding="UTF-8"?><hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><capabilities>` +
	`<capability>urn:ietf:params:netconf:base:1.0</capability><capability>urn:ietf:params:netconf:base:1.1</capability></capabilities></hello>`

// Open exchanges hellos; base 1.1 (chunked framing) is used when the server has it.
func Open(r io.Reader, w io.Writer) (*Client, error) {
	c := &Client{r: bufio.NewReaderSize(r, 64<<10), w: w}
	if _, err := io.WriteString(w, hello+eom); err != nil {
		return nil, err
	}
	h, err := c.readEOM()
	if err != nil {
		return nil, fmt.Errorf("netconf hello: %v", err)
	}
	if !strings.Contains(h, "hello") {
		return nil, errors.New("netconf: the server did not say hello")
	}
	c.Hello = h
	c.v11 = strings.Contains(h, "urn:ietf:params:netconf:base:1.1")
	return c, nil
}

func (c *Client) readEOM() (string, error) {
	var b bytes.Buffer
	for {
		line, err := c.r.ReadString('>')
		b.WriteString(line)
		if bytes.HasSuffix(b.Bytes(), []byte(eom)) {
			return strings.TrimSpace(strings.TrimSuffix(b.String(), eom)), nil
		}
		if err != nil {
			return b.String(), err
		}
		if b.Len() > 64<<20 {
			return "", errors.New("netconf: the answer is larger than 64 MB")
		}
	}
}

func (c *Client) readChunked() (string, error) {
	var b bytes.Buffer
	for {
		// "\n#<size>\n" or "\n##\n"
		for {
			ch, err := c.r.ReadByte()
			if err != nil {
				return "", err
			}
			if ch == '#' {
				break
			}
		}
		head, err := c.r.ReadString('\n')
		if err != nil {
			return "", err
		}
		head = strings.TrimSpace(head)
		if head == "#" {
			return strings.TrimSpace(b.String()), nil
		}
		n, err := strconv.Atoi(head)
		if err != nil || n <= 0 || n > 64<<20 {
			return "", fmt.Errorf("netconf: bad chunk %q", head)
		}
		if _, err := io.CopyN(&b, c.r, int64(n)); err != nil {
			return "", err
		}
	}
}

// RPC sends one operation (the XML inside <rpc>) and returns the <rpc-reply>; an <rpc-error> of severity
// error is returned as an error (with the reply).
func (c *Client) RPC(ctx context.Context, op string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id++
	msg := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><rpc message-id="%d" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">%s</rpc>`, c.id, op)
	var err error
	if c.v11 {
		_, err = fmt.Fprintf(c.w, "\n#%d\n%s\n##\n", len(msg), msg)
	} else {
		_, err = io.WriteString(c.w, msg+eom)
	}
	if err != nil {
		return "", err
	}
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		var r res
		if c.v11 {
			r.s, r.err = c.readChunked()
		} else {
			r.s, r.err = c.readEOM()
		}
		ch <- r
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return r.s, r.err
		}
		if e := rpcError(r.s); e != "" {
			return r.s, errors.New(e)
		}
		return r.s, nil
	}
}

// rpcError is the message of the reply's first rpc-error of severity error ("" when none).
func rpcError(reply string) string {
	if !strings.Contains(reply, "rpc-error") {
		return ""
	}
	d := xml.NewDecoder(strings.NewReader(reply))
	var path []string
	var sev, msg, tag string
	in := false
	for {
		t, err := d.Token()
		if err != nil {
			break
		}
		switch e := t.(type) {
		case xml.StartElement:
			path = append(path, e.Name.Local)
			if e.Name.Local == "rpc-error" {
				in, sev, msg, tag = true, "", "", ""
			}
		case xml.CharData:
			if in && len(path) > 0 {
				switch path[len(path)-1] {
				case "error-severity":
					sev += string(e)
				case "error-message":
					msg += string(e)
				case "error-tag":
					tag += string(e)
				}
			}
		case xml.EndElement:
			if e.Name.Local == "rpc-error" && in {
				in = false
				if strings.TrimSpace(sev) != "warning" {
					m := strings.TrimSpace(msg)
					if m == "" {
						m = strings.TrimSpace(tag)
					}
					return "the device answered with an error: " + m
				}
			}
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		}
	}
	return ""
}

// Pretty indents XML for people (the reply as it is when it cannot be read).
func Pretty(s string) string {
	d := xml.NewDecoder(strings.NewReader(s))
	var b bytes.Buffer
	e := xml.NewEncoder(&b)
	e.Indent("", "  ")
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return s
		}
		if cd, ok := t.(xml.CharData); ok && len(bytes.TrimSpace(cd)) == 0 {
			continue
		}
		if _, ok := t.(xml.ProcInst); ok {
			continue
		}
		if err := e.EncodeToken(xml.CopyToken(t)); err != nil {
			return s
		}
	}
	if e.Flush() != nil {
		return s
	}
	return b.String()
}
