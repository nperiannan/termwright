// Package facts reads a device's model, OS version and serial number from
// command output (show sys-info, show version, /etc/os-release …).
package facts

import (
	"regexp"
	"strings"
)

// Facts of one device; empty when not found.
type Facts struct {
	Model   string `json:"model"`
	Version string `json:"version"`
	Serial  string `json:"serial"`
	MAC     string `json:"mac"` // base (chassis) MAC address
}

// Complete reports whether the version and the serial number are known.
func (f Facts) Complete() bool { return f.Version != "" && f.Serial != "" }

type family struct {
	name     string
	commands []string
	model    []*regexp.Regexp
	version  []*regexp.Regexp
	serial   []*regexp.Regexp
	mac      []*regexp.Regexp
}

func res(p ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(p))
	for i, s := range p {
		out[i] = regexp.MustCompile(`(?m)` + s)
	}
	return out
}

var families = []family{
	{name: "voss", commands: []string{"show sys-info"},
		model:   res(`ModelName\s*:\s*(\S+)`),
		version: res(`SysDescr\s*:\s*\S+\s*\(([^)]+)\)`, `Software Release\s*:\s*(\S+)`),
		serial:  res(`Serial#\s*:\s*(\S+)`),
		mac:     res(`BaseMacAddr\s*:\s*([0-9a-fA-F:.-]{12,17})`, `MacAddr\s*:\s*([0-9a-fA-F:.-]{12,17})`)},
	{name: "exos", commands: []string{"show switch", "show version"},
		model:   res(`System Type:\s*(.+?)\s*$`),
		version: res(`IMG:\s*(\S+)`, `Image\s*:\s*Extreme\S*\s+version\s+(\S+)`, `Primary ver:\s*(\S+)`),
		serial:  res(`^Switch\s*:\s*\S+\s+(\S+)`, `^Slot-1\s*:\s*\S+\s+(\S+)`),
		mac:     res(`System MAC:\s*([0-9a-fA-F:.-]{12,17})`)},
	{name: "cisco", commands: []string{"show version"},
		model:   res(`Model [Nn]umber\s*:\s*(\S+)`, `^cisco (\S+) \(`),
		version: res(`Cisco IOS.*Version ([^,\s]+)`, `NXOS: version (\S+)`, `^\s*Version ([^,\s]+)`),
		serial:  res(`System [Ss]erial [Nn]umber\s*:\s*(\S+)`, `Processor board ID (\S+)`),
		mac:     res(`Base [Ee]thernet MAC [Aa]ddress\s*:\s*([0-9a-fA-F:.-]{12,17})`)},
	{name: "linux", commands: []string{"cat /etc/os-release", "uname -sr"},
		version: res(`PRETTY_NAME="?([^"\n]+)"?`)},
}

// Family is the command family of a device type name ("" = unknown).
func Family(deviceType string) string {
	d := strings.ToLower(deviceType)
	switch {
	case strings.Contains(d, "voss") || strings.Contains(d, "fabric engine") || strings.Contains(d, "vsp"):
		return "voss"
	case strings.Contains(d, "exos") || strings.Contains(d, "switch engine") || strings.Contains(d, "extremexos"):
		return "exos"
	case strings.Contains(d, "cisco") || strings.Contains(d, "ios") || strings.Contains(d, "nx-os") || strings.Contains(d, "nxos"):
		return "cisco"
	case strings.Contains(d, "linux") || strings.Contains(d, "ubuntu") || strings.Contains(d, "debian") || strings.Contains(d, "centos") || strings.Contains(d, "rhel"):
		return "linux"
	}
	return ""
}

// Commands that show the facts for a family (nil when unknown).
func Commands(fam string) []string {
	for _, f := range families {
		if f.name == fam {
			return f.commands
		}
	}
	return nil
}

// Parse finds the facts in text, using the patterns of fam (or of every
// family when fam is "").
func Parse(fam, text string) Facts {
	var out Facts
	for _, f := range families {
		if fam != "" && f.name != fam {
			continue
		}
		first := func(list []*regexp.Regexp) string {
			for _, re := range list {
				if m := re.FindStringSubmatch(text); m != nil {
					return clip(strings.TrimSpace(m[1]))
				}
			}
			return ""
		}
		if out.Model == "" {
			out.Model = first(f.model)
		}
		if out.Version == "" {
			out.Version = first(f.version)
		}
		if out.Serial == "" {
			out.Serial = first(f.serial)
		}
		if out.MAC == "" {
			out.MAC = first(f.mac)
		}
	}
	return out
}

// Merge fills the empty fields of f from g.
func (f Facts) Merge(g Facts) Facts {
	if f.Model == "" {
		f.Model = g.Model
	}
	if f.Version == "" {
		f.Version = g.Version
	}
	if f.Serial == "" {
		f.Serial = g.Serial
	}
	if f.MAC == "" {
		f.MAC = g.MAC
	}
	return f
}

func clip(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}
