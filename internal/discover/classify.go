package discover

import (
	"regexp"
	"strings"
)

// Kinds of devices (each has its own icon in the app).
const (
	KindComputer = "computer"
	KindServer   = "server"
	KindNAS      = "nas"
	KindPhone    = "phone"
	KindMobile   = "mobile"
	KindPrinter  = "printer"
	KindSwitch   = "switch"
	KindRouter   = "router"
	KindFirewall = "firewall"
	KindWireless = "wireless"
	KindIoT      = "iot" // microcontroller boards and smart devices: ESP32, Arduino, STM32, RTOS firmware
	KindUnknown  = "unknown"
)

// Vendors by SNMP enterprise number (1.3.6.1.4.1.<n>).
var enterprises = map[string]string{
	"9": "Cisco", "2636": "Juniper", "11": "HP", "30065": "Arista", "1916": "Extreme Networks", "2272": "Extreme Networks",
	"5624": "Extreme Networks", "2011": "Huawei", "25506": "H3C", "6027": "Dell", "674": "Dell", "12356": "Fortinet",
	"25461": "Palo Alto Networks", "2620": "Check Point", "8741": "SonicWall", "14988": "MikroTik", "41112": "Ubiquiti",
	"4526": "Netgear", "171": "D-Link", "14823": "Aruba", "8072": "Net-SNMP", "2021": "Net-SNMP", "311": "Microsoft",
	"6876": "VMware", "2435": "Brother", "1602": "Canon", "367": "Ricoh", "641": "Lexmark", "1248": "Epson", "253": "Xerox",
	"6889": "Avaya", "6574": "Synology", "24681": "QNAP",
}

// rule: a pattern found in some evidence decides the kind (and maybe the vendor).
type rule struct {
	re     *regexp.Regexp
	kind   string
	vendor string
}

func r(p, kind, vendor string) rule { return rule{regexp.MustCompile(`(?i)` + p), kind, vendor} }

// Rules from specific to general; the first match in the strongest evidence wins.
var rules = []rule{
	// Extreme Networks
	r(`extremexos|\bexos\b|switch ?engine|summit ?x?\d|telnet session telnet\d+ on /dev/pty`, KindSwitch, "Extreme Networks"),
	r(`\bvoss\b|vsp-?\d{3,4}|fabric ?engine|virtual services platform|ethernet routing switch|\bers[- ]?\d{4}`, KindSwitch, "Extreme Networks"),
	r(`extremecloud|extremewireless|\bap\d{3,4}[a-z]*\b.*extreme|extreme.*access point`, KindWireless, "Extreme Networks"),
	r(`extreme networks|enterasys`, KindSwitch, "Extreme Networks"),
	// mobile phones and tablets (names they give themselves or get from DHCP)
	r(`iphone|ipad|ipod`, KindMobile, "Apple"), r(`galaxy|\bsm-[a-z]\d{3}|samsung[- ]?(phone|tab)`, KindMobile, "Samsung"),
	r(`\bpixel[- ]?\d|google[- ]pixel`, KindMobile, "Google"), r(`oneplus`, KindMobile, "OnePlus"), r(`xiaomi|redmi|\bpoco\b`, KindMobile, "Xiaomi"),
	r(`\boppo\b|realme|\bvivo\b`, KindMobile, ""), r(`motorola|\bmoto[- ]?[a-z]\d`, KindMobile, "Motorola"), r(`android|\bphone\b|\btablet\b`, KindMobile, ""),
	// IoT boards, their firmware and RTOS network stacks
	r(`esphome|tasmota|\bwled\b|esp32|esp8266|esp-idf|espressif|nodemcu|wemos|\besp[_-][0-9a-f]{6}\b|esp-link|ai-thinker`, KindIoT, "Espressif"),
	r(`shelly`, KindIoT, "Shelly"), r(`sonoff|itead`, KindIoT, "Sonoff"), r(`\btuya\b`, KindIoT, "Tuya"),
	r(`arduino`, KindIoT, "Arduino"), r(`stm32|stmicroelectronics`, KindIoT, "STMicroelectronics"),
	r(`raspberry ?pi ?pico|\brp2040\b|\brp2350\b`, KindIoT, "Raspberry Pi"), r(`\bnrf(52|53|91)\d*|nordic semi`, KindIoT, "Nordic Semiconductor"),
	r(`particle (photon|argon|boron)|\bseeed|xiao`, KindIoT, ""), r(`wiznet|\bw5[1-5]00\b`, KindIoT, ""),
	r(`micropython|circuitpython|zephyr|freertos|threadx|azure rtos|\bnuttx\b|riot ?os|contiki|mbed ?os|\blwip\b|\buip\b`, KindIoT, ""),
	r(`homekit accessory|chromecast|smart ?plug|smart ?bulb|\biot\b`, KindIoT, ""),
	r(`macbook|\bimac\b|mac-?mini|mac-?studio|mac-?pro`, KindComputer, "Apple"),
	// IP desk phones
	r(`yealink`, KindPhone, "Yealink"), r(`polycom|\bvvx\b|soundpoint|\bpoly (ccx|edge|vvx)`, KindPhone, "Poly"),
	r(`grandstream`, KindPhone, "Grandstream"), r(`\bsnom\b`, KindPhone, "Snom"), r(`mitel`, KindPhone, "Mitel"),
	r(`fanvil`, KindPhone, "Fanvil"), r(`gigaset`, KindPhone, "Gigaset"), r(`cisco ip phone|\bcp-\d{4}`, KindPhone, "Cisco"),
	r(`avaya.*(phone|j1\d\d|96\d\d)`, KindPhone, "Avaya"), r(`ip phone|deskphone|sip phone|\bata\b`, KindPhone, ""),
	// printers
	r(`laserjet|officejet|deskjet|jetdirect|hp ethernet multi-environment`, KindPrinter, "HP"),
	r(`brother (hl|mfc|dcp)|brother industries`, KindPrinter, "Brother"), r(`canon (mf|lbp|ir|imagerunner)|imagerunner`, KindPrinter, "Canon"),
	r(`epson`, KindPrinter, "Epson"), r(`ricoh`, KindPrinter, "Ricoh"), r(`xerox`, KindPrinter, "Xerox"),
	r(`kyocera`, KindPrinter, "Kyocera"), r(`lexmark`, KindPrinter, "Lexmark"), r(`konica|bizhub`, KindPrinter, "Konica Minolta"),
	r(`printer|\bmfp\b|\bipp\b`, KindPrinter, ""),
	// storage
	r(`terramaster|\btos\b.*nas`, KindNAS, "TerraMaster"), r(`synology|diskstation`, KindNAS, "Synology"), r(`qnap|\bqts\b`, KindNAS, "QNAP"),
	r(`truenas|freenas`, KindNAS, "TrueNAS"), r(`readynas`, KindNAS, "Netgear"), r(`asustor`, KindNAS, "ASUSTOR"),
	r(`my ?cloud`, KindNAS, "Western Digital"), r(`\bnas\b|network attached storage`, KindNAS, ""),
	// security and wireless
	r(`fortigate|fortios|fortinet`, KindFirewall, "Fortinet"), r(`palo alto|pan-os`, KindFirewall, "Palo Alto Networks"),
	r(`adaptive security appliance|firepower|\basa\b`, KindFirewall, "Cisco"), r(`check ?point|gaia`, KindFirewall, "Check Point"),
	r(`sonicwall`, KindFirewall, "SonicWall"), r(`pfsense|opnsense`, KindFirewall, ""), r(`firewall`, KindFirewall, ""),
	r(`aironet|meraki mr|cisco.*access point`, KindWireless, "Cisco"), r(`unifi ap|\buap\b`, KindWireless, "Ubiquiti"),
	r(`ruckus`, KindWireless, "Ruckus"), r(`access point|wireless lan|\bwlan\b`, KindWireless, ""),
	// routers and switches of other vendors
	r(`ios[- ]xr|\bisr\d*\b|\basr\d*\b`, KindRouter, "Cisco"), r(`routeros|mikrotik`, KindRouter, "MikroTik"),
	r(`vyos|edgeos|edgerouter`, KindRouter, ""), r(`fritz!?box`, KindRouter, "AVM"), r(`openwrt`, KindRouter, ""), r(`\brouter\b`, KindRouter, ""),
	r(`nx-os|catalyst|cisco ios|cisco`, KindSwitch, "Cisco"), r(`junos|juniper`, KindSwitch, "Juniper"), r(`arista|\beos\b`, KindSwitch, "Arista"),
	r(`procurve|aruba|hpe? ?(comware|officeconnect)`, KindSwitch, "HPE Aruba"), r(`huawei|\bvrp\b`, KindSwitch, "Huawei"),
	r(`dell networking|\bos10\b|force10`, KindSwitch, "Dell"), r(`cumulus|sonic`, KindSwitch, ""), r(`\bswitch\b`, KindSwitch, ""),
	// servers and computers
	r(`idrac|ilo\b|ipmi|supermicro`, KindServer, ""), r(`vmware esxi|vsphere|proxmox`, KindServer, ""),
	r(`windows|microsoft|openssh_for_windows`, KindComputer, "Microsoft"),
	r(`ubuntu`, KindServer, "Ubuntu"), r(`debian`, KindServer, "Debian"), r(`red ?hat|rhel`, KindServer, "Red Hat"),
	r(`centos|rocky|alma|fedora|suse|amazon linux|linux|freebsd|openbsd`, KindServer, ""),
}

// Network operating systems, matched against every piece of evidence; the
// result picks the device type ("EXOS" before "Linux" when an EXOS switch
// also runs OpenSSH).
var platforms = []struct {
	re   *regexp.Regexp
	name string
}{
	{regexp.MustCompile(`(?i)extremexos|\bexos\b|switch ?engine|summit ?x?\d|telnet session telnet\d+ on /dev/pty`), "EXOS"},
	{regexp.MustCompile(`(?i)\bvoss\b|vsp-?\d{3,4}|fabric ?engine|virtual services platform`), "VOSS"},
	{regexp.MustCompile(`(?i)\bboss\b|ethernet routing switch|\bers[- ]?\d{4}`), "BOSS"},
	{regexp.MustCompile(`(?i)nx-os`), "NX-OS"},
	{regexp.MustCompile(`(?i)ios[- ]xr`), "IOS XR"},
	{regexp.MustCompile(`(?i)ios[- ]xe`), "IOS XE"},
	{regexp.MustCompile(`(?i)cisco ios`), "IOS"},
	{regexp.MustCompile(`(?i)junos`), "Junos"},
	{regexp.MustCompile(`(?i)arista|\beos\b`), "EOS"},
	{regexp.MustCompile(`(?i)fortios|fortigate`), "FortiOS"},
	{regexp.MustCompile(`(?i)pan-os`), "PAN-OS"},
	{regexp.MustCompile(`(?i)routeros|mikrotik`), "RouterOS"},
	{regexp.MustCompile(`(?i)comware`), "Comware"},
	{regexp.MustCompile(`(?i)aos-cx|arubaos`), "ArubaOS"},
	{regexp.MustCompile(`(?i)esphome`), "ESPHome"},
	{regexp.MustCompile(`(?i)tasmota`), "Tasmota"},
	{regexp.MustCompile(`(?i)\bwled\b`), "WLED"},
	{regexp.MustCompile(`(?i)esp32`), "ESP32"},
	{regexp.MustCompile(`(?i)esp8266|nodemcu|wemos`), "ESP8266"},
	{regexp.MustCompile(`(?i)esp-idf`), "ESP-IDF"},
	{regexp.MustCompile(`(?i)micropython`), "MicroPython"},
	{regexp.MustCompile(`(?i)circuitpython`), "CircuitPython"},
	{regexp.MustCompile(`(?i)arduino`), "Arduino"},
	{regexp.MustCompile(`(?i)stm32`), "STM32"},
	{regexp.MustCompile(`(?i)\brp2040\b|\brp2350\b|raspberry ?pi ?pico`), "Raspberry Pi Pico"},
	{regexp.MustCompile(`(?i)zephyr`), "Zephyr RTOS"},
	{regexp.MustCompile(`(?i)freertos`), "FreeRTOS"},
	{regexp.MustCompile(`(?i)threadx|azure rtos`), "ThreadX"},
	{regexp.MustCompile(`(?i)\bnuttx\b`), "NuttX"},
	{regexp.MustCompile(`(?i)mbed ?os`), "Mbed OS"},
	{regexp.MustCompile(`(?i)\blwip\b`), "lwIP (embedded / RTOS)"},
}

// MAC vendors of phone makers, used only for devices without open ports.
var macMobile = regexp.MustCompile(`(?i)samsung|xiaomi|oneplus|oppo|vivo mobile|huawei device|honor device|motorola mobility|google|realme|nothing technology`)

// MAC vendors that tell the kind on their own.
var macKinds = []rule{
	r(`yealink|polycom|grandstream|snom|mitel|fanvil|gigaset`, KindPhone, ""),
	r(`brother|canon|seiko epson|lexmark|kyocera|ricoh|xerox|konica`, KindPrinter, ""),
	r(`synology|qnap|terramaster|asustor`, KindNAS, ""),
	r(`extreme networks|enterasys|avaya`, KindSwitch, "Extreme Networks"),
	r(`cisco|juniper|arista|aruba|ubiquiti|mikrotik|ruckus|fortinet|palo alto`, KindSwitch, ""),
	r(`espressif`, KindIoT, "Espressif"), r(`arduino`, KindIoT, "Arduino"), r(`stmicroelectronics`, KindIoT, "STMicroelectronics"),
	r(`nordic semiconductor`, KindIoT, "Nordic Semiconductor"), r(`raspberry pi`, KindIoT, "Raspberry Pi"),
	r(`shelly|allterco`, KindIoT, "Shelly"), r(`tuya`, KindIoT, "Tuya"), r(`itead`, KindIoT, "Sonoff"),
	r(`particle industries|seeed|wiznet|silicon lab|microchip technology|texas instruments|u-blox|nuvoton`, KindIoT, ""),
	r(`apple`, KindComputer, "Apple"),
}

// mDNS service types and the words the rules know them by.
var mdnsWords = map[string]string{
	"esphomelib": "esphome", "arduino": "arduino board", "hap": "homekit accessory", "googlecast": "chromecast",
	"shelly": "shelly", "wled": "wled", "apple-mobdev2": "iphone", "adb-tls-connect": "android", "adb-tls-pairing": "android",
	"ipp": "printer", "ipps": "printer", "printer": "printer", "pdl-datastream": "printer", "scanner": "printer",
	"companion-link": "apple device", "device-info": "", "http": "", "https": "", "ssh": "", "sftp-ssh": "", "smb": "",
	"workstation": "", "raop": "", "airplay": "", "spotify-connect": "", "matter": "matter iot", "matterc": "matter iot",
	"coap": "iot", "mqtt": "iot",
}

// classify sets the kind, vendor and OS of a result and lists the evidence.
func classify(res *Result) {
	res.Kind, res.Vendor, res.OS, res.Platform, res.Evidence = KindUnknown, "", "", "", nil
	has := map[string]bool{}
	for _, s := range res.Services {
		has[s] = true
	}
	ev := func(s string) { res.Evidence = append(res.Evidence, s) }
	if res.Self {
		res.Kind, res.OS = KindComputer, hostOS()
		ev("this computer")
	}
	// Evidence from the strongest to the weakest source.
	type src struct{ label, text string }
	var srcs []src
	add := func(label, text string) {
		if text = strings.TrimSpace(text); text != "" {
			srcs = append(srcs, src{label, text})
			ev(label + ": " + text)
		}
	}
	add("SNMP", res.SysDescr)
	add("SIP", res.SIP)
	web := strings.TrimSpace(strings.Join([]string{res.Web.Title, res.Web.Server, res.Web.Realm, res.Web.Cert, strings.Join(res.Web.Hints, " ")}, " "))
	if res.Web.Title != "" {
		ev("Web page: " + res.Web.Title)
	}
	if res.Web.Server != "" {
		ev("Web server: " + res.Web.Server)
	}
	if res.Web.Realm != "" {
		ev("Web login: " + res.Web.Realm)
	}
	if res.Web.Cert != "" {
		ev("Certificate: " + res.Web.Cert)
	}
	if len(res.Web.Hints) > 0 {
		ev("Web page mentions: " + strings.Join(res.Web.Hints, ", "))
	}
	if web != "" {
		srcs = append(srcs, src{"Web", web})
	}
	add("Telnet", res.Telnet)
	add("mDNS name", res.MDNS)
	if len(res.MDNSServices) > 0 {
		ev("mDNS services: " + strings.Join(res.MDNSServices, ", "))
		var words []string
		for _, t := range res.MDNSServices {
			if w, ok := mdnsWords[t]; ok && w != "" {
				words = append(words, w)
			} else if !ok {
				words = append(words, t)
			}
		}
		if len(words) > 0 {
			srcs = append(srcs, src{"mDNS services", strings.Join(words, " ")})
		}
	}
	add("CoAP", res.CoAP)
	add("SSH", res.Banner)
	if res.DNS != "" {
		srcs = append(srcs, src{"DNS", res.DNS})
		ev("DNS name: " + res.DNS)
	}
	if res.MAC != "" {
		label := res.MAC
		if res.MACVendor != "" {
			label += " (" + res.MACVendor + ")"
		}
		ev("MAC: " + label)
	}

	// the network OS from the strongest source that names one
	for _, s := range srcs {
		for _, p := range platforms {
			if p.re.MatchString(s.text) {
				res.Platform = p.name
				break
			}
		}
		if res.Platform != "" {
			break
		}
	}
	// vendor from the SNMP enterprise number
	if rest, ok := strings.CutPrefix(res.SysObjectID, "1.3.6.1.4.1."); ok {
		n, _, _ := strings.Cut(rest, ".")
		res.Vendor = enterprises[n]
	}
	decided := res.Self
	// Pass 1: product names (any source); pass 2: generic words such as
	// "Linux" or "switch". A NAS whose SNMP says "Linux" but whose web page
	// says "TerraMaster" is a NAS.
	for pass := 1; pass <= 2 && !decided; pass++ {
		for _, s := range srcs {
			if decided {
				break
			}
			for _, ru := range rules {
				if pass == 1 && (ru.vendor == "" || ru.kind == KindServer) {
					continue
				}
				if ru.re.MatchString(s.text) {
					res.Kind = ru.kind
					if res.Vendor == "" {
						res.Vendor = ru.vendor
					}
					decided = true
					break
				}
			}
		}
	}
	if !decided && res.SIP != "" {
		res.Kind, decided = KindPhone, true // anything that answers SIP is a phone or a phone adapter
	}
	quiet := len(res.Services) == 0 || (len(res.Services) == 1 && res.Services[0] == "mdns")
	if !decided && quiet && macMobile.MatchString(res.MACVendor) {
		res.Kind, decided = KindMobile, true
		res.Evidence = append(res.Evidence, "phone maker's MAC address and no open ports")
	}
	if !decided && quiet && strings.HasPrefix(res.MACVendor, "private") {
		// Android and iOS use a random MAC address per Wi-Fi network by default
		res.Kind, decided = KindMobile, true
		res.Evidence = append(res.Evidence, "private (random) MAC address and no open ports: phones and tablets use these on Wi-Fi")
	}
	if !decided && res.MACVendor != "" {
		for _, ru := range macKinds {
			if ru.re.MatchString(res.MACVendor) {
				res.Kind, decided = ru.kind, true
				if res.Vendor == "" {
					res.Vendor = ru.vendor
				}
				break
			}
		}
	}
	if !decided {
		res.Kind = byPorts(res, has)
	}
	if res.Kind == KindIoT && (res.Vendor == "" || res.Vendor == "Arduino") && res.MACVendor != "" && !strings.HasPrefix(res.MACVendor, "private") {
		res.Vendor = shortVendor(res.MACVendor) // the chip maker (Espressif for an ESP32 running Arduino)
	}
	if res.Vendor == "" && res.MACVendor != "" && !strings.HasPrefix(res.MACVendor, "private") {
		res.Vendor = shortVendor(res.MACVendor)
	}
	if res.OS == "" {
		switch {
		case res.SysDescr != "":
			res.OS = firstSentence(res.SysDescr)
		case res.Platform != "":
			res.OS = res.Platform
		case res.SIP != "" && res.SIP != "SIP":
			res.OS = res.SIP
		case res.Web.Title != "":
			res.OS = res.Web.Title
		case res.Telnet != "" && !promptOnly(res.Telnet):
			res.OS = res.Telnet
		case res.Banner != "":
			res.OS = strings.TrimPrefix(strings.TrimPrefix(res.Banner, "SSH-2.0-"), "SSH-1.99-")
		case res.Kind == KindIoT && strings.Contains(strings.ToLower(res.MACVendor), "espressif"):
			res.OS = "ESP32 / ESP8266"
		}
	}
}

var reDistro = regexp.MustCompile(`(?i)ubuntu|debian|raspbian|el\d|fedora|freebsd|alpine|suse|amzn|redhat`)

// byPorts guesses the kind from open ports and the SSH server when nothing named the device.
func byPorts(res *Result, has map[string]bool) string {
	ssh := strings.ToLower(res.Banner)
	switch {
	case strings.Contains(ssh, "mocana") || strings.Contains(ssh, "rosssh") || strings.Contains(ssh, "comware") ||
		strings.Contains(ssh, "cisco") || strings.Contains(ssh, "huawei") || strings.Contains(ssh, "dropbear") && has["telnet"]:
		res.Evidence = append(res.Evidence, "embedded SSH server of network devices")
		return KindSwitch
	case has["esphome"]:
		res.Evidence = append(res.Evidence, "ESPHome API port (6053) open")
		return KindIoT
	case has["coap"] || (has["modbus"] && !has["ssh"] && !has["smb"]) || (has["mqtt"] && !has["ssh"] && !has["smb"] && !has["rdp"]):
		res.Evidence = append(res.Evidence, "CoAP, Modbus or MQTT without a computer's services: embedded / IoT device")
		return KindIoT
	case has["rdp"] || (has["smb"] && !has["ssh"] && !has["telnet"]):
		return KindComputer
	case has["printer"] || has["ipp"]:
		return KindPrinter
	case has["sip"]:
		return KindPhone
	case has["telnet"] && !reDistro.MatchString(ssh):
		// Telnet is rare on servers today but standard on switches and routers
		res.Evidence = append(res.Evidence, "Telnet open: typical of network devices")
		return KindSwitch
	case has["smb"] && has["ssh"]:
		res.Evidence = append(res.Evidence, "SSH with file sharing: Linux / NAS")
		return KindServer
	case strings.Contains(ssh, "openssh"):
		return KindServer
	}
	return KindUnknown
}

// promptOnly reports whether a Telnet text is just a login prompt.
func promptOnly(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return len(s) < 16 && promptEnd(s)
}

// shortVendor trims company suffixes from an IEEE organisation name.
func shortVendor(s string) string {
	for _, suf := range []string{", Inc.", ", Inc", " Inc.", " Inc", " Co., Ltd.", " Co.,Ltd.", " Co., Ltd", " Corporation", " Corp.", " LLC", " Ltd.", " Ltd", " GmbH", " AG", " B.V."} {
		s = strings.TrimSuffix(s, suf)
	}
	return strings.TrimSpace(s)
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
