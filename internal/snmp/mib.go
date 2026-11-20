package snmp

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// The standard IETF and IANA MIB modules (SNMPv2, IF, IP, TCP/UDP, BRIDGE, Q-/P-BRIDGE, RSTP, ENTITY,
// HOST-RESOURCES, EtherLike…), as published in their RFCs.
//
//go:embed mibs
var stdMIBs embed.FS

// Node is one object of a MIB.
type Node struct {
	OID         string           `json:"oid"`
	Name        string           `json:"name"`
	Module      string           `json:"module"`
	Kind        string           `json:"kind"`             // node, scalar, table, row, column, notification, group, compliance, module
	Syntax      string           `json:"syntax,omitempty"` // as written (a textual convention or a base type)
	Base        string           `json:"base,omitempty"`   // the base SMI type behind Syntax
	Enums       map[int64]string `json:"enums,omitempty"`
	Hint        string           `json:"hint,omitempty"` // DISPLAY-HINT of the textual convention
	Access      string           `json:"access,omitempty"`
	Status      string           `json:"status,omitempty"`
	Units       string           `json:"units,omitempty"`
	Index       []string         `json:"index,omitempty"`
	Augments    string           `json:"augments,omitempty"`
	Objects     []string         `json:"objects,omitempty"` // a notification's varbinds
	Description string           `json:"description,omitempty"`
	parentRef   []string
	tc          *textConv
}

type textConv struct {
	syntax string
	enums  map[int64]string
	hint   string
}

// MIB is a set of loaded modules.
type MIB struct {
	mu      sync.RWMutex
	byOID   map[string]*Node
	byName  map[string][]*Node // "ifDescr" and "IF-MIB::ifDescr"
	tcs     map[string]*textConv
	modules map[string]bool
	imports map[string][]string // module → the modules it imports from
	pending []*Node
}

// NewMIB is an empty MIB with the tree's roots.
func NewMIB() *MIB {
	m := &MIB{byOID: map[string]*Node{}, byName: map[string][]*Node{}, tcs: map[string]*textConv{}, modules: map[string]bool{}, imports: map[string][]string{}}
	for _, r := range []struct{ oid, name string }{{"0", "ccitt"}, {"1", "iso"}, {"2", "joint-iso-ccitt"}, {"1.3", "org"}, {"1.3.6", "dod"}, {"1.3.6.1", "internet"},
		{"1.3.6.1.1", "directory"}, {"1.3.6.1.2", "mgmt"}, {"1.3.6.1.2.1", "mib-2"}, {"1.3.6.1.3", "experimental"}, {"1.3.6.1.4", "private"},
		{"1.3.6.1.4.1", "enterprises"}, {"1.3.6.1.5", "security"}, {"1.3.6.1.6", "snmpV2"}, {"1.3.6.1.6.1", "snmpDomains"}, {"1.3.6.1.6.2", "snmpProxys"},
		{"1.3.6.1.6.3", "snmpModules"}, {"1.0.8802", "iso8802"}, {"1.3.6.1.4.1.1916", "extremenetworks"}, {"1.3.6.1.4.1.2272", "rapidCity"}} {
		m.add(&Node{OID: r.oid, Name: r.name, Module: "SNMPv2-SMI", Kind: "node"})
	}
	return m
}

var (
	stdOnce sync.Once
	stdMIB  *MIB
)

// Standard is a MIB with the built-in modules loaded (shared; Load more into a Clone).
func Standard() *MIB {
	stdOnce.Do(func() {
		stdMIB = NewMIB()
		_ = fs.WalkDir(stdMIBs, "mibs", func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				b, _ := stdMIBs.ReadFile(p)
				_, _ = stdMIB.Load(string(b))
			}
			return nil
		})
	})
	return stdMIB
}

// Unplaced lists, per module, the objects that could not be placed in the tree and the parent each waits
// for (usually an object of a module that is not loaded: load the vendor's other MIB files too).
func (m *MIB) Unplaced() map[string][]string {
	out := map[string][]string{}
	seen := map[string]bool{}
	for _, n := range m.pending {
		if len(n.parentRef) > 0 && n.parentRef[0] != "#trap" && !seen[n.Module+"::"+n.Name] {
			seen[n.Module+"::"+n.Name] = true
			out[n.Module] = append(out[n.Module], n.Name+" (needs "+n.parentRef[0]+")")
		}
	}
	return out
}

// Imports are the modules a module imports from.
func (m *MIB) Imports(mod string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string(nil), m.imports[mod]...)
}

// Loaded says whether a module is loaded.
func (m *MIB) Loaded(mod string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.modules[mod]
}

// Dependents are the loaded modules that import mod.
func (m *MIB) Dependents(mod string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []string
	for k, l := range m.imports {
		for _, x := range l {
			if x == mod {
				out = append(out, k)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// Root is a module's top object in the tree (its MODULE-IDENTITY, else its shortest OID).
func (m *MIB) Root(mod string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	best := ""
	for oid, n := range m.byOID {
		if n.Module != mod {
			continue
		}
		if n.Kind == "module" {
			return oid
		}
		if best == "" || strings.Count(oid, ".") < strings.Count(best, ".") || (strings.Count(oid, ".") == strings.Count(best, ".") && CompareOID(oid, best) < 0) {
			best = oid
		}
	}
	return best
}

// Counts is how many objects each module has in the tree.
func (m *MIB) Counts() map[string]int {
	out := map[string]int{}
	for _, n := range m.byOID {
		out[n.Module]++
	}
	return out
}

// Clone copies the MIB (to load private modules without changing the shared one).
func (m *MIB) Clone() *MIB {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := &MIB{byOID: map[string]*Node{}, byName: map[string][]*Node{}, tcs: map[string]*textConv{}, modules: map[string]bool{}, imports: map[string][]string{}}
	for k, v := range m.imports {
		c.imports[k] = v
	}
	for k, v := range m.byOID {
		c.byOID[k] = v
	}
	for k, v := range m.byName {
		c.byName[k] = append([]*Node(nil), v...)
	}
	for k, v := range m.tcs {
		c.tcs[k] = v
	}
	for k := range m.modules {
		c.modules[k] = true
	}
	c.pending = append(c.pending, m.pending...)
	return c
}

// Modules lists the loaded module names.
func (m *MIB) Modules() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []string
	for k := range m.modules {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (m *MIB) add(n *Node) {
	if old, ok := m.byOID[n.OID]; ok && old.Kind != "node" && n.Kind == "node" {
		return // keep the richer definition
	}
	m.byOID[n.OID] = n
	m.byName[n.Name] = append(m.byName[n.Name], n)
	m.byName[n.Module+"::"+n.Name] = []*Node{n}
}

// ---- tokenizer ----

type tok struct {
	s   string
	str bool
}

func tokenize(src string) []tok {
	var out []tok
	r := []rune(src)
	for i := 0; i < len(r); {
		c := r[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case c == '-' && i+1 < len(r) && r[i+1] == '-': // comment to the end of the line or the next "--"
			i += 2
			for i < len(r) && r[i] != '\n' {
				if r[i] == '-' && i+1 < len(r) && r[i+1] == '-' {
					i += 2
					break
				}
				i++
			}
		case c == '"':
			j := i + 1
			for j < len(r) && r[j] != '"' {
				j++
			}
			out = append(out, tok{s: string(r[i+1 : min(j, len(r))]), str: true})
			i = j + 1
		case c == ':' && i+2 < len(r) && r[i+1] == ':' && r[i+2] == '=':
			out = append(out, tok{s: "::="})
			i += 3
		case c == '.' && i+1 < len(r) && r[i+1] == '.':
			out = append(out, tok{s: ".."})
			i += 2
		case strings.ContainsRune("{}(),;|[]", c):
			out = append(out, tok{s: string(c)})
			i++
		case c == '\'': // 'hex'H or 'bin'B
			j := i + 1
			for j < len(r) && r[j] != '\'' {
				j++
			}
			j += 2
			out = append(out, tok{s: string(r[i:min(j, len(r))])})
			i = j
		default:
			j := i
			for j < len(r) && !unicode.IsSpace(r[j]) && !strings.ContainsRune("{}(),;|[]\"", r[j]) && !(r[j] == ':' && j+1 < len(r) && r[j+1] == ':') && !(r[j] == '.' && j+1 < len(r) && r[j+1] == '.') {
				if r[j] == '-' && j+1 < len(r) && r[j+1] == '-' {
					break
				}
				j++
			}
			if j == i {
				j++
			}
			out = append(out, tok{s: string(r[i:j])})
			i = j
		}
	}
	return out
}

var macros = map[string]bool{"MODULE-IDENTITY": true, "OBJECT-IDENTITY": true, "OBJECT-TYPE": true, "NOTIFICATION-TYPE": true, "TRAP-TYPE": true,
	"OBJECT-GROUP": true, "NOTIFICATION-GROUP": true, "MODULE-COMPLIANCE": true, "AGENT-CAPABILITIES": true}

var clauses = map[string]bool{"SYNTAX": true, "UNITS": true, "MAX-ACCESS": true, "ACCESS": true, "MIN-ACCESS": true, "STATUS": true, "DESCRIPTION": true,
	"REFERENCE": true, "INDEX": true, "AUGMENTS": true, "DEFVAL": true, "OBJECTS": true, "LAST-UPDATED": true, "ORGANIZATION": true,
	"CONTACT-INFO": true, "REVISION": true, "DISPLAY-HINT": true, "ENTERPRISE": true, "VARIABLES": true, "NOTIFICATIONS": true, "MODULE": true,
	"MANDATORY-GROUPS": true, "GROUP": true, "OBJECT": true, "WRITE-SYNTAX": true, "PRODUCT-RELEASE": true, "SUPPORTS": true, "INCLUDES": true, "VARIATION": true, "CREATION-REQUIRES": true}

// Load parses one MIB module (or several in one text) and returns their names.
func (m *MIB) Load(src string) ([]string, error) {
	t := tokenize(src)
	var names []string
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := 0; i+3 < len(t); i++ {
		if t[i+1].s == "DEFINITIONS" && t[i+2].s == "::=" && t[i+3].s == "BEGIN" {
			mod := t[i].s
			end := i + 4
			depth := 0
			for end < len(t) {
				if t[end].s == "BEGIN" {
					depth++
				}
				if t[end].s == "END" {
					if depth == 0 {
						break
					}
					depth--
				}
				end++
			}
			m.parseModule(mod, t[i+4:end])
			m.modules[mod] = true
			names = append(names, mod)
			i = end
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("snmp: no MIB module found (expected NAME DEFINITIONS ::= BEGIN … END)")
	}
	m.resolve()
	return names, nil
}

func (m *MIB) parseModule(mod string, t []tok) {
	i := 0
	if i < len(t) && t[i].s == "IMPORTS" {
		var from []string
		for i < len(t) && t[i].s != ";" {
			if t[i].s == "FROM" && i+1 < len(t) {
				from = append(from, t[i+1].s)
			}
			i++
		}
		i++
		m.imports[mod] = from
	}
	for i < len(t) {
		// MACRO definitions (in SNMPv2-SMI): skip to their END
		if i+2 < len(t) && t[i+1].s == "MACRO" {
			for i < len(t) && t[i].s != "END" {
				i++
			}
			i++
			continue
		}
		name := t[i].s
		switch {
		case i+3 < len(t) && t[i+1].s == "OBJECT" && t[i+2].s == "IDENTIFIER" && t[i+3].s == "::=":
			v, n := braceList(t, i+4)
			m.pending = append(m.pending, &Node{Name: name, Module: mod, Kind: "node", parentRef: v})
			i = i + 4 + n
		case i+1 < len(t) && macros[t[i+1].s]:
			j := i + 2
			cl := map[string][]tok{}
			cur := ""
			for j < len(t) && t[j].s != "::=" {
				if clauses[t[j].s] && !(cur == "SYNTAX" && (t[j].s == "OBJECT")) {
					cur = t[j].s
					if _, ok := cl[cur]; !ok {
						cl[cur] = []tok{}
					}
				} else if cur != "" {
					cl[cur] = append(cl[cur], t[j])
				}
				j++
			}
			if j >= len(t) {
				return
			}
			n := &Node{Name: name, Module: mod}
			var used int
			if j+1 < len(t) && t[j+1].s != "{" { // TRAP-TYPE ::= 5
				n.parentRef, used = []string{"#trap", t[j+1].s}, 1
				if e := cl["ENTERPRISE"]; len(e) > 0 {
					n.parentRef = []string{e[0].s, "0", t[j+1].s}
				}
			} else {
				n.parentRef, used = braceList(t, j+1)
			}
			m.fill(n, t[i+1].s, cl)
			m.pending = append(m.pending, n)
			i = j + 1 + used
		case i+2 < len(t) && t[i+1].s == "::=" && t[i+2].s == "TEXTUAL-CONVENTION":
			j := i + 3
			cl := map[string][]tok{}
			cur := ""
			for j < len(t) && !(cur == "SYNTAX" && atDefStart(t, j)) {
				if clauses[t[j].s] && cur != "SYNTAX" {
					cur = t[j].s
					cl[cur] = []tok{}
				} else if cur != "" {
					cl[cur] = append(cl[cur], t[j])
				}
				j++
				if cur == "SYNTAX" && depthEnd(cl["SYNTAX"]) && j < len(t) && (atDefStart(t, j) || t[j].s == "END") {
					break
				}
			}
			syn, enums := syntaxOf(cl["SYNTAX"])
			hint := ""
			if h := cl["DISPLAY-HINT"]; len(h) > 0 {
				hint = h[0].s
			}
			m.tcs[name] = &textConv{syntax: syn, enums: enums, hint: hint}
			i = j
		case i+1 < len(t) && t[i+1].s == "::=": // a type assignment (SEQUENCE, a plain type)
			j := i + 2
			var ty []tok
			for j < len(t) {
				ty = append(ty, t[j])
				j++
				if depthEnd(ty) && j < len(t) && (atDefStart(t, j) || t[j].s == "END") {
					break
				}
			}
			if len(ty) > 0 && ty[0].s != "SEQUENCE" && ty[0].s != "CHOICE" {
				syn, enums := syntaxOf(ty)
				m.tcs[name] = &textConv{syntax: syn, enums: enums}
			}
			i = j
		default:
			i++
		}
	}
}

func depthEnd(ts []tok) bool {
	d := 0
	for _, x := range ts {
		switch x.s {
		case "{", "(":
			d++
		case "}", ")":
			d--
		}
	}
	return d <= 0
}

func atDefStart(t []tok, j int) bool {
	if j+1 >= len(t) {
		return false
	}
	return macros[t[j+1].s] || t[j+1].s == "::=" || (t[j+1].s == "OBJECT" && j+2 < len(t) && t[j+2].s == "IDENTIFIER") || t[j+1].s == "MACRO"
}

// braceList reads "{ parent 1 }" or "{ iso org(3) 6 }" from t[i]: the names and numbers, and how many tokens it took.
func braceList(t []tok, i int) ([]string, int) {
	if i >= len(t) || t[i].s != "{" {
		return nil, 0
	}
	var out []string
	j := i + 1
	for j < len(t) && t[j].s != "}" {
		s := t[j].s
		if j+3 < len(t) && t[j+1].s == "(" && t[j+3].s == ")" { // name(3)
			out = append(out, t[j+2].s)
			j += 4
			continue
		}
		out = append(out, s)
		j++
	}
	return out, j - i + 1
}

func (m *MIB) fill(n *Node, macro string, cl map[string][]tok) {
	text := func(k string) string {
		if v := cl[k]; len(v) > 0 {
			return v[0].s
		}
		return ""
	}
	n.Description = tidy(text("DESCRIPTION"))
	n.Status = text("STATUS")
	n.Units = text("UNITS")
	n.Access = text("MAX-ACCESS")
	if n.Access == "" {
		n.Access = text("ACCESS")
	}
	for _, x := range cl["INDEX"] {
		if !strings.ContainsAny(x.s, "{},") && x.s != "IMPLIED" {
			n.Index = append(n.Index, x.s)
		}
	}
	if a := cl["AUGMENTS"]; len(a) > 1 {
		n.Augments = a[1].s
	}
	for _, x := range append(cl["OBJECTS"], cl["VARIABLES"]...) {
		if !strings.ContainsAny(x.s, "{},") {
			n.Objects = append(n.Objects, x.s)
		}
	}
	switch macro {
	case "MODULE-IDENTITY":
		n.Kind = "module"
	case "OBJECT-IDENTITY":
		n.Kind = "node"
	case "NOTIFICATION-TYPE", "TRAP-TYPE":
		n.Kind = "notification"
	case "OBJECT-GROUP", "NOTIFICATION-GROUP":
		n.Kind = "group"
	case "MODULE-COMPLIANCE", "AGENT-CAPABILITIES":
		n.Kind = "compliance"
	case "OBJECT-TYPE":
		syn := cl["SYNTAX"]
		switch {
		case len(syn) > 1 && syn[0].s == "SEQUENCE" && syn[1].s == "OF":
			n.Kind, n.Syntax = "table", "SEQUENCE OF "+syn[2].s
		case len(n.Index) > 0 || n.Augments != "":
			n.Kind, n.Syntax = "row", syn0(syn)
		default:
			n.Kind = "scalar" // a column when its parent is a row (set in resolve)
			n.Syntax, n.Enums = syntaxOf(syn)
		}
	}
}

func syn0(s []tok) string {
	if len(s) == 0 {
		return ""
	}
	return s[0].s
}

// syntaxOf reads "INTEGER { up(1), down(2) }", "OCTET STRING (SIZE (0..255))", "DisplayString", "Integer32 (1..10)".
func syntaxOf(s []tok) (string, map[int64]string) {
	if len(s) == 0 {
		return "", nil
	}
	name := s[0].s
	rest := s[1:]
	if (name == "OCTET" || name == "OBJECT") && len(s) > 1 {
		name += " " + s[1].s
		rest = s[2:]
	}
	var enums map[int64]string
	if len(rest) > 0 && rest[0].s == "{" {
		enums = map[int64]string{}
		for k := 1; k+3 < len(rest) && rest[k].s != "}"; k++ {
			if rest[k+1].s == "(" {
				if v, err := strconv.ParseInt(rest[k+2].s, 10, 64); err == nil {
					enums[v] = rest[k].s
				}
				k += 3
			}
		}
	}
	return name, enums
}

func tidy(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	out := strings.Join(lines, "\n")
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	// lines of one paragraph joined
	paras := strings.Split(out, "\n\n")
	for i, p := range paras {
		paras[i] = strings.Join(strings.Fields(p), " ")
	}
	return strings.TrimSpace(strings.Join(paras, "\n\n"))
}

var baseTypes = map[string]string{"INTEGER": "Integer", "Integer32": "Integer", "Unsigned32": "Gauge32", "Gauge32": "Gauge32", "Gauge": "Gauge32",
	"Counter32": "Counter32", "Counter": "Counter32", "Counter64": "Counter64", "TimeTicks": "TimeTicks", "OCTET STRING": "OctetString",
	"OBJECT IDENTIFIER": "ObjectIdentifier", "IpAddress": "IpAddress", "NetworkAddress": "IpAddress", "Opaque": "Opaque", "BITS": "Bits"}

// resolve gives OIDs to the pending nodes whose parents are known.
func (m *MIB) resolve() {
	for progress := true; progress && len(m.pending) > 0; {
		progress = false
		var left []*Node
		for _, n := range m.pending {
			ref := n.parentRef
			if len(ref) == 0 {
				continue
			}
			base := ""
			if _, err := strconv.Atoi(ref[0]); err == nil {
				base = ref[0]
			} else if ps := m.byName[n.Module+"::"+ref[0]]; len(ps) > 0 {
				base = ps[0].OID
			} else if ps := m.byName[ref[0]]; len(ps) > 0 {
				base = ps[0].OID
			} else if ref[0] == "#trap" {
				continue
			} else {
				left = append(left, n)
				continue
			}
			oid := base
			for _, x := range ref[1:] {
				if _, err := strconv.ParseUint(x, 10, 64); err != nil {
					oid = ""
					break
				}
				oid += "." + x
			}
			if oid == "" {
				continue
			}
			n.OID = oid
			m.add(n)
			progress = true
		}
		m.pending = left
	}
	// columns, base types
	for _, n := range m.byOID {
		if n.Kind == "scalar" {
			if p := m.byOID[parentOID(n.OID)]; p != nil && p.Kind == "row" {
				n.Kind = "column"
			}
		}
		if n.Syntax != "" && n.Base == "" {
			n.Base, n.Hint = m.base(n.Syntax, &n.Enums)
		}
	}
}

func (m *MIB) base(syn string, enums *map[int64]string) (string, string) {
	hint := ""
	for k := 0; k < 8; k++ {
		if b, ok := baseTypes[syn]; ok {
			return b, hint
		}
		tc := m.tcs[syn]
		if tc == nil {
			return syn, hint
		}
		if hint == "" {
			hint = tc.hint
		}
		if len(*enums) == 0 && len(tc.enums) > 0 {
			*enums = tc.enums
		}
		syn = tc.syntax
	}
	return syn, hint
}

func parentOID(o string) string {
	if i := strings.LastIndexByte(o, '.'); i > 0 {
		return o[:i]
	}
	return ""
}

// Lookup finds the object an OID belongs to and the instance after it ("1.3.6.1.2.1.2.2.1.2.5" → ifDescr, "5").
func (m *MIB) Lookup(oid string) (*Node, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o := strings.Trim(oid, ".")
	for p := o; p != ""; p = parentOID(p) {
		if n := m.byOID[p]; n != nil {
			return n, strings.TrimPrefix(strings.TrimPrefix(o, p), ".")
		}
	}
	return nil, o
}

// Name is "IF-MIB::ifDescr.5" for an OID, or the OID when nothing is known.
func (m *MIB) Name(oid string) string {
	n, inst := m.Lookup(oid)
	if n == nil {
		return oid
	}
	if inst == "" {
		return n.Module + "::" + n.Name
	}
	return n.Module + "::" + n.Name + "." + inst
}

// Resolve turns "ifDescr", "IF-MIB::ifDescr.5", "sysDescr.0" or "1.3.6.1…" into a numeric OID.
func (m *MIB) Resolve(s string) (string, error) {
	s = strings.TrimSpace(strings.Trim(s, "."))
	if s == "" {
		return "", fmt.Errorf("snmp: empty OID")
	}
	if s[0] >= '0' && s[0] <= '9' {
		return s, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	name, inst := s, ""
	if i := strings.IndexByte(s, '.'); i > 0 {
		name, inst = s[:i], s[i:]
	}
	ns := m.byName[name]
	if len(ns) == 0 {
		return "", fmt.Errorf("snmp: %q is not in the loaded MIBs", name)
	}
	return ns[0].OID + inst, nil
}

// Node returns the object at exactly this OID.
func (m *MIB) Node(oid string) *Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byOID[strings.Trim(oid, ".")]
}

// Children lists the objects directly below an OID ("" = the roots), in OID order.
func (m *MIB) Children(oid string) []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Node
	for o, n := range m.byOID {
		if (oid == "" && !strings.Contains(o, ".")) || (oid != "" && parentOID(o) == oid) {
			out = append(out, n)
		}
	}
	// skipped levels (a node whose parent is not defined): show it under the nearest known ancestor
	if oid != "" {
		seen := map[string]bool{}
		for _, n := range out {
			seen[n.OID] = true
		}
		for o, n := range m.byOID {
			if !seen[o] && strings.HasPrefix(o, oid+".") && m.byOID[parentOID(o)] == nil {
				anc := parentOID(o)
				for anc != "" && m.byOID[anc] == nil {
					anc = parentOID(anc)
				}
				if anc == oid {
					out = append(out, n)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return CompareOID(out[i].OID, out[j].OID) < 0 })
	return out
}

// Search finds objects whose name, module or OID contains q (case-insensitive), at most max.
func (m *MIB) Search(q string, max int) []*Node {
	q = strings.ToLower(strings.TrimSpace(q))
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Node
	for _, n := range m.byOID {
		if strings.Contains(strings.ToLower(n.Name), q) || strings.HasPrefix(n.OID, q) || strings.EqualFold(n.Module, q) {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ei, ej := strings.EqualFold(out[i].Name, q), strings.EqualFold(out[j].Name, q)
		if ei != ej {
			return ei
		}
		return CompareOID(out[i].OID, out[j].OID) < 0
	})
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

// Describe is a value as a person reads it: enum names, TimeTicks as days and hours, hex strings by their hint.
func (m *MIB) Describe(v Varbind) string {
	if v.Value == "" && !v.Exception() {
		o := v.OID
		v = withText(v)
		v.OID = o
	}
	n, _ := m.Lookup(v.OID)
	if n == nil || v.Exception() {
		if v.Type == TimeTicks {
			return ticks(v.Uint)
		}
		return v.Value
	}
	switch {
	case v.Type == Integer && len(n.Enums) > 0:
		if s, ok := n.Enums[v.Int]; ok {
			return fmt.Sprintf("%s (%d)", s, v.Int)
		}
	case v.Type == TimeTicks:
		return ticks(v.Uint)
	case v.Type == OctetString && (n.Syntax == "MacAddress" || n.Syntax == "PhysAddress" || n.Hint == "1x:"):
		return hexColon(v.Bytes)
	case v.Type == ObjectID:
		return m.Name(v.Value)
	}
	if n.Units != "" && (v.Type == Integer || v.Type == Gauge32) {
		return v.Value + " " + n.Units
	}
	return v.Value
}

func ticks(t uint64) string {
	s := t / 100
	d, h, mi, se := s/86400, s%86400/3600, s%3600/60, s%60
	if d > 0 {
		return fmt.Sprintf("%d d %02d:%02d:%02d", d, h, mi, se)
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, mi, se)
}

func hexColon(b []byte) string {
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02x", c)
	}
	return strings.Join(parts, ":")
}
