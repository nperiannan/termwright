package standalone

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/netconf"
	"github.com/termwright/termwright/internal/snmp"
	"github.com/termwright/termwright/internal/textfsm"
	"github.com/termwright/termwright/internal/webrun"
)

// ---- REST APIs ----

// HTTPRequest is a REST request; a relative URL goes to the session's base_url with its login.
type HTTPRequest struct {
	Session string            `json:"session,omitempty"`
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

var schemeRe = regexp.MustCompile(`^[a-zA-Z]+://`)

// HTTP sends a request: its status and body.
func (e *Engine) HTTP(ctx context.Context, r HTTPRequest) (int, string, error) {
	u, insecure := r.URL, false
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	if r.Session != "" {
		d, err := e.Device(r.Session)
		if err != nil {
			return 0, "", err
		}
		if strings.EqualFold(d.Protocol, "web") { // a web app: as the user its web steps logged in
			u = webrun.Join(d.WebURL, u)
			if c := webrun.Cookies(webrun.StatePath(filepath.Join(e.Root, ".web-state"), "app-"+d.Session), u); c != "" && hdr.Get("Cookie") == "" {
				hdr.Set("Cookie", c)
			}
		} else if !schemeRe.MatchString(u) {
			u = strings.TrimRight(d.BaseURL, "/") + "/" + strings.TrimLeft(u, "/")
		}
		insecure = d.Insecure
		switch strings.ToLower(d.Auth) {
		case "basic":
			pw, err := e.Secret(d, "password")
			if err != nil {
				return 0, "", err
			}
			if hdr.Get("Authorization") == "" {
				hdr.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(d.Username+":"+pw)))
			}
		case "bearer", "login", "oauth2":
			t, err := e.Secret(d, "token")
			if err != nil {
				return 0, "", err
			}
			if hdr.Get("Authorization") == "" {
				hdr.Set("Authorization", "Bearer "+t)
			}
		case "apikey":
			t, err := e.Secret(d, "token")
			if err != nil {
				return 0, "", err
			}
			name := d.KeyName
			if name == "" {
				name = "X-API-Key"
			}
			if hdr.Get(name) == "" {
				hdr.Set(name, t)
			}
		}
	}
	if !schemeRe.MatchString(u) {
		return 0, "", fmt.Errorf("%q is not a full URL and no API session gives its base URL", r.URL)
	}
	method := strings.ToUpper(r.Method)
	if method == "" {
		method = "GET"
	}
	var body io.Reader
	if r.Body != "" {
		body = strings.NewReader(r.Body)
		if hdr.Get("Content-Type") == "" {
			hdr.Set("Content-Type", "application/json")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return 0, "", err
	}
	req.Header = hdr
	cl := &http.Client{Timeout: 2 * time.Minute}
	if insecure {
		cl.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // the session says so (lab devices)
	}
	res, err := cl.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("%s %s: %v", method, u, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	return res.StatusCode, string(b), err
}

// ---- SNMP ----

// SNMPValue is a value an SNMP set writes.
type SNMPValue struct {
	OID   string `json:"oid"`
	Type  string `json:"type,omitempty"`
	Value string `json:"value"`
}

// SNMPRow is a value an SNMP request got.
type SNMPRow struct {
	OID     string `json:"oid"`
	Name    string `json:"name"`
	Value   string `json:"value"`
	Display string `json:"display"`
}

var (
	mibOnce sync.Once
	mib     *snmp.MIB
)

// MIB is the standard MIBs and the suite's mibs/ folder.
func (e *Engine) MIB() *snmp.MIB {
	mibOnce.Do(func() {
		mib = snmp.Standard().Clone()
		ents, _ := os.ReadDir(filepath.Join(e.Root, "mibs"))
		for pass := 0; pass < 2; pass++ {
			for _, x := range ents {
				if b, err := os.ReadFile(filepath.Join(e.Root, "mibs", x.Name())); err == nil && !x.IsDir() {
					_, _ = mib.Load(string(b))
				}
			}
		}
	})
	return mib
}

// SNMP runs get, getnext, bulk, walk or set on a device.
func (e *Engine) SNMP(ctx context.Context, session, op string, oids []string, values []SNMPValue) ([]SNMPRow, error) {
	d, err := e.Device(session)
	if err != nil {
		return nil, err
	}
	m := e.MIB()
	host := d.SNMPHost
	if host == "" {
		host = d.Host
	}
	cfg := snmp.Config{Address: net.JoinHostPort(strings.Trim(host, "[]"), port(d.SNMPPort, 161)), Timeout: 3 * time.Second, Retries: 1}
	switch d.SNMPVer {
	case "1":
		cfg.Version = snmp.V1
	case "3":
		cfg.Version, cfg.User, cfg.Auth, cfg.Priv = snmp.V3, d.SNMPUser, snmp.Auth(d.SNMPAuth), snmp.Priv(d.SNMPPriv)
		if d.SNMPAuth != "" {
			if cfg.AuthPass, err = e.Secret(d, "password"); err != nil {
				return nil, err
			}
		}
		if d.SNMPPriv != "" {
			cfg.PrivPass = os.Getenv(EnvName("TW_SNMP_PRIV_", d.Session))
			if cfg.PrivPass == "" {
				cfg.PrivPass = cfg.AuthPass
			}
		}
	default:
		cfg.Version = snmp.V2c
		if cfg.Community, err = e.Secret(d, "community"); err != nil {
			return nil, err
		}
	}
	var res []string
	for _, o := range oids {
		if strings.TrimSpace(o) == "" {
			continue
		}
		n, err := m.Resolve(o)
		if err != nil {
			return nil, err
		}
		res = append(res, n)
	}
	c, err := snmp.Dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var vbs []snmp.Varbind
	switch strings.ToLower(op) {
	case "", "get":
		vbs, err = c.Get(ctx, res...)
	case "getnext", "next":
		vbs, err = c.GetNext(ctx, res...)
	case "bulk", "getbulk":
		vbs, err = c.GetBulk(ctx, 0, 25, res...)
	case "walk", "table":
		for _, o := range res {
			w, e2 := c.Walk(ctx, o, 0, 10000, nil)
			vbs = append(vbs, w...)
			if e2 != nil {
				err = e2
				break
			}
		}
	case "set":
		var in []snmp.Varbind
		for _, v := range values {
			vb, e2 := setValue(m, v)
			if e2 != nil {
				return nil, e2
			}
			in = append(in, vb)
		}
		vbs, err = c.Set(ctx, in...)
	default:
		return nil, fmt.Errorf("unknown SNMP operation %q (get, getnext, bulk, walk, set)", op)
	}
	if err != nil {
		return nil, err
	}
	out := make([]SNMPRow, 0, len(vbs))
	for _, v := range vbs {
		r := SNMPRow{OID: v.OID, Name: m.Name(v.OID), Value: v.Value, Display: m.Describe(v)}
		if v.Exception() {
			r.Display = string(v.Type)
		}
		out = append(out, r)
	}
	return out, nil
}

func setValue(m *snmp.MIB, v SNMPValue) (snmp.Varbind, error) {
	oid, err := m.Resolve(v.OID)
	if err != nil {
		return snmp.Varbind{}, err
	}
	n, _ := m.Lookup(oid)
	ty := strings.ToLower(strings.TrimSpace(v.Type))
	if ty == "" && n != nil {
		ty = strings.ToLower(n.Base)
	}
	val, vb := strings.TrimSpace(v.Value), snmp.Varbind{OID: oid}
	switch ty {
	case "i", "integer", "integer32", "int":
		x, err := strconv.ParseInt(val, 10, 64)
		if err != nil && n != nil {
			for k, name := range n.Enums {
				if strings.EqualFold(name, val) {
					x, err = k, nil
				}
			}
		}
		if err != nil {
			return vb, fmt.Errorf("%s: %q is not a number", v.OID, val)
		}
		vb.Type, vb.Int = snmp.Integer, x
	case "u", "gauge32", "unsigned32", "c", "counter32", "t", "timeticks", "counter64":
		x, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return vb, fmt.Errorf("%s: %q is not a whole number", v.OID, val)
		}
		vb.Uint = x
		vb.Type = map[string]snmp.Type{"u": snmp.Gauge32, "gauge32": snmp.Gauge32, "unsigned32": snmp.Gauge32, "c": snmp.Counter32, "counter32": snmp.Counter32, "t": snmp.TimeTicks, "timeticks": snmp.TimeTicks, "counter64": snmp.Counter64}[ty]
	case "x", "hex":
		b, err := hex.DecodeString(strings.NewReplacer(":", "", " ", "", "-", "").Replace(val))
		if err != nil {
			return vb, fmt.Errorf("%s: %q is not hex", v.OID, val)
		}
		vb.Type, vb.Bytes = snmp.OctetString, b
	case "a", "ipaddress":
		if net.ParseIP(val).To4() == nil {
			return vb, fmt.Errorf("%s: %q is not an IPv4 address", v.OID, val)
		}
		vb.Type, vb.Value = snmp.IPAddress, val
	case "o", "objectidentifier", "oid":
		o, err := m.Resolve(val)
		if err != nil {
			return vb, err
		}
		vb.Type, vb.Value = snmp.ObjectID, o
	default:
		vb.Type, vb.Bytes = snmp.OctetString, []byte(v.Value)
	}
	return vb, nil
}

// ---- NETCONF ----

// Netconf is a NETCONF operation: get, get-config, edit-config (a candidate edit is committed) or rpc.
type Netconf struct {
	Session string `json:"session"`
	Op      string `json:"op,omitempty"`
	Source  string `json:"source,omitempty"`
	Target  string `json:"target,omitempty"`
	Filter  string `json:"filter,omitempty"`
	Config  string `json:"config,omitempty"`
	RPC     string `json:"rpc,omitempty"`
	Port    int    `json:"port,omitempty"`
}

// RPCs are the operations a NETCONF step sends, in order.
func (n Netconf) RPCs() ([]string, error) {
	filter := ""
	if f := strings.TrimSpace(n.Filter); f != "" {
		filter = `<filter type="subtree">` + f + `</filter>`
	}
	ds := func(s string) (string, error) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			s = "running"
		}
		if s != "running" && s != "candidate" && s != "startup" {
			return "", fmt.Errorf("datastore %q: running, candidate or startup", s)
		}
		return "<" + s + "/>", nil
	}
	switch strings.ToLower(n.Op) {
	case "", "get":
		return []string{"<get>" + filter + "</get>"}, nil
	case "get-config":
		s, err := ds(n.Source)
		if err != nil {
			return nil, err
		}
		return []string{"<get-config><source>" + s + "</source>" + filter + "</get-config>"}, nil
	case "edit-config":
		t, err := ds(n.Target)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(n.Config) == "" {
			return nil, errors.New("edit-config needs config")
		}
		out := []string{"<edit-config><target>" + t + "</target><config>" + n.Config + "</config></edit-config>"}
		if t == "<candidate/>" {
			out = append(out, "<commit/>")
		}
		return out, nil
	case "rpc":
		if strings.TrimSpace(n.RPC) == "" {
			return nil, errors.New("op rpc needs rpc (the operation's XML)")
		}
		return []string{n.RPC}, nil
	}
	return nil, fmt.Errorf("netconf op %q: get, get-config, edit-config or rpc", n.Op)
}

// Netconf runs a NETCONF operation over the device's SSH login (port 830 by default).
func (e *Engine) Netconf(ctx context.Context, n Netconf) (string, error) {
	d, err := e.Device(n.Session)
	if err != nil {
		return "", err
	}
	rpcs, err := n.RPCs()
	if err != nil {
		return "", err
	}
	cl, closeAll, err := e.dial(ctx, d, port(n.Port, 830))
	if err != nil {
		return "", fmt.Errorf("NETCONF login: %v", err)
	}
	defer closeAll()
	ss, err := cl.NewSession()
	if err != nil {
		return "", err
	}
	defer ss.Close()
	in, _ := ss.StdinPipe()
	out, _ := ss.StdoutPipe()
	if err := ss.RequestSubsystem("netconf"); err != nil {
		return "", fmt.Errorf("%s has no NETCONF on port %s: %v", d.Session, port(n.Port, 830), err)
	}
	c, err := netconf.Open(out, in)
	if err != nil {
		return "", err
	}
	var reply string
	for _, op := range rpcs {
		r, err := c.RPC(ctx, op)
		if err != nil {
			return netconf.Pretty(r), err
		}
		reply = r
	}
	_, _ = c.RPC(ctx, "<close-session/>")
	return netconf.Pretty(reply), nil
}

// ---- web pages (Playwright) ----

// WebRunner is where Playwright is installed for web steps: TW_PLAYWRIGHT_DIR, else .playwright in the suite.
func (e *Engine) WebRunner() webrun.Runner {
	if d := os.Getenv("TW_PLAYWRIGHT_DIR"); d != "" {
		return webrun.Runner{Dir: d}
	}
	return webrun.Runner{Dir: filepath.Join(e.Root, ".playwright")}
}

// Web runs a web step; screenshots go to reports/web-shots.
func (e *Engine) Web(ctx context.Context, w webrun.Step, timeout int) (string, error) {
	if timeout <= 0 {
		timeout = 30
	}
	x := webrun.Extra{Root: e.Root, StateDir: filepath.Join(e.Root, ".web-state")}
	if w.Login != "" {
		d, err := e.Device(w.Login)
		if err != nil {
			return "", err
		}
		pw, err := e.Secret(d, "password")
		if err != nil {
			return "", err
		}
		if strings.EqualFold(d.Protocol, "web") {
			w, x = webrun.ForApp(w, webrun.App{Name: d.Session, URL: d.WebURL, Browser: d.Browser, Viewport: d.Viewport, Insecure: d.Insecure,
				User: d.Username, Password: pw, Login: d.WebLogin}, x)
		} else {
			x.HTTPUser, x.HTTPPassword = d.Username, pw
			w = webrun.WithLogin(w, d.Username, pw)
		}
	}
	return e.WebRunner().Run(ctx, w, filepath.Join(e.Root, "reports", "web-shots"), timeout, x)
}

// ---- tables ----

// Parse turns output into a table with templates/ (template "auto": the index entry for the device type).
func (e *Engine) Parse(session, command, output, template string) ([]string, [][]string, error) {
	dir := filepath.Join(e.Root, "templates")
	name := template
	if template == "" || template == "auto" {
		ix, err := textfsm.LoadIndex(dir)
		if err != nil {
			return nil, nil, fmt.Errorf("templates/index: %v", err)
		}
		dt := ""
		if d, err := e.Device(session); err == nil {
			dt = d.DeviceType
		}
		found := ix.Find(textfsm.Platform(dt), command)
		if len(found) == 0 {
			return nil, nil, fmt.Errorf("no template in templates/index for %q on %s", command, textfsm.Platform(dt))
		}
		name = found[0]
	}
	b, err := os.ReadFile(filepath.Join(dir, filepath.Clean("/"+name)))
	if err != nil {
		return nil, nil, err
	}
	t, err := textfsm.Parse(string(b))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %v", name, err)
	}
	tab, err := t.Run(output)
	if err != nil {
		return nil, nil, err
	}
	return tab.Header, tab.Rows, nil
}
