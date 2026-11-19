package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/termwright/termwright/internal/secrets"
	"github.com/termwright/termwright/internal/session"
	"github.com/termwright/termwright/internal/snmp"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// The SNMP Browser (SNMP menu): Get, GetNext, GetBulk, Walk and Set against a saved session's SNMP
// settings (session properties → SNMP), the MIB tree (the standard MIBs built in, the user's own loaded
// into the data folder's mibs folder) and a trap receiver. The same operations are the snmp tool of the
// AI assistant, the Claude connector and the local API.

// SNMP secrets live in the credential store beside the login password, never in the session file.
func snmpKey(kind, path string) string { return "snmp-" + kind + ":" + path }

var snmpSecretKinds = []string{"community", "auth", "priv"}

// SNMPSecretState says which SNMP secrets are saved for a session (never the secrets themselves).
type SNMPSecretState struct {
	// Community: a v1/v2c community is saved.
	Community bool `json:"community"`
	// Auth: a v3 authentication password is saved.
	Auth bool `json:"auth"`
	// Priv: a v3 privacy password is saved.
	Priv bool `json:"priv"`
}

// SNMPSecrets says which SNMP secrets a session has saved.
func (a *App) SNMPSecrets(path string) SNMPSecretState {
	_, c := secrets.Lookup(snmpKey("community", path))
	_, au := secrets.Lookup(snmpKey("auth", path))
	_, p := secrets.Lookup(snmpKey("priv", path))
	return SNMPSecretState{c, au, p}
}

// SNMPSecretsIn saves or clears a session's SNMP secrets: nil keeps one, "" removes it.
type SNMPSecretsIn struct {
	Path      string  `json:"path"`
	Community *string `json:"community"`
	AuthPass  *string `json:"authPass"`
	PrivPass  *string `json:"privPass"`
}

// SNMPSetSecrets saves a session's SNMP community and v3 passwords in the credential store.
func (a *App) SNMPSetSecrets(in SNMPSecretsIn) error {
	if in.Path == "" {
		return errors.New("save the session first")
	}
	for kind, v := range map[string]*string{"community": in.Community, "auth": in.AuthPass, "priv": in.PrivPass} {
		if v == nil {
			continue
		}
		if *v == "" {
			_ = secrets.Delete(snmpKey(kind, in.Path))
			continue
		}
		if kind != "community" && len(*v) < 8 {
			return errors.New("an SNMPv3 password needs at least 8 characters")
		}
		if err := secrets.Set(snmpKey(kind, in.Path), *v); err != nil {
			return fmt.Errorf("could not save the SNMP %s: %w", kind, err)
		}
	}
	return nil
}

// snmpMoveSecrets follows a session that was renamed (del) or copied.
func snmpMoveSecrets(from, to string, del bool) {
	for _, k := range snmpSecretKinds {
		if v, ok := secrets.Lookup(snmpKey(k, from)); ok {
			if to != "" {
				_ = secrets.Set(snmpKey(k, to), v)
			}
			if del {
				_ = secrets.Delete(snmpKey(k, from))
			}
		}
	}
}

// SNMPTarget is an agent and its credentials given directly (not a saved session); secrets are not kept.
type SNMPTarget struct {
	// Address: host or host:port (UDP 161).
	Address string `json:"address"`
	// Version: 1, 2c or 3.
	Version string `json:"version,omitempty"`
	// Community for v1/v2c (default public).
	Community string `json:"community,omitempty"`
	// User: the v3 user.
	User string `json:"user,omitempty"`
	// Auth: v3 authentication: MD5, SHA, SHA-224, SHA-256, SHA-384, SHA-512 or empty.
	Auth string `json:"auth,omitempty"`
	// AuthPass: the v3 authentication password.
	AuthPass string `json:"authPass,omitempty"`
	// Priv: v3 privacy: DES, AES, AES-192, AES-256 or empty.
	Priv string `json:"priv,omitempty"`
	// PrivPass: the v3 privacy password.
	PrivPass string `json:"privPass,omitempty"`
	// Context: the v3 context name.
	Context string `json:"context,omitempty"`
}

// SNMPValue is a value to write.
type SNMPValue struct {
	// OID: a numeric OID or a name with its instance (ifAdminStatus.5, SNMPv2-MIB::sysContact.0).
	OID string `json:"oid"`
	// Type: Integer, OctetString, Hex, ObjectIdentifier, IpAddress, Counter32, Gauge32, TimeTicks, Counter64 - or empty: from the MIB.
	Type string `json:"type,omitempty"`
	// Value as text; an enumerated Integer takes its name too (down).
	Value string `json:"value"`
}

// SNMPRequest is one SNMP operation.
type SNMPRequest struct {
	// Session: the path of a saved session whose SNMP settings are used.
	Session string `json:"session,omitempty"`
	// Target: an address and credentials instead of a session. With a session as well, the secrets not given come from that session.
	Target *SNMPTarget `json:"target,omitempty"`
	// Op: get, getnext, bulk, walk or set.
	Op string `json:"op"`
	// OIDs: numeric OIDs or names (sysDescr.0, IF-MIB::ifTable, ifOperStatus).
	OIDs []string `json:"oids,omitempty"`
	// Values to write (op set).
	Values []SNMPValue `json:"values,omitempty"`
	// MaxRep: GetBulk's max-repetitions (default 25).
	MaxRep int `json:"maxRep,omitempty"`
	// Max: at most this many values in a walk (default 10000).
	Max int `json:"max,omitempty"`
	// Timeout per try in seconds (default 3).
	Timeout int `json:"timeout,omitempty"`
	// Confirmed: the user confirmed a Set on a production session.
	Confirmed bool `json:"confirmed,omitempty"`
}

// SNMPRow is one value of a result, with the MIB's names.
type SNMPRow struct {
	// OID: the numeric OID.
	OID string `json:"oid"`
	// Name: MODULE::object.instance, or the OID when no loaded MIB knows it.
	Name string `json:"name"`
	// Object: the MIB object's name (ifDescr).
	Object string `json:"object,omitempty"`
	// Module: the MIB module (IF-MIB).
	Module string `json:"module,omitempty"`
	// Instance: what follows the object's OID (the table index; 0 for a scalar).
	Instance string `json:"instance,omitempty"`
	// Type: the SNMP type of the value (Integer, OctetString, Counter64…).
	Type string `json:"type"`
	// Value: the raw value as text.
	Value string `json:"value"`
	// Display: the value as a person reads it (enum names, time, MAC addresses, units).
	Display string `json:"display"`
}

// SNMPResult is what an operation returned.
type SNMPResult struct {
	// Rows: the values, in OID order for a walk.
	Rows []SNMPRow `json:"rows"`
	// Address the request went to.
	Address string `json:"address"`
	// Version used: 1, 2c or 3.
	Version string `json:"version"`
	// EngineID: the agent's v3 engine ID (hex).
	EngineID string `json:"engineId,omitempty"`
	// Ms: how long it took.
	Ms int64 `json:"ms"`
	// Requests: SNMP messages sent.
	Requests int `json:"requests"`
	// Truncated: the walk stopped at max values.
	Truncated bool `json:"truncated,omitempty"`
}

var errSNMPConfirm = errors.New("this is a production session: a Set asks the user first")

// isProtected: a session in production (or an environment that protects like it), folders included.
func (a *App) isProtected(s session.Session) bool {
	env := a.withFolderDefaults(s).Environment
	if env == "" {
		return false
	}
	if strings.EqualFold(env, "production") {
		return true
	}
	for _, e := range a.cfg.Get().Environments {
		if e.ID == env {
			return e.Protect
		}
	}
	return false
}

func snmpVersion(v string) (snmp.Version, string) {
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "v") {
	case "1":
		return snmp.V1, "1"
	case "3":
		return snmp.V3, "3"
	}
	return snmp.V2c, "2c"
}

// snmpConfig makes the client settings from a session (with its saved secrets) or a target.
func (a *App) snmpConfig(r SNMPRequest) (snmp.Config, *session.Session, string, error) {
	timeout := time.Duration(max(1, min(r.Timeout, 60))) * time.Second
	if r.Timeout == 0 {
		timeout = 3 * time.Second
	}
	if r.Session == "" || r.Target != nil {
		t := r.Target
		if t == nil || strings.TrimSpace(t.Address) == "" {
			return snmp.Config{}, nil, "", errors.New("give a session or a target address")
		}
		// a target with a session (the session editor's Test): what is not typed comes from the session's saved secrets
		var sp *session.Session
		tc, ta, tpp := t.Community, t.AuthPass, t.PrivPass
		if r.Session != "" && a.ready() == nil {
			if s, err := a.store.Get(r.Session); err == nil {
				sp = &s
				if tc == "" {
					tc, _ = secrets.Lookup(snmpKey("community", s.Path))
				}
				if ta == "" {
					ta, _ = secrets.Lookup(snmpKey("auth", s.Path))
				}
				if tpp == "" {
					tpp, _ = secrets.Lookup(snmpKey("priv", s.Path))
				}
			}
		}
		v, vs := snmpVersion(t.Version)
		if tc == "" {
			tc = "public"
		}
		return snmp.Config{Address: t.Address, Version: v, Community: tc, User: t.User, Auth: snmp.Auth(t.Auth), AuthPass: ta,
			Priv: snmp.Priv(t.Priv), PrivPass: tpp, Context: t.Context, Timeout: timeout}, sp, vs, nil
	}
	if err := a.ready(); err != nil {
		return snmp.Config{}, nil, "", err
	}
	s, err := a.store.Get(r.Session)
	if err != nil {
		return snmp.Config{}, nil, "", fmt.Errorf("no saved session %q", r.Session)
	}
	host := s.SNMPHost
	if host == "" {
		host = s.Hostname
	}
	if host == "" {
		return snmp.Config{}, nil, "", fmt.Errorf("%s has no address", s.Path)
	}
	port := s.SNMPPort
	if port == 0 {
		port = 161
	}
	v, vs := snmpVersion(s.SNMPVersion)
	cfg := snmp.Config{Address: net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)), Version: v, Timeout: timeout, Context: s.SNMPContext}
	if v == snmp.V3 {
		if s.SNMPUser == "" {
			return cfg, &s, vs, fmt.Errorf("%s has no SNMPv3 user (session properties → SNMP)", s.Path)
		}
		cfg.User, cfg.Auth, cfg.Priv = s.SNMPUser, snmp.Auth(s.SNMPAuth), snmp.Priv(s.SNMPPriv)
		cfg.AuthPass, _ = secrets.Lookup(snmpKey("auth", s.Path))
		cfg.PrivPass, _ = secrets.Lookup(snmpKey("priv", s.Path))
		if cfg.Auth != "" && cfg.AuthPass == "" {
			return cfg, &s, vs, fmt.Errorf("%s has no SNMPv3 authentication password saved (session properties → SNMP)", s.Path)
		}
		if cfg.Priv != "" && cfg.PrivPass == "" {
			return cfg, &s, vs, fmt.Errorf("%s has no SNMPv3 privacy password saved (session properties → SNMP)", s.Path)
		}
	} else {
		c, ok := secrets.Lookup(snmpKey("community", s.Path))
		if !ok {
			c = "public"
		}
		cfg.Community = c
	}
	return cfg, &s, vs, nil
}

// SNMPRun runs one SNMP operation (the SNMP Browser, the AI tools and the local API call it).
func (a *App) SNMPRun(r SNMPRequest) (SNMPResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return a.snmpRun(ctx, r)
}

func (a *App) snmpRun(ctx context.Context, r SNMPRequest) (SNMPResult, error) {
	cfg, s, vs, err := a.snmpConfig(r)
	if err != nil {
		return SNMPResult{}, err
	}
	mib := a.snmpMIB()
	op := strings.ToLower(strings.TrimSpace(r.Op))
	if op == "set" && s != nil {
		if !s.SNMPWrite {
			return SNMPResult{}, fmt.Errorf("writing (Set) is off for %s - turn it on in session properties → SNMP", s.Path)
		}
		if a.isProtected(*s) && !r.Confirmed {
			return SNMPResult{}, errSNMPConfirm
		}
	}
	var oids []string
	for _, o := range r.OIDs {
		if strings.TrimSpace(o) == "" {
			continue
		}
		n, err := mib.Resolve(o)
		if err != nil {
			return SNMPResult{}, err
		}
		oids = append(oids, n)
	}
	if op != "set" && len(oids) == 0 {
		return SNMPResult{}, errors.New("give at least one OID or name")
	}
	start := time.Now()
	c, err := snmp.Dial(ctx, cfg)
	if err != nil {
		return SNMPResult{}, err
	}
	defer c.Close()
	res := SNMPResult{Address: cfg.Address, Version: vs, EngineID: c.EngineID()}
	var vbs []snmp.Varbind
	switch op {
	case "get":
		vbs, err = c.Get(ctx, oids...)
	case "getnext", "next":
		vbs, err = c.GetNext(ctx, oids...)
	case "bulk", "getbulk":
		rep := r.MaxRep
		if rep <= 0 {
			rep = 25
		}
		vbs, err = c.GetBulk(ctx, 0, rep, oids...)
	case "walk", "table":
		maxN := r.Max
		if maxN <= 0 {
			maxN = 10000
		}
		for _, o := range oids {
			w, e := c.Walk(ctx, o, r.MaxRep, maxN-len(vbs), nil)
			vbs = append(vbs, w...)
			if e != nil {
				err = e
				break
			}
			if len(vbs) >= maxN {
				res.Truncated = true
				break
			}
		}
	case "set":
		var in []snmp.Varbind
		for _, v := range r.Values {
			vb, e := snmpValue(mib, v)
			if e != nil {
				return res, e
			}
			in = append(in, vb)
		}
		if len(in) == 0 {
			return res, errors.New("give the values to set")
		}
		vbs, err = c.Set(ctx, in...)
	default:
		return res, fmt.Errorf("unknown operation %q (get, getnext, bulk, walk, set)", r.Op)
	}
	res.Ms, res.Requests = time.Since(start).Milliseconds(), c.Requests
	for _, v := range vbs {
		res.Rows = append(res.Rows, snmpRow(mib, v))
	}
	if res.Rows == nil {
		res.Rows = []SNMPRow{}
	}
	return res, err
}

func snmpRow(mib *snmp.MIB, v snmp.Varbind) SNMPRow {
	row := SNMPRow{OID: v.OID, Name: mib.Name(v.OID), Type: string(v.Type), Value: v.Value, Display: mib.Describe(v)}
	if n, inst := mib.Lookup(v.OID); n != nil {
		row.Object, row.Module, row.Instance = n.Name, n.Module, inst
	}
	if v.Exception() {
		row.Display = string(v.Type)
	}
	return row
}

// snmpValue turns a value to write into a varbind, its type from the MIB when it is not given.
func snmpValue(mib *snmp.MIB, v SNMPValue) (snmp.Varbind, error) {
	oid, err := mib.Resolve(v.OID)
	if err != nil {
		return snmp.Varbind{}, err
	}
	n, _ := mib.Lookup(oid)
	ty := strings.ToLower(strings.TrimSpace(v.Type))
	if ty == "" && n != nil {
		ty = strings.ToLower(n.Base)
	}
	val := v.Value
	vb := snmp.Varbind{OID: oid}
	switch ty {
	case "i", "integer", "integer32", "int":
		x, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil && n != nil { // an enum's name
			for k, name := range n.Enums {
				if strings.EqualFold(name, strings.TrimSpace(val)) {
					x, err = k, nil
				}
			}
		}
		if err != nil {
			return vb, fmt.Errorf("%s: %q is not a number", v.OID, val)
		}
		vb.Type, vb.Int = snmp.Integer, x
	case "u", "gauge32", "unsigned32", "c", "counter32", "t", "timeticks", "counter64":
		x, err := strconv.ParseUint(strings.TrimSpace(val), 10, 64)
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
		o, err := mib.Resolve(val)
		if err != nil {
			return vb, err
		}
		vb.Type, vb.Value = snmp.ObjectID, o
	default: // strings (OctetString, DisplayString…)
		vb.Type, vb.Bytes = snmp.OctetString, []byte(val)
	}
	return vb, nil
}

// ---- MIBs ----

var (
	snmpMibMu  sync.Mutex
	snmpMibCur *snmp.MIB
	snmpMibOf  = map[string]string{} // module → file in the data folder's mibs folder
)

func (a *App) mibDir() string { return filepath.Join(a.cfg.DataDir(), "mibs") }

// snmpMIB is the built-in MIBs plus the user's, loaded once.
func (a *App) snmpMIB() *snmp.MIB {
	snmpMibMu.Lock()
	defer snmpMibMu.Unlock()
	if snmpMibCur == nil {
		snmpMibCur = snmp.Standard().Clone()
		if a.cfg != nil {
			ents, _ := os.ReadDir(a.mibDir())
			// load until nothing new resolves: modules may import each other in any order
			for pass := 0; pass < 2; pass++ {
				for _, e := range ents {
					if e.IsDir() {
						continue
					}
					b, err := os.ReadFile(filepath.Join(a.mibDir(), e.Name()))
					if err != nil {
						continue
					}
					if names, err := snmpMibCur.Load(string(b)); err == nil {
						for _, n := range names {
							snmpMibOf[n] = e.Name()
						}
					}
				}
			}
		}
	}
	return snmpMibCur
}

// SNMPMibNode is a MIB object for the tree.
type SNMPMibNode struct {
	snmp.Node
	// Children: it has objects below it.
	Children bool `json:"children"`
}

func (a *App) mibNodes(ns []*snmp.Node) []SNMPMibNode {
	mib := a.snmpMIB()
	out := make([]SNMPMibNode, 0, len(ns))
	for _, n := range ns {
		out = append(out, SNMPMibNode{Node: *n, Children: len(mib.Children(n.OID)) > 0})
	}
	return out
}

// SNMPMibChildren lists the objects directly below an OID ("" = the top of the tree).
func (a *App) SNMPMibChildren(oid string) []SNMPMibNode {
	return a.mibNodes(a.snmpMIB().Children(strings.Trim(oid, ".")))
}

// SNMPMibInfo is one object by name or OID (an instance is allowed: ifDescr.5).
func (a *App) SNMPMibInfo(nameOrOID string) (SNMPMibNode, error) {
	mib := a.snmpMIB()
	o, err := mib.Resolve(nameOrOID)
	if err != nil {
		return SNMPMibNode{}, err
	}
	n, _ := mib.Lookup(o)
	if n == nil {
		return SNMPMibNode{}, fmt.Errorf("no loaded MIB knows %s", nameOrOID)
	}
	return a.mibNodes([]*snmp.Node{n})[0], nil
}

// SNMPMibSearch finds objects by name, module or OID.
func (a *App) SNMPMibSearch(q string) []SNMPMibNode {
	return a.mibNodes(a.snmpMIB().Search(q, 200))
}

// SNMPModule is a loaded MIB module.
type SNMPModule struct {
	Name    string `json:"name"`
	BuiltIn bool   `json:"builtIn"`
	File    string `json:"file,omitempty"`
}

// SNMPMibModules lists the loaded modules: built in, then the user's.
func (a *App) SNMPMibModules() []SNMPModule {
	mib := a.snmpMIB()
	std := map[string]bool{}
	for _, m := range snmp.Standard().Modules() {
		std[m] = true
	}
	snmpMibMu.Lock()
	defer snmpMibMu.Unlock()
	var out []SNMPModule
	for _, m := range mib.Modules() {
		out = append(out, SNMPModule{Name: m, BuiltIn: std[m], File: snmpMibOf[m]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].BuiltIn && !out[j].BuiltIn })
	return out
}

// SNMPLoadMIBText adds a MIB module (its text) and keeps it in the data folder.
func (a *App) SNMPLoadMIBText(fileName, text string) ([]string, error) {
	mib := a.snmpMIB()
	names, err := mib.Load(text)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(a.mibDir(), 0o700); err != nil {
		return names, err
	}
	base := filepath.Base(fileName)
	if base == "." || base == "/" || base == "" {
		base = names[0] + ".mib"
	}
	if err := os.WriteFile(filepath.Join(a.mibDir(), base), []byte(text), 0o600); err != nil {
		return names, err
	}
	snmpMibMu.Lock()
	for _, n := range names {
		snmpMibOf[n] = base
	}
	snmpMibMu.Unlock()
	return names, nil
}

// SNMPLoadMIBs asks for MIB files and loads them (Load MIB…). It returns the modules found and the files that failed.
func (a *App) SNMPLoadMIBs() ([]string, error) {
	files, err := wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Load MIB Files", ShowHiddenFiles: true})
	if err != nil || len(files) == 0 {
		return nil, err
	}
	var names, bad []string
	for range 2 { // a second pass for modules that import the ones after them
		bad = nil
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				bad = append(bad, filepath.Base(f)+": "+err.Error())
				continue
			}
			n, err := a.SNMPLoadMIBText(f, string(b))
			if err != nil {
				bad = append(bad, filepath.Base(f)+": "+err.Error())
				continue
			}
			names = append(names, n...)
		}
	}
	names = uniq(names)
	if len(bad) > 0 {
		return names, errors.New(strings.Join(bad, "; "))
	}
	return names, nil
}

func uniq(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// SNMPRemoveMIB forgets a module loaded by the user (its file is deleted from the data folder).
func (a *App) SNMPRemoveMIB(module string) error {
	snmpMibMu.Lock()
	f := snmpMibOf[module]
	snmpMibMu.Unlock()
	if f == "" {
		return fmt.Errorf("%s is built in or not loaded", module)
	}
	if err := os.Remove(filepath.Join(a.mibDir(), f)); err != nil && !os.IsNotExist(err) {
		return err
	}
	snmpMibMu.Lock()
	snmpMibCur = nil
	snmpMibOf = map[string]string{}
	snmpMibMu.Unlock()
	a.snmpMIB()
	return nil
}

// ---- traps ----

// SNMPTrapView is a received notification with the MIB's names.
type SNMPTrapView struct {
	snmp.Trap
	// Name: the notification's name (IF-MIB::linkDown).
	Name string `json:"name"`
	// Rows: its values with names.
	Rows []SNMPRow `json:"rows"`
	// Session: the saved session at that address, if any.
	Session string `json:"session,omitempty"`
}

// SNMPTrapState is the trap receiver's state.
type SNMPTrapState struct {
	// On: the receiver listens.
	On bool `json:"on"`
	// Port: its UDP port.
	Port int `json:"port"`
	// Error: why it could not start (a port in use, or below 1024 without permission).
	Error string `json:"error,omitempty"`
	// Count: notifications received since it started.
	Count int `json:"count"`
}

var (
	trapMu    sync.Mutex
	trapConn  net.PacketConn
	trapState SNMPTrapState
	trapList  []SNMPTrapView // newest first, at most 1000
)

// SNMPTrapsStart listens for traps and informs on a UDP port (162 by default).
func (a *App) SNMPTrapsStart(port int) (SNMPTrapState, error) {
	if port <= 0 {
		port = 162
	}
	a.SNMPTrapsStop()
	c, err := net.ListenPacket("udp", ":"+strconv.Itoa(port))
	trapMu.Lock()
	defer trapMu.Unlock()
	trapState = SNMPTrapState{Port: port}
	if err != nil {
		trapState.Error = err.Error()
		if port < 1024 {
			trapState.Error += " - ports below 1024 may need administrator rights; use 1162 and point the devices at it"
		}
		return trapState, errors.New(trapState.Error)
	}
	trapConn, trapState.On = c, true
	go a.trapLoop(c)
	return trapState, nil
}

// SNMPTrapsStop stops the receiver.
func (a *App) SNMPTrapsStop() {
	trapMu.Lock()
	defer trapMu.Unlock()
	if trapConn != nil {
		_ = trapConn.Close()
		trapConn = nil
	}
	trapState.On = false
}

// SNMPTraps is the receiver's state and the notifications received, newest first.
func (a *App) SNMPTraps() SNMPTrapsView {
	trapMu.Lock()
	defer trapMu.Unlock()
	return SNMPTrapsView{State: trapState, Traps: append([]SNMPTrapView{}, trapList...)}
}

// SNMPTrapsClear forgets the notifications received.
func (a *App) SNMPTrapsClear() {
	trapMu.Lock()
	trapList = nil
	trapMu.Unlock()
}

// trapUsers: the v3 users of the saved sessions, so their notifications can be read.
func (a *App) trapUsers() []snmp.TrapUser {
	if a.store == nil {
		return nil
	}
	paths, _ := a.store.AllSessions(nil)
	var out []snmp.TrapUser
	seen := map[string]bool{}
	for _, p := range paths {
		s, err := a.store.Get(p)
		if err != nil || s.SNMPVersion != "3" || s.SNMPUser == "" || seen[s.SNMPUser] {
			continue
		}
		seen[s.SNMPUser] = true
		ap, _ := secrets.Lookup(snmpKey("auth", p))
		pp, _ := secrets.Lookup(snmpKey("priv", p))
		out = append(out, snmp.TrapUser{User: s.SNMPUser, Auth: snmp.Auth(s.SNMPAuth), AuthPass: ap, Priv: snmp.Priv(s.SNMPPriv), PrivPass: pp})
	}
	return out
}

func (a *App) sessionAt(host string) string {
	if a.store == nil {
		return ""
	}
	paths, _ := a.store.AllSessions(nil)
	for _, p := range paths {
		if s, err := a.store.Get(p); err == nil && (s.Hostname == host || s.SNMPHost == host) {
			return p
		}
	}
	return ""
}

func (a *App) trapLoop(c net.PacketConn) {
	buf := make([]byte, 65535)
	users := a.trapUsers()
	for {
		n, from, err := c.ReadFrom(buf)
		if err != nil {
			return
		}
		host, _, _ := net.SplitHostPort(from.String())
		t, reply, err := snmp.DecodeTrap(buf[:n], host, users)
		if err != nil {
			continue
		}
		if reply != nil {
			_, _ = c.WriteTo(reply, from)
		}
		mib := a.snmpMIB()
		v := SNMPTrapView{Trap: t, Name: t.TrapOID, Session: a.sessionAt(host)}
		if t.TrapOID != "" {
			v.Name = mib.Name(t.TrapOID)
		}
		for _, x := range t.Varbinds {
			v.Rows = append(v.Rows, snmpRow(mib, x))
		}
		trapMu.Lock()
		trapList = append([]SNMPTrapView{v}, trapList...)
		if len(trapList) > 1000 {
			trapList = trapList[:1000]
		}
		trapState.Count++
		trapMu.Unlock()
		emitter{a}.Emit("snmp:trap", v)
	}
}
