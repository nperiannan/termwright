package discover

import (
	"regexp"
	"strings"
)

// Kinds of devices (each has its own icon in the app).
const (
	KindComputer = "computer"
	KindServer   = "server"
	KindPhone    = "phone"
	KindPrinter  = "printer"
	KindSwitch   = "switch"
	KindRouter   = "router"
	KindFirewall = "firewall"
	KindWireless = "wireless"
	KindUnknown  = "unknown"
)

// Vendors by SNMP enterprise number (1.3.6.1.4.1.<n>).
var enterprises = map[string]string{
	"9": "Cisco", "2636": "Juniper", "11": "HP", "30065": "Arista", "1916": "Extreme Networks", "2272": "Extreme Networks",
	"5624": "Extreme Networks", "2011": "Huawei", "25506": "H3C", "6027": "Dell", "674": "Dell", "12356": "Fortinet",
	"25461": "Palo Alto Networks", "2620": "Check Point", "8741": "SonicWall", "14988": "MikroTik", "41112": "Ubiquiti",
	"4526": "Netgear", "171": "D-Link", "14823": "Aruba", "8072": "Net-SNMP", "2021": "Net-SNMP", "311": "Microsoft",
	"6876": "VMware", "2435": "Brother", "1602": "Canon", "367": "Ricoh", "641": "Lexmark", "1248": "Epson", "253": "Xerox",
	"6889": "Avaya",
}

var (
	rePrinter  = regexp.MustCompile(`(?i)printer|laserjet|officejet|deskjet|jetdirect|\bmfp\b|lexmark|kyocera|ricoh|xerox|bizhub|imagerunner|\bepson\b|brother (hl|mfc|dcp)`)
	rePhone    = regexp.MustCompile(`(?i)ip phone|\bsip\b|polycom|yealink|grandstream|snom|softphone|\bata\b|deskphone|vvx|cp-\d{4}`)
	reFirewall = regexp.MustCompile(`(?i)fortigate|fortios|palo alto|pan-os|adaptive security appliance|\basa\b|firepower|check ?point|gaia|sonicwall|pfsense|opnsense|firewall`)
	reWireless = regexp.MustCompile(`(?i)access point|aironet|wireless|\bwing\b|unifi ap|instant ap|\bap[0-9]{3}`)
	reRouter   = regexp.MustCompile(`(?i)router|ios[- ]xr|routeros|mikrotik|vyos|edgeos|\bisr\d*\b|\basr\d*\b|junos.*\b(mx|srx)\d`)
	reSwitch   = regexp.MustCompile(`(?i)switch|voss|\bvsp\b|vsp-|fabric ?engine|extremexos|\bexos\b|summit|ethernet routing switch|nx-os|catalyst|cisco ios|arista|\beos\b|junos|procurve|aruba|os10|\bslx\b|cumulus|sonic|fastiron|\bicx\b|comware|vrp|ce\d{4}`)
	reWindows  = regexp.MustCompile(`(?i)windows|microsoft|openssh_for_windows`)
	reUnix     = regexp.MustCompile(`(?i)linux|ubuntu|debian|centos|red ?hat|rhel|rocky|alma|fedora|suse|freebsd|openbsd|darwin|macos|esxi|vmware|net-snmp|amazon`)
)

// classify fills in the kind, vendor and OS of a result.
func classify(r *Result) {
	if r.SysObjectID != "" {
		if rest, ok := strings.CutPrefix(r.SysObjectID, "1.3.6.1.4.1."); ok {
			n, _, _ := strings.Cut(rest, ".")
			r.Vendor = enterprises[n]
		}
	}
	text := r.SysDescr + " " + r.SysName + " " + r.Banner
	if r.SysDescr != "" {
		r.OS = firstSentence(r.SysDescr)
	} else if r.Banner != "" {
		r.OS = strings.TrimPrefix(strings.TrimPrefix(r.Banner, "SSH-2.0-"), "SSH-1.99-")
	}
	has := func(s string) bool {
		for _, x := range r.Services {
			if x == s {
				return true
			}
		}
		return false
	}
	switch {
	case rePrinter.MatchString(text) || ((has("printer") || has("ipp")) && !has("ssh") && !has("rdp")):
		r.Kind = KindPrinter
	case rePhone.MatchString(r.SysDescr) || (has("sip") && !has("ssh") && !has("rdp") && !has("smb")):
		r.Kind = KindPhone
	case reFirewall.MatchString(text):
		r.Kind = KindFirewall
	case reWireless.MatchString(r.SysDescr):
		r.Kind = KindWireless
	case reRouter.MatchString(r.SysDescr):
		r.Kind = KindRouter
	case reSwitch.MatchString(r.SysDescr) || isNetworkVendor(r.Vendor):
		r.Kind = KindSwitch
	case reWindows.MatchString(text) || has("rdp") || has("smb"):
		r.Kind = KindComputer
	case reUnix.MatchString(text) || strings.Contains(r.Banner, "OpenSSH"):
		r.Kind = KindServer
	case has("telnet") && !has("http") && !has("https"):
		r.Kind = KindSwitch // a Telnet-only box is nearly always network gear
	case reSwitch.MatchString(r.Banner):
		r.Kind = KindSwitch
	default:
		r.Kind = KindUnknown
	}
	if r.Vendor == "" {
		r.Vendor = vendorFromText(text)
	}
}

func isNetworkVendor(v string) bool {
	switch v {
	case "Cisco", "Juniper", "Arista", "Extreme Networks", "Huawei", "H3C", "Aruba", "MikroTik", "Ubiquiti", "Netgear", "D-Link":
		return true
	}
	return false
}

var vendorWords = []struct {
	re   *regexp.Regexp
	name string
}{
	{regexp.MustCompile(`(?i)extreme|voss|exos|vsp|fabric ?engine|switch ?engine`), "Extreme Networks"},
	{regexp.MustCompile(`(?i)cisco`), "Cisco"}, {regexp.MustCompile(`(?i)juniper|junos`), "Juniper"},
	{regexp.MustCompile(`(?i)arista`), "Arista"}, {regexp.MustCompile(`(?i)huawei|vrp`), "Huawei"},
	{regexp.MustCompile(`(?i)ubuntu`), "Ubuntu"}, {regexp.MustCompile(`(?i)debian`), "Debian"},
	{regexp.MustCompile(`(?i)red ?hat|rhel`), "Red Hat"}, {regexp.MustCompile(`(?i)windows|microsoft`), "Microsoft"},
	{regexp.MustCompile(`(?i)\bhp\b|hewlett|laserjet`), "HP"}, {regexp.MustCompile(`(?i)polycom`), "Polycom"},
	{regexp.MustCompile(`(?i)yealink`), "Yealink"}, {regexp.MustCompile(`(?i)fortigate|fortinet`), "Fortinet"},
}

func vendorFromText(s string) string {
	for _, v := range vendorWords {
		if v.re.MatchString(s) {
			return v.name
		}
	}
	return ""
}

// firstSentence shortens a system description to its first line or sentence.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{"\r", "\n", " Copyright", " (c)", " (C)"} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	if len(s) > 100 {
		s = s[:100] + "…"
	}
	return strings.TrimSpace(s)
}
