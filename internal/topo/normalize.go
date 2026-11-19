package topo

import (
	"regexp"
	"strconv"
	"strings"
)

// NormMAC returns aa:bb:cc:dd:ee:ff for any spelling of a MAC address (aa-bb-…, aabb.ccdd.eeff, AABBCCDDEEFF),
// or "" when s is not one.
func NormMAC(s string) string {
	var h []byte
	for _, c := range []byte(strings.ToLower(strings.TrimSpace(s))) {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
			h = append(h, c)
		case c == ':' || c == '-' || c == '.':
		default:
			return ""
		}
	}
	if len(h) != 12 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < 12; i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.Write(h[i : i+2])
	}
	return b.String()
}

var slotOne = regexp.MustCompile(`^1:(\d+)$`)

// long names first: a long name may end in a shorter one's text
var ifLong = []struct{ long, short string }{
	{"hundredgige", "hu"}, {"fortygigabitethernet", "fo"}, {"twentyfivegige", "twe"}, {"tengigabitethernet", "te"},
	{"gigabitethernet", "gi"}, {"fastethernet", "fa"}, {"port-channel", "po"}, {"ethernet", "et"}, {"eth", "et"},
}

// NormPort makes the spellings of one port compare equal: case, quotes, an EXOS stack slot 1 ("1:5" = "5"),
// and the long and short names of Cisco and Arista ports (GigabitEthernet1/0/1 = Gi1/0/1, Ethernet1 = Et1).
func NormPort(s string) string {
	s = strings.ToLower(strings.Trim(strings.TrimSpace(s), `"`))
	if m := slotOne.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	for _, p := range ifLong {
		if strings.HasPrefix(s, p.long) && len(s) > len(p.long) && s[len(p.long)] >= '0' && s[len(p.long)] <= '9' {
			return p.short + s[len(p.long):]
		}
	}
	return s
}

// NormName compares system names: case and a domain suffix do not matter.
func NormName(s string) string {
	s = strings.ToLower(strings.Trim(strings.TrimSpace(s), `"`))
	if i := strings.IndexByte(s, '.'); i > 0 && !isIP(s) {
		s = s[:i]
	}
	return s
}

func isIP(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c == '.' || c == ':') {
			return false
		}
	}
	return s != ""
}

func isUnsetIP(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || s == "0.0.0.0" || s == "::" || strings.Trim(s, "-–—") == "" || strings.Count(s, ".") != 3 && !strings.Contains(s, ":")
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func itoa(n int) string { return strconv.Itoa(n) }
