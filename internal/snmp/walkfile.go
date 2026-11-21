package snmp

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ParseWalk reads a walk saved by snmpwalk -On (".1.3.6.1.2.1.1.5.0 = STRING: "sw1"") or by Termwright, one value
// per line; lines it does not understand are skipped. Strings that go on over several lines are joined.
func ParseWalk(text string) []Varbind {
	var out []Varbind
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		oid, rest, ok := strings.Cut(l, " = ")
		if !ok || !strings.HasPrefix(strings.TrimPrefix(oid, "."), "1") {
			continue
		}
		oid = strings.TrimPrefix(oid, ".")
		typ, val, _ := strings.Cut(rest, ": ")
		if !strings.Contains(rest, ": ") {
			typ, val = strings.TrimSuffix(rest, ":"), ""
		}
		// a quoted string that spans lines
		if strings.HasPrefix(val, `"`) && (len(val) == 1 || !strings.HasSuffix(val, `"`)) {
			for i+1 < len(lines) && !strings.Contains(lines[i+1], " = ") {
				i++
				val += "\n" + lines[i]
				if strings.HasSuffix(strings.TrimSpace(lines[i]), `"`) {
					break
				}
			}
		}
		v, ok := walkValue(oid, strings.ToUpper(strings.TrimSpace(typ)), strings.TrimSpace(val))
		if ok {
			out = append(out, v)
		}
	}
	return out
}

func walkValue(oid, typ, val string) (Varbind, bool) {
	v := Varbind{OID: oid}
	num := func(s string) string {
		s = strings.TrimSpace(s)
		if i := strings.IndexAny(s, " ("); i > 0 {
			s = s[:i]
		}
		return s
	}
	switch typ {
	case "STRING", "OCTET STRING":
		s := val
		if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
			s = strings.ReplaceAll(s[1:len(s)-1], `\"`, `"`)
		}
		v.Type, v.Bytes, v.Value = OctetString, []byte(s), s
	case "HEX-STRING":
		b, err := hex.DecodeString(strings.Join(strings.Fields(val), ""))
		if err != nil {
			return v, false
		}
		v.Type, v.Bytes = OctetString, b
	case "INTEGER":
		// "up(1)" or "1"
		s := val
		if i := strings.LastIndex(s, "("); i >= 0 && strings.HasSuffix(s, ")") {
			s = s[i+1 : len(s)-1]
		}
		n, err := strconv.ParseInt(num(s), 10, 64)
		if err != nil {
			return v, false
		}
		v.Type, v.Int = Integer, n
	case "COUNTER32", "GAUGE32", "COUNTER64", "UNSIGNED32", "TIMETICKS":
		s := val
		if typ == "TIMETICKS" && strings.HasPrefix(s, "(") {
			s = s[1:strings.Index(s, ")")]
		}
		n, err := strconv.ParseUint(num(s), 10, 64)
		if err != nil {
			return v, false
		}
		v.Uint = n
		v.Type = map[string]Type{"COUNTER32": Counter32, "GAUGE32": Gauge32, "UNSIGNED32": Gauge32, "COUNTER64": Counter64, "TIMETICKS": TimeTicks}[typ]
	case "IPADDRESS":
		ip := net.ParseIP(val).To4()
		if ip == nil {
			return v, false
		}
		v.Type, v.Bytes, v.Value = IPAddress, ip, val
	case "OID":
		v.Type, v.Value = ObjectID, strings.TrimPrefix(val, ".")
	case "NULL", `""`:
		v.Type = Null
	default:
		return v, false
	}
	return v, true
}

// FormatWalk writes values the way snmpwalk -On does, for ParseWalk and other tools.
func FormatWalk(vs []Varbind) string {
	var b strings.Builder
	for _, v := range vs {
		var t string
		switch v.Type {
		case Integer:
			t = "INTEGER: " + strconv.FormatInt(v.Int, 10)
		case OctetString, "":
			bs := v.Bytes
			if bs == nil {
				bs = []byte(v.Value)
			}
			if printable(bs) {
				t = `STRING: "` + strings.ReplaceAll(string(bs), `"`, `\"`) + `"`
			} else {
				t = "Hex-STRING: " + strings.ToUpper(strings.TrimSpace(fmt.Sprintf("% x", bs)))
			}
		case Counter32, Gauge32, Counter64:
			t = string(v.Type) + ": " + strconv.FormatUint(v.Uint, 10)
		case TimeTicks:
			t = "Timeticks: (" + strconv.FormatUint(v.Uint, 10) + ")"
		case IPAddress:
			ip := net.IP(v.Bytes)
			if len(v.Bytes) != 4 {
				ip = net.ParseIP(v.Value)
			}
			t = "IpAddress: " + ip.String()
		case ObjectID:
			t = "OID: ." + strings.TrimPrefix(v.Value, ".")
		case Null:
			t = `""`
		default:
			continue
		}
		b.WriteString("." + strings.TrimPrefix(v.OID, ".") + " = " + t + "\n")
	}
	return b.String()
}

func printable(b []byte) bool {
	for _, c := range b {
		if (c < 32 && c != '\n' && c != '\r' && c != '\t') || c > 126 {
			return false
		}
	}
	return true
}

// WithText fills in a value's text (Value) from its typed fields.
func WithText(v Varbind) Varbind { return withText(v) }
