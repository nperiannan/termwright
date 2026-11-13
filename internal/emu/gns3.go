package emu

import (
	"encoding/json"
	"errors"
	"net"
	"strings"
)

// qemuProps are the QEMU settings of an appliance (registry v4: "qemu";
// v8: a "settings" entry's "template_properties").
type qemuProps struct {
	AdapterType string `json:"adapter_type"`
	Adapters    int    `json:"adapters"`
	RAM         int    `json:"ram"`
	CPUs        int    `json:"cpus"`
	DiskIf      string `json:"hda_disk_interface"`
	HdbIf       string `json:"hdb_disk_interface"`
	HdcIf       string `json:"hdc_disk_interface"`
	HddIf       string `json:"hdd_disk_interface"`
	Arch        string `json:"arch"`
	Platform    string `json:"platform"`
	Options     string `json:"options"`
	PortName    string `json:"port_name_format"`
}

// over puts the values set in o over p.
func (p qemuProps) over(o qemuProps) qemuProps {
	if o.AdapterType != "" {
		p.AdapterType = o.AdapterType
	}
	if o.Adapters != 0 {
		p.Adapters = o.Adapters
	}
	if o.RAM != 0 {
		p.RAM = o.RAM
	}
	if o.CPUs != 0 {
		p.CPUs = o.CPUs
	}
	if o.DiskIf != "" {
		p.DiskIf = o.DiskIf
	}
	if o.HdbIf != "" {
		p.HdbIf = o.HdbIf
	}
	if o.HdcIf != "" {
		p.HdcIf = o.HdcIf
	}
	if o.HddIf != "" {
		p.HddIf = o.HddIf
	}
	if o.Arch != "" {
		p.Arch = o.Arch
	}
	if o.Platform != "" {
		p.Platform = o.Platform
	}
	if o.Options != "" {
		p.Options = o.Options
	}
	if o.PortName != "" {
		p.PortName = o.PortName
	}
	return p
}

// appliance is the part of a GNS3 appliance file (.gns3a, public JSON) that says how to boot an image.
type appliance struct {
	Name     string     `json:"name"`
	Category string     `json:"category"`
	Vendor   string     `json:"vendor_name"`
	Usage    string     `json:"usage"`
	User     string     `json:"default_username"`
	Password string     `json:"default_password"`
	Qemu     *qemuProps `json:"qemu"`
	Settings []struct {
		Name    string    `json:"name"`
		Default bool      `json:"default"`
		Inherit bool      `json:"inherit_default_properties"`
		Type    string    `json:"template_type"`
		Props   qemuProps `json:"template_properties"`
	} `json:"settings"`
	PortName string `json:"port_name_format"`
	Images   []struct {
		Filename string `json:"filename"`
		Version  string `json:"version"`
		Size     int64  `json:"filesize"`
		MD5      string `json:"md5sum"`
		URL      string `json:"direct_download_url"`
		Page     string `json:"download_url"`
	} `json:"images"`
	Versions []struct {
		Name     string            `json:"name"`
		Settings string            `json:"settings"`
		Images   map[string]string `json:"images"`
	} `json:"versions"`
}

// Appliance is an image profile from a GNS3 appliance file, per version it lists.
type Appliance struct {
	Name     string  `json:"name"`
	Usage    string  `json:"usage"`
	User     string  `json:"user"`
	Password string  `json:"password"`
	Profiles []Image `json:"profiles"` // FileName says which image file each version needs
}

// ParseAppliance reads a GNS3 appliance file.
func ParseAppliance(data []byte) (Appliance, error) {
	var a appliance
	if err := json.Unmarshal(data, &a); err != nil {
		return Appliance{}, errors.New("not a GNS3 appliance file: " + err.Error())
	}
	// the QEMU settings: v4 "qemu", or v8's default "settings" entry (others per version)
	var def *qemuProps
	named := map[string]qemuProps{}
	if a.Qemu != nil {
		def = a.Qemu
	}
	for _, st := range a.Settings {
		if st.Type != "" && st.Type != "qemu" {
			continue
		}
		if st.Default && def == nil {
			p := st.Props
			def = &p
		}
		named[st.Name] = st.Props
	}
	if def == nil {
		for _, st := range a.Settings { // no default: the first QEMU one
			if st.Type == "" || st.Type == "qemu" {
				p := st.Props
				def = &p
				break
			}
		}
	}
	if def == nil {
		return Appliance{}, errors.New(a.Name + " is not a QEMU appliance (only QEMU images can run on a KVM host)")
	}
	arch := orStr(def.Arch, def.Platform)
	if arch != "" && arch != "x86_64" && arch != "i386" {
		return Appliance{}, errors.New(a.Name + " needs " + arch + "; KVM hosts run x86_64")
	}
	mk := func(q qemuProps) Image {
		im := Image{Name: a.Name, CPUs: q.CPUs, RAM: q.RAM, NICs: q.Adapters, NIC: q.AdapterType, Disk: q.DiskIf,
			PortName: gnsPortName(orStr(q.PortName, a.PortName)), Ready: "login:", Login: a.User, Extra: cleanOptions(q.Options), Source: "GNS3 appliance " + a.Name}
		if im.CPUs == 0 {
			im.CPUs = 1
		}
		if im.NIC == "" {
			im.NIC = "e1000"
		}
		if IsLinuxAppliance(a.Name, a.Category) {
			LinuxHost(&im)
		}
		return im
	}
	base := mk(*def)
	out := Appliance{Name: a.Name, Usage: a.Usage, User: a.User, Password: a.Password}
	for _, v := range a.Versions {
		f := v.Images["hda_disk_image"]
		if f == "" {
			continue
		}
		im := base
		q := *def
		if o, ok := named[v.Settings]; ok && v.Settings != "" {
			q = def.over(o)
			im = mk(q)
		}
		im.Disks = nil
		for _, sl := range []struct{ key, slot, bus string }{{"hdb_disk_image", "hdb", q.HdbIf}, {"hdc_disk_image", "hdc", q.HdcIf}, {"hdd_disk_image", "hdd", q.HddIf}, {"cdrom_image", "cdrom", ""}, {"bios_image", "bios", ""}} {
			if fn := v.Images[sl.key]; fn != "" {
				d := Disk{Slot: sl.slot, FileName: fn, Bus: sl.bus}
				for _, x := range a.Images {
					if x.Filename == fn {
						d.Size, d.MD5, d.URL, d.Page = x.Size, strings.ToLower(x.MD5), x.URL, x.Page
					}
				}
				im.Disks = append(im.Disks, d)
			}
		}
		im.FileName = f
		im.Version = strings.TrimPrefix(strings.TrimPrefix(v.Name, "v"), "V")
		for _, x := range a.Images {
			if x.Filename == f {
				im.Size, im.MD5, im.URL, im.Page = x.Size, strings.ToLower(x.MD5), x.URL, x.Page
				if x.Version != "" && v.Name == "" {
					im.Version = strings.TrimPrefix(strings.TrimPrefix(x.Version, "v"), "V")
				}
			}
		}
		out.Profiles = append(out.Profiles, im)
	}
	if len(out.Profiles) == 0 {
		out.Profiles = append(out.Profiles, base)
	}
	return out, nil
}

// gnsPortName turns GNS3's "Ethernet{0}" / "1/{port1}" into "{n}" style (data port n).
func gnsPortName(f string) string {
	if f == "" {
		return ""
	}
	if strings.Contains(f, "{n") {
		return f // already Termwright's form
	}
	f = strings.NewReplacer("{port1}", "{n}", "{0}", "{n}", "{port0}", "{n}").Replace(f)
	return f
}

// cleanOptions drops the options Termwright sets itself.
func cleanOptions(o string) string {
	a := splitArgs(o)
	var keep []string
	for i := 0; i < len(a); i++ {
		switch a[i] {
		case "-nographic", "-enable-kvm", "-daemonize":
			continue
		case "-display", "-serial", "-monitor":
			i++
			continue
		}
		keep = append(keep, a[i])
	}
	return strings.Join(keep, " ")
}

// NoSerialConsole says whether an image is known to have no serial console (Tiny Core boots to a
// screen only), so Termwright cannot log in to it or type its network setup.
func NoSerialConsole(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "tiny core") || strings.Contains(n, "tinycore")
}

// IsLinuxAppliance says whether a GNS3 appliance is a Linux computer (a guest
// whose console is a Linux shell), which Termwright configures as a host.
func IsLinuxAppliance(name, category string) bool {
	n := strings.ToLower(name)
	if NoSerialConsole(name) {
		return false
	}
	for _, d := range []string{"alpine", "debian", "ubuntu", "fedora", "almalinux", "rocky", "centos", "kali", "linux"} {
		if strings.Contains(n, d) {
			return category == "" || category == "guest"
		}
	}
	return false
}

// portIf is a shell line that finds the interface with a MAC address ({mac}) in $i.
const portIf = `i=$(ip -o link | awk -F': ' 'tolower($0) ~ /{mac}/ {sub(/@.*/, "", $2); print $2; exit}')`

// LinuxHost gives an image the console setup of a Linux computer: its host
// name on day 0 (and iperf3 when its package manager can get it), a fixed
// management address when the lab has them, and the data ports' addresses.
// It keeps what the image already has.
func LinuxHost(im *Image) {
	if im.DeviceType == "" {
		im.DeviceType = "Linux"
	}
	if im.Login == "" {
		im.Login = "root"
	}
	if im.Prompt == "" {
		im.Prompt = `[#$]\s*$`
	}
	if im.NICs < 3 { // a computer needs data ports to be cabled: the management port and two more (GNS3's Linux appliances have one network card)
		im.NICs = 3
	}
	if im.PortName == "" {
		im.PortName = "eth{n}"
	}
	if im.Elevate == "" { // Tiny Core logs in as tc: commands that change the network need root
		im.Elevate = `[ "$(id -u)" = 0 ] || exec sudo -s`
	}
	if len(im.Day0) == 0 {
		im.Day0 = []string{
			"hostname {hostname}; echo {hostname} > /etc/hostname",
			LinuxDNS,
			"(command -v iperf3 || apk add -q iperf3 || (apt-get update -q && apt-get install -y -q iperf3) || dnf install -y -q iperf3 || tce-load -wi iperf3) >/dev/null 2>&1 &",
		}
	}
	if len(im.AddrSet) == 0 {
		im.AddrSet = []string{portIf + `; ip link set "$i" up; ip -4 addr flush dev "$i"; ip addr add {mgmtIp} dev "$i"; [ -z "{gateway}" ] || ip route replace default via {gateway}`}
	}
	if im.AddrCmd == "" {
		im.AddrCmd = "ip -4 -o addr show scope global"
	}
	if len(im.PortAddr) == 0 {
		im.PortAddr = []string{portIf + `; ip link set "$i" up; ip -4 addr flush dev "$i"; ip addr add {addr} dev "$i"`}
	}
	if im.PortShow == "" {
		im.PortShow = `for d in /sys/class/net/*; do n=${d##*/}; m=$(cat "$d/address"); for a in $(ip -o addr show dev "$n" scope global | awk '{print $4}'); do echo "PA $m $a"; done; for a in $(ip -o -6 addr show dev "$n" scope link | awk '{print $4}'); do echo "LL $m $a"; done; done; ip route show default | awk '{print "GW", $3; exit}'; ip -6 route show default | awk '{print "GW6", $3; exit}'; awk '/^nameserver/ {print "DNS", $2; exit}' /etc/resolv.conf`
	}
	if len(im.PortAddr6) == 0 {
		im.PortAddr6 = []string{portIf + `; ip link set "$i" up; ip -6 addr flush dev "$i" scope global; ip -6 addr add {addr} dev "$i"`}
	}
	if len(im.PortAuto6) == 0 {
		im.PortAuto6 = []string{portIf + `; echo 2 > /proc/sys/net/ipv6/conf/"$i"/accept_ra; echo 1 > /proc/sys/net/ipv6/conf/"$i"/autoconf; ip link set "$i" down; ip link set "$i" up`}
	}
	if len(im.RouteSet) == 0 {
		im.RouteSet = []string{`[ -z "{gw}" ] || ip route replace default via {gw}; [ -z "{gw6}" ] || ip -6 route replace default via {gw6}; [ -z "{dns}" ] || echo "nameserver {dns}" > /etc/resolv.conf`}
	}
	if len(im.PortDHCP) == 0 {
		im.PortDHCP = []string{portIf + `; ip link set "$i" up; (udhcpc -i "$i" -b -q || dhclient -nw "$i" || dhcpcd -b "$i") >/dev/null 2>&1`}
	}
}

// FillPort puts a data port's MAC address and address into a command.
func FillPort(cmd, mac, addr string) string {
	return strings.NewReplacer("{mac}", strings.ToLower(mac), "{addr}", addr).Replace(cmd)
}

// FillRoute puts a computer's gateways and DNS server into a command.
func FillRoute(cmd string, v VM) string {
	return strings.NewReplacer("{gw}", v.DataGw, "{gw6}", v.DataGw6, "{dns}", v.DNS).Replace(cmd)
}

// CompNet is what a computer's console showed of its network: the addresses of
// its data ports (port → "a/b, c/d"), their IPv6 link-local addresses, its
// default gateways and DNS server.
type CompNet struct {
	Addrs     map[int]string `json:"addrs,omitempty"`
	LinkLocal map[int]string `json:"linkLocal,omitempty"`
	GW        string         `json:"gw,omitempty"`
	GW6       string         `json:"gw6,omitempty"`
	DNS       string         `json:"dns,omitempty"`
}

// ParsePortShow reads the output of an image's PortShow ("PA|LL <mac> <address/bits>",
// "GW|GW6|DNS <address>" lines), with the MACs of its ports (port → MAC).
func ParsePortShow(out string, macs map[int]string) CompNet {
	port := map[string]int{}
	for p, m := range macs {
		port[strings.ToLower(m)] = p
	}
	r := CompNet{Addrs: map[int]string{}, LinkLocal: map[int]string{}}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && net.ParseIP(f[1]) != nil {
			switch f[0] {
			case "GW":
				r.GW = f[1]
			case "GW6":
				r.GW6 = f[1]
			case "DNS":
				r.DNS = f[1]
			}
			continue
		}
		if len(f) != 3 || f[0] != "PA" && f[0] != "LL" {
			continue
		}
		p, ok := port[strings.ToLower(f[1])]
		if !ok || p == 0 {
			continue
		}
		m := r.Addrs
		if f[0] == "LL" {
			m = r.LinkLocal
		}
		if m[p] != "" {
			m[p] += ", "
		}
		m[p] += f[2]
	}
	return r
}

// LinuxDNS gives a Linux computer name servers when it has none (an Alpine installed with a static
// address, a cloud image without DHCP): without them it reaches the Internet by address only, and its
// day 0 cannot install iperf3. A DNS server set for the computer (lab_computer dns) is typed later.
const LinuxDNS = "grep -q '^nameserver' /etc/resolv.conf 2>/dev/null || printf 'nameserver %s\\n' {dnsList} > /etc/resolv.conf"

// linuxDNSFixed is LinuxDNS as it was before the lab's DNS setting (images keep it in their day 0).
const linuxDNSFixed = "grep -q '^nameserver' /etc/resolv.conf 2>/dev/null || printf 'nameserver 8.8.8.8\\nnameserver 1.1.1.1\\n' > /etc/resolv.conf"

// AddLinuxDNS puts LinuxDNS into an older Linux image's day 0, before its iperf3 install, or turns the
// fixed servers of an older LinuxDNS into the lab's (it reports whether it changed the image).
func AddLinuxDNS(im *Image) bool {
	for i, c := range im.Day0 {
		if c == linuxDNSFixed { // from before the lab's DNS setting: it takes the lab's servers now
			im.Day0[i] = LinuxDNS
			return true
		}
		if strings.Contains(c, "resolv.conf") {
			return false
		}
	}
	for i, c := range im.Day0 {
		if strings.Contains(c, "apk add -q iperf3") {
			im.Day0 = append(im.Day0[:i], append([]string{LinuxDNS}, im.Day0[i:]...)...)
			return true
		}
	}
	return false
}
