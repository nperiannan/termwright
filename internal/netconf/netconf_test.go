package netconf

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

// server: a fake device speaking base 1.1 (chunked) after the hello.
func server(t *testing.T, r io.Reader, w io.Writer, v11 bool) {
	br := bufio.NewReader(r)
	caps := "<capability>urn:ietf:params:netconf:base:1.0</capability>"
	if v11 {
		caps += "<capability>urn:ietf:params:netconf:base:1.1</capability>"
	}
	go io.WriteString(w, `<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><capabilities>`+caps+`</capabilities></hello>]]>]]>`)
	c := &Client{r: br}
	if _, err := c.readEOM(); err != nil {
		return
	}
	for {
		var msg string
		var err error
		if v11 {
			msg, err = c.readChunked()
		} else {
			msg, err = c.readEOM()
		}
		if err != nil {
			return
		}
		reply := `<rpc-reply><data><vlans><vlan><name>v200</name></vlan></vlans></data></rpc-reply>`
		if strings.Contains(msg, "<bad/>") {
			reply = `<rpc-reply><rpc-error><error-type>application</error-type><error-tag>invalid-value</error-tag><error-severity>error</error-severity><error-message>no such thing</error-message></rpc-error></rpc-reply>`
		}
		if v11 {
			fmt.Fprintf(w, "\n#%d\n%s\n##\n", len(reply), reply)
		} else {
			io.WriteString(w, reply+"]]>]]>")
		}
	}
}

func TestClient(t *testing.T) {
	for _, v11 := range []bool{true, false} {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		go server(t, sr, sw, v11)
		c, err := Open(cr, cw)
		if err != nil || c.v11 != v11 {
			t.Fatal(err, c)
		}
		r, err := c.RPC(context.Background(), "<get/>")
		if err != nil || !strings.Contains(r, "v200") {
			t.Fatal(r, err)
		}
		if _, err := c.RPC(context.Background(), "<bad/>"); err == nil || !strings.Contains(err.Error(), "no such thing") {
			t.Fatal(err)
		}
		if p := Pretty(r); !strings.Contains(p, "\n") {
			t.Fatal(p)
		}
	}
}
