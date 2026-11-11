package emu

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// SwitchLag is a LAG as a switch lists it: its ports by the switch's names.
type SwitchLag struct {
	Name  string
	Mode  string // "lacp" or "static"
	Ports []string
}

var portLike = regexp.MustCompile(`^\d+(?:[:/]\d+)*$`)

// ParseLags reads the LAGs a switch printed: EXOS "show sharing" or VOSS
// "show mlt" (chosen by what the output looks like).
func ParseLags(out string) []SwitchLag {
	if strings.Contains(out, "MLTID") || strings.Contains(strings.ToUpper(out), "MLT INFO") {
		return parseMLT(out)
	}
	return parseSharing(out)
}

// parseSharing reads EXOS "show sharing": a group starts on the line with
// its master port and control (LACP, Static, Health); every line has one
// member port, the column before its "Agg Mbr" Y/N.
func parseSharing(out string) []SwitchLag {
	var lags []SwitchLag
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) < 3 || strings.HasPrefix(f[0], "=") {
			continue
		}
		ctl := ""
		for _, t := range f {
			switch strings.ToLower(t) {
			case "lacp":
				ctl = "lacp"
			case "static", "health":
				ctl = "static"
			}
		}
		if ctl != "" && portLike.MatchString(f[0]) {
			lags = append(lags, SwitchLag{Name: f[0], Mode: ctl})
		}
		if len(lags) == 0 {
			continue
		}
		for i := 0; i+1 < len(f); i++ {
			if portLike.MatchString(f[i]) && (f[i+1] == "Y" || f[i+1] == "N" || f[i+1] == "-") {
				g := &lags[len(lags)-1]
				g.Ports = append(g.Ports, f[i])
				break
			}
		}
	}
	return lags
}

// parseMLT reads VOSS "show mlt": an MLT per row (its ID first), members as
// ranges ("1/1-1/2,1/5").
func parseMLT(out string) []SwitchLag {
	var lags []SwitchLag
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		if _, err := strconv.Atoi(f[0]); err != nil {
			continue
		}
		var ports []string
		for _, t := range f[1:] {
			if strings.Contains(t, "/") {
				ports = expandPorts(t)
				break
			}
		}
		if len(ports) == 0 {
			continue
		}
		mode := "static"
		if strings.Contains(strings.ToLower(line), "lacp") {
			mode = "lacp"
		}
		lags = append(lags, SwitchLag{Name: "MLT " + f[0], Mode: mode, Ports: ports})
	}
	return lags
}

// expandPorts turns "1/1-1/3,1/5" into 1/1 1/2 1/3 1/5.
func expandPorts(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		a, b, ok := strings.Cut(part, "-")
		if !ok {
			out = append(out, part)
			continue
		}
		sa, na, _ := strings.Cut(a, "/")
		_, nb, _ := strings.Cut(b, "/")
		x, err1 := strconv.Atoi(na)
		y, err2 := strconv.Atoi(nb)
		if err1 != nil || err2 != nil || y < x || y-x > 512 {
			out = append(out, a, b)
			continue
		}
		for n := x; n <= y; n++ {
			out = append(out, sa+"/"+strconv.Itoa(n))
		}
	}
	return out
}

// DataPortOf finds the data port (1…) a switch's port name stands for: the
// image's port names, also without a stack slot ("1:3" is 3 on a switch that is
// not a stack).
func (im *Image) DataPortOf(name string) int {
	for _, n := range []string{name, name[strings.LastIndex(name, ":")+1:]} {
		for p := 1; p <= im.DataPorts(); p++ {
			if im.PortNameOf(p) == n {
				return p
			}
		}
	}
	return 0
}

// LagCommands are the CLI commands that make LAG g on a switch of this image
// (or remove it): the image's LagSet for its mode, or LagDel.
func (im *Image) LagCommands(g Lag, remove bool) ([]string, error) {
	tpl := im.LagSet[g.Mode]
	if remove {
		tpl = im.LagDel
	}
	if len(tpl) == 0 {
		what := "make a " + g.Mode + " LAG"
		if remove {
			what = "remove a LAG"
		}
		return nil, fmt.Errorf("the image %s has no commands to %s (its device profile)", im.Name, what)
	}
	if len(g.Ports) == 0 {
		return nil, errors.New("the LAG has no ports")
	}
	var names []string
	for _, p := range g.Ports {
		names = append(names, im.PortNameOf(p))
	}
	id := strings.TrimLeftFunc(g.Name, func(r rune) bool { return r < '0' || r > '9' })
	if i := strings.IndexFunc(id, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		id = id[:i]
	}
	if id == "" {
		id = "1"
	}
	r := strings.NewReplacer("{id}", id, "{name}", g.Name, "{ports}", strings.Join(names, ","), "{master}", names[0], "{first}", names[0], "{last}", names[len(names)-1], "{mode}", g.Mode)
	out := make([]string, len(tpl))
	for i, t := range tpl {
		out[i] = r.Replace(t)
	}
	return out, nil
}
