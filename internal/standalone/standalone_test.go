package standalone

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// fakeSwitch is an SSH server that answers like an EXOS switch: a prompt whose number grows, "* " after
// a change, paging until "disable clipaging".
func fakeSwitch(t *testing.T) string {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if c.User() == "admin" && string(p) == "secret1" {
			return nil, nil
		}
		return nil, fmt.Errorf("denied")
	}}
	cfg.AddHostKey(signer)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go serveSwitch(c, cfg)
		}
	}()
	return l.Addr().String()
}

func serveSwitch(c net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		ch, rq, _ := nc.Accept()
		go func() {
			for r := range rq {
				r.Reply(r.Type == "shell" || r.Type == "pty-req", nil)
			}
		}()
		go func() {
			n, dirty, paging := 1, false, true
			vlans := []string{"Default", "Mgmt"}
			prompt := func() string {
				p := fmt.Sprintf("X440G2-24x-10G4.%d # ", n)
				if dirty {
					p = "* " + p
				}
				return p
			}
			io.WriteString(ch, "ExtremeXOS\r\nCopyright (C) 2024\r\n\r\n"+prompt())
			buf := make([]byte, 1)
			line := ""
			for {
				if _, err := ch.Read(buf); err != nil {
					return
				}
				if buf[0] == ' ' && line == "" {
					continue
				}
				if buf[0] != '\r' && buf[0] != '\n' {
					line += string(buf[0])
					continue
				}
				io.WriteString(ch, line+"\r\n")
				cmd := strings.TrimSpace(line)
				line = ""
				out := ""
				switch {
				case cmd == "disable clipaging":
					paging = false
				case cmd == "show vlan":
					var b strings.Builder
					for _, v := range vlans {
						fmt.Fprintf(&b, "%-15s 1    ------  ANY 0 /0 VR-Default\n", v)
					}
					fmt.Fprintf(&b, "Total number of VLAN(s) : %d", len(vlans))
					out = b.String()
					if paging {
						io.WriteString(ch, "Press <SPACE> to continue or <Q> to quit:")
						time.Sleep(50 * time.Millisecond)
					}
				case strings.HasPrefix(cmd, "create vlan "):
					vlans = append(vlans, strings.Fields(cmd)[2])
					dirty = true
				case cmd == "show vlan v1000":
					out = "    Admin State:         Enabled     Tagging:   802.1Q Tag 1000\n    Primary IP:          10.1.1.1/24"
				case cmd != "":
					out = "Invalid input detected at '^' marker."
				}
				if out != "" {
					io.WriteString(ch, strings.ReplaceAll(out, "\n", "\r\n")+"\r\n")
				}
				if cmd != "" {
					n++
				}
				io.WriteString(ch, prompt())
			}
		}()
	}
}

func engine(t *testing.T, inv string) *Engine {
	t.Helper()
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "inventory.yaml"), []byte(inv), 0o644)
	e, err := Open(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}

func TestCommandsOverSSH(t *testing.T) {
	addr := fakeSwitch(t)
	h, p, _ := net.SplitHostPort(addr)
	for _, dt := range []string{"EXOS", "Generic"} { // EXOS turns paging off; Generic pages through it
		e := engine(t, fmt.Sprintf("devices:\n  - session: Lab/EXOS/sw1\n    host: %s\n    port: %s\n    username: admin\n    device_type: %s\n", h, p, dt))
		t.Setenv("TW_PASSWORD_SW1", "secret1")
		ctx := context.Background()
		out, err := e.Command(ctx, "Lab/EXOS/sw1", "show vlan", "", 10*time.Second)
		if err != nil || !strings.Contains(out, "Total number of VLAN(s) : 2") || strings.Contains(out, "Press") || strings.Contains(out, "show vlan") {
			t.Fatalf("%s: %q %v", dt, out, err)
		}
		if _, err := e.Command(ctx, "Lab/EXOS/sw1", "create vlan v1000 tag 1000", "", 10*time.Second); err != nil {
			t.Fatal(err)
		}
		// the prompt now starts with "* " and has another number
		a := e.Execute(ctx, []byte(`{"run":{"session":"Lab/EXOS/sw1","command":"show vlan v1000"},"retry":{"timeout":5,"interval":1},"expect":[{"field":"Primary\\s+IP:\\s*(\\S+)","equals":"10.1.1.1/24"},{"contains":"802.1Q Tag 1000"}]}`))
		if a.Error != "" || len(a.Checks) != 2 || !a.Checks[0].Passed || !a.Checks[1].Passed {
			t.Fatalf("%s: %+v", dt, a)
		}
		out, _ = e.Command(ctx, "Lab/EXOS/sw1", "show vlan", "", 10*time.Second)
		if !strings.Contains(out, "Total number of VLAN(s) : 3") {
			t.Fatalf("%q", out)
		}
	}
}

func TestWrongPassword(t *testing.T) {
	addr := fakeSwitch(t)
	h, p, _ := net.SplitHostPort(addr)
	e := engine(t, fmt.Sprintf("devices:\n  - session: sw1\n    host: %s\n    port: %s\n    username: admin\n", h, p))
	if _, err := e.Command(context.Background(), "sw1", "show vlan", "", time.Second); err == nil || !strings.Contains(err.Error(), "TW_PASSWORD_SW1") {
		t.Fatal(err)
	}
	t.Setenv("TW_PASSWORD", "bad")
	if _, err := e.Command(context.Background(), "sw1", "show vlan", "", time.Second); err == nil || !strings.Contains(err.Error(), "unable to authenticate") {
		t.Fatal(err)
	}
}

func TestHTTPAndChecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if u != "api" || p != "pw" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"data":{"vlans":[{"name":"v1000","vid":1000}]}}`)
	}))
	defer srv.Close()
	e := engine(t, "devices:\n  - session: APIs/rest\n    protocol: api\n    base_url: "+srv.URL+"/rest\n    auth: basic\n    username: api\n")
	t.Setenv("TW_PASSWORD_REST", "pw")
	st, body, err := e.HTTP(context.Background(), HTTPRequest{Session: "APIs/rest", URL: "/vlans"})
	if err != nil || st != 200 {
		t.Fatal(st, body, err)
	}
	a := Answer{Output: body, Status: st}
	eq := "v1000"
	for _, c := range []Check{CheckOne(Expect{Status: 200}, a), CheckOne(Expect{JSON: "data.vlans[0].name", Equals: &eq}, a), CheckOne(Expect{JSON: "data.vlans.length", Greater: ptr(0.5)}, a)} {
		if !c.Passed {
			t.Fatal(c)
		}
	}
	if c := CheckOne(Expect{JSON: "data.vlans[1].name", Equals: &eq}, a); c.Passed {
		t.Fatal(c)
	}
	x := XMLDoc(`<rpc-reply><data><vlans><vlan><name>v200</name></vlan><vlan><name>v300</name></vlan></vlans></data></rpc-reply>`)
	if v, ok := JSONPath(x, "data.vlans.vlan[1].name"); !ok || v != "v300" {
		t.Fatal(v)
	}
}

func ptr(f float64) *float64 { return &f }

func TestEnvName(t *testing.T) {
	if n := EnvName("TW_PASSWORD_", "Discovered-10.127.34.x/EXOS/Stack (X440G2-24x-10G4 (Stack))"); n != "TW_PASSWORD_STACK_X440G2_24X_10G4_STACK" {
		t.Fatal(n)
	}
}
