// Package emu runs real network operating systems: the vendor's own VM images
// (qcow2 and the like), booted with QEMU/KVM on a Linux host that Termwright
// reaches over SSH. Nothing is installed on the host: Termwright copies the
// image there once, gives every switch a small copy-on-write disk on top of it
// and runs qemu-system-x86_64 for it, with
//
//   - the first network port as the management port: user-mode networking
//     with an SSH port forward on the host's 127.0.0.1 (no root needed), or a
//     tap on a bridge of the host (needs sudo);
//   - the other ports wired to each other point to point over UDP sockets on
//     the host's 127.0.0.1 - the lab's cables;
//   - the serial console and the QEMU monitor as telnet servers on the host's
//     127.0.0.1, reached through the SSH connection; the console is also
//     written to a log file, which shows how far the boot is;
//   - first-boot (day-0) commands typed on the console once the image is ready.
//
// This package builds the commands and reads their output; the app runs them.
package emu

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Image is a VM image and how to boot it (its profile).
type Image struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	File       string      `json:"file"`       // the image on this computer
	FileName   string      `json:"fileName"`   // its name on the hosts
	Size       int64       `json:"size"`       // bytes
	SHA256     string      `json:"sha256"`     // of the file, to know it is on a host already
	DeviceType string      `json:"deviceType"` // the sessions' device type (Linux, Cisco IOS…)
	CPUs       int         `json:"cpus"`
	RAM        int         `json:"ram"`      // MB
	NICs       int         `json:"nics"`     // network ports, the first one is management
	NIC        string      `json:"nic"`      // e1000, virtio-net-pci, vmxnet3…
	Disk       string      `json:"disk"`     // virtio, ide, sata, scsi
	PortName   string      `json:"portName"` // data port n as the OS calls it: "{n}" or "1/{n}"
	Ready      string      `json:"ready"`    // console text when it can be logged in to ("login:")
	Login      string      `json:"login"`
	Password   string      `json:"password,omitempty"` // in the keychain, never in the lab file
	Prompt     string      `json:"prompt"`             // regexp of the CLI prompt (default [>#$]\s*$)
	Day0       []string    `json:"day0"`               // first-boot commands, with {hostname} {mgmtIp} …
	AddrSet    []string    `json:"addrSet,omitempty"`  // commands that give the management port a fixed address ({mgmtAddr} {mgmtBits} {mgmtMask} {gateway}), typed at each start when the lab's management has fixed addresses
	Answers    []Answer    `json:"answers"`            // questions on the console and what to answer
	Extra      string      `json:"extra"`              // more qemu options
	Source     string      `json:"source"`             // where the profile came from (a GNS3 appliance…)
	AddrCmd    string      `json:"addrCmd"`            // a CLI command that prints the management address (host's network mode)
	LagCmd     string      `json:"lagCmd,omitempty"`   // a CLI command that lists the LAGs ("show sharing" on EXOS, "show mlt" on VOSS)
	Persona    *PersonaDef `json:"persona,omitempty"`  // a file that makes the image act as a given device
	Version    string      `json:"version"`            // the software version of the image (for the lists)
	Match      string      `json:"match,omitempty"`    // profiles: a regexp on the file name that picks this profile; a "version" group gives the version
	Serial     string      `json:"serial"`             // serial number format for new switches ({N:5} {A:2} {YY} {WW} …; "" = none)
	URL        string      `json:"url,omitempty"`      // where the image file can be downloaded (from a GNS3 appliance)
	MD5        string      `json:"md5,omitempty"`      // its MD5 sum, to check a download
	Page       string      `json:"page,omitempty"`     // a web page where it can be had (an account may be needed)
	Disks      []Disk      `json:"disks,omitempty"`    // more files the VM needs: a second disk, a CD-ROM, a BIOS
}

// Disk is another file an image boots with: "hdb", "hdc", "hdd" (more disks,
// each VM writes to a layer of its own), "cdrom" (read-only) or "bios".
type Disk struct {
	Slot     string `json:"slot"`
	FileName string `json:"fileName"` // its name on the hosts
	File     string `json:"file"`     // on this computer
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	URL      string `json:"url,omitempty"`
	MD5      string `json:"md5,omitempty"`
	Page     string `json:"page,omitempty"`
	Bus      string `json:"bus,omitempty"` // hdb…: ide, sata, scsi, virtio ("" = the image's)
}

// HasFiles says the image has all its files on this computer.
func (im *Image) HasFiles() bool {
	if im.File == "" {
		return false
	}
	for _, d := range im.Disks {
		if d.File == "" {
			return false
		}
	}
	return true
}

// Answer: when the console shows Match (plain text), Send is typed (and Enter).
type Answer struct {
	Match string `json:"match"`
	Send  string `json:"send"`
}

// Template is a device to make switches from: an image with its resources,
// the persona model, and where serial numbers and MAC addresses start.
type Template struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	CPUs        int               `json:"cpus"`
	RAM         int               `json:"ram"`    // MB
	DiskGB      int               `json:"diskGb"` // 0 = the image's size
	Model       string            `json:"model"`  // persona model ("" = none)
	Module      string            `json:"module"`
	Values      map[string]string `json:"values"`
	SerialStart string            `json:"serialStart"` // the first serial number; the next switches count up from it
	MACStart    string            `json:"macStart"`    // the first switch's base MAC; the next ones follow
}

// VM is one switch of the lab.
type VM struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"` // hostname, also the session name
	Image    string   `json:"image"`
	Index    int      `json:"index"` // its number in the lab: ports and MACs come from it
	CPUs     int      `json:"cpus"`
	RAM      int      `json:"ram"`
	MgmtIP   string   `json:"mgmtIp"` // address/bits it gets on day 0
	Gateway  string   `json:"gateway"`
	Day0Done bool     `json:"day0Done"`
	Host     string   `json:"host,omitempty"`    // the KVM host it runs on (a session path; "" = the lab's own host)
	Direct   bool     `json:"direct,omitempty"`  // its management address answers this computer straight (no jump host)
	Serial   string   `json:"serial,omitempty"`  // given to the VM as its SMBIOS serial number ("" = the VM's own)
	BaseMAC  string   `json:"baseMac,omitempty"` // port 0's MAC, the others follow ("" = from the lab)
	DiskGB   int      `json:"diskGb,omitempty"`  // grow its disk to this size (0 = the image's)
	Template string   `json:"template,omitempty"`
	Persona  *Persona `json:"persona,omitempty"`
	X        int      `json:"x"`
	Y        int      `json:"y"`
}

// Cable joins data port APort of A to BPort of B (port numbers from 1).
type Cable struct {
	A     string `json:"a"`
	APort int    `json:"aPort"`
	B     string `json:"b"`
	BPort int    `json:"bPort"`
	Down  bool   `json:"down,omitempty"` // pulled: the link is off on both ends
}

// HostNet is how a host's switches are managed (the fields of Lab for its own host).
type HostNet struct {
	Mgmt     string `json:"mgmt"`
	LanIf    string `json:"lanIf,omitempty"`
	LanKind  string `json:"lanKind,omitempty"`
	NatBr    string `json:"natBr,omitempty"`
	MgmtNet  string `json:"mgmtNet,omitempty"`
	CloudGw  string `json:"cloudGw,omitempty"`
	CloudIf  string `json:"cloudIf,omitempty"`
	HostIP   string `json:"hostIp,omitempty"`
	LanFixed bool   `json:"lanFixed,omitempty"`
}

// Net is the management of a host of the lab (main: the lab's own host).
func (l *Lab) Net(host, main string) HostNet {
	if host == "" || host == main {
		return HostNet{Mgmt: l.Mgmt, LanIf: l.LanIf, LanKind: l.LanKind, NatBr: l.NatBr, MgmtNet: l.MgmtNet, CloudGw: l.CloudGw, CloudIf: l.CloudIf, HostIP: l.HostIP, LanFixed: l.LanFixed}
	}
	if n, ok := l.Nets[host]; ok {
		return n
	}
	return HostNet{Mgmt: "forward"}
}

// SetNet sets the management of a host of the lab.
func (l *Lab) SetNet(host, main string, n HostNet) {
	if host == "" || host == main {
		l.Mgmt, l.LanIf, l.LanKind, l.NatBr, l.MgmtNet, l.CloudGw, l.CloudIf, l.HostIP, l.LanFixed = n.Mgmt, n.LanIf, n.LanKind, n.NatBr, n.MgmtNet, n.CloudGw, n.CloudIf, n.HostIP, n.LanFixed
		return
	}
	if l.Nets == nil {
		l.Nets = map[string]HostNet{}
	}
	l.Nets[host] = n
}

// HostOf is the host a VM runs on.
func (l *Lab) HostOf(v VM, main string) string {
	if v.Host == "" {
		return main
	}
	return v.Host
}

// Hosts are the lab's hosts, its own first.
func (l *Lab) Hosts(main string) []string {
	out := []string{main}
	for _, v := range l.VMs {
		if h := l.HostOf(v, main); !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// For is the lab as one host runs it: that host's management, and the VMs on
// other hosts reached at their host's address (cables to them are UDP tunnels
// between the hosts).
func (l *Lab) For(host, main string) *Lab {
	c := *l
	c.SetNet("", "", l.Net(host, main))
	c.remote = map[string]string{}
	for _, v := range l.VMs {
		if h := l.HostOf(v, main); h != host {
			c.remote[v.ID] = orStr(l.Addrs[h], "0.0.0.0")
		}
	}
	return &c
}

// Slot is a VM's place among the VMs of its host (fixed management
// addresses count from the host's first address with it).
func (l *Lab) Slot(v VM) int {
	n := 0
	for _, o := range l.VMs {
		if _, far := l.remote[o.ID]; !far && o.Index < v.Index && o.Host == v.Host {
			n++
		}
	}
	return n
}

// Lag is a link aggregation on one switch: its data ports (VM port numbers),
// static or LACP, set in Termwright or read from the switch.
type Lag struct {
	VM     string `json:"vm"`
	Name   string `json:"name"`
	Mode   string `json:"mode"` // "lacp" or "static"
	Ports  []int  `json:"ports"`
	Source string `json:"source"` // "user" or "switch"
}

// LagOf is the LAG a VM's data port belongs to (nil = none); one read
// from the switch wins over one set in Termwright.
func (l *Lab) LagOf(vm string, port int) *Lag {
	var found *Lag
	for i := range l.Lags {
		g := &l.Lags[i]
		if g.VM != vm || !slices.Contains(g.Ports, port) {
			continue
		}
		if found == nil || g.Source == "switch" {
			found = g
		}
	}
	return found
}

// Lab is what a host runs.
type Lab struct {
	ID       string  `json:"id"`                 // short, part of the folder and interface names
	Dir      string  `json:"dir"`                // on the host, relative to the home folder
	Mgmt     string  `json:"mgmt"`               // "lan" (the host's network, DHCP), "cloud" (the host's network, routed by the host: fixed addresses), "nat" (the host's NAT network, DHCP), "bridge" (a lab bridge on the host) or "forward"
	LanIf    string  `json:"lanIf"`              // lan: the host's bridge (tap on it) or network port (macvtap on it)
	NatBr    string  `json:"natBr"`              // nat: the host's NAT bridge with a DHCP server (libvirt's virbr0)
	LanKind  string  `json:"lanKind"`            // lan: "bridge" or "macvtap"
	MgmtNet  string  `json:"mgmtNet"`            // bridge mode: the bridge's network (10.99.0.0/24); cloud: the first switch's address on the host's network (10.234.179.146/24)
	CloudGw  string  `json:"cloudGw"`            // cloud (and lan with fixed addresses): the host's network's gateway
	LanFixed bool    `json:"lanFixed,omitempty"` // lan: fixed addresses from MgmtNet on (a network without DHCP)
	CloudIf  string  `json:"cloudIf"`            // cloud: the host's port on its network
	HostIP   string  `json:"hostIp"`             // cloud: the host's own address there
	Base     int     `json:"base"`               // first TCP/UDP port on the host's 127.0.0.1
	KVM      bool    `json:"kvm"`                // false: software emulation (slow, for tests)
	VMs      []VM    `json:"vms"`
	Cables   []Cable `json:"cables"`
	Lags     []Lag   `json:"lags,omitempty"`
	// Nets: the management of the lab's other hosts (its own host's is above);
	// Addrs: each host's address the others send cable traffic to.
	Nets  map[string]HostNet `json:"nets,omitempty"`
	Addrs map[string]string  `json:"addrs,omitempty"`
	// set on a host's view (For): VMs elsewhere with their host's address
	remote map[string]string
}

// Ports on the host for VM i: SSH forward, console, monitor, and data port n's UDP socket.
func (l *Lab) SSHPort(v VM) int     { return l.Base + v.Index*4 }
func (l *Lab) ConsolePort(v VM) int { return l.Base + v.Index*4 + 1 }
func (l *Lab) MonitorPort(v VM) int { return l.Base + v.Index*4 + 2 }
func (l *Lab) udpPort(v VM, n int) int {
	return l.Base + 4000 + v.Index*64 + n
}

// VMDir is the lab's folder on the host; ImageDir the shared images.
func (l *Lab) VMDir() string    { return l.Dir + "/lab-" + l.ID }
func (l *Lab) ImageDir() string { return l.Dir + "/images" }

// VMByID finds a VM.
func (l *Lab) VMByID(id string) *VM {
	for i := range l.VMs {
		if l.VMs[i].ID == id {
			return &l.VMs[i]
		}
	}
	return nil
}

// MAC of port n (0 = management) of a VM: QEMU's own prefix, unique in the lab.
func (l *Lab) MAC(v VM, n int) string {
	if hw, err := net.ParseMAC(v.BaseMAC); err == nil && len(hw) == 6 {
		x := uint64(hw[0])<<40 | uint64(hw[1])<<32 | uint64(hw[2])<<24 | uint64(hw[3])<<16 | uint64(hw[4])<<8 | uint64(hw[5])
		x += uint64(n)
		return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", byte(x>>40), byte(x>>32), byte(x>>24), byte(x>>16), byte(x>>8), byte(x))
	}
	id, _ := strconv.ParseUint(l.ID, 16, 64)
	return fmt.Sprintf("52:54:%02x:%02x:%02x:%02x", byte(id), byte(v.Index>>8), byte(v.Index), byte(n))
}

// tap is the management interface name in bridge mode (at most 15 characters).
func (l *Lab) tap(v VM) string { return fmt.Sprintf("tw%s%dm", l.ID, v.Index) }

// Bridge is the host's management bridge in bridge mode.
func (l *Lab) Bridge() string { return "tw" + l.ID + "br" }

// NatBridge is the host's NAT bridge in nat mode.
func (l *Lab) NatBridge() string {
	if l.NatBr != "" {
		return l.NatBr
	}
	return "virbr0"
}

// tapped: the management port is a tap on a bridge of the host.
func (l *Lab) tapped() bool {
	return l.Mgmt == "bridge" || l.Mgmt == "cloud" || l.Mgmt == "nat" || l.Mgmt == "lan" && l.LanKind != "macvtap"
}

// Fixed: the switches get fixed addresses (typed on their console), not DHCP.
func (l *Lab) Fixed() bool {
	return l.Mgmt == "bridge" || l.Mgmt == "cloud" || l.Mgmt == "lan" && l.LanFixed
}

// DownLinks are the data ports of a running VM whose link must be down (the
// HMP set_link names): ports without a cable (a NIC without a network would
// show its link up) and pulled cables.
func (l *Lab) DownLinks(v VM, im *Image) []string {
	cabled := map[int]bool{}
	var out []string
	for _, c := range l.Cables {
		for _, e := range []struct {
			id string
			n  int
		}{{c.A, c.APort}, {c.B, c.BPort}} {
			if e.id == v.ID {
				cabled[e.n] = true
				if c.Down {
					out = append(out, fmt.Sprintf("n%d", e.n))
				}
			}
		}
	}
	for n := 1; n < im.NICs; n++ {
		if !cabled[n] {
			out = append(out, fmt.Sprintf("nic%d", n))
		}
	}
	return out
}

// PortName is data port n as the switch calls it.
func (im *Image) PortNameOf(n int) string {
	f := im.PortName
	if f == "" {
		f = "{n}"
	}
	// {n*4-4}: a step and an offset (SONiC's Ethernet0, Ethernet4…)
	f = portFormula.ReplaceAllStringFunc(f, func(m string) string {
		p := portFormula.FindStringSubmatch(m)
		k, _ := strconv.Atoi(p[1])
		b, _ := strconv.Atoi(strings.ReplaceAll(p[2], "+", ""))
		return strconv.Itoa(n*k + b)
	})
	return strings.ReplaceAll(f, "{n}", strconv.Itoa(n))
}

var portFormula = regexp.MustCompile(`\{n\*(\d+)([+-]\d+)?\}`)

// DataPorts is how many ports can be cabled.
func (im *Image) DataPorts() int {
	if im.NICs <= 1 {
		return 0
	}
	return im.NICs - 1
}

// Args are the qemu-system-x86_64 options for a VM.
func (l *Lab) Args(v VM, im *Image) []string {
	dir := l.VMDir()
	a := []string{"-name", v.Name, "-m", strconv.Itoa(orInt(v.RAM, im.RAM, 1024)), "-smp", strconv.Itoa(orInt(v.CPUs, im.CPUs, 1))}
	if l.KVM {
		a = append(a, "-enable-kvm", "-cpu", "host")
	} else {
		a = append(a, "-accel", "tcg")
	}
	// disks: the VM's layer on the image, then its layers on more disks (hdb…)
	type hd struct{ file, bus string }
	hds := []hd{{dir + "/" + v.ID + ".qcow2", im.Disk}}
	for _, d := range im.Disks {
		if strings.HasPrefix(d.Slot, "hd") {
			hds = append(hds, hd{dir + "/" + v.ID + "-" + d.Slot + ".qcow2", orStr(d.Bus, im.Disk)})
		}
	}
	ahci := false
	for i, h := range hds {
		switch h.bus {
		case "ide", "scsi":
			a = append(a, "-drive", fmt.Sprintf("file=%s,if=%s,index=%d,format=qcow2", h.file, h.bus, i))
		case "sata":
			if !ahci {
				a, ahci = append(a, "-device", "ahci,id=ahci"), true
			}
			a = append(a, "-drive", fmt.Sprintf("file=%s,if=none,id=d%d,format=qcow2", h.file, i), "-device", fmt.Sprintf("ide-hd,drive=d%d,bus=ahci.%d", i, i))
		default:
			a = append(a, "-drive", "file="+h.file+",if=virtio,format=qcow2")
		}
	}
	for _, d := range im.Disks {
		switch d.Slot {
		case "cdrom": // read-only, straight from the image folder
			a = append(a, "-drive", "file="+l.ImageDir()+"/"+d.FileName+",media=cdrom,readonly=on,if=ide,index=3")
		case "bios":
			a = append(a, "-bios", l.ImageDir()+"/"+d.FileName)
		}
	}
	nic := im.NIC
	if nic == "" {
		nic = "e1000"
	}
	// management: the first port
	switch {
	case l.Mgmt == "lan" && l.LanKind == "macvtap":
		a = append(a, "-netdev", "tap,id=n0,fd=3") // the macvtap device, opened by the start script
	case l.tapped():
		a = append(a, "-netdev", "tap,id=n0,ifname="+l.tap(v)+",script=no,downscript=no")
	default:
		a = append(a, "-netdev", fmt.Sprintf("user,id=n0,hostfwd=tcp:127.0.0.1:%d-:22", l.SSHPort(v)))
	}
	a = append(a, "-device", nic+",netdev=n0,mac="+l.MAC(v, 0))
	// data ports: cabled ones get a UDP socket to the other end
	peer, peerAt := map[int]int{}, map[int]string{}
	for _, c := range l.Cables {
		if c.A == v.ID {
			if o := l.VMByID(c.B); o != nil {
				peer[c.APort], peerAt[c.APort] = l.udpPort(*o, c.BPort), l.remote[o.ID]
			}
		}
		if c.B == v.ID {
			if o := l.VMByID(c.A); o != nil {
				peer[c.BPort], peerAt[c.BPort] = l.udpPort(*o, c.APort), l.remote[o.ID]
			}
		}
	}
	for n := 1; n < im.NICs; n++ {
		if p, ok := peer[n]; ok && peerAt[n] != "" { // the other end on another host: a UDP tunnel between the hosts
			a = append(a, "-netdev", fmt.Sprintf("socket,id=n%d,udp=%s:%d,localaddr=0.0.0.0:%d", n, peerAt[n], p, l.udpPort(v, n)),
				"-device", fmt.Sprintf("%s,netdev=n%d,mac=%s", nic, n, l.MAC(v, n)))
		} else if ok {
			a = append(a, "-netdev", fmt.Sprintf("socket,id=n%d,udp=127.0.0.1:%d,localaddr=127.0.0.1:%d", n, p, l.udpPort(v, n)),
				"-device", fmt.Sprintf("%s,netdev=n%d,mac=%s", nic, n, l.MAC(v, n)))
		} else {
			a = append(a, "-device", fmt.Sprintf("%s,id=nic%d,mac=%s", nic, n, l.MAC(v, n)))
		}
	}
	if v.Serial != "" {
		sn := strings.ReplaceAll(v.Serial, ",", ",,")
		a = append(a, "-smbios", "type=1,serial="+sn, "-smbios", "type=2,serial="+sn, "-smbios", "type=3,serial="+sn)
	}
	a = append(a,
		"-chardev", fmt.Sprintf("socket,id=con0,host=127.0.0.1,port=%d,server=on,wait=off,telnet=on,logfile=%s/%s.log", l.ConsolePort(v), dir, v.ID),
		"-serial", "chardev:con0",
		"-monitor", fmt.Sprintf("telnet:127.0.0.1:%d,server,nowait", l.MonitorPort(v)),
		"-display", "none", "-daemonize", "-pidfile", dir+"/"+v.ID+".pid")
	if im.Extra != "" {
		a = append(a, splitArgs(im.Extra)...)
	}
	return a
}

// StartScript starts VMs (each with its image) on the host: their disks are
// made on first start, their console logs are emptied.
func (l *Lab) StartScript(vms []VM, imgs map[string]*Image) string {
	var b strings.Builder
	dir := l.VMDir()
	fmt.Fprintf(&b, "mkdir -p %s || exit 1\n", Q(dir))
	// Termwright's bridges left without ports (removed labs, an earlier management)
	// go: one with the same network would take the switches' traffic
	ours := ""
	if l.Mgmt == "bridge" || l.Mgmt == "cloud" {
		ours = l.Bridge()
	}
	fmt.Fprintf(&b, "for p in /sys/class/net/tw*br; do [ -d \"$p/bridge\" ] || continue; n=${p##*/}; [ \"$n\" = %q ] && continue; [ -z \"$(ls $p/brif 2>/dev/null)\" ] && sudo -n ip link del \"$n\" 2>/dev/null; done\n", ours)
	if l.Mgmt == "lan" && l.LanIf != "" {
		// like GNS3's cloud node: the host's port takes frames for the switches' own MACs too
		fmt.Fprintf(&b, "sudo -n ip link set %s promisc on 2>/dev/null\n", Q(l.LanIf))
	}
	if l.Mgmt == "bridge" {
		br := l.Bridge()
		gw := l.gatewayPrefix()
		fmt.Fprintf(&b, "ip link show %s >/dev/null 2>&1 || sudo -n ip link add %s type bridge\nsudo -n ip link set %s up\n", br, br, br)
		b.WriteString(onlyAddr(br, gw))
	}
	if l.Mgmt == "cloud" {
		// like GNS3's cloud, but routed: the switches sit on a bridge of the host and
		// the host answers ARP for their addresses on its network (proxy ARP) and
		// forwards for them, so the network only ever sees the host's own MAC address
		// (nothing to allow on a VM host's virtual switch, and Wi-Fi works too)
		br := l.Bridge()
		fmt.Fprintf(&b, "ip link show %s >/dev/null 2>&1 || sudo -n ip link add %s type bridge || { echo 'fail all could not make the bridge %s (sudo)'; exit 0; }\nsudo -n ip link set %s up\n", br, br, br, br)
		if ip := strings.Split(l.HostIP, "/")[0]; ip != "" {
			b.WriteString(onlyAddr(br, ip+"/32"))
		}
		fmt.Fprintf(&b, "sudo -n sysctl -qw net.ipv4.ip_forward=1 net.ipv4.conf.%s.proxy_arp=1 net.ipv4.conf.%s.proxy_arp=1 net.ipv4.conf.%s.rp_filter=0 >/dev/null\n", br, l.CloudIf, br)
		fmt.Fprintf(&b, "if command -v iptables >/dev/null 2>&1; then for d in -i -o; do sudo -n iptables -C FORWARD $d %s -j ACCEPT 2>/dev/null || sudo -n iptables -I FORWARD $d %s -j ACCEPT; done; fi\n", br, br)
		for _, v := range vms {
			if ip, _ := l.Address(l.Slot(v)); ip != "" {
				fmt.Fprintf(&b, "sudo -n ip route replace %s/32 dev %s\n", strings.Split(ip, "/")[0], br)
			}
		}
	}
	if l.Mgmt == "nat" { // libvirt's default network: started when it is not up
		br := Q(l.NatBridge())
		fmt.Fprintf(&b, "ip link show %s >/dev/null 2>&1 || sudo -n virsh -c qemu:///system net-start default >/dev/null 2>&1\n", br)
		fmt.Fprintf(&b, "ip link show %s >/dev/null 2>&1 || { echo \"fail all the host has no NAT network %s: run Set Up QEMU/KVM, which sets up libvirt's default network\"; exit 0; }\n", br, l.NatBridge())
	}
	for _, v := range vms {
		im := imgs[v.Image]
		if im == nil {
			fmt.Fprintf(&b, "echo 'fail %s no image'\n", v.ID)
			continue
		}
		fmt.Fprintf(&b, "if [ -f %s/%s.pid ] && kill -0 \"$(cat %s/%s.pid)\" 2>/dev/null; then echo 'up %s'; else\n", Q(dir), v.ID, Q(dir), v.ID, v.ID)
		b.WriteString(l.diskCmd(v, im))
		t, redir := l.tap(v), ""
		switch {
		case l.Mgmt == "lan" && l.LanKind == "macvtap":
			fmt.Fprintf(&b, " ip link show %s >/dev/null 2>&1 || sudo -n ip link add link %s name %s address %s type macvtap mode bridge; sudo -n ip link set %s up\n", t, Q(l.LanIf), t, l.MAC(v, 0), t)
			fmt.Fprintf(&b, " idx=$(cat /sys/class/net/%s/ifindex); for i in 1 2 3 4 5 6 7 8 9 10; do [ -e /dev/tap$idx ] && break; sleep 0.2; done; sudo -n chown \"$(id -un)\" /dev/tap$idx\n", t)
			redir = " 3<>/dev/tap$idx"
		case l.tapped():
			br := l.Bridge()
			switch l.Mgmt {
			case "lan":
				br = l.LanIf
			case "nat":
				br = l.NatBridge()
			}
			fmt.Fprintf(&b, " ip link show %s >/dev/null 2>&1 || sudo -n ip tuntap add %s mode tap user \"$(id -un)\"; sudo -n ip link set %s master %s up\n", t, t, t, Q(br))
		}
		fmt.Fprintf(&b, " : > %s/%s.log\n", Q(dir), v.ID)
		fmt.Fprintf(&b, " if out=$(qemu-system-x86_64 %s%s 2>&1); then echo 'started %s'; else echo \"fail %s $(echo $out | tr '\\n' ' ')\"; fi\nfi\n", joinArgs(l.Args(v, im)), redir, v.ID, v.ID)
	}
	return b.String()
}

// onlyAddr gives a bridge this one address (one left from another management goes).
func onlyAddr(br, addr string) string {
	return fmt.Sprintf("for a in $(ip -4 -o addr show dev %s | awk '{print $4}'); do [ \"$a\" = %q ] || sudo -n ip addr del \"$a\" dev %s; done\nip -4 -o addr show dev %s | grep -q ' %s ' || sudo -n ip addr add %s dev %s\n", br, addr, br, br, addr, addr, br)
}

// diskCmd makes a VM's disk (a copy-on-write layer on the image) and grows it to its size.
func (l *Lab) diskCmd(v VM, im *Image) string {
	disk := l.VMDir() + "/" + v.ID + ".qcow2"
	// a layer on the image (its format read from the file: qcow2, or raw for an .iso or .img)
	layer := func(dst, base string) string {
		return fmt.Sprintf(" [ -f %s ] || { b=\"$HOME/%s/%s\"; f=$(qemu-img info \"$b\" 2>/dev/null | sed -n 's/^file format: //p' | head -1); qemu-img create -q -f qcow2 -F \"${f:-qcow2}\" -b \"$b\" %s; } || { echo 'fail %s disk %s'; }\n", Q(dst), l.ImageDir(), base, Q(dst), v.ID, base)
	}
	s := layer(disk, im.FileName)
	for _, d := range im.Disks {
		if strings.HasPrefix(d.Slot, "hd") {
			s += layer(l.VMDir()+"/"+v.ID+"-"+d.Slot+".qcow2", d.FileName)
		}
	}
	if v.DiskGB > 0 {
		s += fmt.Sprintf(" cur=$(qemu-img info --output=json %s 2>/dev/null | sed -n 's/.*\"virtual-size\": *\\([0-9]*\\).*/\\1/p' | head -1); [ -n \"$cur\" ] && [ \"$cur\" -lt %d ] && qemu-img resize -q %s %dG\n", Q(disk), int64(v.DiskGB)<<30, Q(disk), v.DiskGB)
	}
	return s
}

// PrepareScript makes the disks of VMs without starting them.
func (l *Lab) PrepareScript(vms []VM, imgs map[string]*Image) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mkdir -p %s || exit 1\n", Q(l.VMDir()))
	for _, v := range vms {
		if im := imgs[v.Image]; im != nil {
			b.WriteString(l.diskCmd(v, im))
		}
	}
	return b.String()
}

// StopScript stops VMs (and removes their taps in bridge mode).
func (l *Lab) StopScript(ids []string) string {
	var b strings.Builder
	dir := l.VMDir()
	for _, id := range ids {
		fmt.Fprintf(&b, "if [ -f %s/%s.pid ]; then kill \"$(cat %s/%s.pid)\" 2>/dev/null; rm -f %s/%s.pid; fi\n", Q(dir), id, Q(dir), id, Q(dir), id)
		if l.Mgmt != "forward" && l.Mgmt != "" {
			if v := l.VMByID(id); v != nil {
				fmt.Fprintf(&b, "sleep 0.3; sudo -n ip link del %s 2>/dev/null\n", l.tap(*v))
				if ip, _ := l.Address(l.Slot(*v)); l.Mgmt == "cloud" && ip != "" {
					fmt.Fprintf(&b, "sudo -n ip route del %s/32 dev %s 2>/dev/null\n", strings.Split(ip, "/")[0], l.Bridge())
				}
			}
		}
		fmt.Fprintf(&b, "echo 'stopped %s'\n", id)
	}
	return b.String()
}

// ResetScript stops VMs and throws their disks away: they boot as from the image again.
func (l *Lab) ResetScript(ids []string) string {
	var b strings.Builder
	b.WriteString(l.StopScript(ids))
	for _, id := range ids {
		fmt.Fprintf(&b, "sleep 0.5; rm -f %s/%s.qcow2 %s/%s-hd?.qcow2 %s/%s.log\n", Q(l.VMDir()), id, Q(l.VMDir()), id, Q(l.VMDir()), id)
	}
	return b.String()
}

// StatusScript prints "clk <ticks per second>", a line per VM: "up <id> <CPU
// ticks used so far>" or "down <id>",
// then "log <id> <base64 of the console log's end>" for the ones in tails.
func (l *Lab) StatusScript(tails []string) string {
	dir := Q(l.VMDir())
	s := "cd " + dir + " 2>/dev/null || exit 0\necho \"clk $(getconf CLK_TCK 2>/dev/null || echo 100)\"\nawk '/^MemTotal:/{t=$2} /^MemAvailable:/{a=$2} END{print \"mem \" int(t/1024) \" \" int(a/1024)}' /proc/meminfo\necho \"cores $(nproc) $(cut -d' ' -f1 /proc/loadavg)\"\nfor p in *.pid; do [ -e \"$p\" ] || continue; n=${p%.pid}; pid=$(cat \"$p\")\n" +
		" if kill -0 \"$pid\" 2>/dev/null; then echo \"up $n $(awk '{print $14+$15}' /proc/$pid/stat 2>/dev/null) $(awk '/^VmRSS:/{print $2}' /proc/$pid/status 2>/dev/null)\"; else echo \"down $n\"; fi; done\n"
	if l.Mgmt == "nat" { // the DHCP leases of the NAT network (libvirt), then the host's neighbours
		br := Q(l.NatBridge())
		s += fmt.Sprintf("f=/var/lib/libvirt/dnsmasq/%s.status; { cat $f 2>/dev/null || sudo -n cat $f 2>/dev/null; } | awk -F'\"' '/\"ip-address\"/{ip=$4} /\"mac-address\"/{mac=$4} /}/{if(ip!=\"\"&&mac!=\"\")print \"lease\",mac,ip; ip=\"\";mac=\"\"}'\n", l.NatBridge())
		s += fmt.Sprintf("ip neigh show dev %s 2>/dev/null | awk '$2==\"lladdr\"&&$1~/^[0-9.]+$/{print \"lease\",$3,$1}'\n", br)
		s += fmt.Sprintf("echo \"natnet $(ip -4 -o addr show dev %s 2>/dev/null | awk '{print $4}' | head -1)\"\n", br)
	}
	for _, id := range tails {
		s += fmt.Sprintf("[ -f %s.log ] && echo \"log %s $(tail -c 4000 %s.log | base64 | tr -d '\\n')\"\n", id, id, id)
	}
	return s
}

// Status is what StatusScript found.
type Status struct {
	Clk      float64            // CPU ticks per second
	MemTotal int64              // the host's memory, MB
	MemFree  int64              // available now, MB
	Cores    int                // the host's CPU cores
	Load     float64            // its load average
	Up       map[string]float64 // running VMs and the CPU ticks they used
	RSS      map[string]int64   // their memory in use (resident), MB
	Log      map[string]string  // console log ends
	Leases   map[string]string  // nat: MAC address → IPv4 address on the NAT network
	NatNet   string             // nat: the host's address on it (192.168.122.1/24)
}

// ParseStatus reads StatusScript's output.
func ParseStatus(out string, b64 func(string) ([]byte, error)) Status {
	st := Status{Up: map[string]float64{}, RSS: map[string]int64{}, Log: map[string]string{}, Leases: map[string]string{}}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 3 && f[0] == "mem":
			st.MemTotal, _ = strconv.ParseInt(f[1], 10, 64)
			st.MemFree, _ = strconv.ParseInt(f[2], 10, 64)
		case len(f) == 3 && f[0] == "cores":
			st.Cores, _ = strconv.Atoi(f[1])
			st.Load, _ = strconv.ParseFloat(f[2], 64)
		case len(f) == 3 && f[0] == "lease":
			if m := strings.ToLower(f[1]); st.Leases[m] == "" { // the leases first
				st.Leases[m] = f[2]
			}
		case len(f) == 2 && f[0] == "natnet":
			st.NatNet = f[1]
		case len(f) == 2 && f[0] == "clk":
			st.Clk, _ = strconv.ParseFloat(f[1], 64)
		case len(f) >= 2 && f[0] == "up":
			c := 0.0
			if len(f) > 2 {
				c, _ = strconv.ParseFloat(f[2], 64)
			}
			st.Up[f[1]] = c
			if len(f) > 3 {
				if kb, err := strconv.ParseInt(f[3], 10, 64); err == nil {
					st.RSS[f[1]] = kb / 1024
				}
			}
		case len(f) >= 2 && f[0] == "log":
			if len(f) == 3 {
				if raw, err := b64(f[2]); err == nil {
					st.Log[f[1]] = CleanConsole(string(raw))
				}
			} else {
				st.Log[f[1]] = ""
			}
		}
	}
	return st
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b[()][A-Z0-9]|\r`)

// CleanConsole removes terminal escape sequences and telnet bytes from console text.
func CleanConsole(s string) string {
	s = ansi.ReplaceAllString(s, "")
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0xff && i+1 < len(s) { // telnet IAC
			if s[i+1] >= 251 && s[i+1] <= 254 {
				i += 2
			} else {
				i++
			}
			continue
		}
		if c == '\n' || c == '\t' || c >= 0x20 && c != 0x7f {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Ready says whether the console log shows the image's ready text.
func (im *Image) ReadyIn(log string) bool {
	r := im.Ready
	if r == "" {
		return true
	}
	return strings.Contains(log, r)
}

// gatewayPrefix: the bridge's own address (the network's first) with its bits.
func (l *Lab) gatewayPrefix() string {
	p, err := netip.ParsePrefix(l.MgmtNet)
	if err != nil {
		return "10.99.0.1/24"
	}
	return p.Masked().Addr().Next().String() + "/" + strconv.Itoa(p.Bits())
}

// Address gives VM index i its management address and gateway.
func (l *Lab) Address(i int) (string, string) {
	if l.Mgmt == "lan" && !l.LanFixed || l.Mgmt == "nat" {
		return "", "" // from the network's DHCP; learned from the switch or the host's DHCP leases
	}
	if l.Mgmt == "cloud" || l.Mgmt == "lan" { // from the first address on, the host's network's gateway
		p, err := netip.ParsePrefix(l.MgmtNet)
		if err != nil {
			return "", ""
		}
		a := p.Addr()
		for k := 0; k < i; k++ {
			a = a.Next()
		}
		return a.String() + "/" + strconv.Itoa(p.Bits()), l.CloudGw
	}
	if l.Mgmt != "bridge" {
		return "10.0.2.15/24", "10.0.2.2" // QEMU's user-mode network, the same in every VM
	}
	p, err := netip.ParsePrefix(l.MgmtNet)
	if err != nil {
		p = netip.MustParsePrefix("10.99.0.0/24")
	}
	gw := p.Masked().Addr().Next()
	a := gw
	for k := 0; k < 10+i; k++ {
		a = a.Next()
	}
	return a.String() + "/" + strconv.Itoa(p.Bits()), gw.String()
}

// FreeDataPort is the lowest data port of v without a cable.
func (l *Lab) FreeDataPort(v VM, im *Image) int {
	used := map[int]bool{}
	for _, c := range l.Cables {
		if c.A == v.ID {
			used[c.APort] = true
		}
		if c.B == v.ID {
			used[c.BPort] = true
		}
	}
	for n := 1; n <= im.DataPorts(); n++ {
		if !used[n] {
			return n
		}
	}
	return 0
}

// Wire cables new VMs: "leaf-spine" (the lab's first two VMs are the cores,
// linked to each other; every other VM to both), "chain", "ring" or "".
func (l *Lab) Wire(add []VM, kind string, imgs map[string]*Image) {
	link := func(a, b VM) {
		pa, pb := l.FreeDataPort(a, imgs[a.Image]), l.FreeDataPort(b, imgs[b.Image])
		if pa > 0 && pb > 0 {
			l.Cables = append(l.Cables, Cable{A: a.ID, APort: pa, B: b.ID, BPort: pb})
		}
	}
	switch kind {
	case "leaf-spine":
		if len(l.VMs) < 2 {
			return
		}
		cores := l.VMs[:2]
		isNew := map[string]bool{}
		for _, v := range add {
			isNew[v.ID] = true
		}
		if isNew[cores[1].ID] {
			link(cores[0], cores[1])
		}
		for _, v := range add {
			if v.ID == cores[0].ID || v.ID == cores[1].ID {
				continue
			}
			link(cores[0], v)
			link(cores[1], v)
		}
	case "chain", "ring":
		seq := add
		isNew := map[string]bool{}
		for _, v := range add {
			isNew[v.ID] = true
		}
		for i := len(l.VMs) - 1; i >= 0; i-- { // added to a lab: they go on from its last switch
			if !isNew[l.VMs[i].ID] {
				seq = append([]VM{l.VMs[i]}, add...)
				break
			}
		}
		for i := 0; i+1 < len(seq); i++ {
			link(seq[i], seq[i+1])
		}
		if kind == "ring" && len(seq) > 2 {
			link(seq[len(seq)-1], seq[0])
		}
	}
}

// Fill puts a VM's values into a day-0 command.
func Fill(cmd string, v VM, n int) string {
	addr, bits := v.MgmtIP, ""
	if p, err := netip.ParsePrefix(v.MgmtIP); err == nil {
		addr, bits = p.Addr().String(), strconv.Itoa(p.Bits())
	}
	mask := ""
	if b, err := strconv.Atoi(bits); err == nil {
		m := uint32(0xffffffff) << (32 - b)
		mask = fmt.Sprintf("%d.%d.%d.%d", m>>24, m>>16&255, m>>8&255, m&255)
	}
	r := strings.NewReplacer("{hostname}", v.Name, "{mgmtIp}", v.MgmtIP, "{mgmtAddr}", addr, "{mgmtBits}", bits, "{mgmtMask}", mask, "{gateway}", v.Gateway, "{n}", strconv.Itoa(n))
	return r.Replace(cmd)
}

// Q quotes for a POSIX shell (paths relative to the home folder stay unquoted when safe).
func Q(s string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_./:=,@+-]+$`).MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func joinArgs(a []string) string {
	q := make([]string, len(a))
	for i, s := range a {
		q[i] = Q(s)
	}
	return strings.Join(q, " ")
}

// splitArgs splits options at spaces, keeping "quoted parts" together.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == 0 && (c == ' ' || c == '\t' || c == '\n'):
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func orInt(v ...int) int {
	for _, x := range v {
		if x > 0 {
			return x
		}
	}
	return 0
}

// SortVMs orders VMs by index.
func SortVMs(v []VM) { sort.Slice(v, func(i, j int) bool { return v[i].Index < v[j].Index }) }

// NextSerial is the serial number i steps after start: its last number
// counts up, keeping its width ("AR022205Q-40036", 2 → "AR022205Q-40038").
func NextSerial(start string, i int) string {
	if start == "" || i == 0 {
		return start
	}
	j := len(start)
	for j > 0 && start[j-1] >= '0' && start[j-1] <= '9' {
		j--
	}
	digits := start[j:]
	if digits == "" {
		return fmt.Sprintf("%s-%d", start, i)
	}
	n, _ := strconv.ParseUint(digits, 10, 64)
	return start[:j] + fmt.Sprintf("%0*d", len(digits), n+uint64(i))
}

// NextMAC is the base MAC of the i-th switch from start: each switch keeps a
// block of 64 addresses (its ports take the first ones).
func NextMAC(start string, i int) string {
	hw, err := net.ParseMAC(start)
	if err != nil || len(hw) != 6 {
		return ""
	}
	x := uint64(hw[0])<<40 | uint64(hw[1])<<32 | uint64(hw[2])<<24 | uint64(hw[3])<<16 | uint64(hw[4])<<8 | uint64(hw[5])
	x += uint64(i) * 64
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", byte(x>>40), byte(x>>32), byte(x>>24), byte(x>>16), byte(x>>8), byte(x))
}
