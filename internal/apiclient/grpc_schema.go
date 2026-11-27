package apiclient

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Protobuf schemas for gRPC, from .proto files or the server's reflection, without generated code: the
// messages, enums and services the API client needs to turn JSON into protobuf and back.

// Field types (FieldDescriptorProto.Type).
const (
	pbDouble = 1 + iota
	pbFloat
	pbInt64
	pbUint64
	pbInt32
	pbFixed64
	pbFixed32
	pbBool
	pbString
	pbGroup
	pbMessage
	pbBytes
	pbUint32
	pbEnum
	pbSfixed32
	pbSfixed64
	pbSint32
	pbSint64
)

var pbScalar = map[string]int{"double": pbDouble, "float": pbFloat, "int64": pbInt64, "uint64": pbUint64, "int32": pbInt32, "fixed64": pbFixed64,
	"fixed32": pbFixed32, "bool": pbBool, "string": pbString, "bytes": pbBytes, "uint32": pbUint32, "sfixed32": pbSfixed32, "sfixed64": pbSfixed64,
	"sint32": pbSint32, "sint64": pbSint64}

type pbField struct {
	Name, JSON string
	Num        int
	Type       int
	TypeName   string // ".pkg.Msg" for messages and enums
	Repeated   bool
	Packed     bool
	Oneof      string
	ref        string // the type as written in a .proto, resolved later
	scope      string
}

type pbMsg struct {
	Full     string
	Fields   []*pbField
	MapEntry bool
}

type pbEnumT struct {
	Full   string
	Values []pbEnumVal
}

type pbEnumVal struct {
	Name string
	Num  int
}

type pbMethod struct {
	Name, In, Out              string
	ClientStream, ServerStream bool
	inRef, outRef, scope       string
}

type pbService struct {
	Full    string
	Methods []*pbMethod
}

// ProtoSet is a set of protobuf files: their messages, enums and services by full name (".pkg.Name").
type ProtoSet struct {
	Msgs     map[string]*pbMsg
	Enums    map[string]*pbEnumT
	Services []*pbService
	Warnings []string
	files    map[string]bool
}

func newProtoSet() *ProtoSet {
	return &ProtoSet{Msgs: map[string]*pbMsg{}, Enums: map[string]*pbEnumT{}, files: map[string]bool{}}
}

func jsonName(n string) string {
	var b strings.Builder
	up := false
	for _, r := range n {
		if r == '_' {
			up = true
			continue
		}
		if up && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		up = false
		b.WriteRune(r)
	}
	return b.String()
}

// ---- .proto files ----

type protoLexer struct {
	toks []string
	i    int
	file string
}

func lexProto(src string) []string {
	var out []string
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				i = len(src)
			} else {
				i += j + 4
			}
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(src) && src[j] != c {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(src) {
				j = len(src) - 1
			}
			out = append(out, src[i:j+1])
			i = j + 1
		case c == '_' || c == '.' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '+':
			j := i
			for j < len(src) && (src[j] == '_' || src[j] == '.' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9' || (j == i && (src[j] == '-' || src[j] == '+'))) {
				j++
			}
			if j == i {
				j++
			}
			out = append(out, src[i:j])
			i = j
		default:
			out = append(out, string(c))
			i++
		}
	}
	return out
}

func (l *protoLexer) peek() string {
	if l.i < len(l.toks) {
		return l.toks[l.i]
	}
	return ""
}

func (l *protoLexer) next() string { t := l.peek(); l.i++; return t }

func (l *protoLexer) want(s string) error {
	if t := l.next(); t != s {
		return fmt.Errorf("%s: expected %q, found %q", l.file, s, t)
	}
	return nil
}

// skip skips a statement: to its ";" or past its { ... } block.
func (l *protoLexer) skip() {
	depth := 0
	for l.i < len(l.toks) {
		t := l.next()
		switch t {
		case "{":
			depth++
		case "}":
			depth--
			if depth <= 0 {
				if l.peek() == ";" {
					l.i++
				}
				return
			}
		case ";":
			if depth == 0 {
				return
			}
		}
	}
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') {
		if u, err := strconv.Unquote(`"` + strings.ReplaceAll(s[1:len(s)-1], `"`, `\"`) + `"`); err == nil {
			return u
		}
		return s[1 : len(s)-1]
	}
	return s
}

// ParseProtoFiles reads .proto files (and what they import, looked up beside them and in the import paths).
func ParseProtoFiles(files, importPaths []string) (*ProtoSet, error) {
	ps := newProtoSet()
	for _, f := range files {
		if err := ps.loadFile(f, f, importPaths, true); err != nil {
			return nil, err
		}
	}
	if err := ps.resolve(); err != nil {
		return nil, err
	}
	return ps, nil
}

// ParseProtoText reads one .proto file's text (tests; a pasted file).
func ParseProtoText(name, src string) (*ProtoSet, error) {
	ps := newProtoSet()
	if err := ps.parse(name, src, nil, ""); err != nil {
		return nil, err
	}
	return ps, ps.resolve()
}

func (ps *ProtoSet) loadFile(name, path string, importPaths []string, must bool) error {
	if ps.files[name] {
		return nil
	}
	ps.files[name] = true
	if wkt, ok := wellKnown[name]; ok {
		return ps.parse(name, wkt, nil, "")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if must {
			return fmt.Errorf("proto file: %w", err)
		}
		ps.Warnings = append(ps.Warnings, "import "+name+" not found (types from it cannot be used)")
		return nil
	}
	return ps.parse(name, string(b), importPaths, filepath.Dir(path))
}

func (ps *ProtoSet) parse(name, src string, importPaths []string, dir string) error {
	l := &protoLexer{toks: lexProto(src), file: name}
	pkg, proto3 := "", false
	for l.i < len(l.toks) {
		switch t := l.next(); t {
		case ";":
		case "syntax", "edition":
			_ = l.want("=")
			v := unquote(l.next())
			proto3 = v == "proto3" || t == "edition"
			l.skip()
		case "package":
			pkg = l.next()
			l.skip()
		case "import":
			p := l.next()
			if p == "public" || p == "weak" {
				p = l.next()
			}
			p = unquote(p)
			l.skip()
			path := ""
			for _, d := range append([]string{dir}, importPaths...) {
				if d == "" {
					continue
				}
				if _, err := os.Stat(filepath.Join(d, p)); err == nil {
					path = filepath.Join(d, p)
					break
				}
			}
			if err := ps.loadFile(p, path, importPaths, false); err != nil {
				return err
			}
		case "message":
			if err := ps.parseMessage(l, pkg, proto3); err != nil {
				return err
			}
		case "enum":
			if err := ps.parseEnum(l, pkg); err != nil {
				return err
			}
		case "service":
			if err := ps.parseService(l, pkg); err != nil {
				return err
			}
		default:
			l.skip() // option, extend ...
		}
	}
	return nil
}

func full(scope, n string) string {
	if scope == "" {
		return "." + n
	}
	return "." + scope + "." + n
}

func (ps *ProtoSet) parseMessage(l *protoLexer, scope string, proto3 bool) error {
	name := l.next()
	if err := l.want("{"); err != nil {
		return err
	}
	fn := full(scope, name)
	m := &pbMsg{Full: fn}
	ps.Msgs[fn] = m
	inner := strings.TrimPrefix(fn, ".")
	oneof := ""
	for {
		t := l.next()
		switch t {
		case "":
			return fmt.Errorf("%s: message %s is not closed", l.file, name)
		case "}":
			if oneof != "" {
				oneof = ""
				continue
			}
			return nil
		case ";":
		case "message":
			if err := ps.parseMessage(l, inner, proto3); err != nil {
				return err
			}
		case "enum":
			if err := ps.parseEnum(l, inner); err != nil {
				return err
			}
		case "oneof":
			oneof = l.next()
			if err := l.want("{"); err != nil {
				return err
			}
		case "option", "reserved", "extensions", "extend":
			l.i--
			l.skip()
		case "map":
			// map<K, V> name = n;
			_ = l.want("<")
			k := l.next()
			_ = l.want(",")
			v := l.next()
			_ = l.want(">")
			fname := l.next()
			_ = l.want("=")
			num, _ := strconv.Atoi(l.next())
			entry := &pbMsg{Full: fn + "." + strings.ToUpper(fname[:1]) + jsonName(fname)[1:] + "Entry", MapEntry: true}
			kf := &pbField{Name: "key", JSON: "key", Num: 1, Type: pbScalar[k]}
			vf := &pbField{Name: "value", JSON: "value", Num: 2, scope: inner}
			if st, ok := pbScalar[v]; ok {
				vf.Type = st
			} else {
				vf.ref = v
			}
			entry.Fields = []*pbField{kf, vf}
			ps.Msgs[entry.Full] = entry
			f := &pbField{Name: fname, JSON: jsonName(fname), Num: num, Type: pbMessage, TypeName: entry.Full, Repeated: true}
			ps.fieldOpts(l, f)
			m.Fields = append(m.Fields, f)
		default:
			f := &pbField{scope: inner, Oneof: oneof}
			typ := t
			switch t {
			case "repeated":
				f.Repeated = true
				typ = l.next()
			case "optional", "required":
				typ = l.next()
			}
			if typ == "group" { // proto2 groups: not supported, skipped
				l.skip()
				continue
			}
			f.Name = l.next()
			f.JSON = jsonName(f.Name)
			if err := l.want("="); err != nil {
				return err
			}
			f.Num, _ = strconv.Atoi(l.next())
			if st, ok := pbScalar[typ]; ok {
				f.Type = st
				f.Packed = f.Repeated && proto3 && st != pbString && st != pbBytes
			} else {
				f.ref = typ
			}
			ps.fieldOpts(l, f)
			m.Fields = append(m.Fields, f)
		}
	}
}

// fieldOpts reads [packed = true, json_name = "x"] and the closing ";".
func (ps *ProtoSet) fieldOpts(l *protoLexer, f *pbField) {
	if l.peek() == "[" {
		l.next()
		for l.i < len(l.toks) && l.peek() != "]" {
			k := l.next()
			if l.peek() == "=" {
				l.next()
				v := l.next()
				switch k {
				case "packed":
					f.Packed = v == "true"
				case "json_name":
					f.JSON = unquote(v)
				}
				if v == "{" { // an aggregate option value
					depth := 1
					for depth > 0 && l.i < len(l.toks) {
						switch l.next() {
						case "{":
							depth++
						case "}":
							depth--
						}
					}
				}
			}
		}
		l.next()
	}
	if l.peek() == ";" {
		l.next()
	}
}

func (ps *ProtoSet) parseEnum(l *protoLexer, scope string) error {
	name := l.next()
	if err := l.want("{"); err != nil {
		return err
	}
	e := &pbEnumT{Full: full(scope, name)}
	ps.Enums[e.Full] = e
	for {
		t := l.next()
		switch t {
		case "":
			return fmt.Errorf("%s: enum %s is not closed", l.file, name)
		case "}":
			return nil
		case ";":
		case "option", "reserved":
			l.i--
			l.skip()
		default:
			if l.peek() != "=" {
				l.i--
				l.skip()
				continue
			}
			l.next()
			n, _ := strconv.Atoi(l.next())
			e.Values = append(e.Values, pbEnumVal{Name: t, Num: n})
			if l.peek() == "[" {
				for l.i < len(l.toks) && l.next() != "]" {
				}
			}
			if l.peek() == ";" {
				l.next()
			}
		}
	}
}

func (ps *ProtoSet) parseService(l *protoLexer, scope string) error {
	name := l.next()
	if err := l.want("{"); err != nil {
		return err
	}
	s := &pbService{Full: full(scope, name)}
	ps.Services = append(ps.Services, s)
	for {
		t := l.next()
		switch t {
		case "":
			return fmt.Errorf("%s: service %s is not closed", l.file, name)
		case "}":
			return nil
		case ";":
		case "rpc":
			m := &pbMethod{Name: l.next(), scope: scope}
			_ = l.want("(")
			if l.peek() == "stream" {
				l.next()
				m.ClientStream = true
			}
			m.inRef = l.next()
			_ = l.want(")")
			if err := l.want("returns"); err != nil {
				return err
			}
			_ = l.want("(")
			if l.peek() == "stream" {
				l.next()
				m.ServerStream = true
			}
			m.outRef = l.next()
			_ = l.want(")")
			if l.peek() == "{" {
				l.skip()
			} else if l.peek() == ";" {
				l.next()
			}
			s.Methods = append(s.Methods, m)
		default:
			l.i--
			l.skip()
		}
	}
}

// lookup finds a type named ref as seen from scope ("a.b.C"): the innermost first, like protoc.
func (ps *ProtoSet) lookup(ref, scope string) (string, bool) {
	if strings.HasPrefix(ref, ".") {
		_, m := ps.Msgs[ref]
		_, e := ps.Enums[ref]
		return ref, m || e
	}
	for s := scope; ; {
		c := "." + ref
		if s != "" {
			c = "." + s + "." + ref
		}
		if _, ok := ps.Msgs[c]; ok {
			return c, true
		}
		if _, ok := ps.Enums[c]; ok {
			return c, true
		}
		if s == "" {
			return "", false
		}
		if i := strings.LastIndex(s, "."); i >= 0 {
			s = s[:i]
		} else {
			s = ""
		}
	}
}

func (ps *ProtoSet) resolve() error {
	var missing []string
	for _, m := range ps.Msgs {
		for _, f := range m.Fields {
			if f.ref == "" {
				continue
			}
			n, ok := ps.lookup(f.ref, f.scope)
			if !ok {
				missing = append(missing, f.ref)
				f.Type, f.TypeName = pbBytes, "" // unknown: kept as bytes
				continue
			}
			f.TypeName = n
			if _, isEnum := ps.Enums[n]; isEnum {
				f.Type = pbEnum
				f.Packed = f.Repeated
			} else {
				f.Type = pbMessage
			}
			f.ref = ""
		}
	}
	for _, s := range ps.Services {
		for _, m := range s.Methods {
			if m.inRef != "" {
				if n, ok := ps.lookup(m.inRef, m.scope); ok {
					m.In = n
				} else {
					missing = append(missing, m.inRef)
				}
				if n, ok := ps.lookup(m.outRef, m.scope); ok {
					m.Out = n
				} else {
					missing = append(missing, m.outRef)
				}
				m.inRef, m.outRef = "", ""
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		ps.Warnings = append(ps.Warnings, "unknown types: "+strings.Join(uniqStrings(missing), ", "))
	}
	return nil
}

func uniqStrings(l []string) []string {
	var out []string
	for i, s := range l {
		if i == 0 || s != l[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// method finds "pkg.Service/Method" (or "pkg.Service.Method").
func (ps *ProtoSet) method(name string) (*pbService, *pbMethod, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	svc, m := name, ""
	if i := strings.LastIndexAny(name, "/."); i > 0 {
		svc, m = name[:i], name[i+1:]
	}
	for _, s := range ps.Services {
		if strings.TrimPrefix(s.Full, ".") == svc || strings.HasSuffix(s.Full, "."+svc) {
			for _, x := range s.Methods {
				if x.Name == m {
					return s, x, nil
				}
			}
		}
	}
	return nil, nil, fmt.Errorf("gRPC method %q is not in the schema", name)
}

// ---- FileDescriptorProto (from server reflection) ----

func (ps *ProtoSet) addDescriptor(b []byte) (string, []string, error) {
	var name, pkg, syntax string
	var deps []string
	var msgs, enums, svcs [][]byte
	err := pbEach(b, func(num int, wt int, v uint64, d []byte) {
		switch num {
		case 1:
			name = string(d)
		case 2:
			pkg = string(d)
		case 3:
			deps = append(deps, string(d))
		case 4:
			msgs = append(msgs, d)
		case 5:
			enums = append(enums, d)
		case 6:
			svcs = append(svcs, d)
		case 12:
			syntax = string(d)
		}
	})
	if err != nil {
		return "", nil, err
	}
	if ps.files[name] {
		return name, deps, nil
	}
	ps.files[name] = true
	proto3 := syntax == "proto3" || syntax == "editions"
	for _, d := range msgs {
		if err := ps.descMsg(d, pkg, proto3); err != nil {
			return "", nil, err
		}
	}
	for _, d := range enums {
		ps.descEnum(d, pkg)
	}
	for _, d := range svcs {
		s := &pbService{}
		_ = pbEach(d, func(num, wt int, v uint64, d []byte) {
			switch num {
			case 1:
				s.Full = full(pkg, string(d))
			case 2:
				m := &pbMethod{}
				_ = pbEach(d, func(num, wt int, v uint64, d []byte) {
					switch num {
					case 1:
						m.Name = string(d)
					case 2:
						m.In = string(d)
					case 3:
						m.Out = string(d)
					case 5:
						m.ClientStream = v != 0
					case 6:
						m.ServerStream = v != 0
					}
				})
				s.Methods = append(s.Methods, m)
			}
		})
		ps.Services = append(ps.Services, s)
	}
	return name, deps, nil
}

func (ps *ProtoSet) descMsg(b []byte, scope string, proto3 bool) error {
	m := &pbMsg{}
	var nested, enums, fields, oneofs [][]byte
	var name string
	err := pbEach(b, func(num, wt int, v uint64, d []byte) {
		switch num {
		case 1:
			name = string(d)
		case 2:
			fields = append(fields, d)
		case 3:
			nested = append(nested, d)
		case 4:
			enums = append(enums, d)
		case 7:
			_ = pbEach(d, func(num, wt int, v uint64, d []byte) {
				if num == 7 {
					m.MapEntry = v != 0
				}
			})
		case 8:
			oneofs = append(oneofs, d)
		}
	})
	if err != nil {
		return err
	}
	m.Full = full(scope, name)
	inner := strings.TrimPrefix(m.Full, ".")
	ps.Msgs[m.Full] = m
	var oneofNames []string
	for _, d := range oneofs {
		n := ""
		_ = pbEach(d, func(num, wt int, v uint64, d []byte) {
			if num == 1 {
				n = string(d)
			}
		})
		oneofNames = append(oneofNames, n)
	}
	for _, d := range fields {
		f := &pbField{}
		packedSet, packed, oneof, p3opt := false, false, -1, false
		_ = pbEach(d, func(num, wt int, v uint64, d []byte) {
			switch num {
			case 1:
				f.Name = string(d)
			case 3:
				f.Num = int(v)
			case 4:
				f.Repeated = v == 3
			case 5:
				f.Type = int(v)
			case 6:
				f.TypeName = string(d)
			case 8:
				_ = pbEach(d, func(num, wt int, v uint64, d []byte) {
					if num == 2 {
						packedSet, packed = true, v != 0
					}
				})
			case 9:
				oneof = int(v)
			case 10:
				f.JSON = string(d)
			case 17:
				p3opt = v != 0
			}
		})
		if f.JSON == "" {
			f.JSON = jsonName(f.Name)
		}
		if oneof >= 0 && oneof < len(oneofNames) && !p3opt {
			f.Oneof = oneofNames[oneof]
		}
		scalarNum := f.Type != pbString && f.Type != pbBytes && f.Type != pbMessage && f.Type != pbGroup
		f.Packed = f.Repeated && scalarNum && (packed || !packedSet && proto3)
		m.Fields = append(m.Fields, f)
	}
	for _, d := range nested {
		if err := ps.descMsg(d, inner, proto3); err != nil {
			return err
		}
	}
	for _, d := range enums {
		ps.descEnum(d, inner)
	}
	return nil
}

func (ps *ProtoSet) descEnum(b []byte, scope string) {
	e := &pbEnumT{}
	_ = pbEach(b, func(num, wt int, v uint64, d []byte) {
		switch num {
		case 1:
			e.Full = full(scope, string(d))
		case 2:
			ev := pbEnumVal{}
			_ = pbEach(d, func(num, wt int, v uint64, d []byte) {
				switch num {
				case 1:
					ev.Name = string(d)
				case 2:
					ev.Num = int(int32(v))
				}
			})
			e.Values = append(e.Values, ev)
		}
	})
	ps.Enums[e.Full] = e
}

// ---- protobuf wire format ----

var errPBShort = errors.New("protobuf: the message is cut short")

func pbVarint(b []byte) (uint64, int, error) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return v, i + 1, nil
		}
	}
	return 0, 0, errPBShort
}

// pbEach calls f for each field of a message: varint and fixed values in v, length-delimited ones in d.
func pbEach(b []byte, f func(num, wt int, v uint64, d []byte)) error {
	for len(b) > 0 {
		key, n, err := pbVarint(b)
		if err != nil {
			return err
		}
		b = b[n:]
		num, wt := int(key>>3), int(key&7)
		switch wt {
		case 0:
			v, n, err := pbVarint(b)
			if err != nil {
				return err
			}
			b = b[n:]
			f(num, wt, v, nil)
		case 1:
			if len(b) < 8 {
				return errPBShort
			}
			v := uint64(0)
			for i := 7; i >= 0; i-- {
				v = v<<8 | uint64(b[i])
			}
			f(num, wt, v, b[:8])
			b = b[8:]
		case 2:
			l, n, err := pbVarint(b)
			if err != nil || uint64(len(b)-n) < l {
				return errPBShort
			}
			f(num, wt, 0, b[n:n+int(l)])
			b = b[n+int(l):]
		case 5:
			if len(b) < 4 {
				return errPBShort
			}
			v := uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24
			f(num, wt, v, b[:4])
			b = b[4:]
		case 3, 4: // groups: skipped (their fields come as if top level, then the end marker)
		default:
			return fmt.Errorf("protobuf: unknown wire type %d", wt)
		}
	}
	return nil
}

func pbAppendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func pbAppendKey(b []byte, num, wt int) []byte { return pbAppendVarint(b, uint64(num)<<3|uint64(wt)) }

func pbAppendBytes(b []byte, num int, d []byte) []byte {
	b = pbAppendKey(b, num, 2)
	b = pbAppendVarint(b, uint64(len(d)))
	return append(b, d...)
}

// well-known types (google/protobuf/*.proto) that .proto files import.
var wellKnown = map[string]string{
	"google/protobuf/empty.proto":     `syntax = "proto3"; package google.protobuf; message Empty {}`,
	"google/protobuf/timestamp.proto": `syntax = "proto3"; package google.protobuf; message Timestamp { int64 seconds = 1; int32 nanos = 2; }`,
	"google/protobuf/duration.proto":  `syntax = "proto3"; package google.protobuf; message Duration { int64 seconds = 1; int32 nanos = 2; }`,
	"google/protobuf/wrappers.proto": `syntax = "proto3"; package google.protobuf;
message DoubleValue { double value = 1; } message FloatValue { float value = 1; } message Int64Value { int64 value = 1; }
message UInt64Value { uint64 value = 1; } message Int32Value { int32 value = 1; } message UInt32Value { uint32 value = 1; }
message BoolValue { bool value = 1; } message StringValue { string value = 1; } message BytesValue { bytes value = 1; }`,
	"google/protobuf/struct.proto": `syntax = "proto3"; package google.protobuf;
message Struct { map<string, Value> fields = 1; }
message Value { oneof kind { NullValue null_value = 1; double number_value = 2; string string_value = 3; bool bool_value = 4; Struct struct_value = 5; ListValue list_value = 6; } }
enum NullValue { NULL_VALUE = 0; }
message ListValue { repeated Value values = 1; }`,
	"google/protobuf/field_mask.proto": `syntax = "proto3"; package google.protobuf; message FieldMask { repeated string paths = 1; }`,
	"google/protobuf/any.proto":        `syntax = "proto3"; package google.protobuf; message Any { string type_url = 1; bytes value = 2; }`,
	"google/protobuf/descriptor.proto": `syntax = "proto2"; package google.protobuf; message FileDescriptorSet {} message MethodOptions {} message FieldOptions {} message MessageOptions {} message ServiceOptions {} message FileOptions {} message EnumOptions {} message EnumValueOptions {}`,
	"google/api/annotations.proto":     `syntax = "proto3"; package google.api;`,
	"google/api/http.proto":            `syntax = "proto3"; package google.api; message HttpRule {}`,
	"google/api/field_behavior.proto":  `syntax = "proto3"; package google.api;`,
	"google/api/client.proto":          `syntax = "proto3"; package google.api;`,
}
