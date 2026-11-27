package apiclient

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// JSON <-> protobuf with a ProtoSet, following proto3's JSON mapping: field names or their lowerCamel JSON
// names, 64-bit integers as strings, enums by name, bytes in base64, maps as objects, and the well-known
// types Timestamp, Duration, wrappers, Struct / Value / ListValue, FieldMask and Empty in their JSON forms.

// omap is a JSON object that keeps its keys' order.
type omap struct {
	keys []string
	m    map[string]any
}

func newOmap() *omap { return &omap{m: map[string]any{}} }

func (o *omap) set(k string, v any) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

func (o *omap) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(o.m[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// ---- JSON to protobuf ----

func (ps *ProtoSet) encodeJSON(msg, text string) ([]byte, error) {
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	var v any
	if strings.TrimSpace(text) == "" {
		v = map[string]any{}
	} else if err := d.Decode(&v); err != nil {
		return nil, fmt.Errorf("the message is not JSON: %w", err)
	}
	return ps.encode(msg, v, "")
}

func (ps *ProtoSet) encode(msg string, v any, at string) ([]byte, error) {
	m, ok := ps.Msgs[msg]
	if !ok {
		return nil, fmt.Errorf("unknown message type %s", strings.TrimPrefix(msg, "."))
	}
	if b, done, err := ps.encodeWKT(msg, v, at); done {
		return b, err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		if v == nil {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: a JSON object is needed for %s", where(at), strings.TrimPrefix(msg, "."))
	}
	byName := map[string]*pbField{}
	for _, f := range m.Fields {
		byName[f.Name], byName[f.JSON] = f, f
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { // field number order
		fi, fj := byName[keys[i]], byName[keys[j]]
		if fi == nil || fj == nil {
			return keys[i] < keys[j]
		}
		return fi.Num < fj.Num
	})
	var out []byte
	for _, k := range keys {
		f := byName[k]
		if f == nil {
			var names []string
			for _, x := range m.Fields {
				names = append(names, x.JSON)
			}
			return nil, fmt.Errorf("%s: %s has no field %q (fields: %s)", where(at), strings.TrimPrefix(msg, "."), k, strings.Join(names, ", "))
		}
		val := obj[k]
		if val == nil {
			continue
		}
		path := join(at, k)
		var err error
		switch {
		case f.Type == pbMessage && ps.Msgs[f.TypeName] != nil && ps.Msgs[f.TypeName].MapEntry:
			mo, ok := val.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: a JSON object is needed (a map)", path)
			}
			entry := ps.Msgs[f.TypeName]
			mk := make([]string, 0, len(mo))
			for x := range mo {
				mk = append(mk, x)
			}
			sort.Strings(mk)
			for _, x := range mk {
				var e []byte
				if e, err = ps.encodeField(nil, entry.Fields[0], json.Number(x), path, true); err != nil {
					if e, err = ps.encodeField(nil, entry.Fields[0], x, path, true); err != nil {
						return nil, err
					}
				}
				if e, err = ps.encodeField(e, entry.Fields[1], mo[x], join(path, x), true); err != nil {
					return nil, err
				}
				out = pbAppendBytes(out, f.Num, e)
			}
		case f.Repeated:
			list, ok := val.([]any)
			if !ok {
				return nil, fmt.Errorf("%s: a JSON array is needed", path)
			}
			if f.Packed {
				var p []byte
				for i, x := range list {
					if p, err = ps.encodeValue(p, f, x, fmt.Sprintf("%s[%d]", path, i)); err != nil {
						return nil, err
					}
				}
				out = pbAppendBytes(out, f.Num, p)
			} else {
				for i, x := range list {
					if out, err = ps.encodeField(out, f, x, fmt.Sprintf("%s[%d]", path, i), true); err != nil {
						return nil, err
					}
				}
			}
		default:
			if out, err = ps.encodeField(out, f, val, path, true); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func where(at string) string {
	if at == "" {
		return "message"
	}
	return at
}

func join(a, b string) string {
	if a == "" {
		return b
	}
	return a + "." + b
}

func wireType(t int) int {
	switch t {
	case pbDouble, pbFixed64, pbSfixed64:
		return 1
	case pbFloat, pbFixed32, pbSfixed32:
		return 5
	case pbString, pbBytes, pbMessage:
		return 2
	}
	return 0
}

func (ps *ProtoSet) encodeField(out []byte, f *pbField, v any, at string, key bool) ([]byte, error) {
	if f.Type == pbMessage {
		b, err := ps.encode(f.TypeName, v, at)
		if err != nil {
			return nil, err
		}
		return pbAppendBytes(out, f.Num, b), nil
	}
	if f.Type == pbString || f.Type == pbBytes {
		var d []byte
		s, ok := v.(string)
		if !ok {
			if n, isNum := v.(json.Number); isNum && f.Type == pbString {
				s, ok = n.String(), true
			}
		}
		if !ok {
			return nil, fmt.Errorf("%s: a string is needed", at)
		}
		d = []byte(s)
		if f.Type == pbBytes {
			b, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				if b, err = base64.URLEncoding.DecodeString(s); err != nil {
					return nil, fmt.Errorf("%s: bytes are written in base64", at)
				}
			}
			d = b
		}
		return pbAppendBytes(out, f.Num, d), nil
	}
	out = pbAppendKey(out, f.Num, wireType(f.Type))
	return ps.encodeValue(out, f, v, at)
}

func pbNum(v any, at string) (string, error) {
	switch x := v.(type) {
	case json.Number:
		return x.String(), nil
	case string:
		return strings.TrimSpace(x), nil
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	case bool:
		if x {
			return "1", nil
		}
		return "0", nil
	}
	return "", fmt.Errorf("%s: a number is needed", at)
}

// encodeValue appends a scalar or enum value without its key (packed lists, after encodeField's key).
func (ps *ProtoSet) encodeValue(out []byte, f *pbField, v any, at string) ([]byte, error) {
	if f.Type == pbBool {
		b, ok := v.(bool)
		if !ok {
			if s, isS := v.(string); isS && (s == "true" || s == "false") {
				b, ok = s == "true", true
			}
		}
		if !ok {
			return nil, fmt.Errorf("%s: true or false is needed", at)
		}
		if b {
			return append(out, 1), nil
		}
		return append(out, 0), nil
	}
	if f.Type == pbEnum {
		if s, ok := v.(string); ok {
			if e := ps.Enums[f.TypeName]; e != nil {
				for _, ev := range e.Values {
					if ev.Name == s {
						return pbAppendVarint(out, uint64(int64(ev.Num))), nil
					}
				}
				var names []string
				for _, ev := range e.Values {
					names = append(names, ev.Name)
				}
				return nil, fmt.Errorf("%s: %q is not one of %s", at, s, strings.Join(names, ", "))
			}
		}
	}
	s, err := pbNum(v, at)
	if err != nil {
		return nil, err
	}
	bad := func() ([]byte, error) { return nil, fmt.Errorf("%s: %q is not a valid %s", at, s, typeName(f.Type)) }
	switch f.Type {
	case pbDouble, pbFloat:
		x, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return bad()
		}
		if f.Type == pbFloat {
			return binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(x))), nil
		}
		return binary.LittleEndian.AppendUint64(out, math.Float64bits(x)), nil
	case pbInt32, pbInt64, pbSint32, pbSint64, pbSfixed32, pbSfixed64, pbEnum:
		x, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			fx, ferr := strconv.ParseFloat(s, 64)
			if ferr != nil || fx != math.Trunc(fx) {
				return bad()
			}
			x = int64(fx)
		}
		switch f.Type {
		case pbSint32, pbSint64:
			return pbAppendVarint(out, uint64((x<<1)^(x>>63))), nil
		case pbSfixed32:
			return binary.LittleEndian.AppendUint32(out, uint32(int32(x))), nil
		case pbSfixed64:
			return binary.LittleEndian.AppendUint64(out, uint64(x)), nil
		}
		return pbAppendVarint(out, uint64(x)), nil
	default: // unsigned
		x, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return bad()
		}
		switch f.Type {
		case pbFixed32:
			return binary.LittleEndian.AppendUint32(out, uint32(x)), nil
		case pbFixed64:
			return binary.LittleEndian.AppendUint64(out, x), nil
		}
		return pbAppendVarint(out, x), nil
	}
}

func typeName(t int) string {
	for k, v := range pbScalar {
		if v == t {
			return k
		}
	}
	if t == pbEnum {
		return "enum"
	}
	return "value"
}

const wktPrefix = ".google.protobuf."

func (ps *ProtoSet) encodeWKT(msg string, v any, at string) ([]byte, bool, error) {
	if !strings.HasPrefix(msg, wktPrefix) {
		return nil, false, nil
	}
	name := strings.TrimPrefix(msg, wktPrefix)
	switch name {
	case "Timestamp":
		s, ok := v.(string)
		if !ok {
			return nil, false, nil // {"seconds": ..} also works
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, true, fmt.Errorf("%s: a time like 2026-01-02T15:04:05Z is needed", at)
		}
		return secNanos(t.Unix(), int64(t.Nanosecond())), true, nil
	case "Duration":
		s, ok := v.(string)
		if !ok {
			return nil, false, nil
		}
		d, err := time.ParseDuration(s)
		if err != nil || !strings.HasSuffix(s, "s") {
			return nil, true, fmt.Errorf("%s: a duration like \"1.5s\" is needed", at)
		}
		return secNanos(int64(d/time.Second), int64(d%time.Second)), true, nil
	case "DoubleValue", "FloatValue", "Int64Value", "UInt64Value", "Int32Value", "UInt32Value", "BoolValue", "StringValue", "BytesValue":
		if _, isObj := v.(map[string]any); isObj {
			return nil, false, nil
		}
		b, err := ps.encodeField(nil, ps.Msgs[msg].Fields[0], v, at, true)
		return b, true, err
	case "FieldMask":
		s, ok := v.(string)
		if !ok {
			return nil, false, nil
		}
		var out []byte
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = pbAppendBytes(out, 1, []byte(snake(p)))
			}
		}
		return out, true, nil
	case "Struct":
		o, ok := v.(map[string]any)
		if !ok {
			return nil, true, fmt.Errorf("%s: a JSON object is needed", at)
		}
		var out []byte
		keys := make([]string, 0, len(o))
		for k := range o {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			vb, _, err := ps.encodeWKT(wktPrefix+"Value", o[k], join(at, k))
			if err != nil {
				return nil, true, err
			}
			e := pbAppendBytes(nil, 1, []byte(k))
			e = pbAppendBytes(e, 2, vb)
			out = pbAppendBytes(out, 1, e)
		}
		return out, true, nil
	case "ListValue":
		l, ok := v.([]any)
		if !ok {
			return nil, true, fmt.Errorf("%s: a JSON array is needed", at)
		}
		var out []byte
		for i, x := range l {
			vb, _, err := ps.encodeWKT(wktPrefix+"Value", x, fmt.Sprintf("%s[%d]", at, i))
			if err != nil {
				return nil, true, err
			}
			out = pbAppendBytes(out, 1, vb)
		}
		return out, true, nil
	case "Value":
		switch x := v.(type) {
		case nil:
			return pbAppendVarint(pbAppendKey(nil, 1, 0), 0), true, nil
		case json.Number:
			f, _ := x.Float64()
			return binary.LittleEndian.AppendUint64(pbAppendKey(nil, 2, 1), math.Float64bits(f)), true, nil
		case string:
			return pbAppendBytes(nil, 3, []byte(x)), true, nil
		case bool:
			b := byte(0)
			if x {
				b = 1
			}
			return append(pbAppendKey(nil, 4, 0), b), true, nil
		case map[string]any:
			s, _, err := ps.encodeWKT(wktPrefix+"Struct", x, at)
			return pbAppendBytes(nil, 5, s), true, err
		case []any:
			s, _, err := ps.encodeWKT(wktPrefix+"ListValue", x, at)
			return pbAppendBytes(nil, 6, s), true, err
		}
	}
	return nil, false, nil
}

func secNanos(s, n int64) []byte {
	var out []byte
	if s != 0 {
		out = pbAppendVarint(pbAppendKey(out, 1, 0), uint64(s))
	}
	if n != 0 {
		out = pbAppendVarint(pbAppendKey(out, 2, 0), uint64(n))
	}
	return out
}

func snake(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ---- protobuf to JSON ----

func (ps *ProtoSet) decodeJSON(msg string, b []byte) (string, error) {
	v, err := ps.decode(msg, b)
	if err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(v, "", "  ")
	return string(out), err
}

func (ps *ProtoSet) decode(msg string, b []byte) (any, error) {
	m, ok := ps.Msgs[msg]
	if !ok {
		return nil, fmt.Errorf("unknown message type %s", strings.TrimPrefix(msg, "."))
	}
	byNum := map[int]*pbField{}
	for _, f := range m.Fields {
		byNum[f.Num] = f
	}
	o := newOmap()
	var lists = map[string][]any{}
	var maps = map[string]*omap{}
	var derr error
	err := pbEach(b, func(n, wt int, v uint64, d []byte) {
		f := byNum[n]
		if f == nil || derr != nil {
			return
		}
		if f.Type == pbMessage && ps.Msgs[f.TypeName] != nil && ps.Msgs[f.TypeName].MapEntry {
			entry := ps.Msgs[f.TypeName]
			kv, err := ps.decode(f.TypeName, d)
			if err != nil {
				derr = err
				return
			}
			e := kv.(*omap)
			k := fmt.Sprint(e.m["key"])
			if e.m["key"] == nil {
				k = zeroText(entry.Fields[0].Type)
			}
			if maps[f.JSON] == nil {
				maps[f.JSON] = newOmap()
				o.set(f.JSON, maps[f.JSON])
			}
			val, ok := e.m["value"]
			if !ok {
				val = ps.zero(entry.Fields[1])
			}
			maps[f.JSON].set(k, val)
			return
		}
		var vals []any
		if wt == 2 && f.Type != pbString && f.Type != pbBytes && f.Type != pbMessage { // packed
			for p := d; len(p) > 0; {
				x, used, err := readPacked(p, f.Type)
				if err != nil {
					derr = err
					return
				}
				vals = append(vals, ps.scalar(f, x))
				p = p[used:]
			}
		} else {
			x, err := ps.value(f, v, d)
			if err != nil {
				derr = err
				return
			}
			vals = []any{x}
		}
		if f.Repeated {
			lists[f.JSON] = append(lists[f.JSON], vals...)
			o.set(f.JSON, lists[f.JSON])
		} else if len(vals) > 0 {
			o.set(f.JSON, vals[len(vals)-1])
		}
	})
	if err == nil {
		err = derr
	}
	if err != nil {
		return nil, err
	}
	return ps.decodeWKT(msg, o), nil
}

func zeroText(t int) string {
	if t == pbBool {
		return "false"
	}
	if t == pbString {
		return ""
	}
	return "0"
}

func readPacked(p []byte, t int) (uint64, int, error) {
	switch wireType(t) {
	case 1:
		if len(p) < 8 {
			return 0, 0, errPBShort
		}
		return binary.LittleEndian.Uint64(p), 8, nil
	case 5:
		if len(p) < 4 {
			return 0, 0, errPBShort
		}
		return uint64(binary.LittleEndian.Uint32(p)), 4, nil
	}
	return pbVarint(p)
}

func (ps *ProtoSet) value(f *pbField, v uint64, d []byte) (any, error) {
	switch f.Type {
	case pbString:
		return string(d), nil
	case pbBytes:
		return base64.StdEncoding.EncodeToString(d), nil
	case pbMessage:
		return ps.decode(f.TypeName, d)
	}
	return ps.scalar(f, v), nil
}

func (ps *ProtoSet) scalar(f *pbField, v uint64) any {
	switch f.Type {
	case pbDouble:
		return jsonFloat(math.Float64frombits(v))
	case pbFloat:
		return jsonFloat(float64(math.Float32frombits(uint32(v))))
	case pbInt64, pbSfixed64:
		return strconv.FormatInt(int64(v), 10)
	case pbUint64, pbFixed64:
		return strconv.FormatUint(v, 10)
	case pbSint64:
		return strconv.FormatInt(int64(v>>1)^-int64(v&1), 10)
	case pbInt32, pbSfixed32:
		return int64(int32(v))
	case pbSint32:
		return int64(int32(uint32(v>>1) ^ -uint32(v&1)))
	case pbUint32, pbFixed32:
		return int64(uint32(v))
	case pbBool:
		return v != 0
	case pbEnum:
		if e := ps.Enums[f.TypeName]; e != nil {
			for _, ev := range e.Values {
				if ev.Num == int(int32(v)) {
					return ev.Name
				}
			}
		}
		return int64(int32(v))
	}
	return v
}

func jsonFloat(f float64) any {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	return f
}

func (ps *ProtoSet) zero(f *pbField) any {
	switch f.Type {
	case pbMessage:
		return newOmap()
	case pbString, pbBytes:
		return ""
	case pbBool:
		return false
	case pbInt64, pbUint64, pbSint64, pbFixed64, pbSfixed64:
		return "0"
	case pbEnum:
		if e := ps.Enums[f.TypeName]; e != nil && len(e.Values) > 0 {
			return e.Values[0].Name
		}
	}
	return 0
}

func (ps *ProtoSet) decodeWKT(msg string, o *omap) any {
	if !strings.HasPrefix(msg, wktPrefix) {
		return o
	}
	i64 := func(k string) int64 {
		switch x := o.m[k].(type) {
		case string:
			n, _ := strconv.ParseInt(x, 10, 64)
			return n
		case int64:
			return x
		}
		return 0
	}
	switch strings.TrimPrefix(msg, wktPrefix) {
	case "Timestamp":
		return time.Unix(i64("seconds"), i64("nanos")).UTC().Format(time.RFC3339Nano)
	case "Duration":
		d := time.Duration(i64("seconds"))*time.Second + time.Duration(i64("nanos"))
		return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + "s"
	case "DoubleValue", "FloatValue", "Int64Value", "UInt64Value", "Int32Value", "UInt32Value", "BoolValue", "StringValue", "BytesValue":
		if v, ok := o.m["value"]; ok {
			return v
		}
		return ps.zero(ps.Msgs[msg].Fields[0])
	case "FieldMask":
		var l []string
		if ps, ok := o.m["paths"].([]any); ok {
			for _, p := range ps {
				l = append(l, jsonName(fmt.Sprint(p)))
			}
		}
		return strings.Join(l, ",")
	case "Struct":
		if f, ok := o.m["fields"].(*omap); ok {
			return f
		}
		return newOmap()
	case "ListValue":
		if l, ok := o.m["values"].([]any); ok {
			return l
		}
		return []any{}
	case "Value":
		for _, k := range []string{"numberValue", "stringValue", "boolValue", "structValue", "listValue"} {
			if v, ok := o.m[k]; ok {
				return v
			}
		}
		return nil
	case "Empty":
		return newOmap()
	}
	return o
}

// example is a JSON message with each field at its zero value (to start from in the editor).
func (ps *ProtoSet) example(msg string, depth int) any {
	m := ps.Msgs[msg]
	if m == nil {
		return newOmap()
	}
	if strings.HasPrefix(msg, wktPrefix) {
		switch strings.TrimPrefix(msg, wktPrefix) {
		case "Timestamp":
			return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		case "Duration":
			return "1s"
		case "Struct":
			return newOmap()
		case "Value":
			return nil
		case "ListValue":
			return []any{}
		case "FieldMask":
			return ""
		case "Empty":
			return newOmap()
		}
		if len(m.Fields) == 1 && m.Fields[0].Name == "value" {
			return ps.zero(m.Fields[0])
		}
	}
	o := newOmap()
	seenOneof := map[string]bool{}
	for _, f := range m.Fields {
		if f.Oneof != "" {
			if seenOneof[f.Oneof] {
				continue // one of a oneof
			}
			seenOneof[f.Oneof] = true
		}
		var v any
		if f.Type == pbMessage {
			sub := ps.Msgs[f.TypeName]
			switch {
			case sub != nil && sub.MapEntry:
				mo := newOmap()
				if depth < 3 {
					var val any = ps.zero(sub.Fields[1])
					if sub.Fields[1].Type == pbMessage {
						val = ps.example(sub.Fields[1].TypeName, depth+1)
					}
					mo.set("key", val)
				}
				o.set(f.JSON, mo)
				continue
			case depth >= 3:
				v = newOmap()
			default:
				v = ps.example(f.TypeName, depth+1)
			}
		} else {
			v = ps.zero(f)
		}
		if f.Repeated {
			v = []any{v}
		}
		o.set(f.JSON, v)
	}
	return o
}
