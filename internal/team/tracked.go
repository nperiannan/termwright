package team

import (
	"regexp"
	"strings"
)

var (
	networkKinds = regexp.MustCompile(`firewall|fortigate|fortios|palo|pan-?os|asa\b|pfsense|opnsense|srx|checkpoint|sonicwall|wireless|wi-?fi|\bap\b|access point|wing|aruba ?os|unifi|router|ios-?xr|junos|mikrotik|routeros|vyos|edgeos|isr|asr|voss|exos|boss|switch|nx-?os|\bios\b|eos|cumulus|sonic|fabric|extreme|cisco|arista|aruba|procurve|comware|dell ?os|icx|fastiron|slx|netiron`)
	mediaKinds   = regexp.MustCompile(`\btv\b|television|media player|chromecast|roku|apple tv|fire ?tv|android tv|camera|\bcam\b|ip ?cam|nvr|cctv|hikvision|dahua|reolink`)
	sharedKinds  = regexp.MustCompile(`linux|ubuntu|debian|centos|rhel|red hat|fedora|alpine|unix|bsd|solaris|aix|server|esxi|windows|macos|mac os|desktop|laptop|nas|synology|qnap|truenas|storage|printer|phone|voip|iot|raspberry|embedded|esp32|arduino`)
)

// Tracked tells whether a device type counts for in use, reservations and
// usage: network devices (switches, routers, firewalls, access points and
// other appliances) do; devices without a type, and computers, servers,
// phones, printers, storage, TVs, cameras and IoT boards - which take several
// logins at once - do not. It follows Termwright's device kinds.
func Tracked(deviceType string) bool {
	t := strings.ToLower(strings.TrimSpace(deviceType))
	switch {
	case t == "" || mediaKinds.MatchString(t):
		return false
	case networkKinds.MatchString(t):
		return true
	}
	return !sharedKinds.MatchString(t)
}
