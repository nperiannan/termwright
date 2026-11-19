package topo

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// VLANName is a VLAN of the map: its number and the name a device gave it.
type VLANName struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
}

// expandPorts turns "1/1-1/4,1/49" or "1:1-1:3" or "1-4" into the single ports.
func expandPortList(list string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		part = strings.Trim(part, "*!gbaBcdDEeFfhILlMmNnPpRrsStTuUvVwWxX()")
		if part == "" {
			continue
		}
		a, b, ok := strings.Cut(part, "-")
		if !ok {
			out = append(out, part)
			continue
		}
		pre, an := splitLast(a)
		_, bn := splitLast(b)
		x, err1 := strconv.Atoi(an)
		y, err2 := strconv.Atoi(bn)
		if err1 != nil || err2 != nil || y < x || y-x > 512 {
			out = append(out, a, b)
			continue
		}
		for i := x; i <= y; i++ {
			out = append(out, pre+strconv.Itoa(i))
		}
	}
	return out
}

// splitLast: "1/24" -> ("1/", "24"); "1:3" -> ("1:", "3"); "7" -> ("", "7").
func splitLast(s string) (string, string) {
	i := strings.LastIndexAny(s, "/:")
	return s[:i+1], s[i+1:]
}

var (
	vossVLANRow  = regexp.MustCompile(`^(\d+)\s`)
	exosVLANHead = regexp.MustCompile(`(?i)^VLAN Interface with name (\S+)`)
	exosVLANTag  = regexp.MustCompile(`(?i)802\.1Q Tag (\d+)`)
	exosPortList = regexp.MustCompile(`(?i)^\s*(Untag|Tagged)\s*:\s*(.*)$`)
)

// ParseVOSSVLANMembers reads "show vlan members": the ports (first column, PORT MEMBER) of each VLAN.
func ParseVOSSVLANMembers(s string) map[string][]int {
	out := map[string][]int{}
	end := -1 // where the ACTIVE MEMBER column starts
	cur := -1
	for _, ln := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if i := strings.Index(ln, "ACTIVE"); i > 0 && strings.Contains(ln, "PORT") {
			end = i
			continue
		}
		if end < 0 || strings.HasPrefix(strings.TrimSpace(ln), "-") || strings.HasPrefix(strings.TrimSpace(ln), "=") {
			continue
		}
		col := ln
		if len(col) > end {
			col = col[:end]
		}
		if m := vossVLANRow.FindStringSubmatch(col); m != nil {
			cur, _ = strconv.Atoi(m[1])
			col = col[len(m[1]):]
		} else if strings.TrimSpace(col) == "" || ln == "" || !strings.HasPrefix(ln, " ") {
			if !strings.HasPrefix(ln, " ") {
				cur = -1
			}
			continue
		}
		if cur < 0 {
			continue
		}
		for _, p := range expandPortList(col) {
			if strings.ContainsAny(p, "/:") || isNum(p) {
				out[p] = appendUniq(out[p], cur)
			}
		}
	}
	return out
}

// ParseVOSSVLANBasic reads "show vlan basic": VLAN number -> name.
func ParseVOSSVLANBasic(s string) map[int]string {
	out := map[int]string{}
	for _, ln := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		f := strings.Fields(ln)
		if len(f) >= 3 && vossVLANRow.MatchString(ln) {
			if id, err := strconv.Atoi(f[0]); err == nil {
				out[id] = f[1]
			}
		}
	}
	return out
}

// ParseEXOSVLANDetail reads "show vlan detail": each VLAN's tag, name and its untagged and tagged ports.
func ParseEXOSVLANDetail(s string) (map[string][]int, map[int]string) {
	ports, names := map[string][]int{}, map[int]string{}
	name, id, inList := "", -1, false
	var pending []string
	flush := func() {
		if id >= 0 {
			for _, p := range pending {
				ports[p] = appendUniq(ports[p], id)
			}
			if name != "" {
				names[id] = name
			}
		}
		pending = nil
	}
	for _, ln := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if m := exosVLANHead.FindStringSubmatch(strings.TrimSpace(ln)); m != nil {
			flush()
			name, id, inList = m[1], -1, false
			continue
		}
		if m := exosVLANTag.FindStringSubmatch(ln); m != nil && id < 0 {
			id, _ = strconv.Atoi(m[1])
		}
		if m := exosPortList.FindStringSubmatch(ln); m != nil {
			inList = true
			pending = append(pending, portTokens(m[2])...)
			continue
		}
		t := strings.TrimSpace(ln)
		if inList && t != "" && !strings.Contains(t, ":\t") && !strings.HasPrefix(strings.ToLower(t), "flags") && (strings.HasPrefix(ln, " ") || strings.HasPrefix(ln, "\t")) && !strings.Contains(t, ": ") {
			pending = append(pending, portTokens(t)...)
			continue
		}
		inList = false
	}
	flush()
	return ports, names
}

func portTokens(s string) []string {
	var out []string
	for _, p := range expandPortList(s) {
		if strings.ContainsAny(p, "/:") || isNum(p) {
			out = append(out, p)
		}
	}
	return out
}

func isNum(s string) bool { _, err := strconv.Atoi(s); return err == nil && s != "" }

func appendUniq(a []int, v int) []int {
	for _, x := range a {
		if x == v {
			return a
		}
	}
	a = append(a, v)
	sort.Ints(a)
	return a
}

// vlansOf: the VLANs a cable carries - those both ends have (or the known end's when one end is not read).
func vlansOf(a, b *Port) []int {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil || len(a.VLANs) == 0:
		if b == nil {
			return nil
		}
		return b.VLANs
	case b == nil || len(b.VLANs) == 0:
		return a.VLANs
	}
	var out []int
	for _, x := range a.VLANs {
		for _, y := range b.VLANs {
			if x == y {
				out = append(out, x)
			}
		}
	}
	return out
}
